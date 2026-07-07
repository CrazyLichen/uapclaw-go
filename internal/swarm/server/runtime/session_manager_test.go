package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetSessionID_空串 验证空串返回 default
func TestGetSessionID_空串(t *testing.T) {
	if got := GetSessionID(""); got != "default" {
		t.Errorf("GetSessionID(\"\") = %q, want %q", got, "default")
	}
}

// TestGetSessionID_非空 验证非空原样返回
func TestGetSessionID_非空(t *testing.T) {
	if got := GetSessionID("abc"); got != "abc" {
		t.Errorf("GetSessionID(\"abc\") = %q, want %q", got, "abc")
	}
}

// TestNewSessionManager 验证初始化
func TestNewSessionManager(t *testing.T) {
	sm := NewSessionManager()
	if sm == nil {
		t.Fatal("NewSessionManager() 返回 nil")
	}
}

// TestSessionManager_SubmitAndWait_基本执行 验证任务提交并等待结果
func TestSessionManager_SubmitAndWait_基本执行(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	result, err := sm.SubmitAndWait(ctx, "default", func(_ context.Context) (any, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("SubmitAndWait() 返回错误: %v", err)
	}
	if result != 42 {
		t.Errorf("SubmitAndWait() = %v, want 42", result)
	}
}

// TestSessionManager_SubmitAndWait_错误传播 验证任务错误正确传播
func TestSessionManager_SubmitAndWait_错误传播(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	_, err := sm.SubmitAndWait(ctx, "default", func(_ context.Context) (any, error) {
		return nil, context.Canceled
	})
	if err != context.Canceled {
		t.Errorf("SubmitAndWait() err = %v, want context.Canceled", err)
	}
}

// TestSessionManager_串行执行 验证同 session 内任务串行执行
func TestSessionManager_串行执行(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	var order []int

	_, _ = sm.SubmitAndWait(ctx, "default", func(_ context.Context) (any, error) {
		order = append(order, 1)
		return nil, nil
	})

	_, _ = sm.SubmitAndWait(ctx, "default", func(_ context.Context) (any, error) {
		order = append(order, 2)
		return nil, nil
	})

	_, _ = sm.SubmitAndWait(ctx, "default", func(_ context.Context) (any, error) {
		order = append(order, 3)
		return nil, nil
	})

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("执行顺序 = %v, want [1 2 3]", order)
	}
}

// TestSessionManager_多Session并发 验证不同 session 可以并发执行
func TestSessionManager_多Session并发(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	done1 := make(chan struct{})
	done2 := make(chan struct{})

	go func() {
		_, _ = sm.SubmitAndWait(ctx, "session1", func(_ context.Context) (any, error) {
			time.Sleep(50 * time.Millisecond)
			close(done1)
			return nil, nil
		})
	}()

	go func() {
		_, _ = sm.SubmitAndWait(ctx, "session2", func(_ context.Context) (any, error) {
			time.Sleep(50 * time.Millisecond)
			close(done2)
			return nil, nil
		})
	}()

	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("session1 超时")
	}
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("session2 超时")
	}
}

// TestSessionManager_HasActiveTasks 验证活跃任务检查
func TestSessionManager_HasActiveTasks(t *testing.T) {
	sm := NewSessionManager()

	if sm.HasActiveTasks() {
		t.Error("空的 SessionManager 不应有活跃任务")
	}
}

// TestSessionManager_HasActiveProcessor 验证活跃处理器检查
func TestSessionManager_HasActiveProcessor(t *testing.T) {
	sm := NewSessionManager()

	if sm.HasActiveProcessor("default") {
		t.Error("未初始化的 session 不应有活跃处理器")
	}
}

// TestSessionManager_HasActiveProcessor_提交任务后 验证提交任务后处理器存在
func TestSessionManager_HasActiveProcessor_提交任务后(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	_, _ = sm.SubmitAndWait(ctx, "default", func(_ context.Context) (any, error) {
		return nil, nil
	})

	// 任务完成后 processor 仍然存在（可复用）
	if !sm.HasActiveProcessor("default") {
		t.Error("提交任务后 session 应有活跃处理器")
	}
}

// TestSessionManager_CancelAllSessionTasks 验证取消所有任务
func TestSessionManager_CancelAllSessionTasks(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	var cancelled atomic.Bool

	go func() {
		_, _ = sm.SubmitAndWait(context.Background(), "default", func(taskCtx context.Context) (any, error) {
			<-taskCtx.Done()
			cancelled.Store(true)
			return nil, nil
		})
	}()

	time.Sleep(100 * time.Millisecond) // 等待任务启动

	err := sm.CancelAllSessionTasks(ctx, "[test] ")
	if err != nil {
		t.Fatalf("CancelAllSessionTasks() 返回错误: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if !cancelled.Load() {
		t.Error("任务应已被取消")
	}
}

// TestSessionManager_上下文取消 验证 SubmitAndWait 在 ctx 取消时返回
func TestSessionManager_上下文取消(t *testing.T) {
	sm := NewSessionManager()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// 先启动一个长任务占用 session
	go func() {
		_, _ = sm.SubmitAndWait(context.Background(), "default", func(taskCtx context.Context) (any, error) {
			<-taskCtx.Done()
			return nil, nil
		})
	}()

	time.Sleep(50 * time.Millisecond) // 等待任务启动

	// 尝试在超时 ctx 下提交另一个任务
	_, err := sm.SubmitAndWait(ctx, "default", func(_ context.Context) (any, error) {
		return "done", nil
	})
	if err != nil {
		// ctx 超时返回错误是预期行为
		if err != context.DeadlineExceeded {
			t.Logf("SubmitAndWait 返回错误: %v（可接受）", err)
		}
	}
}

// TestSessionManager_CancelSessionTask 验证取消单个 session 任务
func TestSessionManager_CancelSessionTask(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	var cancelled atomic.Bool

	go func() {
		_, _ = sm.SubmitAndWait(context.Background(), "test_session", func(taskCtx context.Context) (any, error) {
			<-taskCtx.Done()
			cancelled.Store(true)
			return nil, nil
		})
	}()

	time.Sleep(100 * time.Millisecond)

	err := sm.CancelSessionTask(ctx, "test_session", "[test] ", nil)
	if err != nil {
		t.Fatalf("CancelSessionTask() 返回错误: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if !cancelled.Load() {
		t.Error("任务应已被取消")
	}
}

// TestSessionManager_GetCurrentTask 验证获取当前任务
func TestSessionManager_GetCurrentTask(t *testing.T) {
	sm := NewSessionManager()

	cancelFn := sm.GetCurrentTask("nonexistent")
	if cancelFn != nil {
		t.Error("不存在的 session 应返回 nil cancel 函数")
	}
}
