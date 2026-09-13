package worktree

import (
	"context"
	"testing"
)

// TestIsEphemeralSlug 测试匹配/不匹配
func TestIsEphemeralSlug(t *testing.T) {
	matching := []string{
		"teammate-abc12345",
		"teammate-deadbeef",
		"agent-abc1234",
	}
	for _, slug := range matching {
		if !IsEphemeralSlug(slug) {
			t.Errorf("IsEphemeralSlug(%q) 应返回 true", slug)
		}
	}

	nonMatching := []string{
		"feature-auth",
		"teammate-xyz",       // hex 太短
		"teammate-123456789", // hex 太长
		"agent-abc12345",     // 7位 → 8位太长
		"my-worktree",
	}
	for _, slug := range nonMatching {
		if IsEphemeralSlug(slug) {
			t.Errorf("IsEphemeralSlug(%q) 应返回 false", slug)
		}
	}
}

// TestCleanupStaleWorktrees_无WorktreeDir 测试目录不存在
func TestCleanupStaleWorktrees_无WorktreeDir(t *testing.T) {
	cfg := NewWorktreeConfig()
	backend := &fakeBackend{}
	ctx := context.Background()

	// workspace 未设置，直接返回 0
	count, err := CleanupStaleWorktrees(ctx, cfg, backend, "")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if count != 0 {
		t.Errorf("无 workspace 时应返回 0，实际 %d", count)
	}
}
