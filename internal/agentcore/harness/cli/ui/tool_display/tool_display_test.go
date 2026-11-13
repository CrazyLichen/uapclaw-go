package tool_display

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// GetDisplayName 映射与回退。
func TestGetDisplayName_映射与回退(t *testing.T) {
	assert.Equal(t, "Read", GetDisplayName("read_file"))
	assert.Equal(t, "Write", GetDisplayName("write_file"))
	assert.Equal(t, "Bash", GetDisplayName("bash"))
	assert.Equal(t, "TodoWrite", GetDisplayName("todo_create"))
	assert.Equal(t, "WebSearch", GetDisplayName("web_free_search"))
	assert.Equal(t, "VisionQA", GetDisplayName("visual_question_answering"))
	// 未登记：下划线转空格标题化
	assert.Equal(t, "Custom Tool", GetDisplayName("custom_tool"))
}

// TestFormatToolArgs_各工具 对齐 format_tool_args 的分支。
func TestFormatToolArgs_各工具(t *testing.T) {
	// shortPath 会把 CWD 前缀转相对路径——用真实 CWD 构造
	cwd, _ := os.Getwd()
	inside := filepath.Join(cwd, "src", "main.py")

	args := map[string]any{
		"file_path": inside,
		"limit":     10,
		"command":   "git status",
		"pattern":   "TODO",
		"path":      "src/",
		"query":     "天气",
		"url":       "https://x",
	}
	// read_file 带 limit
	assert.Equal(t, "src/main.py, limit=10", FormatToolArgs("read_file", args))
	// read_file 不带 limit
	assert.Equal(t, "src/main.py", FormatToolArgs("read_file",
		map[string]any{"file_path": inside}))
	// write_file / edit_file
	assert.Equal(t, "src/main.py", FormatToolArgs("write_file", args))
	assert.Equal(t, "src/main.py", FormatToolArgs("edit_file", args))
	// bash 短命令
	assert.Equal(t, "git status", FormatToolArgs("bash", args))
	// bash 长命令截断
	long := strings.Repeat("x", 70)
	assert.Equal(t, long[:57]+"...", FormatToolArgs("bash", map[string]any{"command": long}))
	// grep
	assert.Equal(t, `"TODO" src/`, FormatToolArgs("grep", args))
	// glob
	assert.Equal(t, "**/*.go", FormatToolArgs("glob", map[string]any{"pattern": "**/*.go"}))
	// ls 默认 .
	assert.Equal(t, "src/", FormatToolArgs("ls", args))
	assert.Equal(t, ".", FormatToolArgs("ls", map[string]any{}))
	// todo 工具省略参数
	assert.Equal(t, "", FormatToolArgs("todo_create", args))
	// web
	assert.Equal(t, "天气", FormatToolArgs("web_search", args))
	assert.Equal(t, "https://x", FormatToolArgs("web_fetch", args))
	// 未知工具回退第一个参数
	assert.Equal(t, "42", FormatToolArgs("custom", map[string]any{"k": 42}))
}

// TestFormatToolArgs_字符串args JSON 字符串形态解析。
func TestFormatToolArgs_字符串args(t *testing.T) {
	cwd, _ := os.Getwd()
	inside := filepath.Join(cwd, "src", "main.py")
	assert.Equal(t, "src/main.py",
		FormatToolArgs("write_file", `{"file_path": "`+inside+`"}`))
	assert.Equal(t, "", FormatToolArgs("write_file", "not-json"))
	assert.Equal(t, "", FormatToolArgs("write_file", nil))
	assert.Equal(t, "", FormatToolArgs("write_file", "[1,2]"))
}

// TestFormatToolResult_各工具 对齐 format_tool_result 分支。
func TestFormatToolResult_各工具(t *testing.T) {
	// 空 → Done
	assert.Equal(t, "Done", FormatToolResult("bash", "", nil, nil))
	// read_file 优先结构化行数
	assert.Equal(t, "Read 42 lines",
		FormatToolResult("read_file", "a\nb", nil, map[string]any{"line_count": 42}))
	// read_file 回退文本行数
	assert.Equal(t, "Read 2 lines", FormatToolResult("read_file", "a\nb", nil, nil))
	// write_file 多行
	assert.Contains(t, FormatToolResult("write_file", "l1\nl2\nl3",
		map[string]any{"file_path": "/p/x.py"}, nil), "Wrote 2 lines to")
	// write_file 单行
	assert.Contains(t, FormatToolResult("write_file", "single",
		map[string]any{"file_path": "/p/x.py"}, nil), "Wrote to")
	// edit_file 首行
	assert.Equal(t, "已编辑", FormatToolResult("edit_file", "已编辑\n更多", nil, nil))
	// edit_file 超长首行
	assert.Equal(t, "Edited file", FormatToolResult("edit_file", strings.Repeat("x", 100), nil, nil))
	// bash 单行短
	assert.Equal(t, "ok", FormatToolResult("bash", "ok", nil, nil))
	// bash 多行
	assert.Equal(t, "l1... (+2 lines)", FormatToolResult("bash", "l1\nl2\nl3", nil, nil))
	// grep
	assert.Equal(t, "Found 3 matches", FormatToolResult("grep", "a\nb\nc", nil, nil))
	assert.Equal(t, "No matches found", FormatToolResult("grep", "\n\n", nil, nil))
	// glob
	assert.Equal(t, "Found 2 files", FormatToolResult("glob", "a\nb", nil, nil))
	assert.Equal(t, "Done", FormatToolResult("glob", "", nil, nil))
	// ls
	assert.Equal(t, "Listed 2 items", FormatToolResult("ls", "a\nb", nil, nil))
	// todo 工具 → 空
	assert.Equal(t, "", FormatToolResult("todo_list", "内容", nil, nil))
	// 默认：首行截断
	long := strings.Repeat("长", 100)
	assert.Equal(t, strings.Repeat("长", 77)+"...",
		FormatToolResult("custom", long+"\n第二行", nil, nil))
	assert.Equal(t, "第一行", FormatToolResult("custom", "第一行\n第二行", nil, nil))
}

// TestFormatWritePreview 前 5 行 + 截断提示。
func TestFormatWritePreview(t *testing.T) {
	got := FormatWritePreview("l1\nl2\nl3\nl4\nl5\nl6\nl7")
	assert.Contains(t, got, "     1 l1")
	assert.Contains(t, got, "     5 l5")
	assert.Contains(t, got, "     … +2 lines")
	// 不超过 5 行时不加省略
	assert.NotContains(t, FormatWritePreview("l1\nl2"), "lines")
	// 单行超 80 截断
	long := strings.Repeat("y", 100)
	assert.Contains(t, FormatWritePreview(long), strings.Repeat("y", 77)+"...")
}

// TestExtractToolResultLineCount_lineCount坏值 非法 line_count 回退文本。
func TestExtractToolResultLineCount_坏值(t *testing.T) {
	assert.Equal(t, 2, extractToolResultLineCount("a\nb", map[string]any{"line_count": "不是数字"}))
}
