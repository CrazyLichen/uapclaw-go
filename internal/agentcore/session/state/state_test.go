package state

import "testing"

// Test接口满足_ReadableState 验证 InMemoryState 满足 ReadableState 接口。
func Test接口满足_ReadableState(t *testing.T) {
	var _ ReadableState = (*InMemoryState)(nil)
}

// Test接口满足_RecoverableState 验证 InMemoryState 满足 RecoverableState 接口。
func Test接口满足_RecoverableState(t *testing.T) {
	var _ RecoverableState = (*InMemoryState)(nil)
}

// Test接口满足_State 验证 InMemoryState 满足 State 接口。
func Test接口满足_State(t *testing.T) {
	var _ State = (*InMemoryState)(nil)
}

// Test接口满足_CommitState 验证 InMemoryCommitState 满足 CommitState 接口。
func Test接口满足_CommitState(t *testing.T) {
	var _ CommitState = (*InMemoryCommitState)(nil)
}

// Test常量值 验证常量值与 Python 一致。
func Test常量值(t *testing.T) {
	if DefaultNodeID != "default" {
		t.Errorf("DefaultNodeID = %q, 期望 %q", DefaultNodeID, "default")
	}
	if DefaultWorkflowID != "workflow" {
		t.Errorf("DefaultWorkflowID = %q, 期望 %q", DefaultWorkflowID, "workflow")
	}
	if IOStateKey != "io_state" {
		t.Errorf("IOStateKey = %q, 期望 %q", IOStateKey, "io_state")
	}
	if GlobalStateKey != "global_state" {
		t.Errorf("GlobalStateKey = %q, 期望 %q", GlobalStateKey, "global_state")
	}
	if CompStateKey != "comp_state" {
		t.Errorf("CompStateKey = %q, 期望 %q", CompStateKey, "comp_state")
	}
	if WorkflowStateKey != "workflow_state" {
		t.Errorf("WorkflowStateKey = %q, 期望 %q", WorkflowStateKey, "workflow_state")
	}
	if AgentStateKey != "agent_state" {
		t.Errorf("AgentStateKey = %q, 期望 %q", AgentStateKey, "agent_state")
	}
	if IOStateUpdatesKey != "io_state_updates" {
		t.Errorf("IOStateUpdatesKey = %q, 期望 %q", IOStateUpdatesKey, "io_state_updates")
	}
	if GlobalStateUpdatesKey != "global_state_updates" {
		t.Errorf("GlobalStateUpdatesKey = %q, 期望 %q", GlobalStateUpdatesKey, "global_state_updates")
	}
	if CompStateUpdatesKey != "comp_state_updates" {
		t.Errorf("CompStateUpdatesKey = %q, 期望 %q", CompStateUpdatesKey, "comp_state_updates")
	}
	if WorkflowStateUpdatesKey != "workflow_state_updates" {
		t.Errorf("WorkflowStateUpdatesKey = %q, 期望 %q", WorkflowStateUpdatesKey, "workflow_state_updates")
	}
}
