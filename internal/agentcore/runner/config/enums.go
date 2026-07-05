package config

// ──────────────────────────── 结构体 ────────────────────────────

// MessageQueueType 消息队列类型枚举。
//
// 对应 Python: MessageQueueType(str, Enum)
type MessageQueueType string

// ──────────────────────────── 常量 ────────────────────────────
const (
	// MessageQueueTypePulsar Pulsar 消息队列
	MessageQueueTypePulsar MessageQueueType = "pulsar"
	// MessageQueueTypeFake Fake 内存消息队列（用于本地/测试）
	MessageQueueTypeFake MessageQueueType = "fake"
)
