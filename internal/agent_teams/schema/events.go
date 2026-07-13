package schema

// ──────────────────────────── 枚举 ────────────────────────────

// TeamTopic 团队事件路由的 topic 类别。
// 对齐 Python: TeamTopic (openjiuwen/agent_teams/schema/events.py)
type TeamTopic string

const (
	// TeamTopicTeam 团队级事件
	TeamTopicTeam TeamTopic = "team"
	// TeamTopicTask 任务级事件
	TeamTopicTask TeamTopic = "task"
	// TeamTopicMessage 消息级事件
	TeamTopicMessage TeamTopic = "message"
)

// ──────────────────────────── 常量 ────────────────────────────

// TeamEvent 团队事件类型常量，用于跨进程通信。
// 对齐 Python: TeamEvent
const (
	// 团队生命周期事件
	TeamEventCreated        = "team_created"
	TeamEventCleaned        = "team_cleaned"
	TeamEventStandby        = "team_standby"
	TeamEventTeamCompleted  = "team_completed"

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
	TeamEventWorkspaceConflict       = "workspace_conflict"
	TeamEventWorkspaceLockRequest    = "workspace_lock_request"
	TeamEventWorkspaceLockResponse   = "workspace_lock_response"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseEventMessage 所有团队事件消息的基类。
// 对齐 Python: BaseEventMessage
type BaseEventMessage struct {
	// TeamName 团队名（事件路由用）
	TeamName string
	// MemberName 成员名（成员级事件时存在）
	MemberName string
}

// TeamCreatedEvent 团队创建事件。
// 对齐 Python: TeamCreatedEvent
type TeamCreatedEvent struct {
	BaseEventMessage
	// DisplayName 团队显示标签
	DisplayName string
	// LeaderMemberName Leader 成员名
	LeaderMemberName string
	// Created 创建时间戳
	Created int64
}

// TeamCleanedEvent 团队清理事件。
// 对齐 Python: TeamCleanedEvent
type TeamCleanedEvent struct {
	BaseEventMessage
}

// TeamStandbyEvent 持久团队进入待机事件。
// 对齐 Python: TeamStandbyEvent
type TeamStandbyEvent struct {
	BaseEventMessage
}

// TeamCompletedEvent 团队完成事件。
// 对齐 Python: TeamCompletedEvent
type TeamCompletedEvent struct {
	BaseEventMessage
	// MemberCount 完成时的成员数
	MemberCount int
	// TaskCount 完成时的任务数
	TaskCount int
}

// MemberSpawnedEvent 成员生成事件。
// 对齐 Python: MemberSpawnedEvent
type MemberSpawnedEvent struct {
	BaseEventMessage
}

// MemberRestartedEvent 成员重启事件。
// 对齐 Python: MemberRestartedEvent
type MemberRestartedEvent struct {
	BaseEventMessage
	// Reason 重启原因
	Reason string
	// RestartCount 重启次数
	RestartCount int
}

// MemberStatusChangedEvent 成员状态变更事件。
// 对齐 Python: MemberStatusChangedEvent
type MemberStatusChangedEvent struct {
	BaseEventMessage
	// OldStatus 之前状态
	OldStatus string
	// NewStatus 新状态
	NewStatus string
}

// MemberExecutionChangedEvent 成员执行状态变更事件。
// 对齐 Python: MemberExecutionChangedEvent
type MemberExecutionChangedEvent struct {
	BaseEventMessage
	// OldStatus 之前执行状态
	OldStatus string
	// NewStatus 新执行状态
	NewStatus string
}

// MemberShutdownEvent 成员关闭事件。
// 对齐 Python: MemberShutdownEvent
type MemberShutdownEvent struct {
	BaseEventMessage
	// Force 是否强制关闭
	Force bool
}

// MemberCanceledEvent 成员取消事件。
// 对齐 Python: MemberCanceledEvent
type MemberCanceledEvent struct {
	BaseEventMessage
}

// PlanApprovalEvent 计划审批事件。
// 对齐 Python: PlanApprovalEvent
type PlanApprovalEvent struct {
	BaseEventMessage
	// Approved 是否批准
	Approved bool
}

// ToolApprovalResultEvent 工具调用审批结果事件。
// 对齐 Python: ToolApprovalResultEvent
type ToolApprovalResultEvent struct {
	BaseEventMessage
	// ToolCallID 被中断的工具调用 ID
	ToolCallID string
	// Approved 是否批准
	Approved bool
	// Feedback Leader 反馈
	Feedback string
	// AutoConfirm 是否自动确认后续同名工具调用
	AutoConfirm bool
}

// MessageEvent 点对点消息事件。
// 对齐 Python: MessageEvent
type MessageEvent struct {
	BaseEventMessage
	// MessageID 消息唯一标识
	MessageID string
	// FromMemberName 发送者
	FromMemberName string
	// ToMemberName 接收者
	ToMemberName string
}

// BroadcastEvent 广播消息事件。
// 对齐 Python: BroadcastEvent
type BroadcastEvent struct {
	BaseEventMessage
	// MessageID 消息唯一标识
	MessageID string
	// FromMemberName 发送者
	FromMemberName string
}

// TaskCreatedEvent 任务创建事件。
// 对齐 Python: TaskCreatedEvent
type TaskCreatedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
	// Status 初始状态
	Status string
}

// TaskPlanRequestEvent 成员提交执行计划审批事件。
// 对齐 Python: TaskPlanRequestEvent
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
// 对齐 Python: TaskPlanResponseEvent
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
// 对齐 Python: TaskUpdatedEvent
type TaskUpdatedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
}

// TaskClaimedEvent 任务认领事件。
// 对齐 Python: TaskClaimedEvent
type TaskClaimedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
}

// TaskCompletedEvent 任务完成事件。
// 对齐 Python: TaskCompletedEvent
type TaskCompletedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
}

// TaskCancelledEvent 任务取消事件。
// 对齐 Python: TaskCancelledEvent
type TaskCancelledEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
}

// TaskUnblockedEvent 任务解除阻塞事件。
// 对齐 Python: TaskUnblockedEvent
type TaskUnblockedEvent struct {
	BaseEventMessage
	// TaskID 任务唯一标识
	TaskID string
}

// TaskListDrainedEvent 任务列表清空（全部终态）事件。
// 对齐 Python: TaskListDrainedEvent
type TaskListDrainedEvent struct {
	BaseEventMessage
	// TaskCount 终态任务数
	TaskCount int
}

// WorktreeCreatedEvent Worktree 创建/恢复事件。
// 对齐 Python: WorktreeCreatedEvent
type WorktreeCreatedEvent struct {
	BaseEventMessage
	// WorktreeName worktree 滑名
	WorktreeName string
	// WorktreePath 绝对路径
	WorktreePath string
	// Existed 是否从已有 worktree 恢复
	Existed bool
}

// WorktreeRemovedEvent Worktree 移除事件。
// 对齐 Python: WorktreeRemovedEvent
type WorktreeRemovedEvent struct {
	BaseEventMessage
	// WorktreeName worktree 滑名
	WorktreeName string
	// WorktreePath 绝对路径
	WorktreePath string
}

// WorkspaceArtifactEvent 工件创建/更新事件。
// 对齐 Python: WorkspaceArtifactEvent
type WorkspaceArtifactEvent struct {
	BaseEventMessage
	// ArtifactPath 工件在工作空间内的相对路径
	ArtifactPath string
	// CommitSHA Git 提交 SHA（如已版本化）
	CommitSHA string
}

// WorkspaceConflictEvent 合并冲突/推送失败事件。
// 对齐 Python: WorkspaceConflictEvent
type WorkspaceConflictEvent struct {
	BaseEventMessage
	// FilePath 冲突文件路径
	FilePath string
	// ConflictingCommit 冲突提交 SHA
	ConflictingCommit string
}

// WorkspaceLockRequestEvent 锁请求事件。
// 对齐 Python: WorkspaceLockRequestEvent
type WorkspaceLockRequestEvent struct {
	BaseEventMessage
	// Action 锁操作：acquire 或 release
	Action string
	// FilePath 锁定/解锁的文件
	FilePath string
	// HolderName 锁请求者名称
	HolderName string
	// TimeoutSeconds 锁超时秒数
	TimeoutSeconds int
}

// WorkspaceLockResponseEvent 锁响应事件。
// 对齐 Python: WorkspaceLockResponseEvent
type WorkspaceLockResponseEvent struct {
	BaseEventMessage
	// FilePath 锁定/解锁的文件
	FilePath string
	// Granted 是否授予
	Granted bool
	// Holder 当前锁持有者信息（未授予时）
	Holder map[string]any
}

// EventMessage 事件消息包装，将事件类型与载荷配对。
// 对齐 Python: EventMessage
type EventMessage struct {
	// EventType 事件类型（TeamEvent 常量）
	EventType string
	// Payload 原始事件载荷
	Payload map[string]any
	// SenderID 发送者节点 ID（用于过滤自发布消息）
	SenderID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildTopic 构建 topic 字符串。
// 对齐 Python: TeamTopic.build(session_id, team_name)
func (t TeamTopic) Build(sessionID, teamName string) string {
	return "session:" + sessionID + ":team:" + teamName + ":" + string(t)
}

// NewEventMessage 从具体事件创建 EventMessage。
// 对齐 Python: EventMessage.from_event(event)
func NewEventMessage(eventType string, payload map[string]any, senderID string) EventMessage {
	return EventMessage{
		EventType: eventType,
		Payload:   payload,
		SenderID:  senderID,
	}
}
