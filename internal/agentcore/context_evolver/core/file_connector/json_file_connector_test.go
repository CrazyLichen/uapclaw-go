package file_connector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJSONFileConnector(t *testing.T) {
	c := NewJSONFileConnector()
	assert.NotNil(t, c)
	assert.Equal(t, 2, c.Indent()) // 默认 indent=2
	assert.False(t, c.EnsureASCII())
}

func TestNewJSONFileConnector_自定义选项(t *testing.T) {
	c := NewJSONFileConnector(WithIndent(4), WithEnsureASCII(true))
	assert.Equal(t, 4, c.Indent())
	assert.True(t, c.EnsureASCII())
}

func TestJSONFileConnector_SaveToFile_LoadFromFile(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "sub", "test.json")

	data := map[string]any{"key": "value", "count": 42.0}
	require.NoError(t, c.SaveToFile(fp, data))

	loaded, err := c.LoadFromFile(fp)
	require.NoError(t, err)
	assert.Equal(t, "value", loaded["key"])
	assert.Equal(t, 42.0, loaded["count"])
}

func TestJSONFileConnector_SaveToFile_自动建目录(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "a", "b", "c", "test.json")

	require.NoError(t, c.SaveToFile(fp, map[string]any{"ok": true}))
	assert.FileExists(t, fp)
}

func TestJSONFileConnector_SaveToFile_中文编码(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "cn.json")

	data := map[string]any{"内容": "你好世界"}
	require.NoError(t, c.SaveToFile(fp, data))

	loaded, err := c.LoadFromFile(fp)
	require.NoError(t, err)
	assert.Equal(t, "你好世界", loaded["内容"])

	// 验证文件内容非转义
	raw, err := os.ReadFile(fp)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "你好世界") // ensure_ascii=false 时不转义
}

func TestJSONFileConnector_SaveToFile_ensureASCII(t *testing.T) {
	c := NewJSONFileConnector(WithEnsureASCII(true))
	dir := t.TempDir()
	fp := filepath.Join(dir, "cn.json")

	data := map[string]any{"内容": "你好世界"}
	require.NoError(t, c.SaveToFile(fp, data))

	// 验证文件内容被转义
	raw, err := os.ReadFile(fp)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "你好世界") // ensure_ascii=true 时转义为 \uXXXX
}

func TestJSONFileConnector_Exists(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.json")

	assert.False(t, c.Exists(fp))
	require.NoError(t, c.SaveToFile(fp, map[string]any{}))
	assert.True(t, c.Exists(fp))
}

func TestJSONFileConnector_Delete(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.json")

	require.NoError(t, c.SaveToFile(fp, map[string]any{}))

	deleted, err := c.Delete(fp)
	require.NoError(t, err)
	assert.True(t, deleted)
	assert.NoFileExists(t, fp)

	deleted, err = c.Delete(fp)
	require.NoError(t, err)
	assert.False(t, deleted)
}

func TestJSONFileConnector_LoadFromFile_不存在(t *testing.T) {
	c := NewJSONFileConnector()
	_, err := c.LoadFromFile("/nonexistent/path.json")
	require.Error(t, err)
}

func TestJSONFileConnector_覆盖写入(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.json")

	require.NoError(t, c.SaveToFile(fp, map[string]any{"v": 1.0}))
	require.NoError(t, c.SaveToFile(fp, map[string]any{"v": 2.0}))

	loaded, err := c.LoadFromFile(fp)
	require.NoError(t, err)
	assert.Equal(t, 2.0, loaded["v"])
}

func TestJSONFileConnector_缩进格式(t *testing.T) {
	c := NewJSONFileConnector(WithIndent(4))
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.json")

	require.NoError(t, c.SaveToFile(fp, map[string]any{"key": "value"}))
	raw, err := os.ReadFile(fp)
	require.NoError(t, err)
	// 验证4空格缩进
	assert.Contains(t, string(raw), "    ") // 4个空格
}
