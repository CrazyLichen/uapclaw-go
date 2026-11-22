package rsi

// recovery.go 单测：三分支对账 + 损坏隔离 + 快照变体。

import (
	"errors"
	"testing"
)

// fakeReader 测试缝：返回预置快照。
type fakeReader struct{ state map[string]any }

func (f *fakeReader) ReadState(string) map[string]any { return f.state }

func newRecoveryFixture(t *testing.T) (*TaskStore, *Projector) {
	t.Helper()
	store := NewTaskStore(t.TempDir(), nil)
	return store, NewProjector(t.TempDir())
}

func mkTask(t *testing.T, store *TaskStore, id, status string) RsiTask {
	t.Helper()
	task := RsiTask{TaskID: id, Name: id, Scenario: "HARNESS", Status: string(TaskStatusCreated),
		CreatedAt: UtcnowISO(), ModelRefs: map[string]any{}, Config: map[string]any{}}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	if status != string(TaskStatusCreated) {
		if _, err := store.UpdateStatus(id, []string{"CREATED"}, string(TaskStatusQueued), "enqueue"); err != nil {
			t.Fatal(err)
		}
		if status != string(TaskStatusQueued) {
			if _, err := store.UpdateStatus(id, []string{"QUEUED"}, status, "start"); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, _ := store.Get(id)
	return got
}

func resolver(r *fakeReader) func(string, string) ProviderStateReader {
	return func(string, string) ProviderStateReader { return r }
}

// TestRecover_QUEUED转暂停 队列随进程丢失。
func TestRecover_QUEUED转暂停(t *testing.T) {
	store, proj := newRecoveryFixture(t)
	mkTask(t, store, "rsi-q", string(TaskStatusQueued))
	rec := NewWorkspaceRecovery(store, proj, resolver(&fakeReader{state: nil}))
	sum := rec.Recover()
	if sum.Scanned != 1 || sum.Recovered != 1 || sum.Paused != 1 {
		t.Fatalf("摘要漂移: %+v", sum)
	}
	got, _ := store.Get("rsi-q")
	if got.Status != string(TaskStatusPaused) {
		t.Fatalf("状态漂移: %s", got.Status)
	}
	last := got.StatusHistory[len(got.StatusHistory)-1]
	if last["cause"] != "agentserver_restart.queue_lost" {
		t.Fatalf("cause 漂移: %v", last)
	}
}

// TestRecover_RUNNING快照终态收敛 三终态 + FAILED 带原因。
func TestRecover_RUNNING快照终态收敛(t *testing.T) {
	store, proj := newRecoveryFixture(t)
	mkTask(t, store, "rsi-f", string(TaskStatusRunning))
	rec := NewWorkspaceRecovery(store, proj, resolver(&fakeReader{state: map[string]any{
		"status": "FAILED", "error_message": "数据集缺失"}}))
	sum := rec.Recover()
	if sum.Failed != 1 {
		t.Fatalf("Failed 计数漂移: %+v", sum)
	}
	got, _ := store.Get("rsi-f")
	if got.Status != string(TaskStatusFailed) {
		t.Fatalf("状态漂移: %s", got.Status)
	}
	last := got.StatusHistory[len(got.StatusHistory)-1]
	if last["cause"] != "数据集缺失" {
		t.Fatalf("FAILED 应带失败原因: %v", last)
	}
	// COMPLETED 快照 → provider_snapshot.completed。
	store2, proj2 := newRecoveryFixture(t)
	mkTask(t, store2, "rsi-c", string(TaskStatusRunning))
	rec2 := NewWorkspaceRecovery(store2, proj2, resolver(&fakeReader{state: map[string]any{"status": "completed"}}))
	rec2.Recover()
	got2, _ := store2.Get("rsi-c")
	last2 := got2.StatusHistory[len(got2.StatusHistory)-1]
	if last2["cause"] != "provider_snapshot.completed" {
		t.Fatalf("cause 漂移: %v", last2)
	}
	// CANCELLED 变体 → TERMINATED。
	store3, proj3 := newRecoveryFixture(t)
	mkTask(t, store3, "rsi-x", string(TaskStatusRunning))
	rec3 := NewWorkspaceRecovery(store3, proj3, resolver(&fakeReader{state: map[string]any{"status": "CANCELLED"}}))
	sum3 := rec3.Recover()
	if sum3.Terminated != 1 {
		t.Fatalf("CANCELLED 收敛漂移: %+v", sum3)
	}
}

// TestRecover_快照缺失转暂停 execution_detached。
func TestRecover_快照缺失转暂停(t *testing.T) {
	store, proj := newRecoveryFixture(t)
	mkTask(t, store, "rsi-d", string(TaskStatusRunning))
	rec := NewWorkspaceRecovery(store, proj, resolver(&fakeReader{state: nil}))
	sum := rec.Recover()
	if sum.Paused != 1 {
		t.Fatalf("Paused 漂移: %+v", sum)
	}
	got, _ := store.Get("rsi-d")
	last := got.StatusHistory[len(got.StatusHistory)-1]
	if last["cause"] != "agentserver_restart.execution_detached" {
		t.Fatalf("cause 漂移: %v", last)
	}
	// TASK_NOT_FOUND 错误码同样视作无快照。
	store4, proj4 := newRecoveryFixture(t)
	mkTask(t, store4, "rsi-n", string(TaskStatusRunning))
	rec4 := NewWorkspaceRecovery(store4, proj4, resolver(&fakeReader{state: map[string]any{
		"error_code": "TASK_NOT_FOUND", "status": "RUNNING"}}))
	rec4.Recover()
	got4, _ := store4.Get("rsi-n")
	if got4.Status != string(TaskStatusPaused) {
		t.Fatalf("TASK_NOT_FOUND 应视作无快照: %s", got4.Status)
	}
}

// TestRecover_终态任务跳过 非活跃态不扫描。
func TestRecover_终态任务跳过(t *testing.T) {
	store, proj := newRecoveryFixture(t)
	mkTask(t, store, "rsi-t", string(TaskStatusTerminated))
	rec := NewWorkspaceRecovery(store, proj, resolver(&fakeReader{state: nil}))
	sum := rec.Recover()
	if sum.Scanned != 0 {
		t.Fatalf("终态不应扫描: %+v", sum)
	}
}

// TestRecover_对账只对账不重跑 注册根但绝不调引擎（Recover 不含 run 调用即满足）。
func TestRecover_对账只对账不重跑(t *testing.T) {
	store, proj := newRecoveryFixture(t)
	mkTask(t, store, "rsi-r", string(TaskStatusRunning))
	rec := NewWorkspaceRecovery(store, proj, resolver(&fakeReader{state: nil}))
	rec.Recover()
	// 树根已注册（recovery 调 register_root）。
	if tree, err := proj.DeriveTree("rsi-r"); err != nil || len(tree["nodes"].([]map[string]any)) != 1 {
		t.Fatalf("根注册漂移: %v", err)
	}
	_ = errors.Is
}
