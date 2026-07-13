package sysop_builder

import (
	"os"
	"path/filepath"
	"testing"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── CreateLocalSysOpCard 测试 ────────────────────────────

func TestCreateLocalSysOpCard(t *testing.T) {
	card := CreateLocalSysOpCard()
	require.NotNil(t, card)
	assert.Equal(t, sysop.OperationModeLocal, card.Mode)
	assert.NotNil(t, card.WorkConfig)
}

// ──────────────────────────── CreateSandboxSysOpCard 测试 ────────────────────────────

func TestCreateSandboxSysOpCard_基本参数(t *testing.T) {
	card := CreateSandboxSysOpCard(
		"http://127.0.0.1:8321", "jiuwenbox",
		nil, nil, nil, nil, "", false,
	)
	require.NotNil(t, card)
	assert.Equal(t, sysop.OperationModeSandbox, card.Mode)
	require.NotNil(t, card.GatewayConfig)
	require.NotNil(t, card.GatewayConfig.LauncherConfig)
	assert.Equal(t, "http://127.0.0.1:8321", card.GatewayConfig.LauncherConfig.BaseURL)
	assert.Equal(t, "jiuwenbox", card.GatewayConfig.LauncherConfig.SandboxType)
}

func TestCreateSandboxSysOpCard_含IdleTTL(t *testing.T) {
	idleTTL := 600
	idleCheck := 60
	card := CreateSandboxSysOpCard(
		"http://127.0.0.1:8321", "jiuwenbox",
		nil, nil, &idleTTL, &idleCheck, "", false,
	)
	require.NotNil(t, card)
	require.NotNil(t, card.GatewayConfig.LauncherConfig)
	assert.NotNil(t, card.GatewayConfig.LauncherConfig.IdleTTLSeconds)
	assert.Equal(t, 600, *card.GatewayConfig.LauncherConfig.IdleTTLSeconds)

	// idle_check_interval 应在 extraParams 中
	extra := card.GatewayConfig.LauncherConfig.ExtraParams
	require.NotNil(t, extra)
	ici, ok := extra["idle_check_interval"]
	assert.True(t, ok)
	assert.Equal(t, 60, ici)
}

func TestCreateSandboxSysOpCard_含ExcludedCommands(t *testing.T) {
	excluded := []string{"rm", "mkfs"}
	card := CreateSandboxSysOpCard(
		"http://127.0.0.1:8321", "jiuwenbox",
		nil, excluded, nil, nil, "", false,
	)
	require.NotNil(t, card)
	extra := card.GatewayConfig.LauncherConfig.ExtraParams
	require.NotNil(t, extra)
	ec, ok := extra["excluded_commands"]
	assert.True(t, ok)
	assert.Equal(t, excluded, ec)
}

func TestCreateSandboxSysOpCard_含FilesRuntime(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(file, []byte("test"), 0o666))

	filesRuntime := map[string]any{
		"allow": []any{file},
	}
	card := CreateSandboxSysOpCard(
		"http://127.0.0.1:8321", "jiuwenbox",
		filesRuntime, nil, nil, nil, dir, true,
	)
	require.NotNil(t, card)

	// policy 应在 extraParams 中
	extra := card.GatewayConfig.LauncherConfig.ExtraParams
	require.NotNil(t, extra)
	policy, ok := extra["policy"]
	assert.True(t, ok)
	assert.NotNil(t, policy)
}

func TestCreateSandboxSysOpCard_Policy失败返回Nil(t *testing.T) {
	filesRuntime := map[string]any{
		"allow": []any{"/nonexistent_path_12345"},
	}
	card := CreateSandboxSysOpCard(
		"http://127.0.0.1:8321", "jiuwenbox",
		filesRuntime, nil, nil, nil, "", false,
	)
	assert.Nil(t, card)
}

// ──────────────────────────── CreateSysOperationFromCard 测试 ────────────────────────────

func TestCreateSysOperationFromCard_NilCard(t *testing.T) {
	result := CreateSysOperationFromCard(nil)
	assert.Nil(t, result)
}

func TestCreateSysOperationFromCard_LocalCard(t *testing.T) {
	card := CreateLocalSysOpCard()
	result := CreateSysOperationFromCard(card)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Card())
}

func TestCreateSysOperationFromCard_SandboxCard(t *testing.T) {
	card := CreateSandboxSysOpCard(
		"http://127.0.0.1:8321", "jiuwenbox",
		nil, nil, nil, nil, "", false,
	)
	require.NotNil(t, card)
	result := CreateSysOperationFromCard(card)
	assert.NotNil(t, result)
}

// ──────────────────────────── getIntPtr / getStrSlice 测试 ────────────────────────────

func TestGetIntPtr(t *testing.T) {
	t.Run("nil map 返回 nil", func(t *testing.T) {
		assert.Nil(t, getIntPtr(nil, "key"))
	})

	t.Run("key 不存在返回 nil", func(t *testing.T) {
		assert.Nil(t, getIntPtr(map[string]any{}, "key"))
	})

	t.Run("int 值", func(t *testing.T) {
		result := getIntPtr(map[string]any{"key": 42}, "key")
		require.NotNil(t, result)
		assert.Equal(t, 42, *result)
	})

	t.Run("float64 值", func(t *testing.T) {
		result := getIntPtr(map[string]any{"key": float64(600)}, "key")
		require.NotNil(t, result)
		assert.Equal(t, 600, *result)
	})

	t.Run("非数字值返回 nil", func(t *testing.T) {
		assert.Nil(t, getIntPtr(map[string]any{"key": "not_a_number"}, "key"))
	})
}

func TestGetStrSlice(t *testing.T) {
	t.Run("nil map 返回 nil", func(t *testing.T) {
		assert.Nil(t, getStrSlice(nil, "key"))
	})

	t.Run("key 不存在返回 nil", func(t *testing.T) {
		assert.Nil(t, getStrSlice(map[string]any{}, "key"))
	})

	t.Run("[]string 值", func(t *testing.T) {
		result := getStrSlice(map[string]any{"key": []string{"a", "b"}}, "key")
		assert.Equal(t, []string{"a", "b"}, result)
	})

	t.Run("[]any 值", func(t *testing.T) {
		result := getStrSlice(map[string]any{"key": []any{"a", "b"}}, "key")
		assert.Equal(t, []string{"a", "b"}, result)
	})

	t.Run("非切片值返回 nil", func(t *testing.T) {
		assert.Nil(t, getStrSlice(map[string]any{"key": 42}, "key"))
	})
}

// ──────────────────────────── getRegisteredSysOpByIsolationKey 测试 ────────────────────────────

func TestGetRegisteredSysOpByIsolationKey(t *testing.T) {
	t.Run("空 key 返回 nil", func(t *testing.T) {
		result := getRegisteredSysOpByIsolationKey("")
		assert.Nil(t, result)
	})

	t.Run("不存在的 key 返回 nil", func(t *testing.T) {
		result := getRegisteredSysOpByIsolationKey("nonexistent_isolation_key_12345")
		assert.Nil(t, result)
	})
}

// ──────────────────────────── ResolveOperationMode 测试 ────────────────────────────

func TestResolveOperationMode(t *testing.T) {
	t.Run("nil 输入返回 Local", func(t *testing.T) {
		mode := ResolveOperationMode(nil)
		assert.Equal(t, sysop.OperationModeLocal, mode)
	})

	t.Run("空 map 返回 Local", func(t *testing.T) {
		mode := ResolveOperationMode(map[string]any{})
		assert.Equal(t, sysop.OperationModeLocal, mode)
	})

	t.Run("sandbox 模式", func(t *testing.T) {
		config := map[string]any{
			"sys_operation": map[string]any{"mode": "sandbox"},
		}
		mode := ResolveOperationMode(config)
		assert.Equal(t, sysop.OperationModeSandbox, mode)
	})

	t.Run("local 模式", func(t *testing.T) {
		config := map[string]any{
			"sys_operation": map[string]any{"mode": "local"},
		}
		mode := ResolveOperationMode(config)
		assert.Equal(t, sysop.OperationModeLocal, mode)
	})

	t.Run("未知模式回落 Local", func(t *testing.T) {
		config := map[string]any{
			"sys_operation": map[string]any{"mode": "unknown"},
		}
		mode := ResolveOperationMode(config)
		assert.Equal(t, sysop.OperationModeLocal, mode)
	})

	t.Run("sys_operation 不是 map 时回落 Local", func(t *testing.T) {
		config := map[string]any{
			"sys_operation": "invalid",
		}
		mode := ResolveOperationMode(config)
		assert.Equal(t, sysop.OperationModeLocal, mode)
	})
}
