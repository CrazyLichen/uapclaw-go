//go:build test

package shell

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestInterpretBashExitCode_零退出码 测试零退出码
func TestInterpretBashExitCode_零退出码(t *testing.T) {
	result := InterpretBashExitCode("grep foo bar.txt", 0, "match", "")
	if result.IsError {
		t.Errorf("退出码 0 不应为错误")
	}
}

// TestInterpretBashExitCode_grep退出码1 测试 grep 退出码 1
func TestInterpretBashExitCode_grep退出码1(t *testing.T) {
	result := InterpretBashExitCode("grep foo bar.txt", 1, "", "")
	if result.IsError {
		t.Errorf("grep 退出码 1 不应为错误，表示未找到匹配")
	}
}

// TestInterpretBashExitCode_grep退出码2 测试 grep 退出码 2（错误）
func TestInterpretBashExitCode_grep退出码2(t *testing.T) {
	result := InterpretBashExitCode("grep foo bar.txt", 2, "", "error")
	if !result.IsError {
		t.Errorf("grep 退出码 2 应为错误")
	}
}

// TestInterpretBashExitCode_find退出码1 测试 find 退出码 1
func TestInterpretBashExitCode_find退出码1(t *testing.T) {
	result := InterpretBashExitCode("find / -name foo", 1, "", "")
	if result.IsError {
		t.Errorf("find 退出码 1 不应为错误")
	}
}

// TestInterpretBashExitCode_diff退出码1 测试 diff 退出码 1
func TestInterpretBashExitCode_diff退出码1(t *testing.T) {
	result := InterpretBashExitCode("diff a.txt b.txt", 1, "", "")
	if result.IsError {
		t.Errorf("diff 退出码 1 不应为错误，表示文件不同")
	}
}

// TestInterpretBashExitCode_test退出码1 测试 test 退出码 1
func TestInterpretBashExitCode_test退出码1(t *testing.T) {
	result := InterpretBashExitCode("test -f file", 1, "", "")
	if result.IsError {
		t.Errorf("test 退出码 1 不应为错误，表示条件为假")
	}
}

// TestInterpretBashExitCode_未知命令退出码1 测试未知命令退出码 1
func TestInterpretBashExitCode_未知命令退出码1(t *testing.T) {
	result := InterpretBashExitCode("unknown_cmd", 1, "", "")
	if !result.IsError {
		t.Errorf("未知命令退出码 1 应为错误")
	}
}

// TestInterpretBashExitCode_管道命令 测试管道命令取最后一段
func TestInterpretBashExitCode_管道命令(t *testing.T) {
	result := InterpretBashExitCode("cat file | grep foo", 1, "", "")
	if result.IsError {
		t.Errorf("管道末尾 grep 退出码 1 不应为错误")
	}
}

// TestInterpretPowerShellExitCode_零退出码 测试零退出码
func TestInterpretPowerShellExitCode_零退出码(t *testing.T) {
	result := InterpretPowerShellExitCode("Get-ChildItem", 0, "", "")
	if result.IsError {
		t.Errorf("退出码 0 不应为错误")
	}
}

// TestInterpretPowerShellExitCode_GetChildItem退出码1有输出 测试 Get-ChildItem 退出码 1 有输出
func TestInterpretPowerShellExitCode_GetChildItem退出码1有输出(t *testing.T) {
	result := InterpretPowerShellExitCode("Get-ChildItem", 1, "some output", "")
	if result.IsError {
		t.Errorf("Get-ChildItem 退出码 1 有输出且无 stderr 不应为错误")
	}
}

// TestInterpretPowerShellExitCode_GetChildItem退出码1无输出 测试 Get-ChildItem 退出码 1 无输出
func TestInterpretPowerShellExitCode_GetChildItem退出码1无输出(t *testing.T) {
	result := InterpretPowerShellExitCode("Get-ChildItem", 1, "", "error")
	if !result.IsError {
		t.Errorf("Get-ChildItem 退出码 1 无输出应为错误")
	}
}

// TestInterpretPowerShellExitCode_SelectString退出码1 测试搜索命令退出码 1
func TestInterpretPowerShellExitCode_SelectString退出码1(t *testing.T) {
	result := InterpretPowerShellExitCode("Select-String 'foo' file.txt", 1, "", "")
	if result.IsError {
		t.Errorf("Select-String 退出码 1 无输出不应为错误，表示未找到")
	}
}

// TestInterpretPowerShellExitCode_只读命令退出码1有stdout 测试只读命令退出码 1 有 stdout
func TestInterpretPowerShellExitCode_只读命令退出码1有stdout(t *testing.T) {
	result := InterpretPowerShellExitCode("Get-Content file.txt", 1, "partial output", "")
	if result.IsError {
		t.Errorf("只读命令退出码 1 有 stdout 无 stderr 不应为错误")
	}
}

// TestClassifyCommand_bash搜索命令 测试 bash 搜索命令分类
func TestClassifyCommand_bash搜索命令(t *testing.T) {
	if kind := ClassifyCommand("grep foo", false); kind != CommandKindSearch {
		t.Errorf("grep 应为 Search，实际 %v", kind)
	}
	if kind := ClassifyCommand("find / -name x", false); kind != CommandKindSearch {
		t.Errorf("find 应为 Search，实际 %v", kind)
	}
}

// TestClassifyCommand_bash读取命令 测试 bash 读取命令分类
func TestClassifyCommand_bash读取命令(t *testing.T) {
	if kind := ClassifyCommand("cat file.txt", false); kind != CommandKindRead {
		t.Errorf("cat 应为 Read，实际 %v", kind)
	}
	if kind := ClassifyCommand("head -5 file", false); kind != CommandKindRead {
		t.Errorf("head 应为 Read，实际 %v", kind)
	}
}

// TestClassifyCommand_bash列表命令 测试 bash 列表命令分类
func TestClassifyCommand_bash列表命令(t *testing.T) {
	if kind := ClassifyCommand("ls -la", false); kind != CommandKindList {
		t.Errorf("ls 应为 List，实际 %v", kind)
	}
}

// TestClassifyCommand_bash中性命令 测试 bash 中性命令分类
func TestClassifyCommand_bash中性命令(t *testing.T) {
	if kind := ClassifyCommand("echo hello", false); kind != CommandKindNeutral {
		t.Errorf("echo 应为 Neutral，实际 %v", kind)
	}
}

// TestClassifyCommand_bash静默命令 测试 bash 静默命令分类
func TestClassifyCommand_bash静默命令(t *testing.T) {
	if kind := ClassifyCommand("mv a b", false); kind != CommandKindSilent {
		t.Errorf("mv 应为 Silent，实际 %v", kind)
	}
}

// TestClassifyCommand_bash其他命令 测试 bash 其他命令分类
func TestClassifyCommand_bash其他命令(t *testing.T) {
	if kind := ClassifyCommand("python3 script.py", false); kind != CommandKindOther {
		t.Errorf("未知命令应为 Other，实际 %v", kind)
	}
}

// TestClassifyCommand_ps搜索命令 测试 PowerShell 搜索命令分类
func TestClassifyCommand_ps搜索命令(t *testing.T) {
	if kind := ClassifyCommand("Select-String 'foo'", true); kind != CommandKindSearch {
		t.Errorf("Select-String 应为 Search，实际 %v", kind)
	}
}

// TestClassifyCommand_ps读取命令 测试 PowerShell 读取命令分类
func TestClassifyCommand_ps读取命令(t *testing.T) {
	if kind := ClassifyCommand("Get-Content file.txt", true); kind != CommandKindRead {
		t.Errorf("Get-Content 应为 Read，实际 %v", kind)
	}
}

// TestClassifyCommand_ps列表命令 测试 PowerShell 列表命令分类
func TestClassifyCommand_ps列表命令(t *testing.T) {
	if kind := ClassifyCommand("Get-ChildItem", true); kind != CommandKindList {
		t.Errorf("Get-ChildItem 应为 List，实际 %v", kind)
	}
}

// TestIsSilent_全静默命令 测试全静默命令
func TestIsSilent_全静默命令(t *testing.T) {
	if !IsSilent("mv a b", false) {
		t.Errorf("mv 应为静默命令")
	}
	if !IsSilent("cp a b && mv c d", false) {
		t.Errorf("cp 和 mv 都是静默命令")
	}
}

// TestIsSilent_非静默命令 测试非静默命令
func TestIsSilent_非静默命令(t *testing.T) {
	if IsSilent("ls -la", false) {
		t.Errorf("ls 不是静默命令")
	}
	if IsSilent("cat file && mv a b", false) {
		t.Errorf("cat 不是静默命令")
	}
}

// TestIsSilent_ps静默命令 测试 PowerShell 静默命令
func TestIsSilent_ps静默命令(t *testing.T) {
	if !IsSilent("Set-Location /tmp", true) {
		t.Errorf("Set-Location 应为静默命令")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestGrepSemantics 测试 grep 族退出码语义
func TestGrepSemantics(t *testing.T) {
	tests := []struct {
		code    int
		isError bool
		hasMsg  bool
	}{
		{0, false, false},
		{1, false, true},
		{2, true, true},
	}
	for _, tt := range tests {
		result := grepSemantics(tt.code)
		if result.IsError != tt.isError {
			t.Errorf("grepSemantics(%d) IsError = %v, want %v", tt.code, result.IsError, tt.isError)
		}
		if tt.hasMsg && result.Message == "" {
			t.Errorf("grepSemantics(%d) 应有消息", tt.code)
		}
	}
}

// TestFindSemantics 测试 find 命令退出码语义
func TestFindSemantics(t *testing.T) {
	if findSemantics(0).IsError {
		t.Errorf("find 退出码 0 不应为错误")
	}
	if findSemantics(1).IsError {
		t.Errorf("find 退出码 1 不应为错误")
	}
	if !findSemantics(2).IsError {
		t.Errorf("find 退出码 2 应为错误")
	}
}

// TestDiffSemantics 测试 diff 命令退出码语义
func TestDiffSemantics(t *testing.T) {
	if diffSemantics(0).IsError {
		t.Errorf("diff 退出码 0 不应为错误")
	}
	if diffSemantics(1).IsError {
		t.Errorf("diff 退出码 1 不应为错误")
	}
	if !diffSemantics(2).IsError {
		t.Errorf("diff 退出码 2 应为错误")
	}
}

// TestTestSemantics 测试 test 命令退出码语义
func TestTestSemantics(t *testing.T) {
	if testSemantics(0).IsError {
		t.Errorf("test 退出码 0 不应为错误")
	}
	if testSemantics(1).IsError {
		t.Errorf("test 退出码 1 不应为错误")
	}
	if !testSemantics(2).IsError {
		t.Errorf("test 退出码 2 应为错误")
	}
}

// TestPsReadSemantics 测试 PowerShell 读取命令退出码语义
func TestPsReadSemantics(t *testing.T) {
	if psReadSemantics(0, "").IsError {
		t.Errorf("退出码 0 不应为错误")
	}
	if psReadSemantics(1, "").IsError {
		t.Errorf("退出码 1 无 stderr 不应为错误")
	}
	if !psReadSemantics(1, "some error").IsError {
		t.Errorf("退出码 1 有 stderr 应为错误")
	}
}

// TestPsGetChildItemSemantics 测试 Get-ChildItem 退出码语义
func TestPsGetChildItemSemantics(t *testing.T) {
	if psGetChildItemSemantics(0, "", "").IsError {
		t.Errorf("退出码 0 不应为错误")
	}
	if psGetChildItemSemantics(1, "output", "").IsError {
		t.Errorf("退出码 1 有 stdout 无 stderr 不应为错误")
	}
	if !psGetChildItemSemantics(1, "", "error").IsError {
		t.Errorf("退出码 1 有 stderr 应为错误")
	}
}

// TestPsSearchSemantics 测试搜索命令退出码语义
func TestPsSearchSemantics(t *testing.T) {
	if psSearchSemantics(0, "", "").IsError {
		t.Errorf("退出码 0 不应为错误")
	}
	if psSearchSemantics(1, "", "").IsError {
		t.Errorf("退出码 1 无输出不应为错误")
	}
	if !psSearchSemantics(1, "output", "").IsError {
		t.Errorf("退出码 1 有输出应为错误")
	}
}

// TestItoa 测试整数转字符串
func TestItoa(t *testing.T) {
	tests := []struct {
		in  int
		out string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{-1, "-1"},
		{123, "123"},
	}
	for _, tt := range tests {
		if got := itoa(tt.in); got != tt.out {
			t.Errorf("itoa(%d) = %q, want %q", tt.in, got, tt.out)
		}
	}
}
