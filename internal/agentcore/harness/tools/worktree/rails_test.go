package worktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestWorktreeRail_New 测试创建
func TestWorktreeRail_New(t *testing.T) {
	rail := NewWorktreeRail()
	if rail == nil {
		t.Fatal("NewWorktreeRail 不应返回 nil")
	}
	if rail.Priority() != 100 {
		t.Errorf("Priority 期望 100，实际 %d", rail.Priority())
	}
}

// TestWorktreeRail_NewWithOptions 测试带选项创建
func TestWorktreeRail_NewWithOptions(t *testing.T) {
	handler := func(_ context.Context, _ WorktreeEvent) error { return nil }
	cfg := NewWorktreeConfig()
	cfg.Enabled = true
	rail := NewWorktreeRail(
		WithWorktreeRailConfig(cfg),
		WithWorktreeRailEventHandler(handler),
	)
	if rail.userConfig.Enabled != true {
		t.Error("config 应设置 Enabled=true")
	}
	if rail.eventHandler == nil {
		t.Error("eventHandler 应设置")
	}
}

// TestWorktreeRail_BeforeInvoke_无Session 测试无 session 恢复
func TestWorktreeRail_BeforeInvoke_无Session(t *testing.T) {
	rail := NewWorktreeRail()
	wtState := InitWorktreeSessionState()
	ctx := WithWorktreeSessionState(context.Background(), wtState)

	err := rail.BeforeInvoke(ctx, nil)
	if err != nil {
		t.Errorf("BeforeInvoke(nil cbc) 不应返回错误: %v", err)
	}
}

// TestWorktreeRail_AfterInvoke_无Session 测试无 session 持久化
func TestWorktreeRail_AfterInvoke_无Session(t *testing.T) {
	rail := NewWorktreeRail()
	wtState := InitWorktreeSessionState()
	ctx := WithWorktreeSessionState(context.Background(), wtState)

	err := rail.AfterInvoke(ctx, nil)
	if err != nil {
		t.Errorf("AfterInvoke(nil cbc) 不应返回错误: %v", err)
	}
}

// TestDetectSetup_Python 测试 pyproject.toml 检测
func TestDetectSetup_Python(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]"), 0o644)
	commands := detectSetup(dir)
	if len(commands) != 1 || commands[0] != "uv sync --quiet" {
		t.Errorf("Python 项目应返回 'uv sync --quiet'，实际: %v", commands)
	}
}

// TestDetectSetup_Node 测试 package.json 检测
func TestDetectSetup_Node(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644)
	commands := detectSetup(dir)
	if len(commands) != 1 || commands[0] != "npm install --silent" {
		t.Errorf("Node 项目应返回 'npm install --silent'，实际: %v", commands)
	}
}

// TestDetectSetup_Unknown 测试未知项目
func TestDetectSetup_Unknown(t *testing.T) {
	dir := t.TempDir()
	commands := detectSetup(dir)
	if len(commands) != 0 {
		t.Errorf("未知项目应返回空命令，实际: %v", commands)
	}
}

// TestDiffSummaryRail_Remove 测试 action=remove 时跳过
func TestDiffSummaryRail_Remove(t *testing.T) {
	rail := &DiffSummaryRail{}
	ctx := context.Background()
	session := &WorktreeSession{
		WorktreePath:       "/tmp/test",
		OriginalHeadCommit: "abc123",
	}

	result, err := rail.BeforeWorktreeExit(ctx, session, "remove")
	if err != nil {
		t.Errorf("BeforeWorktreeExit 不应返回错误: %v", err)
	}
	// action=remove 时应返回空（不干预）
	if result != "" {
		t.Errorf("action=remove 时应返回空字符串，实际: %q", result)
	}
}
