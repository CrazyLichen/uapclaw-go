package tools

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// TestCheckCommandSafety_危险命令表 覆盖 Python _DANGEROUS_COMMAND_PATTERNS 全部 10 组正则。
func TestCheckCommandSafety_危险命令表(t *testing.T) {
	cases := []struct {
		cmd  string
		want string
	}{
		{"rm -rf /", "blocked pattern: rm -rf"},
		{"DEL /F /S /Q C:\\x", "blocked pattern: del /f /s /q"},
		{"rd /s /q C:\\x", "blocked pattern: rd /s /q"},
		{"format c:", "blocked pattern: format drive"},
		{"shutdown now", "blocked pattern: shutdown"},
		{"sudo reboot", "blocked pattern: reboot"},
		{"diskpart", "blocked pattern: diskpart"},
		{"mkfs.ext4", "blocked pattern: mkfs"},
		{"reg delete HKLM\\X", "blocked pattern: reg delete"},
		{"Remove-Item -Recurse -Force x", "blocked pattern: Remove-Item -Recurse -Force"},
		{"ls -la", ""}, // 安全命令返回空串
		{"echo hello", ""},
	}
	for _, c := range cases {
		got := checkCommandSafety(c.cmd)
		if got != c.want {
			t.Errorf("checkCommandSafety(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}

// TestSplitShellSegments_引号与分隔符 覆盖状态机：引号跟踪 + &&/|| 双字符 + |;\\n\\r 单字符。
func TestSplitShellSegments_引号与分隔符(t *testing.T) {
	cases := []struct {
		cmd  string
		want []string
	}{
		{"echo a && echo b", []string{"echo a", "echo b"}},
		{`echo "a | b" | grep x`, []string{`echo "a | b"`, "grep x"}},
		{"echo a; echo b\necho c", []string{"echo a", "echo b", "echo c"}},
		{"a || b", []string{"a", "b"}},
		{`echo 'it"s'`, []string{`echo 'it"s'`}},
		{"", []string{}},
		{"   ", []string{}},
	}
	for _, c := range cases {
		got := splitShellSegments(c.cmd)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("splitShellSegments(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

// TestSegmentBaseCommand_引号剥离与exe去除 验证 tokens[0] 提取 + 路径段剥离 + .exe 后缀去除。
func TestSegmentBaseCommand_引号剥离与exe去除(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"C:\Program Files\git.exe" --version`, "git"},
		{`/usr/bin/grep -n x`, "grep"},
		{`POWERSHELL.EXE -c x`, "powershell"},
		{"  ", ""},
	}
	for _, c := range cases {
		if got := segmentBaseCommand(c.in); got != c.want {
			t.Errorf("segmentBaseCommand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestLooksLikePowershell_特征token 验证 21 token + here-string + $var 检测。
func TestLooksLikePowershell_特征token(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"Get-ChildItem -Recurse", true},
		{"$env:PATH", true},
		{"$null exists", true},
		{"echo @'\nhi\n'@", true},
		{"value = $myVar", true}, // (^|[\s;(])\$[A-Za-z_]
		{"echo hello", false},
		{"", false},
	}
	for _, c := range cases {
		if got := looksLikePowershell(c.cmd); got != c.want {
			t.Errorf("looksLikePowershell(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

// TestUnwrapPowershellCommand_提取脚本 验证 -Command/-c 参数提取。
func TestUnwrapPowershellCommand_提取脚本(t *testing.T) {
	if got := unwrapPowershellCommand("powershell -Command \"Get-Date\""); got != "Get-Date" {
		t.Errorf("提取失败: %q", got)
	}
	if got := unwrapPowershellCommand("pwsh -c 'echo hi'"); got != "echo hi" {
		t.Errorf("-c 提取失败: %q", got)
	}
	if got := unwrapPowershellCommand("echo plain"); got != "" {
		t.Errorf("非 powershell 应返回空: %q", got)
	}
}

// TestNormalizeShellType_非法值回退auto 验证白名单外回退。
func TestNormalizeShellType_非法值回退auto(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "auto"},
		{"Bash", "bash"},
		{"  POWERSHELL  ", "powershell"},
		{"fish", "auto"},
	}
	for _, c := range cases {
		if got := normalizeShellType(c.in); got != c.want {
			t.Errorf("normalizeShellType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeWindowsPathsForBash_反斜杠替换 验证带引号/无引号 Windows 路径转换。
func TestNormalizeWindowsPathsForBash_反斜杠替换(t *testing.T) {
	if got := normalizeWindowsPathsForBash(`cat "C:\Users\x\a.txt"`); !strings.Contains(got, `C:/Users/x/a.txt`) {
		t.Errorf("带引号替换失败: %q", got)
	}
	if got := normalizeWindowsPathsForBash(`type C:\data\b.txt`); !strings.Contains(got, "C:/data/b.txt") {
		t.Errorf("无引号替换失败: %q", got)
	}
}

// TestClipText_截断 验证 Python _clip_text 语义。
func TestClipText_截断(t *testing.T) {
	if got := clipText("hello", 0); got != "hello" {
		t.Errorf("max=0 不裁剪: %q", got)
	}
	if got := clipText("hello", 10); got != "hello" {
		t.Errorf("短文本不裁剪: %q", got)
	}
	if got := clipText("hello world", 5); got != "hello\n...[truncated]" {
		t.Errorf("裁剪结果异常: %q", got)
	}
}

// TestResolveCommandWorkdir_三根白名单 验证 project_root/workspace_root/agent_workspace 三根白名单。
func TestResolveCommandWorkdir_三根白名单(t *testing.T) {
	// 相对路径基于 cwd 应通过（cwd 本身在三根内或其回退链内）
	if _, err := resolveCommandWorkdir(t.Context(), "."); err != nil {
		t.Errorf("相对路径基于 cwd 应通过: %v", err)
	}
	// /etc 通常不在任何允许根内（除非极端环境），越界应返回错误
	if _, err := resolveCommandWorkdir(t.Context(), "/etc"); err == nil {
		t.Error("/etc 越界应返回错误")
	}
}

// TestResolveExecutionPlan_linuxAuto 验证 Linux auto 分支 bash 探测。
func TestResolveExecutionPlan_linuxAuto(t *testing.T) {
	plan, useShell, resolved := resolveExecutionPlan("echo hi", "auto")
	if useShell {
		t.Error("Linux auto 应返回 argv 形式而非 shell 模式")
	}
	if resolved != "bash" && resolved != "sh" {
		t.Errorf("resolved_shell 异常: %s", resolved)
	}
	if len(plan) < 2 {
		t.Errorf("plan 至少 [exe, flag, command]: %v", plan)
	}
}

// TestMcpExecCommand_空命令与危险命令 验证主流程前置检查。
func TestMcpExecCommand_空命令与危险命令(t *testing.T) {
	result, err := McpExecCommand.Invoke(t.Context(), map[string]any{
		"command": "   ",
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	if result["result"] != "[ERROR]: command cannot be empty." {
		t.Errorf("空命令文案异常: %v", result["result"])
	}

	result, err = McpExecCommand.Invoke(t.Context(), map[string]any{
		"command": "rm -rf /",
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	if result["result"] != "[ERROR]: command rejected for safety (blocked pattern: rm -rf)." {
		t.Errorf("危险命令文案异常: %v", result["result"])
	}
}

// TestMcpExecCommand_真实执行echo Linux 真实执行验证（bash 存在）。
func TestMcpExecCommand_真实执行echo(t *testing.T) {
	result, err := McpExecCommand.Invoke(t.Context(), map[string]any{
		"command": "echo hello24a", "workdir": ".",
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatalf("invoke err: %v", err)
	}
	s := result["result"].(string)
	if !strings.Contains(s, `"exit_code": 0`) || !strings.Contains(s, "hello24a") {
		t.Errorf("echo 输出异常: %s", s)
	}
}

// TestMcpExecCommand_后台进程启动 后台模式返回 started 状态。
func TestMcpExecCommand_后台进程启动(t *testing.T) {
	result, err := McpExecCommand.Invoke(t.Context(), map[string]any{
		"command": "sleep 30", "workdir": ".", "background": true,
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatalf("invoke err: %v", err)
	}
	s := result["result"].(string)
	if !strings.Contains(s, `"status": "started"`) {
		t.Errorf("后台启动异常: %s", s)
	}
}

// TestMcpExecCommand_非零退出落payload 验证 exit 1 返回 JSON payload 而非 [ERROR]。
// 对齐 Python subprocess.run 无 check=True：非零退出是正常返回值（S-01 修复）。
func TestMcpExecCommand_非零退出落payload(t *testing.T) {
	result, err := McpExecCommand.Invoke(t.Context(), map[string]any{
		"command": "echo to_stderr 1>&2; exit 1", "workdir": ".",
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatalf("invoke err: %v", err)
	}
	s, ok := result["result"].(string)
	if !ok {
		t.Fatalf("result 应为 string: %T", result["result"])
	}
	// 非零退出不应被包装成 [ERROR]
	if strings.Contains(s, "[ERROR]") {
		t.Errorf("非零退出不应返回 [ERROR]: %s", s)
	}
	// exit_code=1 + stdout/stderr 保留（Python payload 语义）
	if !strings.Contains(s, `"exit_code": 1`) {
		t.Errorf("payload 应含 exit_code 1: %s", s)
	}
	if !strings.Contains(s, "to_stderr") {
		t.Errorf("payload 应保留 stderr: %s", s)
	}
}

// TestMcpExecCommand_二进制不存在仍走ERROR 验证启动失败（非 ExitError）路径不变。
func TestMcpExecCommand_二进制不存在仍走ERROR(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux/macOS 专属路径")
	}
	// Linux 下 auto 解析为 bash -lc <command>，"命令不存在"是 bash 的 exit 127（ExitError），
	// 恰好落 payload——与 Python returncode=127 一致（该语义已由非零退出用例覆盖）。
	// 真正的非 ExitError 启动失败是 shell_type=cmd（Linux 不支持，plan=nil）：
	// Python raise RuntimeError 被 except Exception 捕获返回 [ERROR]，Go 走同路径。
	result, err := McpExecCommand.Invoke(t.Context(), map[string]any{
		"command": "echo hi", "workdir": ".", "shell_type": "cmd",
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatalf("invoke err: %v", err)
	}
	s, ok := result["result"].(string)
	if !ok {
		t.Fatalf("result 应为 string: %T", result["result"])
	}
	if !strings.Contains(s, "[ERROR]: command execution failed:") {
		t.Errorf("启动失败应返回 [ERROR]: %s", s)
	}
}

// TestResolveCommandWorkdir_绝对路径在白名单内 AgentWorkspaceDir 恒在白名单。
func TestResolveCommandWorkdir_绝对路径在白名单内(t *testing.T) {
	agentDir := agentWorkspaceDirForTest()
	if _, err := resolveCommandWorkdir(t.Context(), agentDir); err != nil {
		t.Errorf("AgentWorkspaceDir 应在白名单: %v", err)
	}
	// 子目录也在白名单
	if _, err := resolveCommandWorkdir(t.Context(), filepath.Join(agentDir, "sub")); err != nil {
		t.Errorf("AgentWorkspaceDir 子目录应在白名单: %v", err)
	}
}

// agentWorkspaceDirForTest 测试辅助：返回 workspace.AgentWorkspaceDir()。
func agentWorkspaceDirForTest() string {
	return workspaceAgentDir()
}
