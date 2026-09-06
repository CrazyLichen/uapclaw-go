//go:build test

package shell

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCheckBashInjection_反引号 测试反引号命令替换检测
func TestCheckBashInjection_反引号(t *testing.T) {
	detected, msg := CheckBashInjection("echo `whoami`")
	if !detected {
		t.Errorf("应检测到反引号注入")
	}
	if msg == "" {
		t.Errorf("应有检测消息")
	}
}

// TestCheckBashInjection_美元括号 测试 $() 命令替换检测
func TestCheckBashInjection_美元括号(t *testing.T) {
	detected, msg := CheckBashInjection("echo $(whoami)")
	if !detected {
		t.Errorf("应检测到 $() 注入")
	}
	_ = msg
}

// TestCheckBashInjection_进程替换 测试进程替换检测
func TestCheckBashInjection_进程替换(t *testing.T) {
	detected, _ := CheckBashInjection("diff <(ls a) <(ls b)")
	if !detected {
		t.Errorf("应检测到进程替换注入")
	}
}

// TestCheckBashInjection_安全命令 测试安全命令
func TestCheckBashInjection_安全命令(t *testing.T) {
	detected, _ := CheckBashInjection("ls -la /home")
	if detected {
		t.Errorf("安全命令不应检测到注入")
	}
}

// TestCheckBashInjection_单引号内反引号 测试单引号内反引号
func TestCheckBashInjection_单引号内反引号(t *testing.T) {
	detected, _ := CheckBashInjection("echo '`not injection`'")
	if detected {
		t.Errorf("单引号内反引号不应检测为注入")
	}
}

// TestCheckPowerShellInjection_InvokeExpression 测试 Invoke-Expression 检测
func TestCheckPowerShellInjection_InvokeExpression(t *testing.T) {
	detected, msg := CheckPowerShellInjection("Invoke-Expression 'cmd'")
	if !detected {
		t.Errorf("应检测到 Invoke-Expression 注入")
	}
	_ = msg
}

// TestCheckPowerShellInjection_编码命令 测试编码命令检测
func TestCheckPowerShellInjection_编码命令(t *testing.T) {
	detected, _ := CheckPowerShellInjection("powershell -EncodedCommand abc123")
	if !detected {
		t.Errorf("应检测到编码命令注入")
	}
}

// TestCheckPowerShellInjection_安全命令 测试安全命令
func TestCheckPowerShellInjection_安全命令(t *testing.T) {
	detected, _ := CheckPowerShellInjection("Get-ChildItem")
	if detected {
		t.Errorf("安全命令不应检测到注入")
	}
}

// TestGetBashDestructiveWarning_破坏性 测试破坏性命令
func TestGetBashDestructiveWarning_破坏性(t *testing.T) {
	warning := GetBashDestructiveWarning("git reset --hard HEAD")
	if warning == "" {
		t.Errorf("git reset --hard 应产生警告")
	}
	warning = GetBashDestructiveWarning("git push --force")
	if warning == "" {
		t.Errorf("git push --force 应产生警告")
	}
}

// TestGetBashDestructiveWarning_安全 测试安全命令
func TestGetBashDestructiveWarning_安全(t *testing.T) {
	warning := GetBashDestructiveWarning("ls -la")
	if warning != "" {
		t.Errorf("ls 不应产生警告")
	}
}

// TestGetPSDestructiveWarning 测试 PowerShell 破坏性警告
func TestGetPSDestructiveWarning(t *testing.T) {
	// 简单验证不会 panic
	_ = GetPSDestructiveWarning("Remove-Item -Recurse -Force C:\\")
	_ = GetPSDestructiveWarning("Get-ChildItem")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestHasBacktickSubstitution 测试反引号替换检测
func TestHasBacktickSubstitution(t *testing.T) {
	if !hasBacktickSubstitution("echo `whoami`") {
		t.Errorf("应检测到反引号替换")
	}
	if hasBacktickSubstitution("echo 'hello'") {
		t.Errorf("无反引号不应检测到")
	}
	if hasBacktickSubstitution("echo '`quoted`'") {
		t.Errorf("单引号内反引号不应检测到")
	}
	if hasBacktickSubstitution("echo hello") {
		t.Errorf("普通命令不应检测到")
	}
}
