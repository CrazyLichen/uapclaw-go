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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func (e MemberSpawnedEvent) EventTypeName() string { return TeamEventMemberSpawned }
func (e MemberSpawnedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName}
}

func (e MemberRestartedEvent) EventTypeName() string { return TeamEventMemberRestarted }
func (e MemberRestartedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "reason": e.Reason, "restart_count": e.RestartCount}
}

func (e MemberStatusChangedEvent) EventTypeName() string { return TeamEventMemberStatusChanged }
func (e MemberStatusChangedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "old_status": e.OldStatus, "new_status": e.NewStatus}
}

func (e MemberExecutionChangedEvent) EventTypeName() string { return TeamEventMemberExecutionChanged }
func (e MemberExecutionChangedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "old_status": e.OldStatus, "new_status": e.NewStatus}
}

func (e MemberShutdownEvent) EventTypeName() string { return TeamEventMemberShutdown }
func (e MemberShutdownEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "force": e.Force}
}

func (e MemberCanceledEvent) EventTypeName() string { return TeamEventMemberCanceled }
func (e MemberCanceledEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName}
}
