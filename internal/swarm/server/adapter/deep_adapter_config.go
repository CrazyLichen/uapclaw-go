package adapter

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/sysop_builder"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentDefinition Agent 定义数据模型（与 runtime.AgentDefinition 对齐）。
// 定义在 adapter 包中以避免 adapter↔runtime 循环依赖。
// 对齐 Python: AgentDefinition dataclass
type AgentDefinition struct {
	// Name 名称
	Name string
	// Description 描述
	Description string
	// Prompt 系统提示词
	Prompt string
	// Source 来源
	Source string
	// FilePath 文件路径
	FilePath string
	// Model 模型名称
	Model string
	// Tools 允许的工具列表
	Tools []string
	// DisallowedTools 禁止的工具列表
	DisallowedTools []string
	// Skills 预加载 skill
	Skills []string
	// MaxIterations 最大迭代次数
	MaxIterations *int
	// WhenToUse 调度描述
	WhenToUse string
}

// AgentConfigLister Agent 配置列表接口（避免 adapter↔runtime 循环依赖）。
// 对齐 Python: AgentConfigService.list_agents()
type AgentConfigLister interface {
	// ListCustomAgents 列出自定义 agent（非 builtin）
	ListCustomAgents() []*AgentDefinition
}

// runtimeConfig 运行时配置。
// 对齐 Python: _RuntimeConfig (line 3098-3106)
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
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// updateRuntimeConfig 更新运行时配置。
// 对齐 Python: _update_runtime_config() (line 3098-3266)
//
// 包含 CWD 种子、language/channel 解析、runtime state 写入、rail/tool 模式切换。
func (d *DeepAdapter) updateRuntimeConfig(ctx context.Context, config *runtimeConfig) {
	if config == nil {
		return
	}

	// 步骤 1: CWD 种子
	if config.CWD != "" {
		d.seedRuntimeCwd(ctx, config.CWD)
	}

	// 步骤 2: language 解析
	if config.Language != "" {
		d.writeRuntimeState("language", config.Language)
	}

	// 步骤 3: channel 解析
	if config.Channel != "" {
		d.writeRuntimeState("channel", config.Channel)
	}

	// 步骤 4: project_dir 写入
	if config.ProjectDir != "" {
		d.writeRuntimeState("project_dir", config.ProjectDir)
	}

	// 步骤 5: workspace_dir 写入
	if config.WorkspaceDir != "" {
		d.writeRuntimeState("workspace_dir", config.WorkspaceDir)
	}

	// 步骤 6: rail/tool 模式切换
	// ⤵️ 10.6.3-10: updateRailsForMode + updatePromptForMode
	// 待实现：按模式更新Rails d.updateRailsForMode(d.mode)
	// 待实现：按模式更新提示词 d.updatePromptForMode(d.mode)

	logger.Info(logComponent).
		Str("cwd", config.CWD).
		Str("language", config.Language).
		Msg("updateRuntimeConfig 完成")
}

// buildConfiguredSubagents 构建子代理规格。
// 对齐 Python: _build_configured_subagents() (line 878-970)
//
// 根据 config 中 subagents 段的启用状态，调用各 builder 构建配置。
// 返回 (subagentSpecs, shouldAddGeneralAgent)。
// 对齐 Python: _build_configured_subagents() — Deep 模式下只直接构建
// research/browser/custom 子代理，explore/plan 通过 code_agent 间接注入
func (d *DeepAdapter) buildConfiguredSubagents(config map[string]any, configBase map[string]any) ([]hschema.SubagentSpec, bool) {
	var specs []hschema.SubagentSpec

	// 对齐 Python: 按 subagents 配置段构建
	subagentsCfg, _ := config["subagents"].(map[string]any)

	// ── general_agent: 配置控制，需 enabled:true 才启用（默认禁用）──
	shouldAddGeneralAgent := d.isSubagentExplicitlyEnabled(subagentsCfg, "general_agent")

	// ── research_agent: 配置控制，需 enabled:true 才启用（默认禁用）──
	if d.isSubagentExplicitlyEnabled(subagentsCfg, "research_agent") {
		cfg := subagents.BuildResearchAgentConfig(d.model, config, configBase)
		if cfg != nil {
			specs = append(specs, cfg)
		}
	}

	// ── browser_agent: 由 browser_runtime_enabled 控制 ──
	// TODO(#browser): 对齐 Python _browser_runtime_enabled() 检查
	if d.isSubagentExplicitlyEnabled(subagentsCfg, "browser_agent") {
		cfg := subagents.BuildBrowserAgentConfig(d.model, config, configBase)
		if cfg != nil {
			specs = append(specs, cfg)
		}
	}

	// ── 自定义 agent: 对齐 Python _load_custom_subagents ──
	// 对齐 Python: custom_specs = _load_custom_subagents(workspace_dir, subagents_cfg, model, workspace, ...)
	customSpecs := d.loadCustomSubagents(subagentsCfg)
	for _, spec := range customSpecs {
		specs = append(specs, spec)
	}

	logger.Info(logComponent).
		Int("subagent_count", len(specs)).
		Bool("should_add_general_agent", shouldAddGeneralAgent).
		Msg("buildConfiguredSubagents 完成")

	return specs, shouldAddGeneralAgent
}

// isSubagentExplicitlyEnabled 检查子代理是否通过配置显式启用。
// 对齐 Python: _is_subagent_enabled() — 只有 enabled:true 才启用，默认禁用
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
// 对齐 Python: _load_custom_subagents(workspace_dir, subagents_cfg, model, workspace, ...)
//
// 仅加载 enabled:true 的自定义 agent（非 builtin）。
func (d *DeepAdapter) loadCustomSubagents(subagentsCfg map[string]any) []hschema.SubagentSpec {
	// 对齐 Python: agent_service = AgentConfigService(workspace_dir)
	// 通过 d.configLister 获取自定义 agent 列表（依赖注入，避免循环依赖）
	if d.configLister == nil {
		return nil
	}
	customAgents := d.configLister.ListCustomAgents()

	var result []hschema.SubagentSpec
	// 对齐 Python: for agent_def in agent_service.list_agents():
	for _, agentDef := range customAgents {
		// 对齐 Python: if agent_def.source == "builtin": continue
		// （ListCustomAgents 已过滤 builtin）

		// 对齐 Python: subagent_cfg = subagents_cfg.get(agent_def.name)
		// 只有显式 enabled: true 才加载
		if !d.isSubagentExplicitlyEnabled(subagentsCfg, agentDef.Name) {
			continue
		}

		// 对齐 Python: custom_spec = _agent_def_to_subagent_config(agent_def, model, workspace, model_cache)
		spec := agentDefToSubagentConfig(agentDef, d.model, d.modelCache)
		if spec != nil {
			result = append(result, spec)
			logger.Info(logComponent).
				Str("agent_name", agentDef.Name).
				Str("source", agentDef.Source).
				Msg("loaded custom agent")
		}
	}
	return result
}

// agentDefToSubagentConfig 将 AgentDefinition 转换为 SubAgentConfig。
// 对齐 Python: _agent_def_to_subagent_config(agent_def, model, workspace, model_cache)
func agentDefToSubagentConfig(agentDef *AgentDefinition, model *llm.Model, modelCache map[string]*llm.Model) *hschema.SubAgentConfig {
	// 步骤 1: 解析模型
	// 对齐 Python: resolved_model = model; if agent_def.model and isinstance(model_cache, dict): resolved_model = model_cache.get(agent_def.model, model)
	resolvedModel := model
	if agentDef.Model != "" && modelCache != nil {
		if cached, ok := modelCache[agentDef.Model]; ok {
			resolvedModel = cached
		}
	}

	// 步骤 2: 构建工具列表
	// 对齐 Python: tools = list(agent_def.tools) if agent_def.tools else ["*"]
	// if agent_def.disallowed_tools and tools != ["*"]: tools = [t for t in tools if t not in agent_def.disallowed_tools]
	tools := agentDef.Tools
	if len(tools) == 0 {
		tools = []string{"*"}
	}
	if len(agentDef.DisallowedTools) > 0 && !(len(tools) == 1 && tools[0] == "*") {
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
		Model:          resolvedModel,
		Skills:         agentDef.Skills,
		MaxIterations:  maxIter,
		EnableTaskLoop: true,
		FactoryName:    "custom_" + agentDef.Name,
	}
}

// writeRuntimeState 将键值对写入运行时状态。
// 对齐 Python: _write_runtime_state(key, value)
func (d *DeepAdapter) writeRuntimeState(key string, value string) {
	// 对齐 Python: os.environ[f"JCLAW_RUNTIME_{key.upper()}"] = value
	envKey := "JCLAW_RUNTIME_" + strings.ToUpper(key)
	_ = os.Setenv(envKey, value)
}

// skillIncludeToolsForProfile 检查当前 profile 下 skill 是否包含工具。
// 对齐 Python: _skill_include_tools_for_profile() (line 722-726)
// 逻辑：ACP tool profile 下不包含工具；否则取决于 filesystemRail 是否为 nil。
func (d *DeepAdapter) skillIncludeToolsForProfile(instanceOverrides map[string]any) bool {
	if d.isAcpToolProfile(instanceOverrides) {
		return false
	}
	return d.filesystemRail == nil
}

// resolvePromptChannel 从 sessionID 解析 prompt channel。
// 对齐 Python: _resolve_prompt_channel(session_id) (line 728-748)
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
// 对齐 Python: _resolve_model_name() (line 750-755)
// 逻辑：从 modelRequestConfig.modelName 获取，默认 "unknown"。
func (d *DeepAdapter) resolveModelName() string {
	if d.modelRequestConfig != nil && d.modelRequestConfig.ModelName != "" {
		return d.modelRequestConfig.ModelName
	}
	return "unknown"
}

// isAcpToolProfile 检查是否为 ACP Tool profile。
// 对齐 Python: _is_acp_tool_profile(config) (L709-716)
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
// 对齐 Python: _filesystem_rail_enabled_for_profile() — 读取 enable_filesystem_rail 配置，默认 true
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
// 对齐 Python: _resolve_runtime_language() — 调用 resolve_language() 标准化
func (d *DeepAdapter) resolveRuntimeLanguage() string {
	return prompts.ResolveLanguage(d.resolvePromptLanguage())
}

// resolvePromptLanguage 解析提示词语言（原始配置值）。
// 对齐 Python: _resolve_prompt_language() — 读取 preferred_language，默认 "zh"
func (d *DeepAdapter) resolvePromptLanguage() string {
	// 对齐 Python: config_base.get("preferred_language", "zh")
	if v, ok := d.configCache["preferred_language"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return strings.TrimSpace(strings.ToLower(s))
		}
	}
	return "zh"
}

// createSysOperation 创建系统操作实例。
// 对齐 Python: _create_sys_operation() (line 2262-2320)
//
// 根据配置决定使用 local 或 sandbox 模式，
// 通过 sysop_builder 构建卡片和实例。
func (d *DeepAdapter) createSysOperation(configBase map[string]any) (sysop.SysOperation, *sysop.SysOperationCard) {
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
					Msg("sandbox runtime 字段类型断言失败，使用默认值")
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
					Msg("sandbox runtime 字段类型断言失败，使用默认值")
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
					Msg("sandbox runtime 字段类型断言失败，使用默认值")
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
		Msg("createSysOperation 完成")

	return instance, card
}

// resolveProjectDirForSandbox 解析沙箱挂载用的项目目录。
// 对齐 Python: _resolve_project_dir_for_sandbox()
func (d *DeepAdapter) resolveProjectDirForSandbox() string {
	if d.projectDir != "" {
		return d.projectDir
	}
	return ""
}

// getSandboxRuntime 从配置获取沙箱运行时参数。
// 对齐 Python: get_sandbox_runtime() + get_config()["sandbox"]
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
