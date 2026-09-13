package worktree

import (
	"path/filepath"
	"testing"
)

// TestValidateSlug_合法 测试合法 slug
func TestValidateSlug_合法(t *testing.T) {
	slugs := []string{"feature-auth", "my-feature", "user/feature-login", "a.b_c-d", "x"}
	for _, slug := range slugs {
		if err := ValidateSlug(slug); err != nil {
			t.Errorf("ValidateSlug(%q) 不应返回错误: %v", slug, err)
		}
	}
}

// TestValidateSlug_路径遍历 测试路径遍历拒绝
func TestValidateSlug_路径遍历(t *testing.T) {
	if err := ValidateSlug(".."); err == nil {
		t.Error("ValidateSlug('..') 应返回错误")
	}
	if err := ValidateSlug("."); err == nil {
		t.Error("ValidateSlug('.') 应返回错误")
	}
	if err := ValidateSlug("../etc"); err == nil {
		t.Error("ValidateSlug('../etc') 应返回错误")
	}
}

// TestValidateSlug_超长 测试超长 slug 拒绝
func TestValidateSlug_超长(t *testing.T) {
	longSlug := makeLongString('a', MaxSlugLength+1)
	if err := ValidateSlug(longSlug); err == nil {
		t.Error("超长 slug 应返回错误")
	}
}

// TestValidateSlug_非法字符 测试非法字符拒绝
func TestValidateSlug_非法字符(t *testing.T) {
	illegal := []string{"feature auth", "feature@auth", "feature#auth", ""}
	for _, slug := range illegal {
		if err := ValidateSlug(slug); err == nil {
			t.Errorf("ValidateSlug(%q) 应返回错误", slug)
		}
	}
}

// TestWorktreeBranchName 测试分支名生成
func TestWorktreeBranchName(t *testing.T) {
	tests := []struct {
		slug     string
		expected string
	}{
		{"feature-auth", "worktree-feature-auth"},
		{"user/feature-login", "worktree-user+feature-login"},
		{"a/b/c", "worktree-a+b+c"},
	}
	for _, tt := range tests {
		got := WorktreeBranchName(tt.slug)
		if got != tt.expected {
			t.Errorf("WorktreeBranchName(%q) = %q, want %q", tt.slug, got, tt.expected)
		}
	}
}

// TestWorktreePathFor 测试路径计算
func TestWorktreePathFor(t *testing.T) {
	got := WorktreePathFor("/tmp/workspace", "feature-auth")
	expected := filepath.Join("/tmp/workspace", ".worktrees", "feature-auth")
	if got != expected {
		t.Errorf("WorktreePathFor = %q, want %q", got, expected)
	}
}

// TestWorktreesDir 测试父目录
func TestWorktreesDir(t *testing.T) {
	got := WorktreesDir("/tmp/workspace")
	expected := filepath.Join("/tmp/workspace", ".worktrees")
	if got != expected {
		t.Errorf("WorktreesDir = %q, want %q", got, expected)
	}
}

// makeLongString 生成长度为 n 的重复字符字符串
func makeLongString(c rune, n int) string {
	result := make([]rune, n)
	for i := range result {
		result[i] = c
	}
	return string(result)
}
