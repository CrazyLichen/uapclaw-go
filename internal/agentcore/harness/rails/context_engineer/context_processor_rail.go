package context_engineer

import (
	"context"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/compressor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/offloader"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextProcessorRail 上下文处理器护栏，配置和注入上下文引擎处理器。
// 在 Init 中读取 agent.react_agent._config 的 context_processors 列表，
// 将 preset 默认处理器与用户处理器合并后写入配置。
// 在 BeforeInvoke 和 OnModelException 中修复不完整的 tool_call/ToolMessage 配对。
// 在 BeforeModelCall 中刷新任务状态并注入 offload 提示节。
// 在 AfterModelCall 中刷新任务状态并调度会话记忆更新。
// Python: ContextProcessorRail (openjiuwen/harness/rails/context_engineer/context_processor_rail.py)
type ContextProcessorRail struct {
	rails.DeepAgentRail
	// preset 是否启用预设默认处理器配置
	preset bool
	// userProcessors 用户自定义处理器规格列表
	userProcessors []ceiface.ProcessorSpec
	// sessionMemoryEnabled 是否启用会话记忆
	sessionMemoryEnabled bool
	// sessionMemoryConfig 会话记忆配置
	sessionMemoryConfig *cecontext.SessionMemoryConfig
	// sessionMemoryMgr 会话记忆管理器
	sessionMemoryMgr *cecontext.SessionMemoryManager
	// systemPromptBuilder 系统提示词构建器引用
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// allProcessors 合并后的完整处理器列表
	allProcessors []ceiface.ProcessorSpec
}

// ──────────────────────────── 枚举 ────────────────────────────

// ContextProcessorRailOption ContextProcessorRail 构造选项函数
type ContextProcessorRailOption func(*ContextProcessorRail)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// contextProcessorRailPriority ContextProcessorRail 优先级
	// Python: ContextProcessorRail.priority = 85
	contextProcessorRailPriority = 85

	// 默认 preset 的处理器类型
	// Python: ContextProcessorRail._build_preset_processors (session_memory=False)
	defaultProcessorOffloader    = "MessageSummaryOffloader"
	defaultProcessorDialogue     = "DialogueCompressor"
	defaultProcessorCurrentRound = "CurrentRoundCompressor"
	defaultProcessorRoundLevel   = "RoundLevelCompressor"

	// session_memory preset 的处理器类型
	// Python: ContextProcessorRail._build_preset_processors (session_memory=True)
	sessionMemoryProcessorToolResultBudget = "ToolResultBudgetProcessor"
	sessionMemoryProcessorMicroCompact     = "MicroCompactProcessor"
	sessionMemoryProcessorFullCompact      = "FullCompactProcessor"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// allPresetProcessorTypes preset 覆盖的全部处理器类型集合。
// 用于区分"preset 内建类型"与"用户自定义处理器"：前者受 mode 激活集过滤，
// 后者始终保留。
var allPresetProcessorTypes = map[string]struct{}{
	defaultProcessorOffloader:              {},
	defaultProcessorDialogue:               {},
	defaultProcessorCurrentRound:           {},
	defaultProcessorRoundLevel:             {},
	sessionMemoryProcessorToolResultBudget: {},
	sessionMemoryProcessorMicroCompact:     {},
	sessionMemoryProcessorFullCompact:      {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextProcessorRail 创建 ContextProcessorRail 实例。
// Python: ContextProcessorRail(processors=..., preset=True, session_memory=...)
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
// Python: ContextProcessorRail.init(agent)
func (r *ContextProcessorRail) Init(_ context.Context, agent sainterfaces.BaseAgent) error {
	config := getReactAgentConfig(agent)
	if config == nil {
		return nil
	}

	modelConfig := config.ModelRequestConfig
	modelClientConfig := config.ModelClientConfig

	// 绑定 session memory 模型默认值（对齐 Python: self._session_memory_config.model = model_config; self._session_memory_mgr.bind_model_defaults()）
	if r.sessionMemoryConfig != nil && r.sessionMemoryMgr != nil {
		r.sessionMemoryConfig.SetModelDefaults(modelConfig, modelClientConfig)
		r.sessionMemoryMgr.BindModelDefaults(modelConfig, modelClientConfig)
	}

	var baseProcessors []ceiface.ProcessorSpec
	if r.preset {
		baseProcessors = r.buildPresetProcessors(modelConfig, modelClientConfig)
	}

	allProcessors, mergeErr := MergeProcessors(
		baseProcessors,
		r.userProcessors,
		modelConfig,
		modelClientConfig,
	)
	if mergeErr != nil {
		// Python 对应场景是 ValueError 被 _build_context_processor_rail 的 try/except
		// 捕获后降级；Go 在这里降级为"跳过该处理器、其余继续"，避免 panic 终止进程。
		logger.Warn(logComponent).
			Err(mergeErr).
			Msg("context processor merge partially failed, offending processors skipped")
	}

	// 激活集过滤：base 同时包含默认与 session_memory 两条分支的默认配置
	// （只为让 yaml 覆盖能命中类型），真正生效的集合由 session_memory 开关决定。
	if r.preset {
		allProcessors = r.filterActiveProcessors(allProcessors)
	}

	config.ContextProcessors = allProcessors
	r.allProcessors = allProcessors

	// 热重载跟随：把新配置与新处理器规格推给上下文引擎，由下一次 CreateContext
	// （用户轮次入口）应用到池内的老上下文。Python 无此行为——其 create_context
	// 命中池子时忽略 processors，老会话不会跟随（见 Go 侧增强说明）。
	pushRuntimeConfig(agent, config.ContextEngineConfig, allProcessors)

	// 获取 systemPromptBuilder 引用
	// Python: self._system_prompt_builder = getattr(agent, "system_prompt_builder", None)
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	logger.Info(logComponent).
		Str("event_type", "context_processor_rail_init").
		Int("processors_count", len(allProcessors)).
		Bool("preset", r.preset).
		Msg("ContextProcessorRail initialized")

	return nil
}

// Uninit Rail 注销钩子：关闭会话记忆管理器，清除处理器和 offload 节。
// Python: ContextProcessorRail.uninit(agent)
func (r *ContextProcessorRail) Uninit(agent sainterfaces.BaseAgent) error {
	// 关闭会话记忆管理器（对齐 Python: if self._session_memory_mgr is not None: self._session_memory_mgr.shutdown()）
	if r.sessionMemoryMgr != nil {
		r.sessionMemoryMgr.Shutdown()
	}

	config := getReactAgentConfig(agent)
	if config != nil {
		config.ContextProcessors = nil
	}

	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection("offload")
	}
	r.allProcessors = nil

	logger.Info(logComponent).
		Str("event_type", "context_processor_rail_uninit").
		Msg("ContextProcessorRail unregistered")

	return nil
}

// BeforeInvoke invoke 开始前：修复不完整的工具上下文。
// Python: ContextProcessorRail.before_invoke(ctx)
func (r *ContextProcessorRail) BeforeInvoke(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	FixIncompleteToolContext(ctx, cbc)
	return nil
}

// BeforeModelCall LLM 调用前：刷新任务状态 + 注入 offload 节。
// Python: ContextProcessorRail.before_model_call(ctx)
func (r *ContextProcessorRail) BeforeModelCall(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	RefreshTaskStateRuntime(cbc)
	r.maybeInjectOffloadSection()
	return nil
}

// AfterModelCall LLM 响应后：刷新任务状态 + 调度会话记忆更新。
// Python: ContextProcessorRail.after_model_call(ctx)
func (r *ContextProcessorRail) AfterModelCall(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	RefreshTaskStateRuntime(cbc)

	// 会话记忆调度（对齐 Python: self._session_memory_mgr.update_inherited_system_prompt(ctx); await self._maybe_schedule_session_memory_update(ctx)）
	if r.sessionMemoryMgr != nil {
		// 提取系统提示词并设置到 updater（对齐 Python: update_inherited_system_prompt）
		if inputs := cbc.Inputs(); inputs != nil {
			if mcInputs, ok := inputs.(*sainterfaces.ModelCallInputs); ok && mcInputs != nil {
				r.sessionMemoryMgr.UpdateInheritedSystemPrompt(mcInputs.Messages)
			}
		}
		// 调度后台更新（对齐 Python: maybe_schedule_update）
		r.maybeScheduleSessionMemoryUpdate(ctx, cbc)
	}

	return nil
}

// AfterToolCall 工具执行后：刷新任务状态。
// Python: ContextProcessorRail.after_tool_call(ctx)
func (r *ContextProcessorRail) AfterToolCall(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	RefreshTaskStateRuntime(cbc)
	return nil
}

// OnModelException LLM 调用异常：刷新任务状态 + 修复工具上下文。
// Python: ContextProcessorRail.on_model_exception(ctx)
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

// WithSessionMemoryConfig 设置会话记忆配置。
// 对齐 Python: ContextProcessorRail(session_memory=SessionMemoryConfig | dict | None)
// 传入配置时自动启用 sessionMemoryEnabled。
func WithSessionMemoryConfig(cfg *cecontext.SessionMemoryConfig) ContextProcessorRailOption {
	return func(r *ContextProcessorRail) {
		if cfg != nil {
			r.sessionMemoryConfig = cfg
			r.sessionMemoryEnabled = true
			// 构造时立即初始化 SessionMemoryManager（对齐 Python: self._session_memory_mgr = SessionMemoryManager(self._session_memory_config)）
			r.sessionMemoryMgr = cecontext.NewSessionMemoryManager(*cfg)
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildPresetProcessors 构建"全量已知处理器的默认配置"（两条 preset 分支的并集）。
// Python: ContextProcessorRail._build_preset_processors(model_config, model_client_config)
// 根据 session_memory 是否启用选择不同的预设路径。Go 把两条分支都构造成 base，
// 目的是让 yaml 里任何已知 processor 段都能在 MergeProcessors 中命中类型并做字段覆盖；
// 合并后再由 filterActiveProcessors 按 session_memory 开关筛出真正生效的集合。
func (r *ContextProcessorRail) buildPresetProcessors(
	modelConfig *llmschema.ModelRequestConfig,
	modelClientConfig *llmschema.ModelClientConfig,
) []ceiface.ProcessorSpec {
	specs := make([]ceiface.ProcessorSpec, 0, 7)
	specs = append(specs, defaultPresetProcessors(modelConfig, modelClientConfig)...)
	specs = append(specs, sessionMemoryPresetProcessors(modelConfig, modelClientConfig)...)
	return specs
}

// filterActiveProcessors 按 session_memory 开关过滤激活集。
// 规则（对齐 Python 两条 preset 分支互斥）：
//   - session_memory 关闭 → 只保留默认 4 个处理器；
//   - session_memory 开启 → 只保留 ToolResultBudget/MicroCompact/FullCompact；
//   - 未知类型（用户自定义处理器）始终保留；
//   - 用户的 yaml 覆盖落在被过滤掉的 preset 上时打 Warn，避免"配置静默失效"。
func (r *ContextProcessorRail) filterActiveProcessors(specs []ceiface.ProcessorSpec) []ceiface.ProcessorSpec {
	active := map[string]struct{}{}
	if r.sessionMemoryEnabled {
		active[sessionMemoryProcessorToolResultBudget] = struct{}{}
		active[sessionMemoryProcessorMicroCompact] = struct{}{}
		active[sessionMemoryProcessorFullCompact] = struct{}{}
	} else {
		active[defaultProcessorOffloader] = struct{}{}
		active[defaultProcessorDialogue] = struct{}{}
		active[defaultProcessorCurrentRound] = struct{}{}
		active[defaultProcessorRoundLevel] = struct{}{}
	}

	userTypes := make(map[string]struct{}, len(r.userProcessors))
	for _, spec := range r.userProcessors {
		userTypes[spec.Type] = struct{}{}
	}

	out := make([]ceiface.ProcessorSpec, 0, len(specs))
	for _, spec := range specs {
		if _, isPreset := allPresetProcessorTypes[spec.Type]; !isPreset {
			// 非 preset 类型（用户自定义处理器）原样保留
			out = append(out, spec)
			continue
		}
		if _, ok := active[spec.Type]; ok {
			out = append(out, spec)
			continue
		}
		if _, overridden := userTypes[spec.Type]; overridden {
			logger.Warn(logComponent).
				Str("processor", spec.Type).
				Bool("session_memory_enabled", r.sessionMemoryEnabled).
				Msg("processor config ignored: not in active preset for current mode")
		}
	}
	return out
}

// defaultPresetProcessors 构造默认（非 session_memory）preset。
// 显式覆盖值严格对齐 Python _build_preset_processors 中的参数。
func defaultPresetProcessors(
	modelConfig *llmschema.ModelRequestConfig,
	modelClientConfig *llmschema.ModelClientConfig,
) []ceiface.ProcessorSpec {
	return []ceiface.ProcessorSpec{
		{
			Type: defaultProcessorOffloader,
			Config: &offloader.MessageSummaryOffloaderConfig{
				LargeMessageThreshold: 10000, // Python: large_message_threshold=10000 (覆盖默认1000)
				OffloadMessageTypes:   []string{"tool"},
				ProtectedToolNames:    []string{"read_file:*SKILL.md", "reload_original_context_messages"},
				Model:                 modelConfig,
				ModelClient:           modelClientConfig,
			},
		},
		{
			Type: defaultProcessorDialogue,
			Config: &compressor.DialogueCompressorConfig{
				TriggerContextRatio:     compressor.DefaultTriggerContextRatio,
				MinTargetContextRatio:   compressor.DefaultMinTargetContextRatio,
				MessagesToKeep:          10,    // Python: messages_to_keep=10
				KeepLastRound:           false, // Python: keep_last_round=False
				CompressionTargetTokens: 1800,
				Model:                   modelConfig,
				ModelClient:             modelClientConfig,
			},
		},
		{
			Type: defaultProcessorCurrentRound,
			// 用 NewCurrentRoundCompressorConfig 构造以携带默认值
			// （对齐 Python pydantic Field(default=...) 自动填默认——
			// 结构体字面量会漏 MinSelectedTokensForCompression 等字段导致
			// Validate 失败），再按 preset 覆盖指定字段
			Config: func() *compressor.CurrentRoundCompressorConfig {
				cfg := compressor.NewCurrentRoundCompressorConfig()
				cfg.MessagesToKeep = 3
				cfg.Model = modelConfig
				cfg.ModelClient = modelClientConfig
				return cfg
			}(),
		},
		{
			Type: defaultProcessorRoundLevel,
			// 用 NewRoundLevelCompressorConfig 构造以携带默认值（对齐
			// Python pydantic 自动填默认，防止 CompressionCallMaxTokens 等
			// 字段漏设导致 Validate 失败），再按 preset 覆盖指定字段
			Config: func() *compressor.RoundLevelCompressorConfig {
				cfg := compressor.NewRoundLevelCompressorConfig()
				cfg.TriggerContextRatio = compressor.DefaultTriggerContextRatio
				cfg.TargetTotalTokens = 160000
				cfg.KeepRecentMessages = 6 // Python: keep_recent_messages=6 (覆盖默认0)
				cfg.Model = modelConfig
				cfg.ModelClient = modelClientConfig
				return cfg
			}(),
		},
	}
}

// sessionMemoryPresetProcessors 构造 session_memory 启用时的 preset。
// Python: ContextProcessorRail._build_preset_processors (session_memory=True)
// 三个处理器均无参构造（Python L157-172），pydantic 自动填默认值。
// Go 端等价性说明：
//   - ToolResultBudgetProcessorConfig：Validate 先调 applyDefaults()
//     自愈填默认（TokensThreshold=50000 等），裸字面量安全；
//   - MicroCompact/FullCompact：Validate 无自愈，裸字面量会漏
//     TriggerThreshold/TriggerTotalTokens 等必填字段导致初始化失败
//     （"初始化处理器类型失败"错误），必须用 New*Config 构造器
func sessionMemoryPresetProcessors(
	modelConfig *llmschema.ModelRequestConfig,
	modelClientConfig *llmschema.ModelClientConfig,
) []ceiface.ProcessorSpec {
	return []ceiface.ProcessorSpec{
		{
			Type:   sessionMemoryProcessorToolResultBudget,
			Config: &offloader.ToolResultBudgetProcessorConfig{},
		},
		{
			Type:   sessionMemoryProcessorMicroCompact,
			Config: compressor.NewMicroCompactProcessorConfig(),
		},
		{
			Type: sessionMemoryProcessorFullCompact,
			// NewFullCompactProcessorConfig 携带全部默认值，再按 preset
			// 覆盖模型配置字段（Python: FullCompactProcessorConfig(model=...,
			// model_client=...) 其余字段走 pydantic 默认）
			Config: func() *compressor.FullCompactProcessorConfig {
				cfg := compressor.NewFullCompactProcessorConfig()
				cfg.Model = modelConfig
				cfg.ModelClient = modelClientConfig
				return cfg
			}(),
		},
	}
}

// maybeInjectOffloadSection 如果配置了处理器，注入 offload 提示节。
// Python: ContextProcessorRail._maybe_inject_offload_section()
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

// maybeScheduleSessionMemoryUpdate 调度会话记忆更新。
// 对齐 Python: ContextProcessorRail._maybe_schedule_session_memory_update(ctx)
// 仅在 session_memory 启用且 mgr 存在时调用 MaybeScheduleUpdate。
func (r *ContextProcessorRail) maybeScheduleSessionMemoryUpdate(
	ctx context.Context,
	cbc *sainterfaces.AgentCallbackContext,
) {
	if !r.sessionMemoryEnabled || r.sessionMemoryMgr == nil {
		return
	}

	// 获取 workspace 和 session（对齐 Python: workspace=self.workspace; ctx.session）
	ws := r.Workspace()
	var workspaceDir string
	if ws != nil {
		workspaceDir = ws.RootPath
	}

	sess := cbc.Session()
	if workspaceDir == "" || sess == nil {
		return
	}

	// 获取 ModelContext（对齐 Python: ctx.context）
	mc := cbc.ModelContext()
	if mc == nil {
		// 尝试从 Agent → DeepAgentInterface → ReactAgent → ContextEngine → GetContext
		if deepAgent, ok := cbc.Agent().(hinterfaces.DeepAgentInterface); ok && deepAgent != nil {
			reactAgent := deepAgent.ReactAgent()
			if reactAgent != nil && reactAgent.ContextEngine() != nil {
				mc = reactAgent.ContextEngine().GetContext("default_context_id", sess.GetSessionID())
			}
		}
	}

	r.sessionMemoryMgr.MaybeScheduleUpdate(ctx, sess, mc, workspaceDir)
}

// pushRuntimeConfig 把热重载后的引擎配置与处理器规格推给上下文引擎。
// 引擎不可用（非 ReActAgent 形态 / 引擎未实现可选能力）时静默跳过并打 Debug，
// 不影响 rail 初始化本身。
func pushRuntimeConfig(agent sainterfaces.BaseAgent, config ceschema.ContextEngineConfig, specs []ceiface.ProcessorSpec) {
	engine := resolveContextEngine(agent)
	if engine == nil {
		logger.Debug(logComponent).Msg("context engine unavailable, skip runtime config push")
		return
	}
	applier, ok := engine.(runtimeConfigApplier)
	if !ok {
		logger.Debug(logComponent).Msg("context engine does not support runtime config refresh")
		return
	}
	applier.ApplyRuntimeConfig(config, specs)
}

// resolveContextEngine 从 agent 上取上下文引擎（DeepAgent → 内层 ReActAgent）。
func resolveContextEngine(agent sainterfaces.BaseAgent) ceiface.ContextEngine {
	if agent == nil {
		return nil
	}
	if deepAgent, ok := agent.(hinterfaces.DeepAgentInterface); ok {
		if inner := deepAgent.ReactAgent(); inner != nil {
			agent = inner
		}
	}
	if provider, ok := agent.(interface{ ContextEngine() ceiface.ContextEngine }); ok {
		return provider.ContextEngine()
	}
	return nil
}

// runtimeConfigApplier 上下文引擎的可选能力：接收热重载后的配置与处理器实例。
type runtimeConfigApplier interface {
	// ApplyRuntimeConfig 暂存新的引擎配置与处理器规格
	ApplyRuntimeConfig(config ceschema.ContextEngineConfig, specs []ceiface.ProcessorSpec)
}

// getReactAgentConfig 从 BaseAgent 获取 ReActAgentConfig。
// Python: config = getattr(getattr(agent, "react_agent", None), "_config", None)
// ——先解引用内层 react_agent 再取 _config。Init 的 agent 是 DeepAgent
// （DeepAgent.Config() 返回 nil），必须走 ReactAgent().Config() 内层配置。
func getReactAgentConfig(agent sainterfaces.BaseAgent) *saconfig.ReActAgentConfig {
	if agent == nil {
		return nil
	}
	// DeepAgent → 解引用内层 ReActAgent（对齐 Python agent.react_agent._config）
	if deepAgent, ok := agent.(hinterfaces.DeepAgentInterface); ok {
		if inner := deepAgent.ReactAgent(); inner != nil {
			agent = inner
		}
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

// UserProcessorTypes 返回用户自定义处理器类型列表（只读，供测试与诊断）。
func (r *ContextProcessorRail) UserProcessorTypes() []string {
	names := make([]string, 0, len(r.userProcessors))
	for _, spec := range r.userProcessors {
		names = append(names, spec.Type)
	}
	return names
}

// SessionMemoryEnabled 返回是否启用 session memory 预设（只读，供测试与诊断）。
func (r *ContextProcessorRail) SessionMemoryEnabled() bool {
	return r.sessionMemoryEnabled
}
