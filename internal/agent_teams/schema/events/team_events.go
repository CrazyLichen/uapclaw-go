package events

// ──────────────────────────── 结构体 ────────────────────────────

// TeamCreatedEvent 团队创建事件。
// Python: TeamCreatedEvent
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
// Python: TeamCleanedEvent
type TeamCleanedEvent struct {
	BaseEventMessage
}

// TeamStandbyEvent 持久团队进入待机事件。
// Python: TeamStandbyEvent
type TeamStandbyEvent struct {
	BaseEventMessage
}

// TeamCompletedEvent 团队完成事件。
// Python: TeamCompletedEvent
type TeamCompletedEvent struct {
	BaseEventMessage
	// MemberCount 完成时的成员数
	MemberCount int
	// TaskCount 完成时的任务数
	TaskCount int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func (e TeamCreatedEvent) EventTypeName() string { return TeamEventCreated }
func (e TeamCreatedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "display_name": e.DisplayName, "leader_member_name": e.LeaderMemberName, "created": e.Created}
}

func (e TeamCleanedEvent) EventTypeName() string { return TeamEventCleaned }
func (e TeamCleanedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName}
}

func (e TeamStandbyEvent) EventTypeName() string { return TeamEventStandby }
func (e TeamStandbyEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName}
}

func (e TeamCompletedEvent) EventTypeName() string { return TeamEventTeamCompleted }
func (e TeamCompletedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_count": e.MemberCount, "task_count": e.TaskCount}
}
