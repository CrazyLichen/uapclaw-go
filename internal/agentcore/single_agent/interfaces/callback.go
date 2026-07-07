package interfaces

import (
	"context"
	"errors"
	"fmt"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cschema2 "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentCallbackEvent Agent 生命周期回调事件类型。
//
// 定义 per-Agent 实例级的 10 个生命周期事件，
// 供 AgentCallbackManager 注册回调和 AgentRail 钩子使用。
// 与框架层 GlobalAgentEventType（全局观测事件）是不同层次：
//   - AgentCallbackEvent = 实例级 Rail 拦截/控制（重试/提前终止/steering）
//   - GlobalAgentEventType = 框架级全局观测（日志/监控/transform_io）
//
// 事件值即 Python AgentRail 对应方法名，无需额外 EVENT_METHOD_MAP 映射。
// AgentCallbackManager 注册时通过 agentID 前缀构造唯一事件名
// （如 "{agentID}_before_invoke"），与框架层事件互不冲突。
//
// 对应 Python: openjiuwen/core/single_agent/rail/base.py (AgentCallbackEvent)
type AgentCallbackEvent string

// AgentCallbackContext Rail 系统与 Agent 运行时之间的核心中介对象。
//
// 承载三个控制机制：Retry（重试）、Force Finish（提前终止）、Steering（外部注入）。
// 在 ReAct 循环中创建，跨事件生命周期持久存在（extra 字段）。
//
// 对应 Python: openjiuwen/core/single_agent/rail/base.py AgentCallbackContext (L226-416)
type AgentCallbackContext struct {
	// agent 当前 Agent 实例引用
	agent BaseAgent
	// event 当前回调事件类型（由 Fire 设置）
	event AgentCallbackEvent
	// inputs 当前事件的输入数据（随事件变化）
	inputs EventInputs
	// config 运行时配置（最小化接口，预留）
	config railConfig
	// session 当前 Session
	session sessioninterfaces.SessionFacade
	// modelContext 当前 ModelContext
	modelContext ceinterface.ModelContext
	// extra 跨 rail 通信字典（单次 invoke 内跨事件持久，子 ctx 共享）
	extra map[string]any
	// exception 异常对象（在错误事件上设置）
	exception error
	// retryAttempt 当前重试索引号
	retryAttempt int

	// retryRequest 重试请求信号
	retryRequest *RetryRequest
	// forceFinishRequest 强制终止请求信号
	forceFinishRequest *ForceFinishRequest
	// steeringQueue 外部注入的 steering 消息队列
	steeringQueue chan string
}

// AgentCallbackManager PerAgent 实例级回调管理器。
//
// 对应 Python: AgentCallbackManager (openjiuwen/core/single_agent/agent_callback_manager.py)
// 不自持回调存储，将注册/触发委托给全局 CallbackFramework，
// 通过 "{agentID}_{event}" 前缀实现命名空间隔离。
type AgentCallbackManager struct {
	// agentID Agent 唯一标识，用于构造事件名前缀
	agentID string
}

// AgentRail Agent 生命周期 Rail 接口。
//
// Rail 是 class-based 的生命周期钩子容器，允许在 Agent 执行流程的
// 特定时机注入拦截逻辑（重试、提前终止、steering 等）。
// 嵌入 BaseRail 后只需覆盖关心的钩子方法，并在 GetCallbacks() 中
// 声明已覆盖的事件映射。
//
// 对应 Python: AgentRail(ABC) (openjiuwen/core/single_agent/rail/base.py L451-573)
type AgentRail interface {
	// Priority 返回执行优先级（数值越大越先执行）
	Priority() int
	// Init Rail 初始化钩子（注册时调用，用于工具自注册等）
	Init(agent BaseAgent) error
	// Uninit Rail 注销钩子（注销时调用，用于工具清理等）
	Uninit(agent BaseAgent) error

	// ── 10 个生命周期钩子方法 ──

	// BeforeInvoke invoke 开始前
	BeforeInvoke(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterInvoke invoke 完成后
	AfterInvoke(ctx context.Context, cbc *AgentCallbackContext) error
	// BeforeTaskIteration 外层任务循环迭代开始前
	BeforeTaskIteration(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterTaskIteration 外层任务循环迭代完成后
	AfterTaskIteration(ctx context.Context, cbc *AgentCallbackContext) error
	// BeforeModelCall LLM 调用前
	BeforeModelCall(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterModelCall LLM 响应后
	AfterModelCall(ctx context.Context, cbc *AgentCallbackContext) error
	// OnModelException LLM 调用异常
	OnModelException(ctx context.Context, cbc *AgentCallbackContext) error
	// BeforeToolCall 工具执行前
	BeforeToolCall(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterToolCall 工具执行后
	AfterToolCall(ctx context.Context, cbc *AgentCallbackContext) error
	// OnToolException 工具执行异常
	OnToolException(ctx context.Context, cbc *AgentCallbackContext) error

	// GetCallbacks 提取已覆盖的钩子方法映射，供 RegisterRail 批量注册。
	GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc
}

// BaseRail AgentRail 的 no-op 默认实现。
//
// 用户嵌入此结构体后只需覆盖关心的钩子方法，并在 GetCallbacks() 中
// 通过 CallbackFrom + BuildCallbacks 声明已覆盖的事件映射。
//
// 对应 Python: AgentRail 基类的 10 个默认 no-op 方法
type BaseRail struct {
	// priority 执行优先级（数值越大越先执行），默认 50
	priority int
}

// EventInputs 回调事件输入接口。
//
// 各事件类型对应不同的 Inputs 结构体，均实现此接口。
// 调用方通过 type switch 获取具体类型。
//
// 对应 Python: EventInputs = Union[InvokeInputs, ModelCallInputs, ToolCallInputs, TaskIterationInputs, Dict]
type EventInputs interface {
	// EventKind 返回事件输入的种类标识
	EventKind() string
}

// InvokeQuery Invoke 阶段的查询输入接口。
//
// 对齐 Python: InvokeInputs.query: Optional[str, InteractiveInput]
// InvokeQueryString（普通字符串）和 *InteractiveInput（中断恢复）均实现此接口。
type InvokeQuery interface {
	// IsInteractiveInput 检查是否为交互式输入（中断恢复）
	IsInteractiveInput() bool
	// PlainText 提取纯文本表示。
	PlainText() string
}

// RunContext 结构化运行时上下文（心跳等场景）。
//
// 对应 Python: RunContext (openjiuwen/core/single_agent/rail/base.py L56-62)
type RunContext struct {
	// Reason 心跳触发原因
	Reason HeartbeatReason
	// SessionID 会话标识
	SessionID string
	// ContextMode 上下文模式（如 "lightweight"）
	ContextMode string
	// Extra 额外上下文信息
	Extra map[string]any
}

// InvokeInputs BEFORE/AFTER_INVOKE 事件输入。
//
// 对应 Python: InvokeInputs (openjiuwen/core/single_agent/rail/base.py L68-96)
type InvokeInputs struct {
	// Query 用户查询输入（普通字符串或交互式输入）
	Query InvokeQuery
	// ConversationID 会话/对话标识
	ConversationID string
	// Result Agent invoke 结果（invoke 完成后填充）
	Result map[string]any
	// RunKind 运行模式（normal/heartbeat/cron）
	RunKind RunKind
	// RunContext 结构化运行时上下文
	RunContext *RunContext
}

// ModelCallInputs BEFORE/AFTER_MODEL_CALL 事件输入。
//
// 对应 Python: ModelCallInputs (openjiuwen/core/single_agent/rail/base.py L103-116)
type ModelCallInputs struct {
	// Messages 发送给 LLM 的消息列表
	Messages []llmschema.BaseMessage
	// Tools 工具定义列表
	Tools []cschema2.ToolInfoInterface
	// ModelContext 当前 ModelContext
	ModelContext ceinterface.ModelContext
	// Response LLM 响应（调用完成后填充）
	Response *llmschema.AssistantMessage
}

// ToolCallInputs BEFORE/AFTER_TOOL_CALL 事件输入。
//
// 对应 Python: ToolCallInputs (openjiuwen/core/single_agent/rail/base.py L119-134)
type ToolCallInputs struct {
	// ToolCall 原始工具调用对象
	ToolCall *llmschema.ToolCall
	// ToolName 工具名称（before 钩子可改写）
	ToolName string
	// ToolArgs 工具参数 JSON 字符串（before 钩子可改写）
	ToolArgs string
	// ToolResult 工具执行结果（调用完成后填充）
	ToolResult any
	// ToolMsg 工具返回消息（调用完成后填充）
	ToolMsg *llmschema.ToolMessage
}

// TaskIterationInputs BEFORE/AFTER_TASK_ITERATION 事件输入。
//
// 对应 Python: TaskIterationInputs (openjiuwen/core/single_agent/rail/base.py L137-162)
type TaskIterationInputs struct {
	// Iteration 1-based 外层循环迭代索引
	Iteration int
	// LoopEvent 触发本次迭代的事件对象
	LoopEvent any
	// ConversationID 会话/对话标识
	ConversationID string
	// Result 迭代结果（迭代完成后填充）
	Result map[string]any
	// Query 本次迭代的有效查询（before_task_iteration 钩子可修改）
	Query string
	// IsFollowUp 是否由 controller follow-up 触发
	IsFollowUp bool
}

// MapInputs 任意字典事件输入，作为 EventInputs 的兜底类型。
//
// 对齐 Python: EventInputs = Union[..., Dict[str, Any]]
type MapInputs struct {
	// Data 任意事件输入数据
	Data map[string]any
}

// RetryRequest 重试指令，由 on_exception 钩子产生。
//
// 对应 Python: RetryRequest (openjiuwen/core/single_agent/rail/base.py L165-169)
type RetryRequest struct {
	// DelaySeconds 重试前等待秒数
	DelaySeconds float64
}

// ForceFinishRequest 提前终止信号，使 Agent 循环立即返回结果。
//
// 对应 Python: ForceFinishRequest (openjiuwen/core/single_agent/rail/base.py L172-176)
type ForceFinishRequest struct {
	// Result 提前终止时返回的结果
	Result map[string]any
}

// InvokeQueryString 普通字符串查询，实现 InvokeQuery 接口。
type InvokeQueryString string

// RunKind 运行模式枚举。
type RunKind string

// HeartbeatReason 心跳触发原因枚举。
type HeartbeatReason string

// callbackEntry 事件→回调映射条目，BuildCallbacks 的参数。
type callbackEntry struct {
	event AgentCallbackEvent
	fn    cb.PerAgentCallbackFunc
}

// railConfig Rail 包所需的最小 Config 接口。
//
// 预留接口，当前无方法。未来 Rail 需要访问配置时在此添加方法。
type railConfig interface{}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// CallbackBeforeInvoke invoke 开始前
	CallbackBeforeInvoke AgentCallbackEvent = "before_invoke"
	// CallbackAfterInvoke invoke 完成后
	CallbackAfterInvoke AgentCallbackEvent = "after_invoke"
	// CallbackBeforeTaskIteration 外层任务循环迭代开始前
	CallbackBeforeTaskIteration AgentCallbackEvent = "before_task_iteration"
	// CallbackAfterTaskIteration 外层任务循环迭代完成后
	CallbackAfterTaskIteration AgentCallbackEvent = "after_task_iteration"
	// CallbackBeforeModelCall LLM 调用前
	CallbackBeforeModelCall AgentCallbackEvent = "before_model_call"
	// CallbackAfterModelCall LLM 响应后
	CallbackAfterModelCall AgentCallbackEvent = "after_model_call"
	// CallbackOnModelException LLM 调用异常
	CallbackOnModelException AgentCallbackEvent = "on_model_exception"
	// CallbackBeforeToolCall 工具执行前
	CallbackBeforeToolCall AgentCallbackEvent = "before_tool_call"
	// CallbackAfterToolCall 工具执行后
	CallbackAfterToolCall AgentCallbackEvent = "after_tool_call"
	// CallbackOnToolException 工具执行异常
	CallbackOnToolException AgentCallbackEvent = "on_tool_exception"
)

// ──────────────────────────── 全局变量（事件分组） ────────────────────────────

// BaseEventMethodMap 基础事件→方法名映射（8个，不含 task-iteration）。
//
// 对齐 Python: EVENT_METHOD_MAP (openjiuwen/core/single_agent/rail/base.py L434-442)
// 注意：Python 的 EVENT_METHOD_MAP 含 10 个事件（含 task-iteration），
// Go 核心层将 10 个事件均定义在接口中，此映射仅用于 DeepAgentRail.get_callbacks 分层。
var BaseEventMethodMap = map[AgentCallbackEvent]string{
	CallbackBeforeInvoke:     "BeforeInvoke",
	CallbackAfterInvoke:      "AfterInvoke",
	CallbackBeforeModelCall:  "BeforeModelCall",
	CallbackAfterModelCall:   "AfterModelCall",
	CallbackOnModelException: "OnModelException",
	CallbackBeforeToolCall:   "BeforeToolCall",
	CallbackAfterToolCall:    "AfterToolCall",
	CallbackOnToolException:  "OnToolException",
}

// DeepEventMethodMap DeepAgent 扩展事件→方法名映射（2个 task-iteration hooks）。
//
// 对齐 Python: DEEP_EVENT_METHOD_MAP (openjiuwen/harness/rails/base.py L22-25)
var DeepEventMethodMap = map[AgentCallbackEvent]string{
	CallbackBeforeTaskIteration: "BeforeTaskIteration",
	CallbackAfterTaskIteration:  "AfterTaskIteration",
}

const (
	// RunKindNormal 正常运行
	RunKindNormal RunKind = "normal"
	// RunKindHeartbeat 心跳运行
	RunKindHeartbeat RunKind = "heartbeat"
	// RunKindCron 定时任务运行
	RunKindCron RunKind = "cron"
)

const (
	// HeartbeatReasonInterval 定时心跳
	HeartbeatReasonInterval HeartbeatReason = "interval"
	// HeartbeatReasonManual 手动触发心跳
	HeartbeatReasonManual HeartbeatReason = "manual"
)

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
	// steeringQueueSize steering 队列缓冲区大小
	steeringQueueSize = 4096
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrSteeringQueueFull steering 队列已满
var ErrSteeringQueueFull = errors.New("steering queue full")

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentCallbackContext 创建 AgentCallbackContext 实例。
//
// 对应 Python: AgentCallbackContext(agent=..., inputs=..., session=...)
func NewAgentCallbackContext(
	agent BaseAgent,
	inputs EventInputs,
	sess sessioninterfaces.SessionFacade,
) *AgentCallbackContext {
	return &AgentCallbackContext{
		agent:   agent,
		inputs:  inputs,
		session: sess,
		extra:   make(map[string]any),
	}
}

// Agent 返回当前 Agent 实例引用
func (c *AgentCallbackContext) Agent() BaseAgent { return c.agent }

// Event 返回当前回调事件类型
func (c *AgentCallbackContext) Event() AgentCallbackEvent { return c.event }

// SetEvent 设置当前回调事件类型
func (c *AgentCallbackContext) SetEvent(event AgentCallbackEvent) { c.event = event }

// Inputs 返回当前事件输入数据
func (c *AgentCallbackContext) Inputs() EventInputs { return c.inputs }

// SetInputs 设置当前事件输入数据
func (c *AgentCallbackContext) SetInputs(inputs EventInputs) { c.inputs = inputs }

// Config 返回运行时配置（最小化接口）
func (c *AgentCallbackContext) Config() railConfig { return c.config }

// SetConfig 设置运行时配置
func (c *AgentCallbackContext) SetConfig(config railConfig) { c.config = config }

// Session 返回当前 Session
func (c *AgentCallbackContext) Session() sessioninterfaces.SessionFacade { return c.session }

// ModelContext 返回当前 ModelContext
func (c *AgentCallbackContext) ModelContext() ceinterface.ModelContext { return c.modelContext }

// SetModelContext 设置 ModelContext
func (c *AgentCallbackContext) SetModelContext(mc ceinterface.ModelContext) { c.modelContext = mc }

// Extra 返回 extra 通信字典
func (c *AgentCallbackContext) Extra() map[string]any { return c.extra }

// Exception 返回异常对象
func (c *AgentCallbackContext) Exception() error { return c.exception }

// SetException 设置异常对象
func (c *AgentCallbackContext) SetException(err error) { c.exception = err }

// RetryAttempt 返回当前重试索引号
func (c *AgentCallbackContext) RetryAttempt() int { return c.retryAttempt }

// SetRetryAttempt 设置当前重试索引号
func (c *AgentCallbackContext) SetRetryAttempt(attempt int) { c.retryAttempt = attempt }

// BindSteeringQueue 绑定外部 steering 队列。
//
// 对应 Python: AgentCallbackContext.bind_steering_queue(queue)
func (c *AgentCallbackContext) BindSteeringQueue(q chan string) {
	c.steeringQueue = q
}

// PushSteering 非阻塞推送 steering 消息。
//
// 对应 Python: AgentCallbackContext.push_steering(msg)
func (c *AgentCallbackContext) PushSteering(msg string) error {
	if c.steeringQueue == nil {
		return nil
	}
	select {
	case c.steeringQueue <- msg:
		return nil
	default:
		logger.Warn(logComponent).
			Str("event_type", "steering_queue_full").
			Str("msg", msg).
			Msg("steering 队列已满")
		return ErrSteeringQueueFull
	}
}

// DrainSteering 非阻塞排空所有待处理 steering 消息。
//
// 对应 Python: AgentCallbackContext.drain_steering() -> List[str]
func (c *AgentCallbackContext) DrainSteering() []string {
	if c.steeringQueue == nil {
		return nil
	}
	var msgs []string
	for {
		select {
		case msg := <-c.steeringQueue:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

// HasPendingSteering 检查是否有待处理的 steering 消息。
//
// 对应 Python: AgentCallbackContext.has_pending_steering() -> bool
func (c *AgentCallbackContext) HasPendingSteering() bool {
	if c.steeringQueue == nil {
		return false
	}
	return len(c.steeringQueue) > 0
}

// SteeringQueue 返回绑定的 steering 队列。
//
// 对应 Python: AgentCallbackContext.steering_queue 属性
func (c *AgentCallbackContext) SteeringQueue() chan string {
	return c.steeringQueue
}

// FireLifecycle 触发 before/after 事件对的生命周期包装。
//
// 对齐 Python: AgentCallbackContext.lifecycle() async context manager
func (c *AgentCallbackContext) FireLifecycle(
	ctx context.Context,
	before, after AgentCallbackEvent,
	fn func() error,
) error {
	savedInputs := c.inputs

	if err := c.Fire(before); err != nil {
		return err
	}

	var origErr error
	err := fn()
	if err != nil {
		origErr = err
		c.exception = err
	}

	c.inputs = savedInputs

	// context 已取消 → 跳过 after 事件（对齐 Python CancelledError 保护）
	if ctx.Err() != nil {
		if origErr != nil {
			return origErr
		}
		return nil
	}

	// 触发 after 钩子，对齐 Python lifecycle() 错误处理
	afterErr := c.Fire(after)
	if afterErr != nil {
		if origErr != nil {
			// after 回调出错但有原始异常 → log 不掩盖
			logger.Error(logger.ComponentAgentCore).
				Str("event", string(after)).
				Err(afterErr).
				Msg("after 回调出错，掩盖原始异常")
			return origErr
		}
		// after 回调出错且无原始异常 → 返回 after 错误
		return afterErr
	}

	if origErr != nil {
		return origErr
	}
	return nil
}

// Fire 触发回调事件。
//
// 对应 Python: AgentCallbackContext.fire(event)
func (c *AgentCallbackContext) Fire(event AgentCallbackEvent) error {
	c.event = event
	if c.agent == nil {
		return nil
	}
	manager := c.agent.CallbackManager()
	if manager == nil {
		return nil
	}
	return manager.Execute(context.Background(), event, c)
}

// RequestRetry 请求重试。
//
// 对应 Python: AgentCallbackContext.request_retry(delay_seconds)
func (c *AgentCallbackContext) RequestRetry(delaySeconds float64) {
	if delaySeconds < 0 {
		delaySeconds = 0
	}
	c.retryRequest = &RetryRequest{DelaySeconds: delaySeconds}
}

// ConsumeRetryRequest 消费重试请求（一次性）。
//
// 对应 Python: AgentCallbackContext.consume_retry_request()
func (c *AgentCallbackContext) ConsumeRetryRequest() *RetryRequest {
	req := c.retryRequest
	c.retryRequest = nil
	return req
}

// RequestForceFinish 请求提前终止。
//
// 对应 Python: AgentCallbackContext.request_force_finish(result)
func (c *AgentCallbackContext) RequestForceFinish(result map[string]any) {
	c.forceFinishRequest = &ForceFinishRequest{Result: result}
}

// ConsumeForceFinish 消费提前终止请求（一次性）。
//
// 对应 Python: AgentCallbackContext.consume_force_finish()
func (c *AgentCallbackContext) ConsumeForceFinish() *ForceFinishRequest {
	req := c.forceFinishRequest
	c.forceFinishRequest = nil
	return req
}

// HasForceFinishRequest 检查是否有待处理的提前终止请求。
func (c *AgentCallbackContext) HasForceFinishRequest() bool {
	return c.forceFinishRequest != nil
}

// ForkForToolCall 为单个工具调用创建隔离的子上下文。
//
// 对应 Python: AbilityManager.execute 中 tool_ctx = AgentCallbackContext(...)
func (c *AgentCallbackContext) ForkForToolCall(toolCall *llmschema.ToolCall) *AgentCallbackContext {
	return &AgentCallbackContext{
		agent: c.agent,
		inputs: &ToolCallInputs{
			ToolCall: toolCall,
			ToolName: toolCall.Name,
			ToolArgs: toolCall.Arguments,
		},
		config:        c.config,
		session:       c.session,
		modelContext:  c.modelContext,
		extra:         c.extra,
		steeringQueue: c.steeringQueue,
	}
}

// NewAgentCallbackManager 创建回调管理器。
func NewAgentCallbackManager(agentID string) *AgentCallbackManager {
	return &AgentCallbackManager{agentID: agentID}
}

// RegisterCallback 注册回调。
//
// 对应 Python: AgentCallbackManager.register_callback(event, callback, priority)
func (m *AgentCallbackManager) RegisterCallback(ctx context.Context, event AgentCallbackEvent, fn cb.PerAgentCallbackFunc, opts ...cb.CallbackOption) {
	agentEvent := m.getAgentEvent(event)
	cb.GetCallbackFramework().OnPerAgent(agentEvent, fn, opts...)
}

// RegisterRail 批量注册一个 Rail 实例的所有回调。
//
// 对应 Python: AgentCallbackManager.register_rail(rail)
func (m *AgentCallbackManager) RegisterRail(ctx context.Context, r AgentRail, opts ...cb.CallbackOption) error {
	callbacks := r.GetCallbacks()
	priorityOpt := cb.WithPriority(r.Priority())
	allOpts := append([]cb.CallbackOption{priorityOpt}, opts...)
	for event, fn := range callbacks {
		m.RegisterCallback(ctx, event, fn, allOpts...)
		logger.Debug(logComponent).
			Str("event_type", "rail_register_callback").
			Str("event", string(event)).
			Int("priority", r.Priority()).
			Msg("Rail 钩子注册到回调框架")
	}
	return nil
}

// UnregisterRail 批量注销一个 Rail 实例的所有回调。
//
// 对应 Python: AgentCallbackManager.unregister_rail(rail)
func (m *AgentCallbackManager) UnregisterRail(_ context.Context, r AgentRail) error {
	callbacks := r.GetCallbacks()
	for event, fn := range callbacks {
		m.Unregister(event, fn)
		logger.Debug(logComponent).
			Str("event_type", "rail_unregister_callback").
			Str("event", string(event)).
			Msg("Rail 钩子从回调框架注销")
	}
	return nil
}

// Unregister 注销指定事件上的单个回调。
func (m *AgentCallbackManager) Unregister(event AgentCallbackEvent, fn cb.PerAgentCallbackFunc) {
	agentEvent := m.getAgentEvent(event)
	cb.GetCallbackFramework().OffPerAgent(agentEvent, fn)
}

// Clear 清除回调。
func (m *AgentCallbackManager) Clear(events ...AgentCallbackEvent) {
	fw := cb.GetCallbackFramework()
	if len(events) == 0 {
		for _, e := range AllCallbackEvents() {
			fw.OffAllPerAgent(m.getAgentEvent(e))
		}
		return
	}
	for _, e := range events {
		fw.OffAllPerAgent(m.getAgentEvent(e))
	}
}

// HasHooks 检查指定事件是否有已注册的回调。
func (m *AgentCallbackManager) HasHooks(event AgentCallbackEvent) bool {
	agentEvent := m.getAgentEvent(event)
	return cb.GetCallbackFramework().HasPerAgentHooks(agentEvent)
}

// Execute 触发指定事件的所有回调。
//
// 对应 Python: AgentCallbackManager.execute(event, ctx)
func (m *AgentCallbackManager) Execute(ctx context.Context, event AgentCallbackEvent, railCtx *AgentCallbackContext) error {
	agentEvent := m.getAgentEvent(event)
	return cb.GetCallbackFramework().TriggerPerAgent(ctx, agentEvent, railCtx)
}

// NewBaseRail 创建默认优先级(50)的 BaseRail。
func NewBaseRail() *BaseRail {
	return &BaseRail{priority: 50}
}

// Priority 返回优先级。
func (r *BaseRail) Priority() int {
	return r.priority
}

// WithPriority 设置优先级（Functional Options 模式）。
func (r *BaseRail) WithPriority(p int) *BaseRail {
	r.priority = p
	return r
}

// Init 默认 no-op。
func (r *BaseRail) Init(_ BaseAgent) error { return nil }

// Uninit 默认 no-op。
func (r *BaseRail) Uninit(_ BaseAgent) error { return nil }

// BeforeInvoke 默认 no-op。
func (r *BaseRail) BeforeInvoke(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterInvoke 默认 no-op。
func (r *BaseRail) AfterInvoke(_ context.Context, _ *AgentCallbackContext) error { return nil }

// BeforeTaskIteration 默认 no-op。
func (r *BaseRail) BeforeTaskIteration(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterTaskIteration 默认 no-op。
func (r *BaseRail) AfterTaskIteration(_ context.Context, _ *AgentCallbackContext) error { return nil }

// BeforeModelCall 默认 no-op。
func (r *BaseRail) BeforeModelCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterModelCall 默认 no-op。
func (r *BaseRail) AfterModelCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// OnModelException 默认 no-op。
func (r *BaseRail) OnModelException(_ context.Context, _ *AgentCallbackContext) error { return nil }

// BeforeToolCall 默认 no-op。
func (r *BaseRail) BeforeToolCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterToolCall 默认 no-op。
func (r *BaseRail) AfterToolCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// OnToolException 默认 no-op。
func (r *BaseRail) OnToolException(_ context.Context, _ *AgentCallbackContext) error { return nil }

// GetCallbacks 返回空映射（默认无钩子覆盖）。
func (r *BaseRail) GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return make(map[AgentCallbackEvent]cb.PerAgentCallbackFunc)
}

// CallbackFrom 创建一条事件→回调映射条目。
func (r *BaseRail) CallbackFrom(event AgentCallbackEvent, fn cb.PerAgentCallbackFunc) callbackEntry {
	return callbackEntry{event: event, fn: fn}
}

// BuildCallbacks 从多条映射条目构建 GetCallbacks 返回值。
func (r *BaseRail) BuildCallbacks(entries ...callbackEntry) map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
	m := make(map[AgentCallbackEvent]cb.PerAgentCallbackFunc, len(entries))
	for _, e := range entries {
		m[e.event] = e.fn
	}
	return m
}

// AllCallbackEvents 返回所有 AgentCallbackEvent 枚举值。
func AllCallbackEvents() []AgentCallbackEvent {
	return []AgentCallbackEvent{
		CallbackBeforeInvoke,
		CallbackAfterInvoke,
		CallbackBeforeTaskIteration,
		CallbackAfterTaskIteration,
		CallbackBeforeModelCall,
		CallbackAfterModelCall,
		CallbackOnModelException,
		CallbackBeforeToolCall,
		CallbackAfterToolCall,
		CallbackOnToolException,
	}
}

// AllBaseCallbackEvents 返回 8 个基础回调事件（不含 task-iteration）。
func AllBaseCallbackEvents() []AgentCallbackEvent {
	return []AgentCallbackEvent{
		CallbackBeforeInvoke,
		CallbackAfterInvoke,
		CallbackBeforeModelCall,
		CallbackAfterModelCall,
		CallbackOnModelException,
		CallbackBeforeToolCall,
		CallbackAfterToolCall,
		CallbackOnToolException,
	}
}

// AllDeepCallbackEvents 返回 2 个 DeepAgent 扩展回调事件（task-iteration）。
func AllDeepCallbackEvents() []AgentCallbackEvent {
	return []AgentCallbackEvent{
		CallbackBeforeTaskIteration,
		CallbackAfterTaskIteration,
	}
}

// String 实现 fmt.Stringer 接口。
func (e AgentCallbackEvent) String() string {
	return string(e)
}

// GoString 实现 fmt.GoStringer 接口。
func (e AgentCallbackEvent) GoString() string {
	return fmt.Sprintf("interfaces.AgentCallbackEvent(%q)", string(e))
}

// NewRunContext 创建 RunContext 实例。
func NewRunContext() *RunContext {
	return &RunContext{Extra: make(map[string]any)}
}

// NewInvokeQueryString 从字符串构造 InvokeQuery。
func NewInvokeQueryString(s string) InvokeQuery {
	return InvokeQueryString(s)
}

// QueryFromInputs 从 inputs map 中提取 query 字段，返回 InvokeQuery 接口。
func QueryFromInputs(inputs map[string]any) InvokeQuery {
	q := inputs["query"]
	if q == nil {
		return NewInvokeQueryString("")
	}
	if ii, ok := q.(*interaction.InteractiveInput); ok {
		return ii
	}
	if qs, ok := q.(InvokeQueryString); ok {
		return qs
	}
	if s, ok := q.(string); ok {
		return NewInvokeQueryString(s)
	}
	return NewInvokeQueryString(fmt.Sprintf("%v", q))
}

// NewMapInputs 创建 MapInputs 实例。
func NewMapInputs() *MapInputs {
	return &MapInputs{Data: make(map[string]any)}
}

// IsInteractiveInput 实现 InvokeQuery 接口，普通字符串查询始终返回 false。
func (q InvokeQueryString) IsInteractiveInput() bool { return false }

// PlainText 实现 InvokeQuery 接口，返回字符串本身。
func (q InvokeQueryString) PlainText() string { return string(q) }

// IsHeartbeat 检查是否为心跳运行。
func (i *InvokeInputs) IsHeartbeat() bool {
	return i.RunKind == RunKindHeartbeat
}

// IsLightweightContext 检查是否启用轻量上下文模式。
func (i *InvokeInputs) IsLightweightContext() bool {
	if i.RunContext != nil && i.RunContext.ContextMode != "" {
		return i.RunContext.ContextMode == "lightweight"
	}
	return false
}

// IsCron 检查是否为定时任务运行。
func (i *InvokeInputs) IsCron() bool {
	return i.RunKind == RunKindCron
}

// EventKind 实现 EventInputs 接口
func (i *InvokeInputs) EventKind() string { return "invoke" }

// EventKind 实现 EventInputs 接口
func (i *ModelCallInputs) EventKind() string { return "model_call" }

// EventKind 实现 EventInputs 接口
func (i *ToolCallInputs) EventKind() string { return "tool_call" }

// EventKind 实现 EventInputs 接口
func (i *TaskIterationInputs) EventKind() string { return "task_iteration" }

// EventKind 实现 EventInputs 接口
func (m *MapInputs) EventKind() string { return "map" }

// ──────────────────────────── 非导出函数 ────────────────────────────

// getAgentEvent 生成带 agentID 前缀的事件名。
func (m *AgentCallbackManager) getAgentEvent(event AgentCallbackEvent) string {
	return m.agentID + "_" + string(event)
}
