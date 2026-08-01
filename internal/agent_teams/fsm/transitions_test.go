package fsm

import "testing"

// TestIsValidMemberTransition_合法转换 合法的状态转换返回 true。
func TestIsValidMemberTransition_合法转换(t *testing.T) {
	tests := []struct {
		current string
		target  string
	}{
		{MemberStatusUnstarted, MemberStatusStarting},
		{MemberStatusReady, MemberStatusBusy},
		{MemberStatusBusy, MemberStatusReady},
		{MemberStatusPaused, MemberStatusRestarting},
		{MemberStatusError, MemberStatusReady},
		{MemberStatusShutdown, MemberStatusRestarting},
	}
	for _, tt := range tests {
		if !IsValidMemberTransition(tt.current, tt.target) {
			t.Errorf("IsValidMemberTransition(%q, %q) 应返回 true", tt.current, tt.target)
		}
	}
}

// TestIsValidMemberTransition_非法转换 非法的状态转换返回 false。
func TestIsValidMemberTransition_非法转换(t *testing.T) {
	tests := []struct {
		current string
		target  string
	}{
		{MemberStatusUnstarted, MemberStatusBusy},
		{MemberStatusBusy, MemberStatusUnstarted},
		{MemberStatusShutdownRequested, MemberStatusReady},
		{"invalid_status", MemberStatusReady},
	}
	for _, tt := range tests {
		if IsValidMemberTransition(tt.current, tt.target) {
			t.Errorf("IsValidMemberTransition(%q, %q) 应返回 false", tt.current, tt.target)
		}
	}
}

// TestIsValidExecutionTransition_合法转换 合法的执行状态转换返回 true。
func TestIsValidExecutionTransition_合法转换(t *testing.T) {
	tests := []struct {
		current string
		target  string
	}{
		{ExecutionStatusIdle, ExecutionStatusStarting},
		{ExecutionStatusStarting, ExecutionStatusRunning},
		{ExecutionStatusRunning, ExecutionStatusCompleting},
		{ExecutionStatusCancelled, ExecutionStatusIdle},
	}
	for _, tt := range tests {
		if !IsValidExecutionTransition(tt.current, tt.target) {
			t.Errorf("IsValidExecutionTransition(%q, %q) 应返回 true", tt.current, tt.target)
		}
	}
}

// TestIsValidExecutionTransition_非法转换 非法的执行状态转换返回 false。
func TestIsValidExecutionTransition_非法转换(t *testing.T) {
	tests := []struct {
		current string
		target  string
	}{
		{ExecutionStatusIdle, ExecutionStatusRunning},
		{ExecutionStatusRunning, ExecutionStatusIdle},
		{"invalid", ExecutionStatusStarting},
	}
	for _, tt := range tests {
		if IsValidExecutionTransition(tt.current, tt.target) {
			t.Errorf("IsValidExecutionTransition(%q, %q) 应返回 false", tt.current, tt.target)
		}
	}
}

// TestIsValidTaskTransition_合法转换 合法的任务状态转换返回 true。
func TestIsValidTaskTransition_合法转换(t *testing.T) {
	tests := []struct {
		current string
		target  string
	}{
		{TaskStatusPending, TaskStatusClaimed},
		{TaskStatusClaimed, TaskStatusCompleted},
		{TaskStatusBlocked, TaskStatusPending},
	}
	for _, tt := range tests {
		if !IsValidTaskTransition(tt.current, tt.target) {
			t.Errorf("IsValidTaskTransition(%q, %q) 应返回 true", tt.current, tt.target)
		}
	}
}

// TestIsValidTaskTransition_非法转换 终态不能再转换。
func TestIsValidTaskTransition_非法转换(t *testing.T) {
	if IsValidTaskTransition(TaskStatusCompleted, TaskStatusPending) {
		t.Error("completed → pending 应非法")
	}
	if IsValidTaskTransition(TaskStatusCancelled, TaskStatusClaimed) {
		t.Error("cancelled → claimed 应非法")
	}
}

// TestMemberTransitions_完整性 每个状态都应在转换表中。
func TestMemberTransitions_完整性(t *testing.T) {
	expectedStates := []string{
		MemberStatusUnstarted, MemberStatusStarting, MemberStatusReady,
		MemberStatusBusy, MemberStatusPaused, MemberStatusStopped,
		MemberStatusRestarting, MemberStatusShutdownRequested,
		MemberStatusShutdown, MemberStatusError,
	}
	for _, state := range expectedStates {
		if _, ok := MemberTransitions[state]; !ok {
			t.Errorf("状态 %q 不在 MemberTransitions 中", state)
		}
	}
}

// TestExecutionTransitions_完整性 每个状态都应在转换表中。
func TestExecutionTransitions_完整性(t *testing.T) {
	expectedStates := []string{
		ExecutionStatusIdle, ExecutionStatusStarting, ExecutionStatusRunning,
		ExecutionStatusCancelRequested, ExecutionStatusCancelling,
		ExecutionStatusCancelled, ExecutionStatusCompleting,
		ExecutionStatusCompleted, ExecutionStatusFailed, ExecutionStatusTimedOut,
	}
	for _, state := range expectedStates {
		if _, ok := ExecutionTransitions[state]; !ok {
			t.Errorf("状态 %q 不在 ExecutionTransitions 中", state)
		}
	}
}

// TestTaskTransitions_完整性 每个状态都应在转换表中。
func TestTaskTransitions_完整性(t *testing.T) {
	expectedStates := []string{
		TaskStatusPending, TaskStatusClaimed, TaskStatusPlanApproved,
		TaskStatusCompleted, TaskStatusCancelled, TaskStatusBlocked,
	}
	for _, state := range expectedStates {
		if _, ok := TaskTransitions[state]; !ok {
			t.Errorf("状态 %q 不在 TaskTransitions 中", state)
		}
	}
}

// TestMemberSettledStatuses_完整性 settled 状态集合应包含 4 个状态。
func TestMemberSettledStatuses_完整性(t *testing.T) {
	expected := []string{MemberStatusReady, MemberStatusPaused, MemberStatusStopped, MemberStatusShutdown}
	if len(MemberSettledStatuses) != len(expected) {
		t.Errorf("MemberSettledStatuses 数量: got %d, want %d", len(MemberSettledStatuses), len(expected))
	}
	for _, s := range expected {
		if !MemberSettledStatuses[s] {
			t.Errorf("状态 %q 应在 MemberSettledStatuses 中", s)
		}
	}
}
