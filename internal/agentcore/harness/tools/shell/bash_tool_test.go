//go:build test

package shell

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
	// 空导入注册本地 fs/shell/code 操作，供 NewLocalSysOperation 懒实例化
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newBashTool 构造基于真实本地 shell 的 bash 工具。
func newBashTool(t *testing.T) tool.Tool {
	t.Helper()
	return NewBashTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-bash", PermissionConfig{})
}

// invokeBash 调用 bash 工具。
func invokeBash(t *testing.T, tl tool.Tool, inputs map[string]any) map[string]any {
	t.Helper()
	res, err := tl.Invoke(context.Background(), inputs)
	require.NoError(t, err)
	return res
}

// resultContent 取结果中的 content 文本。
func resultContent(res map[string]any) string {
	data, ok := res["data"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := data["content"].(string)
	return content
}

// ──────────────────────────── 用例 ────────────────────────────

// TestBashTool_空命令 空命令直接回错误。
func TestBashTool_空命令(t *testing.T) {
	res := invokeBash(t, newBashTool(t), map[string]any{"command": "   "})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "command cannot be empty")
}

// TestBashTool_执行成功 正常命令返回 stdout 内容。
func TestBashTool_执行成功(t *testing.T) {
	res := invokeBash(t, newBashTool(t), map[string]any{"command": "echo hello-bash"})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))
	assert.Contains(t, resultContent(res), "hello-bash")
}

// TestBashTool_工作目录生效 workdir 指定的目录成为命令 cwd。
func TestBashTool_工作目录生效(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o644))

	res := invokeBash(t, newBashTool(t), map[string]any{"command": "ls", "workdir": dir})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))
	assert.Contains(t, resultContent(res), "marker.txt")
}

// TestBashTool_工作目录不存在 workdir 缺失时拒绝执行。
func TestBashTool_工作目录不存在(t *testing.T) {
	res := invokeBash(t, newBashTool(t), map[string]any{
		"command": "echo x",
		"workdir": filepath.Join(t.TempDir(), "missing"),
	})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "workdir does not exist")
}

// TestBashTool_非零退出码 失败命令标记为 error 并保留部分输出。
func TestBashTool_非零退出码(t *testing.T) {
	res := invokeBash(t, newBashTool(t), map[string]any{"command": "echo before-fail; exit 3"})
	assert.Equal(t, false, res["success"])
	text := fmt.Sprint(res["error"])
	assert.Contains(t, text, "before-fail")
}

// TestBashTool_输出截断 max_output_chars 生效时内容被裁剪。
func TestBashTool_输出截断(t *testing.T) {
	res := invokeBash(t, newBashTool(t), map[string]any{
		"command":          `python3 -c "print('a' * 2000)"`,
		"max_output_chars": 100,
	})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))
	content := resultContent(res)
	assert.Contains(t, content, "openjiuwen_bash_outputs", "超长输出必须落盘并给出路径")
	assert.Less(t, len(content), 3000, "落盘提示不得回传全量输出")
}

// TestBashTool_后台执行 run_in_background 立即返回 pid。
func TestBashTool_后台执行(t *testing.T) {
	res := invokeBash(t, newBashTool(t), map[string]any{
		"command":           `python3 -c "import time; time.sleep(0.2)"`,
		"run_in_background": true,
	})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "started", data["status"])
	pid, ok := data["pid"].(float64)
	require.True(t, ok, "pid 应为数值，实际 %T", data["pid"])
	assert.Greater(t, pid, float64(0))
}

// TestBashTool_严格模式拦截注入 OPENJIUWEN_BASH_STRICT=1 时危险命令被拒。
func TestBashTool_严格模式拦截注入(t *testing.T) {
	t.Setenv("OPENJIUWEN_BASH_STRICT", "1")

	res := invokeBash(t, newBashTool(t), map[string]any{"command": "echo hi; rm -rf /tmp/should-not-run"})
	assert.Equal(t, false, res["success"])
	assert.NotEmpty(t, res["error"])
}

// TestBashStreamTool_流式输出 流式工具返回输出块。
func TestBashStreamTool_流式输出(t *testing.T) {
	tl := NewBashStreamTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-bash", PermissionConfig{})
	ch, err := tl.Stream(context.Background(), map[string]any{"command": "echo stream-chunk"})
	require.NoError(t, err)

	var chunks []tool.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	require.NotEmpty(t, chunks, "流式执行必须返回至少一个块")

	joined := strings.Builder{}
	for _, chunk := range chunks {
		joined.WriteString(fmt.Sprint(chunk.Data))
	}
	assert.Contains(t, joined.String(), "stream-chunk")
}

// TestBashStreamTool_空命令 空命令以错误块形式返回。
func TestBashStreamTool_空命令(t *testing.T) {
	tl := NewBashStreamTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-bash", PermissionConfig{})
	ch, err := tl.Stream(context.Background(), map[string]any{"command": ""})
	require.NoError(t, err)

	var chunks []tool.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	require.NotEmpty(t, chunks)
	assert.Contains(t, fmt.Sprint(chunks[0].Data), "command cannot be empty")
}
