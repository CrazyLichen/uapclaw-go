package modules

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskScheduler 任务调度器，负责后台调度循环、并发执行、暂停/取消。
//
// 对齐 Python: openjiuwen/core/controller/modules/task_scheduler.py (TaskScheduler)
type TaskScheduler struct {
	// config 配置
	config *config.ControllerConfig
	// taskManager 任务管理器
	taskManager *TaskManager
	// contextEngine 上下文引擎
	contextEngine any // iface.ContextEngine，用 any 避免循环依赖
	// abilityMgr 能力管理器
	abilityMgr any
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
}

// runningTaskEntry 运行中任务条目。
type runningTaskEntry struct {
	// executor 任务执行器
	executor TaskExecutor
	// cancel 取消函数
	cancel context.CancelFunc
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// payloadTypeTaskCompletion 任务完成载荷类型
	payloadTypeTaskCompletion = "TASK_COMPLETION"
	// payloadTypeTaskInteraction 任务交互载荷类型
	payloadTypeTaskInteraction = "TASK_INTERACTION"
	// payloadTypeTaskFailed 任务失败载荷类型
	payloadTypeTaskFailed = "TASK_FAILED"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskScheduler 创建新的 TaskScheduler 实例。
//
// 对齐 Python: TaskScheduler.__init__
func NewTaskScheduler(
	cfg *config.ControllerConfig,
	taskManager *TaskManager,
	contextEngine any,
	abilityMgr any,
	eventQueue *EventQueue,
	registry *TaskExecutorRegistry,
	card any,
) *TaskScheduler {
	return &TaskScheduler{
		config:               cfg,
		taskManager:          taskManager,
		contextEngine:        contextEngine,
		abilityMgr:           abilityMgr,
		eventQueue:           eventQueue,
		taskExecutorRegistry: registry,
		sessions:             make(map[string]sessioninterfaces.SessionFacade),
		card:                 card,
		runningTasks:         make(map[string]*runningTaskEntry),
		notifyCh:             make(chan struct{}, 1),
	}
}

// Start 启动调度器，设置 running=true 并启动调度 goroutine。
//
// 对齐 Python: TaskScheduler.start
func (s *TaskScheduler) Start(ctx context.Context) error {
	if s.running.Load() {
		logger.Warn(logComponent).
			Str("event_type", "task_scheduler_already_running").
			Msg("TaskScheduler 已在运行，忽略重复启动")
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
		Msg("TaskScheduler 已启动")
	return nil
}

// Stop 停止调度器，取消运行中任务并等待退出。
//
// 对齐 Python: TaskScheduler.stop
func (s *TaskScheduler) Stop(ctx context.Context) error {
	if !s.running.Load() {
		logger.Warn(logComponent).
			Str("event_type", "task_scheduler_not_running").
			Msg("TaskScheduler 未运行，忽略停止")
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
		Msg("TaskScheduler 已停止")
	return nil
}

// NotifyTaskSubmitted 非阻塞写入 notifyCh 唤醒调度循环。
//
// 对齐 Python: TaskScheduler.notify_task_submitted
func (s *TaskScheduler) NotifyTaskSubmitted() {
	select {
	case s.notifyCh <- struct{}{}:
	default:
		// channel 已满，说明已有待处理唤醒信号
	}
}

// Sessions 返回会话字典（供 Controller 读写）。
func (s *TaskScheduler) Sessions() map[string]sessioninterfaces.SessionFacade {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions
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
// 对齐 Python: TaskScheduler.pause_task
func (s *TaskScheduler) PauseTask(ctx context.Context, taskID string) error {
	s.mu.Lock()
	entry, exists := s.runningTasks[taskID]
	s.mu.Unlock()

	if !exists {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Msg("暂停任务失败：任务不在运行中")
		return exception.NewBaseError(exception.StatusAgentControllerTaskExecutionError,
			exception.WithMsg(fmt.Sprintf("任务不在运行中: %s", taskID)),
		)
	}

	// 获取任务信息
	tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})
	if err != nil || len(tasks) == 0 {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("暂停任务失败：无法获取任务")
		return exception.NewBaseError(exception.StatusAgentControllerTaskExecutionError,
			exception.WithMsg(fmt.Sprintf("无法获取任务: %s", taskID)),
		)
	}
	task := tasks[0]

	// 获取会话
	sess := s.sessions[task.SessionID]
	if sess == nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Str("session_id", task.SessionID).
			Msg("暂停任务失败：会话不存在")
		return exception.NewBaseError(exception.StatusAgentControllerTaskExecutionError,
			exception.WithMsg(fmt.Sprintf("会话不存在: %s", task.SessionID)),
		)
	}

	// 检查是否可暂停
	canPause, reason, err := entry.executor.CanPause(ctx, taskID, sess)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("检查暂停失败")
		return err
	}
	if !canPause {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Str("reason", reason).
			Msg("任务不可暂停")
		return exception.NewBaseError(exception.StatusAgentControllerTaskExecutionError,
			exception.WithMsg(fmt.Sprintf("任务不可暂停: %s, 原因: %s", taskID, reason)),
		)
	}

	// 执行暂停
	if err := entry.executor.Pause(ctx, taskID, sess); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("暂停任务执行失败")
		return err
	}

	// 取消运行中的 goroutine
	if entry.cancel != nil {
		entry.cancel()
	}

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
			Msg("更新任务状态为 PAUSED 失败")
		return err
	}

	logger.Info(logComponent).
		Str("event_type", "task_paused").
		Str("task_id", taskID).
		Msg("任务已暂停")
	return nil
}

// CancelTask 取消指定任务。
//
// 对齐 Python: TaskScheduler.cancel_task
func (s *TaskScheduler) CancelTask(ctx context.Context, taskID string) error {
	// 获取任务信息
	tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})
	if err != nil || len(tasks) == 0 {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("取消任务失败：无法获取任务")
		return exception.NewBaseError(exception.StatusAgentControllerTaskExecutionError,
			exception.WithMsg(fmt.Sprintf("无法获取任务: %s", taskID)),
		)
	}
	task := tasks[0]

	// SUBMITTED 状态直接标记为 CANCELED
	if task.Status == schema.TaskSubmitted {
		if err := s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskCanceled); err != nil {
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("task_id", taskID).
				Err(err).
				Msg("更新已提交任务状态为 CANCELED 失败")
			return err
		}
		logger.Info(logComponent).
			Str("event_type", "task_canceled").
			Str("task_id", taskID).
			Msg("已提交任务已取消")
		return nil
	}

	// WORKING 状态需要取消运行中的 goroutine
	s.mu.Lock()
	entry, exists := s.runningTasks[taskID]
	s.mu.Unlock()

	if !exists {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Msg("取消任务失败：任务不在运行中")
		return exception.NewBaseError(exception.StatusAgentControllerTaskExecutionError,
			exception.WithMsg(fmt.Sprintf("任务不在运行中: %s", taskID)),
		)
	}

	// 获取会话
	sess := s.sessions[task.SessionID]
	if sess == nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Str("session_id", task.SessionID).
			Msg("取消任务失败：会话不存在")
		return exception.NewBaseError(exception.StatusAgentControllerTaskExecutionError,
			exception.WithMsg(fmt.Sprintf("会话不存在: %s", task.SessionID)),
		)
	}

	// 检查是否可取消
	canCancel, reason, err := entry.executor.CanCancel(ctx, taskID, sess)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("检查取消失败")
		return err
	}
	if !canCancel {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Str("reason", reason).
			Msg("任务不可取消")
		return exception.NewBaseError(exception.StatusAgentControllerTaskExecutionError,
			exception.WithMsg(fmt.Sprintf("任务不可取消: %s, 原因: %s", taskID, reason)),
		)
	}

	// 执行取消
	if err := entry.executor.Cancel(ctx, taskID, sess); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("取消任务执行失败")
		return err
	}

	// 取消运行中的 goroutine
	if entry.cancel != nil {
		entry.cancel()
	}

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
			Msg("更新任务状态为 CANCELED 失败")
		return err
	}

	logger.Info(logComponent).
		Str("event_type", "task_canceled").
		Str("task_id", taskID).
		Msg("任务已取消")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// schedule 调度循环，扫描 SUBMITTED 任务并启动执行。
//
// 对齐 Python: TaskScheduler._schedule
func (s *TaskScheduler) schedule(ctx context.Context) {
	logger.Info(logComponent).
		Str("event_type", "task_scheduler_started").
		Msg("TaskScheduler 调度循环已启动")

	for s.running.Load() {
		// 1. 扫描 SUBMITTED 任务
		tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{Status: schema.TaskSubmitted})
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Err(err).
				Msg("扫描 SUBMITTED 任务失败")
		}

		// 2. 并发启动（受 maxConcurrentTasks 限制）
		for _, task := range tasks {
			sess := s.sessions[task.SessionID]
			if sess == nil {
				logger.Warn(logComponent).
					Str("task_id", task.TaskID).
					Str("session_id", task.SessionID).
					Msg("任务对应会话不存在，跳过")
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
			s.runningTasks[task.TaskID] = &runningTaskEntry{cancel: taskCancel}
			s.mu.Unlock()

			go s.executeTaskWrapper(taskCtx, task.TaskID, sess)

			logger.Info(logComponent).
				Str("event_type", "task_started").
				Str("task_id", task.TaskID).
				Str("task_type", task.TaskType).
				Msg("任务已启动")
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
		Msg("TaskScheduler 调度循环已停止")
}

// executeTask 执行单个任务，读取 chunk channel 并处理状态转换。
//
// 对齐 Python: TaskScheduler._execute_task
func (s *TaskScheduler) executeTask(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) {
	// 1. 获取任务
	tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})
	if err != nil || len(tasks) == 0 {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("执行任务失败：无法获取任务")
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
			Msg("获取任务执行器失败")
		// 更新任务状态为 FAILED
		_ = s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskFailed, WithErrorMessage(err.Error()))
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
			Msg("更新任务状态为 WORKING 失败")
		return
	}

	// 4. 执行任务，获取 chunk channel
	chunkCh, err := executor.ExecuteAbility(ctx, taskID, sess)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("任务执行启动失败")
		_ = s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskFailed, WithErrorMessage(err.Error()))
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
				Msg("写入流数据失败")
		}

		// 7. 根据 payload.type 判断状态
		if chunk.Payload == nil {
			continue
		}

		payloadType := chunk.Payload.Type

		switch payloadType {
		case payloadTypeTaskCompletion:
			// TASK_COMPLETION → COMPLETED
			if err := s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskCompleted); err != nil {
				logger.Error(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("task_id", taskID).
					Err(err).
					Msg("更新任务状态为 COMPLETED 失败")
			}
			logger.Info(logComponent).
				Str("event_type", "task_completed").
				Str("task_id", taskID).
				Msg("任务已完成")
			// 发布任务事件
			s.publishTaskEvent(ctx, taskID, chunk, sess)
			return

		case payloadTypeTaskInteraction:
			// TASK_INTERACTION → INPUT_REQUIRED
			if err := s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskInputRequired); err != nil {
				logger.Error(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("task_id", taskID).
					Err(err).
					Msg("更新任务状态为 INPUT_REQUIRED 失败")
			}
			logger.Info(logComponent).
				Str("event_type", "task_input_required").
				Str("task_id", taskID).
				Msg("任务需要用户输入")
			// 发布任务事件
			s.publishTaskEvent(ctx, taskID, chunk, sess)
			return

		case payloadTypeTaskFailed:
			// TASK_FAILED → FAILED
			errMsg := ""
			if chunk.Payload.Metadata != nil {
				if msg, ok := chunk.Payload.Metadata["error_message"].(string); ok {
					errMsg = msg
				}
			}
			if err := s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskFailed, WithErrorMessage(errMsg)); err != nil {
				logger.Error(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("task_id", taskID).
					Err(err).
					Msg("更新任务状态为 FAILED 失败")
			}
			logger.Info(logComponent).
				Str("event_type", "task_failed").
				Str("task_id", taskID).
				Msg("任务已失败")
			// 发布任务事件
			s.publishTaskEvent(ctx, taskID, chunk, sess)
			return

		case schema.TaskProcessing:
			// processing → continue
			logger.Debug(logComponent).
				Str("task_id", taskID).
				Msg("任务处理中分片")
			continue

		default:
			// 未知类型，继续处理下一个 chunk
			logger.Debug(logComponent).
				Str("task_id", taskID).
				Str("payload_type", payloadType).
				Msg("未知 payload 类型")
		}
	}
}

// executeTaskWrapper 任务执行包装器，处理超时、取消和异常。
//
// 对齐 Python: TaskScheduler._execute_task_wrapper
func (s *TaskScheduler) executeTaskWrapper(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) {
	defer func() {
		// finally: 从 runningTasks 删除 + ensureSessionCompletionSignal
		s.mu.Lock()
		delete(s.runningTasks, taskID)
		s.mu.Unlock()

		// 获取任务信息判断 session
		tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})
		if err == nil && len(tasks) > 0 {
			s.ensureSessionCompletionSignal(ctx, tasks[0].SessionID, sess)
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
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("task_id", taskID).
				Str("method", "executeTaskWrapper").
				Str("model_provider", "task_scheduler").
				Msg(fmt.Sprintf("任务执行异常: %v", r))
			_ = s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskFailed,
				WithErrorMessage(fmt.Sprintf("任务执行异常: %v", r)),
			)
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
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("task_id", taskID).
				Msg("任务执行超时")
			_ = s.taskManager.UpdateTaskStatus(ctx, taskID, schema.TaskFailed,
				WithErrorMessage("任务执行超时"),
			)
		} else if taskCtx.Err() == context.Canceled {
			logger.Info(logComponent).
				Str("event_type", "task_canceled").
				Str("task_id", taskID).
				Msg("任务已取消")
		}
	}
}

// ensureSessionCompletionSignal 检查并发送 all_tasks_processed 信号。
//
// 对齐 Python: TaskScheduler._ensure_session_completion_signal
func (s *TaskScheduler) ensureSessionCompletionSignal(ctx context.Context, sessionID string, sess sessioninterfaces.SessionFacade) {
	s.mu.Lock()
	suppress := s.config.SuppressCompletionSignal
	s.mu.Unlock()

	if suppress {
		logger.Debug(logComponent).
			Str("session_id", sessionID).
			Msg("完成信号被抑制")
		return
	}

	if !s.areAllTasksCompleted(ctx, sessionID) {
		return
	}

	// 发送 all_tasks_processed chunk 到 session 流
	chunk := &schema.ControllerOutputChunk{
		Payload: &schema.ControllerOutputPayload{
			Type: schema.AllTasksProcessed,
		},
		LastChunk: true,
	}
	if err := sess.WriteStream(ctx, chunk); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("session_id", sessionID).
			Err(err).
			Msg("写入 all_tasks_processed 信号失败")
		return
	}

	logger.Info(logComponent).
		Str("event_type", "all_tasks_processed").
		Str("session_id", sessionID).
		Msg("所有任务已处理，已发送完成信号")
}

// areAllTasksCompleted 检查会话内所有任务是否处于终态。
//
// 对齐 Python: TaskScheduler._are_all_tasks_completed
func (s *TaskScheduler) areAllTasksCompleted(ctx context.Context, sessionID string) bool {
	tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{SessionID: sessionID})
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("session_id", sessionID).
			Err(err).
			Msg("检查任务完成状态失败")
		return false
	}

	for _, task := range tasks {
		if task.Status == schema.TaskSubmitted || task.Status == schema.TaskWorking {
			return false
		}
	}
	return true
}

// publishTaskEvent 根据 chunk.payload.type 构建事件并通过 EventQueue 发布。
//
// 对齐 Python: TaskScheduler._publish_task_event
func (s *TaskScheduler) publishTaskEvent(ctx context.Context, taskID string, chunk *schema.ControllerOutputChunk, sess sessioninterfaces.SessionFacade) {
	if s.eventQueue == nil || chunk.Payload == nil {
		return
	}

	// 获取任务信息
	tasks, err := s.taskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})
	if err != nil || len(tasks) == 0 {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("发布任务事件失败：无法获取任务")
		return
	}
	task := tasks[0]

	// 获取 agentID
	agentID := ""
	if card, ok := s.card.(interface{ GetID() string }); ok {
		agentID = card.GetID()
	}

	var event schema.Event

	switch chunk.Payload.Type {
	case payloadTypeTaskCompletion:
		event = &schema.TaskCompletionEvent{
			BaseEvent:  *schema.NewBaseEvent(schema.EventTaskCompletion),
			TaskResult: chunk.Payload.Data,
			Task:       task,
		}
	case payloadTypeTaskInteraction:
		event = &schema.TaskInteractionEvent{
			BaseEvent:   *schema.NewBaseEvent(schema.EventTaskInteraction),
			Interaction: chunk.Payload.Data,
			Task:        task,
		}
	case payloadTypeTaskFailed:
		errMsg := ""
		if chunk.Payload.Metadata != nil {
			if msg, ok := chunk.Payload.Metadata["error_message"].(string); ok {
				errMsg = msg
			}
		}
		event = &schema.TaskFailedEvent{
			BaseEvent:    *schema.NewBaseEvent(schema.EventTaskFailed),
			ErrorMessage: errMsg,
			Task:         task,
		}
	default:
		return
	}

	if err := s.eventQueue.PublishEventAsync(ctx, agentID, sess, event); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("task_id", taskID).
			Err(err).
			Msg("异步发布任务事件失败")
	} else {
		logger.Info(logComponent).
			Str("event_type", "task_event_published").
			Str("task_id", taskID).
			Str("payload_type", chunk.Payload.Type).
			Msg("任务事件已发布")
	}
}

// waitAllTasksComplete 收集所有运行中任务的 CancelFunc，调用 cancel 并等待退出。
//
// 对齐 Python: TaskScheduler._wait_all_tasks_complete
func (s *TaskScheduler) waitAllTasksComplete(_ context.Context) {
	s.mu.Lock()
	entries := make(map[string]context.CancelFunc, len(s.runningTasks))
	for id, entry := range s.runningTasks {
		entries[id] = entry.cancel
	}
	s.mu.Unlock()

	for id, cancel := range entries {
		if cancel != nil {
			cancel()
		}
		logger.Info(logComponent).
			Str("event_type", "task_cancel_on_stop").
			Str("task_id", id).
			Msg("停止时取消运行中任务")
	}

	// 等待 runningTasks 清空
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		count := len(s.runningTasks)
		s.mu.Unlock()
		if count == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
