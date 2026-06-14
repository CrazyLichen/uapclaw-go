package session

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
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

// TestWorkflowSessionFacade_UpdateState_GetState 测试通过 WorkflowCommitState 读写状态
func TestWorkflowSessionFacade_UpdateState_GetState(t *testing.T) {
	inner := internal.NewWorkflowSession()
	ws := NewWorkflowSession(WithWorkflowSessionInner(inner))

	// 更新状态
	ws.UpdateState(map[string]any{"key1": "value1"})

	// 提交后可读
	if cs, ok := inner.State().(*state.WorkflowCommitState); ok {
		cs.Commit()
	}

	result := ws.GetState("key1")
	if result != "value1" {
		t.Errorf("期望 GetState='value1'，实际=%v", result)
	}
}

// TestWorkflowSessionFacade_DumpState 测试导出完整快照
func TestWorkflowSessionFacade_DumpState(t *testing.T) {
	inner := internal.NewWorkflowSession()
	ws := NewWorkflowSession(WithWorkflowSessionInner(inner))

	dump := ws.DumpState()
	if dump == nil {
		t.Error("期望 DumpState 非 nil")
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
	if ws.State() != nil {
		t.Error("期望 inner 为 nil 时 State 返回 nil")
	}
	ws.UpdateState(map[string]any{"key": "val"}) // 不 panic
	if ws.GetState("key") != nil {
		t.Error("期望 inner 为 nil 时 GetState 返回 nil")
	}
	if ws.DumpState() != nil {
		t.Error("期望 inner 为 nil 时 DumpState 返回 nil")
	}
	if ws.Close() != nil {
		t.Error("期望 inner 为 nil 时 Close 返回 nil")
	}
}
