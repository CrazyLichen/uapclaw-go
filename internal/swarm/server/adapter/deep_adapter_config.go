package adapter

import (
	"context"
	"os"
	"strings"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/sysop_builder"
)

// ──────────────────────────── 结构体 ────────────────────────────

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
	// d.updateRailsForMode(d.mode)
	// d.updatePromptForMode(d.mode)

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
func (d *DeepAdapter) buildConfiguredSubagents(config map[string]any, configBase map[string]any) ([]hschema.SubagentSpec, bool) {
	var specs []hschema.SubagentSpec

	// 对齐 Python: 按 subagents 配置段依次构建
	// explore 和 plan 默认启用，其余按配置

	if d.isSubagentEnabled(config, "explore") {
		cfg := subagents.BuildExploreAgentConfig(d.model, config, configBase)
		if cfg != nil {
			specs = append(specs, cfg)
		}
	}

	if d.isSubagentEnabled(config, "plan") {
		cfg := subagents.BuildPlanAgentConfig(d.model, config, configBase)
		if cfg != nil {
			specs = append(specs, cfg)
		}
	}

	if d.isSubagentEnabled(config, "research") {
		cfg := subagents.BuildResearchAgentConfig(d.model, config, configBase)
		if cfg != nil {
			specs = append(specs, cfg)
		}
	}

	if d.isSubagentEnabled(config, "code") {
		cfg := subagents.BuildCodeAgentConfig(d.model, config, configBase)
		if cfg != nil {
			specs = append(specs, cfg)
		}
	}

	if d.isSubagentEnabled(config, "browser") {
		cfg := subagents.BuildBrowserAgentConfig(d.model, config, configBase)
		if cfg != nil {
			specs = append(specs, cfg)
		}
	}

	// shouldAddGeneralAgent: 当 subMode=="plan" 或 mode 以 "agent" 开头时为 true
	shouldAddGeneralAgent := (d.subMode == "plan") || (d.mode != "" && strings.HasPrefix(d.mode, "agent"))

	logger.Info(logComponent).
		Int("subagent_count", len(specs)).
		Bool("should_add_general_agent", shouldAddGeneralAgent).
		Msg("buildConfiguredSubagents 完成")

	return specs, shouldAddGeneralAgent
}

// isSubagentEnabled 检查配置中子代理是否启用。
// 对齐 Python: _is_subagent_enabled()
func (d *DeepAdapter) isSubagentEnabled(config map[string]any, name string) bool {
	subagents, _ := config["subagents"].(map[string]any)
	if subagents == nil {
		return d.isSubagentDefaultEnabled(name)
	}
	if v, ok := subagents[name]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return d.isSubagentDefaultEnabled(name)
}

// isSubagentDefaultEnabled 检查子代理是否默认启用。
// 对齐 Python: _is_subagent_default_enabled()
func (d *DeepAdapter) isSubagentDefaultEnabled(name string) bool {
	// 对齐 Python: 默认启用的子代理列表
	defaultEnabled := map[string]bool{
		"explore": true,
		"plan":    true,
	}
	return defaultEnabled[name]
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
func (d *DeepAdapter) skillIncludeToolsForProfile(configBase map[string]any) bool {
	if d.isAcpToolProfile(configBase) {
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
// 对齐 Python: _is_acp_tool_profile()
func (d *DeepAdapter) isAcpToolProfile(configBase map[string]any) bool {
	modelsSection, _ := configBase["models"].(map[string]any)
	if modelsSection == nil {
		return false
	}
	defaults, _ := modelsSection["defaults"].([]any)
	for _, entry := range defaults {
		if m, ok := entry.(map[string]any); ok {
			if profile, ok := m["profile"].(string); ok && profile == "acp_tool" {
				return true
			}
		}
	}
	return false
}

// filesystemRailEnabledForProfile 检查当前 profile 是否启用文件系统护栏。
// 对齐 Python: _filesystem_rail_enabled_for_profile()
func (d *DeepAdapter) filesystemRailEnabledForProfile(configBase map[string]any) bool {
	// 对齐 Python: 默认 true，除非 isAcpToolProfile
	return !d.isAcpToolProfile(configBase)
}

// resolveRuntimeLanguage 解析运行时语言。
// 对齐 Python: _resolve_runtime_language()
func (d *DeepAdapter) resolveRuntimeLanguage() string {
	if v, ok := d.configCache["language"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "zh"
}

// resolvePromptLanguage 解析提示词语言。
// 对齐 Python: _resolve_prompt_language()
func (d *DeepAdapter) resolvePromptLanguage() string {
	if v, ok := d.configCache["prompt_language"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return d.resolveRuntimeLanguage()
}

// createSysOperation 创建系统操作实例。
// 对齐 Python: _create_sys_operation() (line 2262-2320)
//
// 根据配置决定使用 local 或 sandbox 模式，
// 通过 sysop_builder 构建卡片和实例。
func (d *DeepAdapter) createSysOperation(configBase map[string]any) (sysop.SysOperation, *sysop.SysOperationCard) {
	// 解析操作模式：默认 local
	// ⤵️ 10.3.7-11: 从 configBase["sys_operation"]["mode"] 解析
	mode := sysop.OperationModeLocal
	if sysOpSection, ok := configBase["sys_operation"].(map[string]any); ok {
		if m, ok := sysOpSection["mode"].(string); ok {
			mode = sysop.FromOperationModeString(m)
		}
	}

	var card *sysop.SysOperationCard
	switch mode {
	case sysop.OperationModeSandbox:
		// ⤵️ 10.3.7-11: 沙箱模式
		card = sysop_builder.CreateSandboxSysOpCard(d.projectDir, d.workspaceDir, d.isCodeAgent)
	default:
		// 本地模式
		card = sysop_builder.CreateLocalSysOpCard(d.projectDir, d.workspaceDir, d.isCodeAgent)
	}

	// 从卡片创建 SysOperation 实例
	instance := sysop_builder.CreateSysOperationFromCard(card)

	d.sysOperation = instance
	d.sysOperationCard = card

	logger.Info(logComponent).
		Str("mode", mode.String()).
		Msg("createSysOperation 完成")

	return instance, card
}
