package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpstate "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/mcp"
)

// TestMcpSkillsDirs_连接记录与磁盘目录交集
func TestMcpSkillsDirs_连接记录与磁盘目录交集(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	sm := NewSkillManager(t.TempDir())
	// 写 state.json：mcp-a connected / mcp-b connecting / mcp-c registered
	require.NoError(t, os.MkdirAll(filepath.Join(mcpstate.McpRoot(), "skills", "mcp-a"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(mcpstate.McpRoot(), "skills", "mcp-b"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(mcpstate.McpRoot(), "skills", "mcp-c"), 0o755))
	data, _ := json.Marshal(map[string]any{"version": 1, "mcp": map[string]any{
		"mcp-a": map[string]any{"state": "connected"},
		"mcp-b": map[string]any{"state": "connecting"},
		"mcp-c": map[string]any{"state": "registered"},
	}, "mounts": map[string]any{}})
	require.NoError(t, os.WriteFile(mcpstate.StatePath(), data, 0o644))
	dirs := sm.McpSkillsDirs()
	require.Len(t, dirs, 2) // registered 的 mcp-c 不算连接
	assert.Equal(t, "mcp-a", dirs[0]["name"])
	assert.Equal(t, "mcp-b", dirs[1]["name"])
}

// TestReloadState_外部写者落盘后重载生效
func TestReloadState_外部写者落盘后重载生效(t *testing.T) {
	// Python reload_state docstring 场景：外部流程（独立 SkillManager 实例）
	// 落盘后，本实例内存 state 仍是旧值——reload 后看到新禁用名单。
	home := t.TempDir()
	t.Setenv("UAPCLAW_HOME", home)
	// 共享同一 workspaceDir（Python 两侧实例共享同一 skills_state.json 路径）
	shared := t.TempDir()
	sm := NewSkillManager(shared)
	sm2 := NewSkillManager(shared) // 独立实例（外部写者），同一状态文件
	sm2.state["skill_configs"] = map[string]any{
		"alpha": map[string]any{"enabled": false},
	}
	sm2.state["installed_skills"] = map[string]any{"alpha": map[string]any{}}
	sm2.saveState()

	// 本实例 reload 前看不到
	assert.NotContains(t, sm.ListDisabledSkills(), "alpha")
	// ✅ ReloadState 对齐 Python reload_state：重读后生效
	sm.ReloadState()
	assert.Contains(t, sm.ListDisabledSkills(), "alpha")
}
