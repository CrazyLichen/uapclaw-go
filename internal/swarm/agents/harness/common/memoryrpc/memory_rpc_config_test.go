package memoryrpc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	commmem "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// setupToggleEnv 隔离 config 环境：UAPCLAW_DATA_DIR/config/config.yaml + UAPCLAW_HOME
func setupToggleEnv(t *testing.T, initialYAML string) string {
	t.Helper()
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	assert.NoError(t, os.MkdirAll(cfgDir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(initialYAML), 0o644))
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	t.Setenv("UAPCLAW_HOME", filepath.Join(tmp, "home"))
	// 立即重置路径缓存，确保本测试的 env 重新解析（getResolvedPaths 有 sync.Once）
	pathutil.ResetCache()
	t.Cleanup(func() {
		pathutil.ResetCache()
		commmem.ClearConfigCache()
	})
	return tmp
}

// ──────────────────────────── HandleMemoryToggle ────────────────────────────

// TestHandleMemoryToggle_禁止记忆全局开关 测试 forbidden_enabled 翻转 + 写回
func TestHandleMemoryToggle_禁止记忆全局开关(t *testing.T) {
	setupToggleEnv(t, "memory:\n  forbidden_memory_definition:\n    enabled: false\n")
	res := HandleMemoryToggle(context.Background(), "/ws", "plan", map[string]any{"key": "memory_forbidden_enabled"})
	assert.Equal(t, "memory_forbidden_enabled", res["key"])
	assert.Equal(t, false, res["old_value"])
	assert.Equal(t, true, res["new_value"])
	assert.Equal(t, "global", res["mode_affected"])
	assert.Equal(t, true, res["needs_restart"])

	// 写回断言：重新 Load
	cfg, err := config.New("")
	assert.NoError(t, err)
	data, err := cfg.Load()
	assert.NoError(t, err)
	memCfg, _ := data["memory"].(map[string]any)
	forbidden, _ := memCfg["forbidden_memory_definition"].(map[string]any)
	assert.Equal(t, true, forbidden["enabled"])
}

// TestHandleMemoryToggle_禁止记忆二次切换 测试 true→false 往返
func TestHandleMemoryToggle_禁止记忆二次切换(t *testing.T) {
	setupToggleEnv(t, "memory:\n  forbidden_memory_definition:\n    enabled: true\n")
	res := HandleMemoryToggle(context.Background(), "/ws", "plan", map[string]any{"key": "memory_forbidden_enabled"})
	assert.Equal(t, true, res["old_value"])
	assert.Equal(t, false, res["new_value"])

	cfg, _ := config.New("")
	data, _ := cfg.Load()
	memCfg := data["memory"].(map[string]any)
	forbidden := memCfg["forbidden_memory_definition"].(map[string]any)
	assert.Equal(t, false, forbidden["enabled"])
}

// TestHandleMemoryToggle_模式级开关 测试 memory_enabled 按模式写回
func TestHandleMemoryToggle_模式级开关(t *testing.T) {
	setupToggleEnv(t, "")
	res := HandleMemoryToggle(context.Background(), "/ws", "plan", map[string]any{"key": "memory_enabled"})
	assert.Equal(t, false, res["old_value"]) // 空配置 → IsMemoryEnabled 类型断言失败 → false
	assert.Equal(t, true, res["new_value"])
	assert.Equal(t, "plan", res["mode_affected"])
	assert.Equal(t, true, res["needs_restart"])

	cfg, err := config.New("")
	assert.NoError(t, err)
	data, err := cfg.Load()
	assert.NoError(t, err)
	modes, _ := data["modes"].(map[string]any)
	agent, _ := modes["agent"].(map[string]any)
	plan, _ := agent["plan"].(map[string]any)
	memory, _ := plan["memory"].(map[string]any)
	assert.Equal(t, true, memory["enabled"])
}

// TestHandleMemoryToggle_主动记忆开关 测试 memory_proactive + 其他字段保留
func TestHandleMemoryToggle_主动记忆开关(t *testing.T) {
	setupToggleEnv(t, "modes:\n  agent:\n    plan:\n      memory:\n        enabled: true\n        is_proactive: true\n")
	res := HandleMemoryToggle(context.Background(), "/ws", "plan", map[string]any{"key": "memory_proactive"})
	assert.Equal(t, true, res["old_value"])
	assert.Equal(t, false, res["new_value"])
	assert.Equal(t, "plan", res["mode_affected"])

	cfg, _ := config.New("")
	data, _ := cfg.Load()
	modes := data["modes"].(map[string]any)
	agent := modes["agent"].(map[string]any)
	plan := agent["plan"].(map[string]any)
	memory := plan["memory"].(map[string]any)
	assert.Equal(t, false, memory["is_proactive"])
	assert.Equal(t, true, memory["enabled"]) // 其他字段不被破坏
}

// TestHandleMemoryToggle_未知key兜底 测试对齐 Python L438-444 兜底返回
func TestHandleMemoryToggle_未知key兜底(t *testing.T) {
	setupToggleEnv(t, "")
	res := HandleMemoryToggle(context.Background(), "/ws", "plan", map[string]any{"key": "no_such_key"})
	assert.Equal(t, "no_such_key", res["key"])
	assert.Equal(t, false, res["old_value"])
	assert.Equal(t, false, res["new_value"])
	assert.Equal(t, "", res["mode_affected"])
	assert.Equal(t, false, res["needs_restart"])
}

// TestHandleMemoryToggle_空key兜底 测试 key 缺省
func TestHandleMemoryToggle_空key兜底(t *testing.T) {
	setupToggleEnv(t, "")
	res := HandleMemoryToggle(context.Background(), "/ws", "plan", map[string]any{})
	assert.Equal(t, "", res["key"])
	assert.Equal(t, false, res["new_value"])
	assert.Equal(t, false, res["needs_restart"])
}

// ──────────────────────────── 写回函数直测 ────────────────────────────

// TestUpdateMemoryForbiddenEnabledInConfig_自动建段 测试缺失段自动创建
func TestUpdateMemoryForbiddenEnabledInConfig_自动建段(t *testing.T) {
	setupToggleEnv(t, "")
	assert.NoError(t, updateMemoryForbiddenEnabledInConfig(true))

	cfg, _ := config.New("")
	data, _ := cfg.Load()
	memCfg := data["memory"].(map[string]any)
	forbidden := memCfg["forbidden_memory_definition"].(map[string]any)
	assert.Equal(t, true, forbidden["enabled"])
}

// TestUpdateModeMemoryConfig_多级创建 测试 modes.agent.{mode}.memory 自动创建
func TestUpdateModeMemoryConfig_多级创建(t *testing.T) {
	setupToggleEnv(t, "")
	assert.NoError(t, updateModeMemoryConfig("fast", "enabled", true))

	cfg, _ := config.New("")
	data, _ := cfg.Load()
	modes := data["modes"].(map[string]any)
	agent := modes["agent"].(map[string]any)
	fast := agent["fast"].(map[string]any)
	memory := fast["memory"].(map[string]any)
	assert.Equal(t, true, memory["enabled"])
}
