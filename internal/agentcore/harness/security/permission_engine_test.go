package security

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewPermissionEngine 测试创建权限引擎
func TestNewPermissionEngine(t *testing.T) {
	// 有配置
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "/workspace")
	assert.True(t, engine.Enabled())
	assert.Equal(t, "/workspace", engine.workspaceRoot)

	// 无配置
	engine2 := NewPermissionEngine(nil, nil, "", "")
	assert.True(t, engine2.Enabled())

	// 明确禁用
	engine3 := NewPermissionEngine(map[string]any{"enabled": false}, nil, "", "")
	assert.False(t, engine3.Enabled())
}

// TestPermissionEngine_UpdateConfig 测试热更新配置
func TestPermissionEngine_UpdateConfig(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "/workspace")
	assert.True(t, engine.Enabled())

	engine.UpdateConfig(map[string]any{"enabled": false})
	assert.False(t, engine.Enabled())

	engine.UpdateConfig(nil)
	assert.True(t, engine.Enabled()) // nil → 默认启用
}

// TestPermissionEngine_EnabledFalse 允许 系统禁用时 → 允许
func TestPermissionEngine_EnabledFalse(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": false}, nil, "", "")
	result := engine.CheckPermission(context.Background(), "bash", map[string]any{"command": "ls"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelAllow, result.Permission)
	assert.Contains(t, result.Reason, "已禁用")
}

// TestPermissionEngine_ChecksInactive 宿主说不要校验 → 允许
func TestPermissionEngine_ChecksInactive(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "/workspace")
	engine.SetPermissionChecksActive(func() bool { return false })

	result := engine.CheckPermission(context.Background(), "bash", map[string]any{"command": "ls"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelAllow, result.Permission)
	assert.Contains(t, result.Reason, "未启用")
}

// TestPermissionEngine_TieredPolicyDeny TieredPolicy DENY → DENY
func TestPermissionEngine_TieredPolicyDeny(t *testing.T) {
	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"bash": "deny",
		},
	}
	engine := NewPermissionEngine(config, nil, "", "")

	result := engine.CheckPermission(context.Background(), "bash", map[string]any{"command": "ls"})
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
	engine := NewPermissionEngine(config, nil, "", "")

	result := engine.CheckPermission(context.Background(), "read_file", map[string]any{"path": "/home/user/file.txt"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelAllow, result.Permission)
}

// TestPermissionEngine_NoConfig 无匹配 → 默认 ASK
func TestPermissionEngine_NoConfig(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "")

	result := engine.CheckPermission(context.Background(), "read_file", map[string]any{"path": "/home/user/file.txt"})
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
	engine := NewPermissionEngine(config, nil, "", "")

	permission, matchedRule, err := engine.EvaluateGlobalPolicyDirectly("bash", map[string]any{"command": "ls"}, false)
	assert.NoError(t, err)
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
	engine := NewPermissionEngine(config, nil, "", "")

	permission, matchedRule, err := engine.CheckToolPermissionDirectly("read_file", map[string]any{"path": "/home/user/file.txt"})
	assert.NoError(t, err)
	assert.Equal(t, PermissionLevelAllow, permission)
	assert.Contains(t, matchedRule, "defaults")
}

// TestPermissionEngine_SetWorkspaceRoot 设置工作空间
func TestPermissionEngine_SetWorkspaceRoot(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "")
	assert.Equal(t, "", engine.workspaceRoot)

	engine.SetWorkspaceRoot("/new/workspace")
	assert.Equal(t, "/new/workspace", engine.workspaceRoot)
	require.NotNil(t, engine.externalChecker)
}

// TestGetReason 测试 reason 生成
func TestGetReason(t *testing.T) {
	assert.Contains(t, getReason(PermissionLevelAllow, "bash", "test_rule"), "放行")
	assert.Contains(t, getReason(PermissionLevelDeny, "bash", "test_rule"), "拒绝")
	assert.Contains(t, getReason(PermissionLevelAsk, "bash", "test_rule"), "需审批")
}

// TestPermissionEngine_UpdateLLM 测试 UpdateLLM 热更新模型
func TestPermissionEngine_UpdateLLM(t *testing.T) {
	engine := NewPermissionEngine(nil, nil, "", "/workspace")
	engine.UpdateLLM(nil, "test-model")
	assert.Equal(t, "test-model", engine.modelName)
	assert.Nil(t, engine.llm)
}

// TestPermissionEngine_SetSceneHook 测试 SetSceneHook
func TestPermissionEngine_SetSceneHook(t *testing.T) {
	engine := NewPermissionEngine(nil, nil, "", "/workspace")
	engine.SetSceneHook(func(input PermissionSceneHookInput) ([]string, error) {
		return nil, nil
	})
	assert.NotNil(t, engine.sceneHook)
}

// TestPermissionEngine_SceneHookApprove 场景钩子批准
func TestPermissionEngine_SceneHookApprove(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "/workspace")
	engine.SetSceneHook(func(input PermissionSceneHookInput) ([]string, error) {
		return []string{"approve"}, nil
	})

	result := engine.CheckPermission(context.Background(), "bash", map[string]any{"command": "ls"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelAllow, result.Permission)
	assert.Contains(t, result.Reason, "场景钩子放行")
}

// TestPermissionEngine_SceneHookReject 场景钩子拒绝
func TestPermissionEngine_SceneHookReject(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "/workspace")
	engine.SetSceneHook(func(input PermissionSceneHookInput) ([]string, error) {
		return []string{"reject", "自定义拒绝原因"}, nil
	})

	result := engine.CheckPermission(context.Background(), "bash", map[string]any{"command": "ls"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelDeny, result.Permission)
	assert.Equal(t, "自定义拒绝原因", result.Reason)
}

// TestPermissionEngine_SceneHookReject无消息 场景钩子拒绝无额外消息
func TestPermissionEngine_SceneHookReject无消息(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "/workspace")
	engine.SetSceneHook(func(input PermissionSceneHookInput) ([]string, error) {
		return []string{"reject"}, nil
	})

	result := engine.CheckPermission(context.Background(), "bash", map[string]any{"command": "ls"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelDeny, result.Permission)
	assert.Contains(t, result.Reason, "操作不被允许")
}

// TestPermissionEngine_SceneHookError 场景钩子报错时继续分层评估
func TestPermissionEngine_SceneHookError(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "/workspace")
	engine.SetSceneHook(func(input PermissionSceneHookInput) ([]string, error) {
		return nil, fmt.Errorf("scene hook error")
	})

	result := engine.CheckPermission(context.Background(), "read_file", map[string]any{"path": "/workspace/file.txt"})
	require.NotNil(t, result)
	// 钩子报错后继续正常评估
	assert.NotEqual(t, PermissionLevelNone, result.Permission)
}

// TestPermissionEngine_SceneHookEmpty 场景钩子返回空列表
func TestPermissionEngine_SceneHookEmpty(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "/workspace")
	engine.SetSceneHook(func(input PermissionSceneHookInput) ([]string, error) {
		return []string{}, nil
	})

	result := engine.CheckPermission(context.Background(), "read_file", map[string]any{"path": "/workspace/file.txt"})
	require.NotNil(t, result)
	// 空列表继续正常评估
	assert.NotEqual(t, PermissionLevelNone, result.Permission)
}

// TestPermissionEngine_NilToolArgs nil toolArgs 不 panic
func TestPermissionEngine_NilToolArgs(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "/workspace")

	result := engine.CheckPermission(context.Background(), "read_file", nil)
	require.NotNil(t, result)
	assert.NotEqual(t, PermissionLevelNone, result.Permission)
}

// TestPermissionEngine_EvaluateGlobalPolicyDirectly_无配置 无配置时返回默认
func TestPermissionEngine_EvaluateGlobalPolicyDirectly_无配置(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "")

	permission, matchedRule, err := engine.EvaluateGlobalPolicyDirectly("read_file", map[string]any{"path": "/home"}, false)
	assert.NoError(t, err)
	_ = permission
	_ = matchedRule
}

// TestPermissionEngine_EvaluateGlobalPolicyDirectly_includeExternal 包含外部目录
func TestPermissionEngine_EvaluateGlobalPolicyDirectly_includeExternal(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "/workspace")

	permission, matchedRule, err := engine.EvaluateGlobalPolicyDirectly("read_file", map[string]any{"path": "/workspace/file.txt"}, true)
	assert.NoError(t, err)
	_ = permission
	_ = matchedRule
}

// TestPermissionEngine_EvaluateGlobalPolicyDirectly_nilArgs nil参数
func TestPermissionEngine_EvaluateGlobalPolicyDirectly_nilArgs(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", "")

	permission, _, err := engine.EvaluateGlobalPolicyDirectly("bash", nil, false)
	assert.NoError(t, err)
	_ = permission
}

// TestPermissionEngine_Config 测试 Config 返回当前配置
func TestPermissionEngine_Config(t *testing.T) {
	cfg := map[string]any{"enabled": true}
	engine := NewPermissionEngine(cfg, nil, "", "/workspace")
	assert.Equal(t, cfg, engine.Config())
}
