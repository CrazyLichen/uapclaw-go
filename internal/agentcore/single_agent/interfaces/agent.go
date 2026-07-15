package interfaces

import (
	"context"

	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

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

type AgentOptions struct {
	// Session 会话实例（可选）
	// 对应 Python: invoke(inputs, session) / stream(inputs, session, stream_modes) 的 session 参数
	Session sessioninterfaces.SessionFacade
	// StreamModes 流式输出模式（可选）
	// 对应 Python: stream(inputs, session, stream_modes) 的 stream_modes 参数
	StreamModes []stream.StreamMode
}

type BaseAgent interface {
	// ── 核心三方法 ──

	// Configure 配置 Agent。
	// 对应 Python: BaseAgent.configure(config)
	Configure(ctx context.Context, config AgentConfig) error

	// Invoke 非流式调用 Agent。
	// 对应 Python: BaseAgent.invoke(inputs, session)
	Invoke(ctx context.Context, inputs map[string]any, opts ...AgentOption) (map[string]any, error)

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
	AbilityManager() AbilityManagerInterface

	// CallbackManager 返回回调管理器。
	// 对应 Python: BaseAgent.agent_callback_manager 属性
	CallbackManager() *AgentCallbackManager

	// SystemPromptBuilder 返回系统提示词构建器接口。
	//
	// 对齐 Python: agent.system_prompt_builder 属性（harness 层扩展）。
	// ReActAgent 和 DeepAgent 均实现此方法。
	SystemPromptBuilder() saprompt.SystemPromptBuilderInterface

	// ── 回调/Rail 注册 ──

	// RegisterCallback 注册回调。
	// 对应 Python: BaseAgent.register_callback(event, callback, priority)
	RegisterCallback(ctx context.Context, event AgentCallbackEvent, fn cb.PerAgentCallbackFunc, opts ...cb.CallbackOption) error

	// RegisterRail 注册 Rail。
	// 对应 Python: BaseAgent.register_rail(rail)
	RegisterRail(ctx context.Context, rail AgentRail, opts ...cb.CallbackOption) error

	// UnregisterRail 注销 Rail。
	// 对应 Python: BaseAgent.unregister_rail(rail)
	UnregisterRail(ctx context.Context, rail AgentRail) error
}

// ──────────────────────────── 枚举 ────────────────────────────

type AgentOption func(*AgentOptions)

// ──────────────────────────── 导出函数 ────────────────────────────

func WithSession(sess sessioninterfaces.SessionFacade) AgentOption {
	return func(o *AgentOptions) { o.Session = sess }
}

func WithStreamModes(modes []stream.StreamMode) AgentOption {
	return func(o *AgentOptions) { o.StreamModes = modes }
}

func NewAgentOptions(opts ...AgentOption) *AgentOptions {
	o := &AgentOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
