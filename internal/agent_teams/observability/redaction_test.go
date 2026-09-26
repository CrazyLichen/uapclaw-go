package observability

import (
	"strings"
	"testing"
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
