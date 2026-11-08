package dmessage_queue

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DmqRequestMessage 分布式请求消息。
// Python: DmqRequestMessage(DmqMessage)
// 内嵌 QueueMessage 复用 message_id/payload/error_code/error_msg 四个基础字段，
// 与 Python 的 QueueMessage 基类字段一一对应。
type DmqRequestMessage struct {
	// QueueMessage 基础消息字段
	message_queue.QueueMessage
	// Type 消息类型（INPUT / STOP），默认 INPUT
	Type DMessageType `json:"type"`
	// ReplyTopic 响应回写 topic
	ReplyTopic string `json:"reply_topic"`
	// RequestID 请求唯一标识
	RequestID string `json:"request_id"`
	// SenderID 发送方标识
	SenderID string `json:"sender_id"`
	// ReceiverID 接收方标识
	ReceiverID string `json:"receiver_id"`
	// EnableStream 是否启用流式响应
	EnableStream bool `json:"enable_stream"`
	// ExpireAt 过期时间戳（秒级浮点），可选
	ExpireAt *float64 `json:"expire_at"`
}

// DmqResponseMessage 分布式响应消息。
// Python: DmqResponseMessage(DmqMessage)
type DmqResponseMessage struct {
	// QueueMessage 基础消息字段
	message_queue.QueueMessage
	// Type 消息类型，默认 OUTPUT
	Type DMessageType `json:"type"`
	// ResultType 结果类型（MESSAGE / ERROR），默认 MESSAGE
	ResultType ResultType `json:"result_type"`
	// RequestID 请求唯一标识，与请求对应
	RequestID string `json:"request_id"`
	// SenderID 发送方标识
	SenderID string `json:"sender_id"`
	// ReceiverID 接收方标识
	ReceiverID string `json:"receiver_id"`
	// Seq 流式分片序号
	Seq int `json:"seq"`
	// LastChunk 是否为流式最后一片
	LastChunk bool `json:"last_chunk"`
	// ExpireAt 过期时间戳（秒级浮点），可选
	ExpireAt *float64 `json:"expire_at"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// DMessageType 分布式消息类型。
// Python: DMessageType(str, Enum)
type DMessageType string

const (
	// DMessageTypeInput 输入消息，发起 agent 调用
	DMessageTypeInput DMessageType = "INPUT"
	// DMessageTypeStop 中断消息
	DMessageTypeStop DMessageType = "STOP"
	// DMessageTypeOutput 输出消息，返回执行结果
	DMessageTypeOutput DMessageType = "OUTPUT"
)

// ResultType 响应结果类型。
// Python: ResultType(str, Enum)
type ResultType string

const (
	// ResultTypeMessage 正常消息结果
	ResultTypeMessage ResultType = "MESSAGE"
	// ResultTypeError 错误结果
	ResultTypeError ResultType = "ERROR"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDmqRequestMessage 创建分布式请求消息，Type 默认 INPUT。
// Python: DmqRequestMessage(payload=...)
func NewDmqRequestMessage(payload map[string]any) *DmqRequestMessage {
	return &DmqRequestMessage{
		QueueMessage: message_queue.QueueMessage{Payload: payload},
		Type:         DMessageTypeInput,
	}
}

// NewDmqResponseMessage 创建分布式响应消息，Type 默认 OUTPUT、ResultType 默认 MESSAGE。
// Python: DmqResponseMessage(payload=...)
func NewDmqResponseMessage(payload map[string]any) *DmqResponseMessage {
	return &DmqResponseMessage{
		QueueMessage: message_queue.QueueMessage{Payload: payload},
		Type:         DMessageTypeOutput,
		ResultType:   ResultTypeMessage,
	}
}
