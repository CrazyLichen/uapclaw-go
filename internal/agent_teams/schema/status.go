package schema

import "github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// MemberStatus 成员状态枚举。
// 对齐 Python: MemberStatus (openjiuwen/agent_teams/schema/status.py)
// 类型底层为 string，值来自 fsm 包常量，提供类型安全的 API。
type MemberStatus string

// ExecutionStatus 任务执行状态枚举。
// 对齐 Python: ExecutionStatus
type ExecutionStatus string

// MemberMode 成员与任务交互模式。
// 对齐 Python: MemberMode
type MemberMode string

// TaskStatus 团队任务状态枚举。
// 对齐 Python: TaskStatus
type TaskStatus string

const (
	// MemberStatusUnstarted 成员已创建但尚未启动
	MemberStatusUnstarted MemberStatus = fsm.MemberStatusUnstarted
	// MemberStatusStarting 成员进程正在启动（过渡态，CAS 防重复 spawn）
	MemberStatusStarting MemberStatus = fsm.MemberStatusStarting
	// MemberStatusReady 成员已就绪，可接收任务
	MemberStatusReady MemberStatus = fsm.MemberStatusReady
	// MemberStatusBusy 成员正在处理任务
	MemberStatusBusy MemberStatus = fsm.MemberStatusBusy
	// MemberStatusPaused 成员协程已在轮次结束时退出（持久团队空闲态）
	MemberStatusPaused MemberStatus = fsm.MemberStatusPaused
	// MemberStatusStopped 成员运行时已被外部 stop_coordination 拆卸（非解散性拆卸）
	MemberStatusStopped MemberStatus = fsm.MemberStatusStopped
	// MemberStatusRestarting 成员进程正在故障后重启
	MemberStatusRestarting MemberStatus = fsm.MemberStatusRestarting
	// MemberStatusShutdownRequested 成员已收到关闭请求
	MemberStatusShutdownRequested MemberStatus = fsm.MemberStatusShutdownRequested
	// MemberStatusShutdown 成员已关闭
	MemberStatusShutdown MemberStatus = fsm.MemberStatusShutdown
	// MemberStatusError 成员处于错误状态
	MemberStatusError MemberStatus = fsm.MemberStatusError
)

const (
	// ExecutionStatusIdle 未执行任何任务
	ExecutionStatusIdle ExecutionStatus = fsm.ExecutionStatusIdle
	// ExecutionStatusStarting 任务执行正在启动
	ExecutionStatusStarting ExecutionStatus = fsm.ExecutionStatusStarting
	// ExecutionStatusRunning 任务正在运行
	ExecutionStatusRunning ExecutionStatus = fsm.ExecutionStatusRunning
	// ExecutionStatusCancelRequested 已请求取消
	ExecutionStatusCancelRequested ExecutionStatus = fsm.ExecutionStatusCancelRequested
	// ExecutionStatusCancelling 正在取消
	ExecutionStatusCancelling ExecutionStatus = fsm.ExecutionStatusCancelling
	// ExecutionStatusCancelled 已取消
	ExecutionStatusCancelled ExecutionStatus = fsm.ExecutionStatusCancelled
	// ExecutionStatusCompleting 正在完成
	ExecutionStatusCompleting ExecutionStatus = fsm.ExecutionStatusCompleting
	// ExecutionStatusCompleted 已完成
	ExecutionStatusCompleted ExecutionStatus = fsm.ExecutionStatusCompleted
	// ExecutionStatusFailed 已失败
	ExecutionStatusFailed ExecutionStatus = fsm.ExecutionStatusFailed
	// ExecutionStatusTimedOut 已超时
	ExecutionStatusTimedOut ExecutionStatus = fsm.ExecutionStatusTimedOut
)

const (
	// MemberModeBuildMode 成员可直接认领并完成任务（默认）
	MemberModeBuildMode MemberMode = "build_mode"
	// MemberModePlanMode 成员需 Leader 审批后才能完成任务
	MemberModePlanMode MemberMode = "plan_mode"
)

const (
	// TaskStatusPending 任务等待被认领
	TaskStatusPending TaskStatus = fsm.TaskStatusPending
	// TaskStatusClaimed 任务已被成员认领
	TaskStatusClaimed TaskStatus = fsm.TaskStatusClaimed
	// TaskStatusPlanApproved 任务计划已批准（仅 PLAN_MODE 成员）
	TaskStatusPlanApproved TaskStatus = fsm.TaskStatusPlanApproved
	// TaskStatusCompleted 任务已完成
	TaskStatusCompleted TaskStatus = fsm.TaskStatusCompleted
	// TaskStatusCancelled 任务已取消
	TaskStatusCancelled TaskStatus = fsm.TaskStatusCancelled
	// TaskStatusBlocked 任务因依赖被阻塞
	TaskStatusBlocked TaskStatus = fsm.TaskStatusBlocked
)

// ──────────────────────────── 全局变量 ────────────────────────────

// MemberTransitions MemberStatus 状态转换表。
// 对齐 Python: MEMBER_TRANSITIONS
// 底层数据来自 fsm 包，通过类型化 wrapper 提供类型安全的 API。
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
// 委托 fsm 包实现，提供类型化 wrapper。
func IsValidMemberTransition(current, target MemberStatus) bool {
	return fsm.IsValidMemberTransition(string(current), string(target))
}

// IsValidExecutionTransition 检查 ExecutionStatus 状态转换是否合法。
// 委托 fsm 包实现，提供类型化 wrapper。
func IsValidExecutionTransition(current, target ExecutionStatus) bool {
	return fsm.IsValidExecutionTransition(string(current), string(target))
}

// IsValidTaskTransition 检查 TaskStatus 状态转换是否合法。
// 委托 fsm 包实现，提供类型化 wrapper。
func IsValidTaskTransition(current, target TaskStatus) bool {
	return fsm.IsValidTaskTransition(string(current), string(target))
}
