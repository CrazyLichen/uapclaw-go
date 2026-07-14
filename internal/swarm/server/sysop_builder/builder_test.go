package sysop_builder

import (
	"os"
	"path/filepath"
	"testing"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
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

func TestCreateSysOperationFromCard_注册成功(t *testing.T) {
	// 确保 ResourceMgr 已初始化
	rm := runner.GetResourceMgr()
	require.NotNil(t, rm)

	card := CreateLocalSysOpCard()
	result := CreateSysOperationFromCard(card)
	require.NotNil(t, result)

	// 验证已注册到 ResourceMgr
	registered, err := rm.GetSysOperation([]string{card.ID})
	require.NoError(t, err)
	require.Len(t, registered, 1)
	assert.Equal(t, result.Card().ID, registered[0].Card().ID)
}

func TestCreateSysOperationFromCard_隔离键复用(t *testing.T) {
	rm := runner.GetResourceMgr()
	require.NotNil(t, rm)

	// 创建带 isolationKeyTemplate 的 card
	card := sysop.NewSysOperationCard(
		sysop.WithSysOpMode(sysop.OperationModeLocal),
		sysop.WithSysOpIsolationKeyTemplate("test_isolation_key_reuse"),
	)
	instance1 := CreateSysOperationFromCard(card)
	require.NotNil(t, instance1)

	// 用相同 card 再次创建应复用已有实例（同 isolation key）
	instance2 := CreateSysOperationFromCard(card)
	assert.NotNil(t, instance2)
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

	t.Run("已注册的 key 返回实例", func(t *testing.T) {
		rm := runner.GetResourceMgr()
		require.NotNil(t, rm)

		card := CreateSandboxSysOpCard(
			"http://127.0.0.1:8321", "jiuwenbox",
			nil, nil, nil, nil, "", false,
		)
		require.NotNil(t, card)
		instance := CreateSysOperationFromCard(card)
		require.NotNil(t, instance)

		// 通过 isolation key 应能查到
		isolationKey := instance.IsolationKeyTemplate()
		if isolationKey != "" {
			found := getRegisteredSysOpByIsolationKey(isolationKey)
			assert.NotNil(t, found)
		}
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
