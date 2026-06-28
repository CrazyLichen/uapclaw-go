package session

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// TestNewWorkflowSessionFacade 测试基本构造
func TestNewWorkflowSessionFacade(t *testing.T) {
	ws := NewWorkflowSession()
	if ws == nil {
		t.Fatal("NewWorkflowSession 返回 nil")
	}
}

// TestWorkflowSessionFacade_GetSessionID 测试返回 inner.SessionID()
func TestWorkflowSessionFacade_GetSessionID(t *testing.T) {
	inner := internal.NewWorkflowSession(internal.WithWorkflowSessionID("test-id"))
	ws := NewWorkflowSession(WithWorkflowSessionInner(inner))

	if ws.GetSessionID() != "test-id" {
		t.Errorf("期望 SessionID='test-id'，实际=%s", ws.GetSessionID())
	}
}

// TestWorkflowSessionFacade_GetEnvs 测试返回 envs
func TestWorkflowSessionFacade_GetEnvs(t *testing.T) {
	ws := NewWorkflowSession()

	envs := ws.GetEnvs()
	if envs == nil {
		t.Error("期望 envs 非 nil")
	}
}

// TestWorkflowSessionFacade_GetParent 测试返回 inner.Parent()
func TestWorkflowSessionFacade_GetParent(t *testing.T) {
	parent := internal.NewAgentSession("parent-123")
	inner := internal.NewWorkflowSession(internal.WithWorkflowParent(parent))
	ws := NewWorkflowSession(WithWorkflowSessionInner(inner))

	p := ws.GetParent()
	if p == nil {
		t.Error("期望 GetParent 非 nil")
	}
}

// TestWorkflowSessionFacade_SetGetWorkflowCard 测试设置和获取卡片
func TestWorkflowSessionFacade_SetGetWorkflowCard(t *testing.T) {
	ws := NewWorkflowSession()

	card := schema.NewWorkflowCard(schema.WithName("test_workflow"), schema.WithID("wf-001"))
	ws.SetWorkflowCard(card)
	got := ws.GetWorkflowCard()
	if got == nil {
		t.Fatal("期望 GetWorkflowCard 返回非 nil")
	}
	if got.ID != "wf-001" {
		t.Errorf("期望 card.ID='wf-001'，实际=%s", got.ID)
	}
	if got.Name != "test_workflow" {
		t.Errorf("期望 card.Name='test_workflow'，实际=%s", got.Name)
	}
}

// TestWorkflowSessionFacade_Close 测试委托 inner.Close()
func TestWorkflowSessionFacade_Close(t *testing.T) {
	inner := internal.NewWorkflowSession()
	ws := NewWorkflowSession(WithWorkflowSessionInner(inner))

	err := ws.Close()
	if err != nil {
		t.Errorf("期望 Close 返回 nil，实际=%v", err)
	}
}

// TestWorkflowSessionFacade_Inner为nil时防御 测试 inner 为 nil 时各方法不 panic
func TestWorkflowSessionFacade_Inner为nil时防御(t *testing.T) {
	ws := &WorkflowSession{envs: make(map[string]any)}

	// 以下均不应 panic
	if ws.GetSessionID() != "" {
		t.Error("期望 inner 为 nil 时 GetSessionID 返回空串")
	}
	if ws.GetParent() != nil {
		t.Error("期望 inner 为 nil 时 GetParent 返回 nil")
	}
	if ws.Close() != nil {
		t.Error("期望 inner 为 nil 时 Close 返回 nil")
	}
}

// TestWithWorkflowSessionID_inner为nil时创建 测试 WithWorkflowSessionID 在 inner 为 nil 时自动创建
func TestWithWorkflowSessionID_inner为nil时创建(t *testing.T) {
	ws := NewWorkflowSession(WithWorkflowSessionID("auto-id"))
	if ws.inner == nil {
		t.Fatal("WithWorkflowSessionID 在 inner 为 nil 时应自动创建 inner")
	}
	if ws.inner.SessionID() != "auto-id" {
		t.Errorf("期望 inner.SessionID()='auto-id'，实际=%s", ws.inner.SessionID())
	}
}

// TestWithWorkflowSessionID_inner已存在时跳过 测试 inner 已设置时不覆盖
func TestWithWorkflowSessionID_inner已存在时跳过(t *testing.T) {
	inner := internal.NewWorkflowSession(internal.WithWorkflowSessionID("original-id"))
	ws := NewWorkflowSession(
		WithWorkflowSessionInner(inner),
		WithWorkflowSessionID("new-id"), // 应因 inner 已存在而跳过
	)
	if ws.inner.SessionID() != "original-id" {
		t.Errorf("inner 已存在时不应覆盖，期望='original-id'，实际=%s", ws.inner.SessionID())
	}
}

// TestWithWorkflowSessionParent 测试 WithWorkflowSessionParent 选项设置 envs
func TestWithWorkflowSessionParent(t *testing.T) {
	parent := internal.NewAgentSession("parent-123")
	ws := NewWorkflowSession(WithWorkflowSessionParent(parent))
	if ws.envs == nil {
		t.Error("WithWorkflowSessionParent 应设置 envs")
	}
}

// TestWorkflowSessionFacade_Inner 测试 Inner() 方法返回内部实例
func TestWorkflowSessionFacade_Inner(t *testing.T) {
	inner := internal.NewWorkflowSession()
	ws := NewWorkflowSession(WithWorkflowSessionInner(inner))

	if ws.Inner() != inner {
		t.Error("Inner() 应返回注入的内部实例")
	}
}
