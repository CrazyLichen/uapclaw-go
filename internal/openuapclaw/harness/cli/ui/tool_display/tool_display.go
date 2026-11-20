package tool_display

import (
	"encoding/json"
	"fmt"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// 工具展示格式化——名字映射、参数与结果摘要。
//
// 提供 Claude Code 风格的工具展示：
//
//	● Read(src/main.py)      — 工具调用头
//	⎿  Read 42 lines         — 结果摘要
//
// Python: openjiuwen/harness/cli/ui/tool_display.py（292 行）

// ──────────────────────────── 常量 ────────────────────────────

// toolDisplayNames 内部 SDK 工具名 → 友好展示名。
// Python: tool_display.py:17-38 _TOOL_DISPLAY_NAMES
var toolDisplayNames = map[string]string{
	"read_file":                 "Read",
	"write_file":                "Write",
	"edit_file":                 "Edit",
	"bash":                      "Bash",
	"grep":                      "Grep",
	"glob":                      "Glob",
	"ls":                        "LS",
	"list_dir":                  "LS",
	"todo_create":               "TodoWrite",
	"todo_modify":               "TodoWrite",
	"todo_list":                 "TodoList",
	"web_search":                "WebSearch",
	"web_free_search":           "WebSearch",
	"web_fetch":                 "WebFetch",
	"web_fetch_webpage":         "WebFetch",
	"image_ocr":                 "ImageOCR",
	"visual_question_answering": "VisionQA",
	"audio_transcription":       "AudioTranscribe",
	"audio_question_answering":  "AudioQA",
	"audio_metadata":            "AudioMetadata",
}

// todoTools 结果按 todo 复选框渲染的工具集合。
// Python: tool_display.py:41 TODO_TOOLS
var todoTools = map[string]bool{
	"todo_create": true,
	"todo_modify": true,
	"todo_list":   true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TodoTools 返回 todo 工具集合（todo_render 使用）。
func TodoTools() map[string]bool {
	return todoTools
}

// caser 标题化工具（strings.Title 弃用替代，Unicode 语义安全）。
var caser = cases.Title(language.Und, cases.NoLower)

// GetDisplayName 内部工具名映射为友好展示名。
// Python: tool_display.py:45-56 get_display_name
//
// 未登记的名字回退为下划线转空格后标题化。
func GetDisplayName(toolName string) string {
	if display, exists := toolDisplayNames[toolName]; exists {
		return display
	}
	// 未登记名回退：下划线转空格后首字母大写（strings.Title 已弃用，改 cases.Title）
	return strings.TrimSpace(caser.String(strings.ReplaceAll(toolName, "_", " ")))
}

// FormatToolArgs 格式化工具参数供展示。
// Python: tool_display.py:74-146 format_tool_args
func FormatToolArgs(toolName string, toolArgs any) string {
	args := parseArgs(toolArgs)

	switch {
	case toolName == "read_file":
		path := stringArg(args, "file_path")
		if path == "" {
			return ""
		}
		path = shortPath(path)
		if limit, exists := args["limit"]; exists && limit != nil {
			return fmt.Sprintf("%s, limit=%v", path, limit)
		}
		return path
	case toolName == "write_file", toolName == "edit_file":
		path := stringArg(args, "file_path")
		if path == "" {
			return ""
		}
		return shortPath(path)
	case toolName == "bash":
		cmd := stringArg(args, "command")
		if runeLen(cmd) > 60 {
			return truncateRunes(cmd, 57) + "..."
		}
		return cmd
	case toolName == "grep":
		pattern := stringArg(args, "pattern")
		path := stringArg(args, "path")
		// Python: f'"{pattern}" {path}'.strip()
		return strings.TrimSpace(fmt.Sprintf("%q %s", pattern, path))
	case toolName == "glob":
		return stringArg(args, "pattern")
	case toolName == "ls" || toolName == "list_dir":
		return orDefault(stringArg(args, "path"), ".")
	case todoTools[toolName]:
		return "" // 任务列表工具在展示里省略参数
	case toolName == "web_search" || toolName == "web_free_search":
		return stringArg(args, "query")
	case toolName == "web_fetch" || toolName == "web_fetch_webpage":
		return stringArg(args, "url")
	default:
		// 回退：展示第一个参数值
		if first, hasFirst := firstValue(args); hasFirst {
			text := fmt.Sprint(first)
			if runeLen(text) > 60 {
				return truncateRunes(text, 57) + "..."
			}
			return text
		}
		return ""
	}
}

// FormatToolResult 把工具结果格式化成简洁摘要。
// Python: tool_display.py:148-232 format_tool_result
func FormatToolResult(toolName, toolResult string, toolArgs any,
	toolMeta map[string]any) string {
	if toolResult == "" {
		return "Done"
	}
	switch {
	case toolName == "read_file":
		lines := extractToolResultLineCount(toolResult, toolMeta)
		return fmt.Sprintf("Read %d lines", lines)
	case toolName == "write_file":
		args := parseArgs(toolArgs)
		path := shortPath(stringArg(args, "file_path"))
		lines := strings.Count(toolResult, "\n")
		if lines > 0 {
			return fmt.Sprintf("Wrote %d lines to %s", lines, path)
		}
		return fmt.Sprintf("Wrote to %s", path)
	case toolName == "edit_file":
		// 展示结果第一行或 "Edited file"
		firstLine := strings.SplitN(toolResult, "\n", 2)[0]
		if firstLine != "" && runeLen(firstLine) <= 80 {
			return firstLine
		}
		return "Edited file"
	case toolName == "bash":
		lines := strings.Split(strings.TrimSpace(toolResult), "\n")
		if len(lines) == 1 && runeLen(lines[0]) <= 80 {
			return lines[0]
		}
		first := ""
		if lines[0] != "" {
			first = truncateRunes(lines[0], 60)
		}
		return fmt.Sprintf("%s... (+%d lines)", first, len(lines)-1)
	case toolName == "grep":
		lines := strings.Split(strings.TrimSpace(toolResult), "\n")
		count := 0
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				count++
			}
		}
		if count == 0 {
			return "No matches found"
		}
		return fmt.Sprintf("Found %d matches", count)
	case toolName == "glob":
		lines := strings.Split(strings.TrimSpace(toolResult), "\n")
		count := 0
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				count++
			}
		}
		if count == 0 {
			return "No files found"
		}
		return fmt.Sprintf("Found %d files", count)
	case toolName == "ls" || toolName == "list_dir":
		lines := strings.Split(strings.TrimSpace(toolResult), "\n")
		count := 0
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				count++
			}
		}
		return fmt.Sprintf("Listed %d items", count)
	case todoTools[toolName]:
		return "" // 由 todo_render 处理
	default:
		// 默认：第一行截断
		firstLine := strings.SplitN(toolResult, "\n", 2)[0]
		if runeLen(firstLine) > 80 {
			return truncateRunes(firstLine, 77) + "..."
		}
		return firstLine
	}
}

// FormatWritePreview 把写入/编辑结果格式化成内容预览。
// Python: tool_display.py:234-260 format_write_preview
//
// 展示前 5 行（带行号），截断时追加 `… +N lines`。
func FormatWritePreview(toolResult string) string {
	lines := strings.Split(toolResult, "\n")
	previewLines := lines
	if len(lines) > 5 {
		previewLines = lines[:5]
	}
	parts := make([]string, 0, len(previewLines)+1)
	for i, line := range previewLines {
		if runeLen(line) > 80 {
			line = truncateRunes(line, 77) + "..."
		}
		parts = append(parts, fmt.Sprintf("     %d %s", i+1, line))
	}
	if len(lines) > 5 {
		parts = append(parts, fmt.Sprintf("     … +%d lines", len(lines)-5))
	}
	return strings.Join(parts, "\n")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseArgs 确保 tool_args 是 map。
// Python: tool_display.py:58-71 _parse_args
func parseArgs(toolArgs any) map[string]any {
	switch typed := toolArgs.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return typed
	case string:
		var parsed map[string]any
		if json.Unmarshal([]byte(typed), &parsed) == nil {
			return parsed
		}
	}
	return map[string]any{}
}

// stringArg 取字符串参数（缺省或类型不符返回空串）。
func stringArg(args map[string]any, key string) string {
	if value, ok := args[key].(string); ok {
		return value
	}
	return ""
}

// orDefault 空串回退默认。
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// firstValue map 的第一个值（Go map 无序，Python 同样不可依赖顺序）。
func firstValue(args map[string]any) (any, bool) {
	for _, value := range args {
		return value, true
	}
	return nil, false
}

// countTextLines 统计渲染文本行数（不低估单行内容）。
// Python: tool_display.py:262-264 _count_text_lines
func countTextLines(text string) int {
	return len(strings.Split(strings.TrimSuffix(text, "\n"), "\n"))
}

// extractToolResultLineCount 优先用结构化行数而非重数渲染文本。
// Python: tool_display.py:266-278 _extract_tool_result_line_count
func extractToolResultLineCount(toolResult string, toolMeta map[string]any) int {
	if toolMeta != nil {
		if lineCount, exists := toolMeta["line_count"]; exists && lineCount != nil {
			if n, ok := toIntValue(lineCount); ok {
				return n
			}
		}
	}
	return countTextLines(toolResult)
}

// toIntValue 宽松转 int。
func toIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	}
	return 0, false
}

// shortPath 展示用缩短路径：前缀是 CWD 时转相对路径。
// Python: tool_display.py:280-289 _short_path
func shortPath(path string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, cwd) {
		if rel, relErr := filepath.Rel(cwd, path); relErr == nil {
			return rel
		}
	}
	return path
}

// runeLen 按 Unicode 码点计长（Python 的 len(str)）。
func runeLen(text string) int { return utf8.RuneCountInString(text) }

// truncateRunes 按码点截断。
func truncateRunes(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}
