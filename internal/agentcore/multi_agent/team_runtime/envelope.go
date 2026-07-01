package team_runtime

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// MessageEnvelope 消息信封，Agent 间路由消息的不可变容器。
//
// 通过 Recipient 和 TopicID 区分 P2P 和 Pub-Sub 两种通信模式：
//   - P2P 模式：Recipient 非空，指定接收者 Agent ID
//   - Pub-Sub 模式：TopicID 非空，指定发布主题
//
// 对应 Python: MessageEnvelope (openjiuwen/core/multi_agent/team_runtime/envelope.py)
type MessageEnvelope struct {
	// MessageID 唯一消息标识
	MessageID string
	// Message 消息负载
	Message any
	// Sender 发送者 Agent ID
	Sender string
	// Recipient 接收者 Agent ID（P2P 模式）
	Recipient string
	// TopicID 主题 ID（Pub-Sub 模式）
	TopicID string
	// SessionID 会话 ID
	SessionID string
	// Metadata 附加元数据
	Metadata map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// EnvelopeOption 消息信封选项函数类型
type EnvelopeOption func(*MessageEnvelope)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageEnvelope 创建消息信封。
func NewMessageEnvelope(messageID string, message any, sender string, opts ...EnvelopeOption) *MessageEnvelope {
	e := &MessageEnvelope{
		MessageID: messageID,
		Message:   message,
		Sender:    sender,
		Metadata:  make(map[string]any),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// IsP2P 判断是否为 P2P 模式消息。
//
// 当 Recipient 非空时为 P2P 模式。
func (e *MessageEnvelope) IsP2P() bool {
	return e.Recipient != ""
}

// IsPubSub 判断是否为 Pub-Sub 模式消息。
//
// 当 TopicID 非空时为 Pub-Sub 模式。
func (e *MessageEnvelope) IsPubSub() bool {
	return e.TopicID != ""
}

// String 返回消息信封的精简表示。
//
// P2P 模式：MessageEnvelope{id=xxx, sender=a, recipient=b}
// Pub-Sub 模式：MessageEnvelope{id=xxx, sender=a, topic=t}
func (e *MessageEnvelope) String() string {
	if e.IsP2P() {
		return fmt.Sprintf("MessageEnvelope{id=%s, sender=%s, recipient=%s}", e.MessageID, e.Sender, e.Recipient)
	}
	if e.IsPubSub() {
		return fmt.Sprintf("MessageEnvelope{id=%s, sender=%s, topic=%s}", e.MessageID, e.Sender, e.TopicID)
	}
	return fmt.Sprintf("MessageEnvelope{id=%s, sender=%s}", e.MessageID, e.Sender)
}

// WithRecipient 设置 P2P 接收者。
func WithRecipient(recipient string) EnvelopeOption {
	return func(e *MessageEnvelope) {
		e.Recipient = recipient
	}
}

// WithTopicID 设置 Pub-Sub 主题 ID。
func WithTopicID(topicID string) EnvelopeOption {
	return func(e *MessageEnvelope) {
		e.TopicID = topicID
	}
}

// WithSessionID 设置会话 ID。
func WithSessionID(sessionID string) EnvelopeOption {
	return func(e *MessageEnvelope) {
		e.SessionID = sessionID
	}
}

// WithMetadata 设置附加元数据。
func WithMetadata(metadata map[string]any) EnvelopeOption {
	return func(e *MessageEnvelope) {
		e.Metadata = metadata
	}
}
