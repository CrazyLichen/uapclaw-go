package controller

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	ability "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Controller 事件驱动任务编排控制器。
// 它是 ControllerAgent 的核心组件，负责处理事件、管理任务生命周期、
// 执行意图识别和处理。
// 对应 Python: openjiuwen/core/controller/base.py::Controller
type Controller struct {
	// card Agent 身份元数据
	card *agentschema.AgentCard
	// abilityMgr 能力管理器
	abilityMgr *ability.AbilityManager
	// config 控制器配置
	config *config.ControllerConfig
	// contextEngine 上下文引擎
	contextEngine iface.ContextEngine

	// taskManager 任务管理器（Init 中创建）
	taskManager *modules.TaskManager
	// eventQueue 事件队列（Init 中创建）
	eventQueue *modules.EventQueue
	// taskScheduler 任务调度器（Init 中创建）
	taskScheduler *modules.TaskScheduler
	// eventHandler 事件处理器
	eventHandler modules.EventHandler

	// started 运行状态标记
	started atomic.Bool
	// mu 保护 Start/Stop 并发
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewController 创建空壳 Controller。
// 必须随后调用 Init() 完成初始化。
// 对应 Python: Controller.__init__()
func NewController() *Controller {
	return &Controller{}
}

// Init 两阶段初始化，创建子组件并接线。
// 对应 Python: Controller.init(card, config, ability_manager, context_engine)
func (c *Controller) Init(
	card *agentschema.AgentCard,
	cfg *config.ControllerConfig,
	abilityMgr *ability.AbilityManager,
	contextEngine iface.ContextEngine,
) {
	c.card = card
	c.config = cfg
	c.abilityMgr = abilityMgr
	c.contextEngine = contextEngine

	c.taskManager = modules.NewTaskManager(cfg)
	c.eventQueue = modules.NewEventQueue(cfg)
	c.taskScheduler = modules.NewTaskScheduler(
		cfg,
		c.taskManager,
		contextEngine,
		abilityMgr,
		c.eventQueue,
		card,
	)

	// 接线：TaskManager 的 onTaskSubmitted 回调 → TaskScheduler.NotifyTaskSubmitted
	c.taskManager.SetOnTaskSubmitted(c.taskScheduler.NotifyTaskSubmitted)

	logger.Info(logComponent).Msg("Controller 初始化完成")
}

// Start 启动控制器（EventQueue + TaskScheduler）。
// 对应 Python: Controller.start()
func (c *Controller) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started.Load() {
		return nil
	}
	c.eventQueue.Start()
	if err := c.taskScheduler.Start(ctx); err != nil {
		return err
	}
	c.started.Store(true)
	logger.Info(logComponent).Msg("Controller 已启动")
	return nil
}

// Stop 停止控制器（TaskScheduler + EventQueue）。
// 对应 Python: Controller.stop()
func (c *Controller) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started.Load() {
		return nil
	}
	if err := c.taskScheduler.Stop(ctx); err != nil {
		return err
	}
	if err := c.eventQueue.Stop(ctx); err != nil {
		return err
	}
	c.started.Store(false)
	logger.Info(logComponent).Msg("Controller 已停止")
	return nil
}

// SetEventHandler 注入事件处理器并接线依赖。
// 对应 Python: Controller.set_event_handler(event_handler)
// 偏差8 修复：SetEventHandler 立即同步 EventQueue 的 EventHandler，对齐 Python 行为。
func (c *Controller) SetEventHandler(handler modules.EventHandler) {
	c.eventHandler = handler
	base := handler.GetBase()
	base.Config = c.config
	base.ContextEngine = c.contextEngine
	base.TaskScheduler = c.taskScheduler
	base.TaskManager = c.taskManager
	base.AbilityMgr = c.abilityMgr
	// 立即同步到 EventQueue
	if c.eventQueue != nil {
		c.eventQueue.SetEventHandler(handler)
	}
}

// AddTaskExecutor 注册 TaskExecutor，支持链式调用。
// 对应 Python: Controller.add_task_executor(task_type, builder)
func (c *Controller) AddTaskExecutor(taskType string, builder func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor) *Controller {
	c.taskScheduler.TaskExecutorRegistry().AddTaskExecutor(taskType, builder)
	return c
}

// RemoveTaskExecutor 移除 TaskExecutor。
// 对应 Python: Controller.remove_task_executor(task_type)
func (c *Controller) RemoveTaskExecutor(taskType string) {
	c.taskScheduler.TaskExecutorRegistry().RemoveTaskExecutor(taskType)
}

// GetTaskExecutor 获取 TaskExecutor。
// 对应 Python: Controller.get_task_executor(config, ability_manager, context_engine, task_manager)
// 注意：Python 的 Controller.get_task_executor 签名与 TaskExecutorRegistry.get_task_executor(task_type, dependencies) 不匹配
// Python 传入的 4 个参数被当作 (task_type, dependencies) 的位置参数，实际有 bug
// Go 保持正确的签名：(taskType, deps)，与 TaskExecutorRegistry.get_task_executor 一致
func (c *Controller) GetTaskExecutor(taskType string, deps *modules.TaskExecutorDependencies) (modules.TaskExecutor, error) {
	return c.taskScheduler.TaskExecutorRegistry().GetTaskExecutor(taskType, deps)
}

// PublishEventAsync 异步发布事件（fire-and-forget）。
// 对应 Python: Controller.publish_event_async(session, event)
func (c *Controller) PublishEventAsync(ctx context.Context, sess *session.Session, event schema.Event) error {
	return c.eventQueue.PublishEventAsync(ctx, c.card.ID, sess, event)
}

// BindSession 绑定 session 到 Controller 基础设施。
// 执行：ensureStarted → 恢复状态 → 注册 session → 订阅事件队列。
// 对应 Python: Controller.bind_session(session)
func (c *Controller) BindSession(ctx context.Context, sess *session.Session) error {
	if err := c.ensureStarted(ctx); err != nil {
		return err
	}
	sessionID := sess.GetSessionID()
	c.restoreTaskManagerState(ctx, sessioninterfaces.SessionFacade(sess))
	c.taskScheduler.Sessions()[sessionID] = sess
	if err := c.eventQueue.Subscribe(ctx, c.card.ID, sessionID); err != nil {
		return err
	}
	logger.Info(logComponent).Str("session_id", sessionID).Msg("session 已绑定")
	return nil
}

// UnbindSession 解绑 session 并执行清理。
// 执行：保存状态 → 取消订阅 → 移除 session。
// 对应 Python: Controller.unbind_session(session)
func (c *Controller) UnbindSession(ctx context.Context, sess *session.Session) error {
	sessionID := sess.GetSessionID()
	_ = c.saveTaskManagerState(ctx, sessioninterfaces.SessionFacade(sess))
	if err := c.eventQueue.Unsubscribe(ctx, c.card.ID, sessionID); err != nil {
		return err
	}
	delete(c.taskScheduler.Sessions(), sessionID)
	logger.Info(logComponent).Str("session_id", sessionID).Msg("session 已解绑")
	return nil
}

// Invoke 批量执行，收集所有 chunk 后返回 ControllerOutput。
// 对应 Python: Controller.invoke(inputs, session)
// 偏差7 修复：对齐 Python invoke() 的异常包装逻辑
func (c *Controller) Invoke(
	ctx context.Context,
	inputs *schema.InputEvent,
	sess *session.Session,
) (*schema.ControllerOutput, error) {
	ch, errCh := c.Stream(ctx, inputs, sess, []stream.StreamMode{stream.StreamModeOutput})

	var chunks []*schema.ControllerOutputChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	// 检查 Stream goroutine 是否返回错误
	// sendStreamError 已做 BaseError/非BaseError 区分，直接返回
	if err := <-errCh; err != nil {
		return nil, err
	}

	return &schema.ControllerOutput{
		Type: string(schema.EventTaskCompletion),
		Data: chunks,
	}, nil
}

// Stream 流式执行，返回输出 chunk channel 和错误 channel。
// 内部启动 goroutine 执行完整流程：
// ensureStarted → 恢复状态 → 注册 session → 订阅 → 发布事件 →
// 确保完成信号 → 读取 stream（首帧超时） → finally 清理。
// 对应 Python: Controller.stream(inputs, session, stream_modes)
// 偏差7 修复：新增 errCh 返回值，供 Invoke 检测流错误
func (c *Controller) Stream(
	ctx context.Context,
	inputs *schema.InputEvent,
	sess *session.Session,
	streamModes []stream.StreamMode,
) (<-chan *schema.ControllerOutputChunk, <-chan error) {
	out := make(chan *schema.ControllerOutputChunk)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)

		// 0. 懒启动
		if err := c.ensureStarted(ctx); err != nil {
			logger.Error(logComponent).Err(err).Msg("Stream ensureStarted 失败")
			sendStreamError(errCh, err)
			return
		}

		agentID := c.card.ID
		sessionID := sess.GetSessionID()

		// 1. 恢复 TaskManager 状态
		stateRestored := c.restoreTaskManagerState(ctx, sessioninterfaces.SessionFacade(sess))
		if !stateRestored {
			logger.Info(logComponent).Str("session_id", sessionID).Msg("以全新 TaskManager 状态启动")
		}

		// 2. 注册 session
		c.taskScheduler.Sessions()[sessionID] = sess

		// finally 清理（对应 Python finally 块）
		defer func() {
			// 7. 保存 TaskManager 状态
			_ = c.saveTaskManagerState(ctx, sessioninterfaces.SessionFacade(sess))
			// 8. 取消订阅
			_ = c.eventQueue.Unsubscribe(ctx, agentID, sessionID)
			// 9. 移除 session
			delete(c.taskScheduler.Sessions(), sessionID)
			logger.Info(logComponent).Str("session_id", sessionID).
				Int("active_sessions", len(c.taskScheduler.Sessions())).
				Msg("session 完成")
		}()

		// 3. 订阅事件
		if err := c.eventQueue.Subscribe(ctx, agentID, sessionID); err != nil {
			logger.Error(logComponent).Err(err).Str("session_id", sessionID).Msg("Stream Subscribe 失败")
			sendStreamError(errCh, err)
			return
		}

		// 4. 发布输入事件（同步，等待 handler 处理完）
		if err := c.eventQueue.PublishEvent(ctx, agentID, sess, inputs); err != nil {
			logger.Error(logComponent).Err(err).Str("session_id", sessionID).Msg("Stream PublishEvent 失败")
			sendStreamError(errCh, err)
			return
		}

		// 5. 确保完成信号（如果 handler 没创建任务，立即发送 all_tasks_processed）
		c.taskScheduler.EnsureSessionCompletionSignal(ctx, sessionID)

		// 6. 从 session stream 读取 chunk
		firstFrameTimeout := c.config.StreamFirstFrameTimeout
		if firstFrameTimeout <= 0 {
			firstFrameTimeout = 30.0
		}

		iter := sess.StreamIterator()
		gotFirst := false

		// 首帧超时等待
		select {
		case firstSchema, ok := <-iter:
			if !ok {
				return // stream 已关闭
			}
			gotFirst = true
			// 偏差5 修复：对齐 Python，非 ControllerOutputChunk 的首帧也转发
			firstChunk, ok := firstSchema.(*schema.ControllerOutputChunk)
			if ok {
				// 检查首帧是否为 all_tasks_processed
				if !c.isCompletionSignal(firstChunk) {
					out <- firstChunk
				}
			} else {
				// Go 的 out channel 类型为 *ControllerOutputChunk，无法直接转发非该类型 chunk
				// 对齐 Python：Python 直接 yield 非 ControllerOutputChunk chunk
				// Go 约束：记录日志后跳过（因强类型 channel 无法承载其他类型）
				logger.Warn(logComponent).Str("session_id", sessionID).
					Str("schema_type", firstSchema.SchemaType()).
					Msg("首帧类型不是 ControllerOutputChunk，Go 强类型约束无法转发，跳过")
			}
		case <-time.After(time.Duration(firstFrameTimeout * float64(time.Second))):
			logger.Error(logComponent).Float64("timeout", firstFrameTimeout).Str("session_id", sessionID).Msg("首帧超时")
			sendStreamError(errCh, fmt.Errorf("Stream 首帧超时 (%.1fs)", firstFrameTimeout))
			return
		case <-ctx.Done():
			logger.Error(logComponent).Err(ctx.Err()).Str("session_id", sessionID).Msg("Stream 上下文取消")
			sendStreamError(errCh, ctx.Err())
			return
		}

		if !gotFirst {
			return
		}

		// 继续读取后续 chunk
		for schemaItem := range iter {
			chunk, ok := schemaItem.(*schema.ControllerOutputChunk)
			if !ok {
				// 偏差6 修复：对齐 Python，非 ControllerOutputChunk chunk 仍 yield
				// Go 约束：强类型 channel 无法转发，记录日志后跳过
				logger.Warn(logComponent).Str("session_id", sessionID).
					Str("schema_type", schemaItem.SchemaType()).
					Msg("chunk 类型不是 ControllerOutputChunk，Go 强类型约束无法转发，跳过")
				continue
			}
			if c.isCompletionSignal(chunk) {
				logger.Info(logComponent).Str("session_id", sessionID).Msg("所有任务已处理完毕，停止流")
				break
			}
			out <- chunk
		}
	}()

	return out, errCh
}

// Config 获取控制器配置。
// 对应 Python: Controller.config (property getter)
func (c *Controller) Config() *config.ControllerConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// SetConfig 设置控制器配置，级联传播到所有子组件。
// 对应 Python: Controller.config (property setter)
func (c *Controller) SetConfig(cfg *config.ControllerConfig) {
	c.mu.Lock()
	c.config = cfg
	c.mu.Unlock()
	if c.taskManager != nil {
		c.taskManager.SetConfig(cfg)
	}
	if c.eventQueue != nil {
		c.eventQueue.SetConfig(cfg)
	}
	if c.taskScheduler != nil {
		c.taskScheduler.SetConfig(cfg)
	}
	if c.eventHandler != nil {
		c.eventHandler.GetBase().Config = cfg
	}
}

// EventQueue 获取事件队列。
// 对应 Python: Controller.event_queue (property)
func (c *Controller) EventQueue() *modules.EventQueue {
	return c.eventQueue
}

// TaskManager 获取任务管理器。
// 对应 Python: Controller.task_manager (property)
func (c *Controller) TaskManager() *modules.TaskManager {
	return c.taskManager
}

// TaskScheduler 获取任务调度器。
// 对应 Python: Controller.task_scheduler (property)
func (c *Controller) TaskScheduler() *modules.TaskScheduler {
	return c.taskScheduler
}

// EventHandler 获取事件处理器。
// 对应 Python: Controller.event_handler (property)
func (c *Controller) EventHandler() modules.EventHandler {
	return c.eventHandler
}

// ContextEngine 获取上下文引擎。
// 对应 Python: Controller.context_engine (property getter)
func (c *Controller) ContextEngine() iface.ContextEngine {
	return c.contextEngine
}

// SetContextEngine 设置上下文引擎。
// 对应 Python: Controller.context_engine (property setter)
func (c *Controller) SetContextEngine(ce iface.ContextEngine) {
	c.contextEngine = ce
}

// AbilityManager 获取能力管理器。
// 对应 Python: Controller.ability_manager (property getter)
func (c *Controller) AbilityManager() *ability.AbilityManager {
	return c.abilityMgr
}

// SetAbilityManager 设置能力管理器。
// 对应 Python: Controller.ability_manager (property setter)
func (c *Controller) SetAbilityManager(am *ability.AbilityManager) {
	c.abilityMgr = am
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureStarted 懒启动，确保 EventQueue 和 TaskScheduler 已运行。
// 对应 Python: Controller._ensure_started()
// Go 中无需事件循环检测，简化为首次启动检查。
func (c *Controller) ensureStarted(ctx context.Context) error {
	if c.started.Load() {
		return nil
	}
	if err := c.Start(ctx); err != nil {
		return err
	}
	// 偏差8 修复：对齐 Python _ensure_started 中 set_event_handler 调用
	if c.eventHandler != nil {
		c.eventQueue.SetEventHandler(c.eventHandler)
	}
	return nil
}

// restoreTaskManagerState 从 session 恢复 TaskManager 状态。
// 如果恢复失败，清空当前 TaskManager 状态，允许后续新任务不受影响。
// 对应 Python: Controller._restore_task_manager_state(session)
func (c *Controller) restoreTaskManagerState(ctx context.Context, sess sessioninterfaces.SessionFacade) bool {
	controllerState, err := sess.GetState(state.StringKey("controller"))
	if err != nil || controllerState == nil {
		// 无保存状态，清空 TaskManager
		logger.Info(logComponent).Str("session_id", sess.GetSessionID()).Msg("无保存状态，清空 TaskManager")
		_ = c.taskManager.ClearState(ctx)
		return false
	}

	stateMap, ok := controllerState.(map[string]any)
	if !ok {
		logger.Info(logComponent).Str("session_id", sess.GetSessionID()).Msg("状态类型不匹配，清空 TaskManager")
		_ = c.taskManager.ClearState(ctx)
		return false
	}

	tmStateData, exists := stateMap["task_manager_state"]
	if !exists {
		logger.Info(logComponent).Str("session_id", sess.GetSessionID()).Msg("无 task_manager_state，清空 TaskManager")
		_ = c.taskManager.ClearState(ctx)
		return false
	}

	logger.Info(logComponent).Str("session_id", sess.GetSessionID()).Msg("恢复 TaskManager 状态")

	// 反序列化 TaskManagerState
	tmStateMap, ok := tmStateData.(map[string]any)
	if !ok {
		logger.Error(logComponent).Str("session_id", sess.GetSessionID()).Msg("task_manager_state 类型不匹配，清空 TaskManager")
		_ = c.taskManager.ClearState(ctx)
		return false
	}

	tmState, err := modules.TaskManagerStateFromMap(tmStateMap)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("session_id", sess.GetSessionID()).Msg("反序列化 TaskManagerState 失败，清空 TaskManager")
		_ = c.taskManager.ClearState(ctx)
		return false
	}

	if err := c.taskManager.LoadState(ctx, tmState); err != nil {
		logger.Error(logComponent).Err(err).Str("session_id", sess.GetSessionID()).Msg("加载 TaskManagerState 失败，清空 TaskManager")
		_ = c.taskManager.ClearState(ctx)
		return false
	}

	logger.Info(logComponent).
		Str("session_id", sess.GetSessionID()).
		Int("task_count", len(tmState.Tasks)).
		Int("root_task_count", len(tmState.RootTasks)).
		Msg("成功恢复 TaskManager 状态")
	return true
}

// saveTaskManagerState 保存 TaskManager 状态到 session。
// 对应 Python: Controller._save_task_manager_state(session)
func (c *Controller) saveTaskManagerState(ctx context.Context, sess sessioninterfaces.SessionFacade) error {
	if !c.config.EnableTaskPersistence {
		logger.Info(logComponent).Msg("任务持久化已禁用，跳过保存 TaskManager 状态")
		return nil
	}

	tmState, err := c.taskManager.GetState(ctx)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("session_id", sess.GetSessionID()).Msg("获取 TaskManagerState 失败")
		return err
	}

	controllerState := map[string]any{
		"task_manager_state": tmState.ToMap(),
	}

	// 先清除旧状态，再更新新状态（确保嵌套键正确清理）
	sess.UpdateState(map[string]any{"controller": nil})
	sess.UpdateState(map[string]any{"controller": controllerState})

	logger.Info(logComponent).
		Str("session_id", sess.GetSessionID()).
		Int("task_count", len(tmState.Tasks)).
		Int("root_task_count", len(tmState.RootTasks)).
		Msg("保存 TaskManager 状态")
	return nil
}

// isCompletionSignal 检查 chunk 是否为 all_tasks_processed 完成信号。
func (c *Controller) isCompletionSignal(chunk *schema.ControllerOutputChunk) bool {
	if chunk == nil || chunk.Payload == nil {
		return false
	}
	return chunk.Payload.Type == schema.AllTasksProcessed
}

// buildControllerRuntimeError 构建控制器运行时错误。
func buildControllerRuntimeError(reason string, cause error) *exception.BaseError {
	opts := []exception.ErrorOption{
		exception.WithParam("error_msg", reason),
	}
	if cause != nil {
		opts = append(opts, exception.WithCause(cause))
	}
	return exception.NewBaseError(exception.StatusAgentControllerRuntimeError, opts...)
}

// sendStreamError 将错误发送到 errCh，对齐 Python 的异常透传逻辑。
// BaseError 直接透传（对应 Python except BaseError: raise），
// 其他 error 包装为 AGENT_CONTROLLER_RUNTIME_ERROR（对应 Python except Exception: build_error）。
func sendStreamError(errCh chan<- error, err error) {
	if _, ok := err.(*exception.BaseError); ok {
		errCh <- err
	} else {
		errCh <- buildControllerRuntimeError(formatError(err), err)
	}
}

// formatError 格式化错误信息用于日志
func formatError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
