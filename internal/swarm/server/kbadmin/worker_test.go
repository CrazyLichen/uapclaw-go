package kbadmin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/indexing/processor/extractor"
)

// newFixedEmbeddingServer httptest 起 OpenAI 兼容 /embeddings 端点（固定向量返回）。
// 网络边界替身——真实链路纪律允许的 mock 边界。150ms 固定延迟拉长 processing
// 窗口（管线全链仅数十 ms 时轮询可能整窗错过，取消类用例会 flake）
func newFixedEmbeddingServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
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

// fakeTripleExtractor 测试用三元组抽取器（实现 extractor.Extractor 接口）
type fakeTripleExtractor struct{}

func (f *fakeTripleExtractor) Extract(_ context.Context, chunks []common.TextChunk,
	_ ...extractor.ExtractOption) ([]common.Triple, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	return []common.Triple{{
		Subject: "主题A", Predicate: "包含", Object: "对象B",
		Metadata: map[string]any{"chunk_id": chunks[0].ID, "doc_id": chunks[0].DocID},
	}}, nil
}

func (f *fakeTripleExtractor) Process(ctx context.Context, chunks []common.TextChunk,
	opts ...extractor.ExtractOption) ([]common.Triple, error) {
	return f.Extract(ctx, chunks, opts...)
}

// TestWorker_graph类型_triple索引 graph 库全链：解析→切分→向量→triple 索引→completed
func TestWorker_graph类型_triple索引(t *testing.T) {
	store, factory, embedURL := newTestWorkerEnv(t)
	_ = store.KB().Create(&KBInfo{ID: "kg-1", Type: "graph",
		ConfigJSON: `{"embedding":{"provider":"openai","model_name":"m","base_url":"` + embedURL + `","api_key":"k"}}`,
		CreatedAt: 1, UpdatedAt: 1})
	if err := factory.CreateCollection(context.Background(), "kg-1", ""); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fp := filepath.Join(dir, "g.txt")
	_ = os.WriteFile(fp, []byte("graph kb triple extraction test content for pipeline."), 0o644)
	svc := NewDocService(store)
	w := NewParseWorker(store, factory, 1)
	w.SetSink(NewProgressSink(store, nil))
	w.SetTripleExtractor(&fakeTripleExtractor{})
	svc.SetEnqueue(w.Enqueue)
	task, doc, err := svc.SubmitParseTask("kg-1", "g.txt", fp, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	t.Cleanup(w.Stop)
	waitTaskStatus(t, store, task.ID, "completed", 15*time.Second)
	gotDoc, _ := store.Doc().Get(doc.ID)
	if gotDoc.ParseStatus != "completed" {
		t.Fatalf("graph doc 应 completed, got %s (%s)", gotDoc.ParseStatus, gotDoc.ErrorMsg)
	}
}

// TestWorker_graph类型_无抽取器跳过triple 未装配抽取器 = 跳过 triple 步不失败
func TestWorker_graph类型_无抽取器跳过triple(t *testing.T) {
	store, factory, embedURL := newTestWorkerEnv(t)
	_ = store.KB().Create(&KBInfo{ID: "kg-2", Type: "graph",
		ConfigJSON: `{"embedding":{"provider":"openai","model_name":"m","base_url":"` + embedURL + `","api_key":"k"}}`,
		CreatedAt: 1, UpdatedAt: 1})
	if err := factory.CreateCollection(context.Background(), "kg-2", ""); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fp := filepath.Join(dir, "g2.txt")
	_ = os.WriteFile(fp, []byte("graph kb without extractor content."), 0o644)
	svc := NewDocService(store)
	w := NewParseWorker(store, factory, 1)
	w.SetSink(NewProgressSink(store, nil))
	svc.SetEnqueue(w.Enqueue)
	task, doc, err := svc.SubmitParseTask("kg-2", "g2.txt", fp, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	t.Cleanup(w.Stop)
	waitTaskStatus(t, store, task.ID, "completed", 15*time.Second)
	gotDoc, _ := store.Doc().Get(doc.ID)
	if gotDoc.ParseStatus != "completed" {
		t.Fatalf("无抽取器 graph doc 应 completed, got %s", gotDoc.ParseStatus)
	}
}

// TestWorker_ConfigAccessor 工厂 config 登记访问器
func TestWorker_ConfigAccessor(t *testing.T) {
	f := NewRetrievalKBFactory(t.TempDir())
	if _, ok := f.Config("nope"); ok {
		t.Fatal("未登记应 false")
	}
	_ = f.CreateCollection(context.Background(), "kb-c", `{"max_file_mb":7}`)
	cfg, ok := f.Config("kb-c")
	if !ok || cfg.MaxFileMB != 7 {
		t.Fatalf("config 登记不符: %+v ok=%v", cfg, ok)
	}
}

// TestDocErrCode 错误哨兵映射
func TestDocErrCode(t *testing.T) {
	if docErrCode(ErrDocDuplicate) != "DOC_DUPLICATE" {
		t.Fatal("DOC_DUPLICATE 映射不符")
	}
	if docErrCode(ErrDocParseActive) != "TASK_ACTIVE" {
		t.Fatal("TASK_ACTIVE 映射不符")
	}
	if docErrCode(ErrDocNotFound) != ErrCodeKBNotFound {
		t.Fatal("NotFound 映射不符")
	}
	if docErrCode(errors.New("x")) != ErrCodeKBInternal {
		t.Fatal("默认映射不符")
	}
}
