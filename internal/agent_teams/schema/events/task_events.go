package events

// ──────────────────────────── 结构体 ────────────────────────────

// TaskCreatedEvent 任务创建事件。
// Python: TaskCreatedEvent
type TaskCreatedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
	// Status 初始状态
	Status string
}

// TaskPlanRequestEvent 成员提交执行计划审批事件。
// Python: TaskPlanRequestEvent
type TaskPlanRequestEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
	// Status 提交后的任务状态
	Status string
	// PlanID 成员计划提交标识
	PlanID string
	// MemberPlanMD 提交的计划文件路径
	MemberPlanMD string
	// ToolCallID submit_plan 工具调用 ID
	ToolCallID string
}

// TaskPlanResponseEvent Leader 审批/驳回成员执行计划事件。
// Python: TaskPlanResponseEvent
type TaskPlanResponseEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
	// Approved 是否批准
	Approved bool
	// Status 审批后的任务状态
	Status string
	// PlanID 成员计划提交标识
	PlanID string
	// Feedback Leader 反馈
	Feedback string
	// ToolCallID submit_plan 工具调用 ID
	ToolCallID string
}

// TaskUpdatedEvent 任务更新事件。
// Python: TaskUpdatedEvent
type TaskUpdatedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
}

// TaskClaimedEvent 任务认领事件。
// Python: TaskClaimedEvent
type TaskClaimedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
}

// TaskStartedEvent scheduled 派发启动事件（F_62）：调度器把已指派任务推进到
// 执行——建队时的指派与执行开始是两个独立事件。
// Python: TaskStartedEvent（schema/events.py，TeamEvent.TASK_STARTED L161）
type TaskStartedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
	// MemberName 被启动的成员
	MemberName string
}

func (e TaskStartedEvent) EventTypeName() string { return TeamEventTaskStarted }

// ToPayload 转换为事件载荷。
func (e TaskStartedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID, "member_name": e.MemberName}
}

// TaskCompletedEvent 任务完成事件。
// Python: TaskCompletedEvent
type TaskCompletedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
}

// TaskCancelledEvent 任务取消事件。
// Python: TaskCancelledEvent
type TaskCancelledEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
}

// TaskUnblockedEvent 任务解除阻塞事件。
// Python: TaskUnblockedEvent
type TaskUnblockedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
}

// TaskListDrainedEvent 任务列表清空（全部终态）事件。
// Python: TaskListDrainedEvent
type TaskListDrainedEvent struct {
	BaseEventMessage
	// TaskCount 终态任务数
	TaskCount int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EventTypeName 返回任务创建事件类型名。
func (e TaskCreatedEvent) EventTypeName() string { return TeamEventTaskCreated }

// ToPayload 转换为事件载荷。
func (e TaskCreatedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID, "status": e.Status}
}

// EventTypeName 返回计划提交事件类型名。
func (e TaskPlanRequestEvent) EventTypeName() string { return TeamEventTaskPlanRequest }

// ToPayload 转换为事件载荷。
func (e TaskPlanRequestEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID, "status": e.Status, "plan_id": e.PlanID, "member_plan_md": e.MemberPlanMD, "tool_call_id": e.ToolCallID}
}

// EventTypeName 返回计划审批响应事件类型名。
func (e TaskPlanResponseEvent) EventTypeName() string { return TeamEventTaskPlanResponse }

// ToPayload 转换为事件载荷。
// S-附录4: 对齐 Python model_dump()——member_name（被审批成员）必须进载荷，
// OnTaskPlanDecision 依赖它区分"给自己的审批"与"他人的审批"。
func (e TaskPlanResponseEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "task_id": e.TaskID, "approved": e.Approved, "status": e.Status, "plan_id": e.PlanID, "feedback": e.Feedback, "tool_call_id": e.ToolCallID}
}

// EventTypeName 返回任务更新事件类型名。
func (e TaskUpdatedEvent) EventTypeName() string { return TeamEventTaskUpdated }

// ToPayload 转换为事件载荷。
func (e TaskUpdatedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID}
}

// EventTypeName 返回任务认领事件类型名。
func (e TaskClaimedEvent) EventTypeName() string { return TeamEventTaskClaimed }

// ToPayload 转换为事件载荷。
func (e TaskClaimedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID}
}

// EventTypeName 返回任务完成事件类型名。
func (e TaskCompletedEvent) EventTypeName() string { return TeamEventTaskCompleted }

// ToPayload 转换为事件载荷。
func (e TaskCompletedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID}
}

// EventTypeName 返回任务取消事件类型名。
func (e TaskCancelledEvent) EventTypeName() string { return TeamEventTaskCancelled }

// ToPayload 转换为事件载荷。
func (e TaskCancelledEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID}
}

// EventTypeName 返回任务解除阻塞事件类型名。
func (e TaskUnblockedEvent) EventTypeName() string { return TeamEventTaskUnblocked }

// ToPayload 转换为事件载荷。
func (e TaskUnblockedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID}
}

// EventTypeName 返回任务列表清空事件类型名。
func (e TaskListDrainedEvent) EventTypeName() string { return TeamEventTaskListDrained }

// ToPayload 转换为事件载荷。
func (e TaskListDrainedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_count": e.TaskCount}
}

// TaskVerifiedEvent 评审通过事件（IN_REVIEW → COMPLETED）。
// member_name 是作者（任务 assignee）——完成解除下游阻塞与直接完成一致。
// Python: TaskVerifiedEvent (events.py L414-421)
type TaskVerifiedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
}

// TaskRevisionRequestedEvent 评审不通过事件（IN_REVIEW → IN_PROGRESS）。
// 返工回路：member_name 是作者（仍持有该任务）；feedback 为汇总后的返工指引。
// Python: TaskRevisionRequestedEvent (events.py L423-431)
type TaskRevisionRequestedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
	// Feedback 返工反馈
	Feedback string
}

// TaskReviewVoteEvent 评审投票事件（scheduled dispatch，F_62）。
// scheduled 模式下 verify_task 只持久化票——任务保持 IN_REVIEW，leader 侧
// 调度器计票并用 settle_review 结算。member_name 是作者（与其他 verify-gate
// 事件一致）；reviewer 是投票人。计数字段是投票后的计票快照。
// Python: TaskReviewVoteEvent (events.py L433-448)
type TaskReviewVoteEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
	// Reviewer 投票成员
	Reviewer string
	// Decision 票值：pass / fail（inspector 为 0~1 分数）
	Decision string
	// ReviewRound 票所属评审轮
	ReviewRound int64
	// PassCount 本轮投 pass 的独立 reviewer 数
	PassCount int
	// FailCount 本轮投 fail 的独立 reviewer 数
	FailCount int
	// ReviewerCount 验收人总数
	ReviewerCount int
}

// EventTypeName 返回评审通过事件类型名。
func (e TaskVerifiedEvent) EventTypeName() string { return TeamEventTaskVerified }

// EventTypeName 返回返工请求事件类型名。
func (e TaskRevisionRequestedEvent) EventTypeName() string { return TeamEventTaskRevisionRequested }

// EventTypeName 返回评审投票事件类型名。
func (e TaskReviewVoteEvent) EventTypeName() string { return TeamEventTaskReviewVote }

// ToPayload 转换为评审通过事件载荷。
// Python: TaskVerifiedEvent.to_payload（task_id + member_name 基线字段）。
func (e TaskVerifiedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "task_id": e.TaskID}
}

// ToPayload 转换为返工请求事件载荷。
// Python: TaskRevisionRequestedEvent.to_payload。
func (e TaskRevisionRequestedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "task_id": e.TaskID, "feedback": e.Feedback}
}

// ToPayload 转换为评审投票事件载荷。
// Python: TaskReviewVoteEvent.to_payload。
func (e TaskReviewVoteEvent) ToPayload() map[string]any {
	return map[string]any{
		"team_name": e.TeamName, "member_name": e.MemberName,
		"task_id": e.TaskID, "reviewer": e.Reviewer, "decision": e.Decision,
		"review_round": e.ReviewRound, "pass_count": e.PassCount,
		"fail_count": e.FailCount, "reviewer_count": e.ReviewerCount,
	}
}

// TaskSubmittedForReviewEvent 作者提交任务待验收事件。
// 由 TeamTaskManager.complete 在完成的任务带验收人时发布
// （IN_PROGRESS → IN_REVIEW）。member_name 是作者。框架按 reviewer 通知验收人。
// Python: TaskSubmittedForReviewEvent (events.py L403-411)
type TaskSubmittedForReviewEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
	// Reviewer 待通知的验收人成员名列表
	Reviewer []string
}

// EventTypeName 返回提交评审事件类型名。
func (e TaskSubmittedForReviewEvent) EventTypeName() string { return TeamEventTaskSubmittedForReview }

// ToPayload 转换为提交评审事件载荷。
// Python: TaskSubmittedForReviewEvent.to_payload。
func (e TaskSubmittedForReviewEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "task_id": e.TaskID, "reviewer": e.Reviewer}
}
