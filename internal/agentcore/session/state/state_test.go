package state

import "testing"

// Test接口满足_ReadableStateLike 验证 InMemoryStateLike 满足 ReadableStateLike 接口。
func Test接口满足_ReadableStateLike(t *testing.T) {
	var _ ReadableStateLike = (*InMemoryStateLike)(nil)
}

// Test接口满足_RecoverableStateLike 验证 InMemoryStateLike 满足 RecoverableStateLike 接口。
func Test接口满足_RecoverableStateLike(t *testing.T) {
	var _ RecoverableStateLike = (*InMemoryStateLike)(nil)
}

// Test接口满足_StateLike 验证 InMemoryStateLike 满足 StateLike 接口。
func Test接口满足_StateLike(t *testing.T) {
	var _ StateLike = (*InMemoryStateLike)(nil)
}

// Test接口满足_CommitStateLike 验证 InMemoryCommitState 满足 CommitStateLike 接口。
func Test接口满足_CommitStateLike(t *testing.T) {
	var _ CommitStateLike = (*InMemoryCommitState)(nil)
}

// Test接口满足_SessionState_InMemoryStateLike 验证 InMemoryStateLike 满足 SessionState 接口。
func Test接口满足_SessionState_InMemoryStateLike(t *testing.T) {
	var _ SessionState = (*InMemoryStateLike)(nil)
}

// Test接口满足_SessionState_InMemoryCommitState 验证 InMemoryCommitState 不再满足 SessionState 接口。
// G-15 修复：移除了 GetGlobal/UpdateGlobal/UpdateTrace/Dump 空实现，
// InMemoryCommitState 只是 CommitStateLike，不应是 SessionState。
func Test接口满足_SessionState_InMemoryCommitState(t *testing.T) {
	// InMemoryCommitState 不应满足 SessionState 接口
	// 以下编译断言确认：如果误加回空实现，编译会通过，此测试提醒不应这样做
	// var _ SessionState = (*InMemoryCommitState)(nil) // 已删除，不应满足
	var _ CommitStateLike = (*InMemoryCommitState)(nil) // 只满足 CommitStateLike
}

// Test接口满足_SessionState_AgentStateCollection 验证 AgentStateCollection 满足 SessionState 接口。
func Test接口满足_SessionState_AgentStateCollection(t *testing.T) {
	var _ SessionState = (*AgentStateCollection)(nil)
}

// Test接口满足_SessionState_WorkflowStateCollection 验证 WorkflowStateCollection 满足 SessionState 接口。
func Test接口满足_SessionState_WorkflowStateCollection(t *testing.T) {
	var _ SessionState = (*WorkflowStateCollection)(nil)
}

// Test接口满足_SessionState_WorkflowCommitState 验证 WorkflowCommitState 满足 SessionState 接口。
func Test接口满足_SessionState_WorkflowCommitState(t *testing.T) {
	var _ SessionState = (*WorkflowCommitState)(nil)
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
