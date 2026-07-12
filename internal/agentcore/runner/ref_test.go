package runner

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAgentRef_ByAgentID 按ID构造
func TestAgentRef_ByAgentID(t *testing.T) {
	ref := ByAgentID("my-agent")
	if !ref.IsByID() {
		t.Error("IsByID 应为 true")
	}
	if ref.IsByInstance() {
		t.Error("IsByInstance 应为 false")
	}
	if ref.ID() != "my-agent" {
		t.Errorf("ID = %q, want my-agent", ref.ID())
	}
	if ref.Agent() != nil {
		t.Error("Agent 应为 nil")
	}
}

// TestAgentRef_ByAgent 按实例构造
func TestAgentRef_ByAgent(t *testing.T) {
	agent := &mockAgent{}
	ref := ByAgent(agent)
	if ref.IsByID() {
		t.Error("IsByID 应为 false")
	}
	if !ref.IsByInstance() {
		t.Error("IsByInstance 应为 true")
	}
	if ref.Agent() == nil {
		t.Error("Agent 不应为 nil")
	}
}

// TestAgentRef_同时设置ID和实例 两者可共存
func TestAgentRef_同时设置ID和实例(t *testing.T) {
	ref := AgentRef{id: "test", agent: &mockAgent{}}
	if !ref.IsByID() {
		t.Error("IsByID 应为 true")
	}
	if !ref.IsByInstance() {
		t.Error("IsByInstance 应为 true")
	}
}

// TestWorkflowRef_ByWorkflowID 按ID构造
func TestWorkflowRef_ByWorkflowID(t *testing.T) {
	ref := ByWorkflowID("my-workflow")
	if !ref.IsByID() {
		t.Error("IsByID 应为 true")
	}
	if ref.IsByInstance() {
		t.Error("IsByInstance 应为 false")
	}
	if ref.ID() != "my-workflow" {
		t.Errorf("ID = %q, want my-workflow", ref.ID())
	}
}

// TestWorkflowRef_ByWorkflow 按实例构造
func TestWorkflowRef_ByWorkflow(t *testing.T) {
	wf := &mockWorkflow{}
	ref := ByWorkflow(wf)
	if ref.IsByID() {
		t.Error("IsByID 应为 false")
	}
	if !ref.IsByInstance() {
		t.Error("IsByInstance 应为 true")
	}
	if ref.Workflow() == nil {
		t.Error("Workflow 不应为 nil")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestSessionRef_BySessionID 按ID构造
func TestSessionRef_BySessionID(t *testing.T) {
	ref := BySessionID("sess-123")
	if !ref.IsByID() {
		t.Error("IsByID 应为 true")
	}
	if ref.IsByInstance() {
		t.Error("IsByInstance 应为 false")
	}
	if ref.ID() != "sess-123" {
		t.Errorf("ID = %q, want sess-123", ref.ID())
	}
}

// TestSessionRef_空ID 按空ID构造时IsByID为false
func TestSessionRef_空ID(t *testing.T) {
	ref := BySessionID("")
	if ref.IsByID() {
		t.Error("空ID时 IsByID 应为 false")
	}
}

// TestChildRunnerImpl_接口满足 编译时校验
func TestChildRunnerImpl_接口满足(t *testing.T) {
	var _ spawn.ChildRunner = (*ChildRunnerImpl)(nil)
}
