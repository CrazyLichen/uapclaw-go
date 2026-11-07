package rails

import (
	"testing"
)

// ──────────────────────────── 常量测试 ────────────────────────────

// TestMaxStructuredQuestions_值 验证与 Python 一致为 4
func TestMaxStructuredQuestions_值(t *testing.T) {
	if MaxStructuredQuestions != 4 {
		t.Fatalf("MaxStructuredQuestions 应为 4，实际 %d", MaxStructuredQuestions)
	}
}

// TestMaxStructuredQuestions_边界可用 验证常量可直接用于切片越界判断
func TestMaxStructuredQuestions_边界可用(t *testing.T) {
	items := make([]string, MaxStructuredQuestions)
	if len(items) > MaxStructuredQuestions {
		t.Fatalf("构造失败")
	}
	over := make([]string, MaxStructuredQuestions+1)
	if len(over) <= MaxStructuredQuestions {
		t.Fatalf("越界判断失败")
	}
}
