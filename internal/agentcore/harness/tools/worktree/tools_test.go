package worktree

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// TestGenerateRandomSlug 测试随机 slug 格式
func TestGenerateRandomSlug(t *testing.T) {
	slug := generateRandomSlug()

	// 格式：<adj>-<noun>-<4hex>
	parts := strings.Split(slug, "-")
	if len(parts) != 3 {
		t.Errorf("随机 slug 应有 3 部分，实际: %d (%q)", len(parts), slug)
	}
	if len(parts[2]) != 4 {
		t.Errorf("hex 后缀应为 4 字符，实际: %d (%q)", len(parts[2]), parts[2])
	}

	// 验证 slug 合法
	if err := ValidateSlug(slug); err != nil {
		t.Errorf("随机 slug 应通过校验: %v", err)
	}
}

// TestEnterWorktreeTool_Invoke_已在Worktree 测试重复进入
func TestEnterWorktreeTool_Invoke_已在Worktree(t *testing.T) {
	state := InitWorktreeSessionState()
	ctx := WithWorktreeSessionState(context.Background(), state)

	// 设置已有 session
	SetCurrentSession(ctx, &WorktreeSession{WorktreeName: "existing"})

	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, &fakeBackend{})
	enterTool := &EnterWorktreeTool{
		worktreeToolBase: worktreeToolBase{manager: mgr},
	}

	result, err := enterTool.Invoke(ctx, map[string]any{}, nil)
	_ = err // 工具层错误通过 result 返回
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if _, ok := result["error"]; !ok {
		t.Error("已在 worktree 时应返回 error 字段")
	}
	if !strings.Contains(result["error"].(string), "Already in worktree") {
		t.Errorf("错误信息应包含 'Already in worktree'，实际: %s", result["error"])
	}
}

// TestExitWorktreeTool_Invoke_无Session 测试无 session 退出
func TestExitWorktreeTool_Invoke_无Session(t *testing.T) {
	state := InitWorktreeSessionState()
	ctx := WithWorktreeSessionState(context.Background(), state)

	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, &fakeBackend{})
	exitTool := &ExitWorktreeTool{
		worktreeToolBase: worktreeToolBase{manager: mgr},
	}

	result, _ := exitTool.Invoke(ctx, map[string]any{"action": "keep"}, nil)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if _, ok := result["error"]; !ok {
		t.Error("无 session 时应返回 error 字段")
	}
}

// TestExitWorktreeTool_Invoke_无效Action 测试无效 action
func TestExitWorktreeTool_Invoke_无效Action(t *testing.T) {
	state := InitWorktreeSessionState()
	ctx := WithWorktreeSessionState(context.Background(), state)
	SetCurrentSession(ctx, &WorktreeSession{WorktreeName: "test"})

	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, &fakeBackend{})
	exitTool := &ExitWorktreeTool{
		worktreeToolBase: worktreeToolBase{manager: mgr},
	}

	result, _ := exitTool.Invoke(ctx, map[string]any{"action": "invalid"}, nil)
	if _, ok := result["error"]; !ok {
		t.Error("无效 action 应返回 error")
	}
}

// TestEnterWorktreeTool_Invoke_Enter失败 测试 Enter 失败场景
func TestEnterWorktreeTool_Invoke_Enter失败(t *testing.T) {
	state := InitWorktreeSessionState()
	ctx := WithWorktreeSessionState(context.Background(), state)

	// 使用会失败的 backend
	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, &failingBackend{})
	enterTool := &EnterWorktreeTool{
		worktreeToolBase: worktreeToolBase{manager: mgr},
	}

	result, _ := enterTool.Invoke(ctx, map[string]any{"name": "test-slug"}, nil)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if _, ok := result["error"]; !ok {
		t.Error("Enter 失败应返回 error")
	}
}

// failingBackend 总是返回错误的假后端
type failingBackend struct{}

func (f *failingBackend) Create(_ context.Context, _, _, _ string) (*WorktreeCreateResult, error) {
	return nil, fmt.Errorf("模拟失败")
}
func (f *failingBackend) Remove(_ context.Context, _, _ string) bool { return false }
func (f *failingBackend) Exists(_ context.Context, _ string) bool    { return false }

// TestExitWorktreeTool_Invoke_Keep 测试 keep 退出
func TestExitWorktreeTool_Invoke_Keep(t *testing.T) {
	state := InitWorktreeSessionState()
	ctx := WithWorktreeSessionState(context.Background(), state)
	SetCurrentSession(ctx, &WorktreeSession{
		OriginalCWD:    "/home/user/project",
		WorktreePath:   "/tmp/worktree-test",
		WorktreeName:   "test",
		WorktreeBranch: "worktree-test",
	})

	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, &fakeBackend{})
	exitTool := &ExitWorktreeTool{
		worktreeToolBase: worktreeToolBase{manager: mgr},
	}

	result, _ := exitTool.Invoke(ctx, map[string]any{"action": "keep"}, nil)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if result["action"] != "keep" {
		t.Errorf("action 期望 keep，实际 %v", result["action"])
	}
	if result["worktree_name"] != "test" {
		t.Errorf("worktree_name 期望 test，实际 %v", result["worktree_name"])
	}
}

// TestWorktreeToolBase_StreamNotSupported 测试流式不支持
func TestWorktreeToolBase_StreamNotSupported(t *testing.T) {
	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, &fakeBackend{})
	base := &worktreeToolBase{
		card:    tool.NewToolCard("test", "test", nil, nil),
		manager: mgr,
	}

	_, err := base.Stream(context.Background(), nil)
	if err == nil {
		t.Error("Stream 应返回 ErrStreamNotSupported")
	}
}

// TestRandomSlug_唯一性 测试随机 slug 不重复
func TestRandomSlug_唯一性(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		slug := generateRandomSlug()
		if seen[slug] {
			t.Errorf("随机 slug 重复: %s", slug)
		}
		seen[slug] = true
	}
}
