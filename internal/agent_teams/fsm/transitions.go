package fsm

// ──────────────────────────── 常量 ────────────────────────────

// MemberStatus 常量 — 对齐 Python: MemberStatus (openjiuwen/agent_teams/schema/status.py)
// 使用 string 类型，使本包成为无依赖的纯数据包，schema/database 都可 import。
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

// TaskStatus 常量 — 对齐 Python: TaskStatus
const (
	TaskStatusPending       = "pending"
	TaskStatusClaimed       = "claimed"
	TaskStatusPlanApproved  = "plan_approved"
	TaskStatusCompleted     = "completed"
	TaskStatusCancelled     = "cancelled"
	TaskStatusBlocked       = "blocked"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// MemberTransitions MemberStatus 状态转换表。
// 对齐 Python: MEMBER_TRANSITIONS (openjiuwen/agent_teams/schema/status.py)
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

// MemberSettledStatuses 成员可以处于空闲时的状态集合（团队完成检查使用）。
// 对齐 Python: MEMBER_SETTLED_STATUSES
var MemberSettledStatuses = map[string]bool{
	MemberStatusReady:    true,
	MemberStatusPaused:   true,
	MemberStatusStopped:  true,
	MemberStatusShutdown: true,
}

// ExecutionTransitions ExecutionStatus 状态转换表。
// 对齐 Python: EXECUTION_TRANSITIONS
var ExecutionTransitions = map[string][]string{
	ExecutionStatusIdle: {ExecutionStatusStarting},
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

// TaskTransitions TaskStatus 状态转换表。
// 对齐 Python: TASK_TRANSITIONS
var TaskTransitions = map[string][]string{
	TaskStatusPending:      {TaskStatusClaimed, TaskStatusBlocked, TaskStatusCancelled},
	TaskStatusClaimed:      {TaskStatusPlanApproved, TaskStatusCompleted, TaskStatusCancelled, TaskStatusBlocked, TaskStatusPending},
	TaskStatusPlanApproved: {TaskStatusCompleted, TaskStatusPending, TaskStatusCancelled},
	TaskStatusBlocked:      {TaskStatusPending, TaskStatusCancelled},
	TaskStatusCompleted:    {},
	TaskStatusCancelled:    {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsValidMemberTransition 检查 MemberStatus 状态转换是否合法。
// 对齐 Python: is_valid_transition(current_status, new_status, MEMBER_TRANSITIONS)
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
// 对齐 Python: is_valid_transition(current, new_status, EXECUTION_TRANSITIONS)
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

// IsValidTaskTransition 检查 TaskStatus 状态转换是否合法。
// 对齐 Python: is_valid_transition(current, new_status, TASK_TRANSITIONS)
func IsValidTaskTransition(current, target string) bool {
	allowed, ok := TaskTransitions[current]
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
