package security

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionSuggestion 权限建议，用于「始终允许」规则持久化。
//
// 对齐 Python: PermissionSuggestion (suggestions.py L34-41)
type PermissionSuggestion struct {
	// Tools 适用的工具列表
	Tools []string
	// MatchType 匹配类型（如 path、command）
	MatchType string
	// Pattern 匹配模式
	Pattern string
	// Action 执行动作（默认 allow）
	Action string
	// Scope 范围（exact 或 prefix）
	Scope string
	// Reason 原因
	Reason string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// shellSuggestionTools Shell 建议工具集合
// 对齐 Python: _SHELL_SUGGESTION_TOOLS (suggestions.py L20)
var shellSuggestionTools = map[string]bool{
	"bash": true, "mcp_exec_command": true, "create_terminal": true,
}

// pathSuggestionTools 路径建议工具集合
// 对齐 Python: _PATH_SUGGESTION_TOOLS (suggestions.py L21-27)
var pathSuggestionTools = map[string]bool{
	"read_file": true, "write_file": true, "edit_file": true,
	"read_text_file": true, "write_text_file": true,
	"write": true, "read": true,
	"glob_file_search": true, "glob": true, "list_dir": true, "list_files": true,
	"grep": true, "search_replace": true,
}

// pathSuggestionKeys 路径建议参数键
// 对齐 Python: _PATH_SUGGESTION_KEYS (suggestions.py L28-31)
var pathSuggestionKeys = []string{
	"path", "file_path", "target_file", "file", "old_path", "new_path",
	"source_path", "dest_path", "directory", "dir",
}

var suggestionsLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildPermissionSuggestions 构建权限建议列表。
//
// 对齐 Python: build_permission_suggestions(tool_name, tool_args, shell_ast_result) (suggestions.py L44-61)
func BuildPermissionSuggestions(toolName string, toolArgs map[string]any, shellAstResult *ShellAstParseResult) []PermissionSuggestion {
	if shellSuggestionTools[toolName] {
		cmd := commandText(toolArgs)
		if cmd == "" {
			return nil
		}
		return BuildShellPermissionSuggestions(toolName, cmd, shellAstResult)
	}
	if pathSuggestionTools[toolName] {
		suggestion := buildPathPermissionSuggestion(toolName, toolArgs)
		if suggestion != nil {
			return []PermissionSuggestion{*suggestion}
		}
		return nil
	}
	return nil
}

// BuildShellPermissionSuggestions 构建 Shell 权限建议列表。
//
// 对齐 Python: build_shell_permission_suggestions(tool_name, command, shell_ast_result) (suggestions.py L64-102)
func BuildShellPermissionSuggestions(toolName string, command string, shellAstResult *ShellAstParseResult) []PermissionSuggestion {
	if shellAstResult == nil {
		shellAstResult = ParseShellForPermission(command)
	}
	flags := &shellAstResult.Flags

	// too_complex → 无建议
	if shellAstResult.Kind == ShellAstKindTooComplex {
		return nil
	}

	// parse_unavailable + risky → 无建议
	if shellAstResult.Kind == ShellAstKindParseUnavailable && flags.HasRiskyStructure() {
		return nil
	}

	// 风险结构标志 → 无建议
	if flags.InputRedirection || flags.OutputRedirection ||
		flags.CommandSubstitution || flags.ProcessSubstitution ||
		flags.Heredoc || flags.Subshell || flags.CommandGroup ||
		flags.ParameterExpansion {
		return nil
	}

	// simple + 多子命令 → 每个子命令生成建议
	if shellAstResult.Kind == ShellAstKindSimple && len(shellAstResult.Subcommands) > 1 {
		var suggestions []PermissionSuggestion
		for _, subcmd := range shellAstResult.Subcommands {
			suggestion := buildSingleShellSuggestion(toolName, subcmd.Text)
			if suggestion != nil {
				suggestions = append(suggestions, *suggestion)
			}
		}
		return dedupeSuggestions(suggestions)
	}

	// simple + 单子命令
	if shellAstResult.Kind == ShellAstKindSimple && len(shellAstResult.Subcommands) == 1 {
		suggestion := buildSingleShellSuggestion(toolName, shellAstResult.Subcommands[0].Text)
		if suggestion != nil {
			return []PermissionSuggestion{*suggestion}
		}
		return nil
	}

	// fallback: 直接用命令文本
	suggestion := buildSingleShellSuggestion(toolName, command)
	if suggestion != nil {
		return []PermissionSuggestion{*suggestion}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSingleShellSuggestion 构建单条 Shell 命令建议。
//
// 对齐 Python: _build_single_shell_suggestion(tool_name, command) (suggestions.py L105-143)
func buildSingleShellSuggestion(toolName, command string) *PermissionSuggestion {
	text := strings.TrimSpace(command)
	if text == "" {
		return nil
	}

	// 检查 heredoc 前缀
	heredocPrefix := extractPrefixBeforeHeredoc(text)
	if heredocPrefix != "" {
		return &PermissionSuggestion{
			Tools:     []string{toolName},
			MatchType: "command",
			Pattern:   buildPrefixPattern(heredocPrefix),
			Scope:     "prefix",
			Action:    "allow",
			Reason:    "heredoc_prefix",
		}
	}

	// 多行命令取首行
	if strings.Contains(text, "\n") {
		firstLine := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
		if firstLine != "" {
			prefix := extractSimpleCommandPrefix(firstLine)
			if prefix != "" {
				return &PermissionSuggestion{
					Tools:     []string{toolName},
					MatchType: "command",
					Pattern:   buildPrefixPattern(prefix),
					Scope:     "prefix",
					Action:    "allow",
					Reason:    "first_line_prefix",
				}
			}
		}
		return nil
	}

	// 精确命令匹配
	return &PermissionSuggestion{
		Tools:     []string{toolName},
		MatchType: "command",
		Pattern:   text,
		Scope:     "exact",
		Action:    "allow",
		Reason:    "exact_command",
	}
}

// extractPrefixBeforeHeredoc 提取 heredoc 前缀。
//
// 对齐 Python: _extract_prefix_before_heredoc(command) (suggestions.py L146-152)
func extractPrefixBeforeHeredoc(command string) string {
	if !strings.Contains(command, "<<") {
		return ""
	}
	before := strings.TrimSpace(strings.SplitN(command, "<<", 2)[0])
	if before == "" {
		return ""
	}
	prefix := extractSimpleCommandPrefix(before)
	if prefix != "" {
		return prefix
	}
	return before
}

// extractSimpleCommandPrefix 提取简单命令前缀（取前两个 argv）。
//
// 对齐 Python: _extract_simple_command_prefix(command) (suggestions.py L155-162)
func extractSimpleCommandPrefix(command string) string {
	argv := shlexSplit(command)
	if len(argv) == 0 {
		return ""
	}
	if len(argv) >= 2 {
		return strings.TrimSpace(argv[0] + " " + argv[1])
	}
	return strings.TrimSpace(argv[0])
}

// buildPrefixPattern 构建前缀模式。
//
// 对齐 Python: _build_prefix_pattern(prefix) (suggestions.py L165-166)
func buildPrefixPattern(prefix string) string {
	return strings.TrimSpace(prefix) + " *"
}

// buildPathPermissionSuggestion 构建路径权限建议。
//
// 对齐 Python: _build_path_permission_suggestion(tool_name, tool_args) (suggestions.py L169-197)
func buildPathPermissionSuggestion(toolName string, toolArgs map[string]any) *PermissionSuggestion {
	// 已知路径参数键
	for _, key := range pathSuggestionKeys {
		if value, ok := toolArgs[key].(string); ok {
			value = strings.TrimSpace(value)
			if value != "" {
				return &PermissionSuggestion{
					Tools:     []string{toolName},
					MatchType: "path",
					Pattern:   value,
					Scope:     "exact",
					Action:    "allow",
					Reason:    "exact_path",
				}
			}
		}
	}

	// 其他键值中形似路径的
	for key, value := range toolArgs {
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if valueLooksLikePath(key, text) {
			return &PermissionSuggestion{
				Tools:     []string{toolName},
				MatchType: "path",
				Pattern:   text,
				Scope:     "exact",
				Action:    "allow",
				Reason:    "derived_exact_path",
			}
		}
	}
	return nil
}

// valueLooksLikePath 判断值是否形似路径。
//
// 对齐 Python: _value_looks_like_path(key, text) (suggestions.py L200-205)
func valueLooksLikePath(key, text string) bool {
	// 已知路径键
	for _, k := range pathSuggestionKeys {
		if k == key {
			return true
		}
	}
	// 含 / 或 \
	if strings.Contains(text, "/") || strings.Contains(text, `\`) {
		return true
	}
	// Windows 盘符 C:
	if len(text) > 1 && text[1] == ':' {
		return true
	}
	return false
}

// dedupeSuggestions 去重建议列表。
//
// 对齐 Python: _dedupe_suggestions(suggestions) (suggestions.py L208-224)
func dedupeSuggestions(suggestions []PermissionSuggestion) []PermissionSuggestion {
	type sig struct {
		tools     string
		matchType string
		pattern   string
		action    string
	}
	seen := make(map[sig]bool)
	var result []PermissionSuggestion
	for _, s := range suggestions {
		key := sig{
			tools:     strings.Join(s.Tools, ","),
			matchType: s.MatchType,
			pattern:   s.Pattern,
			action:    s.Action,
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, s)
	}
	return result
}
