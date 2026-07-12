package adapter

import (
	"context"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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
// ⤵️ agentcore: 需要完整的 subagent builder
func (d *DeepAdapter) buildConfiguredSubagents(config map[string]any, configBase map[string]any) ([]any, bool) {
	// ⤵️ agentcore: 实现 _build_configured_subagents
	// 对齐 Python: configured_subagents, should_add_general_agent = self._build_configured_subagents(model, config, config_base)
	return nil, false
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
	os.Setenv(envKey, value)
}
