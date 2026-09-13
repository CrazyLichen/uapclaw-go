package events

// ──────────────────────────── 结构体 ────────────────────────────

// MessageEvent 点对点消息事件。
// Python: MessageEvent
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
// Python: BroadcastEvent
type BroadcastEvent struct {
	BaseEventMessage
	// MessageID 消息唯一标识
	MessageID string
	// FromMemberName 发送者
	FromMemberName string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func (e MessageEvent) EventTypeName() string { return TeamEventMessage }
func (e MessageEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "message_id": e.MessageID, "from_member_name": e.FromMemberName, "to_member_name": e.ToMemberName}
}

func (e BroadcastEvent) EventTypeName() string { return TeamEventBroadcast }
func (e BroadcastEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "message_id": e.MessageID, "from_member_name": e.FromMemberName}
}
