package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setState(t *testing.T, payload map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("UAPCLAW_HOME", dir)
	// workspace.WorkspaceDir 依赖 UAPCLAW_HOME 派生
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	root := filepath.Join(os.TempDir())
	_ = root
	p := StatePath()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, data, 0o644))
	return p
}

func sampleState() map[string]any {
	return map[string]any{
		"version": 1,
		"mcp": map[string]any{
			"mcp-a": map[string]any{"state": "connected", "transport": "stdio"},
			"mcp-b": map[string]any{"state": "connecting"},
			"mcp-c": map[string]any{"state": "registered"},
			"mcp-d": map[string]any{"state": "disconnected"},
		},
		"mounts": map[string]any{},
	}
}

// TestLoadState_默认键与缺失文件
func TestLoadState_默认键与缺失文件(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	state := ReadMcpState()
	assert.Equal(t, 1, state["version"])
	assert.NotNil(t, state["mcp"])
	assert.NotNil(t, state["mounts"])
}

// TestLoadState_BOM容忍与损坏回落
func TestLoadState_BOM容忍与损坏回落(t *testing.T) {
	home := t.TempDir()
	t.Setenv("UAPCLAW_HOME", home)
	p := StatePath()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	// BOM 前缀（PowerShell Set-Content 旧工具形态）
	require.NoError(t, os.WriteFile(p, append([]byte("\xef\xbb\xbf"), []byte(`{"version":1,"mcp":{"x":{"state":"connected"}},"mounts":{}}`)...), 0o644))
	rec := GetMcpRecord("x")
	require.NotNil(t, rec)
	assert.Equal(t, "connected", rec["state"])
	// 损坏 JSON → 空态
	require.NoError(t, os.WriteFile(p, []byte("not json"), 0o644))
	state := ReadMcpState()
	assert.Equal(t, 1, state["version"])
	assert.NotNil(t, state["mcp"])
}

// TestListConnectedMcps_四态过滤
func TestListConnectedMcps_四态过滤(t *testing.T) {
	setState(t, sampleState())
	connected := ListConnectedMcps()
	// connected + connecting，且 name 键回填、按名排序
	names := make([]string, 0, len(connected))
	for _, rec := range connected {
		names = append(names, rec["name"].(string))
	}
	assert.Equal(t, []string{"mcp-a", "mcp-b"}, names)
	assert.Len(t, ListTrulyConnectedMcps(), 1)
	assert.Len(t, ListConnectingMcps(), 1)
	assert.Len(t, ListRegisteredMcps(), 2) // registered + connected
}

// TestListTuiEnabledMcps_enabled开关
func TestListTuiEnabledMcps_enabled开关(t *testing.T) {
	setState(t, map[string]any{"version": 1, "mcp": map[string]any{
		"tui-on":  map[string]any{"state": "connected", "enabled": true},
		"tui-off": map[string]any{"state": "connected", "enabled": false},
	}, "mounts": map[string]any{}})
	enabled := ListTuiEnabledMcps()
	require.Len(t, enabled, 1)
	assert.Equal(t, "tui-on", enabled[0]["name"])
}

// TestConnectedMcpSkillDirs_磁盘存在才返回
func TestConnectedMcpSkillDirs_磁盘存在才返回(t *testing.T) {
	home := t.TempDir()
	t.Setenv("UAPCLAW_HOME", home)
	setState(t, sampleState())
	// 只给 mcp-a 建捆绑目录
	require.NoError(t, os.MkdirAll(filepath.Join(McpRoot(), "skills", "mcp-a"), 0o755))
	dirs := ConnectedMcpSkillDirs()
	require.Len(t, dirs, 1)
	assert.Equal(t, "mcp-a", dirs[0]["name"])
	assert.Equal(t, filepath.Join(McpRoot(), "skills", "mcp-a"), dirs[0]["dir"])
	// 断连（删记录）→ 技能目录不再返回
	state := map[string]any{"version": 1, "mcp": map[string]any{
		"mcp-b": map[string]any{"state": "connecting"},
	}, "mounts": map[string]any{}}
	p := StatePath()
	data, _ := json.Marshal(state)
	require.NoError(t, os.WriteFile(p, data, 0o644))
	assert.Empty(t, ConnectedMcpSkillDirs())
}

// TestSaveState_原子写与读回
func TestSaveState_原子写与读回(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	SaveState(map[string]any{"version": 1, "mcp": map[string]any{
		"w1": map[string]any{"state": "connected"},
	}, "mounts": map[string]any{}})
	rec := GetMcpRecord("w1")
	require.NotNil(t, rec)
	assert.Equal(t, "connected", rec["state"])
	// 无 .tmp 残留
	_, err := os.Stat(StatePath() + ".tmp")
	assert.True(t, os.IsNotExist(err))
}
