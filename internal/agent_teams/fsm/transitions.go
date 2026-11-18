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

// TaskStatus 常量 — 对齐 Python: TaskStatus (schema/status.py L304-341)
// condition-named 状态机：状态名描述"任务处于什么条件"，迁移（claim/start/
// submit/approve/verify）不是状态。两个可选闸门 PLANNING（执行前，plan 闸）
// 与 IN_REVIEW（执行后，verify 闸）结构对称，各有返工自环。
// IN_PROGRESS 统一了旧模型的 CLAIMED / STARTED / PLAN_APPROVED 执行节点。
const (
	TaskStatusPending    = "pending"
	TaskStatusBlocked    = "blocked"
	TaskStatusPlanning   = "planning"
	TaskStatusInProgress = "in_progress"
	TaskStatusInReview   = "in_review"
	TaskStatusCompleted  = "completed"
	TaskStatusCancelled  = "cancelled"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// MemberTransitions MemberStatus 状态转换表。
// Python: MEMBER_TRANSITIONS (openjiuwen/agent_teams/schema/status.py)
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
// Python: MEMBER_SETTLED_STATUSES
var MemberSettledStatuses = map[string]bool{
	MemberStatusReady:    true,
	MemberStatusPaused:   true,
	MemberStatusStopped:  true,
	MemberStatusShutdown: true,
}

// ExecutionTransitions ExecutionStatus 状态转换表。
// Python: EXECUTION_TRANSITIONS
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
// Python: TASK_TRANSITIONS (schema/status.py L345-365)
// 覆盖双调度模式与两个可选闸门的单一超集；任务走哪条路径由执行动作决定
// （claim/start → IN_PROGRESS，submit_plan → PLANNING，
// member_complete → IN_REVIEW/COMPLETED），不按 dispatch mode 分支。
var TaskTransitions = map[string][]string{
	TaskStatusPending: {
		TaskStatusPlanning,   // plan_mode: 预留进入计划
		TaskStatusInProgress, // claim（autonomous）/ start（scheduled）
		TaskStatusBlocked,
		TaskStatusCancelled,
	},
	TaskStatusBlocked: {
		TaskStatusPending,
		TaskStatusCancelled,
	},
	TaskStatusPlanning: {
		TaskStatusPlanning,   // submit / reject 返工自环
		TaskStatusInProgress, // approve_plan
		TaskStatusPending,    // reset
		TaskStatusBlocked,
		TaskStatusCancelled,
	},
	TaskStatusInProgress: {
		TaskStatusInReview,  // member_complete_task（有 reviewer）
		TaskStatusCompleted, // member_complete_task（无 reviewer）
		TaskStatusPending,   // reset
		TaskStatusBlocked,
		TaskStatusCancelled,
	},
	TaskStatusInReview: {
		TaskStatusCompleted,  // verify pass
		TaskStatusInProgress, // verify fail — 返工环
		TaskStatusPending,    // reset
		TaskStatusCancelled,
	},
	TaskStatusCompleted: {},
	TaskStatusCancelled: {},
}

// TaskReassignableStatuses 任务 owner 可原位交换的状态集合（改派不改状态）。
// 这是所有权问题不是进度问题，刻意不是终态的补集：PENDING/BLOCKED 是
// scheduled 模式的静息态、无在飞推理，最安全；IN_PROGRESS 交接会损失原
// owner 的在飞推理（TASK_REVOKED 负责让它丢弃）。两个闸门被排除——
// PLANNING/IN_REVIEW 各有一份绑定在当前 owner 上的产物（已提交的计划/
// 进行中的评审），换 owner 会让产物归属错人。
//
// Python: TASK_REASSIGNABLE_STATUSES (schema/status.py L399-405)
var TaskReassignableStatuses = map[string]bool{
	TaskStatusPending:    true,
	TaskStatusBlocked:    true,
	TaskStatusInProgress: true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsValidMemberTransition 检查 MemberStatus 状态转换是否合法。
// Python: is_valid_transition(current_status, new_status, MEMBER_TRANSITIONS)
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
// Python: is_valid_transition(current, new_status, EXECUTION_TRANSITIONS)
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
// Python: is_valid_transition(current, new_status, TASK_TRANSITIONS)
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

// TerminalTaskStatuses 返回任务终态集合（用于 SQL NOT IN 过滤）。
// Python: list(TASK_TERMINAL_STATUSES)
func TerminalTaskStatuses() []string {
	return []string{TaskStatusCompleted, TaskStatusCancelled}
}

// IsTaskTerminal 判断任务状态是否为终态。
// Python: TASK_TERMINAL_STATUSES = {COMPLETED, CANCELLED}
func IsTaskTerminal(status string) bool {
	return status == TaskStatusCompleted || status == TaskStatusCancelled
}
