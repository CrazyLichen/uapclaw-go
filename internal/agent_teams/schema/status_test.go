package schema

import "testing"

// ──────────────────────────── IsValidMemberTransition 测试 ────────────────────────────

// TestIsValidMemberTransition_合法转换 测试 MemberStatus 合法状态转换
func TestIsValidMemberTransition_合法转换(t *testing.T) {
	tests := []struct {
		name    string
		current MemberStatus
		target  MemberStatus
	}{
		{"unstarted→starting", MemberStatusUnstarted, MemberStatusStarting},
		{"unstarted→ready", MemberStatusUnstarted, MemberStatusReady},
		{"unstarted→shutdown", MemberStatusUnstarted, MemberStatusShutdown},
		{"unstarted→error", MemberStatusUnstarted, MemberStatusError},
		{"starting→ready", MemberStatusStarting, MemberStatusReady},
		{"starting→unstarted", MemberStatusStarting, MemberStatusUnstarted},
		{"starting→shutdown", MemberStatusStarting, MemberStatusShutdown},
		{"starting→error", MemberStatusStarting, MemberStatusError},
		{"ready→busy", MemberStatusReady, MemberStatusBusy},
		{"ready→paused", MemberStatusReady, MemberStatusPaused},
		{"ready→stopped", MemberStatusReady, MemberStatusStopped},
		{"ready→shutdown_requested", MemberStatusReady, MemberStatusShutdownRequested},
		{"ready→shutdown", MemberStatusReady, MemberStatusShutdown},
		{"ready→ready", MemberStatusReady, MemberStatusReady},
		{"busy→ready", MemberStatusBusy, MemberStatusReady},
		{"busy→paused", MemberStatusBusy, MemberStatusPaused},
		{"busy→stopped", MemberStatusBusy, MemberStatusStopped},
		{"busy→shutdown_requested", MemberStatusBusy, MemberStatusShutdownRequested},
		{"busy→error", MemberStatusBusy, MemberStatusError},
		{"paused→ready", MemberStatusPaused, MemberStatusReady},
		{"paused→restarting", MemberStatusPaused, MemberStatusRestarting},
		{"paused→stopped", MemberStatusPaused, MemberStatusStopped},
		{"paused→shutdown_requested", MemberStatusPaused, MemberStatusShutdownRequested},
		{"paused→shutdown", MemberStatusPaused, MemberStatusShutdown},
		{"paused→error", MemberStatusPaused, MemberStatusError},
		{"stopped→ready", MemberStatusStopped, MemberStatusReady},
		{"stopped→restarting", MemberStatusStopped, MemberStatusRestarting},
		{"stopped→shutdown_requested", MemberStatusStopped, MemberStatusShutdownRequested},
		{"stopped→shutdown", MemberStatusStopped, MemberStatusShutdown},
		{"stopped→error", MemberStatusStopped, MemberStatusError},
		{"restarting→ready", MemberStatusRestarting, MemberStatusReady},
		{"restarting→stopped", MemberStatusRestarting, MemberStatusStopped},
		{"restarting→error", MemberStatusRestarting, MemberStatusError},
		{"restarting→shutdown", MemberStatusRestarting, MemberStatusShutdown},
		{"shutdown_requested→shutdown", MemberStatusShutdownRequested, MemberStatusShutdown},
		{"shutdown_requested→error", MemberStatusShutdownRequested, MemberStatusError},
		{"shutdown→restarting", MemberStatusShutdown, MemberStatusRestarting},
		{"error→restarting", MemberStatusError, MemberStatusRestarting},
		{"error→ready", MemberStatusError, MemberStatusReady},
		{"error→stopped", MemberStatusError, MemberStatusStopped},
		{"error→shutdown_requested", MemberStatusError, MemberStatusShutdownRequested},
		{"error→shutdown", MemberStatusError, MemberStatusShutdown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !IsValidMemberTransition(tt.current, tt.target) {
				t.Errorf("IsValidMemberTransition(%q, %q) = false, 期望 true", tt.current, tt.target)
			}
		})
	}
}

// TestIsValidMemberTransition_非法转换 测试 MemberStatus 非法状态转换
func TestIsValidMemberTransition_非法转换(t *testing.T) {
	tests := []struct {
		name    string
		current MemberStatus
		target  MemberStatus
	}{
		{"unstarted→busy", MemberStatusUnstarted, MemberStatusBusy},
		{"unstarted→paused", MemberStatusUnstarted, MemberStatusPaused},
		{"starting→busy", MemberStatusStarting, MemberStatusBusy},
		{"starting→paused", MemberStatusStarting, MemberStatusPaused},
		{"ready→unstarted", MemberStatusReady, MemberStatusUnstarted},
		{"ready→starting", MemberStatusReady, MemberStatusStarting},
		{"busy→unstarted", MemberStatusBusy, MemberStatusUnstarted},
		{"busy→starting", MemberStatusBusy, MemberStatusStarting},
		{"busy→shutdown", MemberStatusBusy, MemberStatusShutdown},
		{"paused→starting", MemberStatusPaused, MemberStatusStarting},
		{"paused→busy", MemberStatusPaused, MemberStatusBusy},
		{"stopped→starting", MemberStatusStopped, MemberStatusStarting},
		{"stopped→busy", MemberStatusStopped, MemberStatusBusy},
		{"restarting→starting", MemberStatusRestarting, MemberStatusStarting},
		{"restarting→busy", MemberStatusRestarting, MemberStatusBusy},
		{"shutdown_requested→ready", MemberStatusShutdownRequested, MemberStatusReady},
		{"shutdown_requested→busy", MemberStatusShutdownRequested, MemberStatusBusy},
		{"shutdown→ready", MemberStatusShutdown, MemberStatusReady},
		{"shutdown→error", MemberStatusShutdown, MemberStatusError},
		{"error→starting", MemberStatusError, MemberStatusStarting},
		{"error→busy", MemberStatusError, MemberStatusBusy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsValidMemberTransition(tt.current, tt.target) {
				t.Errorf("IsValidMemberTransition(%q, %q) = true, 期望 false", tt.current, tt.target)
			}
		})
	}
}

// TestIsValidMemberTransition_未知状态 测试未注册的 MemberStatus
func TestIsValidMemberTransition_未知状态(t *testing.T) {
	if IsValidMemberTransition(MemberStatus("unknown"), MemberStatusReady) {
		t.Error("期望未知状态转换返回 false")
	}
	if IsValidMemberTransition(MemberStatusReady, MemberStatus("unknown")) {
		t.Error("期望转换到未知状态返回 false")
	}
}

// ──────────────────────────── IsValidExecutionTransition 测试 ────────────────────────────

// TestIsValidExecutionTransition_合法转换 测试 ExecutionStatus 合法状态转换
func TestIsValidExecutionTransition_合法转换(t *testing.T) {
	tests := []struct {
		name    string
		current ExecutionStatus
		target  ExecutionStatus
	}{
		{"idle→starting", ExecutionStatusIdle, ExecutionStatusStarting},
		{"starting→running", ExecutionStatusStarting, ExecutionStatusRunning},
		{"starting→cancel_requested", ExecutionStatusStarting, ExecutionStatusCancelRequested},
		{"starting→cancelling", ExecutionStatusStarting, ExecutionStatusCancelling},
		{"starting→failed", ExecutionStatusStarting, ExecutionStatusFailed},
		{"starting→timed_out", ExecutionStatusStarting, ExecutionStatusTimedOut},
		{"running→cancel_requested", ExecutionStatusRunning, ExecutionStatusCancelRequested},
		{"running→cancelling", ExecutionStatusRunning, ExecutionStatusCancelling},
		{"running→completing", ExecutionStatusRunning, ExecutionStatusCompleting},
		{"running→failed", ExecutionStatusRunning, ExecutionStatusFailed},
		{"running→timed_out", ExecutionStatusRunning, ExecutionStatusTimedOut},
		{"cancel_requested→cancelling", ExecutionStatusCancelRequested, ExecutionStatusCancelling},
		{"cancel_requested→cancelled", ExecutionStatusCancelRequested, ExecutionStatusCancelled},
		{"cancel_requested→failed", ExecutionStatusCancelRequested, ExecutionStatusFailed},
		{"cancel_requested→timed_out", ExecutionStatusCancelRequested, ExecutionStatusTimedOut},
		{"cancelling→cancelled", ExecutionStatusCancelling, ExecutionStatusCancelled},
		{"cancelling→failed", ExecutionStatusCancelling, ExecutionStatusFailed},
		{"cancelling→timed_out", ExecutionStatusCancelling, ExecutionStatusTimedOut},
		{"cancelled→idle", ExecutionStatusCancelled, ExecutionStatusIdle},
		{"completing→completed", ExecutionStatusCompleting, ExecutionStatusCompleted},
		{"completing→failed", ExecutionStatusCompleting, ExecutionStatusFailed},
		{"completing→timed_out", ExecutionStatusCompleting, ExecutionStatusTimedOut},
		{"completed→idle", ExecutionStatusCompleted, ExecutionStatusIdle},
		{"failed→idle", ExecutionStatusFailed, ExecutionStatusIdle},
		{"timed_out→idle", ExecutionStatusTimedOut, ExecutionStatusIdle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !IsValidExecutionTransition(tt.current, tt.target) {
				t.Errorf("IsValidExecutionTransition(%q, %q) = false, 期望 true", tt.current, tt.target)
			}
		})
	}
}

// TestIsValidExecutionTransition_非法转换 测试 ExecutionStatus 非法状态转换
func TestIsValidExecutionTransition_非法转换(t *testing.T) {
	tests := []struct {
		name    string
		current ExecutionStatus
		target  ExecutionStatus
	}{
		{"idle→running", ExecutionStatusIdle, ExecutionStatusRunning},
		{"idle→completed", ExecutionStatusIdle, ExecutionStatusCompleted},
		{"idle→failed", ExecutionStatusIdle, ExecutionStatusFailed},
		{"running→idle", ExecutionStatusRunning, ExecutionStatusIdle},
		{"running→starting", ExecutionStatusRunning, ExecutionStatusStarting},
		{"completed→running", ExecutionStatusCompleted, ExecutionStatusRunning},
		{"completed→failed", ExecutionStatusCompleted, ExecutionStatusFailed},
		{"failed→running", ExecutionStatusFailed, ExecutionStatusRunning},
		{"failed→completed", ExecutionStatusFailed, ExecutionStatusCompleted},
		{"cancelled→running", ExecutionStatusCancelled, ExecutionStatusRunning},
		{"cancelled→failed", ExecutionStatusCancelled, ExecutionStatusFailed},
		{"timed_out→running", ExecutionStatusTimedOut, ExecutionStatusRunning},
		{"timed_out→completed", ExecutionStatusTimedOut, ExecutionStatusCompleted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsValidExecutionTransition(tt.current, tt.target) {
				t.Errorf("IsValidExecutionTransition(%q, %q) = true, 期望 false", tt.current, tt.target)
			}
		})
	}
}

// TestIsValidExecutionTransition_未知状态 测试未注册的 ExecutionStatus
func TestIsValidExecutionTransition_未知状态(t *testing.T) {
	if IsValidExecutionTransition(ExecutionStatus("unknown"), ExecutionStatusRunning) {
		t.Error("期望未知状态转换返回 false")
	}
	if IsValidExecutionTransition(ExecutionStatusIdle, ExecutionStatus("unknown")) {
		t.Error("期望转换到未知状态返回 false")
	}
}

// ──────────────────────────── IsValidTaskTransition 测试 ────────────────────────────

// TestIsValidTaskTransition_合法转换 测试 TaskStatus 合法状态转换
func TestIsValidTaskTransition_合法转换(t *testing.T) {
	tests := []struct {
		name    string
		current TaskStatus
		target  TaskStatus
	}{
		{"pending→claimed", TaskStatusPending, TaskStatusClaimed},
		{"pending→blocked", TaskStatusPending, TaskStatusBlocked},
		{"pending→cancelled", TaskStatusPending, TaskStatusCancelled},
		{"claimed→plan_approved", TaskStatusClaimed, TaskStatusPlanApproved},
		{"claimed→completed", TaskStatusClaimed, TaskStatusCompleted},
		{"claimed→cancelled", TaskStatusClaimed, TaskStatusCancelled},
		{"claimed→blocked", TaskStatusClaimed, TaskStatusBlocked},
		{"claimed→pending", TaskStatusClaimed, TaskStatusPending},
		{"plan_approved→completed", TaskStatusPlanApproved, TaskStatusCompleted},
		{"plan_approved→pending", TaskStatusPlanApproved, TaskStatusPending},
		{"plan_approved→cancelled", TaskStatusPlanApproved, TaskStatusCancelled},
		{"blocked→pending", TaskStatusBlocked, TaskStatusPending},
		{"blocked→cancelled", TaskStatusBlocked, TaskStatusCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !IsValidTaskTransition(tt.current, tt.target) {
				t.Errorf("IsValidTaskTransition(%q, %q) = false, 期望 true", tt.current, tt.target)
			}
		})
	}
}

// TestIsValidTaskTransition_非法转换 测试 TaskStatus 非法状态转换
func TestIsValidTaskTransition_非法转换(t *testing.T) {
	tests := []struct {
		name    string
		current TaskStatus
		target  TaskStatus
	}{
		// 终态不可转换
		{"completed→claimed", TaskStatusCompleted, TaskStatusClaimed},
		{"completed→pending", TaskStatusCompleted, TaskStatusPending},
		{"completed→blocked", TaskStatusCompleted, TaskStatusBlocked},
		{"cancelled→claimed", TaskStatusCancelled, TaskStatusClaimed},
		{"cancelled→pending", TaskStatusCancelled, TaskStatusPending},
		{"cancelled→blocked", TaskStatusCancelled, TaskStatusBlocked},
		// 非法跳转
		{"pending→completed", TaskStatusPending, TaskStatusCompleted},
		{"pending→plan_approved", TaskStatusPending, TaskStatusPlanApproved},
		{"blocked→claimed", TaskStatusBlocked, TaskStatusClaimed},
		{"blocked→plan_approved", TaskStatusBlocked, TaskStatusPlanApproved},
		{"plan_approved→claimed", TaskStatusPlanApproved, TaskStatusClaimed},
		{"plan_approved→blocked", TaskStatusPlanApproved, TaskStatusBlocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsValidTaskTransition(tt.current, tt.target) {
				t.Errorf("IsValidTaskTransition(%q, %q) = true, 期望 false", tt.current, tt.target)
			}
		})
	}
}

// TestIsValidTaskTransition_未知状态 测试未注册的 TaskStatus
func TestIsValidTaskTransition_未知状态(t *testing.T) {
	if IsValidTaskTransition(TaskStatus("unknown"), TaskStatusClaimed) {
		t.Error("期望未知状态转换返回 false")
	}
	if IsValidTaskTransition(TaskStatusPending, TaskStatus("unknown")) {
		t.Error("期望转换到未知状态返回 false")
	}
}
