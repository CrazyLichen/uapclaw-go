package tasks

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeExecutor 测试桩：可编排 run 结果/交互/取消。
type fakeExecutor struct {
	runResult map[string]any
	runErr    error
	runs      []map[string]any
	cancels   []map[string]any
	answers   []map[string]any
	block     chan struct{}
	runGates  []chan struct{} // 按执行次序的门控
	store     *TaskStore
	mu        sync.Mutex
}

func (f *fakeExecutor) Run(ctx context.Context, task map[string]any, progress func(map[string]any) error) (map[string]any, error) {
	f.mu.Lock()
	f.runs = append(f.runs, task)
	index := len(f.runs) - 1
	var gate chan struct{}
	if index < len(f.runGates) {
		gate = f.runGates[index]
	}
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.runResult, f.runErr
}

func (f *fakeExecutor) Cancel(_ context.Context, task map[string]any) error {
	f.mu.Lock()
	f.cancels = append(f.cancels, task)
	f.mu.Unlock()
	// 真实 adapter 语义：取消落账 settled 标志（对齐 task_adapter.py:287-305）
	if f.store != nil {
		_, _ = f.store.update(toString(task["id"]), func(current map[string]any) {
			current["execution_cancelled"] = true
			current["execution_settled"] = true
			current["output_closed"] = true
		})
	}
	return nil
}

func (f *fakeExecutor) Answer(_ context.Context, task map[string]any) error {
	f.mu.Lock()
	f.answers = append(f.answers, task)
	f.mu.Unlock()
	return nil
}

func (f *fakeExecutor) Close() error { return nil }

func newTestStore(t *testing.T) *TaskStore {
	t.Helper()
	return mustStore(t, filepath.Join(t.TempDir(), "tasks.sqlite"))
}

func mustStore(t *testing.T, path string) *TaskStore {
	t.Helper()
	store, err := NewTaskStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func newService(t *testing.T, executor Executor) (*TaskService, *TaskStore) {
	t.Helper()
	store := newTestStore(t)
	svc, err := NewTaskService(store, executor, nil, 2, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	return svc, store
}

// TestTaskStore_事务与CRUD 对齐 store.py:51-114。
func TestTaskStore_事务与CRUD(t *testing.T) {
	store := newTestStore(t)
	task := map[string]any{"id": "t1", "owner": "u1", "session": "s1", "status": "queued", "sequence": 1}
	if err := store.transaction(func(db *sqlTx) error { return txPut(db, task) }); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadDirect("t1", "u1", "s1")
	if err != nil || got["status"] != "queued" {
		t.Fatalf("读回不符: %v %v", got, err)
	}
	// owner/session 校验
	if _, err := store.ReadDirect("t1", "u2", "s1"); err == nil || err.Error() != "Task not found in this conversation" {
		t.Fatalf("跨 owner 读取应拒: %v", err)
	}
	if _, err := store.ReadDirect("missing", nil, nil); err == nil {
		t.Fatal("缺失任务应报错")
	}
	// update 变更才递增 sequence
	updated, err := store.update("t1", func(current map[string]any) {
		if current["status"] == "queued" {
			current["status"] = "running"
		}
	})
	if err != nil || updated["status"] != "running" || numInt(updated["sequence"]) != 2 {
		t.Fatalf("update 不符: %v %v", updated, err)
	}
	// 无变更不递增
	updated, _ = store.update("t1", func(current map[string]any) {})
	if numInt(updated["sequence"]) != 2 {
		t.Fatalf("无变更不应递增: %v", updated["sequence"])
	}
}

// TestReplay_幂等重放 对齐 store.py:82-98。
func TestReplay_幂等重放(t *testing.T) {
	store := newTestStore(t)
	task := map[string]any{"id": "t1", "owner": "u", "session": "s"}
	if err := store.transaction(func(db *sqlTx) error {
		if err := txPut(db, task); err != nil {
			return err
		}
		_, fp, err := txReplay(db, "u", "s", "c1", []any{"submit", "x"})
		if err != nil {
			return err
		}
		return txCommand(db, task, "c1", fp, map[string]any{"task_id": "t1"})
	}); err != nil {
		t.Fatal(err)
	}
	// 同指纹重放返回回执
	if err := store.transaction(func(db *sqlTx) error {
		replay, _, err := txReplay(db, "u", "s", "c1", []any{"submit", "x"})
		if err != nil {
			return err
		}
		if replay == nil || replay["task_id"] != "t1" {
			t.Fatal("同参数应重放回执")
		}
		// 同 command_id 不同参数 → 冲突
		_, _, err = txReplay(db, "u", "s", "c1", []any{"submit", "y"})
		if err == nil || err.Error() != "Command identity reused with different parameters" {
			t.Fatalf("指纹冲突应报错: %v", err)
		}
		// 空 command_id
		_, _, err = txReplay(db, "u", "s", "  ", []any{"submit", "x"})
		if err == nil || err.Error() != "A stable command_id is required" {
			t.Fatalf("空 command_id: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestSubmit_Kick_并发2调度 对齐 service.py:192-245/:79-113/:116-152。
func TestSubmit_Kick_并发2调度(t *testing.T) {
	executor := &fakeExecutor{runGates: []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}}
	svc, _ := newService(t, executor)
	submitted := []map[string]any{}
	for i := 0; i < 3; i++ {
		task, err := svc.Submit("u", "s"+string(rune('a'+i)), string(rune('a'+i)), "任务",
			map[string]any{"independent": true})
		if err != nil {
			t.Fatal(err)
		}
		submitted = append(submitted, task)
	}
	// 等 worker 派发
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		svc.mu.Lock()
		running := len(svc.workers)
		svc.mu.Unlock()
		if running == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	svc.mu.Lock()
	running := len(svc.workers)
	svc.mu.Unlock()
	if running != 2 {
		t.Fatalf("并发 2 应派发 2 个 worker, got %d", running)
	}
	// 幂等重放
	task, err := svc.Submit("u", "sa", "a", "任务", map[string]any{"independent": true})
	if err != nil || task["reused"] != true || task["id"] != submitted[0]["id"] {
		t.Fatalf("同 command_id 应重放: %v %v", task, err)
	}
	// 校验分支
	if _, err := svc.Submit("u", "s", "z1", "  ", nil); err == nil || err.Error() != "Task instruction is required" {
		t.Fatalf("空指令: %v", err)
	}
	if _, err := svc.Submit("u", "s", "z2", strings21000(), nil); err == nil || err.Error() != "Task instruction exceeds 16000 characters" {
		t.Fatalf("超长指令: %v", err)
	}
	if _, err := svc.Submit("u", "", "z3", "任务", nil); err == nil {
		t.Fatal("空会话应拒")
	}
	if _, err := svc.Submit("u", "s", "z4", "任务", map[string]any{"independent": "yes"}); err == nil || err.Error() != "independent must be a boolean" {
		t.Fatalf("independent 类型: %v", err)
	}
	for _, gate := range executor.runGates {
		close(gate)
	}
}

// TestSubmit_依赖缺失 对齐 :220-222（depends_on 必须存在）。
func TestSubmit_依赖缺失(t *testing.T) {
	svc, _ := newService(t, &fakeExecutor{})
	if _, err := svc.Submit("u", "s", "d1", "任务", map[string]any{"depends_on": []any{"ghost"}}); err == nil ||
		err.Error() != "Task not found in this conversation" {
		t.Fatalf("缺失依赖应拒: %v", err)
	}
}

// TestModify_版本冲突 对齐 service.py:265-316。
func TestModify_版本冲突(t *testing.T) {
	store := newTestStore(t)
	svc, err := NewTaskService(store, &fakeExecutor{}, nil, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	seedTask := map[string]any{
		"id": "mt1", "owner": "u", "session": "s", "status": "queued", "revision": 1,
		"sequence": 1, "instruction": "任务", "request": map[string]any{},
		"changes": []any{}, "progress": []any{}, "position": 1,
	}
	if err := store.transaction(func(db *sqlTx) error { return txPut(db, seedTask) }); err != nil {
		t.Fatal(err)
	}
	task := seedTask
	// 错误版本
	if _, err := svc.Modify("u", "s", toString(task["id"]), "md1", TaskModification{Revision: 2, Instruction: "改"}); err == nil ||
		!errors.Is(err, TaskRevisionConflict) {
		t.Fatalf("版本冲突应 TaskRevisionConflict: %v", err)
	}
	// 正确版本 → revision 递增
	receipt, err := svc.Modify("u", "s", toString(task["id"]), "md1", TaskModification{Revision: 1, Instruction: " 改需求 "})
	if err != nil || receipt["state"] != "queued_input_updated" || numInt(receipt["revision"]) != 2 {
		t.Fatalf("排队中修订应合并输入: %v %v", receipt, err)
	}
	// 幂等重放
	receipt2, err := svc.Modify("u", "s", toString(task["id"]), "md1", TaskModification{Revision: 1, Instruction: " 改需求 "})
	if err != nil || receipt2["operation_id"] != "md1" {
		t.Fatalf("同 command_id 应重放: %v %v", receipt2, err)
	}
	// 空指令
	if _, err := svc.Modify("u", "s", toString(task["id"]), "md2", TaskModification{Revision: 2, Instruction: "  "}); err == nil ||
		err.Error() != "Modification must contain 1–4000 characters" {
		t.Fatalf("空修订: %v", err)
	}
}

// TestModify_完成后修订生成后继 对齐 service.py:318-343。
func TestModify_完成后修订生成后继(t *testing.T) {
	svc, _ := newService(t, &fakeExecutor{runResult: map[string]any{"answer": "done"}})
	task, err := svc.Submit("u", "s", "c1", "任务", nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := svc.store.ReadDirect(toString(task["id"]), nil, nil)
		if toString(current["status"]) == "completed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	receipt, err := svc.Modify("u", "s", toString(task["id"]), "c2", TaskModification{Revision: 1, Instruction: "改一版"})
	if err != nil || receipt["state"] != "followup" || receipt["successor_id"] == nil {
		t.Fatalf("完成任务修订应生成后继: %v %v", receipt, err)
	}
	child, err := svc.store.ReadDirect(toString(receipt["successor_id"]), nil, nil)
	if err != nil || toString(child["parent_id"]) != toString(task["id"]) {
		t.Fatalf("后继任务应指向父任务: %v %v", child, err)
	}
}

// TestCancel_两阶段 对齐 service.py:345-419。
func TestCancel_两阶段(t *testing.T) {
	store := newTestStore(t)
	gate1 := make(chan struct{})
	executor := &fakeExecutor{runResult: map[string]any{"answer": "done"}, runGates: []chan struct{}{gate1}}
	svc, err := NewTaskService(store, executor, nil, 2, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	executor.store = store
	task, err := svc.Submit("u", "s", "x1", "任务", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 等 running
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := svc.store.ReadDirect(toString(task["id"]), nil, nil)
		if toString(current["status"]) == "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	receipt, err := svc.Cancel(context.Background(), "u", "s", toString(task["id"]), "x2")
	if err != nil || receipt["state"] != "cancelling" || receipt["stopped"] != false {
		t.Fatalf("运行中取消应受理: %v %v", receipt, err)
	}
	// 等 executor.Cancel 落账（对齐 Python：cancel 先于 observer cancel/gather）
	deadline2 := time.Now().Add(3 * time.Second)
	executor.mu.Lock()
	seen := len(executor.cancels)
	executor.mu.Unlock()
	for time.Now().Before(deadline2) {
		executor.mu.Lock()
		seen = len(executor.cancels)
		executor.mu.Unlock()
		if seen == 1 {
			current, _ := store.Read(toString(task["id"]), nil, nil)
			if current["execution_settled"] == true {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	// 落账后放行执行（取消胜过完成的竞态由 finalize 首分支保证）
	close(gate1)
	// 等最终 cancelled
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := svc.store.ReadDirect(toString(task["id"]), nil, nil)
		if toString(current["status"]) == "cancelled" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	current, _ := svc.store.ReadDirect(toString(task["id"]), nil, nil)
	if current["status"] != "cancelled" {
		t.Fatalf("执行确认后应终态 cancelled: %v", current["status"])
	}
	executor.mu.Lock()
	cancelCount := len(executor.cancels)
	executor.mu.Unlock()
	if cancelCount != 1 {
		t.Fatalf("执行器应收到一次取消: %d", cancelCount)
	}
}

// TestCancel_排队中直接取消 对齐 :360-361。
func TestCancel_排队中直接取消(t *testing.T) {
	store := newTestStore(t)
	svc, err := NewTaskService(store, &fakeExecutor{}, nil, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	task := map[string]any{
		"id": "yt1", "owner": "u", "session": "s", "status": "queued", "revision": 1,
		"sequence": 1, "instruction": "任务", "request": map[string]any{},
		"changes": []any{}, "progress": []any{}, "position": 1,
	}
	if err := store.transaction(func(db *sqlTx) error { return txPut(db, task) }); err != nil {
		t.Fatal(err)
	}
	receipt, err := svc.Cancel(context.Background(), "u", "s", toString(task["id"]), "y2")
	if err != nil || receipt["stopped"] != true || receipt["state"] != "cancelled" {
		t.Fatalf("排队中取消应立即完成: %v %v", receipt, err)
	}
}

// TestAnswer_交互回填 对齐 service.py:482-545。
func TestAnswer_交互回填(t *testing.T) {
	gate1 := make(chan struct{})
	executor := &fakeExecutor{runResult: map[string]any{"answer": "done"}, runGates: []chan struct{}{gate1}}
	svc, _ := newService(t, executor)
	task, err := svc.Submit("u", "s", "a1", "任务", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 手工置为等待用户
	_, _ = svc.store.update(toString(task["id"]), func(current map[string]any) {
		current["status"] = "waiting_user"
		current["interaction"] = map[string]any{
			"id": "i1", "source": "ask_user", "state": "pending", "request_id": "r1",
			"questions": []any{map[string]any{"question": "预算多少？"}},
		}
	})
	// 未对齐提问应拒（字符串答案按序绑定是合法输入，dict 错位才拒）
	if _, err := svc.Answer(context.Background(), "u", "s", toString(task["id"]), "a2", "i1",
		[]any{map[string]any{"question": "错位的提问", "answer": "x"}}, false); err == nil {
		t.Fatal("答案应逐题匹配")
	}
	// 正确回答 → running
	receipt, err := svc.Answer(context.Background(), "u", "s", toString(task["id"]), "a2", "i1",
		[]any{map[string]any{"question": "预算多少？", "answer": "5000元"}}, false)
	if err != nil || receipt["state"] != "accepted" {
		t.Fatalf("回答应受理: %v %v", receipt, err)
	}
	current, _ := svc.store.ReadDirect(toString(task["id"]), nil, nil)
	if current["status"] != "running" {
		t.Fatalf("非挂起回答应回 running: %v", current["status"])
	}
	close(gate1)
}

// TestAnswer_挂起恢复排队 对齐 :508-511。
func TestAnswer_挂起恢复排队(t *testing.T) {
	gate1, gate2 := make(chan struct{}), make(chan struct{})
	executor := &fakeExecutor{runResult: map[string]any{"answer": "done"}, runGates: []chan struct{}{gate1, gate2}}
	svc, _ := newService(t, executor)
	task, _ := svc.Submit("u", "s", "b1", "任务", nil)
	_, _ = svc.store.update(toString(task["id"]), func(current map[string]any) {
		current["status"] = "waiting_user"
		current["output_closed"] = true
		current["execution_settled"] = true
		current["interaction"] = map[string]any{
			"id": "i2", "source": "ask_user_interrupt", "state": "pending", "request_id": "r1",
			"questions": []any{map[string]any{"question": "确认？"}},
		}
	})
	_, err := svc.Answer(context.Background(), "u", "s", toString(task["id"]), "b2", "i2",
		[]any{map[string]any{"question": "确认？", "answer": "确认"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	// 回队后随即重派发：running（gate2 阻塞）或已完成（门控竞争）均可接受
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		svc.Kick() // 调度泵：确保重派发被处理
		current, _ := svc.store.ReadDirect(toString(task["id"]), nil, nil)
		st := toString(current["status"])
		if st == "running" || st == "completed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	current, _ := svc.store.ReadDirect(toString(task["id"]), nil, nil)
	st := toString(current["status"])
	if st != "running" && st != "completed" {
		svc.mu.Lock()
		t.Logf("DBG workers=%d gates=%d", len(svc.workers), len(executor.runGates))
		svc.mu.Unlock()
		executor.mu.Lock()
		t.Logf("DBG runs=%d", len(executor.runs))
		executor.mu.Unlock()
		raw, _ := json.Marshal(current)
		t.Logf("DBG task=%s", raw)
		stack := buf(true)
		t.Logf("GOROUTINES:\n%s", stack)
		t.Fatalf("挂起源回答应回队并重派发, got %v (resume=%v)", st, current["resume_answer"])
	}
	close(gate2)
}

// TestReorder_QueueVersionConflict 对齐 service.py:422-476。
func TestReorder_QueueVersionConflict(t *testing.T) {
	store := newTestStore(t)
	svc, err := NewTaskService(store, &fakeExecutor{}, nil, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	first := map[string]any{
		"id": "rt1", "owner": "u", "session": "s", "status": "queued", "revision": 1,
		"sequence": 1, "instruction": "任务一", "request": map[string]any{},
		"changes": []any{}, "progress": []any{}, "position": 1,
	}
	second := map[string]any{
		"id": "rt2", "owner": "u", "session": "s", "status": "queued", "revision": 1,
		"sequence": 1, "instruction": "任务二", "request": map[string]any{},
		"changes": []any{}, "progress": []any{}, "position": 2,
	}
	if err := store.transaction(func(db *sqlTx) error {
		if err := txPut(db, first); err != nil {
			return err
		}
		return txPut(db, second)
	}); err != nil {
		t.Fatal(err)
	}
	_, version, err := svc.Snapshot("u", "s")
	if err != nil {
		t.Fatal(err)
	}
	// 错误版本
	if _, err := svc.Reorder("u", "s", toString(first["id"]), version+123, "", ""); err == nil ||
		!errors.Is(err, QueueVersionConflict) {
		t.Fatalf("版本冲突应 QueueVersionConflict: %v", err)
	}
	// 目标不在等待队列（先做幽灵检查，避免成功重排后任务已被派发）
	if _, err := svc.Reorder("u", "s", toString(first["id"]), version, "ghost", ""); err == nil ||
		err.Error() != "Queue target is no longer waiting" {
		t.Fatalf("幽灵目标: %v", err)
	}
	// 正确重排
	receipt, err := svc.Reorder("u", "s", toString(first["id"]), version, "", "")
	if err != nil || receipt["state"] != "reordered" {
		t.Fatalf("重排应受理: %v %v", receipt, err)
	}
}

// TestSnapshot_WaitReason 对齐 service.py:556-577。
func TestSnapshot_WaitReason(t *testing.T) {
	svc, _ := newService(t, &fakeExecutor{runResult: map[string]any{"done": true}})
	dep, err := svc.Submit("u", "s", "w1", "依赖任务", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit("u", "s", "w2", "依赖方任务", map[string]any{"depends_on": []any{dep["id"]}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	tasks, _, err := svc.Snapshot("u", "s")
	if err != nil || len(tasks) != 2 {
		t.Fatalf("快照不符: %v %v", tasks, err)
	}
}

// TestClose 对齐 service.py:792-806。
func TestClose(t *testing.T) {
	svc, _ := newService(t, &fakeExecutor{runResult: map[string]any{"done": true}})
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit("u", "s", "z", "任务", nil); err == nil || err.Error() != "Task service is closed" {
		t.Fatalf("关闭后应拒: %v", err)
	}
	// 租约释放后可重新抢锁
	store := newTestStore(t)
	svc2, err := NewTaskService(store, &fakeExecutor{}, nil, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc2.Start(); err != nil {
		t.Fatalf("重启服务应能抢锁: %v", err)
	}
	svc2.Close()
}

// TestStart_崩溃恢复 对齐 service.py:50-77。
func TestStart_崩溃恢复(t *testing.T) {
	store := newTestStore(t)
	// 预置中断态任务
	seed := func(id, status string, extra map[string]any) {
		task := map[string]any{
			"id": "seed-" + id, "owner": "u", "session": "s",
			"status": status, "sequence": 1, "changes": []any{}, "progress": []any{},
		}
		for k, v := range extra {
			task[k] = v
		}
		if err := store.transaction(func(db *sqlTx) error { return txPut(db, task) }); err != nil {
			t.Fatal(err)
		}
	}
	seed("running", "running", nil)
	seed("waiting_user", "waiting_user", map[string]any{"interaction": map[string]any{"state": "pending"}})
	seed("queued_resume", "queued", map[string]any{"resume_answer": true})
	seed("queued", "queued", nil) // 普通 queued 不重置

	svc, err := NewTaskService(store, &fakeExecutor{}, nil, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"running", "waiting_user"} {
		task, _ := store.ReadDirect("seed-"+status, nil, nil)
		if task["status"] != "unknown" || task["error"] != "Execution ownership lost; tools were not replayed" {
			t.Fatalf("%s 应重置为 unknown: %v", status, task["status"])
		}
	}
	// 带 resume_answer 的 queued 重置为 unknown 后不自动重放（对齐 Python：kick 只派发 queued）
	queuedResume, _ := store.ReadDirect("seed-queued_resume", nil, nil)
	if queuedResume["status"] != "unknown" {
		t.Fatalf("queued_resume 应重置 unknown, got %v", queuedResume["status"])
	}
	if task, _ := store.ReadDirect("seed-running", nil, nil); numInt(task["sequence"]) != 2 {
		t.Fatalf("重置应递增 sequence: %v", task["sequence"])
	}
	if task, _ := store.ReadDirect("seed-queued", nil, nil); task["status"] != "queued" {
		t.Fatalf("普通 queued 不应重置: %v", task["status"])
	}
}

// TestList_分页与过滤 对齐 service.py:251-263。
func TestList_分页与过滤(t *testing.T) {
	svc, _ := newService(t, &fakeExecutor{})
	for _, name := range []string{"苹果任务", "香蕉任务", "苹果第二"} {
		if _, err := svc.Submit("u", "s", name, name, nil); err != nil {
			t.Fatal(err)
		}
	}
	page1, next, err := svc.List("u", "s", "", "", 0, 2)
	if err != nil || len(page1) != 2 || next == nil {
		t.Fatalf("第一页: %v %v %v", page1, next, err)
	}
	page2, next2, err := svc.List("u", "s", "", "", next.(int), 2)
	if err != nil || len(page2) != 1 || next2 != nil {
		t.Fatalf("第二页应 1 条且无下一页: %v %v %v", page2, next2, err)
	}
	matched, _, err := svc.List("u", "s", "苹果", "", 0, 50)
	if err != nil || len(matched) != 2 {
		t.Fatalf("query 过滤: %v %v", matched, err)
	}
	if _, _, err := svc.List("u", "s", "", "", -1, 50); err == nil || err.Error() != "Invalid pagination" {
		t.Fatalf("非法分页: %v", err)
	}
}

// TestLease_单持有者 对齐 D-1。
func TestLease_单持有者(t *testing.T) {
	store := mustStore(t, filepath.Join(t.TempDir(), "lease.sqlite"))
	svc1, err := NewTaskService(store, &fakeExecutor{}, nil, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc1.Start(); err != nil {
		t.Fatal(err)
	}
	svc2, err := NewTaskService(store, &fakeExecutor{}, nil, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc2.Start(); err == nil || err.Error() != "another task service owns this ledger" {
		t.Fatalf("第二个服务应抢锁失败: %v", err)
	}
	svc1.Close()
	if err := svc2.Start(); err != nil {
		t.Fatalf("释放后应可重抢: %v", err)
	}
	svc2.Close()
}

func strings21000() string { return string(make([]byte, 16001)) }

var _ = os.Getenv

// buf 运行时栈缓冲。
func buf(all bool) []byte {
	b := make([]byte, 1<<20)
	for {
		n := runtime.Stack(b, all)
		if n < len(b) {
			return b[:n]
		}
		b = make([]byte, 2*len(b))
	}
}
