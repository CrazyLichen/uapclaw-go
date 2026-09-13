package events

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// TeamEvent 团队事件类型常量，用于跨进程通信。
// Python: TeamEvent
const (
	// 团队生命周期事件
	TeamEventCreated       = "team_created"
	TeamEventCleaned       = "team_cleaned"
	TeamEventStandby       = "team_standby"
	TeamEventTeamCompleted = "team_completed"

	// 成员生命周期事件
	TeamEventMemberSpawned          = "member_spawned"
	TeamEventMemberRestarted        = "member_restarted"
	TeamEventMemberStatusChanged    = "member_status_changed"
	TeamEventMemberExecutionChanged = "member_execution_changed"
	TeamEventMemberShutdown         = "member_shutdown"
	TeamEventMemberCanceled         = "member_canceled"

	// 协作事件
	TeamEventPlanApproval       = "plan_approval"
	TeamEventToolApprovalResult = "tool_approval_result"

	// 消息事件
	TeamEventMessage   = "message"
	TeamEventBroadcast = "broadcast"

	// 任务事件
	TeamEventTaskCreated      = "task_created"
	TeamEventTaskPlanRequest  = "task_plan_request"
	TeamEventTaskPlanResponse = "task_plan_response"
	TeamEventTaskUpdated      = "task_updated"
	TeamEventTaskClaimed      = "task_claimed"
	TeamEventTaskCompleted    = "task_completed"
	TeamEventTaskCancelled    = "task_cancelled"
	TeamEventTaskUnblocked    = "task_unblocked"
	TeamEventTaskListDrained  = "task_list_drained"

	// Worktree 事件
	TeamEventWorktreeCreated = "worktree_created"
	TeamEventWorktreeRemoved = "worktree_removed"

	// Workspace 事件
	TeamEventWorkspaceArtifactUpdated = "workspace_artifact_updated"
	TeamEventWorkspaceConflict        = "workspace_conflict"
	TeamEventWorkspaceLockRequest     = "workspace_lock_request"
	TeamEventWorkspaceLockResponse    = "workspace_lock_response"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
