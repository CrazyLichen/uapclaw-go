package schema

import "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/fsm"

// ──────────────────────────── 枚举 ────────────────────────────

// MemberStatus 成员状态枚举。
// Python: MemberStatus (openjiuwen/agent_teams/schema/status.py)
// 类型底层为 string，值来自 fsm 包常量，提供类型安全的 API。
type MemberStatus string

// ExecutionStatus 任务执行状态枚举。
// Python: ExecutionStatus
type ExecutionStatus string

// MemberMode 成员与任务交互模式。
// Python: MemberMode
type MemberMode string

// TaskStatus 团队任务状态枚举。
// Python: TaskStatus
type TaskStatus string

// ──────────────────────────── 常量 ────────────────────────────

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
	// MemberStatusDormant 退役休眠态（options.lifetime=="dormant"，F_118）。
	// Python: MemberStatus.DORMANT（schema/status.py L55）
	MemberStatusDormant MemberStatus = fsm.MemberStatusDormant
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
	// TaskStatusPending 任务等待被认领（autonomous）或已分配未开始（scheduled）
	TaskStatusPending TaskStatus = fsm.TaskStatusPending
	// TaskStatusBlocked 任务因依赖被阻塞
	TaskStatusBlocked TaskStatus = fsm.TaskStatusBlocked
	// TaskStatusPlanning plan 闸门：成员准备计划等待 leader 审批（可选，PLAN_MODE）
	TaskStatusPlanning TaskStatus = fsm.TaskStatusPlanning
	// TaskStatusInProgress 执行中（统一旧模型 CLAIMED/STARTED/PLAN_APPROVED）
	TaskStatusInProgress TaskStatus = fsm.TaskStatusInProgress
	// TaskStatusInReview verify 闸门：成员已提交结果，reviewer 验收中（可选，有 reviewer 时）
	TaskStatusInReview TaskStatus = fsm.TaskStatusInReview
	// TaskStatusCompleted 任务已完成（终态）
	TaskStatusCompleted TaskStatus = fsm.TaskStatusCompleted
	// TaskStatusCancelled 任务已取消（终态）
	TaskStatusCancelled TaskStatus = fsm.TaskStatusCancelled
)

// ──────────────────────────── 全局变量 ────────────────────────────

// MemberTransitions MemberStatus 状态转换表。
// Python: MEMBER_TRANSITIONS
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
// Python: MEMBER_SETTLED_STATUSES
var MemberSettledStatuses = map[MemberStatus]bool{
	MemberStatusReady:    true,
	MemberStatusPaused:   true,
	MemberStatusStopped:  true,
	MemberStatusShutdown: true,
}

// ExecutionTransitions ExecutionStatus 状态转换表。
// Python: EXECUTION_TRANSITIONS
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
// Python: TASK_TRANSITIONS
var TaskTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusPending:    {TaskStatusPlanning, TaskStatusInProgress, TaskStatusBlocked, TaskStatusCancelled},
	TaskStatusBlocked:    {TaskStatusPending, TaskStatusCancelled},
	TaskStatusPlanning:   {TaskStatusPlanning, TaskStatusInProgress, TaskStatusPending, TaskStatusBlocked, TaskStatusCancelled},
	TaskStatusInProgress: {TaskStatusInReview, TaskStatusCompleted, TaskStatusPending, TaskStatusBlocked, TaskStatusCancelled},
	TaskStatusInReview:   {TaskStatusCompleted, TaskStatusInProgress, TaskStatusPending, TaskStatusCancelled},
	TaskStatusCompleted:  {},
	TaskStatusCancelled:  {},
}

// TaskReassignableStatuses 任务 owner 可原位交换的状态集合。
// Python: TASK_REASSIGNABLE_STATUSES (schema/status.py L399-405)
var TaskReassignableStatuses = map[TaskStatus]bool{
	TaskStatusPending:    true,
	TaskStatusBlocked:    true,
	TaskStatusInProgress: true,
}

// MemberDepartedStatuses 返回"leader 已释放该成员"的状态集合。
//
// 与 MemberUnreachableStatuses 不是一回事：保护成员在飞工作的守卫在这里解除
// （在**请求**时而非终态），消息投递则要到 MemberUnreachableStatuses 才停——
// shutdown_member 先写 SHUTDOWN_REQUESTED 再发通知，若在请求时就断投递，
// 那条"你已被移除"的通知永远送不到。
//
// Python: MEMBER_DEPARTED_STATUSES (schema/status.py:84-89)
func MemberDepartedStatuses() map[MemberStatus]struct{} {
	return map[MemberStatus]struct{}{
		MemberStatusShutdownRequested: {},
		MemberStatusShutdown:          {},
	}
}

// IsMemberQuiescent 成员状态是否属于「未在动」集合。
// Python: MEMBER_QUIESCENT_STATUSES 成员关系（schema/status.py L202-213）
func IsMemberQuiescent(status MemberStatus) bool {
	switch status {
	case MemberStatusUnstarted, MemberStatusReady, MemberStatusPaused,
		MemberStatusStopped, MemberStatusShutdown, MemberStatusDormant, MemberStatusError:
		return true
	}
	return false
}

// ParseMemberStatus 解析状态值字符串为 MemberStatus；未知返回 ok=false。
// Python: parse_member_status（member_activity.py L55-71）
func ParseMemberStatus(value string) (MemberStatus, bool) {
	switch MemberStatus(value) {
	case MemberStatusUnstarted, MemberStatusStarting, MemberStatusReady,
		MemberStatusBusy, MemberStatusPaused, MemberStatusStopped,
		MemberStatusRestarting, MemberStatusShutdownRequested,
		MemberStatusShutdown, MemberStatusDormant, MemberStatusError:
		return MemberStatus(value), true
	}
	return "", false
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsValidMemberTransition 检查 MemberStatus 状态转换是否合法。
// Python: is_valid_transition(current_status, new_status, MEMBER_TRANSITIONS)
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
