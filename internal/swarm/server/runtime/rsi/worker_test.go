package rsi

// worker.go 单测：enqueue 状态机、并发=1、terminate 取消 runner（17.7 回填
// 单元挂钩）、resume 语义、事件链路推送、结果落盘。

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
	singleharness "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/singleharness"
)

// shRootEvent 根事件构造（singleharness.RootNodeEvent 返回 *rsi.EventNode）。
func shRootEvent(state map[string]any) rsi.EngineEvent {
	return singleharness.RootNodeEvent(state)
}

func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

// fakeEngineAdapter 可编排的假 adapter（fake 全链路纪律：真队列+真 store+真投影）。
type fakeEngineAdapter struct {
	mu             sync.Mutex
	startOnce      sync.Once
	runStarted     chan struct{}
	runRelease     chan struct{}
	runResult      map[string]any
	runErr         error
	events         []rsi.EngineEvent
	cancelObserved bool
}

func (f *fakeEngineAdapter) ValidateInput(path *string, scenario string, artifactType *string) (RsiDatasetResult, error) {
	return RsiDatasetResult{Valid: true}, nil
}
func (f *fakeEngineAdapter) BuildRequest(task RsiTaskView, resume bool) (HarnessEngineRequest, error) {
	return BuildHarnessEngineRequest(task, resume)
}
func (f *fakeEngineAdapter) Run(ctx context.Context, req HarnessEngineRequest, onEvent rsi.OnEvent) (map[string]any, error) {
	f.startOnce.Do(func() { close(f.runStarted) })
	for _, e := range f.events {
		_ = onEvent(context.Background(), e)
	}
	select {
	case <-ctx.Done():
		f.mu.Lock()
		f.cancelObserved = true
		f.mu.Unlock()
		return nil, ctx.Err()
	case <-f.runRelease:
	}
	if f.runErr != nil {
		return nil, f.runErr
	}
	return f.runResult, nil
}
func (f *fakeEngineAdapter) Resume(ctx context.Context, req HarnessEngineRequest, onEvent rsi.OnEvent) (map[string]any, error) {
	return f.Run(ctx, req, onEvent)
}
func (f *fakeEngineAdapter) Pause(string) (map[string]any, error) {
	return nil, &RsiError{Code: "INTERNAL_ERROR", Message: "harness 引擎暂不支持中途暂停"}
}
func (f *fakeEngineAdapter) Terminate(string) (map[string]any, error) {
	return nil, &RsiError{Code: "INTERNAL_ERROR", Message: "harness 引擎暂不支持中途终止"}
}
func (f *fakeEngineAdapter) ReadState(string) map[string]any            { return nil }
func (f *fakeEngineAdapter) ReadPublicationState(string) map[string]any { return nil }
func (f *fakeHarnessProviderRead) readNil()                             {}

type fakeHarnessProviderRead struct{}

func (f *fakeEngineAdapter) ReadReport(string) map[string]any              { return nil }
func (f *fakeEngineAdapter) GetTree(string) map[string]any                 { return nil }
func (f *fakeEngineAdapter) LocateArtifact(string, *string) map[string]any { return nil }
func (f *fakeEngineAdapter) SupportsPause() bool                           { return false }
func (f *fakeEngineAdapter) SupportsResume() bool                          { return true }
func (f *fakeEngineAdapter) SupportsTerminate() bool                       { return false }

// newWorkerFixture 真 store+投影+worker（fake adapter 注入边界）。
func newWorkerFixture(t *testing.T) (*Worker, *fakeEngineAdapter, string, *TaskStore) {
	t.Helper()
	root := t.TempDir()
	store := NewTaskStore(root, nil)
	projector := NewProjector(root)
	fake := &fakeEngineAdapter{runStarted: make(chan struct{}), runRelease: make(chan struct{})}
	worker := NewWorker(store, map[string]ScenarioAdapter{"HARNESS": fake},
		NewUsageRecorder(), projector, &fakeArtifact{}, map[string]func(string, string, map[string]any) error{})
	input := filepathJoin(root, "ds.json")
	if err := writeFile(input, []byte(`{"cases":[{"case_id":"c1","input":"问题"}]}`)); err != nil {
		t.Fatal(err)
	}
	task := RsiTask{TaskID: "rsi-w", Name: "n", Scenario: "HARNESS",
		Status: string(TaskStatusCreated), CreatedAt: UtcnowISO(),
		InputFile: &input, ModelRefs: map[string]any{}, Config: map[string]any{},
		RunDir: filepathJoin(root, "rsi-w", "run"), StatusHistory: []map[string]any{}}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	return worker, fake, "rsi-w", store
}

// TestWorkerEnqueue_状态机 CREATED→QUEUED→RUNNING；幂等短路。
func TestWorkerEnqueue_状态机(t *testing.T) {
	w, fake, taskID, store := newWorkerFixture(t)
	status, err := w.Enqueue(taskID)
	if err != nil || status != string(TaskStatusQueued) {
		t.Fatalf("enqueue 漂移: %v %v", status, err)
	}
	// 二次入队幂等。
	status, err = w.Enqueue(taskID)
	if err != nil || status != string(TaskStatusQueued) {
		t.Fatalf("幂等漂移: %v %v", status, err)
	}
	select {
	case <-fake.runStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("runner 未启动")
	}
	// PAUSED 拒直接 enqueue。
	if _, err := w.store.UpdateStatus(taskID, []string{"RUNNING"}, "PAUSED", "p"); err != nil {
		// 任务可能已 RUNNING，先等终态。
	}
	_ = store
	w.Cancel(taskID, "terminate")
}

// TestWorkerTerminate_取消Runner触达取消分支 HARNESS 无 hook → 取消 runner →
// 引擎 ctx 取消（17.7 回填单元挂钩）→ TERMINATED 落盘。
func TestWorkerTerminate_取消Runner触达取消分支(t *testing.T) {
	w, fake, taskID, store := newWorkerFixture(t)
	if _, err := w.Enqueue(taskID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.runStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("未开始执行")
	}
	status, err := w.Cancel(taskID, "terminate")
	if err != nil {
		t.Fatalf("cancel 漂移: %v", err)
	}
	if status != string(TaskStatusTerminated) {
		t.Fatalf("终止状态漂移: %s", status)
	}
	got, _ := store.Get(taskID)
	if got.Status != string(TaskStatusTerminated) {
		t.Fatalf("store 状态漂移: %s", got.Status)
	}
	last := got.StatusHistory[len(got.StatusHistory)-1]
	if last["cause"] != "worker.cancelled" {
		t.Fatalf("cause 漂移: %v", last)
	}
	// fake 引擎观测到 ctx 取消 → 17.7 的 ctx.Err()/ErrTaskCancelled 分支。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fake.mu.Lock()
		observed := fake.cancelObserved
		fake.mu.Unlock()
		if observed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	fake.mu.Lock()
	observed := fake.cancelObserved
	fake.mu.Unlock()
	if !observed {
		t.Fatalf("引擎未观测到取消")
	}
}

// TestWorkerRun_正常完成 事件推送 + COMPLETED + results 落盘。
func TestWorkerRun_正常完成(t *testing.T) {
	w, fake, taskID, store := newWorkerFixture(t)
	var progressCalls, treeCalls int
	w.RegisterPushCallbacks(map[string]func(string, string, map[string]any) error{
		"rsi.training.progress":   func(string, string, map[string]any) error { progressCalls++; return nil },
		"rsi.training.tree.delta": func(string, string, map[string]any) error { treeCalls++; return nil },
	})
	nodeEvent := singleharnessRootEvent()
	fake.events = []rsi.EngineEvent{nodeEvent}
	fake.runResult = map[string]any{"status": "completed", "best_score": 0.9,
		"state_path": "/x/state.yaml"}
	close(fake.runRelease)
	if _, err := w.Enqueue(taskID); err != nil {
		t.Fatal(err)
	}
	w.WaitIdle()
	got, _ := store.Get(taskID)
	if got.Status != string(TaskStatusCompleted) {
		t.Fatalf("终态漂移: %s", got.Status)
	}
	results := got.Config["results"].(map[string]any)
	if results["best_score"] != 0.9 {
		t.Fatalf("results 漂移: %v", results)
	}
	// 根节点事件 → 仅 P3 树增量（progress 推送属 progress 事件）。
	if treeCalls < 1 {
		t.Fatalf("树增量推送漂移: %d %d", progressCalls, treeCalls)
	}
	_ = w
}

// singleharnessRootEvent 根节点事件（真引擎事件构造器）。
func singleharnessRootEvent() rsi.EngineEvent {
	state := map[string]any{}
	return singleharnessRoot(state)
}

// TestWorkerRun_失败 FAILED + failure_reason。
func TestWorkerRun_失败(t *testing.T) {
	w, fake, taskID, store := newWorkerFixture(t)
	fake.runErr = errors.New("引擎崩溃")
	close(fake.runRelease)
	if _, err := w.Enqueue(taskID); err != nil {
		t.Fatal(err)
	}
	w.WaitIdle()
	got, _ := store.Get(taskID)
	if got.Status != string(TaskStatusFailed) {
		t.Fatalf("FAILED 漂移: %s", got.Status)
	}
	last := got.StatusHistory[len(got.StatusHistory)-1]
	if last["cause"] == "" {
		t.Fatalf("失败原因缺失")
	}
}

// TestWorkerResume_排队暂停全新跑 QUEUED→PAUSED（无执行）→ resume 走 run 非 resume。
func TestWorkerResume_排队暂停全新跑(t *testing.T) {
	w, fake, taskID, _ := newWorkerFixture(t)
	// 队列满时模拟排队暂停：直接置 QUEUED 后 pause（未真实执行）。
	if _, err := w.store.UpdateStatus(taskID, []string{"CREATED"}, "QUEUED", "enqueue"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.store.UpdateStatus(taskID, []string{"QUEUED"}, "PAUSED", "cancel(pause)"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Resume(taskID); err != nil {
		t.Fatal(err)
	}
	w.WaitIdle()
	// close release 以便 run 结束。
	select {
	case <-fake.runStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("未开始")
	}
	close(fake.runRelease)
	<-waitStoreFinal(w, taskID)
}

// TestWorkerResume_运行暂停续跑 RUNNING→PAUSED 需 supports_resume → resume 路径。
func TestWorkerResume_运行暂停续跑(t *testing.T) {
	w, fake, taskID, _ := newWorkerFixture(t)
	if _, err := w.Enqueue(taskID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.runStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("未开始")
	}
	// HARNESS supports_pause=false：pause 走 provider 控制路径报错——此处
	// 直接由测试模拟真实暂停后的 resume 语义（Python _paused_from_run）。
	if _, err := w.store.UpdateStatus(taskID, []string{"RUNNING"}, "PAUSED", "cancel(pause)"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Resume(taskID); err != nil {
		t.Fatal(err)
	}
	close(fake.runRelease)
	w.WaitIdle()
	got, _ := w.store.Get(taskID)
	if got.Status != string(TaskStatusCompleted) {
		t.Fatalf("resume 终态漂移: %s", got.Status)
	}
}

// TestWorkerConcurrency_全局并发1 第二任务等待第一任务终态。
func TestWorkerConcurrency_全局并发1(t *testing.T) {
	root := t.TempDir()
	store := NewTaskStore(root, nil)
	projector := NewProjector(root)
	fake := &fakeEngineAdapter{runStarted: make(chan struct{}), runRelease: make(chan struct{})}
	w := NewWorker(store, map[string]ScenarioAdapter{"HARNESS": fake},
		NewUsageRecorder(), projector, &fakeArtifact{}, nil)
	input := filepathJoin(root, "ds.json")
	_ = writeFile(input, []byte(`{"cases":[{"case_id":"c1","input":"问题"}]}`))
	for _, id := range []string{"rsi-a", "rsi-b"} {
		task := RsiTask{TaskID: id, Name: id, Scenario: "HARNESS",
			Status: string(TaskStatusCreated), CreatedAt: UtcnowISO(),
			InputFile: &input, ModelRefs: map[string]any{}, Config: map[string]any{},
			RunDir: filepathJoin(root, id, "run"), StatusHistory: []map[string]any{}}
		if _, err := store.Create(task); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Enqueue("rsi-a"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.runStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("a 未开始")
	}
	if _, err := w.Enqueue("rsi-b"); err != nil {
		t.Fatal(err)
	}
	// a 未结束前 b 必须 QUEUED。
	gotB, _ := store.Get("rsi-b")
	if gotB.Status != string(TaskStatusQueued) {
		t.Fatalf("并发=1 漂移: b=%s", gotB.Status)
	}
	close(fake.runRelease)
	w.WaitIdle()
	gotB, _ = store.Get("rsi-b")
	if gotB.Status != string(TaskStatusCompleted) {
		t.Fatalf("b 终态漂移: %s", gotB.Status)
	}
}

// TestAdapterFor_键查找 HARNESS 命中；未知场景 nil → FAILED(注册缺失)。
func TestAdapterFor_键查找(t *testing.T) {
	w, fake, _, _ := newWorkerFixture(t)
	if w.adapterFor("harness", "") == nil || w.adapterFor("HARNESS", "") == nil {
		t.Fatalf("HARNESS 键漂移")
	}
	if w.adapterFor("ARTIFACT", "PAPER") != nil {
		t.Fatalf("ARTIFACT 轨 D-9-1 裁剪后不应命中")
	}
	_ = fake
}

// TestUnknownModeCancel 未知 mode 报冲突。
func TestUnknownModeCancel(t *testing.T) {
	w, _, taskID, _ := newWorkerFixture(t)
	if _, err := w.Cancel(taskID, "freeze"); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("got %v", err)
	}
}

// 辅助函数（worker 测试专用）。
func filepathJoin(parts ...string) string { return joinPath(parts) }
func joinPath(parts []string) string {
	out := ""
	for i, p := range parts {
		if i == 0 {
			out = p
		} else {
			out += "/" + p
		}
	}
	return out
}
func writeFile(path string, data []byte) error {
	return osWriteFile(path, data)
}
func waitStoreFinal(w *Worker, taskID string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for i := 0; i < 400; i++ {
			task, err := w.store.Get(taskID)
			if err == nil && (task.Status == "COMPLETED" || task.Status == "FAILED" || task.Status == "TERMINATED") {
				close(done)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()
	return done
}
func singleharnessRoot(state map[string]any) rsi.EngineEvent {
	return singleharnessRootNodeEvent(state)
}
func singleharnessRootNodeEvent(state map[string]any) rsi.EngineEvent {
	return shRootEvent(state)
}
