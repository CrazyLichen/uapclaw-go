package rail

import (
	"context"
	"errors"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RailAgent Rail 包所需的最小 Agent 接口。
//
// 在 rail 包内定义，打破 rail → interfaces 循环依赖，
// 使 AgentCallbackContext 可以直接访问 CallbackManager 具体类型，
// Fire() 无需类型断言。
// interfaces.BaseAgent 隐式满足此接口。
//
// 对应 Python: BaseAgent (openjiuwen/core/single_agent/base.py)
type RailAgent interface {
	// CallbackManager 返回 PerAgent 回调管理器
	CallbackManager() *AgentCallbackManager
	// AgentID 返回 Agent 唯一标识
	// ⤴️ 6.7 定义；BaseAgent 通过 Card().ID 隐式满足
	AgentID() string
	// ⤵️ 后续 Rail 子类实现时按需扩充：
	// AbilityManager() — 工具注册/注销（MemoryRail, SkillUseRail 等需要）
	// SystemPromptBuilder() — 系统提示词构建器（多数 Rail init 中需要）
	// Card() — Agent 元数据（agent.card.id 等场景）
	// DeepConfig() — 深层配置（HeartbeatRail 等需要）
}

// railConfig Rail 包所需的最小 Config 接口。
//
// 预留接口，当前无方法。未来 Rail 需要访问配置时在此添加方法，
// 避免 rail → interfaces 循环依赖。
type railConfig interface{}

// AgentCallbackContext Rail 系统与 Agent 运行时之间的核心中介对象。
//
// 承载三个控制机制：Retry（重试）、Force Finish（提前终止）、Steering（外部注入）。
// 在 ReAct 循环中创建，跨事件生命周期持久存在（extra 字段）。
//
// 对应 Python: openjiuwen/core/single_agent/rail/base.py AgentCallbackContext (L226-416)
type AgentCallbackContext struct {
	// agent 当前 Agent 实例引用（最小化接口，仅暴露 CallbackManager）
	agent RailAgent
	// event 当前回调事件类型（由 Fire 设置）
	event AgentCallbackEvent
	// inputs 当前事件的输入数据（随事件变化）
	inputs EventInputs
	// config 运行时配置（最小化接口，预留）
	config railConfig
	// session 当前 Session
	session *session.Session
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
	// steeringQueueSize steering 队列缓冲区大小
	// Python 用无界 asyncio.Queue，Go 用大容量 buffered chan 对齐
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
	agent RailAgent,
	inputs EventInputs,
	sess *session.Session,
) *AgentCallbackContext {
	return &AgentCallbackContext{
		agent:   agent,
		inputs:  inputs,
		session: sess,
		extra:   make(map[string]any),
	}
}

// Agent 返回当前 Agent 实例引用（最小化接口）
func (c *AgentCallbackContext) Agent() RailAgent { return c.agent }

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
func (c *AgentCallbackContext) Session() *session.Session { return c.session }

// ModelContext 返回当前 ModelContext
func (c *AgentCallbackContext) ModelContext() ceinterface.ModelContext { return c.modelContext }

// SetModelContext 设置当前 ModelContext
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
// 无队列时 no-op，队列满时返回 ErrSteeringQueueFull（对齐 Python QueueFull 异常）。
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
// 差异：Python 用 async with，Go 用函数 + defer
//
// 流程：
//  1. 保存 inputs
//  2. fire(before)      ← 6.6 回填
//  3. 执行 fn()
//  4. finally: 恢复 inputs → fire(after)  ← 6.6 回填
//
// 异常处理：
//   - fn() 出错时设置 ctx.exception
//   - after 回调出错时：如有原始异常则 log 不掩盖，否则 re-raise
func (c *AgentCallbackContext) FireLifecycle(
	before, after AgentCallbackEvent,
	fn func() error,
) error {
	savedInputs := c.inputs

	// 2. fire(before)
	if err := c.Fire(before); err != nil {
		return err
	}

	var origErr error
	err := fn()
	if err != nil {
		origErr = err
		c.exception = err
	}

	// finally: 恢复 inputs + fire(after)
	c.inputs = savedInputs
	_ = c.Fire(after) // 异常安全：忽略 after 阶段的错误

	if origErr != nil {
		return origErr
	}
	return nil
}

// Fire 触发回调事件。
//
// 对应 Python: AgentCallbackContext.fire(event)
// 通过 RailAgent 最小接口直接访问 CallbackManager，无需类型断言。
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
// 在 on_model_exception / on_tool_exception 钩子内调用。
// 负数 delaySeconds 被静默归零（对齐 Python: if delay_seconds < 0: delay_seconds = 0.0）。
// 对应 Python: AgentCallbackContext.request_retry(delay_seconds)
func (c *AgentCallbackContext) RequestRetry(delaySeconds float64) {
	if delaySeconds < 0 {
		delaySeconds = 0
	}
	c.retryRequest = &RetryRequest{DelaySeconds: delaySeconds}
}

// ConsumeRetryRequest 消费重试请求（一次性）。
//
// 返回并清除待处理的 RetryRequest（read-and-clear 模式）。
// 对应 Python: AgentCallbackContext.consume_retry_request() -> Optional[RetryRequest]
func (c *AgentCallbackContext) ConsumeRetryRequest() *RetryRequest {
	req := c.retryRequest
	c.retryRequest = nil
	return req
}

// RequestForceFinish 请求提前终止。
//
// 在任何钩子中调用（如 before_model_call、after_tool_call）。
// 对应 Python: AgentCallbackContext.request_force_finish(result)
func (c *AgentCallbackContext) RequestForceFinish(result map[string]any) {
	c.forceFinishRequest = &ForceFinishRequest{Result: result}
}

// ConsumeForceFinish 消费提前终止请求（一次性）。
//
// 返回并清除待处理的 ForceFinishRequest（read-and-clear 模式）。
// 对应 Python: AgentCallbackContext.consume_force_finish() -> Optional[ForceFinishRequest]
func (c *AgentCallbackContext) ConsumeForceFinish() *ForceFinishRequest {
	req := c.forceFinishRequest
	c.forceFinishRequest = nil
	return req
}

// HasForceFinishRequest 检查是否有待处理的提前终止请求。
//
// 对应 Python: AgentCallbackContext.has_force_finish_request -> bool
func (c *AgentCallbackContext) HasForceFinishRequest() bool {
	return c.forceFinishRequest != nil
}

// ForkForToolCall 为单个工具调用创建隔离的子上下文。
//
// 共享字段（引用共享，跨 rail 通信）：
//   - agent、extra、steeringQueue、session、config、modelContext
//
// 独立字段（每个工具调用各自持有零值）：
//   - retryRequest、forceFinishRequest、exception、retryAttempt、event、inputs
//
// 对应 Python: AbilityManager.execute 中 tool_ctx = AgentCallbackContext(
//
//	agent=ctx.agent, inputs=ToolCallInputs(...), config=ctx.config,
//	session=session, context=ctx.context, extra=ctx.extra,
//
// )
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

// ──────────────────────────── 非导出函数 ────────────────────────────
