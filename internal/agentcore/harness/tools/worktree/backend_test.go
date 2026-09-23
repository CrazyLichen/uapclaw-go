package worktree

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TestCreateBackend_默认Git 测试注册表默认 Git 后端
func TestCreateBackend_默认Git(t *testing.T) {
	cfg := NewWorktreeConfig()
	backend, err := CreateBackend("git", cfg)
	if err != nil {
		t.Fatalf("CreateBackend('git') 不应返回错误: %v", err)
	}
	if backend == nil {
		t.Fatal("CreateBackend('git') 不应返回 nil")
	}
	// 验证类型
	gitBackend, ok := backend.(*GitBackend)
	if !ok {
		t.Fatal("默认后端应为 *GitBackend")
	}
	_ = gitBackend
}

// TestCreateBackend_未知名称 测试返回错误
func TestCreateBackend_未知名称(t *testing.T) {
	cfg := NewWorktreeConfig()
	_, err := CreateBackend("nonexistent", cfg)
	if err == nil {
		t.Error("未知后端名称应返回错误")
	}
}

// TestGitBackend_Exists_不存在 测试不存在的 worktree
func TestGitBackend_Exists_不存在(t *testing.T) {
	cfg := NewWorktreeConfig()
	backend := &GitBackend{config: cfg}
	// 不存在的路径
	if backend.Exists(context.TODO(), "/nonexistent/path/abc") {
		t.Error("不存在的 worktree 路径应返回 false")
	}
}

// TestRegisterWorktreeBackend 测试注册自定义后端
func TestRegisterWorktreeBackend(t *testing.T) {
	// 注意：这个测试修改全局注册表，但在同一包内测试
	called := false
	RegisterWorktreeBackend("test-custom", func(cfg WorktreeConfig) WorktreeBackend {
		called = true
		return &GitBackend{config: cfg}
	})

	backend, err := CreateBackend("test-custom", NewWorktreeConfig())
	if err != nil {
		t.Fatalf("CreateBackend('test-custom') 不应返回错误: %v", err)
	}
	if backend == nil {
		t.Fatal("自定义后端不应返回 nil")
	}
	if !called {
		t.Error("工厂函数应被调用")
	}

	// 清理：恢复默认注册表
	backendRegistry.Lock()
	delete(backendRegistry.m, "test-custom")
	backendRegistry.Unlock()
}

// TestWithEventHandler 测试 ManagerOption
func TestWithEventHandler(t *testing.T) {
	handler := func(_ context.Context, _ WorktreeEvent) error { return nil }
	opt := WithEventHandler(handler)
	opts := &managerOptions{}
	opt(opts)
	if opts.eventHandler == nil {
		t.Error("WithEventHandler 应设置 eventHandler")
	}
}

// TestWithLifecycleRails 测试 ManagerOption
func TestWithLifecycleRails(t *testing.T) {
	rail := &baseLifecycleRail{}
	opt := WithLifecycleRails(rail)
	opts := &managerOptions{}
	opt(opts)
	if len(opts.lifecycleRails) != 1 {
		t.Errorf("WithLifecycleRails 应设置 1 个 rail，实际: %d", len(opts.lifecycleRails))
	}
}

// baseLifecycleRail WorktreeLifecycleRail 的最小实现，仅用于测试
type baseLifecycleRail struct{}

func (b *baseLifecycleRail) BeforeWorktreeCreate(_ context.Context, _ *interfaces.AgentCallbackContext, _, _ string) (*string, error) {
	return nil, nil
}
func (b *baseLifecycleRail) AfterWorktreeCreate(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession) error {
	return nil
}
func (b *baseLifecycleRail) BeforeWorktreeExit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) (*string, error) {
	return nil, nil
}
func (b *baseLifecycleRail) AfterWorktreeExit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) error {
	return nil
}
func (b *baseLifecycleRail) OnWorktreeFileWrite(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) bool {
	return true
}
func (b *baseLifecycleRail) BeforeWorktreeCommit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string, _ []string) (*string, error) {
	return nil, nil
}
func (b *baseLifecycleRail) AfterWorktreeCommit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) error {
	return nil
}
func (b *baseLifecycleRail) OnWorktreeSync(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string, files []string) []string {
	return files
}
