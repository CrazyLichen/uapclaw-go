package utils

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// ──────────────────────────── BackgroundTask 测试 ────────────────────────────

func TestBackgroundTask_StartAndWait(t *testing.T) {
	task := NewBackgroundTask("test", "group1", func(ctx context.Context) error {
		return nil
	})

	task.Start(context.Background())
	err := task.Wait()
	if err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
}

func TestBackgroundTask_Stop(t *testing.T) {
	started := make(chan struct{})
	task := NewBackgroundTask("long-running", "group1", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})

	task.Start(context.Background())
	<-started // 等待任务启动

	err := task.Stop(2 * time.Second)
	// context.Cancelled 会返回，但 Stop 应正常返回
	if err != nil {
		// context.Canceled 是预期行为
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Stop() = %v, want context.Canceled or nil", err)
		}
	}
}

func TestBackgroundTask_Done(t *testing.T) {
	task := NewBackgroundTask("test", "group1", func(ctx context.Context) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	task.Start(context.Background())

	// 未完成时 Done 不应关闭
	select {
	case <-task.Done():
		t.Fatal("Done() should not be closed yet")
	default:
		// 正确
	}

	// 等待完成
	<-task.Done()
}

func TestBackgroundTask_Error(t *testing.T) {
	expectedErr := errors.New("task failed")
	task := NewBackgroundTask("failing", "group1", func(ctx context.Context) error {
		return expectedErr
	})

	task.Start(context.Background())
	err := task.Wait()
	if err == nil || err.Error() != expectedErr.Error() {
		t.Fatalf("Wait() = %v, want %v", err, expectedErr)
	}
}

// ──────────────────────────── TaskStatus 测试 ────────────────────────────

func TestTaskStatus_IsTerminal(t *testing.T) {
	terminal := []TaskStatus{TaskCompleted, TaskFailed, TaskCancelled, TaskTimeout}
	nonTerminal := []TaskStatus{TaskPending, TaskRunning}

	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Fatalf("%s should be terminal", s)
		}
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Fatalf("%s should not be terminal", s)
		}
	}
}

func TestTaskStatus_String(t *testing.T) {
	tests := []struct {
		status TaskStatus
		want   string
	}{
		{TaskPending, "PENDING"},
		{TaskRunning, "RUNNING"},
		{TaskCompleted, "COMPLETED"},
		{TaskFailed, "FAILED"},
		{TaskCancelled, "CANCELLED"},
		{TaskTimeout, "TIMEOUT"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Fatalf("TaskStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// ──────────────────────────── TaskManager 测试 ────────────────────────────

func newTestManager() *TaskManager {
	return &TaskManager{registry: make(map[string]*Task)}
}

func TestTaskManager_CreateTask(t *testing.T) {
	mgr := newTestManager()

	var executed atomic.Bool
	task, err := mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		executed.Store(true)
		return "hello", nil
	}, WithTaskName("test-task"), WithTaskGroup("test-group"))

	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.Name != "test-task" {
		t.Fatalf("task.Name = %q, want %q", task.Name, "test-task")
	}
	if task.Group != "test-group" {
		t.Fatalf("task.Group = %q, want %q", task.Group, "test-group")
	}

	// 等待任务完成
	result, err := task.Wait()
	if err != nil {
		t.Fatalf("task.Wait() error = %v", err)
	}
	if result != "hello" {
		t.Fatalf("result = %v, want %q", result, "hello")
	}
	if !executed.Load() {
		t.Fatal("task function was not executed")
	}
}

func TestTaskManager_TaskFailure(t *testing.T) {
	mgr := newTestManager()

	task, err := mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		return nil, errors.New("something went wrong")
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	result, err := task.Wait()
	if err == nil {
		t.Fatal("task.Wait() should return error for failed task")
	}
	if result != nil {
		t.Fatalf("result = %v, want nil", result)
	}

	if !task.IsTerminal() {
		t.Fatal("task should be in terminal state")
	}
	task.mu.RLock()
	status := task.Status
	task.mu.RUnlock()
	if status != TaskFailed {
		t.Fatalf("task.Status = %s, want FAILED", status)
	}
}

func TestTaskManager_CancelTask(t *testing.T) {
	mgr := newTestManager()

	started := make(chan struct{})
	task, err := mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	<-started // 等待任务启动

	cancelled := mgr.Cancel(task.ID, "test_cancel")
	if !cancelled {
		t.Fatal("Cancel() should return true")
	}

	// 等待任务完成
	<-task.done

	task.mu.RLock()
	status := task.Status
	reason := task.CancelReason
	task.mu.RUnlock()

	if status != TaskCancelled {
		t.Fatalf("task.Status = %s, want CANCELLED", status)
	}
	if reason != "test_cancel" {
		t.Fatalf("task.CancelReason = %q, want %q", reason, "test_cancel")
	}
}

func TestTaskManager_CancelGroup(t *testing.T) {
	mgr := newTestManager()

	started1 := make(chan struct{})
	started2 := make(chan struct{})

	mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		close(started1)
		<-ctx.Done()
		return nil, ctx.Err()
	}, WithTaskGroup("workers"))

	mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		close(started2)
		<-ctx.Done()
		return nil, ctx.Err()
	}, WithTaskGroup("workers"))

	mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		time.Sleep(5 * time.Second)
		return "other", nil
	}, WithTaskGroup("other"))

	<-started1
	<-started2

	count := mgr.CancelGroup("workers", "group_cancel")
	if count != 2 {
		t.Fatalf("CancelGroup() = %d, want 2", count)
	}
}

func TestTaskManager_TaskTimeout(t *testing.T) {
	mgr := newTestManager()

	task, err := mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		time.Sleep(5 * time.Second)
		return "too slow", nil
	}, WithTaskTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	_, _ = task.Wait()

	task.mu.RLock()
	status := task.Status
	task.mu.RUnlock()

	if status != TaskTimeout {
		t.Fatalf("task.Status = %s, want TIMEOUT", status)
	}
}

func TestTaskManager_RemoveCompleted(t *testing.T) {
	mgr := newTestManager()

	// 创建并完成一个任务
	task, _ := mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		return "done", nil
	})
	task.Wait()

	// 创建一个运行中的任务
	mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		time.Sleep(5 * time.Second)
		return nil, nil
	})

	count := mgr.RemoveCompleted()
	if count != 1 {
		t.Fatalf("RemoveCompleted() = %d, want 1", count)
	}
}

func TestTaskManager_WaitGroup(t *testing.T) {
	mgr := newTestManager()

	mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		time.Sleep(50 * time.Millisecond)
		return 1, nil
	}, WithTaskGroup("calc"))

	mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		time.Sleep(100 * time.Millisecond)
		return 2, nil
	}, WithTaskGroup("calc"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results := mgr.WaitGroup(ctx, "calc")
	if len(results) != 2 {
		t.Fatalf("WaitGroup() returned %d results, want 2", len(results))
	}
}

func TestTaskManager_CascadeCancel(t *testing.T) {
	mgr := newTestManager()

	// 创建父任务
	parentStarted := make(chan struct{})
	parent, _ := mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		close(parentStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}, WithTaskName("parent"))

	<-parentStarted

	// 创建子任务
	childStarted := make(chan struct{})
	mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		close(childStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}, WithTaskName("child"), WithTaskParentID(parent.ID))

	<-childStarted

	// 级联取消
	count := mgr.CascadeCancel(parent.ID, "test_cascade")
	if count < 1 {
		t.Fatalf("CascadeCancel() = %d, want at least 1", count)
	}
}

func TestTaskManager_GetTaskTree(t *testing.T) {
	mgr := newTestManager()

	parent, _ := mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		time.Sleep(5 * time.Second)
		return nil, nil
	}, WithTaskName("parent"))

	mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		time.Sleep(5 * time.Second)
		return nil, nil
	}, WithTaskName("child"), WithTaskParentID(parent.ID))

	tree := mgr.GetTaskTree(parent.ID)
	if tree == "" {
		t.Fatal("GetTaskTree() returned empty string")
	}
}

func TestTask_DisplayName(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"with name", "abc123", "my-task"},
		{"without name", "abc123def456", "abc123de"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{ID: tt.id, Name: tt.name}
			if tt.name == "with name" {
				task.Name = "my-task"
			} else {
				task.Name = ""
			}
			got := task.DisplayName()
			if got != tt.want {
				t.Fatalf("DisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTaskManager_CancelNonExistentTask(t *testing.T) {
	mgr := newTestManager()
	result := mgr.Cancel("nonexistent", "test")
	if result {
		t.Fatal("Cancel() should return false for non-existent task")
	}
}

func TestTaskManager_CancelAlreadyCompletedTask(t *testing.T) {
	mgr := newTestManager()

	task, _ := mgr.CreateTask(context.Background(), func(ctx context.Context) (any, error) {
		return "done", nil
	})
	task.Wait()

	// 尝试取消已完成的任务
	result := mgr.Cancel(task.ID, "too_late")
	if result {
		t.Fatal("Cancel() should return false for completed task")
	}
}
