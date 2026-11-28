package kbadmin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newFixedEmbeddingServer httptest 起 OpenAI 兼容 /embeddings 端点（固定向量返回）。
// 网络边界替身——真实链路纪律允许的 mock 边界。
func newFixedEmbeddingServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"index": i, "embedding": []float64{0.1, 0.2, 0.3, 0.4}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "model": "test-embed"})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// newTestWorkerEnv 测试环境：真实 SQLite + 真 Chroma 工厂 + httptest embedding 端点
func newTestWorkerEnv(t *testing.T) (*Store, *RetrievalKBFactory, string) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "kbadmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	factory := NewRetrievalKBFactory(t.TempDir())
	return store, factory, newFixedEmbeddingServer(t)
}

// TestWorker_全链完成 建任务→worker 认领→真 parser/chunker→embedding(httptest)→completed
func TestWorker_全链完成(t *testing.T) {
	store, factory, embedURL := newTestWorkerEnv(t)
	_ = store.KB().Create(&KBInfo{ID: "kb-1", Type: "document",
		ConfigJSON: `{"embedding":{"provider":"openai","model_name":"m","base_url":"` + embedURL + `","api_key":"k"}}`,
		CreatedAt: 1, UpdatedAt: 1})
	if err := factory.CreateCollection(context.Background(), "kb-1", ""); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fp := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(fp, []byte("第一段内容，用于解析测试。\n\n第二段内容足够长以触发切分行为，产生至少一个分块。"), 0o644)
	svc := NewDocService(store)
	w := NewParseWorker(store, factory, 2)
	sink := NewProgressSink(store, nil)
	w.SetSink(sink)
	svc.SetEnqueue(w.Enqueue)
	task, doc, err := svc.SubmitParseTask("kb-1", "a.txt", fp, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	t.Cleanup(w.Stop)
	waitTaskStatus(t, store, task.ID, "completed", 15*time.Second)
	gotDoc, _ := store.Doc().Get(doc.ID)
	if gotDoc.ParseStatus != "completed" {
		t.Fatalf("doc 应 completed: stages=%s err=%s status=%s", gotDoc.StagesJSON, gotDoc.ErrorMsg, gotDoc.ParseStatus)
	}
	chunks, _ := store.Chunk().ListByDoc(doc.ID)
	if len(chunks) == 0 {
		t.Fatal("chunk 台账应有行")
	}
	for _, ch := range chunks {
		if !ch.Enabled || ch.Revision != 1 {
			t.Fatalf("chunk 台账默认字段不符: %+v", ch)
		}
	}
}

// TestWorker_CAS单认领 并发认领同一 pending 任务仅一次成功
func TestWorker_CAS单认领(t *testing.T) {
	store, _, _ := newTestWorkerEnv(t)
	_ = store.KB().Create(&KBInfo{ID: "kb-1", CreatedAt: 1, UpdatedAt: 1})
	_ = store.Doc().Create(&KBDoc{ID: "d1", KBID: "kb-1", ParseStatus: "pending", CreatedAt: 1, UpdatedAt: 1})
	_ = store.Task().Create(&KBTask{ID: "t1", DocID: "d1", KBID: "kb-1", Status: "pending", CreatedAt: 1})
	w := NewParseWorker(store, nil, 1)
	ok1 := w.claimTask("t1")
	ok2 := w.claimTask("t1")
	if !ok1 || ok2 {
		t.Fatalf("CAS 认领应恰一次成功: %v %v", ok1, ok2)
	}
}

// TestWorker_重启孤儿恢复 processing→cancelled(server_restart)；pending→重新入队
func TestWorker_重启孤儿恢复(t *testing.T) {
	store, factory, _ := newTestWorkerEnv(t)
	now := time.Now().Unix()
	_ = store.KB().Create(&KBInfo{ID: "kb-1", CreatedAt: now, UpdatedAt: now})
	_ = store.Doc().Create(&KBDoc{ID: "d1", KBID: "kb-1", ParseStatus: "processing", CreatedAt: now, UpdatedAt: now})
	_ = store.Task().Create(&KBTask{ID: "t-orphan", DocID: "d1", KBID: "kb-1", Status: "processing", CreatedAt: now})
	_ = store.Task().Create(&KBTask{ID: "t-pending", DocID: "d1", KBID: "kb-1", Status: "pending", CreatedAt: now})
	w := NewParseWorker(store, factory, 1)
	w.recoverOrphans()
	t1, _ := store.Task().Get("t-orphan")
	if t1.Status != "cancelled" || t1.CancelReason != "server_restart" {
		t.Fatalf("孤儿应 cancelled(server_restart): %+v", t1)
	}
	d1, _ := store.Doc().Get("d1")
	if d1.ParseStatus != "cancelled" {
		t.Fatalf("孤儿文档状态应跟随 cancelled, got %s", d1.ParseStatus)
	}
	select {
	case id := <-w.wake:
		if id != "t-pending" {
			t.Fatalf("应唤醒 pending 行, got %s", id)
		}
	default:
		t.Fatal("pending 行未入队")
	}
}

// TestWorker_取消中途退出 embedding 阶段挂起时 cancel → cancelled 终态
func TestWorker_取消中途退出(t *testing.T) {
	store, factory, embedURL := newTestWorkerEnv(t)
	_ = store.KB().Create(&KBInfo{ID: "kb-1", Type: "document",
		ConfigJSON: `{"embedding":{"provider":"openai","model_name":"m","base_url":"` + embedURL + `","api_key":"k"}}`,
		CreatedAt: 1, UpdatedAt: 1})
	if err := factory.CreateCollection(context.Background(), "kb-1", ""); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fp := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(fp, []byte("cancel test content"), 0o644)
	svc := NewDocService(store)
	w := NewParseWorker(store, factory, 1)
	sink := NewProgressSink(store, nil)
	w.SetSink(sink)
	svc.SetEnqueue(w.Enqueue)
	svc.SetCancelFn(w.CancelTask)
	task, _, err := svc.SubmitParseTask("kb-1", "a.txt", fp, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	t.Cleanup(w.Stop)
	// 等任务进入 processing 后取消
	waitTaskStatus(t, store, task.ID, "processing", 5*time.Second)
	if err := svc.CancelParse(task.ID); err != nil {
		t.Fatal(err)
	}
	waitTaskStatus(t, store, task.ID, "cancelled", 10*time.Second)
	if t.Failed() {
		docAll, _ := store.Doc().ListByKB("kb-1")
		for _, d := range docAll {
			t.Logf("doc %s status=%s err=%s stages=%s", d.ID, d.ParseStatus, d.ErrorMsg, d.StagesJSON)
		}
	}
}

// waitTaskStatus 轮询等待任务进入期望状态
func waitTaskStatus(t *testing.T, store *Store, taskID, status string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		task, err := store.Task().Get(taskID)
		if err == nil && task.Status == status {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	task, _ := store.Task().Get(taskID)
	t.Errorf("任务未在 %v 内进入 %s: %+v", timeout, status, task)
	if docs, err := store.Doc().ListByKB(task.KBID); err == nil {
		for _, d := range docs {
			t.Logf("doc %s status=%s err=%s stages=%s", d.ID, d.ParseStatus, d.ErrorMsg, d.StagesJSON)
		}
	} else {
		t.Logf("ListByKB err: %v", err)
	}
	t.FailNow()
}
