package adapter

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	agentcron "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/cron"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/sysop_builder"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentConfigLister Agent 配置列表接口（避免 adapter↔runtime 循环依赖）。
// Python: AgentConfigService.list_agents()
type AgentConfigLister interface {
	// ListCustomAgents 列出自定义 agent（非 builtin）
	ListCustomAgents() []*types.AgentDefinition
}

// runtimeConfig 运行时配置。
// Python: _RuntimeConfig (interface_deep.py L3108-3119)
type runtimeConfig struct {
	// CWD 当前工作目录
	CWD string
	// Language 运行时语言
	Language string
	// Channel 请求频道
	Channel string
	// ProjectDir 项目目录
	ProjectDir string
	// WorkspaceDir 工作区目录
	WorkspaceDir string
	// SessionID 会话标识
	// Python: session_id
	SessionID string
	// Mode 运行模式，默认 "agent.plan"
	// Python: mode
	Mode string
	// RequestID 请求标识
	// Python: request_id
	RequestID string
	// ChannelID 渠道标识
	// Python: channel_id
	ChannelID string
	// RequestMetadata 请求元数据
	// Python: request_metadata，⤵️ 具体类型待 10.4 定义
	RequestMetadata map[string]any
	// TrustedDirs 可信目录列表
	// Python: trusted_dirs
	TrustedDirs []string
	// ForceEnglish 强制英文 section（code 模式）
	// Python: force_english_runtime_prompt（CodeAdapter 默认 True，DeepAdapter 默认 False）
	ForceEnglish bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// updateRuntimeConfig 更新运行时配置。
// Python: _update_runtime_config() (interface_deep.py L3121-3176)
//
// 包含 CWD 种子、language/channel 解析、runtime state YAML 写入、RuntimePromptRail setter、prompt 模式切换。
func (d *DeepAdapter) updateRuntimeConfig(ctx context.Context, config *runtimeConfig) {
	if config == nil {
		return
	}

	// 步骤 1: CWD 种子
	// Python: self._seed_runtime_cwd(runtime_config.cwd or ...)
	if config.CWD != "" {
		d.seedRuntimeCwd(ctx, config.CWD)
	}

	// 步骤 2: language 解析
	// Python: resolved_language = self._resolve_runtime_language()
	resolvedLanguage := d.resolveRuntimeLanguage()

	// 步骤 3: channel 解析
	// Python: resolved_channel = str(runtime_config.channel_id or self._resolve_prompt_channel(session_id) or "web").strip() or "web"
	resolvedChannel := commrails.FirstNonEmpty(
		config.ChannelID,
		resolvePromptChannel(config.SessionID),
		"web",
	)

	// 步骤 4: 写 runtime_state.yaml（替代旧的环境变量写入）
	// Python: self._write_runtime_state(mode=..., language=..., channel=..., project_dir=...)
	d.writeRuntimeStateYAML(
		ctx,
		config.Mode,
		resolvedLanguage,
		resolvedChannel,
		config.ProjectDir,
	)

	// 步骤 5: RuntimePromptRail 7 个 setter
	// Python: if self._runtime_prompt_rail: self._runtime_prompt_rail.set_language(resolved_language) ...
	if d.runtimePromptRail != nil {
		d.runtimePromptRail.SetLanguage(resolvedLanguage)
		d.runtimePromptRail.SetForceEnglish(config.ForceEnglish)
		d.runtimePromptRail.SetChannel(resolvedChannel)
		d.runtimePromptRail.SetTrustedDirs(config.TrustedDirs)
		d.runtimePromptRail.SetRuntimePaths(config.CWD, config.ProjectDir)
		d.runtimePromptRail.SetModelName(d.resolveModelName())
		d.runtimePromptRail.SetMode(config.Mode)
	}

	// 步骤 6: updatePromptForMode
	// Python: self._update_prompt_for_mode(runtime_config.mode, resolved_language)
	d.updatePromptForMode(config.Mode)

	// 步骤 7: rail/tool 模式切换（仅 evolution 分支已实现）
	// Python: await self._update_rails_for_mode(runtime_config.mode)
	d.updateRailsForMode(ctx, config.Mode)

	// 步骤 7.5: 会话工具刷新（cron 注册分支）
	// Python: await self._update_session_tools(session_id, request_id, channel_id) (L3165)
	// _update_tools_for_mode（agent.fast multi-session 工具）⤵️ 待 agent.fast 模式回填
	d.updateSessionTools(ctx, config)

	logger.Info(logComponent).
		Str("cwd", config.CWD).
		Str("language", resolvedLanguage).
		Str("channel", resolvedChannel).
		Str("mode", config.Mode).
		Msg("updateRuntimeConfig completed")
}

// buildConfiguredSubagents 构建子代理规格。
// Python: _build_configured_subagents() (line 878-970)
//
// 根据 config 中 subagents 段的启用状态，调用各 builder 构建配置。
// 返回 (subagentSpecs, shouldAddGeneralAgent)。
// Python: _build_configured_subagents() — Deep 模式下只直接构建
// research/browser/custom 子代理，explore/plan 通过 code_agent 间接注入
func (d *DeepAdapter) buildConfiguredSubagents(config map[string]any, configBase map[string]any) ([]hschema.SubagentSpec, bool) {
	var specs []hschema.SubagentSpec

	// Python: 按 subagents 配置段构建
	subagentsCfg, _ := config["subagents"].(map[string]any)

	// ── general_agent: 配置控制，需 enabled:true 才启用（默认禁用）──
	shouldAddGeneralAgent := d.isSubagentExplicitlyEnabled(subagentsCfg, "general_agent")

	// ── research_agent: 配置控制，需 enabled:true 才启用（默认禁用）──
	if d.isSubagentExplicitlyEnabled(subagentsCfg, "research_agent") {
		params := d.buildResearchSubagentParams(config, configBase)
		cfg := subagents.BuildResearchAgentConfig(d.model, params)
		if cfg != nil {
			specs = append(specs, cfg)
		}
	}

	// ── browser_agent: 由 browser_runtime_enabled 控制 ──
	// 对齐 Python _build_configured_subagents browser 分支：
	// browser_enabled → BROWSER_DRIVER 默认值注入 + chrome_path 解析 + 注册 subagent；
	// 未启用但配置了 browser_agent → 跳过并记录日志
	browserAgentCfg, _ := subagentsCfg["browser_agent"].(map[string]any)
	if d.browserRuntimeEnabled() {
		// Python: if not str(os.getenv("BROWSER_DRIVER") or "").strip():
		//   os.environ["BROWSER_DRIVER"] = "managed"
		if strings.TrimSpace(os.Getenv("BROWSER_DRIVER")) == "" {
			// Python os.environ 赋值无错误返回；Setenv 失败仅影响默认值兜底，不阻断
			_ = os.Setenv("BROWSER_DRIVER", "managed")
			logger.Info(logComponent).
				Msg("browser subagent enabled without BROWSER_DRIVER; defaulting to managed mode")
		}
		// Python: if not str(os.getenv("BROWSER_MANAGED_BINARY") or "").strip():
		//   chrome_path = self._resolve_managed_browser_binary_from_config()
		//   if chrome_path: os.environ["BROWSER_MANAGED_BINARY"] = chrome_path
		if strings.TrimSpace(os.Getenv("BROWSER_MANAGED_BINARY")) == "" {
			if chromePath := d.resolveManagedBrowserBinaryFromConfig(configBase); chromePath != "" {
				// Python os.environ 赋值无错误返回；Setenv 失败仅影响默认值兜底，不阻断
				_ = os.Setenv("BROWSER_MANAGED_BINARY", chromePath)
				logger.Info(logComponent).
					Str("binary", chromePath).
					Msg("using browser.chrome_path for managed browser")
			}
		}
		params := d.buildBrowserSubagentParams(config, browserAgentCfg)
		cfg := subagents.BuildBrowserAgentConfig(d.model, params)
		if cfg != nil {
			specs = append(specs, cfg)
		}
	} else if subagentsCfg != nil && len(browserAgentCfg) > 0 {
		// Python: logger.info("browser_agent config detected but browser runtime is not enabled; skipping")
		logger.Info(logComponent).
			Msg("browser_agent config detected but browser runtime is not enabled; skipping browser subagent registration")
	}

	// ── 自定义 agent: 对齐 Python _load_custom_subagents ──
	// Python: custom_specs = _load_custom_subagents(workspace_dir, subagents_cfg, model, workspace, ...)
	customSpecs := d.loadCustomSubagents(subagentsCfg)
	specs = append(specs, customSpecs...)

	logger.Info(logComponent).
		Int("subagent_count", len(specs)).
		Bool("should_add_general_agent", shouldAddGeneralAgent).
		Msg("buildConfiguredSubagents completed")

	return specs, shouldAddGeneralAgent
}

// isSubagentExplicitlyEnabled 检查子代理是否通过配置显式启用。
// Python: _is_subagent_enabled() — 只有 enabled:true 才启用，默认禁用
func (d *DeepAdapter) isSubagentExplicitlyEnabled(subagentsCfg map[string]any, name string) bool {
	if subagentsCfg == nil {
		return false
	}
	v, ok := subagentsCfg[name]
	if !ok {
		return false
	}
	// 配置值为 bool 时直接返回
	if b, ok := v.(bool); ok {
		return b
	}
	// 配置值为 dict 时检查 enabled 字段，对齐 Python: subagent_cfg.get("enabled", False)
	if m, ok := v.(map[string]any); ok {
		if enabled, ok := m["enabled"]; ok {
			if b, ok := enabled.(bool); ok {
				return b
			}
		}
		return false
	}
	return false
}

// loadCustomSubagents 从 AgentConfigService 加载自定义 agent 并转换为 SubagentSpec 列表。
// Python: _load_custom_subagents(workspace_dir, subagents_cfg, model, workspace, ...)
//
// 仅加载 enabled:true 的自定义 agent（非 builtin）。
func (d *DeepAdapter) loadCustomSubagents(subagentsCfg map[string]any) []hschema.SubagentSpec {
	// Python: agent_service = AgentConfigService(workspace_dir)
	// 通过 d.configLister 获取自定义 agent 列表（依赖注入，避免循环依赖）
	if d.configLister == nil {
		return nil
	}
	customAgents := d.configLister.ListCustomAgents()

	var result []hschema.SubagentSpec
	// Python: for agent_def in agent_service.list_agents():
	for _, agentDef := range customAgents {
		// Python: if agent_def.source == "builtin": continue
		// （ListCustomAgents 已过滤 builtin）

		// Python: subagent_cfg = subagents_cfg.get(agent_def.name)
		// 只有显式 enabled: true 才加载
		if !d.isSubagentExplicitlyEnabled(subagentsCfg, agentDef.Name) {
			continue
		}

		// Python: custom_spec = _agent_def_to_subagent_config(agent_def, model, workspace, model_cache)
		spec := agentDefToSubagentConfig(agentDef, d.model, d.modelCache, d.toolCards)
		if spec != nil {
			result = append(result, spec)
			logger.Info(logComponent).
				Str("agent_name", agentDef.Name).
				Str("source", agentDef.Source).
				Msg("custom agent loaded")
		}
	}
	return result
}

// agentDefToSubagentConfig 将 AgentDefinition 转换为 SubAgentConfig。
// Python: _agent_def_to_subagent_config(agent_def, model, workspace, model_cache)
func agentDefToSubagentConfig(agentDef *types.AgentDefinition, model *llm.Model, modelCache map[string]*llm.Model, allToolCards []*tool.ToolCard) *hschema.SubAgentConfig {
	// 步骤 1: 解析模型
	// Python: resolved_model = model; if agent_def.model and isinstance(model_cache, dict): resolved_model = model_cache.get(agent_def.model, model)
	resolvedModel := model
	if agentDef.Model != "" && modelCache != nil {
		if cached, ok := modelCache[agentDef.Model]; ok {
			resolvedModel = cached
		}
	}

	// 步骤 2: 构建工具列表
	// Python: tools = list(agent_def.tools) if agent_def.tools else ["*"]
	// Python: if agent_def.disallowed_tools and tools != ["*"]: tools = [t for t in tools if t not in agent_def.disallowed_tools]
	tools := agentDef.Tools
	if len(tools) == 0 {
		tools = []string{"*"}
	}
	if len(agentDef.DisallowedTools) > 0 && (len(tools) != 1 || tools[0] != "*") {
		disallowedSet := make(map[string]bool, len(agentDef.DisallowedTools))
		for _, t := range agentDef.DisallowedTools {
			disallowedSet[t] = true
		}
		filtered := make([]string, 0, len(tools))
		for _, t := range tools {
			if !disallowedSet[t] {
				filtered = append(filtered, t)
			}
		}
		tools = filtered
	}

	// Python: tools=tools — 将字符串列表转为 []*tool.ToolCard
	// Python 的 SubAgentConfig.tools 接受 List[Tool | ToolCard | str]
	// Go 强类型，需要通过 filterToolCards 转换
	var filteredToolCards []*tool.ToolCard
	if len(allToolCards) > 0 {
		filteredToolCards = filterToolCards(allToolCards, tools, agentDef.DisallowedTools)
	}

	// 步骤 3: 构建 AgentCard
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName(agentDef.Name),
		agentschema.WithAgentID(agentDef.Name),
		agentschema.WithAgentDescription(agentDef.Description),
	)

	// 步骤 4: 构建 SubAgentConfig
	maxIter := 0
	if agentDef.MaxIterations != nil {
		maxIter = *agentDef.MaxIterations
	}

	return &hschema.SubAgentConfig{
		AgentCard:      card,
		SystemPrompt:   agentDef.Prompt,
		Tools:          filteredToolCards,
		Model:          resolvedModel,
		Skills:         agentDef.Skills,
		MaxIterations:  maxIter,
		EnableTaskLoop: true,
		FactoryName:    "custom_" + agentDef.Name,
		// Python: custom_spec.factory_kwargs = {"auto_create_workspace": False}
		FactoryKwargs: map[string]any{"auto_create_workspace": false},
	}
}

// writeRuntimeStateYAML 将运行时状态写入 config 目录下的 runtime_state.yaml。
// Python: _write_runtime_state() (interface_deep.py L756-821)
func (d *DeepAdapter) writeRuntimeStateYAML(ctx context.Context, mode, language, channel, projectDir string) {
	commrails.WriteRuntimeStateYAML(
		ctx,
		d.resolveModelName(),
		mode,
		language,
		channel,
		d.agentName,
		projectDir,
	)
}

// skillIncludeToolsForProfile 检查当前 profile 下 skill 是否包含工具。
// Python: _skill_include_tools_for_profile() (line 722-726)
// 逻辑：ACP tool profile 下不包含工具；否则取决于 filesystemRail 是否为 nil。
func (d *DeepAdapter) skillIncludeToolsForProfile(instanceOverrides map[string]any) bool {
	if d.isAcpToolProfile(instanceOverrides) {
		return false
	}
	return d.filesystemRail == nil
}

// resolvePromptChannel 从 sessionID 解析 prompt channel。
// Python: _resolve_prompt_channel(session_id) (line 728-748)
// 逻辑：从 sessionID 前缀解析 channel，支持 acp/cron/heartbeat/feishu/web/dingtalk/wecom。
func resolvePromptChannel(sessionID string) string {
	if sessionID == "" {
		return "web"
	}
	// 提取前缀（下划线前的部分）
	channel := sessionID
	if idx := strings.Index(sessionID, "_"); idx > 0 {
		channel = sessionID[:idx]
	}
	switch channel {
	case "acp", "cron", "heartbeat", "feishu", "web", "dingtalk", "wecom":
		return channel
	default:
		// "sess" 前缀或其他情况回退到 "web"
		return "web"
	}
}

// resolveModelName 从模型请求配置解析当前模型名称。
// Python: _resolve_model_name() (line 750-755)
// 逻辑：从 modelRequestConfig.modelName 获取，默认 "unknown"。
func (d *DeepAdapter) resolveModelName() string {
	if d.modelRequestConfig != nil && d.modelRequestConfig.ModelName != "" {
		return d.modelRequestConfig.ModelName
	}
	return "unknown"
}

// isAcpToolProfile 检查是否为 ACP Tool profile。
// Python: _is_acp_tool_profile(config) (L709-716)
// 优先检查 tool_profile，fallback 检查 channel_id，值为 "acp"
func (d *DeepAdapter) isAcpToolProfile(instanceOverrides map[string]any) bool {
	if instanceOverrides == nil {
		return false
	}
	// 优先检查 tool_profile
	if tp, ok := instanceOverrides["tool_profile"]; ok {
		s := strings.TrimSpace(strings.ToLower(fmt.Sprint(tp)))
		if s != "" {
			return s == "acp"
		}
	}
	// fallback 检查 channel_id
	if cid, ok := instanceOverrides["channel_id"]; ok {
		s := strings.TrimSpace(strings.ToLower(fmt.Sprint(cid)))
		return s == "acp"
	}
	return false
}

// filesystemRailEnabledForProfile 检查当前 profile 是否启用文件系统护栏。
// Python: _filesystem_rail_enabled_for_profile() — 读取 enable_filesystem_rail 配置，默认 true
func (d *DeepAdapter) filesystemRailEnabledForProfile(instanceOverrides map[string]any) bool {
	if instanceOverrides != nil {
		if v, ok := instanceOverrides["enable_filesystem_rail"]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
	}
	// 默认启用
	return true
}

// resolveRuntimeLanguage 解析运行时语言（标准化后）。
// Python: _resolve_runtime_language() — 调用 resolve_language() 标准化
func (d *DeepAdapter) resolveRuntimeLanguage() string {
	return prompts.ResolveLanguage(d.resolvePromptLanguage())
}

// updateSessionTools 注册 cron 和 send_file 工具（与 mode 无关，每次请求刷新）。
// Python: _update_session_tools (interface_deep.py L2966-3010)。
// 定时工具按当前 session 的 channel 注册（ctx values 已由步骤 9
// BindRuntimeCronContext 绑定）；heartbeat/cron 前缀会话跳过（系统触发
// 的会话不需要 cron 工具）。send_file 分支 ⤵️ 10.6.24 SendFileToolkit 回填。
func (d *DeepAdapter) updateSessionTools(ctx context.Context, config *runtimeConfig) {
	if config == nil {
		return
	}
	// Python: if not (session_id.startswith("heartbeat") or session_id.startswith("cron"))
	if strings.HasPrefix(config.SessionID, "heartbeat") || strings.HasPrefix(config.SessionID, "cron") {
		return
	}
	// Python L2974: cron_tools = self._build_cron_tools()
	// context 实参是 _runtime_cron_tool_context（ContextVar 代理）；
	// Go 对应从 ctx values 读绑定值组装 CronToolContext。
	rcc := cronbridge.FromRuntimeCronContext(ctx)
	if rcc == nil {
		return
	}
	// Python L2516: agent_id = self._instance.card.id if self._instance else None
	agentID := ""
	if d.instance != nil && d.instance.Card() != nil {
		agentID = d.instance.Card().ID
	}
	cronCtx := &agentcron.CronToolContext{
		ChannelID: rcc.ChannelID,
		SessionID: rcc.SessionID,
		Metadata:  rcc.Metadata,
		Mode:      rcc.Mode,
	}
	// Python L539-541: tool_scope=f"runtime_{id(self):x}"（构造时一次性）。
	// Go 对齐：runtime_{指针十六进制}，adapter 实例生命周期内稳定。
	cronCtx.ToolScopeOverride = fmt.Sprintf("runtime_%x", uintptr(unsafe.Pointer(d)))
	tools := d.cronRuntime.BuildTools(cronCtx, agentID, d.resolveRuntimeLanguage())
	if len(tools) == 0 {
		return
	}
	logger.Info(logComponent).
		Int("count", len(tools)).
		Msg("[DeepAdapter] Registering cron tools")
	// Python: 注册块整体 try/except——instance 未初始化时 ability_manager 访问
	// 抛 AttributeError → error 日志降级。Go 用显式 nil 守卫对齐该降级语义。
	if d.instance == nil {
		logger.Error(logComponent).
			Msg("[DeepAdapter] instance not initialized, cron tools registration skipped")
		return
	}
	resourceMgr := runner.GetResourceMgr()
	for _, cronTool := range tools {
		// Python L2979: if not Runner.resource_mgr.get_tool(cron_tool.card.id)
		card := cronTool.Card()
		if existing, gerr := resourceMgr.GetTool([]string{card.ID}); gerr != nil || len(existing) == 0 {
			if aerr := resourceMgr.AddTool(cronTool); aerr != nil {
				logger.Error(logComponent).
					Err(aerr).
					Str("tool_id", card.ID).
					Msg("[DeepAdapter] cron tool registration failed")
				continue
			}
		}
		d.instance.AbilityManager().Add(card)
	}
	logger.Info(logComponent).Msg("[DeepAdapter] Cron tools registered successfully")
	// ⤵️ 10.6.24: send_file 工具注册分支（channels.<channel>.send_file_allowed
	// 控制 + SendFileToolkit），待 SendFile 工具集实现回填
}

// resolvePromptLanguage 解析提示词语言（原始配置值）。
// Python: _resolve_prompt_language() — 读取 preferred_language，默认 "zh"
func (d *DeepAdapter) resolvePromptLanguage() string {
	// Python: config_base.get("preferred_language", "zh")
	if v, ok := d.configCache["preferred_language"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return strings.TrimSpace(strings.ToLower(s))
		}
	}
	return "zh"
}

// getAgentWorkspaceDir 获取 Agent 数据存储路径，带回退。
// Python: getattr(self, "_agent_workspace_dir", None) or self._workspace_dir
func (d *DeepAdapter) getAgentWorkspaceDir() string {
	if d.agentWorkspaceDir != "" {
		return d.agentWorkspaceDir
	}
	return d.workspaceDir
}

// createSysOperation 创建系统操作实例。
// Python: _create_sys_operation() (line 2262-2320)
//
// 根据配置决定使用 local 或 sandbox 模式，
// 通过 sysop_builder 构建卡片和实例。
func (d *DeepAdapter) createSysOperation(configBase map[string]any) (result sysop.SysOperation) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Any("recover", rec).Msg("[DeepAdapter] createSysOperation panic degraded")
			result = nil
		}
	}()

	mode := sysop_builder.ResolveOperationMode(configBase)

	var card *sysop.SysOperationCard
	switch mode {
	case sysop.OperationModeSandbox:
		sandboxURL, sandboxType, runtime := d.getSandboxRuntime(configBase)
		filesRuntime, _ := runtime["files"].(map[string]any)

		// excluded_commands: 配置层已规范化为 []string
		var excludedCommands []string
		if v, ok := runtime["excluded_commands"]; ok {
			if s, ok := v.([]string); ok {
				excludedCommands = s
			} else {
				logger.Warn(logComponent).
					Str("key", "excluded_commands").
					Str("actual_type", fmt.Sprintf("%T", v)).
					Msg("sandbox runtime field type assertion failed, using default value")
			}
		}

		// idle_ttl_seconds / idle_check_interval: 配置层已规范化为 int
		var idleTTLSecs *int
		if v, ok := runtime["idle_ttl_seconds"]; ok {
			if n, ok := v.(int); ok {
				idleTTLSecs = &n
			} else {
				logger.Warn(logComponent).
					Str("key", "idle_ttl_seconds").
					Str("actual_type", fmt.Sprintf("%T", v)).
					Msg("sandbox runtime field type assertion failed, using default value")
			}
		}

		var idleCheckInterval *int
		if v, ok := runtime["idle_check_interval"]; ok {
			if n, ok := v.(int); ok {
				idleCheckInterval = &n
			} else {
				logger.Warn(logComponent).
					Str("key", "idle_check_interval").
					Str("actual_type", fmt.Sprintf("%T", v)).
					Msg("sandbox runtime field type assertion failed, using default value")
			}
		}

		card = sysop_builder.CreateSandboxSysOpCard(
			sandboxURL, sandboxType,
			filesRuntime, excludedCommands,
			idleTTLSecs,
			idleCheckInterval,
			d.resolveProjectDirForSandbox(),
			d.isCodeAgent,
		)
	default:
		card = sysop_builder.CreateLocalSysOpCard()
	}

	// 从卡片创建 SysOperation 实例（含隔离复用 + ResourceMgr 注册）
	instance := sysop_builder.CreateSysOperationFromCard(card)

	d.sysOperation = instance
	d.sysOperationCard = card

	logger.Info(logComponent).
		Str("mode", mode.String()).
		Msg("createSysOperation completed")

	return instance
}

// resolveProjectDirForSandbox 解析沙箱挂载用的项目目录。
// Python: _resolve_project_dir_for_sandbox()
// 回退链：self._project_dir → self._instance_overrides["project_dir"] → ""
func (d *DeepAdapter) resolveProjectDirForSandbox() string {
	if d.projectDir != "" {
		return d.projectDir
	}
	// 回退到 instance_overrides["project_dir"]
	if d.instanceOverrides != nil {
		if v, ok := d.instanceOverrides["project_dir"].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// getSandboxRuntime 从配置获取沙箱运行时参数。
// Python: get_sandbox_runtime() + get_config()["sandbox"]
func (d *DeepAdapter) getSandboxRuntime(configBase map[string]any) (url, typ string, runtime map[string]any) {
	sandbox, _ := configBase["sandbox"].(map[string]any)
	if sandbox == nil {
		sandbox = make(map[string]any)
	}
	url, _ = sandbox["url"].(string)
	typ, _ = sandbox["type"].(string)
	runtime = sandbox
	return
}

// buildResearchSubagentParams 从配置映射构建 research 子代理的 SubagentCreateParams。
// Python: _build_configured_subagents 中对 build_research_agent_config 的调用
//
// adapter 层负责从 map[string]any 解析出类型安全的 SubagentCreateParams，
// 然后传入 subagents.BuildResearchAgentConfig(model, params)。
func (d *DeepAdapter) buildResearchSubagentParams(config map[string]any, configBase map[string]any) *hschema.SubagentCreateParams {
	// Python: resolved_language = self._resolve_runtime_language()
	resolvedLanguage := d.resolveRuntimeLanguage()

	// Python: max_iterations=parse_int(
	//   Python: research_agent_cfg.get("max_iterations"),
	//   Python: 获取 max_iterations 配置（默认 15）
	// )
	maxIterations := 0
	subagentsCfg, _ := config["subagents"].(map[string]any)
	if subagentsCfg != nil {
		if researchCfg, ok := subagentsCfg["research_agent"]; ok {
			if m, ok := researchCfg.(map[string]any); ok {
				if v, ok := m["max_iterations"]; ok {
					switch n := v.(type) {
					case int:
						maxIterations = n
					case float64:
						maxIterations = int(n)
					}
				}
			}
		}
	}
	if maxIterations == 0 {
		if v, ok := config["max_iterations"]; ok {
			switch n := v.(type) {
			case int:
				maxIterations = n
			case float64:
				maxIterations = int(n)
			}
		}
	}
	if maxIterations == 0 {
		maxIterations = 15
	}

	return &hschema.SubagentCreateParams{
		Language:      resolvedLanguage,
		MaxIterations: maxIterations,
		// Python: workspace=self._workspace_dir or "./"
		// DeepAdapter.workspaceDir 在初始化时已解析，默认为 workspace.AgentRootDir()
		Workspace: hworkspace.NewWorkspace(d.workspaceDir, resolvedLanguage),
	}
}

// browserRuntimeEnabled 判断 browser runtime 是否启用。
//
// 对齐 Python _browser_runtime_enabled()：PLAYWRIGHT_RUNTIME_MCP_ENABLED 优先，
// 回退 BROWSER_RUNTIME_MCP_ENABLED；值 strip+lower 后 ∈ {1,true,yes,on} 才启用。
//
// Python: interface_deep.py _browser_runtime_enabled
func (d *DeepAdapter) browserRuntimeEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(
		firstNonEmptyStr(
			os.Getenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED"),
			os.Getenv("BROWSER_RUNTIME_MCP_ENABLED"),
		),
	))
	switch value {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// resolveManagedBrowserBinaryFromConfig 从配置解析 managed browser 二进制路径。
//
// 对齐 Python _resolve_managed_browser_binary_from_config()：
//   - browser.chrome_path 为字符串 → strip 后直接返回
//   - 为 dict → 按 runtime.GOOS 映射平台键（darwin→macos 等），
//     依序尝试平台键与 "default"
//   - 其余情况返回空串
//
// Python: interface_deep.py _resolve_managed_browser_binary_from_config
func (d *DeepAdapter) resolveManagedBrowserBinaryFromConfig(configBase map[string]any) string {
	if configBase == nil {
		return ""
	}
	browserCfg, _ := configBase["browser"].(map[string]any)
	if browserCfg == nil {
		return ""
	}
	chromePath, ok := browserCfg["chrome_path"]
	if !ok {
		return ""
	}
	// 字符串形式：strip 后直取
	if s, ok := chromePath.(string); ok {
		return strings.TrimSpace(s)
	}
	// 平台 map 形式：win32/cygwin→windows、darwin→macos、linux/linux2→linux
	pathMap, ok := chromePath.(map[string]any)
	if !ok {
		return ""
	}
	osKey := "default"
	switch runtime.GOOS {
	case "windows":
		osKey = "windows"
	case "darwin":
		osKey = "macos"
	case "linux":
		osKey = "linux"
	}
	for _, key := range []string{osKey, "default"} {
		if v, ok := pathMap[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// buildBrowserSubagentParams 从配置构建 browser 子代理的 SubagentCreateParams。
//
// 对齐 Python _build_configured_subagents 中对 build_browser_agent_config 的调用：
// max_iterations=parse_int(browser_agent_cfg.get("max_iterations"), react_cfg.get("max_iterations", 15))。
//
// Python: interface_deep.py _build_configured_subagents
func (d *DeepAdapter) buildBrowserSubagentParams(config map[string]any, browserAgentCfg map[string]any) *hschema.SubagentCreateParams {
	resolvedLanguage := d.resolveRuntimeLanguage()

	// Python: parse_int(browser_agent_cfg.get("max_iterations"), react_cfg.get("max_iterations", 15))
	maxIterations := 0
	if browserAgentCfg != nil {
		maxIterations = parseIntAny(browserAgentCfg["max_iterations"], 0)
	}
	if maxIterations == 0 {
		maxIterations = parseIntAny(config["max_iterations"], 0)
	}
	if maxIterations == 0 {
		maxIterations = 15
	}

	return &hschema.SubagentCreateParams{
		Language:      resolvedLanguage,
		MaxIterations: maxIterations,
		// Python: workspace=self._workspace_dir or "./"
		Workspace: hworkspace.NewWorkspace(d.workspaceDir, resolvedLanguage),
	}
}

// firstNonEmptyStr 返回第一个非空字符串（对齐 Python 的 `a or b` 短路语义）。
func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseIntAny 安全解析整数（对齐 Python parse_int：None/空串/非法值返回 default）。
func parseIntAny(value any, def int) int {
	switch n := value.(type) {
	case nil:
		return def
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		if n == "" {
			return def
		}
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i
		}
	}
	return def
}
