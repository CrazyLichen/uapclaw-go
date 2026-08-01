package database

// ──────────────────────────── 常量 ────────────────────────────

// MemberStatus 常量 — 对齐 Python: MemberStatus (openjiuwen/agent_teams/schema/status.py)
// 本包独立定义，避免 database→schema 循环依赖。值与 schema 包保持一致。
const (
	MemberStatusUnstarted         = "unstarted"
	MemberStatusStarting          = "starting"
	MemberStatusReady             = "ready"
	MemberStatusBusy              = "busy"
	MemberStatusPaused            = "paused"
	MemberStatusStopped           = "stopped"
	MemberStatusRestarting        = "restarting"
	MemberStatusShutdownRequested = "shutdown_requested"
	MemberStatusShutdown          = "shut_down"
	MemberStatusError             = "error"
)

// ExecutionStatus 常量 — 对齐 Python: ExecutionStatus
// 本包独立定义，避免 database→schema 循环依赖。值与 schema 包保持一致。
const (
	ExecutionStatusIdle            = "idle"
	ExecutionStatusStarting        = "starting"
	ExecutionStatusRunning         = "running"
	ExecutionStatusCancelRequested = "cancel_requested"
	ExecutionStatusCancelling      = "cancelling"
	ExecutionStatusCancelled       = "cancelled"
	ExecutionStatusCompleting      = "completing"
	ExecutionStatusCompleted       = "completed"
	ExecutionStatusFailed          = "failed"
	ExecutionStatusTimedOut        = "timed_out"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// MemberTransitions MemberStatus 状态转换表。
// 对齐 Python: MEMBER_TRANSITIONS (openjiuwen/agent_teams/schema/status.py)
// 本包独立定义，避免 database→schema 循环依赖。值与 schema.MemberTransitions 保持一致。
var MemberTransitions = map[string][]string{
	MemberStatusUnstarted: {
		MemberStatusStarting, MemberStatusReady, MemberStatusShutdown, MemberStatusError,
	},
	MemberStatusStarting: {
		MemberStatusReady, MemberStatusUnstarted, MemberStatusShutdown, MemberStatusError,
	},
	MemberStatusReady: {
		MemberStatusReady, MemberStatusBusy, MemberStatusPaused, MemberStatusStopped,
		MemberStatusShutdownRequested, MemberStatusShutdown, MemberStatusError,
	},
	MemberStatusBusy: {
		MemberStatusReady, MemberStatusPaused, MemberStatusStopped,
		MemberStatusShutdownRequested, MemberStatusError,
	},
	MemberStatusPaused: {
		MemberStatusReady, MemberStatusRestarting, MemberStatusStopped,
		MemberStatusShutdownRequested, MemberStatusShutdown, MemberStatusError,
	},
	MemberStatusStopped: {
		MemberStatusReady, MemberStatusRestarting,
		MemberStatusShutdownRequested, MemberStatusShutdown, MemberStatusError,
	},
	MemberStatusRestarting: {
		MemberStatusReady, MemberStatusStopped, MemberStatusError, MemberStatusShutdown,
	},
	MemberStatusShutdownRequested: {
		MemberStatusShutdown, MemberStatusError,
	},
	MemberStatusShutdown: {
		MemberStatusRestarting,
	},
	MemberStatusError: {
		MemberStatusRestarting, MemberStatusReady, MemberStatusStopped,
		MemberStatusShutdownRequested, MemberStatusShutdown,
	},
}

// ExecutionTransitions ExecutionStatus 状态转换表。
// 对齐 Python: EXECUTION_TRANSITIONS
var ExecutionTransitions = map[string][]string{
	ExecutionStatusIdle: {
		ExecutionStatusStarting,
	},
	ExecutionStatusStarting: {
		ExecutionStatusRunning, ExecutionStatusCancelRequested,
		ExecutionStatusCancelling, ExecutionStatusFailed, ExecutionStatusTimedOut,
	},
	ExecutionStatusRunning: {
		ExecutionStatusCancelRequested, ExecutionStatusCancelling,
		ExecutionStatusCompleting, ExecutionStatusFailed, ExecutionStatusTimedOut,
	},
	ExecutionStatusCancelRequested: {
		ExecutionStatusCancelling, ExecutionStatusCancelled,
		ExecutionStatusFailed, ExecutionStatusTimedOut,
	},
	ExecutionStatusCancelling: {
		ExecutionStatusCancelled, ExecutionStatusFailed, ExecutionStatusTimedOut,
	},
	ExecutionStatusCancelled:  {ExecutionStatusIdle},
	ExecutionStatusCompleting: {ExecutionStatusCompleted, ExecutionStatusFailed, ExecutionStatusTimedOut},
	ExecutionStatusCompleted:  {ExecutionStatusIdle},
	ExecutionStatusFailed:     {ExecutionStatusIdle},
	ExecutionStatusTimedOut:   {ExecutionStatusIdle},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsValidMemberTransition 检查 MemberStatus 状态转换是否合法。
// 对齐 Python: is_valid_transition(current, target, MEMBER_TRANSITIONS)
func IsValidMemberTransition(current, target string) bool {
	allowed, ok := MemberTransitions[current]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == target {
			return true
		}
	}
	return false
}

// IsValidExecutionTransition 检查 ExecutionStatus 状态转换是否合法。
// 对齐 Python: is_valid_transition(current, target, EXECUTION_TRANSITIONS)
func IsValidExecutionTransition(current, target string) bool {
	allowed, ok := ExecutionTransitions[current]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == target {
			return true
		}
	}
	return false
}
