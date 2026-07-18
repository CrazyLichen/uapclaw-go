package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 测试用例 ────────────────────────────

// TestUpsertSubagentInConfigAt_NewEntry 测试添加新的 subagent 启用状态
func TestUpsertSubagentInConfigAt_NewEntry(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// 初始配置文件
	initialData := map[string]any{
		"react": map[string]any{
			"subagents": map[string]any{},
		},
	}
	writeYAMLHelper(t, configPath, initialData)

	err := upsertSubagentInConfigAt("my-agent", true, configPath)
	require.NoError(t, err)

	// 验证文件内容
	data := readYAMLHelper(t, configPath)
	react, _ := data["react"].(map[string]any)
	subagents, _ := react["subagents"].(map[string]any)
	agentCfg, _ := subagents["my-agent"].(map[string]any)
	assert.Equal(t, true, agentCfg["enabled"])
}

// TestUpsertSubagentInConfigAt_UpdateExisting 测试更新已有 subagent 的 enabled 状态
func TestUpsertSubagentInConfigAt_UpdateExisting(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// 初始配置文件，含已有 subagent 配置
	initialData := map[string]any{
		"react": map[string]any{
			"subagents": map[string]any{
				"my-agent": map[string]any{
					"enabled":        false,
					"max_iterations": 5,
				},
			},
		},
	}
	writeYAMLHelper(t, configPath, initialData)

	err := upsertSubagentInConfigAt("my-agent", true, configPath)
	require.NoError(t, err)

	// 验证 enabled 被更新，其他配置保留
	data := readYAMLHelper(t, configPath)
	react, _ := data["react"].(map[string]any)
	subagents, _ := react["subagents"].(map[string]any)
	agentCfg, _ := subagents["my-agent"].(map[string]any)
	assert.Equal(t, true, agentCfg["enabled"])
	assert.Equal(t, 5, agentCfg["max_iterations"])
}

// TestUpsertSubagentInConfigAt_CreateMissingSections 测试自动创建不存在的 react/subagents 段
func TestUpsertSubagentInConfigAt_CreateMissingSections(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// 空配置文件
	writeYAMLHelper(t, configPath, map[string]any{})

	err := upsertSubagentInConfigAt("new-agent", true, configPath)
	require.NoError(t, err)

	data := readYAMLHelper(t, configPath)
	react, _ := data["react"].(map[string]any)
	require.NotNil(t, react)
	subagents, _ := react["subagents"].(map[string]any)
	require.NotNil(t, subagents)
	agentCfg, _ := subagents["new-agent"].(map[string]any)
	assert.Equal(t, true, agentCfg["enabled"])
}

// TestUpsertSubagentInConfigAt_EmptyName 测试空名称校验
func TestUpsertSubagentInConfigAt_EmptyName(t *testing.T) {
	err := upsertSubagentInConfigAt("", true, "/tmp/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required")

	err = upsertSubagentInConfigAt("   ", true, "/tmp/config.yaml")
	assert.Error(t, err)
}

// TestUpsertSubagentInConfigAt_NonExistentFile 测试配置文件不存在时自动创建
func TestUpsertSubagentInConfigAt_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// 不创建文件，让函数自动创建
	err := upsertSubagentInConfigAt("my-agent", true, configPath)
	require.NoError(t, err)

	// 验证文件被创建
	data := readYAMLHelper(t, configPath)
	react, _ := data["react"].(map[string]any)
	require.NotNil(t, react)
	subagents, _ := react["subagents"].(map[string]any)
	agentCfg, _ := subagents["my-agent"].(map[string]any)
	assert.Equal(t, true, agentCfg["enabled"])
}

// TestRemoveSubagentFromConfigAt_Existing 测试删除已有的 subagent 条目
func TestRemoveSubagentFromConfigAt_Existing(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	initialData := map[string]any{
		"react": map[string]any{
			"subagents": map[string]any{
				"my-agent": map[string]any{
					"enabled": true,
				},
				"other-agent": map[string]any{
					"enabled": false,
				},
			},
		},
	}
	writeYAMLHelper(t, configPath, initialData)

	found, err := removeSubagentFromConfigAt("my-agent", configPath)
	require.NoError(t, err)
	assert.True(t, found)

	// 验证条目已删除，其他条目保留
	data := readYAMLHelper(t, configPath)
	react, _ := data["react"].(map[string]any)
	subagents, _ := react["subagents"].(map[string]any)
	assert.Nil(t, subagents["my-agent"])
	assert.NotNil(t, subagents["other-agent"])
}

// TestRemoveSubagentFromConfigAt_NotFound 测试删除不存在的 subagent
func TestRemoveSubagentFromConfigAt_NotFound(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	initialData := map[string]any{
		"react": map[string]any{
			"subagents": map[string]any{},
		},
	}
	writeYAMLHelper(t, configPath, initialData)

	found, err := removeSubagentFromConfigAt("non-existent", configPath)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestRemoveSubagentFromConfigAt_NoReactSection 测试无 react 段时返回 false
func TestRemoveSubagentFromConfigAt_NoReactSection(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	writeYAMLHelper(t, configPath, map[string]any{})

	found, err := removeSubagentFromConfigAt("any-agent", configPath)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestRemoveSubagentFromConfigAt_EmptyName 测试空名称校验
func TestRemoveSubagentFromConfigAt_EmptyName(t *testing.T) {
	found, err := removeSubagentFromConfigAt("", "/tmp/config.yaml")
	assert.Error(t, err)
	assert.False(t, found)
}

// TestLoadYAMLForRoundTrip_NonExistent 测试加载不存在的文件返回空 map
func TestLoadYAMLForRoundTrip_NonExistent(t *testing.T) {
	data, err := loadYAMLForRoundTrip("/non/existent/config.yaml")
	require.NoError(t, err)
	assert.Empty(t, data)
}

// TestLoadYAMLForRoundTrip_Valid 测试加载有效的 YAML 文件
func TestLoadYAMLForRoundTrip_Valid(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := "react:\n  subagents:\n    my-agent:\n      enabled: true\n"
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	data, err := loadYAMLForRoundTrip(configPath)
	require.NoError(t, err)
	react, _ := data["react"].(map[string]any)
	assert.NotNil(t, react)
}

// TestYamlRoundTrip_UpsertThenRemove 测试先添加再删除的完整流程
func TestYamlRoundTrip_UpsertThenRemove(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// 初始空配置
	writeYAMLHelper(t, configPath, map[string]any{})

	// 添加
	err := upsertSubagentInConfigAt("test-agent", true, configPath)
	require.NoError(t, err)

	data := readYAMLHelper(t, configPath)
	react, _ := data["react"].(map[string]any)
	subagents, _ := react["subagents"].(map[string]any)
	agentCfg, _ := subagents["test-agent"].(map[string]any)
	assert.Equal(t, true, agentCfg["enabled"])

	// 禁用
	err = upsertSubagentInConfigAt("test-agent", false, configPath)
	require.NoError(t, err)

	data = readYAMLHelper(t, configPath)
	react, _ = data["react"].(map[string]any)
	subagents, _ = react["subagents"].(map[string]any)
	agentCfg, _ = subagents["test-agent"].(map[string]any)
	assert.Equal(t, false, agentCfg["enabled"])

	// 删除
	found, err := removeSubagentFromConfigAt("test-agent", configPath)
	require.NoError(t, err)
	assert.True(t, found)

	data = readYAMLHelper(t, configPath)
	react, _ = data["react"].(map[string]any)
	subagents, _ = react["subagents"].(map[string]any)
	assert.Nil(t, subagents["test-agent"])
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// writeYAMLHelper 将数据写入 YAML 文件
func writeYAMLHelper(t *testing.T, path string, data map[string]any) {
	t.Helper()
	content, err := yaml.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, content, 0o644))
}

// readYAMLHelper 从 YAML 文件读取数据
func readYAMLHelper(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, yaml.Unmarshal(content, &data))
	return data
}
