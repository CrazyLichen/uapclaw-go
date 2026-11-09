package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// stubFileGuard 满足 FileGuardEvaluator 的替身。
type stubFileGuard struct{}

// Enabled 固定启用。
func (stubFileGuard) Enabled() bool { return true }

// Mode 返回测试模式。
func (stubFileGuard) Mode() string { return "stub" }

// Evaluate 无意见。
func (stubFileGuard) Evaluate(_ string, _ map[string]any) *PermissionResult { return nil }

// CollectAskAccesses 返回空。
func (stubFileGuard) CollectAskAccesses(_ string, _ map[string]any) []PathAction { return nil }

// stubNetGuard 满足 NetGuardEvaluator 的替身。
type stubNetGuard struct{}

// Evaluate 无意见。
func (stubNetGuard) Evaluate(_ string, _ map[string]any) *PermissionResult { return nil }

// restoreGuardRegistries 用例结束后恢复全局注册表，避免串味。
func restoreGuardRegistries(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		RegisterFileGuardFactory(nil)
		RegisterNetGuardFactory(nil)
		RegisterPermissionBridges(nil, nil, nil)
	})
}

// ──────────────────────────── 用例 ────────────────────────────

// TestTruthyAny_Python布尔语义 覆盖各类型的真值判定。
func TestTruthyAny_Python布尔语义(t *testing.T) {
	assert.False(t, truthyAny(nil))
	assert.False(t, truthyAny(false))
	assert.True(t, truthyAny(true))
	assert.False(t, truthyAny(""))
	assert.True(t, truthyAny("x"))
	assert.False(t, truthyAny(0))
	assert.True(t, truthyAny(1))
	assert.False(t, truthyAny(int64(0)))
	assert.True(t, truthyAny(int64(2)))
	assert.False(t, truthyAny(0.0))
	assert.True(t, truthyAny(0.5))
	assert.False(t, truthyAny([]any{}))
	assert.True(t, truthyAny([]any{1}))
	assert.False(t, truthyAny(map[string]any{}))
	assert.True(t, truthyAny(map[string]any{"k": 1}))
	assert.True(t, truthyAny(struct{}{}), "未知类型按非 nil 视为真")
}

// TestRegisterFileGuardFactory_构造判定器 注册后可构造，未注册返回 nil。
func TestRegisterFileGuardFactory_构造判定器(t *testing.T) {
	restoreGuardRegistries(t)

	assert.Nil(t, NewFileGuardEvaluator(map[string]any{}, "/ws", nil), "未注册工厂应返回 nil")

	RegisterFileGuardFactory(func(map[string]any, string, []string) FileGuardEvaluator {
		return stubFileGuard{}
	})
	evaluator := NewFileGuardEvaluator(map[string]any{}, "/ws", []string{"/trusted"})
	require.NotNil(t, evaluator)
	assert.True(t, evaluator.Enabled())
	assert.Equal(t, "stub", evaluator.Mode())
}

// TestRegisterNetGuardFactory_构造判定器 注册后可构造，未注册返回 nil。
func TestRegisterNetGuardFactory_构造判定器(t *testing.T) {
	restoreGuardRegistries(t)

	assert.Nil(t, NewNetGuardEvaluator(map[string]any{}))

	RegisterNetGuardFactory(func(map[string]any) NetGuardEvaluator { return stubNetGuard{} })
	assert.NotNil(t, NewNetGuardEvaluator(map[string]any{}))
}

// TestRegisterPermissionBridges_注入函数生效 三个桥接函数注册后按序生效。
func TestRegisterPermissionBridges_注入函数生效(t *testing.T) {
	restoreGuardRegistries(t)

	RegisterPermissionBridges(
		func(cfg map[string]any, _ *bool) map[string]any {
			cfg["merged_paths"] = true
			return cfg
		},
		func(cfg map[string]any, _ *bool) map[string]any {
			cfg["merged_urls"] = true
			return cfg
		},
		func(urls any) bool { return urls == "ready" },
	)

	merged := mergeWithSensitivePaths(map[string]any{})
	assert.Equal(t, true, merged["merged_paths"])

	merged = mergeWithNetURLs(map[string]any{})
	assert.Equal(t, true, merged["merged_urls"])

	assert.True(t, checkPackageNetURLs("ready"))
	assert.False(t, checkPackageNetURLs("nope"))
}

// TestMergeHelpers_未注册时保守返回 缺注册函数时原样返回并触发注入。
func TestMergeHelpers_未注册时保守返回(t *testing.T) {
	restoreGuardRegistries(t)
	RegisterPermissionBridges(nil, nil, nil)

	cfg := map[string]any{"k": "v"}
	assert.Equal(t, cfg, mergeWithSensitivePaths(cfg))
	assert.Equal(t, cfg, mergeWithNetURLs(cfg))
	assert.False(t, checkPackageNetURLs(nil), "缺判定函数时保守触发注入")
}

// TestPreparePermissionsForEngine_关闭内置规则 关闭后只补 legacy action。
func TestPreparePermissionsForEngine_关闭内置规则(t *testing.T) {
	restoreGuardRegistries(t)

	out := PreparePermissionsForEngine(map[string]any{
		"package_builtin_rules": map[string]any{"enabled": false},
		"rules": []any{
			map[string]any{"severity": "HIGH"},
		},
	})
	require.NotNil(t, out)
	rules, ok := out["rules"].([]any)
	require.True(t, ok)
	first, ok := rules[0].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, first["action"], "legacy 规则应回填 action")
}

// TestPreparePermissionsForEngine_注入敏感路径与net 内置规则开启时按需注入。
func TestPreparePermissionsForEngine_注入敏感路径与net(t *testing.T) {
	restoreGuardRegistries(t)
	RegisterPermissionBridges(
		func(cfg map[string]any, _ *bool) map[string]any {
			cfg["sensitive_merged"] = true
			return cfg
		},
		func(cfg map[string]any, _ *bool) map[string]any {
			cfg["net_merged"] = true
			return cfg
		},
		func(any) bool { return false },
	)

	out := PreparePermissionsForEngine(map[string]any{
		"package_builtin_rules": map[string]any{"enabled": true},
		"file_guard":            map[string]any{"enabled": true},
		"net_guard":             map[string]any{"enabled": true},
	})
	assert.Equal(t, true, out["sensitive_merged"])
	assert.Equal(t, true, out["net_merged"])
}

// TestSeverityToAction_四档映射 severity 映射为 allow/ask/deny。
func TestSeverityToAction_四档映射(t *testing.T) {
	assert.Equal(t, "", severityToAction(nil, ""))
	assert.Equal(t, "", severityToAction("", ""))
	assert.Contains(t, []string{"allow", "ask", "deny"}, severityToAction("LOW", ""))
	assert.NotEmpty(t, severityToAction("critical", ""))
	assert.NotEmpty(t, severityToAction("  high  ", ""))
}

// TestResolveUserPath_展开家目录与环境变量 绝对化并清理路径。
func TestResolveUserPath_展开家目录与环境变量(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SEC_TEST_DIR", "/tmp/sec-test")

	assert.Equal(t, home, resolveUserPath("~"))
	assert.Equal(t, home+"/sub", resolveUserPath("~/sub"))
	assert.Equal(t, "/tmp/sec-test", resolveUserPath("$SEC_TEST_DIR"))
	assert.Equal(t, "/abs/path", resolveUserPath("/abs/./path"))
}

// TestPermissionEngine_受信目录热更新 更新后可信目录规范化并可读回。
func TestPermissionEngine_受信目录热更新(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": true}, nil, "", t.TempDir())
	require.NotNil(t, engine)

	dir := t.TempDir()
	engine.UpdateTrustedDirs([]string{"  " + dir + "  ", ""})
	assert.Equal(t, []string{dir}, engine.TrustedDirs())
}

// TestPermissionEngine_禁用时直接放行 系统关闭时返回 allow。
func TestPermissionEngine_禁用时直接放行(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{"enabled": false}, nil, "", t.TempDir())
	result := engine.CheckPermission(context.Background(), "bash", map[string]any{"command": "ls"})
	require.NotNil(t, result)
	assert.Equal(t, PermissionLevelAllow, result.Permission)
}
