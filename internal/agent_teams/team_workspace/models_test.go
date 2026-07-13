package team_workspace

import (
	"encoding/json"
	"testing"
	"time"
)

// TestNewTeamWorkspaceConfig 测试默认 TeamWorkspaceConfig 创建
func TestNewTeamWorkspaceConfig(t *testing.T) {
	cfg := NewTeamWorkspaceConfig()
	if cfg.Enabled {
		t.Error("默认 Enabled 应为 false")
	}
	expectedDirs := []string{
		"artifacts/code",
		"artifacts/docs",
		"artifacts/reports",
		"trajectories",
	}
	if len(cfg.ArtifactDirs) != len(expectedDirs) {
		t.Fatalf("ArtifactDirs 长度期望 %d，实际 %d", len(expectedDirs), len(cfg.ArtifactDirs))
	}
	for i, dir := range expectedDirs {
		if cfg.ArtifactDirs[i] != dir {
			t.Errorf("ArtifactDirs[%d] 期望 %s，实际 %s", i, dir, cfg.ArtifactDirs[i])
		}
	}
	if !cfg.VersionControl {
		t.Error("VersionControl 默认应为 true")
	}
	if cfg.ConflictStrategy != ConflictStrategyLock {
		t.Errorf("ConflictStrategy 期望 lock，实际 %s", cfg.ConflictStrategy)
	}
}

// TestTeamWorkspaceConfig_JSON序列化 测试 TeamWorkspaceConfig JSON 序列化往返
func TestTeamWorkspaceConfig_JSON序列化(t *testing.T) {
	original := TeamWorkspaceConfig{
		Enabled:          true,
		RootPath:         "/workspace/team-a",
		ArtifactDirs:     []string{"artifacts/code", "artifacts/docs"},
		VersionControl:   true,
		ConflictStrategy: ConflictStrategyMerge,
		RemoteURL:        "https://github.com/example/repo.git",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var decoded TeamWorkspaceConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded.Enabled != original.Enabled {
		t.Errorf("Enabled 期望 %v，实际 %v", original.Enabled, decoded.Enabled)
	}
	if decoded.RootPath != original.RootPath {
		t.Errorf("RootPath 期望 %s，实际 %s", original.RootPath, decoded.RootPath)
	}
	if decoded.ConflictStrategy != original.ConflictStrategy {
		t.Errorf("ConflictStrategy 期望 %s，实际 %s", original.ConflictStrategy, decoded.ConflictStrategy)
	}
	if len(decoded.ArtifactDirs) != len(original.ArtifactDirs) {
		t.Errorf("ArtifactDirs 长度期望 %d，实际 %d", len(original.ArtifactDirs), len(decoded.ArtifactDirs))
	}
}

// TestNewWorkspaceFileLock 测试默认 WorkspaceFileLock 创建
func TestNewWorkspaceFileLock(t *testing.T) {
	lock := NewWorkspaceFileLock("/src/main.go", "agent-1", "Agent One")
	if lock.FilePath != "/src/main.go" {
		t.Errorf("FilePath 期望 /src/main.go，实际 %s", lock.FilePath)
	}
	if lock.HolderID != "agent-1" {
		t.Errorf("HolderID 期望 agent-1，实际 %s", lock.HolderID)
	}
	if lock.HolderName != "Agent One" {
		t.Errorf("HolderName 期望 Agent One，实际 %s", lock.HolderName)
	}
	if lock.TimeoutSeconds != 300 {
		t.Errorf("TimeoutSeconds 期望 300，实际 %d", lock.TimeoutSeconds)
	}
	// 验证 AcquiredAt 是有效的 RFC3339 时间戳
	if _, err := time.Parse(time.RFC3339, lock.AcquiredAt); err != nil {
		t.Errorf("AcquiredAt 不是有效的 RFC3339 格式: %v", err)
	}
}

// TestWorkspaceFileLock_IsExpired_未过期 测试锁未过期时返回 false
func TestWorkspaceFileLock_IsExpired_未过期(t *testing.T) {
	lock := WorkspaceFileLock{
		FilePath:       "/src/main.go",
		HolderID:       "agent-1",
		HolderName:     "Agent One",
		AcquiredAt:     time.Now().UTC().Format(time.RFC3339),
		TimeoutSeconds: 300,
	}
	if lock.IsExpired() {
		t.Error("刚创建的锁不应过期")
	}
}

// TestWorkspaceFileLock_IsExpired_已过期 测试锁已过期时返回 true
func TestWorkspaceFileLock_IsExpired_已过期(t *testing.T) {
	// 使用过去的时间戳，确保已过期
	pastTime := time.Now().UTC().Add(-600 * time.Second)
	lock := WorkspaceFileLock{
		FilePath:       "/src/main.go",
		HolderID:       "agent-1",
		HolderName:     "Agent One",
		AcquiredAt:     pastTime.Format(time.RFC3339),
		TimeoutSeconds: 300,
	}
	if !lock.IsExpired() {
		t.Error("已过期的锁应返回 true")
	}
}

// TestWorkspaceFileLock_IsExpired_无效时间戳 测试无效时间戳视为过期
func TestWorkspaceFileLock_IsExpired_无效时间戳(t *testing.T) {
	lock := WorkspaceFileLock{
		FilePath:       "/src/main.go",
		HolderID:       "agent-1",
		HolderName:     "Agent One",
		AcquiredAt:     "invalid-timestamp",
		TimeoutSeconds: 300,
	}
	if !lock.IsExpired() {
		t.Error("无效时间戳应视为过期")
	}
}

// TestWorkspaceMode_枚举值 测试工作空间模式枚举值
func TestWorkspaceMode_枚举值(t *testing.T) {
	cases := []struct {
		mode WorkspaceMode
		want string
	}{
		{WorkspaceModeLocal, "local"},
		{WorkspaceModeDistributed, "distributed"},
	}
	for _, tc := range cases {
		if string(tc.mode) != tc.want {
			t.Errorf("期望 %s，实际 %s", tc.want, tc.mode)
		}
	}
}

// TestConflictStrategy_枚举值 测试冲突策略枚举值
func TestConflictStrategy_枚举值(t *testing.T) {
	cases := []struct {
		strategy ConflictStrategy
		want     string
	}{
		{ConflictStrategyLock, "lock"},
		{ConflictStrategyMerge, "merge"},
		{ConflictStrategyLastWriteWins, "last_write_wins"},
	}
	for _, tc := range cases {
		if string(tc.strategy) != tc.want {
			t.Errorf("期望 %s，实际 %s", tc.want, tc.strategy)
		}
	}
}

// TestWorkspaceFileLock_JSON序列化 测试 WorkspaceFileLock JSON 序列化往返
func TestWorkspaceFileLock_JSON序列化(t *testing.T) {
	original := WorkspaceFileLock{
		FilePath:       "/src/main.go",
		HolderID:       "agent-1",
		HolderName:     "Agent One",
		AcquiredAt:     "2024-01-01T00:00:00Z",
		TimeoutSeconds: 600,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var decoded WorkspaceFileLock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded.FilePath != original.FilePath {
		t.Errorf("FilePath 期望 %s，实际 %s", original.FilePath, decoded.FilePath)
	}
	if decoded.TimeoutSeconds != original.TimeoutSeconds {
		t.Errorf("TimeoutSeconds 期望 %d，实际 %d", original.TimeoutSeconds, decoded.TimeoutSeconds)
	}
}
