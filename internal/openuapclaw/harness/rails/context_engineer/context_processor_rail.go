package context_engineer

import (
	"context"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cecontext "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/context"
	ceiface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	forkedcompressor "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor"
	forkedoffloader "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/offloader"
	ceschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	saconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/config"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	compressionrecall "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/compression_recall"
	filesystemtools "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/filesystem"
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
	// recallEnabled 压缩召回生效开关（recall 配置开启 + 存在受支持的 forked 压缩器）
	// Python: self._recall_enabled
	recallEnabled bool
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
	presetProcessorOffloader    = "MessageSummaryOffloader"
	presetProcessorDialogue     = "DialogueCompressor"
	presetProcessorCurrentRound = "CurrentRoundCompressor"
	presetProcessorRoundLevel   = "RoundLevelCompressor"

	// session_memory preset 的处理器类型
	// Python: ContextProcessorRail._build_preset_processors (session_memory=True)

	// forked 链的会话记忆物化压缩器（24.2；Python: _SESSION_MEMORY_PROCESSOR_KEY，
	// _build_preset_processors 中 SessionMemoryCompressorConfig(enabled=False)）
	presetProcessorSessionMemory = "SessionMemoryCompressor"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// allPresetProcessorTypes preset 覆盖的全部处理器类型集合。
// 用于区分"preset 内建类型"与"用户自定义处理器"：前者受 mode 激活集过滤，
// 后者始终保留。
var allPresetProcessorTypes = map[string]struct{}{
	presetProcessorOffloader:     {},
	presetProcessorDialogue:      {},
	presetProcessorCurrentRound:  {},
	presetProcessorRoundLevel:    {},
	presetProcessorSessionMemory: {},
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
		// agent_edit 模式：注入 SessionMemoryAgent 执行体 + 笔记读状态预填充
		// （挂账①⑤销账；Python: SessionMemoryManager 构造期闭包装配）
		r.wireSessionMemoryAgent(agent, modelConfig, modelClientConfig)
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

	config.ContextProcessors = allProcessors
	r.allProcessors = allProcessors

	// 热重载跟随：把新配置与新处理器规格推给上下文引擎，由下一次 CreateContext
	// （用户轮次入口）应用到池内的老上下文。Python 无此行为——其 create_context
	// 命中池子时忽略 processors，老会话不会跟随（见 Go 侧增强说明）。
	pushRuntimeConfig(agent, config.ContextEngineConfig, allProcessors)

	// 压缩召回生效判定与接线（Python: init 尾部 _recall_enabled 分支）
	r.wireCompressionRecall(agent, config.ContextEngineConfig, allProcessors)

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
		r.systemPromptBuilder.RemoveSection("compression_recall")
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
	// Python: self._maybe_inject_compression_recall_section()
	r.maybeInjectCompressionRecallSection()
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

// buildPresetProcessors 构建单 forked preset 链（对齐当前 Python 单链，无两分支互斥）。
// Python: ContextProcessorRail._build_preset_processors (context_processor_rail.py:204-272)：
// forked.activate() 后注册 5 处理器——MessageSummaryOffloader（forked 版，15b 回填
// 前暂用旧配置形态，D-24.15-a1）+ SessionMemoryCompressor（默认 disabled，用户以
// enabled=True 覆盖即启用并伴随 SessionMemoryManager 启动）+ Dialogue/CurrentRound/
// RoundLevel 三压缩器（model/model_client 透传）。context_debug 传播随 15b 补齐
// （Go 侧 enable_context_debug 字段在 15b offloader/dump 半区接入）。
func (r *ContextProcessorRail) buildPresetProcessors(
	modelConfig *llmschema.ModelRequestConfig,
	modelClientConfig *llmschema.ModelClientConfig,
) []ceiface.ProcessorSpec {
	// Python: forked.activate()——工厂注册表 last-write-wins，forked 实现覆盖旧工厂
	forkedcompressor.Activate()
	specs := make([]ceiface.ProcessorSpec, 0, 5)
	specs = append(specs, ceiface.ProcessorSpec{
		Type: presetProcessorOffloader,
		// Python: ForkedMessageSummaryOffloaderConfig（enable_debug_dump 半区随 context_debug 半区接入）
		Config: func() *forkedoffloader.MessageSummaryOffloaderConfig {
			cfg := forkedoffloader.NewMessageSummaryOffloaderConfig()
			cfg.ProtectedToolNames = []string{"read_file:*SKILL.md", "reload_original_context_messages"}
			return cfg
		}(),
	})
	specs = append(specs, ceiface.ProcessorSpec{
		Type: presetProcessorSessionMemory,
		// Python: SessionMemoryCompressorConfig(enabled=False, memory=...)
		Config: func() *forkedcompressor.SessionMemoryCompressorConfig {
			cfg := forkedcompressor.NewSessionMemoryCompressorConfig()
			if r.sessionMemoryConfig != nil {
				cfg.Memory = r.sessionMemoryConfig
			}
			return cfg
		}(),
	})
	specs = append(specs, ceiface.ProcessorSpec{
		Type: presetProcessorDialogue,
		Config: func() *forkedcompressor.DialogueCompressorConfig {
			cfg := forkedcompressor.NewDialogueCompressorConfig()
			cfg.Model = modelConfig
			cfg.ModelClient = modelClientConfig
			return cfg
		}(),
	})
	specs = append(specs, ceiface.ProcessorSpec{
		Type: presetProcessorCurrentRound,
		Config: func() *forkedcompressor.CurrentRoundCompressorConfig {
			cfg := forkedcompressor.NewCurrentRoundCompressorConfig()
			cfg.Model = modelConfig
			cfg.ModelClient = modelClientConfig
			return cfg
		}(),
	})
	specs = append(specs, ceiface.ProcessorSpec{
		Type: presetProcessorRoundLevel,
		Config: func() *forkedcompressor.RoundLevelCompressorConfig {
			cfg := forkedcompressor.NewRoundLevelCompressorConfig()
			cfg.Model = modelConfig
			cfg.ModelClient = modelClientConfig
			return cfg
		}(),
	})
	return specs
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

// wireSessionMemoryAgent agent_edit 模式装配：SessionMemoryAgent 执行体注入
// SetAgentEditAgent + SetPrimeNotesFn（文件读状态预填充，filesystem.SetFileReadState）。
// 挂账①⑤销账；Python: SessionMemoryUpdateAgent（session_memory_manager.py L214-466）
// 的构建半区 + _prime_notes_file_as_read（:430-447）。
func (r *ContextProcessorRail) wireSessionMemoryAgent(
	agent sainterfaces.BaseAgent,
	modelConfig *llmschema.ModelRequestConfig,
	modelClientConfig *llmschema.ModelClientConfig,
) {
	if r.sessionMemoryMgr == nil {
		return
	}
	_ = agent
	updater, ok := r.sessionMemoryMgr.AgentUpdater()
	if !ok {
		return // direct_replace 模式无需执行体
	}
	workspaceRoot := ""
	if ws := r.Workspace(); ws != nil {
		workspaceRoot = ws.RootPath
	}
	sma := forkedcompressor.NewSessionMemoryAgent(
		nil, modelConfig, modelClientConfig, nil,
		forkedcompressor.NewSessionMemoryAgentConfig(),
		workspaceRoot,
	)
	r.sessionMemoryMgr.SetAgentEditAgent(sma)
	// 预填充：把笔记当前内容登记为已读（对齐 Python _prime_notes_file_as_read）
	updater.SetPrimeNotesFn(func(notesPath string, currentNotes string) {
		if stat, err := os.Stat(notesPath); err == nil {
			filesystemtools.SetFileReadState(notesPath, &filesystemtools.FileReadState{
				MtimeNS:   stat.ModTime().UnixNano(),
				SizeBytes: stat.Size(),
				IsPartial: false,
				Content:   currentNotes,
			})
		}
	})
}

// wireCompressionRecall 压缩召回生效判定 + 工具注册 + offloader 保护名单。
// Python: init 尾部 recall_requested/_recall_enabled 分支 + _protect_compression_recall_tool_results
// + _register_compression_recall_tool（context_processor_rail.py:378-392）。
func (r *ContextProcessorRail) wireCompressionRecall(
	agent sainterfaces.BaseAgent,
	engineConfig ceschema.ContextEngineConfig,
	allProcessors []ceiface.ProcessorSpec,
) {
	recallConfig := engineConfig.CompressionRecall
	recallRequested := recallConfig != nil && recallConfig.Enabled
	supportedCompressorPresent := false
	supported := map[string]bool{
		"DialogueCompressor": true, "CurrentRoundCompressor": true,
		"RoundLevelCompressor": true, "SessionMemoryCompressor": true,
	}
	for _, spec := range allProcessors {
		if supported[spec.Type] {
			supportedCompressorPresent = true
			break
		}
	}
	r.recallEnabled = recallRequested && supportedCompressorPresent
	if recallConfig != nil {
		logger.Info(logComponent).
			Bool("enabled", recallConfig.Enabled).
			Msg("compression recall effective config")
	}
	if recallRequested && !supportedCompressorPresent {
		logger.Warn(logComponent).Msg("compression recall is enabled but no supported forked compressor is configured")
	}
	if !r.recallEnabled {
		return
	}
	// Python: _protect_compression_recall_tool_results——recall 工具结果免于规则压缩
	r.protectCompressionRecallToolResults(allProcessors)
	// Python: _register_compression_recall_tool
	r.registerCompressionRecallTool(agent)
}

// protectCompressionRecallToolResults 向 forked offloader 配置追加 recall 工具保护名单。
// Python: context_processor_rail.py:694 (_protect_compression_recall_tool_results)
func (r *ContextProcessorRail) protectCompressionRecallToolResults(processors []ceiface.ProcessorSpec) {
	for i := range processors {
		if processors[i].Type != "MessageSummaryOffloader" {
			continue
		}
		if cfg, ok := processors[i].Config.(*forkedoffloader.MessageSummaryOffloaderConfig); ok {
			already := false
			for _, name := range cfg.ProtectedToolNames {
				if name == "recall_compressed_context" {
					already = true
					break
				}
			}
			if !already {
				cfg.ProtectedToolNames = append(cfg.ProtectedToolNames, "recall_compressed_context")
			}
		}
	}
}

// registerCompressionRecallTool 注册 recall 工具到 agent 能力管理器。
// Python: context_processor_rail.py:656 (_register_compression_recall_tool)
func (r *ContextProcessorRail) registerCompressionRecallTool(agent sainterfaces.BaseAgent) {
	abilityManager := agent.AbilityManager()
	if abilityManager == nil {
		return
	}
	workspaceDir := ""
	if ws := r.Workspace(); ws != nil {
		workspaceDir = ws.RootPath
	}
	agentID := "default"
	language := "cn"
	if r.systemPromptBuilder != nil && r.systemPromptBuilder.Language() != "" {
		language = r.systemPromptBuilder.Language()
	}
	recallTool, card, err := compressionrecall.NewCompressionRecallTool(workspaceDir, language, agentID)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("register compression recall tool failed")
		return
	}
	abilityManager.AddAbility(card, recallTool)
}

// maybeInjectCompressionRecallSection 按召回开关注入/移除 compression_recall 节。
// Python: context_processor_rail.py:686 (_maybe_inject_compression_recall_section)
func (r *ContextProcessorRail) maybeInjectCompressionRecallSection() {
	if r.systemPromptBuilder == nil {
		return
	}
	if !r.recallEnabled {
		r.systemPromptBuilder.RemoveSection("compression_recall")
		return
	}
	lang := "cn"
	if r.systemPromptBuilder.Language() != "" {
		lang = r.systemPromptBuilder.Language()
	}
	r.systemPromptBuilder.AddSection(sections.BuildCompressionRecallSection(lang))
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
