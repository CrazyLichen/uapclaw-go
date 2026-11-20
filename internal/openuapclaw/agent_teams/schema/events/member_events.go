package events

// ──────────────────────────── 结构体 ────────────────────────────

// MemberSpawnedEvent 成员生成事件。
// Python: MemberSpawnedEvent
type MemberSpawnedEvent struct {
	BaseEventMessage
}

// MemberRestartedEvent 成员重启事件。
// Python: MemberRestartedEvent
type MemberRestartedEvent struct {
	BaseEventMessage
	// Reason 重启原因
	Reason string
	// RestartCount 重启次数
	RestartCount int
}

// CheckpointCreatedEvent 成员打了命名快照（fork 半区）。leader 侧渲染为
// <team-event kind="checkpoint"> 的 announcement-only 注记——替代
// send_message 通知，不触发回复。Base 的 member_name 即创建者。
// Python: CheckpointCreatedEvent (schema/events.py:287-299)
type CheckpointCreatedEvent struct {
	BaseEventMessage
	// Name 快照名（团队全局唯一）
	Name string
	// MessageCount 快照处的消息数
	MessageCount int
	// Description 快照描述
	Description string
}

// MemberStatusChangedEvent 成员状态变更事件。
// Python: MemberStatusChangedEvent
type MemberStatusChangedEvent struct {
	BaseEventMessage
	// OldStatus 之前状态
	OldStatus string
	// NewStatus 新状态
	NewStatus string
}

// MemberExecutionChangedEvent 成员执行状态变更事件。
// Python: MemberExecutionChangedEvent
type MemberExecutionChangedEvent struct {
	BaseEventMessage
	// OldStatus 之前执行状态
	OldStatus string
	// NewStatus 新执行状态
	NewStatus string
}

// MemberShutdownEvent 成员关闭事件。
// Python: MemberShutdownEvent
type MemberShutdownEvent struct {
	BaseEventMessage
	// Force 是否强制关闭
	Force bool
}

// MemberCanceledEvent 成员取消事件。
// Python: MemberCanceledEvent
type MemberCanceledEvent struct {
	BaseEventMessage
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EventTypeName 返回成员生成事件类型名。
func (e MemberSpawnedEvent) EventTypeName() string { return TeamEventMemberSpawned }

// ToPayload 转换为事件载荷。
func (e MemberSpawnedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName}
}

// EventTypeName 返回成员重启事件类型名。
func (e MemberRestartedEvent) EventTypeName() string { return TeamEventMemberRestarted }

// ToPayload 转换为事件载荷。
func (e MemberRestartedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "reason": e.Reason, "restart_count": e.RestartCount}
}

// EventTypeName 返回成员状态变更事件类型名。
func (e MemberStatusChangedEvent) EventTypeName() string { return TeamEventMemberStatusChanged }

// ToPayload 转换为事件载荷。
func (e MemberStatusChangedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "old_status": e.OldStatus, "new_status": e.NewStatus}
}

// EventTypeName 返回成员执行状态变更事件类型名。
func (e MemberExecutionChangedEvent) EventTypeName() string { return TeamEventMemberExecutionChanged }

// ToPayload 转换为事件载荷。
func (e MemberExecutionChangedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "old_status": e.OldStatus, "new_status": e.NewStatus}
}

// EventTypeName 返回成员关闭事件类型名。
func (e MemberShutdownEvent) EventTypeName() string { return TeamEventMemberShutdown }

// ToPayload 转换为事件载荷。
func (e MemberShutdownEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "force": e.Force}
}

// EventTypeName 返回成员取消事件类型名。
func (e MemberCanceledEvent) EventTypeName() string { return TeamEventMemberCanceled }

// ToPayload 转换为事件载荷。
func (e MemberCanceledEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName}
}

// EventTypeName 事件类型名。Python: TeamEvent.CHECKPOINT_CREATED (events.py:143)
func (e CheckpointCreatedEvent) EventTypeName() string { return TeamEventCheckpointCreated }

// ToPayload 转换为事件载荷。Python: CheckpointCreatedEvent 字段（events.py:287-299）。
func (e CheckpointCreatedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "name": e.Name, "message_count": e.MessageCount, "description": e.Description}
}
