package local

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── PowerShell 检测 ────────────────────────────

// powershellTokens PowerShell 检测令牌。
// 对齐 Python _POWERSHELL_TOKENS。
// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

var powershellTokens = []string{
	"powershell ", "powershell.exe ", "pwsh ", "pwsh.exe ",
	"get-childitem", "set-location", "remove-item", "test-path",
	"join-path", "select-object", "where-object", "foreach-object",
	"invoke-webrequest", "invoke-restmethod", "out-file", "start-process",
	"$env:", "$psversiontable", "$null", "$true", "$false",
}

// psVariablePattern PowerShell 变量模式。
// 对齐 Python _PS_VARIABLE_PATTERN。
var psVariablePattern = regexp.MustCompile(`(^|[\s;(])\$[A-Za-z_][A-Za-z0-9_]*`)

// powershellExecutablePattern PowerShell 可执行文件模式。
// 对齐 Python _POWERSHELL_EXECUTABLE_PATTERN。
var powershellExecutablePattern = regexp.MustCompile(`(?i)^\s*(?:powershell(?:\.exe)?|pwsh(?:\.exe)?)\b`)

// powershellCommandArgPattern PowerShell -Command 参数模式。
// 对齐 Python _POWERSHELL_COMMAND_ARG_PATTERN。
var powershellCommandArgPattern = regexp.MustCompile(`(?is)(?:^|\s)-(?:command|c)\s+(?P<script>.+)\s*$`)

// powershellCandidates PowerShell 候选可执行文件。
// 对齐 Python _POWERSHELL_CANDIDATES。
var powershellCandidates = []string{"pwsh", "powershell", "powershell.exe"}

// ──────────────────────────── POSIX 检测 ────────────────────────────

// posixCommands POSIX 命令集合。
// 对齐 Python _POSIX_COMMANDS。
var posixCommands = map[string]bool{
	"ls": true, "grep": true, "egrep": true, "fgrep": true, "cat": true,
	"head": true, "tail": true, "find": true, "rm": true, "cp": true,
	"mv": true, "touch": true, "chmod": true, "chown": true, "sed": true,
	"awk": true, "gawk": true, "cut": true, "sort": true, "uniq": true,
	"wc": true, "du": true, "df": true, "pwd": true, "which": true, "mkdir": true,
}

// ──────────────────────────── Windows 路径模式 ────────────────────────────

// Go 的 regexp 使用 RE2 语法，不支持 Python 的 (?P=quote) 反向引用和 (?<!...) lookbehind。
// 因此 Windows 路径归一化使用手动扫描实现，而非正则。
// 对齐 Python _QUOTED_WINDOWS_PATH_PATTERN 和 _UNQUOTED_WINDOWS_PATH_PATTERN。

// quotedWindowsPathPattern 带引号的 Windows 路径匹配（不使用反向引用，分别匹配单引号和双引号）。
var quotedWindowsPathPatternSingle = regexp.MustCompile(`'([A-Za-z]:\\[^']+)'`)
var quotedWindowsPathPatternDouble = regexp.MustCompile(`"([A-Za-z]:\\[^"]+)"`)

// unquotedWindowsPathPattern 不带引号的 Windows 路径匹配（简化：不使用 lookbehind）。
var unquotedWindowsPathPattern = regexp.MustCompile(`([A-Za-z]:\\[^\s|&;'"<>]+)`)

// ──────────────────────────── 导出函数 ────────────────────────────

// LooksLikePowerShell 判断命令是否看起来像 PowerShell。
// 对齐 Python _looks_like_powershell。
func LooksLikePowerShell(command string) bool {
	lowered := strings.TrimSpace(strings.ToLower(command))
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
	if psVariablePattern.MatchString(command) {
		return true
	}
	return false
}

// AvailablePowerShell 查找可用的 PowerShell 可执行文件路径。
// 对齐 Python _available_powershell。
func AvailablePowerShell() string {
	if runtime.GOOS == "windows" {
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			systemRoot = os.Getenv("WINDIR")
		}
		if systemRoot == "" {
			systemRoot = `C:\Windows`
		}
		systemPS := filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(systemPS); err == nil {
			return systemPS
		}
	}

	for _, candidate := range powershellCandidates {
		resolved, err := exec.LookPath(candidate)
		if err == nil && resolved != "" {
			return resolved
		}
	}
	return "powershell"
}

// UnwrapPowerShellCommand 从 PowerShell -Command 包装中提取脚本。
// 对齐 Python _unwrap_powershell_command。
func UnwrapPowerShellCommand(command string) string {
	if !powershellExecutablePattern.MatchString(command) {
		return ""
	}
	remainder := strings.TrimSpace(powershellExecutablePattern.ReplaceAllString(command, ""))
	match := powershellCommandArgPattern.FindStringSubmatchIndex(remainder)
	if match == nil {
		return ""
	}
	// 提取 script 命名组
	script := ""
	for i, name := range powershellCommandArgPattern.SubexpNames() {
		if name == "script" && match[2*i] >= 0 {
			script = remainder[match[2*i]:match[2*i+1]]
			break
		}
	}
	script = stripMatchingQuotes(script)
	if script == "" {
		return ""
	}
	return script
}

// IsWSLBashPath 判断路径是否是 WSL Bash 路径。
// 对齐 Python _is_wsl_bash_path。
func IsWSLBashPath(path string) bool {
	normalized := strings.ToLower(filepath.FromSlash(path))
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	systemRoot = strings.ToLower(filepath.FromSlash(systemRoot))
	expectedWSL := filepath.Join(systemRoot, "system32", "bash.exe")
	if normalized == strings.ToLower(expectedWSL) {
		return true
	}
	return strings.Contains(normalized, `\microsoft\windowsapps\bash.exe`)
}

// GitBashCandidates 获取 Git Bash 候选路径列表。
// 对齐 Python _git_bash_candidates。
func GitBashCandidates() []string {
	var candidates []string

	envPath := os.Getenv("GIT_BASH")
	if envPath == "" {
		envPath = os.Getenv("GIT_BASH_PATH")
	}
	if envPath != "" {
		candidates = append(candidates, envPath)
	}

	for _, root := range []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
	} {
		if root != "" {
			candidates = append(candidates, filepath.Join(root, "Git", "bin", "bash.exe"))
		}
	}
	localAppData := os.Getenv("LocalAppData")
	if localAppData != "" {
		candidates = append(candidates, filepath.Join(localAppData, "Programs", "Git", "bin", "bash.exe"))
	}

	gitPath, err := exec.LookPath("git")
	if err == nil && gitPath != "" {
		gitDir := filepath.Dir(gitPath)
		candidates = append(candidates, filepath.Join(filepath.Dir(gitDir), "bin", "bash.exe"))
	}

	return candidates
}

// AvailableGitBash 查找可用的 Git Bash 路径。
// 对齐 Python _available_git_bash。
func AvailableGitBash() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	for _, candidate := range GitBashCandidates() {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// AvailableBash 查找可用的 Bash 路径。
// 对齐 Python _available_bash。
func AvailableBash(allowWSL bool) string {
	if runtime.GOOS == "windows" {
		gitBash := AvailableGitBash()
		if gitBash != "" {
			return gitBash
		}
	}
	resolved, err := exec.LookPath("bash")
	if err == nil && resolved != "" {
		if !allowWSL && IsWSLBashPath(resolved) {
			return ""
		}
		return resolved
	}
	return ""
}

// AvailableSh 查找可用的 sh 路径。
// 对齐 Python _available_sh。
func AvailableSh() string {
	if runtime.GOOS == "windows" {
		bashPath := AvailableGitBash()
		if bashPath != "" {
			shPath := filepath.Join(filepath.Dir(filepath.Dir(bashPath)), "usr", "bin", "sh.exe")
			if _, err := os.Stat(shPath); err == nil {
				return shPath
			}
		}
	}
	resolved, err := exec.LookPath("sh")
	if err == nil {
		return resolved
	}
	return ""
}

// SplitShellSegments 将命令按 shell 分隔符（&&, ||, |, ;, \n）拆分为段。
// 对齐 Python _split_shell_segments。
func SplitShellSegments(command string) []string {
	var segments []string
	var current strings.Builder
	quote := byte(0)
	i := 0
	for i < len(command) {
		char := command[i]
		// 引号处理
		if char == '"' || char == '\'' {
			switch quote {
			case char:
				quote = 0
			case 0:
				quote = char
			}
		}
		// 分隔符检测（不在引号内时）
		if quote == 0 {
			// && 和 ||
			if i+1 < len(command) && (command[i:i+2] == "&&" || command[i:i+2] == "||") {
				segment := strings.TrimSpace(current.String())
				if segment != "" {
					segments = append(segments, segment)
				}
				current.Reset()
				i += 2
				continue
			}
			// |, ;, \n, \r
			if char == '|' || char == ';' || char == '\n' || char == '\r' {
				segment := strings.TrimSpace(current.String())
				if segment != "" {
					segments = append(segments, segment)
				}
				current.Reset()
				i++
				continue
			}
		}
		current.WriteByte(char)
		i++
	}
	segment := strings.TrimSpace(current.String())
	if segment != "" {
		segments = append(segments, segment)
	}
	return segments
}

// SegmentBaseCommand 从命令段中提取基础命令名。
// 对齐 Python _segment_base_command。
func SegmentBaseCommand(segment string) string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return ""
	}
	// 简化的 shlex.split：按空白拆分，处理引号
	tokens := simpleShellSplit(segment)
	if len(tokens) == 0 {
		return ""
	}
	base := stripMatchingQuotes(tokens[0])
	// 去掉路径前缀
	base = filepath.Base(base)
	base = strings.ToLower(base)
	// 去掉 .exe 后缀
	base = strings.TrimSuffix(base, ".exe")
	return base
}

// LooksLikePosix 判断命令是否看起来像 POSIX 命令。
// 对齐 Python _looks_like_posix。
func LooksLikePosix(command string) bool {
	for _, segment := range SplitShellSegments(command) {
		base := SegmentBaseCommand(segment)
		if posixCommands[base] {
			return true
		}
	}
	return false
}

// StripMatchingQuotes 去除字符串两端匹配的引号。
// 对齐 Python _strip_matching_quotes。
func StripMatchingQuotes(value string) string {
	return stripMatchingQuotes(value)
}

// NormalizeWindowsPathsForBash 将 Windows 路径中的反斜杠替换为正斜杠，以便 Bash 使用。
// 对齐 Python _normalize_windows_paths_for_bash。
func NormalizeWindowsPathsForBash(command string) string {
	// 先处理带引号的路径：'C:\path' 和 "C:\path"
	normalize := func(path string) string {
		return strings.ReplaceAll(path, `\`, "/")
	}

	// 双引号路径
	result := quotedWindowsPathPatternDouble.ReplaceAllStringFunc(command, func(match string) string {
		submatch := quotedWindowsPathPatternDouble.FindStringSubmatch(match)
		if len(submatch) >= 2 {
			return `"` + normalize(submatch[1]) + `"`
		}
		return match
	})

	// 单引号路径
	result = quotedWindowsPathPatternSingle.ReplaceAllStringFunc(result, func(match string) string {
		submatch := quotedWindowsPathPatternSingle.FindStringSubmatch(match)
		if len(submatch) >= 2 {
			return `'` + normalize(submatch[1]) + `'`
		}
		return match
	})

	// 不带引号的路径
	result = unquotedWindowsPathPattern.ReplaceAllStringFunc(result, func(match string) string {
		submatch := unquotedWindowsPathPattern.FindStringSubmatch(match)
		if len(submatch) >= 2 {
			return normalize(submatch[1])
		}
		return match
	})

	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stripMatchingQuotes 去除字符串两端匹配的引号。
// 对齐 Python _strip_matching_quotes。
func stripMatchingQuotes(value string) string {
	stripped := strings.TrimSpace(value)
	if len(stripped) >= 2 && stripped[0] == stripped[len(stripped)-1] && (stripped[0] == '"' || stripped[0] == '\'') {
		return stripped[1 : len(stripped)-1]
	}
	return stripped
}

// simpleShellSplit 简化的 shell 参数拆分（处理引号，不做完整 shlex 解析）。
func simpleShellSplit(s string) []string {
	var tokens []string
	var current strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		char := s[i]
		if char == '"' || char == '\'' {
			switch quote {
			case char:
				quote = 0
				tokens = append(tokens, current.String())
				current.Reset()
			case 0:
				if current.Len() > 0 {
					tokens = append(tokens, current.String())
					current.Reset()
				}
				quote = char
			default:
				current.WriteByte(char)
			}
			continue
		}
		if quote == 0 && char == ' ' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteByte(char)
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// trackShellProcess 追踪 Shell 进程到注册表。
// 对齐 Python _track_shell_process。
func trackShellProcess(ctx context.Context, proc *os.Process) string {
	sid := sysop.ResolveShellSessionID(ctx)
	if sid != "" {
		sysop.RegisterShellProcess(sid, proc)
	}
	return sid
}

// untrackShellProcess 从注册表注销 Shell 进程。
// 对齐 Python _untrack_shell_process。
func untrackShellProcess(sessionID string, proc *os.Process) {
	if sessionID != "" {
		sysop.UnregisterShellProcess(sessionID, proc)
	}
}
