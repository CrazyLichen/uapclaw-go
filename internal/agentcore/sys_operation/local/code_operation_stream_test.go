package local

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newCodeOperation 构造本地代码执行操作实例。
func newCodeOperation(t *testing.T) *LocalCodeOperation {
	t.Helper()
	return NewLocalCodeOperation(nil).(*LocalCodeOperation)
}

// ──────────────────────────── 用例 ────────────────────────────

// TestBuildSubprocessCmd_四种语言分支 python/javascript/非法语言。
func TestBuildSubprocessCmd_四种语言分支(t *testing.T) {
	op := newCodeOperation(t)

	args, tmpFile, err := op.buildSubprocessCmd(`print("hi")`, "python", false)
	require.NoError(t, err)
	assert.Empty(t, tmpFile, "短脚本走命令行内联")
	assert.Contains(t, args, "-u")

	args, tmpFile, err = op.buildSubprocessCmd(`console.log(1)`, "javascript", false)
	require.NoError(t, err)
	assert.Empty(t, tmpFile)
	assert.Equal(t, "node", args[0])

	_, _, err = op.buildSubprocessCmd("code", "ruby", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不支持的语言")
}

// TestBuildSubprocessCmd_强制落盘文件 force_file 时写入临时脚本文件。
func TestBuildSubprocessCmd_强制落盘文件(t *testing.T) {
	op := newCodeOperation(t)

	args, tmpFile, err := op.buildSubprocessCmd(`print("hi")`, "python", true)
	require.NoError(t, err)
	require.NotEmpty(t, tmpFile)
	t.Cleanup(func() { _ = os.Remove(tmpFile) })
	assert.Contains(t, args, tmpFile)
}

// TestExecuteCodeStream_流式输出 逐块返回 stdout 并以退出码收尾。
func TestExecuteCodeStream_流式输出(t *testing.T) {
	op := newCodeOperation(t)

	ch, err := op.ExecuteCodeStream(context.Background(), `print("stream-hello")`,
		sysop.WithCodeLanguage("python"), sysop.WithCodeTimeout(30))
	require.NoError(t, err)

	var text strings.Builder
	sawExit := false
	for item := range ch {
		require.NotNil(t, item.Data)
		if item.Data.Text != "" {
			text.WriteString(item.Data.Text)
		}
		if item.Data.ExitCode != nil {
			sawExit = true
			assert.Equal(t, 0, *item.Data.ExitCode)
		}
	}
	assert.Contains(t, text.String(), "stream-hello")
	assert.True(t, sawExit, "流式执行必须以退出事件收尾")
}

// TestExecuteCodeStream_非法语言 不支持的语言在启动前返回错误。
func TestExecuteCodeStream_非法语言(t *testing.T) {
	op := newCodeOperation(t)

	_, err := op.ExecuteCodeStream(context.Background(), "puts 1", sysop.WithCodeLanguage("ruby"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不支持的语言")
}

// TestExecuteCodeStream_运行时报错 非零退出码以错误块返回。
func TestExecuteCodeStream_运行时报错(t *testing.T) {
	op := newCodeOperation(t)

	ch, err := op.ExecuteCodeStream(context.Background(), `raise RuntimeError("boom")`,
		sysop.WithCodeLanguage("python"), sysop.WithCodeTimeout(30))
	require.NoError(t, err)

	exitCode := 0
	for item := range ch {
		if item.Data != nil && item.Data.ExitCode != nil {
			exitCode = *item.Data.ExitCode
		}
	}
	assert.NotEqual(t, 0, exitCode, "运行时异常必须返回非零退出码")
}

// TestExecuteCode_非零退出 同步执行返回非零退出码。
func TestExecuteCode_非零退出(t *testing.T) {
	op := newCodeOperation(t)

	res, err := op.ExecuteCode(context.Background(), `import sys; sys.exit(3)`,
		sysop.WithCodeLanguage("python"), sysop.WithCodeTimeout(30))
	require.NoError(t, err)
	require.NotNil(t, res.Data)
	require.NotNil(t, res.Data.ExitCode)
	assert.NotEqual(t, 0, *res.Data.ExitCode)
}
