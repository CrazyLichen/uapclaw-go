package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	// blank import 触发 local 包 init 注册 GlobalRegistry 的 fs/shell/code 操作
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
)

// newLocalSysOpForTest 构造基于 t.TempDir 的真实 LocalSysOperation。
//
// 对齐 Python conftest 的 sys_op fixture：OperationMode.LOCAL +
// 默认 LocalWorkConfig（restrict_to_sandbox=False，Fs 走真实文件系统，
// 绝对路径直接操作）。
func newLocalSysOpForTest(t *testing.T) (sys_operation.SysOperation, string) {
	t.Helper()
	tempDir := t.TempDir()
	card := sys_operation.NewSysOperationCard(
		sys_operation.WithSysOpWorkConfig(sys_operation.NewLocalWorkConfig()),
	)
	op, err := sys_operation.NewSysOperation(card)
	require.NoError(t, err)
	return op, tempDir
}

// toStringSlice 将 JSON 往返后的 []any 转为 []string。
// InvokeFunction.Invoke 内部经 json.Marshal/Unmarshal 中转，[]string 会变成 []any。
func toStringSlice(t *testing.T, v any) []string {
	t.Helper()
	raw, ok := v.([]any)
	require.True(t, ok, "应为 []any，实际 %T", v)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		require.True(t, ok, "元素应为 string，实际 %T", item)
		out = append(out, s)
	}
	return out
}

// TestListDirTool_Invoke 对齐 Python test_glob_and_ls 的 ls 部分：
// 写入文件+子目录 → invoke → 断言 success/dirs/files。
// T-13: 替换原桩级测试，补齐 invoke 链路真实覆盖。
func TestListDirTool_Invoke(t *testing.T) {
	op, tempDir := newLocalSysOpForTest(t)

	// 对齐 Python: os.makedirs(os.path.join(temp_dir, "subdir"), exist_ok=True)
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "subdir"), 0o755))
	// 对齐 Python: write_tool.invoke({"file_path": ..., "content": "1"}) x3
	writeTool := NewWriteFileTool(op, "cn", "test-agent-id")
	for _, name := range []string{"a.py", "c.txt"} {
		_, err := writeTool.Invoke(context.Background(), map[string]any{
			"file_path": filepath.Join(tempDir, name),
			"content":   "1",
		})
		require.NoError(t, err)
	}
	_, err := writeTool.Invoke(context.Background(), map[string]any{
		"file_path": filepath.Join(tempDir, "subdir", "b.py"),
		"content":   "2",
	})
	require.NoError(t, err)

	// 对齐 Python: ls_res = await ls_tool.invoke({"path": temp_dir, "show_hidden": False})
	lsTool := NewListDirTool(op, "cn", "test-agent-id")
	res, err := lsTool.Invoke(context.Background(), map[string]any{
		"path":        tempDir,
		"show_hidden": false,
	})
	require.NoError(t, err)

	// 对齐 Python: assert ls_res.success is True
	require.Equal(t, true, res["success"], "invoke 应成功，实际返回: %v", res)

	// 对齐 Python: assert "subdir" in ls_res.data["dirs"]; assert "a.py" in ls_res.data["files"]
	// 注：InvokeFunction 经 JSON 往返后 []string 变 []any
	data, ok := res["data"].(map[string]any)
	require.True(t, ok, "data 应为 map")
	dirs := toStringSlice(t, data["dirs"])
	files := toStringSlice(t, data["files"])

	assert.Contains(t, dirs, "subdir")
	assert.Contains(t, files, "a.py")
	assert.Contains(t, files, "c.txt")
	assert.NotContains(t, files, "b.py") // 子目录内文件不直接列出
}

// TestListDirTool_Invoke_隐藏文件过滤 对齐 Python show_hidden=False 语义：
// . 开头文件/目录被过滤，show_hidden=true 保留。
func TestListDirTool_Invoke_隐藏文件过滤(t *testing.T) {
	op, tempDir := newLocalSysOpForTest(t)

	// 隐藏文件 + 隐藏目录 + 普通文件
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, ".hidden.txt"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, ".hiddendir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "visible.txt"), []byte("x"), 0o644))

	lsTool := NewListDirTool(op, "cn", "test-agent-id")

	// show_hidden=false（默认）：过滤隐藏项
	res, err := lsTool.Invoke(context.Background(), map[string]any{
		"path":        tempDir,
		"show_hidden": false,
	})
	require.NoError(t, err)
	data := res["data"].(map[string]any)
	files := toStringSlice(t, data["files"])
	dirs := toStringSlice(t, data["dirs"])
	assert.Contains(t, files, "visible.txt")
	assert.NotContains(t, files, ".hidden.txt")
	assert.NotContains(t, dirs, ".hiddendir")

	// show_hidden=true：保留隐藏项
	res, err = lsTool.Invoke(context.Background(), map[string]any{
		"path":        tempDir,
		"show_hidden": true,
	})
	require.NoError(t, err)
	data = res["data"].(map[string]any)
	files = toStringSlice(t, data["files"])
	dirs = toStringSlice(t, data["dirs"])
	assert.Contains(t, files, "visible.txt")
	assert.Contains(t, files, ".hidden.txt")
	assert.Contains(t, dirs, ".hiddendir")
}
