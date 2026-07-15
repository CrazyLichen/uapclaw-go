package schema

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// MemberStatus 成员状态枚举。
// 对齐 Python: MemberStatus (openjiuwen/agent_teams/schema/status.py)
type MemberStatus string

const (
	// MemberStatusUnstarted 成员已创建但尚未启动
	MemberStatusUnstarted MemberStatus = "unstarted"
	// MemberStatusStarting 成员进程正在启动（过渡态，CAS 防重复 spawn）
	MemberStatusStarting MemberStatus = "starting"
	// MemberStatusReady 成员已就绪，可接收任务
	MemberStatusReady MemberStatus = "ready"
	// MemberStatusBusy 成员正在处理任务
	MemberStatusBusy MemberStatus = "busy"
	// MemberStatusPaused 成员协程已在轮次结束时退出（持久团队空闲态）
	MemberStatusPaused MemberStatus = "paused"
	// MemberStatusStopped 成员运行时已被外部 stop_coordination 拆卸（非解散性拆卸）
	MemberStatusStopped MemberStatus = "stopped"
	// MemberStatusRestarting 成员进程正在故障后重启
	MemberStatusRestarting MemberStatus = "restarting"
	// MemberStatusShutdownRequested 成员已收到关闭请求
	MemberStatusShutdownRequested MemberStatus = "shutdown_requested"
	// MemberStatusShutdown 成员已关闭
	MemberStatusShutdown MemberStatus = "shut_down"
	// MemberStatusError 成员处于错误状态
	MemberStatusError MemberStatus = "error"
)

// ExecutionStatus 任务执行状态枚举。
// 对齐 Python: ExecutionStatus
type ExecutionStatus string

const (
	// ExecutionStatusIdle 未执行任何任务
	ExecutionStatusIdle ExecutionStatus = "idle"
	// ExecutionStatusStarting 任务执行正在启动
	ExecutionStatusStarting ExecutionStatus = "starting"
	// ExecutionStatusRunning 任务正在运行
	ExecutionStatusRunning ExecutionStatus = "running"
	// ExecutionStatusCancelRequested 已请求取消
	ExecutionStatusCancelRequested ExecutionStatus = "cancel_requested"
	// ExecutionStatusCancelling 正在取消
	ExecutionStatusCancelling ExecutionStatus = "cancelling"
	// ExecutionStatusCancelled 已取消
	ExecutionStatusCancelled ExecutionStatus = "cancelled"
	// ExecutionStatusCompleting 正在完成
	ExecutionStatusCompleting ExecutionStatus = "completing"
	// ExecutionStatusCompleted 已完成
	ExecutionStatusCompleted ExecutionStatus = "completed"
	// ExecutionStatusFailed 已失败
	ExecutionStatusFailed ExecutionStatus = "failed"
	// ExecutionStatusTimedOut 已超时
	ExecutionStatusTimedOut ExecutionStatus = "timed_out"
)

// MemberMode 成员与任务交互模式。
// 对齐 Python: MemberMode
type MemberMode string

const (
	// MemberModeBuildMode 成员可直接认领并完成任务（默认）
	MemberModeBuildMode MemberMode = "build_mode"
	// MemberModePlanMode 成员需 Leader 审批后才能完成任务
	MemberModePlanMode MemberMode = "plan_mode"
)

// TaskStatus 团队任务状态枚举。
// 对齐 Python: TaskStatus
type TaskStatus string

const (
	// TaskStatusPending 任务等待被认领
	TaskStatusPending TaskStatus = "pending"
	// TaskStatusClaimed 任务已被成员认领
	TaskStatusClaimed TaskStatus = "claimed"
	// TaskStatusPlanApproved 任务计划已批准（仅 PLAN_MODE 成员）
	TaskStatusPlanApproved TaskStatus = "plan_approved"
	// TaskStatusCompleted 任务已完成
	TaskStatusCompleted TaskStatus = "completed"
	// TaskStatusCancelled 任务已取消
	TaskStatusCancelled TaskStatus = "cancelled"
	// TaskStatusBlocked 任务因依赖被阻塞
	TaskStatusBlocked TaskStatus = "blocked"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// MemberTransitions MemberStatus 状态转换表。
// 对齐 Python: MEMBER_TRANSITIONS
var MemberTransitions = map[MemberStatus][]MemberStatus{
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
var MemberSettledStatuses = map[MemberStatus]bool{
	MemberStatusReady:    true,
	MemberStatusPaused:   true,
	MemberStatusStopped:  true,
	MemberStatusShutdown: true,
}

// ExecutionTransitions ExecutionStatus 状态转换表。
// 对齐 Python: EXECUTION_TRANSITIONS
var ExecutionTransitions = map[ExecutionStatus][]ExecutionStatus{
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
var TaskTransitions = map[TaskStatus][]TaskStatus{
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
func IsValidMemberTransition(current, target MemberStatus) bool {
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

// ──────────────────────────── 非导出函数 ────────────────────────────
// IsValidExecutionTransition 检查 ExecutionStatus 状态转换是否合法。
func IsValidExecutionTransition(current, target ExecutionStatus) bool {
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

// ──────────────────────────── 非导出函数 ────────────────────────────
// IsValidTaskTransition 检查 TaskStatus 状态转换是否合法。
func IsValidTaskTransition(current, target TaskStatus) bool {
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

// ──────────────────────────── 非导出函数 ────────────────────────────