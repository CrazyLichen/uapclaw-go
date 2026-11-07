package worktree

import (
	"path/filepath"
	"strings"
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

// TestWorktreePathFor_嵌套slug扁平化 验证 user/feature 落盘为 user+feature。
//
// 理由（上游 _flatten_slug docstring）：嵌套目录在删除父 worktree 时
// 会连带删掉其下所有子 worktree。
func TestWorktreePathFor_嵌套slug扁平化(t *testing.T) {
	got := WorktreePathFor("/ws", "user/feat")
	want := filepath.Join("/ws", ".worktrees", "user+feat")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestWorktreePathFor_拒绝穿越 验证非法 slug 不被扁平化绕过
func TestWorktreePathFor_拒绝穿越(t *testing.T) {
	if got := WorktreePathFor("/ws", "../escape"); got != "" {
		t.Fatalf("非法 slug 应返回空路径，实际 %q", got)
	}
	if got := WorktreePathFor("/ws", "a/../b"); got != "" {
		t.Fatalf("含 .. 段的 slug 应返回空路径，实际 %q", got)
	}
}

// TestWorktreeBranchName_嵌套slug扁平化 验证分支名同样扁平化
func TestWorktreeBranchName_嵌套slug扁平化(t *testing.T) {
	if got := WorktreeBranchName("user/feat"); got != "worktree-user+feat" {
		t.Fatalf("got %q, want worktree-user+feat", got)
	}
}

// TestWorktreeBranchName_拒绝穿越 验证非法 slug 返回空分支名
func TestWorktreeBranchName_拒绝穿越(t *testing.T) {
	if got := WorktreeBranchName("../escape"); got != "" {
		t.Fatalf("非法 slug 应返回空分支名，实际 %q", got)
	}
}

// TestValidateSlug_Windows路径 验证反斜杠与盘符被拒绝。
//
// Python: test_path_traversal_windows_separator / test_windows_drive_path_rejected
func TestValidateSlug_Windows路径(t *testing.T) {
	for _, bad := range []string{`..\evil`, `C:\evil`, `a\..\..\b`, `..\\..\\x`, `a/..\b`} {
		if err := ValidateSlug(bad); err == nil {
			t.Fatalf("%q 应被拒绝", bad)
		}
	}
}

// TestValidateSlug_反斜杠归一化错误文案 验证 `..\evil` 报的是路径段错误
//
// 未归一化时 `..\evil` 被当成单段，命中的是"字符集不合法"分支，
// 错误原因误导排障方向。
func TestValidateSlug_反斜杠归一化错误文案(t *testing.T) {
	err := ValidateSlug(`..\evil`)
	if err == nil {
		t.Fatal("应被拒绝")
	}
	if !strings.Contains(err.Error(), "path segments") {
		t.Fatalf("应提示路径段问题，实际 %q", err.Error())
	}
}
