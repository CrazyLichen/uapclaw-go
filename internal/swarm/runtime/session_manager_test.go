package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetSessionID 缺省落到 default。
func TestGetSessionID(t *testing.T) {
	if got := GetSessionID(nil); got != "default" {
		t.Errorf("nil 应为 default，实际 %q", got)
	}
	empty := ""
	if got := GetSessionID(&empty); got != "default" {
		t.Errorf("空串应为 default，实际 %q", got)
	}
	sid := "s1"
	if got := GetSessionID(&sid); got != "s1" {
		t.Errorf("应为 s1，实际 %q", got)
	}
}

// TestIsOneShotSession 一次性会话前缀判定。
func TestIsOneShotSession(t *testing.T) {
	for _, id := range []string{"health_check_1", "heartbeat", "heartbeat_x", "cron", "cron_1"} {
		if !isOneShotSession(id) {
			t.Errorf("%q 应判为一次性会话", id)
		}
	}
	for _, id := range []string{"s1", "session", "mycron"} {
		if isOneShotSession(id) {
			t.Errorf("%q 不应判为一次性会话", id)
		}
	}
}

// TestSessionManager_SubmitAndWait 同会话串行执行。
func TestSessionManager_SubmitAndWait(t *testing.T) {
	manager := NewSessionManager()
	ctx := context.Background()

	var order []string
	var mu sync.Mutex
	record := func(tag string) {
		mu.Lock()
		order = append(order, tag)
		mu.Unlock()
	}

	_, _ = manager.SubmitAndWait(ctx, "s1", func(context.Context) (any, error) {
		record("first")
		time.Sleep(20 * time.Millisecond)
		return "1", nil
	})
	_, _ = manager.SubmitAndWait(ctx, "s1", func(context.Context) (any, error) {
		record("second")
		return "2", nil
	})

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Errorf("同会话应串行，实际顺序 %v", order)
	}
}

// TestSessionManager_SubmitAndWait_返回值 结果透传。
func TestSessionManager_SubmitAndWait_返回值(t *testing.T) {
	manager := NewSessionManager()
	value, err := manager.SubmitAndWait(context.Background(), "s1",
		func(context.Context) (any, error) {
			return "hello", nil
		})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if value != "hello" {
		t.Errorf("返回值应为 hello，实际 %v", value)
	}
}

// TestSessionManager_SubmitAndWait_返回错误 任务错误透传给调用方。
func TestSessionManager_SubmitAndWait_返回错误(t *testing.T) {
	manager := NewSessionManager()
	_, err := manager.SubmitAndWait(context.Background(), "s1", func(context.Context) (any, error) {
		return nil, context.Canceled
	})
	if err == nil {
		t.Fatal("任务错误应透传")
	}
}

// TestSessionManager_HasSessionRuntime 排队后为 true，关闭后为 false。
func TestSessionManager_HasSessionRuntime(t *testing.T) {
	manager := NewSessionManager()
	sid := "s1"
	if manager.HasSessionRuntime(&sid) {
		t.Error("新建后不应有会话运行时")
	}
	_, _ = manager.SubmitAndWait(context.Background(), sid, func(context.Context) (any, error) {
		return nil, nil
	})
	if !manager.HasSessionRuntime(&sid) {
		t.Error("提交后应保留会话运行时（processor 常驻）")
	}
	cleaned, err := manager.CloseSession(context.Background(), sid, nil)
	if err != nil {
		t.Fatalf("关闭不应报错: %v", err)
	}
	if !cleaned {
		t.Error("关闭应报告清理了运行时")
	}
	if manager.HasSessionRuntime(&sid) {
		t.Error("关闭后不应再有会话运行时")
	}
}

// TestSessionManager_HasSessionRuntime_全表 传 nil 查全表。
func TestSessionManager_HasSessionRuntime_全表(t *testing.T) {
	manager := NewSessionManager()
	if manager.HasSessionRuntime(nil) {
		t.Error("空管理器不应有运行时")
	}
	_, _ = manager.SubmitAndWait(context.Background(), "s1", func(context.Context) (any, error) {
		return nil, nil
	})
	if !manager.HasSessionRuntime(nil) {
		t.Error("有会话时应报告全表存在运行时")
	}
}

// TestSessionManager_CloseSession_无运行时 返回 false。
func TestSessionManager_CloseSession_无运行时(t *testing.T) {
	manager := NewSessionManager()
	cleaned, err := manager.CloseSession(context.Background(), "nope", nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if cleaned {
		t.Error("无运行时应返回 false")
	}
}

// TestSessionManager_OneShot哨兵 一次性会话结束后回收 processor。
func TestSessionManager_OneShot哨兵(t *testing.T) {
	manager := NewSessionManager()
	sid := "cron_test_1"
	_, _ = manager.SubmitAndWait(context.Background(), sid, func(context.Context) (any, error) {
		return "done", nil
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !manager.HasActiveProcessor(sid) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("一次性会话结束后 processor 应被回收（哨兵未生效）")
}

// TestSessionManager_CancelSessionTask 取消正在跑的任务。
func TestSessionManager_CancelSessionTask(t *testing.T) {
	manager := NewSessionManager()
	started := make(chan struct{})
	var cancelled atomic.Bool
	manager.SubmitTask(context.Background(), "s1", func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		cancelled.Store(true)
		return nil, ctx.Err()
	})
	<-started

	if err := manager.CancelSessionTask(context.Background(), "s1", "", nil); err != nil {
		t.Fatalf("取消不应报错: %v", err)
	}
	if !cancelled.Load() {
		t.Error("任务应被取消")
	}
}

// TestSessionManager_CancelSessionTask_无任务 静默返回。
func TestSessionManager_CancelSessionTask_无任务(t *testing.T) {
	manager := NewSessionManager()
	if err := manager.CancelSessionTask(context.Background(), "nope", "", nil); err != nil {
		t.Errorf("无任务时应静默返回，实际 %v", err)
	}
}

// TestSessionManager_CancelAllSessionTasks 排除集合生效。
func TestSessionManager_CancelAllSessionTasks(t *testing.T) {
	manager := NewSessionManager()
	block := make(chan struct{})
	for _, sid := range []string{"s1", "s2"} {
		manager.SubmitTask(context.Background(), sid, func(ctx context.Context) (any, error) {
			select {
			case <-block:
			case <-ctx.Done():
			}
			return nil, nil
		})
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if manager.HasActiveTasks() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	exclude := map[string]struct{}{"s2": {}}
	manager.CancelAllSessionTasks(context.Background(), "", &exclude)
	close(block)

	sid2 := "s2"
	if !manager.HasSessionRuntime(&sid2) {
		t.Error("被排除的会话应仍存在运行时")
	}
}

// TestSessionManager_HasActiveTasks dreaming busy_checker 用。
func TestSessionManager_HasActiveTasks(t *testing.T) {
	manager := NewSessionManager()
	if manager.HasActiveTasks() {
		t.Error("初始不应有活跃任务")
	}
	release := make(chan struct{})
	manager.SubmitTask(context.Background(), "s1", func(ctx context.Context) (any, error) {
		<-release
		return nil, nil
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if manager.HasActiveTasks() {
			close(release)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	t.Error("应有活跃任务")
}

// TestSessionManager_CloseAllSessions 进程级清理。
func TestSessionManager_CloseAllSessions(t *testing.T) {
	manager := NewSessionManager()
	_, _ = manager.SubmitAndWait(context.Background(), "s1", func(context.Context) (any, error) {
		return nil, nil
	})
	_, _ = manager.SubmitAndWait(context.Background(), "s2", func(context.Context) (any, error) {
		return nil, nil
	})
	manager.CloseAllSessions(context.Background())
	sid1, sid2 := "s1", "s2"
	if manager.HasSessionRuntime(&sid1) || manager.HasSessionRuntime(&sid2) {
		t.Error("全量关闭后不应再有会话运行时")
	}
}

// TestSessionManager_GetCurrentTask 任务在跑时为 true。
func TestSessionManager_GetCurrentTask(t *testing.T) {
	manager := NewSessionManager()
	if manager.GetCurrentTask("s1") {
		t.Error("无任务时应为 false")
	}
	release := make(chan struct{})
	manager.SubmitTask(context.Background(), "s1", func(ctx context.Context) (any, error) {
		<-release
		return nil, nil
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if manager.GetCurrentTask("s1") {
			close(release)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	t.Error("任务在跑时应为 true")
}

// TestSessionManager_EnsureSessionProcessor_幂等 重复调用不重建。
func TestSessionManager_EnsureSessionProcessor_幂等(t *testing.T) {
	manager := NewSessionManager()
	ctx := context.Background()
	manager.EnsureSessionProcessor(ctx, "s1")
	if !manager.HasActiveProcessor("s1") {
		t.Fatal("应启动 processor")
	}
	manager.EnsureSessionProcessor(ctx, "s1")
	if !manager.HasActiveProcessor("s1") {
		t.Error("重复调用后 processor 应仍在")
	}
	manager.CloseAllSessions(ctx)
}

// TestSessionManager_跨会话并发 两个会话互不阻塞。
func TestSessionManager_跨会话并发(t *testing.T) {
	manager := NewSessionManager()
	ctx := context.Background()
	started := make(chan struct{}, 2)
	release := make(chan struct{})

	var wg sync.WaitGroup
	for _, sid := range []string{"s1", "s2"} {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			_, _ = manager.SubmitAndWait(ctx, sessionID, func(opCtx context.Context) (any, error) {
				started <- struct{}{}
				select {
				case <-release:
				case <-opCtx.Done():
				}
				return nil, nil
			})
		}(sid)
	}

	// 两个会话的任务都应能同时启动（跨会话并发）
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("跨会话应并发启动")
		}
	}
	close(release)
	wg.Wait()
}

// TestSessionManager_任务panic panic 转错误不外泄。
func TestSessionManager_任务panic(t *testing.T) {
	manager := NewSessionManager()
	_, err := manager.SubmitAndWait(context.Background(), "s1", func(context.Context) (any, error) {
		panic("boom")
	})
	if err == nil {
		t.Fatal("panic 应转为错误")
	}
}

// TestSessionManager_CloseSession_队列有残留任务 关会话时取消排队任务的结果。
func TestSessionManager_CloseSession_队列有残留任务(t *testing.T) {
	manager := NewSessionManager()
	ctx := context.Background()

	// 先占住处理器：一个长跑任务
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = manager.SubmitAndWait(ctx, "s1", func(opCtx context.Context) (any, error) {
			close(started)
			select {
			case <-release:
			case <-opCtx.Done():
			}
			return nil, nil
		})
	}()
	<-started

	// 再排一个任务，让它留在队列里
	queuedResult := make(chan error, 1)
	go func() {
		_, err := manager.SubmitAndWait(ctx, "s1", func(context.Context) (any, error) {
			return "queued", nil
		})
		queuedResult <- err
	}()
	time.Sleep(20 * time.Millisecond)

	cleaned, err := manager.CloseSession(ctx, "s1", nil)
	if err != nil {
		t.Fatalf("关闭不应报错: %v", err)
	}
	if !cleaned {
		t.Error("应报告清理了运行时")
	}
	close(release)

	select {
	case err := <-queuedResult:
		if err == nil {
			t.Error("排队任务应被取消并返回错误")
		}
	case <-time.After(2 * time.Second):
		t.Error("排队任务应被唤醒")
	}
}

// TestSessionManager_CancelSessionTask_带超时 超时路径只告警。
func TestSessionManager_CancelSessionTask_带超时(t *testing.T) {
	manager := NewSessionManager()
	ctx := context.Background()
	started := make(chan struct{})
	// 这个任务忽略取消（模拟不响应取消的活）
	manager.SubmitTask(ctx, "s1", func(opCtx context.Context) (any, error) {
		close(started)
		time.Sleep(100 * time.Millisecond)
		return nil, nil
	})
	<-started

	timeout := 0.01
	if err := manager.CancelSessionTask(ctx, "s1", "p:", &timeout); err != nil {
		t.Errorf("超时不应报错，只告警: %v", err)
	}
}

// TestSessionManager_CloseSession_带超时 处理器不退出时按超时返回。
func TestSessionManager_CloseSession_带超时(t *testing.T) {
	manager := NewSessionManager()
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	manager.SubmitTask(ctx, "s1", func(context.Context) (any, error) {
		close(started)
		<-release
		return nil, nil
	})
	<-started

	timeout := 0.01
	_, err := manager.CloseSession(ctx, "s1", &timeout)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	close(release)
}

// TestTaskQueue_直接操作 覆盖 heap 接口与 drainCancelled。
func TestTaskQueue_直接操作(t *testing.T) {
	queue := &taskQueue{}
	low := &sessionTask{priority: 10, result: make(chan taskResult, 1)}
	high := &sessionTask{priority: 1, result: make(chan taskResult, 1)}
	queue.Push(low)
	queue.Push(high)
	if queue.Len() != 2 {
		t.Fatalf("队列长度应为 2，实际 %d", queue.Len())
	}
	// Less 定义为"优先级数值更小者先出"
	if !queue.Less(1, 0) {
		t.Error("1 号（priority=1）应小于 0 号（priority=10）")
	}
	queue.Swap(0, 1)
	if queue.items[0].priority != 1 {
		t.Error("Swap 后 0 号应为 priority=1")
	}
	queue.Swap(0, 1)

	// Pop 取末尾元素（container/heap 约定由 heap 包调用，直接 Pop 即取最后一个）
	last := queue.Pop().(*sessionTask)
	if last.priority != 1 {
		t.Errorf("Pop 取末尾元素，应为 1，实际 %d", last.priority)
	}
	queue.drainCancelled()

	select {
	case result := <-low.result:
		if result.err == nil {
			t.Error("drainCancelled 应写入取消错误")
		}
	default:
		t.Error("drainCancelled 应尝试写入结果")
	}
}

// TestSDrainCancelled_nil任务 队列含 nil 元素时不 panic。
func TestDrainCancelled_nil任务(t *testing.T) {
	queue := &taskQueue{items: []*sessionTask{nil, {priority: 1}}}
	queue.drainCancelled()
	if queue.Len() != 0 {
		t.Error("清空后队列应为空")
	}
}

// TestSessionManager_closingTasks_登记与清理 closing_tasks 三个助手的行为。
//
// 这三个方法此前被合并成 cleanupProcessorGeneration 而未实现，
// 导致 closingTasks 字段只读不写——"同一 session_id 可在旧 processor
// 退出前重连"的保证实际不存在。
func TestSessionManager_closingTasks_登记与清理(t *testing.T) {
	manager := NewSessionManager()
	task := &sessionTask{result: make(chan taskResult, 1)}

	manager.trackClosingTask("s1", task)
	manager.mu.Lock()
	registered := len(manager.closingTasks["s1"])
	manager.mu.Unlock()
	if registered != 1 {
		t.Fatalf("应登记 1 个待关闭任务，实际 %d", registered)
	}

	// 重复登记幂等
	manager.trackClosingTask("s1", task)
	manager.mu.Lock()
	registered = len(manager.closingTasks["s1"])
	manager.mu.Unlock()
	if registered != 1 {
		t.Errorf("重复登记应幂等，实际 %d", registered)
	}

	manager.finishClosingTask("s1", task)
	manager.mu.Lock()
	_, stillExists := manager.closingTasks["s1"]
	manager.mu.Unlock()
	if stillExists {
		t.Error("清空后应删除会话条目，避免按会话泄漏")
	}
}

// TestSessionManager_finishClosingTask_吞取消错误 取消错误静默，其他错误记日志。
func TestSessionManager_finishClosingTask_吞取消错误(t *testing.T) {
	manager := NewSessionManager()
	task := &sessionTask{result: make(chan taskResult, 1)}
	manager.trackClosingTask("s1", task)
	task.result <- taskResult{err: context.Canceled}
	// 不应 panic
	manager.finishClosingTask("s1", task)

	// 未登记的任务调用是空操作
	manager.finishClosingTask("s2", &sessionTask{result: make(chan taskResult, 1)})
}

// TestSessionManager_cancelResultFuture 取消结果等待唤醒调用方。
func TestSessionManager_cancelResultFuture(t *testing.T) {
	manager := NewSessionManager()
	task := &sessionTask{result: make(chan taskResult, 1)}
	manager.cancelResultFuture(task)
	select {
	case result := <-task.result:
		if !errors.Is(result.err, context.Canceled) {
			t.Errorf("应投递取消错误，实际 %v", result.err)
		}
	default:
		t.Error("应唤醒等待方")
	}
	// nil 安全
	manager.cancelResultFuture(nil)
	manager.cancelResultFuture(&sessionTask{})
}

// TestSessionManager_CloseSession_待到任务结束 close 时等 task 完成。
func TestSessionManager_CloseSession_待到任务结束(t *testing.T) {
	manager := NewSessionManager()
	started := make(chan struct{})
	manager.SubmitTask(context.Background(), "s1", func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	<-started

	timeout := 2.0
	cleaned, err := manager.CloseSession(context.Background(), "s1", &timeout)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !cleaned {
		t.Error("应报告清理了运行时")
	}
	// 关闭后不应残留 closing_tasks 条目
	manager.mu.Lock()
	_, leftover := manager.closingTasks["s1"]
	manager.mu.Unlock()
	if leftover {
		t.Error("关闭后不应残留 closing_tasks 条目")
	}
}
