package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewPermissionEngine 测试创建权限引擎
func TestNewPermissionEngine(t *testing.T) {
	// 有配置
	engine := NewPermissionEngine(map[string]any{"enabled": true}, "/workspace")
	assert.True(t, engine.Enabled())
	assert.Equal(t, "/workspace", engine.workspaceRoot)

	// 无配置
	engine2 := NewPermissionEngine(nil, "")
	assert.True(t, engine2.Enabled())

	// 明确禁用
	engine3 := NewPermissionEngine(map[string]any{"enabled": false}, "")
	assert.False(t, engine3.Enabled())
}

// TestPermissionEngine_UpdateConfig 测试热更新配置
func TestPermissionEngine_UpdateConfig(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, "/workspace")
	assert.True(t, engine.Enabled())

	engine.UpdateConfig(map[string]any{"enabled": false})
	assert.False(t, engine.Enabled())

	engine.UpdateConfig(nil)
	assert.True(t, engine.Enabled()) // nil → 默认启用
}

// TestPermissionEngine_EnabledFalse 允许 系统禁用时 → 允许
func TestPermissionEngine_EnabledFalse(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": false}, "")
	result := engine.CheckPermission("bash", map[string]any{"command": "ls"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelAllow, result.Permission)
	assert.Contains(t, result.Reason, "disabled")
}

// TestPermissionEngine_ChecksInactive 宿主说不要校验 → 允许
func TestPermissionEngine_ChecksInactive(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, "/workspace")
	engine.SetPermissionChecksActive(func() bool { return false })

	result := engine.CheckPermission("bash", map[string]any{"command": "ls"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelAllow, result.Permission)
	assert.Contains(t, result.Reason, "inactive")
}

// TestPermissionEngine_TieredPolicyDeny TieredPolicy DENY → DENY
func TestPermissionEngine_TieredPolicyDeny(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "deny",
		},
	}
	engine := NewPermissionEngine(config, "")

	result := engine.CheckPermission("bash", map[string]any{"command": "ls"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelDeny, result.Permission)
}

// TestPermissionEngine_DefaultsAllow defaults.* → ALLOW
func TestPermissionEngine_DefaultsAllow(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"defaults": map[string]any{
			"*": "allow",
		},
	}
	engine := NewPermissionEngine(config, "")

	result := engine.CheckPermission("read_file", map[string]any{"path": "/home/user/file.txt"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelAllow, result.Permission)
}

// TestPermissionEngine_NoConfig 无匹配 → 默认 ASK
func TestPermissionEngine_NoConfig(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, "")

	result := engine.CheckPermission("read_file", map[string]any{"path": "/home/user/file.txt"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelAsk, result.Permission)
}

// TestPermissionEngine_EvaluateGlobalPolicyDirectly 直接评估
func TestPermissionEngine_EvaluateGlobalPolicyDirectly(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "deny",
		},
	}
	engine := NewPermissionEngine(config, "")

	permission, matchedRule := engine.EvaluateGlobalPolicyDirectly("bash", map[string]any{"command": "ls"}, false)
	assert.Equal(t, PermissionLevelDeny, permission)
	assert.Contains(t, matchedRule, "tools.bash")
}

// TestPermissionEngine_CheckToolPermissionDirectly 直接检查
func TestPermissionEngine_CheckToolPermissionDirectly(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"defaults": map[string]any{
			"*": "allow",
		},
	}
	engine := NewPermissionEngine(config, "")

	permission, matchedRule := engine.CheckToolPermissionDirectly("read_file", map[string]any{"path": "/home/user/file.txt"})
	assert.Equal(t, PermissionLevelAllow, permission)
	assert.Contains(t, matchedRule, "defaults")
}

// TestPermissionEngine_SetWorkspaceRoot 设置工作空间
func TestPermissionEngine_SetWorkspaceRoot(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, "")
	assert.Equal(t, "", engine.workspaceRoot)

	engine.SetWorkspaceRoot("/new/workspace")
	assert.Equal(t, "/new/workspace", engine.workspaceRoot)
	require.NotNil(t, engine.externalChecker)
}

// TestGetReason 测试 reason 生成
func TestGetReason(t *testing.T) {
	assert.Contains(t, getReason(PermissionLevelAllow, "bash", "test_rule"), "Allowed")
	assert.Contains(t, getReason(PermissionLevelDeny, "bash", "test_rule"), "Denied")
	assert.Contains(t, getReason(PermissionLevelAsk, "bash", "test_rule"), "Approval required")
}
