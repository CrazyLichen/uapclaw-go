package context_engineer

import (
	"context"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextProcessorRail 上下文处理器护栏，配置和注入上下文引擎处理器。
//
// 在 Init 中读取 agent.react_agent._config 的 context_processors 列表，
// 将 preset 默认处理器与用户处理器合并后写入配置。
//
// 在 BeforeInvoke 和 OnModelException 中修复不完整的 tool_call/ToolMessage 配对。
//
// 在 BeforeModelCall 中刷新任务状态并注入 offload 提示节。
//
// 在 AfterModelCall 中刷新任务状态并调度会话记忆更新。
//
// 对齐 Python: ContextProcessorRail (openjiuwen/harness/rails/context_engineer/context_processor_rail.py)
type ContextProcessorRail struct {
	rails.DeepAgentRail
	// preset 是否启用预设默认处理器配置
	preset bool
	// userProcessors 用户自定义处理器规格列表
	userProcessors []ceiface.ProcessorSpec
	// sessionMemoryEnabled 是否启用会话记忆
	sessionMemoryEnabled bool
	// sessionMemoryConfig 会话记忆配置（预留，后续回填）
	sessionMemoryConfig interface{}
	// sessionMemoryMgr 会话记忆管理器（预留，后续回填）
	sessionMemoryMgr interface{}
	// systemPromptBuilder 系统提示词构建器引用
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// allProcessors 合并后的完整处理器列表
	allProcessors []ceiface.ProcessorSpec
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// contextProcessorRailPriority ContextProcessorRail 优先级
	// 对齐 Python: ContextProcessorRail.priority = 85
	contextProcessorRailPriority = 85
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextProcessorRail 创建 ContextProcessorRail 实例。
//
// 对齐 Python: ContextProcessorRail(processors=..., preset=True, session_memory=...)
func NewContextProcessorRail(opts ...ContextProcessorRailOption) *ContextProcessorRail {
	r := &ContextProcessorRail{
		preset: true,
	}
	for _, opt := range opts {
		opt(r)
	}
	r.WithPriority(contextProcessorRailPriority)
	return r
}

// Init Rail 初始化钩子：注入/合并处理器到 agent.react_agent._config.context_processors。
//
// 对齐 Python: ContextProcessorRail.init(agent)
func (r *ContextProcessorRail) Init(agent sainterfaces.BaseAgent) error {
	config := getReactAgentConfig(agent)
	if config == nil {
		return nil
	}

	modelConfig := config.ModelRequestConfig
	modelClientConfig := config.ModelClientConfig

	var baseProcessors []ceiface.ProcessorSpec
	if r.preset {
		baseProcessors = r.buildPresetProcessors(modelConfig, modelClientConfig)
	}

	allProcessors := MergeProcessors(
		baseProcessors,
		r.userProcessors,
		modelConfig,
		modelClientConfig,
	)

	config.ContextProcessors = allProcessors
	r.allProcessors = allProcessors

	// 获取 systemPromptBuilder 引用
	// 对齐 Python: self._system_prompt_builder = getattr(agent, "system_prompt_builder", None)
	r.systemPromptBuilder = getSystemPromptBuilder(agent)

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "context_processor_rail_init").
		Int("processors_count", len(allProcessors)).
		Bool("preset", r.preset).
		Msg("ContextProcessorRail 初始化完成")

	return nil
}

// Uninit Rail 注销钩子：清除处理器和 offload 节。
//
// 对齐 Python: ContextProcessorRail.uninit(agent)
func (r *ContextProcessorRail) Uninit(agent sainterfaces.BaseAgent) error {
	// 关闭会话记忆管理器（预留）
	// TODO: 后续回填 session memory manager shutdown

	config := getReactAgentConfig(agent)
	if config != nil {
		config.ContextProcessors = nil
	}

	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection("offload")
	}
	r.allProcessors = nil

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "context_processor_rail_uninit").
		Msg("ContextProcessorRail 注销完成")

	return nil
}

// BeforeInvoke invoke 开始前：修复不完整的工具上下文。
//
// 对齐 Python: ContextProcessorRail.before_invoke(ctx)
func (r *ContextProcessorRail) BeforeInvoke(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	FixIncompleteToolContext(ctx, cbc)
	return nil
}

// BeforeModelCall LLM 调用前：刷新任务状态 + 注入 offload 节。
//
// 对齐 Python: ContextProcessorRail.before_model_call(ctx)
func (r *ContextProcessorRail) BeforeModelCall(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	RefreshTaskStateRuntime(cbc)
	r.maybeInjectOffloadSection()
	return nil
}

// AfterModelCall LLM 响应后：刷新任务状态 + 调度会话记忆更新。
//
// 对齐 Python: ContextProcessorRail.after_model_call(ctx)
func (r *ContextProcessorRail) AfterModelCall(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	RefreshTaskStateRuntime(cbc)
	// TODO: 后续回填 session memory update_inherited_system_prompt
	// TODO: 后续回填 session memory maybe_schedule_update
	return nil
}

// AfterToolCall 工具执行后：刷新任务状态。
//
// 对齐 Python: ContextProcessorRail.after_tool_call(ctx)
func (r *ContextProcessorRail) AfterToolCall(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	RefreshTaskStateRuntime(cbc)
	return nil
}

// OnModelException LLM 调用异常：刷新任务状态 + 修复工具上下文。
//
// 对齐 Python: ContextProcessorRail.on_model_exception(ctx)
func (r *ContextProcessorRail) OnModelException(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	RefreshTaskStateRuntime(cbc)
	FixIncompleteToolContext(ctx, cbc)
	return nil
}

// GetCallbacks 返回已覆盖的钩子映射。
func (r *ContextProcessorRail) GetCallbacks() map[sainterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	// 覆盖基础事件
	callbacks[sainterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*sainterfaces.AgentCallbackContext))
	}
	callbacks[sainterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*sainterfaces.AgentCallbackContext))
	}
	callbacks[sainterfaces.CallbackAfterModelCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterModelCall(ctx, railCtx.(*sainterfaces.AgentCallbackContext))
	}
	callbacks[sainterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*sainterfaces.AgentCallbackContext))
	}
	callbacks[sainterfaces.CallbackOnModelException] = func(ctx context.Context, railCtx any) error {
		return r.OnModelException(ctx, railCtx.(*sainterfaces.AgentCallbackContext))
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildPresetProcessors 构建预设默认处理器列表。
//
// 对齐 Python: ContextProcessorRail._build_preset_processors(model_config, model_client_config)
// 根据 session_memory 是否启用选择不同的预设路径。
func (r *ContextProcessorRail) buildPresetProcessors(
	modelConfig *llmschema.ModelRequestConfig,
	modelClientConfig *llmschema.ModelClientConfig,
) []ceiface.ProcessorSpec {
	if r.sessionMemoryEnabled {
		// session memory 启用时的预设
		return []ceiface.ProcessorSpec{
			{Type: "ToolResultBudgetProcessor"},
			{Type: "MicroCompactProcessor"},
			{Type: "FullCompactProcessor"},
		}
	}

	// 默认预设（对齐 Python 非 session_memory 路径）
	// 注意：Go 中 ProcessorConfig 的具体配置由 ContextEngine 的 registry 根据类型创建默认值
	// 这里只指定 Type，Config 留空让 registry 填充默认配置
	return []ceiface.ProcessorSpec{
		{Type: "MessageSummaryOffloader"},
		{Type: "DialogueCompressor"},
		{Type: "CurrentRoundCompressor"},
		{Type: "RoundLevelCompressor"},
	}
}

// maybeInjectOffloadSection 如果配置了处理器，注入 offload 提示节。
//
// 对齐 Python: ContextProcessorRail._maybe_inject_offload_section()
func (r *ContextProcessorRail) maybeInjectOffloadSection() {
	if len(r.allProcessors) == 0 {
		if r.systemPromptBuilder != nil {
			r.systemPromptBuilder.RemoveSection("offload")
		}
		return
	}

	if r.systemPromptBuilder == nil {
		return
	}

	lang := "cn"
	if r.systemPromptBuilder.Language() != "" {
		lang = r.systemPromptBuilder.Language()
	}
	section := sections.BuildReloadSection(lang)
	r.systemPromptBuilder.AddSection(section)
}

// getReactAgentConfig 从 BaseAgent 获取 ReActAgentConfig。
//
// 对齐 Python: config = getattr(getattr(agent, "react_agent", None), "_config", None)
func getReactAgentConfig(agent sainterfaces.BaseAgent) *saconfig.ReActAgentConfig {
	if agent == nil {
		return nil
	}
	cfg := agent.Config()
	if cfg == nil {
		return nil
	}
	reactCfg, ok := cfg.(*saconfig.ReActAgentConfig)
	if !ok {
		return nil
	}
	return reactCfg
}

// getSystemPromptBuilder 从 BaseAgent 获取 SystemPromptBuilder。
//
// 对齐 Python: self._system_prompt_builder = getattr(agent, "system_prompt_builder", None)
// Go 中 BaseAgent 接口没有直接暴露 system_prompt_builder，
// 但 DeepAgent 在 Init(agent) 时传入的 agent 实际上是 DeepAgent 自身，
// 它有 SystemPromptBuilder() 方法。
func getSystemPromptBuilder(agent sainterfaces.BaseAgent) saprompt.SystemPromptBuilderInterface {
	// 尝试通过类型断言获取 SystemPromptBuilder
	type promptBuilderProvider interface {
		SystemPromptBuilder() saprompt.SystemPromptBuilderInterface
	}
	if provider, ok := agent.(promptBuilderProvider); ok {
		return provider.SystemPromptBuilder()
	}
	return nil
}

// ──────────────────────────── 选项函数 ────────────────────────────

// ContextProcessorRailOption ContextProcessorRail 构造选项函数
type ContextProcessorRailOption func(*ContextProcessorRail)

// WithPreset 设置是否启用预设默认处理器
func WithPreset(preset bool) ContextProcessorRailOption {
	return func(r *ContextProcessorRail) { r.preset = preset }
}

// WithUserProcessors 设置用户自定义处理器规格列表
func WithUserProcessors(procs []ceiface.ProcessorSpec) ContextProcessorRailOption {
	return func(r *ContextProcessorRail) { r.userProcessors = procs }
}

// WithSessionMemoryEnabled 设置是否启用会话记忆
func WithSessionMemoryEnabled(enabled bool) ContextProcessorRailOption {
	return func(r *ContextProcessorRail) { r.sessionMemoryEnabled = enabled }
}
