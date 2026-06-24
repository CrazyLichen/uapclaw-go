package interfaces

import (
	"context"

	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentConfig Agent 配置接口，所有 Agent 配置必须实现。
//
// 定义所有 Agent 子类共有的配置访问方法，
// ReActAgentConfig、ControllerAgentConfig 等具体配置均实现此接口。
// 包含模型名称、内存作用域、上下文引擎配置、模型客户端配置四个核心访问方法。
//
// 对应 Python: BaseAgent.config 属性（无类型约束，子类各自持有具体 config 类型）
type AgentConfig interface {
	// ModelName 返回模型名称
	ModelName() string
	// MemScopeID 返回内存作用域标识
	MemScopeID() string
	// GetContextEngineConfig 返回上下文引擎配置
	GetContextEngineConfig() ceschema.ContextEngineConfig
	// GetModelClientConfig 返回模型客户端配置（可能为 nil）
	GetModelClientConfig() *llmschema.ModelClientConfig
	// Validate 校验配置的合法性
	Validate() error
}

// Workflow 工作流执行接口（最小定义，领域八扩展）。
//
// 对应 Python: openjiuwen/core/workflow/workflow.py (Workflow)
// Python 的 Workflow 有 invoke/stream/card 三个能力，
// Go 当前定义 Invoke/Stream/Card 三个方法，对齐 Python。
// Invoke 返回值暂用 (any, error)，领域八扩展为 (*WorkflowOutput, error)。
type Workflow interface {
	// Invoke 非流式调用工作流
	//
	// 对应 Python: Workflow.invoke(inputs, session, context, **kwargs) -> WorkflowOutput
	Invoke(ctx context.Context, inputs map[string]any, opts ...WorkflowOption) (any, error)
	// Stream 流式调用工作流
	//
	// 对应 Python: Workflow.stream(inputs, session, context, stream_modes, **kwargs) -> AsyncIterator[WorkflowChunk]
	// 返回 channel 中的 stream.Schema 对应 Python 的 WorkflowChunk = Union[OutputSchema, CustomSchema, TraceSchema]。
	Stream(ctx context.Context, inputs map[string]any, opts ...WorkflowOption) (<-chan stream.Schema, error)
	// Card 返回工作流配置卡片
	//
	// 对应 Python: Workflow.card 属性（@property）
	// 用于 tracer 装饰器提取 instanceInfo.metadata（id/name/description/version）。
	Card() *schema.WorkflowCard
}

// BaseAgent Agent 执行的核心行为契约。
//
// 对应 Python: openjiuwen/core/single_agent/base.py (BaseAgent)
//
// 设计原则：
//   - Card is required（定义 Agent 是什么）
//   - Config is optional（定义 Agent 怎么运行）
//   - 所有子类（ReActAgent/ControllerAgent）实现此接口
type BaseAgent interface {
	// ── 核心三方法 ──

	// Configure 配置 Agent。
	// 对应 Python: BaseAgent.configure(config)
	Configure(ctx context.Context, config AgentConfig) error

	// Invoke 非流式调用 Agent。
	// 对应 Python: BaseAgent.invoke(inputs, session)
	Invoke(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error)

	// Stream 流式调用 Agent。
	// 对应 Python: BaseAgent.stream(inputs, session, stream_modes)
	Stream(ctx context.Context, inputs map[string]any, opts ...AgentOption) (<-chan stream.Schema, error)

	// ── 访问器 ──

	// Card 返回 Agent 身份卡片。
	// 对应 Python: BaseAgent.card 属性
	Card() *agentschema.AgentCard

	// Config 返回当前配置。
	// 对应 Python: BaseAgent.config 属性
	Config() AgentConfig

	// AbilityManager 返回能力管理器。
	// 对应 Python: BaseAgent.ability_manager 属性
	// 返回 any，因为 interfaces → ability → resource → interfaces 构成循环依赖，
	// 无法用具体类型。调用方通过类型断言获取 *ability.AbilityManager。
	AbilityManager() any

	// CallbackManager 返回回调管理器。
	// 对应 Python: BaseAgent.agent_callback_manager 属性
	// 通过 rail 包内定义 RailAgent 最小接口打破循环依赖，返回具体类型。
	CallbackManager() *rail.AgentCallbackManager

	// ── 回调/Rail 注册 ──

	// RegisterCallback 注册回调。
	// 对应 Python: BaseAgent.register_callback(event, callback, priority)
	// event 实际类型 rail.AgentCallbackEvent，callback 实际类型 cb.PerAgentCallbackFunc，
	// 用 any 避免循环依赖，委托给 AgentCallbackManager.RegisterCallback。
	RegisterCallback(ctx context.Context, event any, fn any, opts ...cb.CallbackOption) error

	// RegisterRail 注册 Rail。
	// 对应 Python: BaseAgent.register_rail(rail)
	RegisterRail(ctx context.Context, rail rail.AgentRail, opts ...cb.CallbackOption) error

	// UnregisterRail 注销 Rail。
	// 对应 Python: BaseAgent.unregister_rail(rail)
	UnregisterRail(ctx context.Context, rail rail.AgentRail) error
}

// WorkflowOptions 工作流执行选项。
type WorkflowOptions struct {
	// Session 工作流会话（对齐 Python workflow.invoke(inputs, session=...)）
	Session *session.WorkflowSession
	// Context 模型上下文，待领域八具体化（对齐 Python workflow.invoke(inputs, context=...)）
	Context any
}

// AgentOptions Agent 调用选项。
type AgentOptions struct {
	// Session 会话实例（可选）
	// 对应 Python: invoke(inputs, session) / stream(inputs, session, stream_modes) 的 session 参数
	Session *session.Session
	// StreamModes 流式输出模式（可选）
	// 对应 Python: stream(inputs, session, stream_modes) 的 stream_modes 参数
	StreamModes []stream.StreamMode
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorkflowOption 工作流执行选项函数（预留，领域八扩展）。
type WorkflowOption func(*WorkflowOptions)

// AgentOption Agent 调用选项函数。
type AgentOption func(*AgentOptions)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithSession 设置会话实例。
func WithSession(sess *session.Session) AgentOption {
	return func(o *AgentOptions) { o.Session = sess }
}

// WithStreamModes 设置流式输出模式。
func WithStreamModes(modes []stream.StreamMode) AgentOption {
	return func(o *AgentOptions) { o.StreamModes = modes }
}

// NewAgentOptions 从选项列表构建 AgentOptions。
func NewAgentOptions(opts ...AgentOption) *AgentOptions {
	o := &AgentOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithWorkflowSession 设置工作流会话。
func WithWorkflowSession(sess *session.WorkflowSession) WorkflowOption {
	return func(o *WorkflowOptions) { o.Session = sess }
}

// WithWorkflowContext 设置模型上下文。
func WithWorkflowContext(ctx any) WorkflowOption {
	return func(o *WorkflowOptions) { o.Context = ctx }
}

// NewWorkflowOptions 从选项列表构建 WorkflowOptions。
func NewWorkflowOptions(opts ...WorkflowOption) *WorkflowOptions {
	o := &WorkflowOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// ──────────────────────────── 非导出函数 ────────────────────────────
