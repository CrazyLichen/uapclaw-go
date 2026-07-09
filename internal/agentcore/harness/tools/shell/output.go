package shell

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CommandOutput 命令输出
type CommandOutput struct {
	// Stdout 标准输出
	Stdout string
	// Stderr 标准错误
	Stderr string
	// ExitCode 退出码
	ExitCode int
	// Warning 破坏性命令警告
	Warning string
	// MaxOutputChars 最大输出字符数
	MaxOutputChars int
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// persistedOutputTag 持久化输出起始标签
	persistedOutputTag = "<persisted-output>"
	// persistedOutputClosingTag 持久化输出结束标签
	persistedOutputClosingTag = "</persisted-output>"
	// previewSizeBytes 预览大小（字节）
	previewSizeBytes = 2000
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// leadingBlankLinesRe 匹配开头的空白行
	// 对齐 Python: _LEADING_BLANK_LINES (bash/_output.py L90)
	leadingBlankLinesRe = regexp.MustCompile(`^(\s*\n)+`)

	// outputDir 大输出持久化目录
	// 对齐 Python: _OUTPUT_DIR (bash/_output.py L47)
	outputDir = filepath.Join(os.TempDir(), "openjiuwen_bash_outputs")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RenderToolContent 渲染工具输出内容。
// 对齐 Python: render_tool_content (bash/_output.py L158-191)
// 错误路径: 合并 stderr+stdout, 添加 "Exit code N" 头, 前缀警告
// 正常路径: 合并 stdout+stderr, 去除首尾空白, 大输出持久化, 前缀警告
func RenderToolContent(output CommandOutput, isError bool) (string, bool) {
	if isError {
		// 错误路径: stderr 在前，以便失败详情先展示
		merged := merge(output.Stderr, output.Stdout)
		parts := []string{fmt.Sprintf("Exit code %d", output.ExitCode), merged}
		content := strings.Join(filterEmpty(parts), "\n")
		return prependWarning(content, output.Warning), true
	}

	// 正常路径: stdout 在前
	merged := merge(output.Stdout, output.Stderr)
	var processed string
	if merged != "" {
		processed = leadingBlankLinesRe.ReplaceAllString(merged, "")
		processed = strings.TrimRight(processed, " \t\n\r")
	} else {
		processed = merged
	}

	if output.MaxOutputChars > 0 && len(merged) > output.MaxOutputChars {
		path, size := PersistLargeOutput(output.Stdout, output.Stderr)
		preview, hasMore := generatePreview(processed, previewSizeBytes)
		processed = buildPersistedMessage(path, size, preview, hasMore)
	}
	return prependWarning(processed, output.Warning), false
}

// TruncateOutput 截断输出。
// 对齐 Python: truncate_output (bash/_output.py L14-42)
// 80% 预算给头部, 20% 给尾部, 中间插入 "[N lines omitted]"
func TruncateOutput(text string, maxChars int) string {
	return truncateOutputWithRatio(text, maxChars, 0.8)
}

// PersistLargeOutput 持久化大输出到临时文件。
// 对齐 Python: persist_large_output (bash/_output.py L50-77)
// 写入 /tmp/openjiuwen_bash_outputs/bash_{sha256[:12]}.txt
// 返回文件路径和总字节数
func PersistLargeOutput(stdout, stderr string) (string, int) {
	combined := stdout
	if stderr != "" {
		combined += "\n--- stderr ---\n" + stderr
	}

	contentBytes := []byte(combined)
	digest := fmt.Sprintf("%x", sha256.Sum256(contentBytes))[:12]

	// 确保目录存在
	_ = os.MkdirAll(outputDir, 0o755)

	path := filepath.Join(outputDir, "bash_"+digest+".txt")

	// 仅在文件不存在时写入
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.WriteFile(path, contentBytes, 0o644)
	}

	return path, len(contentBytes)
}

// RenderPartialOnFailure 超时等失败时渲染部分输出。
// 对齐 Python: render_partial_on_failure (bash/_output.py L194-215)
func RenderPartialOnFailure(output CommandOutput, failureReason string) string {
	if output.Stdout == "" && output.Stderr == "" {
		return ""
	}
	content, _ := RenderToolContent(output, true)
	if content != "" {
		return failureReason + "\n" + content
	}
	return failureReason
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncateOutputWithRatio 截断输出（指定头部比例）
// 对齐 Python: truncate_output (bash/_output.py L14-42)
func truncateOutputWithRatio(text string, maxChars int, headRatio float64) string {
	if maxChars == 0 || len(text) <= maxChars {
		return text
	}

	headBudget := int(float64(maxChars) * headRatio)
	tailBudget := maxChars - headBudget

	head := text[:headBudget]
	var tail string
	if tailBudget > 0 {
		tail = text[len(text)-tailBudget:]
	}

	var omitted string
	if tailBudget > 0 {
		omitted = text[headBudget : len(text)-tailBudget]
	} else {
		omitted = text[headBudget:]
	}
	omittedLines := strings.Count(omitted, "\n")

	return fmt.Sprintf("%s\n\n... [%d lines omitted] ...\n\n%s", head, omittedLines, tail)
}

// formatFileSize 渲染文件大小
// 对齐 Python: _format_file_size (bash/_output.py L93-107)
func formatFileSize(sizeInBytes float64) string {
	kb := sizeInBytes / 1024
	if kb < 1 {
		return fmt.Sprintf("%.0f bytes", sizeInBytes)
	}
	if kb < 1024 {
		text := fmt.Sprintf("%.1f", kb)
		text = strings.TrimSuffix(text, ".0")
		return text + "KB"
	}
	mb := kb / 1024
	if mb < 1024 {
		text := fmt.Sprintf("%.1f", mb)
		text = strings.TrimSuffix(text, ".0")
		return text + "MB"
	}
	gb := mb / 1024
	text := fmt.Sprintf("%.1f", gb)
	text = strings.TrimSuffix(text, ".0")
	return text + "GB"
}

// generatePreview 生成预览内容
// 对齐 Python: _generate_preview (bash/_output.py L110-117)
func generatePreview(content string, maxBytes int) (string, bool) {
	if len(content) <= maxBytes {
		return content, false
	}
	truncated := content[:maxBytes]
	lastNewline := strings.LastIndex(truncated, "\n")
	cut := lastNewline
	if cut <= maxBytes/2 {
		cut = maxBytes
	}
	return content[:cut], true
}

// buildPersistedMessage 构建持久化输出消息
// 对齐 Python: _build_persisted_message (bash/_output.py L120-128)
func buildPersistedMessage(filepath string, originalSize int, preview string, hasMore bool) string {
	var sb strings.Builder
	sb.WriteString(persistedOutputTag)
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "Output too large (%s). Full output saved to: %s", formatFileSize(float64(originalSize)), filepath)
	sb.WriteString("\n\n")
	fmt.Fprintf(&sb, "Preview (first %s):", formatFileSize(float64(previewSizeBytes)))
	sb.WriteString("\n")
	sb.WriteString(preview)
	if hasMore {
		sb.WriteString("\n...\n")
	} else {
		sb.WriteString("\n")
	}
	sb.WriteString(persistedOutputClosingTag)
	return sb.String()
}

// prependWarning 在内容前添加警告
// 对齐 Python: _prepend_warning (bash/_output.py L131-135)
func prependWarning(content, warning string) string {
	if warning == "" {
		return content
	}
	if content == "" {
		return warning
	}
	return warning + "\n" + content
}

// merge 合并两个输出流
// 对齐 Python: _merge (bash/_output.py L138-144)
func merge(first, second string) string {
	if first == "" {
		return second
	}
	if second == "" {
		return first
	}
	return first + "\n" + second
}

// filterEmpty 过滤空字符串
func filterEmpty(parts []string) []string {
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
