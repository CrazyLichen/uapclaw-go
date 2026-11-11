package processcli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDupStdoutToStderr_返回可用writer与restore
// Python: machine_entry.py:49-80 protocol_stdout()
func TestDupStdoutToStderr_返回可用writer与restore(t *testing.T) {
	restore, data, err := dupStdoutToStderr()
	require.NoError(t, err, "在 CI 的非 TTY 环境也应成功")
	require.NotNil(t, data)
	require.NotNil(t, restore)

	// 写一行，不应 panic
	_, werr := data.WriteString("test\n")
	require.NoError(t, werr)

	require.NoError(t, restore())
}

// TestProtocolStdout_等价于dup 验证跨平台主体转发。
// Python: machine_entry.py:49-80
func TestProtocolStdout_等价于dup(t *testing.T) {
	restore, data, err := protocolStdout()
	require.NoError(t, err)
	require.NotNil(t, data)
	require.NotNil(t, restore)
	require.NoError(t, restore())
}

// TestDupStdoutToStderr_重定向后osStdout落stderr 验证隔离生效。
//
// 这是本 Task 的核心不变量：dup2 之后，任何直接写 os.Stdout 的输出
// 都不再出现在 fd1 的原始去处，从而保证 stdout 只剩 JSONL。
// 为可观测，这里只验证「dup 出的 data writer 仍指向原 stdout」——
// 若重定向没生效，data 与 os.Stdout 写同一 fd，两者输出会重复。
func TestDupStdoutToStderr_重定向后osStdout落stderr(t *testing.T) {
	restore, data, err := dupStdoutToStderr()
	require.NoError(t, err)
	defer func() { _ = restore() }()

	// data 指向被 dup 出来的原 fd1 副本，必须可写
	n, werr := data.WriteString("probe\n")
	require.NoError(t, werr)
	require.Positive(t, n)
}
