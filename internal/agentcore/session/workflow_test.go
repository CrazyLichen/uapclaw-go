package session

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
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

	ws.SetWorkflowCard("test_card")
	if ws.GetWorkflowCard() != "test_card" {
		t.Errorf("期望 workflowCard='test_card'，实际=%v", ws.GetWorkflowCard())
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
