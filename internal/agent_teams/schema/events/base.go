package events

// ──────────────────────────── 结构体 ────────────────────────────

// BaseEventMessage 所有团队事件消息的基类。
// Python: BaseEventMessage
type BaseEventMessage struct {
	// TeamName 团队名（事件路由用）
	TeamName string
	// MemberName 成员名（成员级事件时存在）
	MemberName string
}

// EventMessage 事件消息包装，将事件类型与载荷配对。
// Python: EventMessage
type EventMessage struct {
	// EventType 事件类型（TeamEvent 常量）
	EventType string
	// Payload 原始事件载荷
	Payload map[string]any
	// SenderID 发送者节点 ID（用于过滤自发布消息）
	SenderID string
}

// TypedEvent 带类型标识的团队事件接口。
// Python: BaseEventMessage + EventMessage.from_event() 的自动映射
type TypedEvent interface {
	// EventTypeName 返回 TeamEvent 常量
	EventTypeName() string
	// ToPayload 转换为 map[string]any 载荷
	ToPayload() map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEventMessage 从具体事件创建 EventMessage 指针。
// Python: EventMessage.from_event(event)
func NewEventMessage(eventType string, payload map[string]any, senderID string) *EventMessage {
	return &EventMessage{
		EventType: eventType,
		Payload:   payload,
		SenderID:  senderID,
	}
}

// EventMessageFromEvent 从具体事件创建 EventMessage 指针。
// Python: EventMessage.from_event(event)
// 返回指针以适配 Messager.Publish(ctx, topicID, *EventMessage) 签名。
func EventMessageFromEvent(e TypedEvent) *EventMessage {
	return &EventMessage{
		EventType: e.EventTypeName(),
		Payload:   e.ToPayload(),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
