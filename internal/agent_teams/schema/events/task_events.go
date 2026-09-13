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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func (e TaskCreatedEvent) EventTypeName() string { return TeamEventTaskCreated }
func (e TaskCreatedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID, "status": e.Status}
}

func (e TaskPlanRequestEvent) EventTypeName() string { return TeamEventTaskPlanRequest }
func (e TaskPlanRequestEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID, "status": e.Status, "plan_id": e.PlanID, "member_plan_md": e.MemberPlanMD, "tool_call_id": e.ToolCallID}
}

func (e TaskPlanResponseEvent) EventTypeName() string { return TeamEventTaskPlanResponse }
func (e TaskPlanResponseEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID, "approved": e.Approved, "status": e.Status, "plan_id": e.PlanID, "feedback": e.Feedback, "tool_call_id": e.ToolCallID}
}

func (e TaskUpdatedEvent) EventTypeName() string { return TeamEventTaskUpdated }
func (e TaskUpdatedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID}
}

func (e TaskClaimedEvent) EventTypeName() string { return TeamEventTaskClaimed }
func (e TaskClaimedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID}
}

func (e TaskCompletedEvent) EventTypeName() string { return TeamEventTaskCompleted }
func (e TaskCompletedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID}
}

func (e TaskCancelledEvent) EventTypeName() string { return TeamEventTaskCancelled }
func (e TaskCancelledEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID}
}

func (e TaskUnblockedEvent) EventTypeName() string { return TeamEventTaskUnblocked }
func (e TaskUnblockedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_id": e.TaskID}
}

func (e TaskListDrainedEvent) EventTypeName() string { return TeamEventTaskListDrained }
func (e TaskListDrainedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "task_count": e.TaskCount}
}
