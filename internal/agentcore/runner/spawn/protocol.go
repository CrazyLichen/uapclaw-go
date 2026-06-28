package spawn

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// ──────────────────────────── 枚举 ────────────────────────────

// MessageType 消息类型枚举。
// 对齐 Python: MessageType (protocol.py)
type MessageType int

const (
	// MessageTypeInput 父→子：输入数据/Agent配置
	MessageTypeInput MessageType = iota
	// MessageTypeOutput 子→父：输出结果
	MessageTypeOutput
	// MessageTypeHealthCheck 父→子：健康检查请求
	MessageTypeHealthCheck
	// MessageTypeHealthCheckResponse 子→父：健康检查响应
	MessageTypeHealthCheckResponse
	// MessageTypeShutdown 父→子：关闭请求
	MessageTypeShutdown
	// MessageTypeShutdownAck 子→父：关闭确认
	MessageTypeShutdownAck
	// MessageTypeError 子→父：错误报告
	MessageTypeError
	// MessageTypeStreamChunk 子→父：流式块
	MessageTypeStreamChunk
	// MessageTypeDone 子→父：执行完成
	MessageTypeDone
)

// String 返回消息类型名称。
func (t MessageType) String() string {
	switch t {
	case MessageTypeInput:
		return "INPUT"
	case MessageTypeOutput:
		return "OUTPUT"
	case MessageTypeHealthCheck:
		return "HEALTH_CHECK"
	case MessageTypeHealthCheckResponse:
		return "HEALTH_CHECK_RESPONSE"
	case MessageTypeShutdown:
		return "SHUTDOWN"
	case MessageTypeShutdownAck:
		return "SHUTDOWN_ACK"
	case MessageTypeError:
		return "ERROR"
	case MessageTypeStreamChunk:
		return "STREAM_CHUNK"
	case MessageTypeDone:
		return "DONE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", t)
	}
}

// MarshalJSON 实现 json.Marshaler 接口。
func (t MessageType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
func (t *MessageType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	mapping := map[string]MessageType{
		"INPUT":                 MessageTypeInput,
		"OUTPUT":                MessageTypeOutput,
		"HEALTH_CHECK":          MessageTypeHealthCheck,
		"HEALTH_CHECK_RESPONSE": MessageTypeHealthCheckResponse,
		"SHUTDOWN":              MessageTypeShutdown,
		"SHUTDOWN_ACK":          MessageTypeShutdownAck,
		"ERROR":                 MessageTypeError,
		"STREAM_CHUNK":          MessageTypeStreamChunk,
		"DONE":                  MessageTypeDone,
	}
	if v, ok := mapping[s]; ok {
		*t = v
		return nil
	}
	return fmt.Errorf("未知的消息类型: %s", s)
}

// ──────────────────────────── 结构体 ────────────────────────────

// Message 通信消息结构体。
// 对齐 Python: Message (protocol.py)
type Message struct {
	// Type 消息类型
	Type MessageType `json:"type"`
	// Payload 消息载荷
	Payload any `json:"payload"`
	// Timestamp 时间戳
	Timestamp time.Time `json:"timestamp"`
	// MessageID 消息唯一标识
	MessageID string `json:"message_id"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SerializeMessage 序列化消息为 JSON 字节。
// 对齐 Python: serialize_message()
func SerializeMessage(msg Message) ([]byte, error) {
	return json.Marshal(msg)
}

// DeserializeMessage 反序列化 JSON 字节为消息。
// 对齐 Python: deserialize_message()
func DeserializeMessage(data []byte) (Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return Message{}, fmt.Errorf("反序列化消息失败: %w", err)
	}
	return msg, nil
}

// WriteMessage 写入消息到 io.Writer（JSON + \n）。
// 对齐 Python: serialize_message_to_stream()
func WriteMessage(w io.Writer, msg Message) error {
	data, err := SerializeMessage(msg)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("写入消息失败: %w", err)
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return fmt.Errorf("写入换行符失败: %w", err)
	}
	return nil
}

// ReadMessage 从 io.Reader 读取一行并反序列化为消息。
// 跳过非 JSON 行（子进程可能输出非协议日志到 stdout）。
// 对齐 Python: deserialize_message_from_stream()
func ReadMessage(r io.Reader) (Message, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		msg, err := DeserializeMessage(line)
		if err != nil {
			// 跳过非 JSON 行
			continue
		}
		return msg, nil
	}
	if err := scanner.Err(); err != nil {
		return Message{}, fmt.Errorf("读取消息失败: %w", err)
	}
	return Message{}, io.EOF
}

// NewMessage 创建新消息，自动设置时间戳和消息 ID。
func NewMessage(msgType MessageType, payload any) Message {
	return Message{
		Type:      msgType,
		Payload:   payload,
		Timestamp: time.Now(),
		MessageID: generateMessageID(),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// messageIDCounter 消息 ID 计数器。
var messageIDCounter uint64

// generateMessageID 生成消息唯一标识。
func generateMessageID() string {
	n := atomic.AddUint64(&messageIDCounter, 1)
	return fmt.Sprintf("msg-%d-%d", time.Now().UnixNano(), n)
}
