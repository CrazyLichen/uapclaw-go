package worktree

import (
	"encoding/json"
	"testing"
)

// TestNewWorktreeConfig 测试默认 WorktreeConfig 创建
func TestNewWorktreeConfig(t *testing.T) {
	cfg := NewWorktreeConfig()
	if cfg.Enabled {
		t.Error("默认 Enabled 应为 false")
	}
	if cfg.CleanupAfterDays != 30 {
		t.Errorf("CleanupAfterDays 期望 30，实际 %d", cfg.CleanupAfterDays)
	}
	if !cfg.AutoCleanupOnShutdown {
		t.Error("AutoCleanupOnShutdown 默认应为 true")
	}
	if cfg.LifecyclePolicy != WorktreeLifecyclePolicyAuto {
		t.Errorf("LifecyclePolicy 期望 auto，实际 %s", cfg.LifecyclePolicy)
	}
	if cfg.BaseDir != "" {
		t.Errorf("BaseDir 默认应为空，实际 %s", cfg.BaseDir)
	}
	if len(cfg.SparsePaths) != 0 {
		t.Errorf("SparsePaths 默认应为空，实际 %v", cfg.SparsePaths)
	}
}

// TestWorktreeConfig_JSON序列化 测试 WorktreeConfig JSON 序列化往返
func TestWorktreeConfig_JSON序列化(t *testing.T) {
	original := WorktreeConfig{
		Enabled:               true,
		BaseDir:               "/tmp/worktree",
		SparsePaths:           []string{"src", "docs"},
		SymlinkDirectories:    []string{".env"},
		IncludePatterns:       []string{"*.go"},
		CleanupAfterDays:      7,
		AutoCleanupOnShutdown: false,
		LifecyclePolicy:       WorktreeLifecyclePolicyEphemeral,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var decoded WorktreeConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded.Enabled != original.Enabled {
		t.Errorf("Enabled 期望 %v，实际 %v", original.Enabled, decoded.Enabled)
	}
	if decoded.BaseDir != original.BaseDir {
		t.Errorf("BaseDir 期望 %s，实际 %s", original.BaseDir, decoded.BaseDir)
	}
	if decoded.CleanupAfterDays != original.CleanupAfterDays {
		t.Errorf("CleanupAfterDays 期望 %d，实际 %d", original.CleanupAfterDays, decoded.CleanupAfterDays)
	}
	if decoded.LifecyclePolicy != original.LifecyclePolicy {
		t.Errorf("LifecyclePolicy 期望 %s，实际 %s", original.LifecyclePolicy, decoded.LifecyclePolicy)
	}
	if len(decoded.SparsePaths) != len(original.SparsePaths) {
		t.Errorf("SparsePaths 长度期望 %d，实际 %d", len(original.SparsePaths), len(decoded.SparsePaths))
	}
}

// TestWorktreeLifecyclePolicy_枚举值 测试生命周期策略枚举值
func TestWorktreeLifecyclePolicy_枚举值(t *testing.T) {
	cases := []struct {
		policy WorktreeLifecyclePolicy
		want   string
	}{
		{WorktreeLifecyclePolicyAuto, "auto"},
		{WorktreeLifecyclePolicyEphemeral, "ephemeral"},
		{WorktreeLifecyclePolicyDurable, "durable"},
	}
	for _, tc := range cases {
		if string(tc.policy) != tc.want {
			t.Errorf("期望 %s，实际 %s", tc.want, tc.policy)
		}
	}
}

// TestWorktreeSession_JSON序列化 测试 WorktreeSession JSON 序列化往返
func TestWorktreeSession_JSON序列化(t *testing.T) {
	original := WorktreeSession{
		OriginalCWD:      "/home/user/project",
		WorktreePath:     "/tmp/worktree-abc",
		WorktreeName:     "worktree-abc",
		WorktreeBranch:   "feature-x",
		OriginalBranch:   "main",
		OriginalHeadCommit: "abc123",
		MemberName:       "agent-1",
		TeamName:         "team-a",
		HookBased:        true,
		LifecyclePolicy:  WorktreeLifecyclePolicyDurable,
		TeamLifecycle:    "active",
		CreationDurationMs: 150.5,
		UsedSparsePaths:  false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var decoded WorktreeSession
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded.OriginalCWD != original.OriginalCWD {
		t.Errorf("OriginalCWD 期望 %s，实际 %s", original.OriginalCWD, decoded.OriginalCWD)
	}
	if decoded.WorktreePath != original.WorktreePath {
		t.Errorf("WorktreePath 期望 %s，实际 %s", original.WorktreePath, decoded.WorktreePath)
	}
	if decoded.CreationDurationMs != original.CreationDurationMs {
		t.Errorf("CreationDurationMs 期望 %f，实际 %f", original.CreationDurationMs, decoded.CreationDurationMs)
	}
}

// TestWorktreeCreateResult_JSON序列化 测试 WorktreeCreateResult JSON 序列化往返
func TestWorktreeCreateResult_JSON序列化(t *testing.T) {
	original := WorktreeCreateResult{
		WorktreePath:   "/tmp/worktree-xyz",
		WorktreeBranch: "feature-y",
		HeadCommit:     "def456",
		BaseBranch:     "main",
		Existed:        false,
		HookBased:      true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var decoded WorktreeCreateResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded.WorktreePath != original.WorktreePath {
		t.Errorf("WorktreePath 期望 %s，实际 %s", original.WorktreePath, decoded.WorktreePath)
	}
	if decoded.Existed != original.Existed {
		t.Errorf("Existed 期望 %v，实际 %v", original.Existed, decoded.Existed)
	}
}

// TestWorktreeChangeSummary_JSON序列化 测试 WorktreeChangeSummary JSON 序列化往返
func TestWorktreeChangeSummary_JSON序列化(t *testing.T) {
	original := WorktreeChangeSummary{
		ChangedFiles: 5,
		Commits:      2,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var decoded WorktreeChangeSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded.ChangedFiles != original.ChangedFiles {
		t.Errorf("ChangedFiles 期望 %d，实际 %d", original.ChangedFiles, decoded.ChangedFiles)
	}
	if decoded.Commits != original.Commits {
		t.Errorf("Commits 期望 %d，实际 %d", original.Commits, decoded.Commits)
	}
}
