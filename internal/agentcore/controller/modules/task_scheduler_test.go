package modules

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// schedulerFakeSessionFacade 测试用 SessionFacade，记录 WriteStream 调用
type schedulerFakeSessionFacade struct {
	// sessionID 会话标识
	sessionID string
	// writtenChunks 写入的 chunk 记录
	writtenChunks []any
	// mu 保护 writtenChunks
	mu sync.Mutex
}

// configurableFakeTaskExecutor 可配置的模拟任务执行器
type configurableFakeTaskExecutor struct {
	// executeFunc 执行函数
	executeFunc func(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error)
	// canPauseResult 是否可暂停
	canPauseResult bool
	// canPauseReason 不可暂停原因
	canPauseReason string
	// pauseErr 暂停错误
	pauseErr error
	// canCancelResult 是否可取消
	canCancelResult bool
	// canCancelReason 不可取消原因
	canCancelReason string
	// cancelErr 取消错误
	cancelErr error
}

// schedulerFakeEventHandler 测试用 EventHandler 实现
type schedulerFakeEventHandler struct {
	EventHandlerBase
	handledInput           atomic.Int32
	handledTaskInteraction atomic.Int32
	handledTaskCompletion  atomic.Int32
	handledTaskFailed      atomic.Int32
	handledFollowUp        atomic.Int32
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSessionID 实现 SessionFacade 接口
func (f *schedulerFakeSessionFacade) GetSessionID() string { return f.sessionID }

// UpdateState 实现 SessionFacade 接口
func (f *schedulerFakeSessionFacade) UpdateState(_ map[string]any) {}

// GetState 实现 SessionFacade 接口
func (f *schedulerFakeSessionFacade) GetState(_ state.StateKey) (any, error) { return nil, nil }

// DumpState 实现 SessionFacade 接口
func (f *schedulerFakeSessionFacade) DumpState() map[string]any { return nil }

// WriteStream 实现 SessionFacade 接口，记录写入的 chunk
func (f *schedulerFakeSessionFacade) WriteStream(_ context.Context, data any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writtenChunks = append(f.writtenChunks, data)
	return nil
}

// WriteCustomStream 实现 SessionFacade 接口
func (f *schedulerFakeSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }

// GetEnv 实现 SessionFacade 接口
func (f *schedulerFakeSessionFacade) GetEnv(_ string, _ ...any) any { return nil }

// Interact 实现 SessionFacade 接口
func (f *schedulerFakeSessionFacade) Interact(_ context.Context, _ any) error { return nil }

// getWrittenChunks 获取已写入的 chunk 列表
func (f *schedulerFakeSessionFacade) getWrittenChunks() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]any, len(f.writtenChunks))
	copy(result, f.writtenChunks)
	return result
}

// ExecuteAbility 实现 TaskExecutor 接口
func (e *configurableFakeTaskExecutor) ExecuteAbility(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error) {
	if e.executeFunc != nil {
		return e.executeFunc(ctx, taskID, sess)
	}
	// 默认：发送一个 completion chunk 然后关闭
	ch := make(chan *schema.ControllerOutputChunk, 1)
	ch <- &schema.ControllerOutputChunk{
		Payload: &schema.ControllerOutputPayload{
			Type: payloadTypeTaskCompletion,
			Data: []schema.DataFrame{&schema.TextDataFrame{Text: "done"}},
		},
		LastChunk: true,
	}
	close(ch)
	return ch, nil
}

// CanPause 实现 TaskExecutor 接口
func (e *configurableFakeTaskExecutor) CanPause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return e.canPauseResult, e.canPauseReason, nil
}

// Pause 实现 TaskExecutor 接口
func (e *configurableFakeTaskExecutor) Pause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, error) {
	return true, e.pauseErr
}

// CanCancel 实现 TaskExecutor 接口
func (e *configurableFakeTaskExecutor) CanCancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return e.canCancelResult, e.canCancelReason, nil
}

// Cancel 实现 TaskExecutor 接口
func (e *configurableFakeTaskExecutor) Cancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, error) {
	return true, e.cancelErr
}

// HandleInput 实现 EventHandler 接口
func (h *schedulerFakeEventHandler) HandleInput(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
	h.handledInput.Add(1)
	return map[string]any{"status": "handled_input"}, nil
}

// HandleTaskInteraction 实现 EventHandler 接口
func (h *schedulerFakeEventHandler) HandleTaskInteraction(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
	h.handledTaskInteraction.Add(1)
	return map[string]any{"status": "handled_task_interaction"}, nil
}

// HandleTaskCompletion 实现 EventHandler 接口
func (h *schedulerFakeEventHandler) HandleTaskCompletion(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
	h.handledTaskCompletion.Add(1)
	return map[string]any{"status": "handled_task_completion"}, nil
}

// HandleTaskFailed 实现 EventHandler 接口
func (h *schedulerFakeEventHandler) HandleTaskFailed(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
	h.handledTaskFailed.Add(1)
	return map[string]any{"status": "handled_task_failed"}, nil
}

// HandleFollowUp 实现 EventHandler 接口
func (h *schedulerFakeEventHandler) HandleFollowUp(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
	h.handledFollowUp.Add(1)
	return map[string]any{"status": "handled_follow_up"}, nil
}

// GetBase 实现 EventHandler 接口
func (h *schedulerFakeEventHandler) GetBase() *EventHandlerBase {
	return &h.EventHandlerBase
}

// PrepareRound 实现 EventHandler 接口
func (h *schedulerFakeEventHandler) PrepareRound() int { return 0 }

// WaitCompletion 实现 EventHandler 接口
func (h *schedulerFakeEventHandler) WaitCompletion(_ context.Context, _ time.Duration) map[string]any {
	return map[string]any{"status": "completed"}
}

// OnAbort 实现 EventHandler 接口
func (h *schedulerFakeEventHandler) OnAbort() {}

// newTestScheduler 创建测试用 TaskScheduler
func newTestScheduler(cfg *config.ControllerConfig) (*TaskScheduler, *TaskManager, *EventQueue, *TaskExecutorRegistry) {
	tm := NewTaskManager(cfg)
	eq := NewEventQueue(cfg)

	sched := NewTaskScheduler(cfg, tm, nil, nil, eq, nil)
	reg := sched.TaskExecutorRegistry()

	// 注册默认执行器
	reg.AddTaskExecutor("test-type", func(deps *TaskExecutorDependencies) TaskExecutor {
		return &configurableFakeTaskExecutor{}
	})

	return sched, tm, eq, reg
}

// TestTaskScheduler_启停 测试 Start/Stop 生命周期
func TestTaskScheduler_启停(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.ScheduleInterval = 0.1

	sched, _, _, _ := newTestScheduler(cfg)

	// 启动
	err := sched.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, sched.running.Load())

	// 重复启动不报错
	err = sched.Start(context.Background())
	require.NoError(t, err)

	// 停止
	err = sched.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, sched.running.Load())

	// 重复停止不报错
	err = sched.Stop(context.Background())
	require.NoError(t, err)
}

// TestTaskScheduler_NotifyTaskSubmitted 测试 NotifyTaskSubmitted 唤醒调度循环
func TestTaskScheduler_NotifyTaskSubmitted(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	sched := NewTaskScheduler(cfg, NewTaskManager(cfg), nil, nil, nil, nil)

	// 非阻塞写入
	sched.NotifyTaskSubmitted()
	// channel 中应有一个信号
	assert.Len(t, sched.notifyCh, 1)

	// 再次写入（channel 已满，不阻塞）
	sched.NotifyTaskSubmitted()
	assert.Len(t, sched.notifyCh, 1) // 最多 1 个，因为 buffer=1
}

// TestTaskScheduler_调度执行 测试提交任务后调度器自动执行并完成
func TestTaskScheduler_调度执行(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.ScheduleInterval = 0.05
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	sched, tm, eq, reg := newTestScheduler(cfg)

	// 注册完成执行器
	reg.AddTaskExecutor("test-type", func(deps *TaskExecutorDependencies) TaskExecutor {
		return &configurableFakeTaskExecutor{
			canPauseResult: true,
			canCancelResult: true,
		}
	})

	// 启动 EventQueue
	eq.SetEventHandler(&schedulerFakeEventHandler{})
	eq.Start()
	defer eq.Stop(context.Background())

	// 添加会话
	sess := &schedulerFakeSessionFacade{sessionID: "sess-1"}
	sched.Sessions()["sess-1"] = sess

	// 启动调度器
	err := sched.Start(context.Background())
	require.NoError(t, err)
	defer sched.Stop(context.Background())

	// 提交任务
	task := schema.NewTask("sess-1", "test-type")
	err = tm.AddTask(context.Background(), task)
	require.NoError(t, err)

	// 等待任务完成
	assert.Eventually(t, func() bool {
		tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: task.TaskID})
		if len(tasks) == 0 {
			return false
		}
		return tasks[0].Status == schema.TaskCompleted
	}, 3*time.Second, 50*time.Millisecond, "任务应在超时前完成")
}

// TestTaskScheduler_并发执行 测试多个 SUBMITTED 任务并发执行
func TestTaskScheduler_并发执行(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.MaxConcurrentTasks = 5
	cfg.ScheduleInterval = 0.05
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	sched, tm, eq, reg := newTestScheduler(cfg)

	var completedCount atomic.Int32

	// 注册执行器，延迟后完成
	reg.AddTaskExecutor("test-type", func(deps *TaskExecutorDependencies) TaskExecutor {
		return &configurableFakeTaskExecutor{
			canPauseResult: true,
			canCancelResult: true,
			executeFunc: func(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error) {
				ch := make(chan *schema.ControllerOutputChunk, 1)
				go func() {
					time.Sleep(100 * time.Millisecond)
					ch <- &schema.ControllerOutputChunk{
						Payload: &schema.ControllerOutputPayload{
							Type: payloadTypeTaskCompletion,
							Data: []schema.DataFrame{&schema.TextDataFrame{Text: "done"}},
						},
						LastChunk: true,
					}
					close(ch)
					completedCount.Add(1)
				}()
				return ch, nil
			},
		}
	})

	eq.SetEventHandler(&schedulerFakeEventHandler{})
	eq.Start()
	defer eq.Stop(context.Background())

	sess := &schedulerFakeSessionFacade{sessionID: "sess-1"}
	sched.Sessions()["sess-1"] = sess

	err := sched.Start(context.Background())
	require.NoError(t, err)
	defer sched.Stop(context.Background())

	// 提交 3 个任务
	taskIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		task := schema.NewTask("sess-1", "test-type")
		err = tm.AddTask(context.Background(), task)
		require.NoError(t, err)
		taskIDs[i] = task.TaskID
	}

	// 等待所有任务完成
	assert.Eventually(t, func() bool {
		return completedCount.Load() == 3
	}, 5*time.Second, 50*time.Millisecond, "3 个任务应全部完成")

	// 验证所有任务状态
	for _, id := range taskIDs {
		tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: id})
		if len(tasks) > 0 {
			assert.Equal(t, schema.TaskCompleted, tasks[0].Status)
		}
	}
}

// TestTaskScheduler_最大并发限制 测试超过 maxConcurrentTasks 时等待
func TestTaskScheduler_最大并发限制(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.MaxConcurrentTasks = 2
	cfg.ScheduleInterval = 0.05
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	sched, tm, eq, reg := newTestScheduler(cfg)

	var startedCount atomic.Int32

	// 注册执行器，延迟后完成，记录启动数
	reg.AddTaskExecutor("test-type", func(deps *TaskExecutorDependencies) TaskExecutor {
		return &configurableFakeTaskExecutor{
			canPauseResult: true,
			canCancelResult: true,
			executeFunc: func(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error) {
				startedCount.Add(1)
				ch := make(chan *schema.ControllerOutputChunk, 1)
				go func() {
					time.Sleep(200 * time.Millisecond)
					ch <- &schema.ControllerOutputChunk{
						Payload: &schema.ControllerOutputPayload{
							Type: payloadTypeTaskCompletion,
							Data: []schema.DataFrame{&schema.TextDataFrame{Text: "done"}},
						},
						LastChunk: true,
					}
					close(ch)
				}()
				return ch, nil
			},
		}
	})

	eq.SetEventHandler(&schedulerFakeEventHandler{})
	eq.Start()
	defer eq.Stop(context.Background())

	sess := &schedulerFakeSessionFacade{sessionID: "sess-1"}
	sched.Sessions()["sess-1"] = sess

	err := sched.Start(context.Background())
	require.NoError(t, err)
	defer sched.Stop(context.Background())

	// 提交 4 个任务
	for i := 0; i < 4; i++ {
		task := schema.NewTask("sess-1", "test-type")
		err = tm.AddTask(context.Background(), task)
		require.NoError(t, err)
	}

	// 等待所有任务完成
	assert.Eventually(t, func() bool {
		tasks, _ := tm.GetTask(context.Background(), &TaskFilter{SessionID: "sess-1"})
		for _, task := range tasks {
			if task.Status == schema.TaskSubmitted || task.Status == schema.TaskWorking {
				return false
			}
		}
		return len(tasks) == 4
	}, 5*time.Second, 50*time.Millisecond, "所有任务应完成")
}

// TestTaskScheduler_暂停任务 测试暂停运行中的任务
func TestTaskScheduler_暂停任务(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.ScheduleInterval = 0.05
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	sched, tm, eq, reg := newTestScheduler(cfg)

	pausedCh := make(chan struct{}, 1)

	// 注册可暂停执行器
	reg.AddTaskExecutor("test-type", func(deps *TaskExecutorDependencies) TaskExecutor {
		return &configurableFakeTaskExecutor{
			canPauseResult: true,
			canCancelResult: true,
			executeFunc: func(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error) {
				ch := make(chan *schema.ControllerOutputChunk)
				go func() {
					<-pausedCh // 阻塞直到被暂停
					close(ch)
				}()
				return ch, nil
			},
		}
	})

	eq.SetEventHandler(&schedulerFakeEventHandler{})
	eq.Start()
	defer eq.Stop(context.Background())

	sess := &schedulerFakeSessionFacade{sessionID: "sess-1"}
	sched.Sessions()["sess-1"] = sess

	err := sched.Start(context.Background())
	require.NoError(t, err)
	defer sched.Stop(context.Background())

	// 提交任务
	task := schema.NewTask("sess-1", "test-type")
	err = tm.AddTask(context.Background(), task)
	require.NoError(t, err)

	// 等待任务进入 WORKING 状态
	assert.Eventually(t, func() bool {
		tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: task.TaskID})
		return len(tasks) > 0 && tasks[0].Status == schema.TaskWorking
	}, 2*time.Second, 50*time.Millisecond)

	// 暂停任务
	ok, err := sched.PauseTask(context.Background(), task.TaskID)
	assert.True(t, ok)
	require.NoError(t, err)
	pausedCh <- struct{}{} // 解除执行器阻塞

	// 验证任务状态
	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: task.TaskID})
	require.Len(t, tasks, 1)
	assert.Equal(t, schema.TaskPaused, tasks[0].Status)
}

// TestTaskScheduler_取消任务 测试取消运行中的任务
func TestTaskScheduler_取消任务(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.ScheduleInterval = 0.05
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	sched, tm, eq, reg := newTestScheduler(cfg)

	// 测试取消 SUBMITTED 状态的任务
	task1 := schema.NewTask("sess-1", "test-type")
	err := tm.AddTask(context.Background(), task1)
	require.NoError(t, err)

	// 直接取消 SUBMITTED 任务
	ok, err := sched.CancelTask(context.Background(), task1.TaskID)
	assert.True(t, ok)
	require.NoError(t, err)

	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: task1.TaskID})
	require.Len(t, tasks, 1)
	assert.Equal(t, schema.TaskCanceled, tasks[0].Status)

	// 测试取消 WORKING 状态的任务
	canceledCh := make(chan struct{}, 1)

	reg.AddTaskExecutor("test-type", func(deps *TaskExecutorDependencies) TaskExecutor {
		return &configurableFakeTaskExecutor{
			canPauseResult: true,
			canCancelResult: true,
			executeFunc: func(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error) {
				ch := make(chan *schema.ControllerOutputChunk)
				go func() {
					<-canceledCh // 阻塞直到被取消
					close(ch)
				}()
				return ch, nil
			},
		}
	})

	eq.SetEventHandler(&schedulerFakeEventHandler{})
	eq.Start()
	defer eq.Stop(context.Background())

	sess := &schedulerFakeSessionFacade{sessionID: "sess-1"}
	sched.Sessions()["sess-1"] = sess

	err = sched.Start(context.Background())
	require.NoError(t, err)
	defer sched.Stop(context.Background())

	task2 := schema.NewTask("sess-1", "test-type")
	err = tm.AddTask(context.Background(), task2)
	require.NoError(t, err)

	// 等待任务进入 WORKING 状态
	assert.Eventually(t, func() bool {
		tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: task2.TaskID})
		return len(tasks) > 0 && tasks[0].Status == schema.TaskWorking
	}, 2*time.Second, 50*time.Millisecond)

	// 取消任务
	ok, err = sched.CancelTask(context.Background(), task2.TaskID)
	assert.True(t, ok)
	require.NoError(t, err)
	canceledCh <- struct{}{}

	// 验证任务状态
	tasks, _ = tm.GetTask(context.Background(), &TaskFilter{TaskID: task2.TaskID})
	require.Len(t, tasks, 1)
	assert.Equal(t, schema.TaskCanceled, tasks[0].Status)
}

// TestTaskScheduler_任务超时 测试 task_timeout 后标记 FAILED
func TestTaskScheduler_任务超时(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.ScheduleInterval = 0.05
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5
	taskTimeout := 0.2 // 200ms 超时
	cfg.TaskTimeout = &taskTimeout

	sched, tm, eq, reg := newTestScheduler(cfg)

	// 注册慢执行器（永远不完成）
	reg.AddTaskExecutor("test-type", func(deps *TaskExecutorDependencies) TaskExecutor {
		return &configurableFakeTaskExecutor{
			canPauseResult: true,
			canCancelResult: true,
			executeFunc: func(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error) {
				ch := make(chan *schema.ControllerOutputChunk)
				go func() {
					// 长时间阻塞，等待 context 取消
					<-ctx.Done()
					close(ch)
				}()
				return ch, nil
			},
		}
	})

	eq.SetEventHandler(&schedulerFakeEventHandler{})
	eq.Start()
	defer eq.Stop(context.Background())

	sess := &schedulerFakeSessionFacade{sessionID: "sess-1"}
	sched.Sessions()["sess-1"] = sess

	err := sched.Start(context.Background())
	require.NoError(t, err)
	defer sched.Stop(context.Background())

	task := schema.NewTask("sess-1", "test-type")
	err = tm.AddTask(context.Background(), task)
	require.NoError(t, err)

	// 等待任务超时后变为 FAILED
	assert.Eventually(t, func() bool {
		tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: task.TaskID})
		return len(tasks) > 0 && tasks[0].Status == schema.TaskFailed
	}, 3*time.Second, 50*time.Millisecond, "任务应在超时后标记为 FAILED")
}

// TestTaskScheduler_完成信号 测试所有任务完成后写入 all_tasks_processed
func TestTaskScheduler_完成信号(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.ScheduleInterval = 0.05
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5
	cfg.SuppressCompletionSignal = false

	sched, tm, eq, reg := newTestScheduler(cfg)

	reg.AddTaskExecutor("test-type", func(deps *TaskExecutorDependencies) TaskExecutor {
		return &configurableFakeTaskExecutor{
			canPauseResult: true,
			canCancelResult: true,
		}
	})

	eq.SetEventHandler(&schedulerFakeEventHandler{})
	eq.Start()
	defer eq.Stop(context.Background())

	sess := &schedulerFakeSessionFacade{sessionID: "sess-1"}
	sched.Sessions()["sess-1"] = sess

	err := sched.Start(context.Background())
	require.NoError(t, err)
	defer sched.Stop(context.Background())

	task := schema.NewTask("sess-1", "test-type")
	err = tm.AddTask(context.Background(), task)
	require.NoError(t, err)

	// 等待任务完成
	assert.Eventually(t, func() bool {
		tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: task.TaskID})
		return len(tasks) > 0 && tasks[0].Status == schema.TaskCompleted
	}, 3*time.Second, 50*time.Millisecond)

	// 等待完成信号写入
	assert.Eventually(t, func() bool {
		chunks := sess.getWrittenChunks()
		for _, c := range chunks {
			if chunk, ok := c.(*schema.ControllerOutputChunk); ok {
				if chunk.Payload != nil && chunk.Payload.Type == schema.AllTasksProcessed {
					return true
				}
			}
		}
		return false
	}, 3*time.Second, 50*time.Millisecond, "应写入 all_tasks_processed 信号")
}

// TestTaskScheduler_抑制完成信号 测试 SuppressCompletionSignal=true 时不发送信号
func TestTaskScheduler_抑制完成信号(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.ScheduleInterval = 0.05
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5
	cfg.SuppressCompletionSignal = true

	sched, tm, eq, reg := newTestScheduler(cfg)

	reg.AddTaskExecutor("test-type", func(deps *TaskExecutorDependencies) TaskExecutor {
		return &configurableFakeTaskExecutor{
			canPauseResult: true,
			canCancelResult: true,
		}
	})

	eq.SetEventHandler(&schedulerFakeEventHandler{})
	eq.Start()
	defer eq.Stop(context.Background())

	sess := &schedulerFakeSessionFacade{sessionID: "sess-1"}
	sched.Sessions()["sess-1"] = sess

	err := sched.Start(context.Background())
	require.NoError(t, err)
	defer sched.Stop(context.Background())

	task := schema.NewTask("sess-1", "test-type")
	err = tm.AddTask(context.Background(), task)
	require.NoError(t, err)

	// 等待任务完成
	assert.Eventually(t, func() bool {
		tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: task.TaskID})
		return len(tasks) > 0 && tasks[0].Status == schema.TaskCompleted
	}, 3*time.Second, 50*time.Millisecond)

	// 短暂等待后检查不应有 all_tasks_processed
	time.Sleep(200 * time.Millisecond)
	chunks := sess.getWrittenChunks()
	for _, c := range chunks {
		if chunk, ok := c.(*schema.ControllerOutputChunk); ok {
			assert.NotEqual(t, schema.AllTasksProcessed, chunk.Payload.Type)
		}
	}
}

// TestTaskScheduler_Sessions 测试 Sessions 返回会话字典
func TestTaskScheduler_Sessions(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	sched := NewTaskScheduler(cfg, NewTaskManager(cfg), nil, nil, nil, nil)

	sess := &schedulerFakeSessionFacade{sessionID: "sess-1"}
	sched.Sessions()["sess-1"] = sess

	sessions := sched.Sessions()
	assert.Contains(t, sessions, "sess-1")
	assert.Equal(t, sess, sessions["sess-1"])
}

// TestTaskScheduler_SetConfig 测试 SetConfig 更新配置
func TestTaskScheduler_SetConfig(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	sched := NewTaskScheduler(cfg, NewTaskManager(cfg), nil, nil, nil, nil)

	newCfg := config.DefaultControllerConfig()
	newCfg.MaxConcurrentTasks = 10
	sched.SetConfig(newCfg)

	sched.mu.Lock()
	actual := sched.config
	sched.mu.Unlock()
	assert.Equal(t, 10, actual.MaxConcurrentTasks)
}

// TestTaskScheduler_TaskExecutorRegistry 测试 TaskExecutorRegistry 返回注册表
func TestTaskScheduler_TaskExecutorRegistry(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	sched := NewTaskScheduler(cfg, NewTaskManager(cfg), nil, nil, nil, nil)
	reg := sched.TaskExecutorRegistry()

	assert.NotNil(t, reg)
}

// TestTaskScheduler_执行TaskInteraction 测试 TASK_INTERACTION 事件触发 INPUT_REQUIRED 状态
func TestTaskScheduler_执行TaskInteraction(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.ScheduleInterval = 0.05
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	sched, tm, eq, reg := newTestScheduler(cfg)

	// 注册发送 TASK_INTERACTION 的执行器
	reg.AddTaskExecutor("interaction-type", func(deps *TaskExecutorDependencies) TaskExecutor {
		return &configurableFakeTaskExecutor{
			canPauseResult: true,
			canCancelResult: true,
			executeFunc: func(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error) {
				ch := make(chan *schema.ControllerOutputChunk, 1)
				ch <- &schema.ControllerOutputChunk{
					Payload: &schema.ControllerOutputPayload{
						Type: payloadTypeTaskInteraction,
						Data: []schema.DataFrame{&schema.TextDataFrame{Text: "请确认"}},
					},
					LastChunk: true,
				}
				close(ch)
				return ch, nil
			},
		}
	})

	eq.SetEventHandler(&schedulerFakeEventHandler{})
	eq.Start()
	defer eq.Stop(context.Background())

	sess := &schedulerFakeSessionFacade{sessionID: "sess-1"}
	sched.Sessions()["sess-1"] = sess

	err := sched.Start(context.Background())
	require.NoError(t, err)
	defer sched.Stop(context.Background())

	task := schema.NewTask("sess-1", "interaction-type")
	err = tm.AddTask(context.Background(), task)
	require.NoError(t, err)

	// 等待任务进入 INPUT_REQUIRED 状态
	assert.Eventually(t, func() bool {
		tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: task.TaskID})
		return len(tasks) > 0 && tasks[0].Status == schema.TaskInputRequired
	}, 3*time.Second, 50*time.Millisecond, "任务应在 TASK_INTERACTION 后进入 INPUT_REQUIRED 状态")
}

// TestTaskScheduler_执行TaskFailed 测试 TASK_FAILED 事件触发 FAILED 状态
func TestTaskScheduler_执行TaskFailed(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.ScheduleInterval = 0.05
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	sched, tm, eq, reg := newTestScheduler(cfg)

	// 注册发送 TASK_FAILED 的执行器
	reg.AddTaskExecutor("failed-type", func(deps *TaskExecutorDependencies) TaskExecutor {
		return &configurableFakeTaskExecutor{
			canPauseResult: true,
			canCancelResult: true,
			executeFunc: func(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error) {
				ch := make(chan *schema.ControllerOutputChunk, 1)
				ch <- &schema.ControllerOutputChunk{
					Payload: &schema.ControllerOutputPayload{
						Type: payloadTypeTaskFailed,
						Metadata: map[string]any{
							"error_message": "执行失败",
						},
					},
					LastChunk: true,
				}
				close(ch)
				return ch, nil
			},
		}
	})

	eq.SetEventHandler(&schedulerFakeEventHandler{})
	eq.Start()
	defer eq.Stop(context.Background())

	sess := &schedulerFakeSessionFacade{sessionID: "sess-1"}
	sched.Sessions()["sess-1"] = sess

	err := sched.Start(context.Background())
	require.NoError(t, err)
	defer sched.Stop(context.Background())

	task := schema.NewTask("sess-1", "failed-type")
	err = tm.AddTask(context.Background(), task)
	require.NoError(t, err)

	// 等待任务进入 FAILED 状态
	assert.Eventually(t, func() bool {
		tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: task.TaskID})
		return len(tasks) > 0 && tasks[0].Status == schema.TaskFailed
	}, 3*time.Second, 50*time.Millisecond, "任务应在 TASK_FAILED 后进入 FAILED 状态")

	// 验证 ErrorMessage
	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: task.TaskID})
	require.Len(t, tasks, 1)
	assert.Equal(t, "执行失败", tasks[0].ErrorMessage)
}

// TestTaskScheduler_PauseTask_不在运行中 测试暂停不在运行中的任务返回 (false, nil)。
func TestTaskScheduler_PauseTask_不在运行中(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	tm := NewTaskManager(cfg)
	sched := NewTaskScheduler(cfg, tm, nil, nil, nil, nil)

	ok, err := sched.PauseTask(context.Background(), "nonexistent")
	assert.False(t, ok)
	assert.NoError(t, err)
}

// TestTaskScheduler_CancelTask_不在运行中 测试取消不在运行中的任务返回 (false, nil)。
func TestTaskScheduler_CancelTask_不在运行中(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	tm := NewTaskManager(cfg)
	sched := NewTaskScheduler(cfg, tm, nil, nil, nil, nil)

	// 先添加一个 WORKING 任务到 TaskManager 但不在 runningTasks
	task := schema.NewTask("sess1", "test_type")
	task.Status = schema.TaskWorking
	_ = tm.AddTask(context.Background(), task)

	ok, err := sched.CancelTask(context.Background(), task.TaskID)
	assert.False(t, ok)
	assert.NoError(t, err)
}

// TestTaskScheduler_CancelTask_终态幂等 测试取消已终态任务幂等返回 (true, nil)。
func TestTaskScheduler_CancelTask_终态幂等(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	tm := NewTaskManager(cfg)
	sched := NewTaskScheduler(cfg, tm, nil, nil, nil, nil)

	// 添加一个 COMPLETED 任务
	task := schema.NewTask("sess1", "test_type")
	task.Status = schema.TaskCompleted
	_ = tm.AddTask(context.Background(), task)

	ok, err := sched.CancelTask(context.Background(), task.TaskID)
	assert.True(t, ok)
	assert.NoError(t, err)
}

// TestTaskScheduler_EnsureSessionCompletionSignal_会话不存在 测试完成信号在会话不存在时优雅返回。
func TestTaskScheduler_EnsureSessionCompletionSignal_会话不存在(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	tm := NewTaskManager(cfg)
	sched := NewTaskScheduler(cfg, tm, nil, nil, nil, nil)

	// 没有注册任何 session，EnsureSessionCompletionSignal 应优雅返回
	sched.EnsureSessionCompletionSignal(context.Background(), "nonexistent-session")
}
