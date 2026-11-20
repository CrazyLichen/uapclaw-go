package subagents_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	hcua "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/cua"
)

// TestBuildCuaAgentConfig_默认参数 默认授权（input+app_lifecycle，core 恒补）、
// 默认卡片、默认提示词、factory_name 与 factory_kwargs 形状。
//
// Python: build_cua_agent_config (cua_agent.py:220-290)。
func TestBuildCuaAgentConfig_默认参数(t *testing.T) {
	cfg, err := subagents.BuildCuaAgentConfig(nil, &subagents.CuaAgentConfigParams{})
	require.NoError(t, err)

	assert.Equal(t, subagents.CuaAgentFactoryName, cfg.FactoryName)
	require.NotNil(t, cfg.AgentCard)
	assert.Equal(t, "cua_agent", cfg.AgentCard.GetName())
	assert.Equal(t, 25, cfg.MaxIterations) // Python keyword 默认 max_iterations=25
	assert.Equal(t, "cn", cfg.Language)    // resolve_language 缺省 cn

	// factory_kwargs 形状：未提供 permissions/permission_host 时不含这两个键
	require.NotNil(t, cfg.FactoryKwargs)
	assert.Contains(t, cfg.FactoryKwargs, "cua_capabilities")
	assert.Contains(t, cfg.FactoryKwargs, "cua_instance_key")
	assert.Contains(t, cfg.FactoryKwargs, "cua_snapshot_keep_last_k")
	assert.Contains(t, cfg.FactoryKwargs, "cua_pause_on_user_input")
	assert.NotContains(t, cfg.FactoryKwargs, "permissions")
	assert.NotContains(t, cfg.FactoryKwargs, "permission_host")

	// 默认提示词不含 kill_app 后缀（app_termination 未授予）
	assert.NotContains(t, cfg.SystemPrompt, "kill_app 只在协作式关闭失败后使用")
}

// TestBuildCuaAgentConfig_快照保留数校验 keep_last_k < 1 在配置期报错。
//
// Python: _validate_snapshot_keep_last_k (cua_agent.py:208-215)。
func TestBuildCuaAgentConfig_快照保留数校验(t *testing.T) {
	_, err := subagents.BuildCuaAgentConfig(nil, &subagents.CuaAgentConfigParams{CuaSnapshotKeepLastK: -1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cua_snapshot_keep_last_k must be an int >= 1")
}

// TestBuildCuaAgentConfig_未知能力报错 未知名被拒绝并列出可用能力。
//
// Python: _resolve_capability_selection (cua_agent.py:193-205)。
func TestBuildCuaAgentConfig_未知能力报错(t *testing.T) {
	_, err := subagents.BuildCuaAgentConfig(nil, &subagents.CuaAgentConfigParams{
		CuaCapabilities: []string{"input", "no_such_capability"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unsupported cua capabilities: no_such_capability")
	assert.Contains(t, err.Error(), "Available capabilities:")
}

// TestBuildCuaAgentConfig_killApp后缀 app_termination 授予后提示词追加后缀
// 且 factory_kwargs 带上 permissions（提供时才出现）。
func TestBuildCuaAgentConfig_killApp后缀(t *testing.T) {
	cfg, err := subagents.BuildCuaAgentConfig(nil, &subagents.CuaAgentConfigParams{
		CuaCapabilities: []string{"input", "app_lifecycle", "app_termination"},
		Permissions:     map[string]any{"mode": "ask"},
	})
	require.NoError(t, err)
	assert.True(t, strings.Contains(cfg.SystemPrompt, "kill_app 只在协作式关闭失败后使用") ||
		strings.Contains(cfg.SystemPrompt, "Use kill_app only after"),
		"授予 app_termination 后应追加 kill_app 后缀")
	assert.Contains(t, cfg.FactoryKwargs, "permissions")
}

// TestWithCuaPermissionRules_桌面规则合入 桌面三规则追加到调用方规则之后，
// 且调用方原 map 不被修改（深拷贝）。
//
// Python: _with_cua_permission_rules (cua_agent.py:174-178)；消费方
// create_cua_agent :317-319（物化半区 ⤴️ 领域十八）。
func TestWithCuaPermissionRules_桌面规则合入(t *testing.T) {
	permissions := map[string]any{"mode": "ask", "rules": []map[string]any{{"id": "r1"}}}
	merged := subagents.WithCuaPermissionRules(permissions)

	rules, ok := merged["rules"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, rules, 4) // r1 + 桌面三规则
	assert.Equal(t, "r1", rules[0]["id"])
	assert.Equal(t, "cua_desktop_force_terminate_app", rules[1]["id"])
	assert.Equal(t, "cua_desktop_launch_app", rules[2]["id"])
	assert.Equal(t, "cua_desktop_type_text", rules[3]["id"])
	// 调用方原 map 未被污染
	assert.Len(t, permissions["rules"].([]map[string]any), 1)
}

// TestResolveCuaCapabilities_core恒补且允许清单确定 core 能力恒在、
// 允许清单按目录顺序展开并稳定去重。
//
// Python: resolve_cua_capabilities (cua_capabilities.py:124-155)。
func TestResolveCuaCapabilities_core恒补且允许清单确定(t *testing.T) {
	resolved, err := hcua.ResolveCuaCapabilities(nil, nil)
	require.NoError(t, err)
	// nil 请求 → 空选择，仅 core（默认授权替换发生在
	// _resolve_capability_selection，不在解析器内——对齐 Python :139-140）
	assert.Equal(t, []string{"core"}, resolved.SelectedNames)
	assert.Empty(t, resolved.RequestedNames)
	assert.Empty(t, resolved.RejectedNames)
	// core 工具在前（目录顺序）
	assert.Equal(t, "get_cursor_position", resolved.AllowedToolNames[0])
}
