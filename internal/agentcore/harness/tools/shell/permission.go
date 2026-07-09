package shell

import (
	"regexp"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionConfig 权限配置
type PermissionConfig struct {
	// Mode 权限模式
	Mode PermissionMode
	// DenyPatterns 拒绝模式列表
	DenyPatterns []*regexp.Regexp
	// AllowPatterns 允许模式列表
	AllowPatterns []*regexp.Regexp
}

// ──────────────────────────── 枚举 ────────────────────────────

// PermissionMode 权限模式枚举
type PermissionMode int

const (
	// PermissionModeAuto AUTO - 仅建议
	PermissionModeAuto PermissionMode = iota
	// PermissionModeReadOnly READ_ONLY - 只允许只读命令
	PermissionModeReadOnly
	// PermissionModeAcceptEdits ACCEPT_EDITS - 只允许已知安全命令
	PermissionModeAcceptEdits
	// PermissionModeBypass BYPASS - 绕过所有检查
	PermissionModeBypass
)

// ──────────────────────────── 全局变量 ────────────────────────────

// bash 文件操作命令集
// 对齐 Python: _FILE_OP_COMMANDS (bash/_permission.py L43-46)
var bashFileOpCommands = map[string]bool{
	"mkdir": true, "touch": true, "rm": true, "rmdir": true, "mv": true, "cp": true,
	"sed": true, "chmod": true, "chown": true, "chgrp": true, "ln": true,
}

// bash 已知安全命令集
// 对齐 Python: _KNOWN_SAFE_COMMANDS (bash/_permission.py L49-75)
var bashKnownSafeCommands = map[string]bool{
	// 搜索命令
	"find": true, "grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true, "ack": true,
	"locate": true, "which": true, "whereis": true, "type": true, "command": true,
	// 读取命令
	"cat": true, "head": true, "tail": true, "less": true, "more": true, "wc": true, "stat": true,
	"file": true, "strings": true, "jq": true, "yq": true, "awk": true, "gawk": true, "cut": true,
	"sort": true, "uniq": true, "tr": true, "tee": true, "od": true, "xxd": true, "hexdump": true,
	"sha256sum": true, "sha1sum": true, "md5sum": true, "md5": true, "shasum": true,
	// 列表命令
	"ls": true, "tree": true, "du": true, "df": true, "lsof": true,
	// 中性命令
	"echo": true, "printf": true, "true": true, "false": true, ":": true, "test": true, "[": true,
	// 静默/文件操作命令
	"mkdir": true, "touch": true, "rm": true, "rmdir": true, "mv": true, "cp": true,
	"sed": true, "chmod": true, "chown": true, "chgrp": true, "ln": true,
	"cd": true, "export": true, "unset": true, "source": true, ".": true, "wait": true, "pushd": true, "popd": true,
	// 常用开发工具
	"git": true, "python": true, "python3": true, "pip": true, "pip3": true, "uv": true,
	"node": true, "npm": true, "npx": true, "yarn": true, "pnpm": true,
	"make": true, "cmake": true, "cargo": true, "go": true, "java": true, "javac": true, "mvn": true, "gradle": true,
	"docker": true, "docker-compose": true, "kubectl": true,
	"curl": true, "wget": true, "ssh": true, "scp": true, "rsync": true,
	"tar": true, "zip": true, "unzip": true, "gzip": true, "gunzip": true,
	"date": true, "env": true, "id": true, "whoami": true, "hostname": true, "uname": true, "ps": true, "top": true,
	"diff": true, "patch": true, "xargs": true, "basename": true, "dirname": true, "realpath": true,
}

// powershell 文件操作命令集
// 对齐 Python: _FILE_OP_COMMANDS (powershell/_permission.py L34-39)
var psFileOpCommands = map[string]bool{
	"new-item": true, "ni": true, "remove-item": true, "ri": true, "rm": true,
	"move-item": true, "mi": true, "mv": true, "copy-item": true, "cp": true, "cpi": true,
	"rename-item": true, "rni": true, "set-content": true, "sc": true, "add-content": true, "ac": true,
	"clear-content": true, "clc": true,
}

// powershell 已知安全命令集
// 对齐 Python: _KNOWN_SAFE_COMMANDS (powershell/_permission.py L41-56)
var psKnownSafeCommands = map[string]bool{
	"get-childitem": true, "gci": true, "dir": true, "ls": true,
	"get-content": true, "gc": true, "type": true, "get-item": true, "gi": true, "test-path": true, "resolve-path": true, "get-filehash": true,
	"select-string": true, "findstr": true, "get-command": true, "where-object": true,
	"write-output": true, "echo": true, "write-host": true, "out-host": true,
	"set-location": true, "cd": true, "sl": true, "push-location": true, "pop-location": true,
	"new-item": true, "ni": true, "remove-item": true, "ri": true, "rm": true,
	"move-item": true, "mi": true, "mv": true, "copy-item": true, "cp": true, "cpi": true,
	"rename-item": true, "rni": true, "set-content": true, "sc": true, "add-content": true, "ac": true,
	"clear-content": true, "clc": true,
	"git": true, "python": true, "python3": true, "pip": true, "pip3": true, "uv": true,
	"node": true, "npm": true, "npx": true, "yarn": true, "pnpm": true,
	"make": true, "cmake": true, "cargo": true, "go": true, "java": true, "javac": true, "mvn": true, "gradle": true,
	"docker": true, "kubectl": true, "curl": true, "wget": true,
	"date": true, "get-date": true, "hostname": true, "whoami": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPermissionConfig 创建权限配置
func NewPermissionConfig(mode PermissionMode, denyPatterns, allowPatterns []string) PermissionConfig {
	return PermissionConfig{
		Mode:          mode,
		DenyPatterns:  CompilePatterns(denyPatterns),
		AllowPatterns: CompilePatterns(allowPatterns),
	}
}

// CheckPermission 5层权限检查管道。
// 对齐 Python: check_permission (bash/_permission.py L94-155)
// 第一层: BYPASS → 直接放行
// 第二层: denyPatterns → 任一 segment 命中任一 pattern → 拒绝
// 第三层: allowPatterns → 任一 pattern 命中整个命令 → 放行
// 第四层: READ_ONLY 模式 → 每个 segment 必须属于只读命令集
// 第四层: ACCEPT_EDITS 模式 → 每个 segment 必须在文件操作或已知安全命令集中
// 第五层: AUTO 模式 → bash 仅建议性检查，powershell 直接放行
func CheckPermission(command string, config PermissionConfig, isPowerShell bool) (bool, string) {
	// 第一层：绕过
	if config.Mode == PermissionModeBypass {
		return true, ""
	}

	// 第二层：拒绝模式（逐个子命令检查）
	if len(config.DenyPatterns) > 0 {
		for _, segment := range SplitPipeline(command, isPowerShell) {
			for _, pattern := range config.DenyPatterns {
				if pattern.MatchString(segment) {
					return false, "Command denied by pattern: " + pattern.String()
				}
			}
		}
	}

	// 第三层：允许模式（任一匹配 → 放行整条命令）
	if len(config.AllowPatterns) > 0 {
		for _, pattern := range config.AllowPatterns {
			if pattern.MatchString(command) {
				return true, ""
			}
		}
	}

	// 第四层：模式特定检查
	if config.Mode == PermissionModeReadOnly {
		if IsReadOnlyCommand(command, isPowerShell) {
			return true, ""
		}
		return false, "Read-only mode: only read/search/list commands are allowed"
	}

	if config.Mode == PermissionModeAcceptEdits {
		var fileOps, safeCmds map[string]bool
		if isPowerShell {
			fileOps = psFileOpCommands
			safeCmds = psKnownSafeCommands
		} else {
			fileOps = bashFileOpCommands
			safeCmds = bashKnownSafeCommands
		}
		segments := SplitPipeline(command, isPowerShell)
		for _, segment := range segments {
			base := extractBaseCommand(segment, isPowerShell)
			if fileOps[base] || safeCmds[base] {
				continue
			}
			return false, "Accept-edits mode: unknown command '" + base + "' requires explicit approval"
		}
		return true, ""
	}

	// 第五层（AUTO 模式）：bash 仅建议性检查，powershell 直接放行
	if isPowerShell {
		return true, ""
	}
	// bash AUTO：管道子命令验证（建议性，始终放行）
	return true, ""
}

// IsReadOnlyCommand 判断命令是否为只读命令
// 对齐 Python: is_read_only (bash/_semantics.py L118-130 / powershell/_semantics.py L158-170)
func IsReadOnlyCommand(command string, isPowerShell bool) bool {
	return isReadOnly(command, isPowerShell)
}

// SplitPipeline 拆分管道命令
// 对齐 Python: _split_pipeline (bash/_semantics.py L76-79 / powershell/_semantics.py L76-124)
func SplitPipeline(command string, isPowerShell bool) []string {
	if isPowerShell {
		return splitPSPipeline(command)
	}
	return splitBashPipeline(command)
}

// CompilePatterns 将字符串列表编译为正则表达式列表
// 对齐 Python: PermissionConfig.compile_patterns (bash/_permission.py L86-91)
func CompilePatterns(raw []string) []*regexp.Regexp {
	if len(raw) == 0 {
		return nil
	}
	patterns := make([]*regexp.Regexp, 0, len(raw))
	for _, p := range raw {
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			continue
		}
		patterns = append(patterns, re)
	}
	return patterns
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitBashPipeline 拆分 bash 管道命令
// 对齐 Python: _split_pipeline (bash/_semantics.py L76-79)
func splitBashPipeline(command string) []string {
	operatorRe := regexp.MustCompile(`\s*(?:\|\||&&|[;|])\s*`)
	parts := operatorRe.Split(command, -1)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// splitPSPipeline 拆分 PowerShell 管道命令
// 对齐 Python: _split_pipeline (powershell/_semantics.py L76-124)
func splitPSPipeline(command string) []string {
	return splitPSPipelineImpl(command)
}

// psOperatorLengthAt 返回指定位置的操作符长度
// 对齐 Python: _operator_length_at (powershell/_semantics.py L127-135)
func psOperatorLengthAt(command string, index int) int {
	char := command[index]
	nextChar := byte(0)
	if index+1 < len(command) {
		nextChar = command[index+1]
	}
	if (char == '|' || char == '&') && nextChar == char {
		return 2
	}
	if char == '|' || char == ';' {
		return 1
	}
	return 0
}

// splitPSPipelineImpl 用 index 遍历实现 PowerShell 管道拆分
func splitPSPipelineImpl(command string) []string {
	var parts []string
	start := 0
	depths := map[byte]int{'{': 0, '(': 0, '[': 0}
	quote := byte(0)
	escaped := false
	index := 0

	for index < len(command) {
		char := command[index]
		if escaped {
			escaped = false
		} else if char == '`' {
			escaped = true
		} else if quote != 0 {
			if char == quote {
				quote = 0
			}
		} else if char == '\'' || char == '"' {
			quote = char
		} else if _, ok := depths[char]; ok {
			depths[char]++
		} else if char == '}' {
			depths['{'] = max(0, depths['{']-1)
		} else if char == ')' {
			depths['('] = max(0, depths['(']-1)
		} else if char == ']' {
			depths['['] = max(0, depths['[']-1)
		} else if depths['{'] == 0 && depths['('] == 0 && depths['['] == 0 {
			opLen := psOperatorLengthAt(command, index)
			if opLen > 0 {
				part := strings.TrimSpace(command[start:index])
				if part != "" {
					parts = append(parts, part)
				}
				index += opLen
				start = index
				continue
			}
		}
		index++
	}

	tail := strings.TrimSpace(command[start:])
	if tail != "" {
		parts = append(parts, tail)
	}
	return parts
}

// extractBaseCommand 提取命令段的基础命令名
// 对齐 Python: _extract_base_command (bash/_semantics.py L82-97 / powershell/_semantics.py L138-152)
func extractBaseCommand(segment string, isPowerShell bool) string {
	if isPowerShell {
		return extractPSBaseCommand(segment)
	}
	return extractBashBaseCommand(segment)
}

// extractBashBaseCommand 提取 bash 命令段的基础命令名
// 对齐 Python: _extract_base_command (bash/_semantics.py L82-97)
func extractBashBaseCommand(segment string) string {
	tokens := strings.Fields(segment)
	for _, token := range tokens {
		// 跳过变量赋值 (FOO=bar)
		if strings.Contains(token, "=") && !strings.HasPrefix(token, "-") {
			continue
		}
		// 去除引号
		base := strings.Trim(token, "\"'")
		// 取路径最后一段: /usr/bin/grep -> grep
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		if idx := strings.LastIndex(base, "\\"); idx >= 0 {
			base = base[idx+1:]
		}
		base = strings.ToLower(base)
		if strings.HasSuffix(base, ".exe") {
			base = base[:len(base)-4]
		}
		return base
	}
	return ""
}

// extractPSBaseCommand 提取 PowerShell 命令段的基础命令名
// 对齐 Python: _extract_base_command (powershell/_semantics.py L138-152)
func extractPSBaseCommand(segment string) string {
	tokens := strings.Fields(segment)
	for _, token := range tokens {
		// 跳过调用运算符 & 和点源 .
		if token == "&" || token == "." {
			continue
		}
		// 跳过变量赋值 ($x=...)
		if strings.HasPrefix(token, "$") && strings.Contains(token, "=") {
			continue
		}
		// 跳过参数
		if strings.HasPrefix(token, "-") {
			continue
		}
		base := token
		if idx := strings.LastIndex(base, "\\"); idx >= 0 {
			base = base[idx+1:]
		}
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		if strings.HasSuffix(strings.ToLower(base), ".exe") {
			base = base[:len(base)-4]
		}
		return strings.ToLower(base)
	}
	return ""
}
