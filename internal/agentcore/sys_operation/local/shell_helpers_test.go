package local

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── LooksLikePowerShell ────────────────────────────

// TestLooksLikePowerShell_令牌匹配 测试 PowerShell 令牌匹配
func TestLooksLikePowerShell_令牌匹配(t *testing.T) {
	assert.True(t, LooksLikePowerShell("powershell Get-ChildItem"))
	assert.True(t, LooksLikePowerShell("pwsh -Command 'hello'"))
	assert.True(t, LooksLikePowerShell("Get-ChildItem"))
	assert.True(t, LooksLikePowerShell("$env:PATH"))
	assert.True(t, LooksLikePowerShell("$true"))
}

// TestLooksLikePowerShell_HereString 测试 here-string 检测
func TestLooksLikePowerShell_HereString(t *testing.T) {
	assert.True(t, LooksLikePowerShell("@'\nhello\n'@"))
	assert.True(t, LooksLikePowerShell("@\"\nhello\n\"@"))
}

// TestLooksLikePowerShell_PSVariable 测试 PS 变量检测
func TestLooksLikePowerShell_PSVariable(t *testing.T) {
	assert.True(t, LooksLikePowerShell("$myVariable = 1"))
}

// TestLooksLikePowerShell_非PS 测试非 PowerShell 命令
func TestLooksLikePowerShell_非PS(t *testing.T) {
	assert.False(t, LooksLikePowerShell("ls -la"))
	assert.False(t, LooksLikePowerShell("echo hello"))
	assert.False(t, LooksLikePowerShell(""))
	assert.False(t, LooksLikePowerShell("   "))
}

// ──────────────────────────── SplitShellSegments ────────────────────────────

// TestSplitShellSegments 测试基本分隔
func TestSplitShellSegments(t *testing.T) {
	segments := SplitShellSegments("ls && cat file")
	assert.Equal(t, []string{"ls", "cat file"}, segments)
}

// TestSplitShellSegments_管道 测试管道分隔
func TestSplitShellSegments_管道(t *testing.T) {
	segments := SplitShellSegments("ls | grep foo")
	assert.Equal(t, []string{"ls", "grep foo"}, segments)
}

// TestSplitShellSegments_分号 测试分号分隔
func TestSplitShellSegments_分号(t *testing.T) {
	segments := SplitShellSegments("cd /tmp; ls")
	assert.Equal(t, []string{"cd /tmp", "ls"}, segments)
}

// TestSplitShellSegments_引号内分隔符 测试引号内不分割
func TestSplitShellSegments_引号内分隔符(t *testing.T) {
	segments := SplitShellSegments(`echo "a && b"`)
	assert.Equal(t, []string{`echo "a && b"`}, segments)
}

// TestSplitShellSegments_或运算 测试 || 分隔
func TestSplitShellSegments_或运算(t *testing.T) {
	segments := SplitShellSegments("cmd1 || cmd2")
	assert.Equal(t, []string{"cmd1", "cmd2"}, segments)
}

// ──────────────────────────── SegmentBaseCommand ────────────────────────────

// TestSegmentBaseCommand 测试提取基础命令
func TestSegmentBaseCommand(t *testing.T) {
	assert.Equal(t, "ls", SegmentBaseCommand("ls -la"))
	assert.Equal(t, "grep", SegmentBaseCommand("grep pattern"))
	assert.Equal(t, "cat", SegmentBaseCommand("/usr/bin/cat file"))
}

// TestSegmentBaseCommand_带引号 测试带引号
func TestSegmentBaseCommand_带引号(t *testing.T) {
	assert.Equal(t, "echo", SegmentBaseCommand(`echo "hello"`))
}

// TestSegmentBaseCommand_WindowsExe 测试 .exe 后缀
func TestSegmentBaseCommand_WindowsExe(t *testing.T) {
	assert.Equal(t, "cmd", SegmentBaseCommand("cmd.exe /c echo"))
}

// TestSegmentBaseCommand_空 测试空输入
func TestSegmentBaseCommand_空(t *testing.T) {
	assert.Equal(t, "", SegmentBaseCommand(""))
	assert.Equal(t, "", SegmentBaseCommand("   "))
}

// ──────────────────────────── LooksLikePosix ────────────────────────────

// TestLooksLikePosix 测试 POSIX 命令检测
func TestLooksLikePosix(t *testing.T) {
	assert.True(t, LooksLikePosix("ls -la"))
	assert.True(t, LooksLikePosix("grep pattern file"))
	assert.True(t, LooksLikePosix("find . -name '*.go'"))
	assert.False(t, LooksLikePosix("Get-ChildItem"))
	assert.False(t, LooksLikePosix(""))
}

// ──────────────────────────── stripMatchingQuotes / StripMatchingQuotes ────────────────────────────

// TestStripMatchingQuotes 测试去除匹配引号
func TestStripMatchingQuotes(t *testing.T) {
	assert.Equal(t, "hello", StripMatchingQuotes(`"hello"`))
	assert.Equal(t, "hello", StripMatchingQuotes(`'hello'`))
	assert.Equal(t, `"hello'`, StripMatchingQuotes(`"hello'`)) // 不匹配
	assert.Equal(t, "hello", StripMatchingQuotes("  \"hello\"  "))
	assert.Equal(t, "hello", StripMatchingQuotes("hello"))
}

// ──────────────────────────── UnwrapPowerShellCommand ────────────────────────────

// TestUnwrapPowerShellCommand 测试解包 PowerShell 命令
func TestUnwrapPowerShellCommand(t *testing.T) {
	assert.Equal(t, "Get-Item", UnwrapPowerShellCommand("powershell -Command Get-Item"))
	assert.Equal(t, "Get-ChildItem", UnwrapPowerShellCommand("pwsh -c \"Get-ChildItem\""))
	assert.Equal(t, "", UnwrapPowerShellCommand("echo hello")) // 非 PS 命令
}

// ──────────────────────────── AvailablePowerShell ────────────────────────────

// TestAvailablePowerShell 测试 PowerShell 查找
func TestAvailablePowerShell(t *testing.T) {
	// 仅验证不 panic，返回值取决于运行环境
	result := AvailablePowerShell()
	assert.NotEmpty(t, result)
}

// ──────────────────────────── AvailableBash ────────────────────────────

// TestAvailableBash 测试 Bash 查找
func TestAvailableBash(t *testing.T) {
	// 非 Windows 上应该找到 bash
	if runtime.GOOS != "windows" {
		result := AvailableBash(true)
		assert.NotEmpty(t, result)
	}
}

// ──────────────────────────── AvailableSh ────────────────────────────

// TestAvailableSh 测试 sh 查找
func TestAvailableSh(t *testing.T) {
	// 非 Windows 上应该找到 sh
	if runtime.GOOS != "windows" {
		result := AvailableSh()
		assert.NotEmpty(t, result)
	}
}

// ──────────────────────────── IsWSLBashPath ────────────────────────────

// TestIsWSLBashPath 测试 WSL Bash 路径检测
func TestIsWSLBashPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		assert.True(t, IsWSLBashPath(`C:\Windows\system32\bash.exe`))
		assert.True(t, IsWSLBashPath(`C:\Windows\System32\Microsoft\WindowsApps\bash.exe`))
	}
	assert.False(t, IsWSLBashPath("/usr/bin/bash"))
}

// ──────────────────────────── NormalizeWindowsPathsForBash ────────────────────────────

// TestNormalizeWindowsPathsForBash 测试 Windows 路径归一化
func TestNormalizeWindowsPathsForBash(t *testing.T) {
	// 带引号
	result := NormalizeWindowsPathsForBash(`"C:\Users\test\file.txt"`)
	assert.Equal(t, `"C:/Users/test/file.txt"`, result)

	// 不带引号
	result = NormalizeWindowsPathsForBash(`copy C:\Users\test\file.txt /tmp`)
	assert.Contains(t, result, "C:/Users/test/file.txt")
}

// ──────────────────────────── GitBashCandidates ────────────────────────────

// TestGitBashCandidates 测试 Git Bash 候选列表
func TestGitBashCandidates(t *testing.T) {
	candidates := GitBashCandidates()
	// 仅验证不 panic，结果取决于运行环境
	_ = candidates
}

// ──────────────────────────── AvailableGitBash ────────────────────────────

// TestAvailableGitBash 测试 Git Bash 查找
func TestAvailableGitBash(t *testing.T) {
	// 非 Windows 返回空
	if runtime.GOOS != "windows" {
		assert.Empty(t, AvailableGitBash())
	}
}
