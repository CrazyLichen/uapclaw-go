package modules

import (
	"context"
	"fmt"
	"maps"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskScheduler 任务调度器，负责后台调度循环、并发执行、暂停/取消。
//
// Python: openjiuwen/core/controller/modules/task_scheduler.py (TaskScheduler)
type TaskScheduler struct {
	// config 配置
	config *config.ControllerConfig
	// taskManager 任务管理器
	taskManager *TaskManager
	// contextEngine 上下文引擎
	contextEngine any // iface.ContextEngine，用 any 避免循环依赖
	// abilityMgr 能力管理器
	abilityMgr agentinterfaces.AbilityManagerInterface
	// eventQueue 事件队列
	eventQueue *EventQueue
	// taskExecutorRegistry 任务执行器注册表
	taskExecutorRegistry *TaskExecutorRegistry
	// sessions 会话字典 sessionID → SessionFacade
	sessions map[string]sessioninterfaces.SessionFacade
	// card Agent 卡片
	card any // *agentschema.AgentCard，用 any 避免 import 冲突

	// runningTasks 运行中任务 taskID → entry
	runningTasks map[string]*runningTaskEntry
	// mu 保护 runningTasks + sessions
	mu sync.Mutex
	// running 是否运行中
	running atomic.Bool
	// notifyCh 事件驱动唤醒信号
	notifyCh chan struct{}
	// cancelFunc 调度 goroutine 取消函数
	cancelFunc context.CancelFunc
	// wg 等待所有任务 goroutine 退出（对齐 Python: asyncio.gather）
	wg sync.WaitGroup
}

// runningTaskEntry 运行中任务条目。
type runningTaskEntry struct {
	// executor 任务执行器
	executor TaskExecutor
	// cancel 取消函数
	cancel context.CancelFunc
	// done goroutine 退出信号（对齐 Python: await exec_task 等待任务真正退出）
	done chan struct{}
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────
const (
	// payloadTypeTaskCompletion 任务完成载荷类型（对齐 EventType 枚举值）
	payloadTypeTaskCompletion = "task_completion"
	// payloadTypeTaskInteraction 任务交互载荷类型（对齐 EventType 枚举值）
	payloadTypeTaskInteraction = "task_interaction"
	// payloadTypeTaskFailed 任务失败载荷类型（对齐 EventType 枚举值）
	payloadTypeTaskFailed = "task_failed"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskScheduler 创建新的 TaskScheduler 实例。
//
// Python: TaskScheduler.__init__
func NewTaskScheduler(
	cfg *config.ControllerConfig,
	taskManager *TaskManager,
	contextEngine any,
	abilityMgr agentinterfaces.AbilityManagerInterface,
	eventQueue *EventQueue,
	card any,
) *TaskScheduler {
	return &TaskScheduler{
		config:               cfg,
		taskManager:          taskManager,
		contextEngine:        contextEngine,
		abilityMgr:           abilityMgr,
		eventQueue:           eventQueue,
		taskExecutorRegistry: NewTaskExecutorRegistry(),
		sessions:             make(map[string]sessioninterfaces.SessionFacade),
		card:                 card,
		runningTasks:         make(map[string]*runningTaskEntry),
		notifyCh:             make(chan struct{}, 1),
	}
}

// Start 启动调度器，设置 running=true 并启动调度 goroutine。
//
// Python: TaskScheduler.start
func (s *TaskScheduler) Start(ctx context.Context) error {
	if s.running.Load() {
		logger.Warn(logComponent).
			Str("event_type", "task_scheduler_already_running").
			Msg("TaskScheduler already running, ignoring duplicate start")
		return nil
	}
	s.running.Store(true)

	schedCtx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel

	// 注册 TaskManager 的 SUBMITTED 回调
	s.taskManager.SetOnTaskSubmitted(s.NotifyTaskSubmitted)

	go s.schedule(schedCtx)

	logger.Info(logComponent).
		Str("event_type", "task_scheduler_start").
		Msg("TaskScheduler started")
	return nil
}

// Stop 停止调度器，取消运行中任务并等待退出。
//
// Python: TaskScheduler.stop
func (s *TaskScheduler) Stop(ctx context.Context) error {
	if !s.running.Load() {
		logger.Warn(logComponent).
			Str("event_type", "task_scheduler_not_running").
			Msg("TaskScheduler not running, ignoring stop")
		return nil
	}
	s.running.Store(false)

	// 取消调度 goroutine
	if s.cancelFunc != nil {
		s.cancelFunc()
	}

	// 等待所有运行中任务完成
	s.waitAllTasksComplete(ctx)

	logger.Info(logComponent).
		Str("event_type", "task_scheduler_stop").
		Msg("TaskScheduler stopped")
	return nil
}

// NotifyTaskSubmitted 非阻塞写入 notifyCh 唤醒调度循环。
//
// Python: TaskScheduler.notify_task_submitted
func (s *TaskScheduler) NotifyTaskSubmitted() {
	select {
	case s.notifyCh <- struct{}{}:
	default:
		// channel 已满，说明已有待处理唤醒信号
	}
}

// AddSession 添加 session（线程安全）。
func (s *TaskScheduler) AddSession(sessionID string, sess sessioninterfaces.SessionFacade) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = sess
}

// RemoveSession 移除 session（线程安全）。
func (s *TaskScheduler) RemoveSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

// GetSession 获取指定 session（线程安全）。
func (s *TaskScheduler) GetSession(sessionID string) (sessioninterfaces.SessionFacade, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	return sess, ok
}

// Sessions 返回 sessions 的副本（对齐 Python: asyncio 单线程无需锁，Go 返回副本避免数据竞争）。
func (s *TaskScheduler) Sessions() map[string]sessioninterfaces.SessionFacade {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.sessions)
}

// TaskExecutorRegistry 返回任务执行器注册表。
func (s *TaskScheduler) TaskExecutorRegistry() *TaskExecutorRegistry {
	return s.taskExecutorRegistry
}

// SetConfig 更新配置。
func (s *TaskScheduler) SetConfig(cfg *config.ControllerConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
}

// PauseTask 暂停指定任务。
//
// Python: TaskScheduler.pause_task
// 返回 (是否成功暂停, 系统级错误)。
func (s *TaskScheduler) PauseTask(ctx context.Context, taskID string) (bool, error) {
	s.mu.Lock()
	entry, exists := s.runningTasks[taskID]
	s.mu.Unlock()

	if !exists {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Msg("failed to pause task: task not running")
		return false, nil
	}

	// 获取任务信息
	tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to pause task: unable to get task")
		return false, err
	}
	if len(tasks) == 0 {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Msg("failed to pause task: task not found")
		return false, nil
	}
	task := tasks[0]

	// 获取会话
	sess := s.sessions[task.SessionID]
	if sess == nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Str("session_id", task.SessionID).
			Msg("failed to pause task: session not found")
		return false, nil
	}

	// 检查是否可暂停
	canPause, reason, err := entry.executor.CanPause(ctx, taskID, sess)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to check pause")
		return false, err
	}
	if !canPause {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Str("reason", reason).
			Msg("task cannot be paused")
		return false, nil
	}

	// 执行暂停
	if paused, err := entry.executor.Pause(ctx, taskID, sess); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to execute task pause")
		return false, err
	} else if !paused {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Msg("executor.Pause returned false")
		return false, nil
	}

	// 取消运行中的 goroutine
	if entry.cancel != nil {
		entry.cancel()
	}

	// 等待 goroutine 退出（对齐 Python: await exec_task）
	s.waitForTaskDone(entry.done, taskID)

	// 从运行中列表移除
	s.mu.Lock()
	delete(s.runningTasks, taskID)
	s.mu.Unlock()

	// 更新任务状态为 PAUSED
	if err := s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskPaused); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to update task status to PAUSED")
		return false, err
	}

	logger.Info(logComponent).
		Str("event_type", "task_paused").
		Str("task_id", taskID).
		Msg("task paused")
	return true, nil
}

// CancelTask 取消指定任务。
//
// Python: TaskScheduler.cancel_task
// 返回 (是否成功取消, 系统级错误)。
func (s *TaskScheduler) CancelTask(ctx context.Context, taskID string) (bool, error) {
	// 获取任务信息
	tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to cancel task: unable to get task")
		return false, err
	}
	if len(tasks) == 0 {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Msg("failed to cancel task: task not found")
		return false, nil
	}
	task := tasks[0]

	// SUBMITTED 状态直接标记为 CANCELED
	if task.Status == schema.TaskSubmitted {
		if err := s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskCanceled); err != nil {
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("task_id", taskID).
				Err(err).
				Msg("failed to update submitted task status to CANCELED")
			return false, err
		}
		logger.Info(logComponent).
			Str("event_type", "task_canceled").
			Str("task_id", taskID).
			Msg("submitted task canceled")
		return true, nil
	}

	// 已终态幂等返回 true（对齐 Python）
	if task.Status == schema.TaskCanceled || task.Status == schema.TaskCompleted || task.Status == schema.TaskFailed {
		logger.Info(logComponent).
			Str("event_type", "task_canceled_idempotent").
			Str("task_id", taskID).
			Str("status", string(task.Status)).
			Msg("task already in terminal state, cancel returned idempotently")
		return true, nil
	}

	// WORKING 状态需要取消运行中的 goroutine
	s.mu.Lock()
	entry, exists := s.runningTasks[taskID]
	s.mu.Unlock()

	if !exists {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Msg("failed to cancel task: task not running")
		return false, nil
	}

	// 获取会话
	sess := s.sessions[task.SessionID]
	if sess == nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Str("session_id", task.SessionID).
			Msg("failed to cancel task: session not found")
		return false, nil
	}

	// 检查是否可取消
	canCancel, reason, err := entry.executor.CanCancel(ctx, taskID, sess)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to check cancel")
		return false, err
	}
	if !canCancel {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Str("reason", reason).
			Msg("task cannot be canceled")
		return false, nil
	}

	// 执行取消
	if canceled, err := entry.executor.Cancel(ctx, taskID, sess); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to execute task cancel")
		return false, err
	} else if !canceled {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Msg("executor.Cancel returned false")
		return false, nil
	}

	// 取消运行中的 goroutine
	if entry.cancel != nil {
		entry.cancel()
	}

	// 等待 goroutine 退出（对齐 Python: await exec_task）
	s.waitForTaskDone(entry.done, taskID)

	// 从运行中列表移除
	s.mu.Lock()
	delete(s.runningTasks, taskID)
	s.mu.Unlock()

	// 更新任务状态为 CANCELED
	if err := s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskCanceled); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to update task status to CANCELED")
		return false, err
	}

	logger.Info(logComponent).
		Str("event_type", "task_canceled").
		Str("task_id", taskID).
		Msg("task canceled")
	return true, nil
}

// EnsureSessionCompletionSignal 检查并发送 all_tasks_processed 信号。
//
// Python: TaskScheduler.ensure_session_completion_signal
// Controller.stream() 在 publish_event 返回后调用此方法，
// 使得没有新任务的轮次也能发送完成信号。
func (s *TaskScheduler) EnsureSessionCompletionSignal(ctx context.Context, sessionID string) {
	s.mu.Lock()
	suppress := s.config.SuppressCompletionSignal
	s.mu.Unlock()

	if suppress {
		logger.Debug(logComponent).
			Str("session_id", sessionID).
			Msg("completion signal suppressed")
		return
	}

	if !s.areAllTasksCompleted(ctx, sessionID) {
		return
	}

	// 从 sessions map 查找 session（对齐 Python: self._sessions.get）
	s.mu.Lock()
	sess, exists := s.sessions[sessionID]
	s.mu.Unlock()

	if !exists {
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Msg("session not found, cannot send completion signal")
		return
	}

	// 发送 all_tasks_processed chunk 到 session 流（补充 Payload.Data 对齐 Python）
	chunk := &stream.OutputSchema{
		Type: "controller_output",
		Payload: &schema.ControllerOutputPayload{
			Type: schema.AllTasksProcessed,
			Data: []schema.DataFrame{
				&schema.TextDataFrame{Text: "所有任务已成功处理"},
			},
		},
		IsLastSchema: true,
	}
	if err := sess.WriteStream(ctx, chunk); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("session_id", sessionID).
			Err(err).
			Msg("failed to write all_tasks_processed signal")
		return
	}

	logger.Info(logComponent).
		Str("event_type", "all_tasks_processed").
		Str("session_id", sessionID).
		Msg("all tasks processed, completion signal sent")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// schedule 调度循环，扫描 SUBMITTED 任务并启动执行。
//
// Python: TaskScheduler._schedule
func (s *TaskScheduler) schedule(ctx context.Context) {
	logger.Info(logComponent).
		Str("event_type", "task_scheduler_started").
		Msg("TaskScheduler schedule loop started")

	for s.running.Load() {
		// 1. 扫描 SUBMITTED 任务
		tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{Status: schema.TaskSubmitted})
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Err(err).
				Msg("failed to scan SUBMITTED tasks")
		}

		// 2. 并发启动（受 maxConcurrentTasks 限制）
		for _, task := range tasks {
			sess := s.sessions[task.SessionID]
			if sess == nil {
				logger.Warn(logComponent).
					Str("task_id", task.TaskID).
					Str("session_id", task.SessionID).
					Msg("session for task not found, skipping")
				continue
			}

			s.mu.Lock()
			// 检查并发限制
			maxConcurrent := s.config.MaxConcurrentTasks
			if maxConcurrent > 0 && len(s.runningTasks) >= maxConcurrent {
				s.mu.Unlock()
				break
			}
			// 检查是否已在运行
			if _, exists := s.runningTasks[task.TaskID]; exists {
				s.mu.Unlock()
				continue
			}

			// 启动执行 goroutine
			taskCtx, taskCancel := context.WithCancel(ctx)
			entry := &runningTaskEntry{
				cancel: taskCancel,
				done:   make(chan struct{}),
			}
			s.runningTasks[task.TaskID] = entry
			s.mu.Unlock()

			s.wg.Add(1)
			go s.executeTaskWrapper(taskCtx, task.TaskID, sess, entry)

			logger.Info(logComponent).
				Str("event_type", "task_started").
				Str("task_id", task.TaskID).
				Str("task_type", task.TaskType).
				Msg("task started")
		}

		// 3. 等待唤醒或超时
		scheduleInterval := time.Duration(s.config.ScheduleInterval * float64(time.Second))
		if scheduleInterval <= 0 {
			scheduleInterval = time.Second
		}
		select {
		case <-s.notifyCh:
			// 收到新任务提交通知
		case <-time.After(scheduleInterval):
			// 调度间隔超时，重新扫描
		case <-ctx.Done():
			goto done
		}
	}

done:
	s.waitAllTasksComplete(ctx)
	logger.Info(logComponent).
		Str("event_type", "task_scheduler_stopped").
		Msg("TaskScheduler schedule loop stopped")
}

// executeTask 执行单个任务，读取 chunk channel 并处理状态转换。
//
// Python: TaskScheduler._execute_task
func (s *TaskScheduler) executeTask(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) {
	// 1. 获取任务
	tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})
	if err != nil || len(tasks) == 0 {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to execute task: unable to get task")
		// Python: 抛出 AGENT_CONTROLLER_TASK_EXECUTION_ERROR
		s.handleTaskExecutionFailure(ctx, taskID, sess, fmt.Sprintf("task %s not found", taskID))
		return
	}
	task := tasks[0]

	// 2. 创建 TaskExecutor（从 registry 获取）
	deps := &TaskExecutorDependencies{
		Config:      s.config,
		AbilityMgr:  s.abilityMgr,
		TaskManager: s.taskManager,
		EventQueue:  s.eventQueue,
	}
	// contextEngine 存储为 any，需要类型断言为 iface.ContextEngine
	if ce, ok := s.contextEngine.(iface.ContextEngine); ok {
		deps.ContextEngine = ce
	}
	executor, err := s.taskExecutorRegistry.GetTaskExecutor(task.TaskType, deps)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Str("task_type", task.TaskType).
			Err(err).
			Msg("failed to get task executor")
		// Python: 失败时构建 failed chunk 并发布事件
		s.handleTaskExecutionFailure(ctx, taskID, sess, fmt.Sprintf("failed to get task executor: %v", err))
		return
	}

	// 保存 executor 到 runningTaskEntry
	s.mu.Lock()
	if entry, ok := s.runningTasks[taskID]; ok {
		entry.executor = executor
	}
	s.mu.Unlock()

	// 3. 更新状态 WORKING
	if err := s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskWorking); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to update task status to WORKING")
		return
	}

	// 4. 执行任务，获取 chunk channel
	chunkCh, err := executor.ExecuteAbility(ctx, taskID, sess)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to start task execution")
		// Python: 失败时构建 failed chunk 并发布事件
		s.handleTaskExecutionFailure(ctx, taskID, sess, fmt.Sprintf("failed to start task execution: %v", err))
		return
	}

	// 5. range channel 读取 chunk
	for chunk := range chunkCh {
		if chunk == nil {
			continue
		}

		// 6. WriteStream 到 session
		if writeErr := sess.WriteStream(ctx, chunk); writeErr != nil {
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("task_id", taskID).
				Err(writeErr).
				Msg("failed to write stream data")
		}

		// 7. 根据 payload.type 判断状态（Payload 为 any，类型断言为 *ControllerOutputPayload）
		payload, ok := chunk.Payload.(*schema.ControllerOutputPayload)
		if !ok || payload == nil {
			continue
		}

		switch payload.Type {
		case payloadTypeTaskCompletion:
			// 任务完成转为已完成状态
			if err := s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskCompleted); err != nil {
				logger.Error(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("task_id", taskID).
					Err(err).
					Msg("failed to update task status to COMPLETED")
			}
			logger.Info(logComponent).
				Str("event_type", "task_completed").
				Str("task_id", taskID).
				Msg("task completed")
			// 发布任务事件
			s.publishTaskEvent(ctx, taskID, payload, sess)
			return

		case payloadTypeTaskInteraction:
			// 任务交互转为需要输入状态
			if err := s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskInputRequired); err != nil {
				logger.Error(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("task_id", taskID).
					Err(err).
					Msg("failed to update task status to INPUT_REQUIRED")
			}
			logger.Info(logComponent).
				Str("event_type", "task_input_required").
				Str("task_id", taskID).
				Msg("task requires user input")
			// 发布任务事件
			s.publishTaskEvent(ctx, taskID, payload, sess)
			return

		case payloadTypeTaskFailed:
			// 任务失败转为失败状态
			errMsg := ""
			if payload.Metadata != nil {
				if msg, ok := payload.Metadata["error_message"].(string); ok {
					errMsg = msg
				}
			}
			if err := s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskFailed, WithErrorMessage(errMsg)); err != nil {
				logger.Error(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("task_id", taskID).
					Err(err).
					Msg("failed to update task status to FAILED")
			}
			logger.Info(logComponent).
				Str("event_type", "task_failed").
				Str("task_id", taskID).
				Msg("task failed")
			// 发布任务事件
			s.publishTaskEvent(ctx, taskID, payload, sess)
			return

		case schema.TaskProcessing:
			// 处理中 → 继续
			logger.Debug(logComponent).
				Str("task_id", taskID).
				Msg("task processing chunk")
			continue

		default:
			// 未知类型，继续处理下一个 chunk
			logger.Debug(logComponent).
				Str("task_id", taskID).
				Str("payload_type", payload.Type).
				Msg("unknown payload type")
		}
	}
}

// executeTaskWrapper 任务执行包装器，处理超时、取消和异常。
//
// Python: TaskScheduler._execute_task_wrapper
func (s *TaskScheduler) executeTaskWrapper(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade, entry *runningTaskEntry) {
	defer s.wg.Done()

	defer func() {
		// 通知 goroutine 已退出
		if entry != nil && entry.done != nil {
			close(entry.done)
		}
	}()

	defer func() {
		// finally: 从 runningTasks 删除 + ensureSessionCompletionSignal
		s.mu.Lock()
		delete(s.runningTasks, taskID)
		s.mu.Unlock()

		// 获取任务信息判断 session
		tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})
		if err == nil && len(tasks) > 0 {
			s.EnsureSessionCompletionSignal(ctx, tasks[0].SessionID)
		}
	}()

	// 处理超时
	var taskCtx context.Context
	var taskCancel context.CancelFunc

	s.mu.Lock()
	taskTimeout := s.config.TaskTimeout
	s.mu.Unlock()

	if taskTimeout != nil && *taskTimeout > 0 {
		taskCtx, taskCancel = context.WithTimeout(ctx, time.Duration(*taskTimeout*float64(time.Second)))
	} else {
		taskCtx, taskCancel = context.WithCancel(ctx)
	}
	defer taskCancel()

	// 捕获异常
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			logger.Error(logComponent).
				Any("recover", r).
				Str("stack", string(buf[:n])).
				Str("event_type", "LLM_CALL_ERROR").
				Str("task_id", taskID).
				Str("method", "executeTaskWrapper").
				Str("model_provider", "task_scheduler").
				Msg("executeTaskWrapper panic recovered")
			// Python: _handle_task_execution_failure — 更新状态+构建failed chunk+发布事件
			s.handleTaskExecutionFailure(ctx, taskID, sess, fmt.Sprintf("task execution panic: %v", r))
		}
	}()

	done := make(chan struct{})
	go func() {
		s.executeTask(taskCtx, taskID, sess)
		close(done)
	}()

	select {
	case <-done:
		// 正常完成
	case <-taskCtx.Done():
		// 超时或取消
		if taskCtx.Err() == context.DeadlineExceeded {
			s.mu.Lock()
			timeout := s.config.TaskTimeout
			s.mu.Unlock()
			timeoutStr := "unknown"
			if timeout != nil {
				timeoutStr = fmt.Sprintf("%v", *timeout)
			}
			errorMsg := fmt.Sprintf("task timeout, timeout %s seconds", timeoutStr)
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("task_id", taskID).
				Msg(errorMsg)
			// Python: _handle_task_execution_failure — 更新状态+构建failed chunk+发布事件
			s.handleTaskExecutionFailure(ctx, taskID, sess, errorMsg)
		} else if taskCtx.Err() == context.Canceled {
			logger.Info(logComponent).
				Str("event_type", "task_canceled").
				Str("task_id", taskID).
				Msg("task canceled")
		}
	}
}

// areAllTasksCompleted 检查会话内所有任务是否处于终态。
//
// Python: TaskScheduler._are_all_tasks_completed
func (s *TaskScheduler) areAllTasksCompleted(ctx context.Context, sessionID string) bool {
	tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{SessionID: sessionID})
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("session_id", sessionID).
			Err(err).
			Msg("failed to check task completion status")
		return false
	}

	for _, task := range tasks {
		if task.Status == schema.TaskSubmitted || task.Status == schema.TaskWorking {
			return false
		}
	}
	return true
}

// handleTaskExecutionFailure 处理任务执行失败：更新状态为 FAILED + 构建 failed chunk + 发布事件。
//
// Python: TaskScheduler._handle_task_execution_failure
func (s *TaskScheduler) handleTaskExecutionFailure(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade, errorMsg string) {
	// 1. 更新任务状态为 FAILED
	_ = s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskFailed, WithErrorMessage(errorMsg))

	// 2. 构建 failed payload（对齐 Python: ControllerOutputPayload(type=TASK_FAILED, data=[TextDataFrame(text=error_msg)])）
	failedPayload := &schema.ControllerOutputPayload{
		Type: payloadTypeTaskFailed,
		Data: []schema.DataFrame{
			&schema.TextDataFrame{Text: errorMsg},
		},
	}

	// 3. 发布 TaskFailedEvent
	s.publishTaskEvent(ctx, taskID, failedPayload, sess)
}

// publishTaskEvent 根据 payload.type 构建事件并通过 EventQueue 发布。
//
// Python: TaskScheduler._publish_task_event
func (s *TaskScheduler) publishTaskEvent(ctx context.Context, taskID string, payload *schema.ControllerOutputPayload, sess sessioninterfaces.SessionFacade) {
	if s.eventQueue == nil || payload == nil {
		return
	}

	// 获取任务信息
	tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})
	if err != nil || len(tasks) == 0 {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to publish task event: unable to get task")
		return
	}
	task := tasks[0]

	// 获取 agentID
	agentID := ""
	if card, ok := s.card.(interface{ GetID() string }); ok {
		agentID = card.GetID()
	}

	var event schema.Event

	switch payload.Type {
	case payloadTypeTaskCompletion:
		event = &schema.TaskCompletionEvent{
			BaseEvent:  *schema.NewBaseEvent(schema.EventTaskCompletion),
			TaskResult: payload.Data,
			Task:       task,
		}
	case payloadTypeTaskInteraction:
		event = &schema.TaskInteractionEvent{
			BaseEvent:   *schema.NewBaseEvent(schema.EventTaskInteraction),
			Interaction: payload.Data,
			Task:        task,
		}
	case payloadTypeTaskFailed:
		// Python: error_msg = payload_data[0].text if payload_data else "Unknown error"
		errMsg := "Unknown error"
		if len(payload.Data) > 0 {
			if textDF, ok := payload.Data[0].(*schema.TextDataFrame); ok {
				errMsg = textDF.Text
			}
		}
		// Python: task.error_message = error_msg（同步更新 Task 对象）
		task.ErrorMessage = errMsg
		_ = s.taskManager.UpdateTask(ctx, task)
		event = &schema.TaskFailedEvent{
			BaseEvent:    *schema.NewBaseEvent(schema.EventTaskFailed),
			ErrorMessage: errMsg,
			Task:         task,
		}
	default:
		return
	}

	// Python: 合并 chunk.payload.metadata 与 task.metadata 到 event.metadata
	// Python: event 构造时 metadata=payload_metadata（含 task_id/task_type，
	// handler 依赖 task_type 区分 SessionSpawn 完成分支），
	// 随后 event.metadata.update(task.metadata)（_handler_round_id 等传播）。
	// Go 此前丢弃 payload.Metadata 导致 handler 收不到 task_type，SessionSpawn
	// 完成后 toolkit 行不更新——此处对齐 Python 双源合并语义。
	eventMeta := event.GetMetadata()
	if eventMeta == nil {
		eventMeta = make(map[string]any)
	}
	for k, v := range payload.Metadata {
		eventMeta[k] = v
	}
	if task.Metadata != nil {
		for k, v := range task.Metadata {
			eventMeta[k] = v
		}
	}
	event.SetMetadata(eventMeta)

	// Python: 同步发布事件，等待 EventHandler 处理完成
	if err := s.eventQueue.PublishEvent(ctx, agentID, sess, event); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("failed to publish task event synchronously")
	} else {
		logger.Info(logComponent).
			Str("event_type", "task_event_published").
			Str("task_id", taskID).
			Str("payload_type", payload.Type).
			Msg("task event published")
	}
}

// waitAllTasksComplete 取消所有运行中任务并等待退出。
//
// Python: TaskScheduler._wait_all_tasks_complete（使用 asyncio.gather 等待）
func (s *TaskScheduler) waitAllTasksComplete(_ context.Context) {
	// 取消所有运行中任务
	s.mu.Lock()
	for id, entry := range s.runningTasks {
		if entry.cancel != nil {
			entry.cancel()
		}
		logger.Info(logComponent).
			Str("event_type", "task_cancel_on_stop").
			Str("task_id", id).
			Msg("canceling running task on stop")
	}
	s.mu.Unlock()

	// 等待所有任务 goroutine 退出（对齐 Python: asyncio.gather）
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// 所有任务 goroutine 已退出
	case <-time.After(5 * time.Second):
		logger.Warn(logComponent).Msg("timeout waiting for task completion (5s)")
	}
}

// waitForTaskDone 等待任务 goroutine 退出。
// Python: await exec_task 等待 asyncio.Task 完成。
func (s *TaskScheduler) waitForTaskDone(done chan struct{}, taskID string) {
	if done == nil {
		return
	}
	select {
	case <-done:
		// goroutine 已正常退出
	case <-time.After(5 * time.Second):
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Msg("timeout waiting for task goroutine to exit (5s)")
	}
}
