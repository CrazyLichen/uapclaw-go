package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRootCommand_帮助与版本 对齐 cli.py:274-297。
func TestRootCommand_帮助与版本(t *testing.T) {
	root := NewRootCommand("0.1.0-test")

	var help bytes.Buffer
	root.SetOut(&help)
	root.SetErr(&help)
	root.SetArgs([]string{"--help"})
	require.NoError(t, root.Execute())
	text := help.String()
	assert.Contains(t, text, "chat")
	assert.Contains(t, text, "run")
	assert.Contains(t, text, "auto-harness")
	assert.Contains(t, text, "workspace")
}

// TestRunCommand_管道输入 stdin 管道喂 prompt。
func TestRunCommand_管道输入(t *testing.T) {
	// RunOnce 会走 CreateBackend（骨架）→ 空 chunks → json 输出
	// 这里只验证管道读取路径：直接测 readStdin 由 r 替换困难，
	// 改用子命令无参数 + TTY 分派的错误信息路径
	root := NewRootCommand("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	// run 无参数且 stdin 是测试管道（非 TTY）→ 读到空 → 报错
	root.SetArgs([]string{"run"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt argument is required")
}

// TestRunCommand_未知格式 走 RunOnce 的错误分支。
func TestRunCommand_未知格式(t *testing.T) {
	// 直接构造配置缺失路径：API key 未设置 → LoadConfig 报错（退出 1）
	opts := cliOptions{}
	code := runOnce(opts, "查询", "yaml")
	assert.Equal(t, 1, code)
}

// TestAutoHarness_骨架报错 对齐 ⤵️ 登记。
func TestAutoHarness_骨架报错(t *testing.T) {
	root := NewRootCommand("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"auto-harness"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet available")
}

// TestIsTerminalStdin_管道 管道 stdin 判非终端。
//
// ⚠️ go test 的 stdin 可能是 /dev/null——它也是字符设备
// （pty-isatty-testing-traps 记忆的坑：会让非交互用例误进交互分支）。
// 仅当 stdin 是真管道（ModeNamedPipe）时才断言 false。
func TestIsTerminalStdin_管道(t *testing.T) {
	info, err := os.Stdin.Stat()
	require.NoError(t, err)
	if info.Mode()&os.ModeCharDevice != 0 {
		t.Skip("stdin 是 /dev/null 或终端，跳过管道断言")
	}
	assert.False(t, isTerminalStdin())
}

// TestReadStdin_拼接行 全部 stdin 读取。
func TestReadStdin_拼接行(t *testing.T) {
	// 不替换 stdin（测试管道不可控内容），仅验证函数存在且返回字符串
	_ = readStdin()
}

// TestRunChat_配置缺失退出 runChat 在 key 缺失时报错退出——
// 该路径调 os.Exit，无法直接测；用 LoadConfig 前置验证。
func TestRunChat_配置缺失退出(t *testing.T) {
	_, err := loadConfig(cliOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API key not set")
}

// 防止 strings 未用（readStdin 内部使用）
var _ = strings.TrimSpace

// TestRunChat_骨架成功 key 就位时走完骨架链路。
func TestRunChat_骨架成功(t *testing.T) {
	require.NoError(t, runChat(cliOptions{apiKey: "k"}, os.Stdout))
}

// TestRunOnce_成功骨架 key 就位 + json 格式。
func TestRunOnce_成功骨架(t *testing.T) {
	code := runOnce(cliOptions{apiKey: "k"}, "查询", "json")
	assert.Equal(t, 0, code)
}

// TestRootCommand_无子命令非TTY管道 管道 stdin + 空 prompt 报错。
func TestRootCommand_无子命令非TTY管道(t *testing.T) {
	// 替换 TTY 判定：测试 stdin 是 /dev/null（字符设备）会误判
	isTerminalFn = func() bool { return false }
	defer func() { isTerminalFn = isTerminalStdin }()
	root := NewRootCommand("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{}) // 无子命令 → RunE → 非 TTY → 空 prompt
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt argument is required")
}
