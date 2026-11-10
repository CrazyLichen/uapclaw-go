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

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// editTestEnv 编辑工具测试环境：真实本地文件系统 + 已构造的工具。
type editTestEnv struct {
	// op 本地系统操作
	op sys_operation.SysOperation
	// dir 临时工作目录
	dir string
}

// newEditTestEnv 构造测试环境。
func newEditTestEnv(t *testing.T) *editTestEnv {
	t.Helper()
	return &editTestEnv{op: sys_operation.NewLocalSysOperation(nil), dir: t.TempDir()}
}

// path 拼接临时目录下的路径。
func (e *editTestEnv) path(name string) string { return filepath.Join(e.dir, name) }

// write 写入文件。
func (e *editTestEnv) write(t *testing.T, name, content string) string {
	t.Helper()
	path := e.path(name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// read 通过 read_file 工具登记读取状态（编辑前置条件）。
func (e *editTestEnv) read(t *testing.T, path string) {
	t.Helper()
	tl := NewReadFileTool(e.op, "cn", "agent-edit", false)
	res, err := tl.Invoke(context.Background(), map[string]any{"file_path": path})
	require.NoError(t, err)
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))
}

// edit 调用编辑工具。
func (e *editTestEnv) edit(t *testing.T, input map[string]any) map[string]any {
	t.Helper()
	tl := NewEditFileTool(e.op, "cn", "agent-edit")
	res, err := tl.Invoke(context.Background(), input)
	require.NoError(t, err)
	return res
}

// readFile 读取磁盘内容。
func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(raw)
}

// ──────────────────────────── 用例 ────────────────────────────

// TestEditFileTool_参数校验 四类非法入参各自回错误。
func TestEditFileTool_参数校验(t *testing.T) {
	env := newEditTestEnv(t)

	noPath := env.edit(t, map[string]any{"file_path": "", "old_string": "", "new_string": "x"})
	assert.Equal(t, false, noPath["success"])
	assert.Contains(t, fmt.Sprint(noPath["error"]), "file_path is required")

	noNew := env.edit(t, map[string]any{"file_path": env.path("a.txt"), "old_string": "", "new_string": ""})
	assert.Equal(t, false, noNew["success"])
	assert.Contains(t, fmt.Sprint(noNew["error"]), "new_string is required")

	notebook := env.write(t, "nb.ipynb", "{}")
	samePath := env.write(t, "same.txt", "a")
	same := env.edit(t, map[string]any{"file_path": samePath, "old_string": "a", "new_string": "a"})
	assert.Equal(t, false, same["success"])
	assert.Contains(t, fmt.Sprint(same["error"]), "identical")

	nb := env.edit(t, map[string]any{"file_path": notebook, "old_string": "a", "new_string": "b"})
	assert.Equal(t, false, nb["success"])
	assert.Contains(t, fmt.Sprint(nb["error"]), "NotebookEdit")
}

// TestEditFileTool_创建新文件 old_string 为空时创建文件并登记读取状态。
func TestEditFileTool_创建新文件(t *testing.T) {
	env := newEditTestEnv(t)
	path := env.path("new.txt")

	res := env.edit(t, map[string]any{"file_path": path, "old_string": "", "new_string": "hello\n"})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))
	assert.Equal(t, "hello\n", readFile(t, path))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, data["created"])

	_, hasState := GetFileReadState(path)
	assert.True(t, hasState, "创建后必须登记读取状态")
}

// TestEditFileTool_创建时文件已存在 目标已有内容时拒绝覆盖。
func TestEditFileTool_创建时文件已存在(t *testing.T) {
	env := newEditTestEnv(t)
	path := env.write(t, "exists.txt", "existing\n")

	res := env.edit(t, map[string]any{"file_path": path, "old_string": "", "new_string": "new\n"})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "file already exists")
}

// TestEditFileTool_文件不存在 报错并附带相似路径提示。
func TestEditFileTool_文件不存在(t *testing.T) {
	env := newEditTestEnv(t)
	env.write(t, "config.yaml", "a: 1\n")

	res := env.edit(t, map[string]any{
		"file_path":  env.path("config.yml"),
		"old_string": "a: 1",
		"new_string": "a: 2",
	})
	assert.Equal(t, false, res["success"])
	errText := fmt.Sprint(res["error"])
	assert.Contains(t, errText, "File not found")
	assert.Contains(t, errText, "Similar paths")
}

// TestEditFileTool_未读先改 未登记读取状态时拒绝编辑。
func TestEditFileTool_未读先改(t *testing.T) {
	env := newEditTestEnv(t)
	path := env.write(t, "unread.txt", "old\n")

	res := env.edit(t, map[string]any{
		"file_path":  path,
		"old_string": "old",
		"new_string": "new",
	})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "must be read before editing")
}

// TestEditFileTool_编辑成功 读取后替换首个匹配并更新状态。
func TestEditFileTool_编辑成功(t *testing.T) {
	env := newEditTestEnv(t)
	path := env.write(t, "edit.txt", "alpha\nbeta\n")
	env.read(t, path)

	res := env.edit(t, map[string]any{
		"file_path":  path,
		"old_string": "beta",
		"new_string": "gamma",
	})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))
	assert.Equal(t, "alpha\ngamma\n", readFile(t, path))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 1, data["replacements"])

	state, found := GetFileReadState(path)
	require.True(t, found)
	assert.Equal(t, "alpha\ngamma\n", state.Content)
}

// TestEditFileTool_唯一性校验 多处匹配需显式 replace_all。
func TestEditFileTool_唯一性校验(t *testing.T) {
	env := newEditTestEnv(t)
	path := env.write(t, "dup.txt", "x\nx\n")
	env.read(t, path)

	ambiguous := env.edit(t, map[string]any{
		"file_path":  path,
		"old_string": "x",
		"new_string": "y",
	})
	assert.Equal(t, false, ambiguous["success"])
	assert.Contains(t, fmt.Sprint(ambiguous["error"]), "matches 2 times")

	env.read(t, path)
	all := env.edit(t, map[string]any{
		"file_path":   path,
		"old_string":  "x",
		"new_string":  "y",
		"replace_all": true,
	})
	require.Equal(t, true, all["success"], fmt.Sprint(all["error"]))
	assert.Equal(t, "y\ny\n", readFile(t, path))
	data, ok := all["data"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 2, data["replacements"])
}

// TestEditFileTool_old_string未找到 无匹配且无引号变体时报错。
func TestEditFileTool_old_string未找到(t *testing.T) {
	env := newEditTestEnv(t)
	path := env.write(t, "nomatch.txt", "hello\n")
	env.read(t, path)

	res := env.edit(t, map[string]any{
		"file_path":  path,
		"old_string": "absent",
		"new_string": "x",
	})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "old_string not found")
}

// TestEditFileTool_外部修改检测 读取后内容被改则拒绝；内容未变仅 mtime 变化则放行。
func TestEditFileTool_外部修改检测(t *testing.T) {
	env := newEditTestEnv(t)
	path := env.write(t, "stale.txt", "old\n")
	env.read(t, path)

	require.NoError(t, os.WriteFile(path, []byte("changed\n"), 0o644))
	stale := env.edit(t, map[string]any{
		"file_path":  path,
		"old_string": "changed",
		"new_string": "new",
	})
	assert.Equal(t, false, stale["success"])
	assert.Contains(t, fmt.Sprint(stale["error"]), "modified externally")

	// 内容相同、mtime 变化：内容比对通过，允许编辑
	env.read(t, path)
	require.NoError(t, os.WriteFile(path, []byte("changed\n"), 0o644))
	same := env.edit(t, map[string]any{
		"file_path":  path,
		"old_string": "changed",
		"new_string": "fresh",
	})
	require.Equal(t, true, same["success"], fmt.Sprint(same["error"]))
	assert.Equal(t, "fresh\n", readFile(t, path))
}

// TestEditFileTool_保留CRLF 原文件为 CRLF 时写回仍为 CRLF。
func TestEditFileTool_保留CRLF(t *testing.T) {
	env := newEditTestEnv(t)
	path := env.write(t, "crlf.txt", "a\r\nb\r\n")
	env.read(t, path)

	res := env.edit(t, map[string]any{
		"file_path":  path,
		"old_string": "a\nb",
		"new_string": "a\nc",
	})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))
	assert.Equal(t, "a\r\nc\r\n", readFile(t, path))
}

// TestEditFileTool_行尾空白策略 Markdown 保留行尾空白，普通文本剔除。
func TestEditFileTool_行尾空白策略(t *testing.T) {
	env := newEditTestEnv(t)

	md := env.write(t, "doc.md", "line\n")
	env.read(t, md)
	mdRes := env.edit(t, map[string]any{
		"file_path":  md,
		"old_string": "line",
		"new_string": "line   ",
	})
	require.Equal(t, true, mdRes["success"], fmt.Sprint(mdRes["error"]))
	assert.True(t, strings.HasSuffix(readFile(t, md), "line   \n"), "Markdown 行尾空白应保留")

	txt := env.write(t, "plain.txt", "line\n")
	env.read(t, txt)
	txtRes := env.edit(t, map[string]any{
		"file_path":  txt,
		"old_string": "line",
		"new_string": "line   ",
	})
	require.Equal(t, true, txtRes["success"], fmt.Sprint(txtRes["error"]))
	assert.Equal(t, "line\n", readFile(t, txt), "普通文本行尾空白应剔除")
}

// TestFindSimilarPaths_目录不可读 目录不存在时返回空。
func TestFindSimilarPaths_目录不可读(t *testing.T) {
	assert.Nil(t, findSimilarPaths(filepath.Join(t.TempDir(), "no-such-dir", "x.txt")))
}
