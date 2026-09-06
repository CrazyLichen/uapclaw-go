//go:build test

package shell

import (
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTruncateOutput_无需截断 测试短文本无需截断
func TestTruncateOutput_无需截断(t *testing.T) {
	result := TruncateOutput("hello", 100)
	if result != "hello" {
		t.Errorf("短文本不应截断，got %q", result)
	}
}

// TestTruncateOutput_零上限 测试零上限
func TestTruncateOutput_零上限(t *testing.T) {
	result := TruncateOutput("hello", 0)
	if result != "hello" {
		t.Errorf("零上限不应截断，got %q", result)
	}
}

// TestTruncateOutput_需要截断 测试需要截断
func TestTruncateOutput_需要截断(t *testing.T) {
	text := strings.Repeat("a\n", 100) // 200 字符
	result := TruncateOutput(text, 50)
	if !strings.Contains(result, "lines omitted") {
		t.Errorf("截断结果应包含 'lines omitted'")
	}
}

// TestRenderToolContent_正常输出 测试正常输出
func TestRenderToolContent_正常输出(t *testing.T) {
	output := CommandOutput{Stdout: "hello", Stderr: "", ExitCode: 0}
	content, isErr := RenderToolContent(output, false)
	if isErr {
		t.Errorf("正常输出不应为错误")
	}
	if content != "hello" {
		t.Errorf("正常输出内容 = %q, want %q", content, "hello")
	}
}

// TestRenderToolContent_错误输出 测试错误输出
func TestRenderToolContent_错误输出(t *testing.T) {
	output := CommandOutput{Stdout: "out", Stderr: "err", ExitCode: 1}
	content, isErr := RenderToolContent(output, true)
	if !isErr {
		t.Errorf("错误输出应为错误")
	}
	if !strings.Contains(content, "Exit code 1") {
		t.Errorf("错误输出应包含退出码")
	}
	if !strings.Contains(content, "err") {
		t.Errorf("错误输出应包含 stderr")
	}
}

// TestRenderToolContent_带警告 测试带警告输出
func TestRenderToolContent_带警告(t *testing.T) {
	output := CommandOutput{Stdout: "hello", Warning: "DESTRUCTIVE", ExitCode: 0}
	content, _ := RenderToolContent(output, false)
	if !strings.Contains(content, "DESTRUCTIVE") {
		t.Errorf("输出应包含警告")
	}
}

// TestRenderToolContent_合并stdoutstderr 测试合并 stdout 和 stderr
func TestRenderToolContent_合并stdoutstderr(t *testing.T) {
	output := CommandOutput{Stdout: "out", Stderr: "err", ExitCode: 0}
	content, _ := RenderToolContent(output, false)
	if !strings.Contains(content, "out") || !strings.Contains(content, "err") {
		t.Errorf("正常输出应包含 stdout 和 stderr")
	}
}

// TestRenderPartialOnFailure_有输出 测试有部分输出
func TestRenderPartialOnFailure_有输出(t *testing.T) {
	output := CommandOutput{Stdout: "partial", Stderr: "", ExitCode: 1}
	result := RenderPartialOnFailure(output, "timeout")
	if !strings.Contains(result, "timeout") {
		t.Errorf("应包含失败原因")
	}
}

// TestRenderPartialOnFailure_无输出 测试无输出
func TestRenderPartialOnFailure_无输出(t *testing.T) {
	output := CommandOutput{Stdout: "", Stderr: "", ExitCode: 1}
	result := RenderPartialOnFailure(output, "timeout")
	if result != "" {
		t.Errorf("无输出时应返回空字符串，got %q", result)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestTruncateOutputWithRatio_无需截断 测试无需截断
func TestTruncateOutputWithRatio_无需截断(t *testing.T) {
	result := truncateOutputWithRatio("short", 100, 0.8)
	if result != "short" {
		t.Errorf("短文本不应截断")
	}
}

// TestTruncateOutputWithRatio_零上限 测试零上限
func TestTruncateOutputWithRatio_零上限(t *testing.T) {
	result := truncateOutputWithRatio("hello", 0, 0.8)
	if result != "hello" {
		t.Errorf("零上限不应截断")
	}
}

// TestFormatFileSize 测试文件大小格式化
func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{500, "500 bytes"},
		{1024, "1KB"},
		{1536, "1.5KB"},
		{1048576, "1MB"},
		{1572864, "1.5MB"},
		{1073741824, "1GB"},
	}
	for _, tt := range tests {
		got := formatFileSize(tt.in)
		if got != tt.want {
			t.Errorf("formatFileSize(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestGeneratePreview_短内容 测试短内容不截断
func TestGeneratePreview_短内容(t *testing.T) {
	content, hasMore := generatePreview("hello", 100)
	if content != "hello" || hasMore {
		t.Errorf("短内容不应截断")
	}
}

// TestGeneratePreview_长内容 测试长内容截断
func TestGeneratePreview_长内容(t *testing.T) {
	text := strings.Repeat("a\n", 500) // 1000 字符
	content, hasMore := generatePreview(text, 100)
	if !hasMore {
		t.Errorf("长内容应有更多内容")
	}
	if len(content) > 200 {
		t.Errorf("预览内容过长: %d", len(content))
	}
}

// TestBuildPersistedMessage 测试构建持久化消息
func TestBuildPersistedMessage(t *testing.T) {
	msg := buildPersistedMessage("/tmp/test.txt", 1024, "preview content", true)
	if !strings.Contains(msg, persistedOutputTag) {
		t.Errorf("应包含持久化标签开始")
	}
	if !strings.Contains(msg, persistedOutputClosingTag) {
		t.Errorf("应包含持久化标签结束")
	}
	if !strings.Contains(msg, "/tmp/test.txt") {
		t.Errorf("应包含文件路径")
	}
	if !strings.Contains(msg, "...") {
		t.Errorf("hasMore=true 时应包含省略号")
	}
}

// TestPrependWarning 测试前缀警告
func TestPrependWarning(t *testing.T) {
	if prependWarning("content", "") != "content" {
		t.Errorf("空警告应返回原内容")
	}
	if prependWarning("", "warn") != "warn" {
		t.Errorf("空内容应返回警告")
	}
	result := prependWarning("content", "warn")
	if result != "warn\ncontent" {
		t.Errorf("有警告和内容应合并，got %q", result)
	}
}

// TestMerge 测试合并输出流
func TestMerge(t *testing.T) {
	if merge("", "") != "" {
		t.Errorf("两个空应返回空")
	}
	if merge("a", "") != "a" {
		t.Errorf("第二个为空应返回第一个")
	}
	if merge("", "b") != "b" {
		t.Errorf("第一个为空应返回第二个")
	}
	if merge("a", "b") != "a\nb" {
		t.Errorf("两个非空应合并")
	}
}

// TestFilterEmpty 测试过滤空字符串
func TestFilterEmpty(t *testing.T) {
	result := filterEmpty([]string{"a", "", "b", "", "c"})
	if len(result) != 3 {
		t.Errorf("应过滤空字符串，got %v", result)
	}
	result = filterEmpty([]string{"", ""})
	if len(result) != 0 {
		t.Errorf("全空应返回空切片")
	}
	result = filterEmpty(nil)
	if len(result) != 0 {
		t.Errorf("nil 应返回空切片")
	}
}
