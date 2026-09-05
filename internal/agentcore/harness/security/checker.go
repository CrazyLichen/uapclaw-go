package security

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/anmitsu/go-shlex"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalDirectoryChecker 检查命令是否访问 workspace 外路径，若越界则触发 external_directory 权限。
//
// 对齐 Python: ExternalDirectoryChecker (checker.py L98-101)
type ExternalDirectoryChecker struct {
	// config 权限配置
	config map[string]any
	// workspaceRoot 工作空间根目录
	workspaceRoot string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// shellOperatorsRE Shell 操作符正则：检测命令链/注入元字符。
// 如果命令匹配 allow 模式但同时包含这些操作符，权限从 ALLOW → ASK 作为安全网。
//
// 对齐 Python: _SHELL_OPERATORS_RE (checker.py L36-40)
var shellOperatorsRE = regexp.MustCompile(
	`[;&|` + "`" + `<>]` + // ; & | ` < >（覆盖 &&、||、管道、重定向、反引号）
		`|\$[({]` + // $( 或 ${ — 命令/变量替换
		`|\r?\n`, // 换行注入
)

// commandExecTools 命令执行工具集合
// 对齐 Python: _COMMAND_EXEC_TOOLS (checker.py L41)
var commandExecTools = map[string]bool{
	"mcp_exec_command": true,
}

// pathAwareCommands 会操作路径的命令（需做外部目录检测）
// 对齐 Python: _PATH_AWARE_COMMANDS (checker.py L44-48)
var pathAwareCommands = map[string]bool{
	"cd": true, "rm": true, "cp": true, "mv": true, "mkdir": true, "touch": true,
	"chmod": true, "chown": true, "cat": true, "ls": true, "dir": true,
	"type": true, "del": true, "rd": true, "copy": true, "move": true,
	"md": true, "head": true, "tail": true, "more": true, "less": true,
	"vim": true, "nano": true, "gedit": true, "notepad": true,
}

var checkerLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExternalDirectoryChecker 创建外部目录检查器。
//
// 对齐 Python: ExternalDirectoryChecker.__init__(config, workspace_root) (checker.py L101-103)
func NewExternalDirectoryChecker(config map[string]any, workspaceRoot string) *ExternalDirectoryChecker {
	return &ExternalDirectoryChecker{
		config:        config,
		workspaceRoot: workspaceRoot,
	}
}

// CheckExternalPaths 若访问了 workspace 外路径，根据 external_directory 配置返回 DENY/ASK；否则返回 nil。
//
// 对齐 Python: ExternalDirectoryChecker.check_external_paths(tool_name, tool_args) (checker.py L105-197)
func (c *ExternalDirectoryChecker) CheckExternalPaths(toolName string, toolArgs map[string]any) *PermissionResult {
	workspace := c.workspaceRoot
	if workspace == "" {
		logger.Debug(checkerLogComponent).Msg("permission.external.workspace: missing; skip external_directory check")
		return nil
	}
	logger.Debug(checkerLogComponent).
		Str("workspace", workspace).
		Msg("permission.external.workspace: source=config")

	var externalPaths []string

	if shellApprovalTools[toolName] {
		// Shell 工具：提取命令中的路径
		workdir, _ := toolArgs["workdir"].(string)
		workdirResolved := filepath.Join(workspace, workdir)
		workdirResolved = filepath.Clean(workdirResolved)
		cmd := commandText(toolArgs)

		logger.Debug(checkerLogComponent).
			Str("tool", toolName).
			Str("cmd", cmd).
			Str("workdir", workdirResolved).
			Msg("permission.external.shell_input")

		paths := extractPathsFromCommand(cmd, workdirResolved)
		for _, p := range paths {
			if !ContainsPath(workspace, p) {
				externalPaths = append(externalPaths, filepath.ToSlash(p))
			}
		}
	} else if pathApprovalTools[toolName] {
		// 路径工具：直接从参数提取路径
		for _, s := range iterPathStrings(toolName, toolArgs) {
			raw := strings.TrimSpace(s)
			raw = strings.Trim(raw, `"'`)
			if raw == "" {
				continue
			}
			p := raw
			if !filepath.IsAbs(p) {
				p = filepath.Join(workspace, p)
			}
			p = filepath.Clean(p)
			if !ContainsPath(workspace, p) {
				externalPaths = append(externalPaths, filepath.ToSlash(p))
			}
		}
		logger.Debug(checkerLogComponent).
			Str("tool", toolName).
			Msg("permission.external.path_input")
	} else {
		return nil
	}

	if len(externalPaths) == 0 {
		return nil
	}

	// 检查 external_directory 配置
	extCfg, ok := c.config["external_directory"].(map[string]any)
	if !ok {
		if action, ok := c.config["external_directory"].(string); ok {
			extCfg = map[string]any{"*": action}
		} else {
			extCfg = map[string]any{"*": "ask"}
		}
	}

	action, _ := extCfg["*"].(string)
	if action == "" {
		action = "ask"
	}

	// 检查所有外部路径是否在 allow 规则下
	allAllowed := true
	for _, pathStr := range externalPaths {
		pathCovered := false
		for cfgPathStr, cfgActionVal := range extCfg {
			cfgActionStr, _ := cfgActionVal.(string)
			if cfgPathStr == "*" || cfgActionStr != "allow" {
				continue
			}
			cfgPathNorm := strings.ReplaceAll(cfgPathStr, `\`, "/")
			cfgPathNorm = strings.TrimRight(cfgPathNorm, "/")
			// 跳过过短前缀（如 "C:" 会匹配 C 盘下任意路径）
			if !strings.Contains(cfgPathNorm, "/") {
				continue
			}
			if ContainsPath(cfgPathNorm, pathStr) {
				pathCovered = true
				break
			}
		}
		if !pathCovered {
			allAllowed = false
			break
		}
	}
	if allAllowed {
		action = "allow"
	}

	switch strings.ToLower(action) {
	case "deny":
		return &PermissionResult{
			Permission:    PermissionLevelDeny,
			Reason:        fmt.Sprintf("Access to paths outside workspace is denied: %s", externalPaths[0]),
			MatchedRule:   "external_directory.*",
			ExternalPaths: externalPaths,
		}
	case "ask":
		return &PermissionResult{
			Permission:    PermissionLevelAsk,
			Reason:        fmt.Sprintf("Access to paths outside workspace requires approval: %s", externalPaths[0]),
			MatchedRule:   "external_directory.*",
			ExternalPaths: externalPaths,
		}
	default:
		// "allow" 或其他 → 不拦截
		return nil
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractPathsFromCommand 从命令字符串中提取可能为路径的参数，并解析为绝对路径。
//
// 对齐 Python: _extract_paths_from_command(command, workdir) (checker.py L51-84)
func extractPathsFromCommand(command, workdir string) []string {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	tokens, _ := shlex.Split(command, true)
	if len(tokens) == 0 {
		return nil
	}

	cmd := strings.ToLower(tokens[0])
	logger.Debug(checkerLogComponent).
		Str("tool_command", command).
		Str("cmd", cmd).
		Bool("path_aware", pathAwareCommands[cmd]).
		Msg("permission.external.parse")

	if !pathAwareCommands[cmd] {
		return nil
	}

	base := workdir
	if base == "" {
		base, _ = os.Getwd()
	}
	base = filepath.Clean(base)
	logger.Debug(checkerLogComponent).Str("base", base).Msg("permission.external.parse_base")

	var paths []string
	for _, tok := range tokens[1:] {
		tok = strings.TrimSpace(tok)
		tok = strings.Trim(tok, `"'`)
		if tok == "" || strings.HasPrefix(tok, "-") {
			continue
		}
		if !looksLikePath(tok) {
			continue
		}
		p := tok
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		p = filepath.Clean(p)
		paths = append(paths, p)
	}
	logger.Debug(checkerLogComponent).
		Strs("extracted_paths", paths).
		Msg("permission.external.parse_paths")
	return paths
}

// looksLikePath 启发式路径检测。
//
// 对齐 Python: _looks_like_path(token) (checker.py L87-92)
func looksLikePath(token string) bool {
	if strings.HasPrefix(token, `\\`) || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") {
		return true
	}
	// Windows 盘符 C:\
	if len(token) >= 3 && token[1] == ':' && (token[2] == '\\' || token[2] == '/') {
		return true
	}
	return strings.Contains(token, `\`) || strings.Contains(token, "/")
}

// iterPathStrings 遍历工具参数中的路径字符串。
//
// 对齐 Python: _iter_path_strings(tool_name, tool_args) (tiered_policy.py L232-239)
func iterPathStrings(toolName string, toolArgs map[string]any) []string {
	var out []string
	for k, v := range toolArgs {
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		if toolArgValueLooksLikePath(k, s) {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// toolArgValueLooksLikePath 判断参数值是否纳入路径类 pattern 匹配。
//
// 对齐 Python: _tool_arg_value_looks_like_path(arg_key, value) (tiered_policy.py L223-229)
func toolArgValueLooksLikePath(argKey, value string) bool {
	if pathArgKeys[argKey] {
		return true
	}
	if strings.Contains(value, "/") || strings.Contains(value, `\`) {
		return true
	}
	if len(value) > 1 && value[1] == ':' {
		return true
	}
	return false
}
