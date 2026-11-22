package rsi

// task_store.go 单测：状态机全迁移矩阵、P1 钩子、delete 守卫、merge 语义。

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newTestStore 构造带 3 个任务的临时存储。
func newTestStore(t *testing.T) (*TaskStore, RsiTask) {
	t.Helper()
	store := NewTaskStore(t.TempDir(), nil)
	task := RsiTask{TaskID: "rsi-t1", Name: "n", Scenario: "HARNESS",
		Status: string(TaskStatusCreated), CreatedAt: UtcnowISO(),
		ModelRefs: map[string]any{}, Config: map[string]any{}, StatusHistory: []map[string]any{}}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	return store, task
}

// TestUpdateStatus_非法迁移报StateConflict CREATED→RUNNING 必须拒
// （Python 表：CREATED 只允许→QUEUED/TERMINATED）。
func TestUpdateStatus_非法迁移报StateConflict(t *testing.T) {
	store, _ := newTestStore(t)
	_, err := store.UpdateStatus("rsi-t1", []string{"CREATED"}, "RUNNING", "cause")
	if !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("got %v", err)
	}
	// 终态封闭：COMPLETED 不可再迁移。
	if _, err := store.UpdateStatus("rsi-t1", []string{"CREATED"}, "QUEUED", "enqueue"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateStatus("rsi-t1", []string{"QUEUED"}, "RUNNING", "start"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateStatus("rsi-t1", []string{"RUNNING"}, "COMPLETED", "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateStatus("rsi-t1", []string{"COMPLETED"}, "TERMINATED", "x"); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("终态应封闭: %v", err)
	}
}

// TestUpdateStatus_来源不匹配报错 from_states 不含当前状态时拒绝
// （错误文案含允许来源排序，对齐 Python）。
func TestUpdateStatus_来源不匹配报错(t *testing.T) {
	store, _ := newTestStore(t)
	_, err := store.UpdateStatus("rsi-t1", []string{"RUNNING", "PAUSED"}, "QUEUED", "requeue")
	if !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("got %v", err)
	}
	if want := "允许来源: [PAUSED RUNNING]"; !containsAll(err.Error(), want) {
		t.Fatalf("文案漂移: %q", err.Error())
	}
}

func containsAll(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// TestUpdateStatus_合法迁移触发P1钩子 钩子锁外逐次回调。
func TestUpdateStatus_合法迁移触发P1钩子(t *testing.T) {
	var calls [][3]string
	store := NewTaskStore(t.TempDir(), func(id, old, new string) { calls = append(calls, [3]string{id, old, new}) })
	task := RsiTask{TaskID: "rsi-h", Name: "n", Scenario: "HARNESS",
		Status: string(TaskStatusCreated), CreatedAt: UtcnowISO(),
		ModelRefs: map[string]any{}, Config: map[string]any{}, StatusHistory: []map[string]any{}}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateStatus("rsi-h", []string{"CREATED"}, "QUEUED", "enqueue"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != [3]string{"rsi-h", "CREATED", "QUEUED"} {
		t.Fatalf("钩子漂移: %v", calls)
	}
	// 历史记录落盘。
	got, _ := store.Get("rsi-h")
	if len(got.StatusHistory) != 1 || got.StatusHistory[0]["cause"] != "enqueue" {
		t.Fatalf("status_history 漂移: %v", got.StatusHistory)
	}
	if got.UpdatedAt == nil {
		t.Fatalf("updated_at 未写")
	}
}

// TestMergeResults_None不合并 对齐 Python results.items() if v is not None。
func TestMergeResults_None不合并(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.MergeResults("rsi-t1", map[string]any{"status": "COMPLETED", "empty": nil}); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get("rsi-t1")
	results := got.Config["results"].(map[string]any)
	if results["status"] != "COMPLETED" {
		t.Fatalf("results 漂移: %v", results)
	}
	if _, ok := results["empty"]; ok {
		t.Fatalf("nil 值不应合并")
	}
	// 二次合并不覆盖已有键（update 语义）。
	if err := store.MergeResults("rsi-t1", map[string]any{"score": 9.5}); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Get("rsi-t1")
	results = got.Config["results"].(map[string]any)
	if results["status"] != "COMPLETED" || results["score"] != 9.5 {
		t.Fatalf("增量合并漂移: %v", results)
	}
}

// TestDelete守卫 RUNNING/PAUSED 拒删；产物在用拒删；释放后可删。
func TestDelete守卫(t *testing.T) {
	store, _ := newTestStore(t)
	// RUNNING 拒删。
	if _, err := store.UpdateStatus("rsi-t1", []string{"CREATED"}, "QUEUED", "e"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateStatus("rsi-t1", []string{"QUEUED"}, "RUNNING", "s"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("rsi-t1", true, true); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("RUNNING 应拒删: %v", err)
	}
	// PAUSED 拒删。
	if _, err := store.UpdateStatus("rsi-t1", []string{"RUNNING"}, "PAUSED", "p"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("rsi-t1", true, true); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("PAUSED 应拒删: %v", err)
	}
	// 产物在用拒删（harness_refs_path 且未 released）。
	if _, err := store.UpdateStatus("rsi-t1", []string{"PAUSED"}, "TERMINATED", "term"); err != nil {
		t.Fatal(err)
	}
	if err := store.MergeConfig("rsi-t1", map[string]any{"harness_refs_path": "/x/refs.yaml"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("rsi-t1", true, true); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("产物在用应拒删: %v", err)
	}
	// 释放后可删。
	if err := store.MarkActiveRefReleased("rsi-t1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("rsi-t1", true, true); err != nil {
		t.Fatalf("released 后应可删: %v", err)
	}
	if _, err := store.Get("rsi-t1"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("删除后应 NOT_FOUND: %v", err)
	}
}

// TestList过滤与排序 scenario/artifact_type 过滤 + created_at 逆序。
func TestList过滤与排序(t *testing.T) {
	store := NewTaskStore(t.TempDir(), nil)
	for i, id := range []string{"rsi-a", "rsi-b"} {
		task := RsiTask{TaskID: id, Name: id, Scenario: "HARNESS", Status: "CREATED",
			CreatedAt: UtcnowISO(), ModelRefs: map[string]any{}, Config: map[string]any{}}
		if i == 1 {
			at := "2999-01-01T00:00:00+00:00"
			task.CreatedAt = at
		}
		if _, err := store.Create(task); err != nil {
			t.Fatal(err)
		}
	}
	all := store.List("", "")
	if len(all) != 2 || all[0].TaskID != "rsi-b" {
		t.Fatalf("created_at 逆序漂移: %v", all)
	}
	if got := store.List("ARTIFACT", ""); len(got) != 0 {
		t.Fatalf("scenario 过滤失效")
	}
}

// Test原子写任务文件 tmp→rename 后无残留。
func Test原子写任务文件(t *testing.T) {
	store, _ := newTestStore(t)
	dir := TaskDir(store.TasksRoot(), "rsi-t1")
	if _, err := os.Stat(filepath.Join(dir, "task.json.tmp")); !os.IsNotExist(err) {
		t.Fatalf("tmp 文件残留")
	}
	if _, err := os.Stat(filepath.Join(dir, "task.json")); err != nil {
		t.Fatalf("task.json 缺失: %v", err)
	}
}

// TestGet不存在报NotFound 对齐 Python RsiTaskNotFound(task_id)。
func TestGet不存在报NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	_, err := store.Get("rsi-none")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("got %v", err)
	}
	if err.Error() != "任务不存在: rsi-none" {
		t.Fatalf("文案漂移: %q", err.Error())
	}
}
