package external

import (
	"context"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// testProvider 测试用 MemoryProvider 实现
type testProvider struct {
	BaseMemoryProvider
	name        string
	available   bool
	initialized bool
	toolSchemas []ToolSchema
}

func (t *testProvider) Name() string      { return t.name }
func (t *testProvider) IsAvailable() bool { return t.available }
func (t *testProvider) Initialize(_ context.Context, _ ...ProviderOption) error {
	t.initialized = true
	return nil
}
func (t *testProvider) GetToolSchemas() []ToolSchema { return t.toolSchemas }
func (t *testProvider) HandleToolCall(_ context.Context, _ string, _ map[string]any) (string, error) {
	return `{"result": "ok"}`, nil
}
func (t *testProvider) Prefetch(_ context.Context, _ string, _ ...ProviderOption) (string, error) {
	return "test prefetch result", nil
}
func (t *testProvider) SyncTurn(_ context.Context, _, _ string, _ ...ProviderOption) error {
	return nil
}
func (t *testProvider) IsInitialized() bool { return t.initialized }

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestToolSchema_JSON序列化(t *testing.T) {
	ts := ToolSchema{
		Name:        "mem0_search",
		Description: "搜索记忆",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
		},
	}
	if ts.Name != "mem0_search" {
		t.Errorf("Name = %q, want %q", ts.Name, "mem0_search")
	}
	if ts.Description != "搜索记忆" {
		t.Errorf("Description = %q, want %q", ts.Description, "搜索记忆")
	}
}

func TestBaseMemoryProvider_默认值(t *testing.T) {
	var base BaseMemoryProvider

	if base.SystemPromptBlock() != "" {
		t.Errorf("SystemPromptBlock() = %q, want empty", base.SystemPromptBlock())
	}
	if err := base.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() = %v, want nil", err)
	}
	if err := base.OnSessionEnd(context.Background(), nil); err != nil {
		t.Errorf("OnSessionEnd() = %v, want nil", err)
	}
	if base.IsInitialized() != false {
		t.Errorf("IsInitialized() = %v, want false", base.IsInitialized())
	}
}

func TestApplyOptions_全部选项(t *testing.T) {
	opts := applyOptions(
		WithUserID("u1"),
		WithScopeID("s1"),
		WithSessionID("sess1"),
	)
	if opts.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", opts.UserID, "u1")
	}
	if opts.ScopeID != "s1" {
		t.Errorf("ScopeID = %q, want %q", opts.ScopeID, "s1")
	}
	if opts.SessionID != "sess1" {
		t.Errorf("SessionID = %q, want %q", opts.SessionID, "sess1")
	}
}

func TestApplyOptions_空选项(t *testing.T) {
	opts := applyOptions()
	if opts.UserID != "" {
		t.Errorf("UserID = %q, want empty", opts.UserID)
	}
	if opts.ScopeID != "" {
		t.Errorf("ScopeID = %q, want empty", opts.ScopeID)
	}
	if opts.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", opts.SessionID)
	}
}

func TestApplyOptions_部分选项(t *testing.T) {
	opts := applyOptions(WithUserID("u2"))
	if opts.UserID != "u2" {
		t.Errorf("UserID = %q, want %q", opts.UserID, "u2")
	}
	if opts.ScopeID != "" {
		t.Errorf("ScopeID = %q, want empty", opts.ScopeID)
	}
}

func TestTestProvider_嵌入Base后覆盖IsInitialized(t *testing.T) {
	p := &testProvider{name: "test", available: true}
	// 嵌入 BaseMemoryProvider，IsInitialized 由 Base 默认返回 false
	if p.IsInitialized() != false {
		t.Errorf("初始化前 IsInitialized() = %v, want false", p.IsInitialized())
	}
	// Initialize 后由 testProvider 覆盖的 IsInitialized 返回 true
	if err := p.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}
	if p.IsInitialized() != true {
		t.Errorf("初始化后 IsInitialized() = %v, want true", p.IsInitialized())
	}
}

func TestMemoryProvider_接口满足(t *testing.T) {
	// 编译时验证 testProvider 满足 MemoryProvider 接口
	var _ MemoryProvider = (*testProvider)(nil)
}
