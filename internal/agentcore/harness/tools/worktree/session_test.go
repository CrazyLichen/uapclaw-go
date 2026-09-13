package worktree

import (
	"context"
	"testing"
)

// TestInitWorktreeSessionState 测试创建 WorktreeSessionState
func TestInitWorktreeSessionState(t *testing.T) {
	state := InitWorktreeSessionState()
	if state == nil {
		t.Fatal("InitWorktreeSessionState 不应返回 nil")
	}
	if state.GetCurrentSession() != nil {
		t.Error("新创建的 session 应为 nil")
	}
	if state.GetDefaultWorktreeName() != "" {
		t.Error("新创建的 defaultWorktreeName 应为空")
	}
}

// TestWithWorktreeSessionState 测试 context 注入和取出
func TestWithWorktreeSessionState(t *testing.T) {
	state := InitWorktreeSessionState()
	ctx := context.Background()

	// 注入前
	if WorktreeSessionStateFromCtx(ctx) != nil {
		t.Error("未注入 context 应返回 nil")
	}

	// 注入后
	ctx = WithWorktreeSessionState(ctx, state)
	got := WorktreeSessionStateFromCtx(ctx)
	if got != state {
		t.Error("注入后应返回同一 WorktreeSessionState 实例")
	}
}

// TestSetCurrentSession 测试设置后读取
func TestSetCurrentSession(t *testing.T) {
	state := InitWorktreeSessionState()
	ctx := WithWorktreeSessionState(context.Background(), state)

	session := &WorktreeSession{
		OriginalCWD:  "/home/user/project",
		WorktreePath: "/tmp/worktree-abc",
		WorktreeName: "abc",
	}

	SetCurrentSession(ctx, session)
	got := GetCurrentSession(ctx)
	if got == nil {
		t.Fatal("SetCurrentSession 后应能取出 session")
	}
	if got.WorktreeName != "abc" {
		t.Errorf("WorktreeName 期望 abc，实际 %s", got.WorktreeName)
	}

	// 清空
	SetCurrentSession(ctx, nil)
	if GetCurrentSession(ctx) != nil {
		t.Error("SetCurrentSession(nil) 后应为 nil")
	}
}

// TestRequireCurrentSession_存在 测试正常返回
func TestRequireCurrentSession_存在(t *testing.T) {
	state := InitWorktreeSessionState()
	ctx := WithWorktreeSessionState(context.Background(), state)

	session := &WorktreeSession{WorktreeName: "test"}
	SetCurrentSession(ctx, session)

	got, err := RequireCurrentSession(ctx)
	if err != nil {
		t.Fatalf("RequireCurrentSession 不应返回错误: %v", err)
	}
	if got.WorktreeName != "test" {
		t.Errorf("WorktreeName 期望 test，实际 %s", got.WorktreeName)
	}
}

// TestRequireCurrentSession_不存在 测试返回 error
func TestRequireCurrentSession_不存在(t *testing.T) {
	state := InitWorktreeSessionState()
	ctx := WithWorktreeSessionState(context.Background(), state)

	_, err := RequireCurrentSession(ctx)
	if err == nil {
		t.Error("无 session 时 RequireCurrentSession 应返回错误")
	}
}

// TestGetSetDefaultWorktreeName 测试默认名读写
func TestGetSetDefaultWorktreeName(t *testing.T) {
	state := InitWorktreeSessionState()
	ctx := WithWorktreeSessionState(context.Background(), state)

	if name := GetDefaultWorktreeName(ctx); name != "" {
		t.Errorf("默认 defaultWorktreeName 应为空，实际 %q", name)
	}

	SetDefaultWorktreeName(ctx, "swift-fox-a3b1")
	if name := GetDefaultWorktreeName(ctx); name != "swift-fox-a3b1" {
		t.Errorf("defaultWorktreeName 期望 swift-fox-a3b1，实际 %q", name)
	}

	SetDefaultWorktreeName(ctx, "")
	if name := GetDefaultWorktreeName(ctx); name != "" {
		t.Errorf("清空后 defaultWorktreeName 应为空，实际 %q", name)
	}
}
