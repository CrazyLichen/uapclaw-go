package observability

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRedactPrompt_不脱敏时截断(t *testing.T) {
	cfg := &ObservabilityConfig{AttributeValueMaxLength: 10}
	result := RedactPrompt("hello world this is long", cfg)
	if result != "hello worl...<truncated 14 chars>" {
		t.Errorf("期望截断结果，实际 %q", result)
	}
}

func TestRedactPrompt_不脱敏时不截断(t *testing.T) {
	cfg := &ObservabilityConfig{AttributeValueMaxLength: 100}
	result := RedactPrompt("short", cfg)
	if result != "short" {
		t.Errorf("期望 short，实际 %q", result)
	}
}

func TestRedactPrompt_脱敏时返回哈希(t *testing.T) {
	cfg := &ObservabilityConfig{RedactPrompts: true, AttributeValueMaxLength: 100}
	result := RedactPrompt("secret prompt", cfg)
	if !strings.HasPrefix(result, "sha256:") {
		t.Errorf("期望 sha256: 前缀，实际 %q", result)
	}
	// 相同输入应产生相同哈希
	result2 := RedactPrompt("secret prompt", cfg)
	if result != result2 {
		t.Errorf("期望相同输入产生相同哈希，%q != %q", result, result2)
	}
}

func TestRedactPrompt_nil输入(t *testing.T) {
	cfg := &ObservabilityConfig{AttributeValueMaxLength: 100}
	result := RedactPrompt(nil, cfg)
	if result != "" {
		t.Errorf("期望空字符串，实际 %q", result)
	}
}

func TestRedactCompletion_不脱敏时截断(t *testing.T) {
	cfg := &ObservabilityConfig{AttributeValueMaxLength: 5}
	result := RedactCompletion("abcdefgh", cfg)
	if result != "abcde...<truncated 3 chars>" {
		t.Errorf("期望截断结果，实际 %q", result)
	}
}

func TestRedactCompletion_脱敏时返回哈希(t *testing.T) {
	cfg := &ObservabilityConfig{RedactCompletions: true, AttributeValueMaxLength: 100}
	result := RedactCompletion("secret completion", cfg)
	if !strings.HasPrefix(result, "sha256:") {
		t.Errorf("期望 sha256: 前缀，实际 %q", result)
	}
}

func TestTruncate_零maxLength不截断(t *testing.T) {
	result := truncate("any", 0)
	if result != "any" {
		t.Errorf("期望不截断，实际 %q", result)
	}
}

func TestHashValue_空字符串(t *testing.T) {
	result := hashValue("")
	if !strings.HasPrefix(result, "sha256:") {
		t.Errorf("期望 sha256: 前缀，实际 %q", result)
	}
}

// TestTruncate_按rune截断 验证 CJK 截断不产生非法 UTF-8，且计数为字符数
func TestTruncate_按rune截断(t *testing.T) {
	got := truncate("中文中文中文", 3)
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果不是合法 UTF-8: %q", got)
	}
	if !strings.Contains(got, "中文中") {
		t.Fatalf("截断结果 = %q, 应包含 '中文中'", got)
	}
	if !strings.Contains(got, "truncated 3 chars") {
		t.Fatalf("截断结果 = %q, 应包含 'truncated 3 chars'（6 字符截到 3，按字符计数）", got)
	}
}

// TestTruncate_未超长原样返回
func TestTruncate_未超长原样返回(t *testing.T) {
	if got := truncate("中文", 3); got != "中文" {
		t.Fatalf("truncate = %q, want 原样", got)
	}
}

// TestRedactErrorSummary_脱敏与截断 验证凭据原位脱敏与 4096 上限。
// Python: redaction.py:56-72
func TestRedactErrorSummary_脱敏与截断(t *testing.T) {
	cfg := DefaultObservabilityConfig()
	got := RedactErrorSummary("authorization: Bearer abcdef123456 failed", cfg)
	if strings.Contains(got, "abcdef123456") {
		t.Fatalf("凭据未脱敏: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Fatalf("应含掩码: %q", got)
	}
	// config 为 nil 时仍保留摘要上限
	long := RedactErrorSummary(strings.Repeat("x", 5000), nil)
	if len([]rune(long)) > 4096+64 {
		t.Fatalf("超过错误摘要上限 4096: %d", len([]rune(long)))
	}
	// 多行折叠为单行
	multi := RedactErrorSummary("a\n  b\n\tc", cfg)
	if strings.ContainsAny(multi, "\n\t") {
		t.Fatalf("应折叠为单行: %q", multi)
	}
}

// TestTruncate后缀对齐Python 验证截断后缀与 Python truncate 逐字一致。
// Python: redaction.py:93-99
func TestTruncate后缀对齐Python(t *testing.T) {
	got := truncate("abcdef", 4)
	want := "abcd" + "...<OTel attribute truncated: 2 chars omitted>"
	if got != want {
		t.Fatalf("截断后缀漂移:\n got  %q\n want %q", got, want)
	}
}

// TestRedactSystemPrompt_无长度上限 验证系统提示词不走截断。
// Python: redaction.py:115-126
func TestRedactSystemPrompt_无长度上限(t *testing.T) {
	cfg := DefaultObservabilityConfig()
	cfg.AttributeValueMaxLength = 4
	text := "very long system prompt"
	if got := RedactSystemPrompt(text, cfg); got != text {
		t.Fatalf("系统提示词不应截断: %q", got)
	}
	cfg.RedactPrompts = true
	if got := RedactSystemPrompt(text, cfg); got == text || !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("显式脱敏应哈希: %q", got)
	}
}
