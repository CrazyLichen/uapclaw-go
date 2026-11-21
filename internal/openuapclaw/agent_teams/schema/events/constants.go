package events

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
	TeamEventCheckpointCreated      = "checkpoint_created"
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
	// TeamEventTaskRevoked 改派撤回事件：成员名下任务被 leader 原子换手给别人。
	// payload.member_name = 前任 assignee（定向通知，非成员级广播）；与
	// TASK_RELEASED（票回到可认领池的看板唤醒）语义不同。
	// Python: TeamEvent.TASK_REVOKED = "task_revoked"（schema/events.py L166）
	TeamEventTaskRevoked = "task_revoked"
	// TeamEventTaskReleased 释放事件：任务被 reset 回 PENDING 重新进入可认领池，
	// 唤醒 idle teammate（与 TASK_UNBLOCKED 同款下游 nudge）。
	// Python: TeamEvent.TASK_RELEASED = "task_released"（schema/events.py L165）
	TeamEventTaskReleased = "task_released"
	// TeamEventTaskStarted scheduled 派发启动事件（F_62）。
	// Python: TeamEvent.TASK_STARTED = "task_started"（schema/events.py L161）
	TeamEventTaskStarted   = "task_started"
	TeamEventTaskCompleted = "task_completed"
	// TeamEventTaskVerified 验收通过事件（verify gate pass，F_62）。
	// Python: TeamEvent.TASK_VERIFIED = "task_verified" (schema/events.py L170)
	TeamEventTaskVerified = "task_verified"
	// TeamEventTaskRevisionRequested 返工请求事件（verify gate fail，F_62）。
	// Python: TeamEvent.TASK_REVISION_REQUESTED = "task_revision_requested" (events.py L171)
	TeamEventTaskRevisionRequested = "task_revision_requested"
	// TeamEventTaskSubmittedForReview 提交评审事件（verify-gate 入口，F_62）。
	// Python: TeamEvent.TASK_SUBMITTED_FOR_REVIEW = "task_submitted_for_review" (events.py L169)
	TeamEventTaskSubmittedForReview = "task_submitted_for_review"
	// TeamEventTaskReviewVote 评审投票事件（scheduled dispatch，F_62）。
	// Python: TeamEvent.TASK_REVIEW_VOTE = "task_review_vote" (events.py L173)
	TeamEventTaskReviewVote  = "task_review_vote"
	TeamEventTaskCancelled   = "task_cancelled"
	TeamEventTaskUnblocked   = "task_unblocked"
	TeamEventTaskListDrained = "task_list_drained"

	// Worktree 事件
	TeamEventWorktreeCreated = "worktree_created"
	TeamEventWorktreeRemoved = "worktree_removed"

	// Workspace 事件
	TeamEventWorkspaceArtifactUpdated = "workspace_artifact_updated"
	TeamEventWorkspaceConflict        = "workspace_conflict"
	TeamEventWorkspaceLockRequest     = "workspace_lock_request"
	TeamEventWorkspaceLockResponse    = "workspace_lock_response"
)
