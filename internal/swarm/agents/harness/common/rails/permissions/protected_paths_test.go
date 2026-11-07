package permissions

import (
	"strings"
	"testing"
)

// ──────────────────────────── MergeProtectedWritePaths 测试 ────────────────────────────

// TestMergeProtectedWritePaths_去重保序 验证合并去重并保持首次出现顺序
func TestMergeProtectedWritePaths_去重保序(t *testing.T) {
	got := MergeProtectedWritePaths(
		[]string{"b", "a"},
		[]string{"a", "c"},
	)
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("期望 %v，实际 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("期望 %v，实际 %v", want, got)
		}
	}
}

// TestMergeProtectedWritePaths_过滤空白 验证空白项被过滤
func TestMergeProtectedWritePaths_过滤空白(t *testing.T) {
	got := MergeProtectedWritePaths([]string{"", "  ", "a", "\t"})
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("应只剩 a，实际 %v", got)
	}
}

// TestMergeProtectedWritePaths_trim 验证每项被 trim
func TestMergeProtectedWritePaths_trim(t *testing.T) {
	got := MergeProtectedWritePaths([]string{"  a  "})
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("应 trim 为 a，实际 %v", got)
	}
}

// TestMergeProtectedWritePaths_空输入 返回空切片
func TestMergeProtectedWritePaths_空输入(t *testing.T) {
	if got := MergeProtectedWritePaths(); len(got) != 0 {
		t.Fatalf("空输入应返回空，实际 %v", got)
	}
	if got := MergeProtectedWritePaths(nil, nil); len(got) != 0 {
		t.Fatalf("nil 输入应返回空，实际 %v", got)
	}
}

// TestMergeProtectedWritePaths_默认集 验证默认保护路径非空且无重复
func TestMergeProtectedWritePaths_默认集(t *testing.T) {
	got := MergeProtectedWritePaths(JiuwenclawProtectedWritePaths)
	if len(got) == 0 {
		t.Fatalf("默认集不应为空")
	}
	seen := make(map[string]struct{}, len(got))
	for _, p := range got {
		if _, ok := seen[p]; ok {
			t.Fatalf("出现重复项 %q", p)
		}
		seen[p] = struct{}{}
		if strings.TrimSpace(p) != p {
			t.Fatalf("默认项不应含空白: %q", p)
		}
	}
}

// TestMergeProtectedWritePaths_默认集条目数 验证默认集为 4 项（对齐 Python）
func TestMergeProtectedWritePaths_默认集条目数(t *testing.T) {
	if len(JiuwenclawProtectedWritePaths) != 4 {
		t.Fatalf("默认集应为 4 项，实际 %d", len(JiuwenclawProtectedWritePaths))
	}
}
