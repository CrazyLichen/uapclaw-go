//go:build test

package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// invokeTool 以给定输入调用工具并返回结果。
func invokeTool(t *testing.T, tl tool.Tool, input map[string]any) map[string]any {
	t.Helper()
	res, err := tl.Invoke(context.Background(), input)
	require.NoError(t, err)
	return res
}

// ──────────────────────────── 用例：glob ───────────────────────────────

// TestGlobTool_匹配文件 递归匹配指定模式。
func TestGlobTool_匹配文件(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sub", "b.go"), []byte("y"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "c.txt"), []byte("z"), 0o644))

	tl := NewGlobTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-glob")
	res := invokeTool(t, tl, map[string]any{"pattern": "**/*.go", "path": root})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	files := fmt.Sprint(data["matching_files"])
	assert.Contains(t, files, "a.go")
	assert.Contains(t, files, "b.go")
	assert.NotContains(t, files, "c.txt")
}

// TestGlobTool_缺少模式 pattern 为空时回错误。
func TestGlobTool_缺少模式(t *testing.T) {
	tl := NewGlobTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-glob")
	res := invokeTool(t, tl, map[string]any{"pattern": ""})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "pattern is required")
}

// ──────────────────────────── 用例：write file ───────────────────────────────

// TestWriteFileTool_创建新文件 新建文件返回 created=true。
func TestWriteFileTool_创建新文件(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	tl := NewWriteFileTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-write")
	res := invokeTool(t, tl, map[string]any{"file_path": path, "content": "hello"})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, data["created"])
	assert.Equal(t, "create", data["type"])
	assert.Equal(t, "hello", readFile(t, path))
}

// TestWriteFileTool_参数校验 缺少路径/内容各自回错误。
func TestWriteFileTool_参数校验(t *testing.T) {
	tl := NewWriteFileTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-write")

	noPath := invokeTool(t, tl, map[string]any{"file_path": "", "content": "x"})
	assert.Equal(t, false, noPath["success"])
	assert.Contains(t, fmt.Sprint(noPath["error"]), "file_path is required")

	noContent := invokeTool(t, tl, map[string]any{"file_path": filepath.Join(t.TempDir(), "a.txt"), "content": ""})
	assert.Equal(t, false, noContent["success"])
	assert.Contains(t, fmt.Sprint(noContent["error"]), "content is required")
}

// TestWriteFileTool_目标是目录 目录不可作为写入目标。
func TestWriteFileTool_目标是目录(t *testing.T) {
	dir := t.TempDir()
	tl := NewWriteFileTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-write")

	res := invokeTool(t, tl, map[string]any{"file_path": dir, "content": "x"})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "Target path is a directory")
}

// TestWriteFileTool_未读先写 覆盖已有文件前必须先读取。
func TestWriteFileTool_未读先写(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exists.txt")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

	tl := NewWriteFileTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-write")
	res := invokeTool(t, tl, map[string]any{"file_path": path, "content": "new"})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "has not been read yet")
}

// TestWriteFileTool_读取后覆盖 读取后覆盖返回 update 与原内容。
func TestWriteFileTool_读取后覆盖(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.txt")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

	op := sys_operation.NewLocalSysOperation(nil)
	readRes, err := NewReadFileTool(op, "cn", "agent-write", false).Invoke(context.Background(), map[string]any{"file_path": path})
	require.NoError(t, err)
	require.Equal(t, true, readRes["success"], fmt.Sprint(readRes["error"]))

	tl := NewWriteFileTool(op, "cn", "agent-write")
	res := invokeTool(t, tl, map[string]any{"file_path": path, "content": "new"})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "update", data["type"])
	assert.Equal(t, false, data["created"])
	assert.Equal(t, "old", data["original_file"])
	assert.Equal(t, "new", readFile(t, path))
}

// TestWriteFileTool_外部修改检测 读取后文件被改则拒绝覆盖。
func TestWriteFileTool_外部修改检测(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.txt")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

	op := sys_operation.NewLocalSysOperation(nil)
	_, err := NewReadFileTool(op, "cn", "agent-write", false).Invoke(context.Background(), map[string]any{"file_path": path})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("changed"), 0o644))

	tl := NewWriteFileTool(op, "cn", "agent-write")
	res := invokeTool(t, tl, map[string]any{"file_path": path, "content": "new"})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "modified since read")
}

// TestWriteFileTool_内容超限 超过 5 MiB 的内容被拒绝。
func TestWriteFileTool_内容超限(t *testing.T) {
	tl := NewWriteFileTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-write")
	res := invokeTool(t, tl, map[string]any{
		"file_path": filepath.Join(t.TempDir(), "big.txt"),
		"content":   strings.Repeat("a", maxContentBytes+1),
	})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "content is too large")
}

// ──────────────────────────── 用例：grep ───────────────────────────────

// TestGrepTool_参数校验 缺少 pattern / 非法输出模式各自回错误。
func TestGrepTool_参数校验(t *testing.T) {
	tl := NewGrepTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-grep")

	noPattern := invokeTool(t, tl, map[string]any{"pattern": ""})
	assert.Equal(t, false, noPattern["success"])
	assert.Contains(t, fmt.Sprint(noPattern["error"]), "pattern is required")

	badMode := invokeTool(t, tl, map[string]any{"pattern": "x", "path": t.TempDir(), "output_mode": "weird"})
	assert.Equal(t, false, badMode["success"])
	assert.Contains(t, fmt.Sprint(badMode["error"]), "output_mode must be one of")
}

// TestGrepTool_内容搜索 content 模式返回命中行。
func TestGrepTool_内容搜索(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\nneedle here\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("nothing\n"), 0o644))

	tl := NewGrepTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-grep")
	res := invokeTool(t, tl, map[string]any{"pattern": "needle", "path": dir})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	content := fmt.Sprint(data["content"])
	assert.Contains(t, content, "needle here")
	assert.NotContains(t, content, "nothing")
}

// TestGrepTool_文件列表模式 files_with_matches 只返回文件名。
func TestGrepTool_文件列表模式(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hit.txt"), []byte("needle\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "miss.txt"), []byte("other\n"), 0o644))

	tl := NewGrepTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-grep")
	res := invokeTool(t, tl, map[string]any{
		"pattern":     "needle",
		"path":        dir,
		"output_mode": "files_with_matches",
	})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	files := fmt.Sprint(data["filenames"])
	assert.Contains(t, files, "hit.txt")
	assert.NotContains(t, files, "miss.txt")
}
