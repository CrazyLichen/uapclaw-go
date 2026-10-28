// mcp_exec_command 工具：命令行执行。
// 对齐 Python jiuwenswarm/agents/harness/common/tools/command_tools.py（541 行）。

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mcpExecCommandInput mcp_exec_command 工具输入参数
// Python: mcp_exec_command(command, timeout_seconds=300, workdir=".",
// max_output_chars=0, shell_type="auto", background=False)
type mcpExecCommandInput struct {
	// Command 命令行
	Command string `json:"command" jsonschema:"description=The command line to execute,required"`
	// TimeoutSeconds 超时秒数（默认 300）
	TimeoutSeconds int `json:"timeout_seconds" jsonschema:"description=Optional timeout in seconds"`
	// Workdir 工作目录（默认 "."）
	Workdir string `json:"workdir" jsonschema:"description=Working directory for the command"`
	// MaxOutputChars 输出裁剪上限（0 不裁剪）
	MaxOutputChars int `json:"max_output_chars" jsonschema:"description=Optional max output chars before clipping (0 disables clipping)"`
	// ShellType shell 类型 auto|cmd|powershell|bash|sh
	ShellType string `json:"shell_type" jsonschema:"description=Optional shell type: auto|cmd|powershell|bash|sh"`
	// Background 是否后台执行
	Background bool `json:"background" jsonschema:"description=Run non-blocking in background when true"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识（沿 skill_toolkit.go 先例用 ComponentAgentServer）
	logComponent = logger.ComponentAgentServer
	// execCommandMaxTimeoutEnv 超时上限环境变量
	execCommandMaxTimeoutEnv = "MCP_EXEC_COMMAND_MAX_TIMEOUT_SECONDS"
	// execCommandDefaultTimeout 默认超时
	execCommandDefaultTimeout = 300
	// execCommandDefaultMaxTimeout 超时上限默认值（env 解析失败回退 3600）
	execCommandDefaultMaxTimeout = 600
	// execCommandFallbackMaxTimeout env 解析失败时回退值
	execCommandFallbackMaxTimeout = 3600
	// jwStartNewSessionEnv 进程组环境变量（对齐 Python JW_START_NEW_SESSION）
	jwStartNewSessionEnv = "JW_START_NEW_SESSION"
	// backgroundGraceSeconds 后台进程宽限秒数（Python grace_seconds=5.0）
	backgroundGraceSeconds = 5.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

// dangerousCommandPatterns 危险命令正则表
// Python: _DANGEROUS_COMMAND_PATTERNS（10 组，逐字）
var dangerousCommandPatterns = []struct {
	pattern *regexp.Regexp
	message string
}{
	{regexp.MustCompile(`(?i)\brm\s+-rf\b`), "blocked pattern: rm -rf"},
	{regexp.MustCompile(`(?i)\bdel\s+/[a-z]*[fsq][a-z]*\b`), "blocked pattern: del /f /s /q"},
	{regexp.MustCompile(`(?i)\brd\s+/s\s+/q\b`), "blocked pattern: rd /s /q"},
	{regexp.MustCompile(`(?i)\bformat\s+[a-z]:`), "blocked pattern: format drive"},
	{regexp.MustCompile(`(?i)\bshutdown\b`), "blocked pattern: shutdown"},
	{regexp.MustCompile(`(?i)\breboot\b`), "blocked pattern: reboot"},
	{regexp.MustCompile(`(?i)\bdiskpart\b`), "blocked pattern: diskpart"},
	{regexp.MustCompile(`(?i)\bmkfs\b`), "blocked pattern: mkfs"},
	{regexp.MustCompile(`(?i)\breg\s+delete\b`), "blocked pattern: reg delete"},
	{regexp.MustCompile(`(?is)\bremove-item\b[^\n\r]*-recurse[^\n\r]*-force`), "blocked pattern: Remove-Item -Recurse -Force"},
}

// powershellTokens PowerShell 特征 token
// Python: _POWERSHELL_TOKENS（21 项，逐字——command_tools.py L39-61）
var powershellTokens = []string{
	"powershell ", "powershell.exe ", "pwsh ", "pwsh.exe ",
	"get-childitem", "set-location", "remove-item", "test-path",
	"join-path", "select-object", "where-object", "foreach-object",
	"invoke-webrequest", "invoke-restmethod", "out-file", "start-process",
	"$env:", "$psversiontable", "$null", "$true", "$false",
}

// posixCommands POSIX 命令集合
// Python: _POSIX_COMMANDS
var posixCommands = map[string]bool{
	"ls": true, "grep": true, "egrep": true, "fgrep": true, "cat": true,
	"head": true, "tail": true, "find": true, "rm": true, "cp": true,
	"mv": true, "touch": true, "chmod": true, "chown": true, "sed": true,
	"awk": true, "gawk": true, "cut": true, "sort": true, "uniq": true,
	"wc": true, "du": true, "df": true, "pwd": true, "which": true,
	"mkdir": true,
}

var (
	// powershellExecutablePattern PowerShell 可执行前缀
	// Python: _POWERSHELL_EXECUTABLE_PATTERN
	powershellExecutablePattern = regexp.MustCompile(`(?i)^\s*(?:powershell(?:\.exe)?|pwsh(?:\.exe)?)\b`)
	// powershellCommandArgPattern -Command/-c 参数提取（RE2 支持 (?P<name>...) 命名组）
	// Python: _POWERSHELL_COMMAND_ARG_PATTERN
	powershellCommandArgPattern = regexp.MustCompile(`(?is)(?:^|\s)-(?:command|c)\s+(?P<script>.+)\s*$`)
	// quotedWindowsPathCore 带引号 Windows 路径核心（引号内路径）。
	// Python: _QUOTED_WINDOWS_PATH_PATTERN 用 (?P=quote) 反向引用保证首尾引号同类；
	// RE2 不支持反向引用——由 normalizeWindowsPathsForBash 手动校验首尾引号一致实现等价语义。
	quotedWindowsPathCore = regexp.MustCompile(`['"]([A-Za-z]:\\[^'"]+)['"]`)
	// unquotedWindowsPathCore 无引号 Windows 路径核心正则。
	// Python: _UNQUOTED_WINDOWS_PATH_PATTERN 带 lookbehind (?<![\w/])，RE2 不支持 lookbehind——
	// 由 normalizeWindowsPathsForBash 手动检查匹配起点前一字符实现等价语义。
	unquotedWindowsPathCore = regexp.MustCompile(`[A-Za-z]:\\[^\s|&;]+`)
	// shellVariablePattern PowerShell $var 特征
	// Python: _looks_like_powershell 内 re.search(r"(^|[\s;(])\$[A-Za-z_][A-Za-z0-9_]*", command)
	shellVariablePattern = regexp.MustCompile(`(^|[\s;(])\$[A-Za-z_][A-Za-z0-9_]*`)
)

// McpExecCommand mcp_exec_command 工具单例（对齐 Python @tool 模块级实例）
var McpExecCommand = newMcpExecCommandTool()

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// workspaceAgentDir 获取 Agent 工作空间目录（测试辅助暴露点）。
func workspaceAgentDir() string {
	return workspace.AgentWorkspaceDir()
}

// clipText 裁剪文本到指定长度。
// Python: _clip_text
func clipText(value string, maxChars int) string {
	if maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	return value[:maxChars] + "\n...[truncated]"
}

// checkCommandSafety 检查命令是否命中危险模式，命中返回说明文案。
// Python: _check_command_safety
func checkCommandSafety(command string) string {
	for _, p := range dangerousCommandPatterns {
		if p.pattern.MatchString(command) {
			return p.message
		}
	}
	return ""
}

// contextCwd 获取当前工作目录（resolve 后）。
// Python: _context_cwd —— get_cwd 失败回退 get_agent_workspace_dir
// Go 差异：cwd.GetCwd 无 error 返回（ctx 未绑定时回退 os.Getwd），恒非空。
func contextCwd(ctx context.Context) string {
	dir := cwd.GetCwd(ctx)
	if dir == "" {
		return workspace.AgentWorkspaceDir()
	}
	return resolvePathString(dir)
}

// contextProjectRoot 获取项目根目录（resolve 后）。
// Python: _context_project_root —— 失败回退 _context_cwd
// Go 差异：GetProjectRoot 无 error 返回（回退链 internal 已处理），恒非空。
func contextProjectRoot(ctx context.Context) string {
	dir := cwd.GetProjectRoot(ctx)
	if dir == "" {
		return contextCwd(ctx)
	}
	return resolvePathString(dir)
}

// contextWorkspaceRoot 获取 agent workspace（空=未设置）。
// Python: _context_workspace_root —— 失败返回 None
func contextWorkspaceRoot(ctx context.Context) string {
	dir := cwd.GetWorkspace(ctx)
	if dir == "" {
		return ""
	}
	return resolvePathString(dir)
}

// resolvePathString filepath.EvalSymlinks 失败回退 filepath.Abs（对齐 Python Path.resolve 宽松语义）。
func resolvePathString(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// isRelativeTo 判断 path 是否位于 root 之下。
// Python: _is_relative_to
func isRelativeTo(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveCommandWorkdir 解析并校验工作目录（三根白名单）。
// Python: _resolve_command_workdir（Go 签名差异：加 ctx 参数，因 Go cwd 函数需要 ctx）
// 白名单：project_root、workspace_root（非空时）、agent_workspace_dir。
func resolveCommandWorkdir(ctx context.Context, workdir string) (string, error) {
	currentCwd := contextCwd(ctx)
	projectRoot := contextProjectRoot(ctx)
	candidate := workdir
	if candidate == "" {
		candidate = currentCwd
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(currentCwd, candidate)
	}
	candidate = resolvePathString(candidate)

	allowedRoots := []string{projectRoot}
	if workspaceRoot := contextWorkspaceRoot(ctx); workspaceRoot != "" {
		allowedRoots = append(allowedRoots, workspaceRoot)
	}
	allowedRoots = append(allowedRoots, workspace.AgentWorkspaceDir())

	for _, root := range allowedRoots {
		if isRelativeTo(candidate, root) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("workdir is outside project workspace")
}

// normalizeShellType 归一化 shell 类型。
// Python: _normalize_shell_type
func normalizeShellType(shellType string) string {
	value := strings.ToLower(strings.TrimSpace(shellType))
	if value == "" {
		return "auto"
	}
	switch value {
	case "auto", "cmd", "powershell", "bash", "sh":
		return value
	default:
		return "auto"
	}
}

// looksLikePowershell 判断命令是否具有 PowerShell 特征。
// Python: _looks_like_powershell（21 token + @' @“ here-string + $var 正则）
func looksLikePowershell(command string) bool {
	lowered := strings.ToLower(strings.TrimSpace(command))
	if lowered == "" {
		return false
	}
	for _, token := range powershellTokens {
		if strings.Contains(lowered, token) {
			return true
		}
	}
	if strings.Contains(command, "@'") || strings.Contains(command, "@\"") {
		return true
	}
	if shellVariablePattern.MatchString(command) {
		return true
	}
	return false
}

// stripMatchingQuotes 剥离首尾匹配的同类引号。
// Python: _strip_matching_quotes
func stripMatchingQuotes(value string) string {
	stripped := strings.TrimSpace(value)
	if len(stripped) >= 2 && stripped[0] == stripped[len(stripped)-1] && (stripped[0] == '"' || stripped[0] == '\'') {
		return stripped[1 : len(stripped)-1]
	}
	return stripped
}

// unwrapPowershellCommand 从 powershell -Command "script" 形式中提取脚本。
// Python: _unwrap_powershell_command
func unwrapPowershellCommand(command string) string {
	if !powershellExecutablePattern.MatchString(command) {
		return ""
	}
	remainder := strings.TrimSpace(powershellExecutablePattern.ReplaceAllString(command, ""))
	match := powershellCommandArgPattern.FindStringSubmatch(remainder)
	if match == nil {
		return ""
	}
	// RE2 命名组索引：SubexpNames()[i] 对应 match[i]
	scriptIndex := 0
	for i, name := range powershellCommandArgPattern.SubexpNames() {
		if name == "script" {
			scriptIndex = i
			break
		}
	}
	if scriptIndex == 0 || scriptIndex >= len(match) {
		return ""
	}
	script := stripMatchingQuotes(match[scriptIndex])
	return script
}

// splitShellSegments 按分隔符切分命令行（引号保护）。
// Python: _split_shell_segments（手写状态机逐字复刻）
// 切分符：&&、|| 双字符 + |、;、\n、\r 单字符；引号内不切分。
func splitShellSegments(command string) []string {
	var segments []string
	var current []byte
	quote := byte(0)
	index := 0
	for index < len(command) {
		char := command[index]
		if char == '"' || char == '\'' {
			switch quote {
			case char:
				quote = 0
			case 0:
				quote = char
			}
		}
		if quote == 0 && (strings.HasPrefix(command[index:], "&&") || strings.HasPrefix(command[index:], "||")) {
			segment := strings.TrimSpace(string(current))
			if segment != "" {
				segments = append(segments, segment)
			}
			current = current[:0]
			index += 2
			continue
		}
		if quote == 0 && (char == '|' || char == ';' || char == '\n' || char == '\r') {
			segment := strings.TrimSpace(string(current))
			if segment != "" {
				segments = append(segments, segment)
			}
			current = current[:0]
			index += 1
			continue
		}
		current = append(current, char)
		index += 1
	}
	segment := strings.TrimSpace(string(current))
	if segment != "" {
		segments = append(segments, segment)
	}
	return segments
}

// segmentBaseCommand 提取命令段的基础命令名。
// Python: _segment_base_command —— shlex.split(segment, posix=False) tokens[0]
// Go 差异：无 shlex，firstShellToken 等价实现首 token 提取（引号成组保留，
// posix=False 语义——引号不剥离、反斜杠不转义）；stripMatchingQuotes 后取
// / 和 \ 最后段 + .exe 后缀去除 + 小写。
func segmentBaseCommand(segment string) string {
	token, ok := firstShellToken(segment)
	if !ok {
		return ""
	}
	base := stripMatchingQuotes(token)
	base = lastPathSegment(base)
	base = strings.ToLower(base)
	if strings.HasSuffix(base, ".exe") {
		return base[:len(base)-4]
	}
	return base
}

// firstShellToken 提取命令段首个 token（等价 Python shlex.split(segment, posix=False)[0]）。
// 引号成组保留在 token 内（posix=False 语义：引号不剥离、反斜杠不转义）；
// 未闭合引号返回 false（对齐 shlex ValueError → _segment_base_command 返回空串）。
func firstShellToken(segment string) (string, bool) {
	var token []byte
	inQuote := byte(0)
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		if c == '"' || c == '\'' {
			switch inQuote {
			case c:
				inQuote = 0
			case 0:
				inQuote = c
			}
			token = append(token, c)
			continue
		}
		if inQuote == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
			if len(token) > 0 {
				return string(token), true
			}
			continue
		}
		token = append(token, c)
	}
	if inQuote != 0 {
		return "", false
	}
	if len(token) == 0 {
		return "", false
	}
	return string(token), true
}

// lastPathSegment 取路径最后一段（同时处理 / 和 \ 分隔符，对齐 Python 双 rsplit）。
func lastPathSegment(p string) string {
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		p = p[idx+1:]
	}
	if idx := strings.LastIndex(p, "\\"); idx >= 0 {
		p = p[idx+1:]
	}
	return p
}

// normalizeWindowsPathsForBash 将 Windows 路径转换为 bash 兼容形式。
// Python: _normalize_windows_paths_for_bash
// RE2 差异（两处）：
//  1. 带引号路径的 (?P=quote) 反向引用——手动校验首尾引号字符一致（同为 ' 或同为 "）
//  2. 无引号路径的 lookbehind (?<![\w/])——手动检查匹配起点前一字符是否字母数字下划线或 /
func normalizeWindowsPathsForBash(command string) string {
	// 先处理带引号路径
	normalized := quotedWindowsPathCore.ReplaceAllStringFunc(command, func(m string) string {
		// m 形如 "C:\x\y" 或 'C:\x\y'——校验首尾引号同类
		if len(m) < 3 {
			return m
		}
		open, close := m[0], m[len(m)-1]
		if open != close {
			return m
		}
		path := m[1 : len(m)-1]
		return string(open) + strings.ReplaceAll(path, "\\", "/") + string(open)
	})
	// 再处理无引号路径（手动 lookbehind 等价）
	var b strings.Builder
	lastEnd := 0
	for _, loc := range unquotedWindowsPathCore.FindAllStringIndex(normalized, -1) {
		start, end := loc[0], loc[1]
		// 手动 lookbehind：前一个字符不能是 \w 或 /
		if start > 0 {
			prev := normalized[start-1]
			if prev == '/' || prev == '_' || (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') {
				continue
			}
		}
		b.WriteString(normalized[lastEnd:start])
		b.WriteString(strings.ReplaceAll(normalized[start:end], "\\", "/"))
		lastEnd = end
	}
	b.WriteString(normalized[lastEnd:])
	return b.String()
}

// availableUnixShell 探测 Unix shell（Linux/macOS 侧）。
// Python: _available_unix_shell
func availableUnixShell(preferBash bool) []string {
	if preferBash {
		if bash, err := exec.LookPath("bash"); err == nil {
			return []string{bash, "-lc"}
		}
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		sh = "/bin/sh"
	}
	if preferBash {
		return []string{sh, "-lc"}
	}
	return []string{sh, "-c"}
}

// resolveExecutionPlan 解析执行计划。
// Python: _resolve_execution_plan —— 返回 (argv 或命令字符串, 是否 shell 模式, resolved_shell)
// 平台分支拆分：Windows 专属逻辑在 mcp_command_windows.go / mcp_command_other.go。
func resolveExecutionPlan(command, shellType string) ([]string, bool, string) {
	normalized := normalizeShellType(shellType)
	if runtime.GOOS == "windows" {
		return resolveExecutionPlanWindows(command, normalized)
	}

	// Linux/macOS 分支
	if normalized == "auto" {
		if _, err := exec.LookPath("bash"); err == nil {
			normalized = "bash"
		} else {
			normalized = "sh"
		}
	}
	if normalized == "powershell" {
		exe, err := exec.LookPath("pwsh")
		if err != nil {
			exe, err = exec.LookPath("powershell")
		}
		if err != nil {
			return []string{"powershell"}, false, "powershell"
		}
		return []string{exe, "-NoProfile", "-NonInteractive", "-Command", command}, false, "powershell"
	}
	if normalized == "cmd" {
		// Python raise RuntimeError("shell_type 'cmd' is only supported on Windows.")
		// Go 侧返回哨兵由调用方处理；此处保语义：非 Windows 不支持 cmd。
		return nil, false, "cmd"
	}
	if normalized == "bash" {
		plan := availableUnixShell(true)
		return append(plan, command), false, "bash"
	}
	if normalized == "sh" {
		plan := availableUnixShell(false)
		return append(plan, command), false, "sh"
	}
	return nil, false, normalized
}

// runCommandBlocking 同步执行命令。
// Python: _run_command_sync —— subprocess.run + text encoding + errors='replace'
// Go 差异：输出 []byte 直接 string 转换（Go string 语义天然等价 Python errors='replace'
// 的"无损坏"路径；编码问题在 JSON 序列化时由 Go 运行时处理）。
// 超时返回 err（对齐 Python subprocess.TimeoutExpired 由上层转 "[ERROR]: command timed out"）。
func runCommandBlocking(command string, timeoutSeconds int, workdir string, shellType string) (exitCode int, stdout, stderr, resolvedShell string, err error) {
	plan, useShell, resolvedShell := resolveExecutionPlan(command, shellType)
	if plan == nil {
		return 0, "", "", "", fmt.Errorf("requested shell is not available on this system")
	}

	ctx := context.Background()
	if timeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
		defer cancel()
	}

	var cmd *exec.Cmd
	if useShell {
		cmd = exec.CommandContext(ctx, "cmd", "/c", plan[0])
	} else {
		cmd = exec.CommandContext(ctx, plan[0], plan[1:]...)
	}
	cmd.Dir = workdir
	applyStartNewSession(cmd)

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err = cmd.Run()
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	} else {
		exitCode = -1
	}
	return exitCode, stdoutBuf.String(), stderrBuf.String(), resolvedShell, err
}

// runCommandBackground 后台启动命令。
// Python: _run_command_background —— Popen + DEVNULL + 5s grace 轮询 Wait
// 返回 (pid, resolved_shell, error_msg)；error_msg 为 nil 表示成功（进程存活超宽限期）。
func runCommandBackground(command, workdir, shellType string) (int, string, string) {
	plan, useShell, resolvedShell := resolveExecutionPlan(command, shellType)
	if plan == nil {
		return 0, "", "requested shell is not available on this system"
	}

	var cmd *exec.Cmd
	if useShell {
		cmd = exec.Command("cmd", "/c", plan[0])
	} else {
		cmd = exec.Command(plan[0], plan[1:]...)
	}
	cmd.Dir = workdir
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	applyStartNewSession(cmd)

	if err := cmd.Start(); err != nil {
		return 0, resolvedShell, fmt.Sprintf("command failed to start: %v", err)
	}

	// 5s 宽限期轮询（Python proc.wait(timeout=grace)）
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return cmd.Process.Pid, resolvedShell, fmt.Sprintf("Process exited with code %d", exitErr.ExitCode())
			}
			return cmd.Process.Pid, resolvedShell, fmt.Sprintf("Process exited with code %d", cmd.ProcessState.ExitCode())
		}
		// exit_code == 0 —— Python 语义：非 0 才报错，0 也算 started 失败？
		// Python L435-437: exit_code != 0 才返回 error；== 0 时 fallthrough 返回成功。
		return cmd.Process.Pid, resolvedShell, ""
	case <-time.After(time.Duration(backgroundGraceSeconds * float64(time.Second))):
		// 宽限期后仍在运行 → 成功
		return cmd.Process.Pid, resolvedShell, ""
	}
}

// applyStartNewSession 按 JW_START_NEW_SESSION env 决定是否启用新会话（进程组）。
// Python: popen_kw["start_new_session"] = True（非 Windows 平台）
// Go 对应 syscall.SysProcAttr{Setsid: true}。
func applyStartNewSession(cmd *exec.Cmd) {
	if runtime.GOOS == "windows" {
		return
	}
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(jwStartNewSessionEnv)))
	if raw == "" {
		raw = "true"
	}
	switch raw {
	case "0", "false", "no", "off":
		// 显式关闭
	default:
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
}

// newMcpExecCommandTool 构造 mcp_exec_command 工具实例。
// Python: @tool 装饰的 mcp_exec_command（description 逐字复制）
func newMcpExecCommandTool() tool.Tool {
	card := tool.NewToolCard(
		"mcp_exec_command",
		"Execute simple cross-platform command-line command in project workspace. "+
			"Supports Windows cmd/PowerShell and macOS/Linux bash/sh. "+
			"Optional shell_type=auto|cmd|powershell|bash|sh. "+
			"Set background=True to run non-blocking (e.g. start a server); "+
			"returns immediately on success, error on failure. "+
			"Set max_output_chars=0 to disable output clipping. "+
			"Use a larger timeout_seconds for long-running commands. "+
			"Returns JSON: exit_code/stdout/stderr (blocking) or pid/status (background).",
		nil, // InputParams 由 NewTool 从 input struct 反射生成
		nil,
	)

	fn := func(ctx context.Context, input mcpExecCommandInput, opts ...tool.ToolOption) (map[string]any, error) {
		command := strings.TrimSpace(input.Command)
		if command == "" {
			return map[string]any{"result": "[ERROR]: command cannot be empty."}, nil
		}

		blockedReason := checkCommandSafety(command)
		if blockedReason != "" {
			return map[string]any{"result": fmt.Sprintf("[ERROR]: command rejected for safety (%s).", blockedReason)}, nil
		}

		resolvedWorkdir, err := resolveCommandWorkdir(ctx, input.Workdir)
		if err != nil {
			return map[string]any{"result": "[ERROR]: workdir is outside project workspace."}, nil
		}

		timeoutSeconds := input.TimeoutSeconds
		if timeoutSeconds == 0 {
			timeoutSeconds = execCommandDefaultTimeout
		}
		maxTimeoutSeconds := execCommandDefaultMaxTimeout
		if raw := os.Getenv(execCommandMaxTimeoutEnv); raw != "" {
			if parsed, pErr := strconv.Atoi(raw); pErr == nil {
				maxTimeoutSeconds = parsed
			} else {
				maxTimeoutSeconds = execCommandFallbackMaxTimeout
			}
		}
		if maxTimeoutSeconds < 1 {
			maxTimeoutSeconds = 1
		}
		if timeoutSeconds < 1 {
			timeoutSeconds = 1
		}
		if timeoutSeconds > maxTimeoutSeconds {
			timeoutSeconds = maxTimeoutSeconds
		}
		maxOutputChars := input.MaxOutputChars
		if maxOutputChars < 0 {
			maxOutputChars = 0
		}
		normalizedShellType := normalizeShellType(input.ShellType)

		if input.Background {
			pid, resolvedShell, bgErr := runCommandBackground(command, resolvedWorkdir, normalizedShellType)
			if bgErr != "" {
				return map[string]any{"result": fmt.Sprintf("[ERROR]: background command failed: %s", bgErr)}, nil
			}
			payload := map[string]any{
				"command":        command,
				"cwd":            resolvedWorkdir,
				"shell_type":     normalizedShellType,
				"resolved_shell": resolvedShell,
				"background":     true,
				"pid":            pid,
				"status":         "started",
			}
			data, _ := json.MarshalIndent(payload, "", "  ")
			return map[string]any{"result": string(data)}, nil
		}

		exitCode, stdout, stderr, resolvedShell, runErr := runCommandBlocking(command, timeoutSeconds, resolvedWorkdir, normalizedShellType)
		if runErr != nil {
			if isTimeoutError(runErr) {
				return map[string]any{"result": fmt.Sprintf("[ERROR]: command timed out after %ds.", timeoutSeconds)}, nil
			}
			var exitErr *exec.ExitError
			if errors.As(runErr, &exitErr) {
				// 非零退出 = 正常返回（对齐 Python subprocess.run 无 check=True，
				// exit 1 等探查型命令的退出码/stdout/stderr 落 payload 供 Agent 继续推理）。
				// exitCode 已由 runCommandBlocking 从 ProcessState.ExitCode() 提取；
				// 被信号杀死（ExitCode() == -1）时 Python 同样走 payload 路径，此处完整对齐。
			} else {
				return map[string]any{"result": fmt.Sprintf("[ERROR]: command execution failed: %v", runErr)}, nil
			}
		}

		payload := map[string]any{
			"command":        command,
			"cwd":            resolvedWorkdir,
			"shell_type":     normalizedShellType,
			"resolved_shell": resolvedShell,
			"exit_code":      exitCode,
			"stdout":         clipText(stdout, maxOutputChars),
			"stderr":         clipText(stderr, maxOutputChars),
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		return map[string]any{"result": string(data)}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// isTimeoutError 判断错误是否为超时（exec.CommandContext 超时时 ctx 被 cancel，
// 进程被杀后 Wait 返回 "signal: killed" 类错误）。
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "signal: killed") {
		return true
	}
	return strings.Contains(err.Error(), context.DeadlineExceeded.Error())
}
