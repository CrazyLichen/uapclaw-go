package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGet_未加载时自动触发Load 测试首次 Get 自动触发 Load。
func TestGet_未加载时自动触发Load(t *testing.T) {
	Reset()
	// 即使未显式调 Load，Get 也应能工作
	val := Get("NONEXISTENT_KEY_12345", "default_val")
	assert.Equal(t, "default_val", val)
}

// TestGet_优先级 测试 _config > os.Getenv > default。
func TestGet_优先级(t *testing.T) {
	Reset()
	// 设置环境变量
	_ = os.Setenv("CE_TEST_KEY", "env_value")
	defer os.Unsetenv("CE_TEST_KEY")

	// 环境变量 > default
	val := Get("CE_TEST_KEY", "default_val")
	assert.Equal(t, "env_value", val)

	// _config > 环境变量
	Set("CE_TEST_KEY", "config_value")
	val = Get("CE_TEST_KEY", "default_val")
	assert.Equal(t, "config_value", val)
}

// TestGetString 测试字符串类型安全获取。
func TestGetString(t *testing.T) {
	Reset()
	Set("STR_KEY", "hello")
	assert.Equal(t, "hello", GetString("STR_KEY", "fallback"))
	assert.Equal(t, "fallback", GetString("MISSING_KEY", "fallback"))
}

// TestGetInt 测试整数类型安全获取。
func TestGetInt(t *testing.T) {
	Reset()
	Set("INT_KEY", 42)
	assert.Equal(t, 42, GetInt("INT_KEY", 0))

	Set("INT_STR_KEY", "100")
	assert.Equal(t, 100, GetInt("INT_STR_KEY", 0))

	assert.Equal(t, 7, GetInt("MISSING_KEY", 7))
}

// TestGetBool 测试布尔类型安全获取。
func TestGetBool(t *testing.T) {
	Reset()
	Set("BOOL_TRUE", true)
	assert.True(t, GetBool("BOOL_TRUE", false))

	Set("BOOL_FALSE", false)
	assert.False(t, GetBool("BOOL_FALSE", true))

	Set("BOOL_STR_TRUE", "true")
	assert.True(t, GetBool("BOOL_STR_TRUE", false))

	Set("BOOL_STR_YES", "yes")
	assert.True(t, GetBool("BOOL_STR_YES", false))

	Set("BOOL_STR_FALSE", "false")
	assert.False(t, GetBool("BOOL_STR_FALSE", true))

	Set("BOOL_STR_NO", "no")
	assert.False(t, GetBool("BOOL_STR_NO", true))

	assert.True(t, GetBool("MISSING_KEY", true))
}

// TestLoad_env文件 测试 .env 文件加载。
func TestLoad_env文件(t *testing.T) {
	Reset()
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	err := os.WriteFile(envPath, []byte("TEST_API_KEY=sk-test123\nTEST_NUM=42\nTEST_BOOL=true\n# comment\nTEST_STR=hello\n"), 0o644)
	require.NoError(t, err)

	err = Load(filepath.Join(dir, "config.yaml"), envPath)
	require.NoError(t, err)

	assert.Equal(t, "sk-test123", Get("TEST_API_KEY", ""))
	assert.Equal(t, 42, GetInt("TEST_NUM", 0))
	assert.True(t, GetBool("TEST_BOOL", false))
	assert.Equal(t, "hello", GetString("TEST_STR", ""))
}

// TestLoad_configYaml不覆盖env 测试 config.yaml 不覆盖 .env 已有 key。
func TestLoad_configYaml不覆盖env(t *testing.T) {
	Reset()
	dir := t.TempDir()

	envPath := filepath.Join(dir, ".env")
	err := os.WriteFile(envPath, []byte("API_KEY=from_env\nSHARED_KEY=env_wins\n"), 0o644)
	require.NoError(t, err)

	yamlPath := filepath.Join(dir, "config.yaml")
	err = os.WriteFile(yamlPath, []byte("SHARED_KEY: yaml_loses\nYAML_ONLY: yaml_value\n"), 0o644)
	require.NoError(t, err)

	err = Load(yamlPath, envPath)
	require.NoError(t, err)

	// .env 优先
	assert.Equal(t, "from_env", Get("API_KEY", ""))
	assert.Equal(t, "env_wins", Get("SHARED_KEY", ""))
	// YAML 独有 key 正常加载
	assert.Equal(t, "yaml_value", Get("YAML_ONLY", ""))
}

// TestSet_运行时修改 测试运行时 Set。
func TestSet_运行时修改(t *testing.T) {
	Reset()
	Set("RUNTIME_KEY", "v1")
	assert.Equal(t, "v1", Get("RUNTIME_KEY", ""))

	Set("RUNTIME_KEY", "v2")
	assert.Equal(t, "v2", Get("RUNTIME_KEY", ""))
}

// TestLoad_文件不存在返回空配置 测试文件不存在时不报错。
func TestLoad_文件不存在返回空配置(t *testing.T) {
	Reset()
	dir := t.TempDir()
	err := Load(filepath.Join(dir, "nonexistent.yaml"), filepath.Join(dir, "nonexistent.env"))
	require.NoError(t, err)
	// 应返回默认值
	assert.Equal(t, "default", Get("ANY_KEY", "default"))
}

// TestSnapshotRestore 测试快照保存和恢复。
func TestSnapshotRestore(t *testing.T) {
	Reset()
	Set("KEY1", "v1")
	Set("KEY2", 42)

	snap := Snapshot()
	assert.Equal(t, "v1", snap["KEY1"])
	assert.Equal(t, 42, snap["KEY2"])

	// 修改后恢复
	Set("KEY1", "changed")
	Restore(snap)
	assert.Equal(t, "v1", Get("KEY1", ""))
}

// TestDelete 测试删除配置值。
func TestDelete(t *testing.T) {
	Reset()
	Set("DEL_KEY", "val")
	assert.Equal(t, "val", Get("DEL_KEY", ""))

	Delete("DEL_KEY")
	assert.Equal(t, "default", Get("DEL_KEY", "default"))
}

// TestReload 测试强制重新加载。
func TestReload(t *testing.T) {
	Reset()
	Set("RELOAD_KEY", "before")
	// Reload 会清空并重新加载（无文件时为空）
	Reload()
	assert.Equal(t, "default", Get("RELOAD_KEY", "default"))
}

// TestConvertValue 测试类型推断。对齐 Python _convert_value。
func TestConvertValue(t *testing.T) {
	tests := []struct {
		input    string
		expected any
	}{
		{"true", true},
		{"True", true},
		{"yes", true},
		{"1", true},
		{"false", false},
		{"False", false},
		{"no", false},
		{"0", false},
		{"42", 42},
		{"3.14", 3.14},
		{"hello", "hello"},
		{"", ""},
	}
	for _, tt := range tests {
		result := convertValue(tt.input)
		assert.Equal(t, tt.expected, result, "convertValue(%q)", tt.input)
	}
}
