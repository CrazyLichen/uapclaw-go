package schema

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Message 消息结构体，描述一次请求/响应/事件的完整信息。
//
// 对齐 Python: Message dataclass (message.py)，
// 核心字段与 Python 一一对应，可选字段按类别分组。
type Message struct {
	// ─── 必填字段 ───

	// ID 消息唯一标识（UUID v4，32 hex 无连字符）
	ID string `json:"id"`
	// Type 消息方向：req/res/event
	Type MessageType `json:"type"`
	// ChannelID 来源渠道标识
	ChannelID string `json:"channel_id"`
	// SessionID 会话标识（req 中总是非空，res/event 中可能为空）
	SessionID string `json:"session_id"`
	// Params 请求参数（req 用 json.RawMessage 延迟解析，res/event 为空）
	Params json.RawMessage `json:"params"`
	// Timestamp Unix 秒时间戳（含小数精度，对齐 Python time.time()）
	Timestamp float64 `json:"timestamp"`
	// OK 是否成功
	OK bool `json:"ok"`

	// ─── 可选字段（字符串/bool 不加 omitempty，保留零值输出） ───

	// Provider IM 平台名称（仅 IM 渠道 req 使用）
	Provider string `json:"provider"`
	// ChatID IM 聊天标识
	ChatID string `json:"chat_id"`
	// UserID 发送者标识
	UserID string `json:"user_id"`
	// BotID 机器人标识
	BotID string `json:"bot_id"`
	// ReqMethod 请求方法（仅 req 使用）
	ReqMethod ReqMethod `json:"req_method"`
	// EventType 事件类型（仅 event/res 使用）
	EventType EventType `json:"event_type"`
	// Mode 运行模式（仅 req 使用）
	Mode Mode `json:"mode"`
	// IsStream 是否流式请求（仅 req 使用）
	IsStream bool `json:"is_stream"`
	// StreamSeq 流式序号（0 表示未设置，实际序号从 1 开始）
	StreamSeq int `json:"stream_seq"`
	// StreamID 流式标识（空串表示未设置）
	StreamID string `json:"stream_id"`
	// GroupDigitalAvatar 数字分身群聊模式
	GroupDigitalAvatar bool `json:"group_digital_avatar"`
	// EnableMemory 是否启用记忆
	EnableMemory bool `json:"enable_memory"`
	// EnableStreaming 是否启用流式输出
	// 注意：Python 默认 True，Go bool 零值 False；工厂函数显式设 True 对齐 Python
	EnableStreaming bool `json:"enable_streaming"`

	// ─── 可选字段（指针/切片/map 加 omitempty） ───

	// Payload 响应/事件负载（res/event 用，req 为 nil，对齐 Python payload: dict | None）
	Payload map[string]any `json:"payload,omitempty"`
	// Metadata 扩展元数据（Gateway 需要主动读写内部字段）
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MessageOption 消息构造选项函数。
type MessageOption func(*Message)

// ──────────────────────────── 枚举 ────────────────────────────

// MessageType 消息类型枚举（req/res/event）。
type MessageType string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MessageTypeReq 请求消息
	MessageTypeReq MessageType = "req"
	// MessageTypeRes 响应消息
	MessageTypeRes MessageType = "res"
	// MessageTypeEvent 事件消息
	MessageTypeEvent MessageType = "event"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// messageTypeLookup 消息类型字符串→枚举值查找表。
var messageTypeLookup map[string]MessageType

// ──────────────────────────── 导出函数 ────────────────────────────

// AllMessageTypes 返回所有消息类型列表。
func AllMessageTypes() []MessageType {
	return []MessageType{
		MessageTypeReq,
		MessageTypeRes,
		MessageTypeEvent,
	}
}

// ParseMessageType 解析字符串为 MessageType。
func ParseMessageType(s string) (MessageType, error) {
	if mt, ok := messageTypeLookup[s]; ok {
		return mt, nil
	}
	return MessageType(""), fmt.Errorf("不合法的 MessageType 值: %q", s)
}

// IsValidMessageType 判断字符串是否为有效的 MessageType。
func IsValidMessageType(s string) bool {
	_, ok := messageTypeLookup[s]
	return ok
}

// Validate 校验消息必填字段。
func (m *Message) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("消息 id 不能为空")
	}
	if !IsValidMessageType(string(m.Type)) {
		return fmt.Errorf("不合法的 MessageType 值: %q", m.Type)
	}
	if m.ChannelID == "" {
		return fmt.Errorf("channel_id 不能为空")
	}
	if m.Type == MessageTypeReq && m.ReqMethod == "" {
		return fmt.Errorf("请求消息 req_method 不能为空")
	}
	if m.Type == MessageTypeEvent && m.EventType == "" {
		return fmt.Errorf("事件消息 event_type 不能为空")
	}
	return nil
}

// NewMessageID 生成消息唯一标识（UUID v4，32 hex 无连字符）。
func NewMessageID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// NowTimestamp 返回当前 Unix 秒时间戳（含小数精度）。
func NowTimestamp() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// WithMode 设置运行模式的选项。
func WithMode(m Mode) MessageOption {
	return func(msg *Message) { msg.Mode = m }
}

// WithIsStream 设置是否流式请求的选项。
func WithIsStream(v bool) MessageOption {
	return func(msg *Message) { msg.IsStream = v }
}

// WithProvider 设置 IM 平台名称的选项。
func WithProvider(p string) MessageOption {
	return func(msg *Message) { msg.Provider = p }
}

// WithChatID 设置 IM 聊天标识的选项。
func WithChatID(id string) MessageOption {
	return func(msg *Message) { msg.ChatID = id }
}

// WithUserID 设置发送者标识的选项。
func WithUserID(id string) MessageOption {
	return func(msg *Message) { msg.UserID = id }
}

// WithBotID 设置机器人标识的选项。
func WithBotID(id string) MessageOption {
	return func(msg *Message) { msg.BotID = id }
}

// WithMetadata 设置扩展元数据的选项。
func WithMetadata(m map[string]any) MessageOption {
	return func(msg *Message) { msg.Metadata = m }
}

// WithGroupDigitalAvatar 设置数字分身群聊模式的选项。
func WithGroupDigitalAvatar(v bool) MessageOption {
	return func(msg *Message) { msg.GroupDigitalAvatar = v }
}

// WithEnableMemory 设置是否启用记忆的选项。
func WithEnableMemory(v bool) MessageOption {
	return func(msg *Message) { msg.EnableMemory = v }
}

// WithEnableStreaming 设置是否启用流式输出的选项。
func WithEnableStreaming(v bool) MessageOption {
	return func(msg *Message) { msg.EnableStreaming = v }
}

// WithSessionID 设置会话标识的选项。
func WithSessionID(id string) MessageOption {
	return func(msg *Message) { msg.SessionID = id }
}

// WithStreamSeq 设置流式序号的选项。
func WithStreamSeq(seq int) MessageOption {
	return func(msg *Message) { msg.StreamSeq = seq }
}

// WithStreamID 设置流式标识的选项。
func WithStreamID(id string) MessageOption {
	return func(msg *Message) { msg.StreamID = id }
}

// WithEventType 设置事件类型的选项。
func WithEventType(et EventType) MessageOption {
	return func(msg *Message) { msg.EventType = et }
}

// NewReqMessage 创建请求消息。
func NewReqMessage(channelID, sessionID string, reqMethod ReqMethod, params json.RawMessage, opts ...MessageOption) *Message {
	msg := &Message{
		ID:              NewMessageID(),
		Type:            MessageTypeReq,
		ChannelID:       channelID,
		SessionID:       sessionID,
		Params:          params,
		Timestamp:       NowTimestamp(),
		OK:              true,
		ReqMethod:       reqMethod,
		EnableStreaming: true,
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}

// NewResMessage 创建响应消息。
func NewResMessage(channelID, sessionID string, ok bool, payload map[string]any, opts ...MessageOption) *Message {
	msg := &Message{
		ID:              NewMessageID(),
		Type:            MessageTypeRes,
		ChannelID:       channelID,
		SessionID:       sessionID,
		Timestamp:       NowTimestamp(),
		OK:              ok,
		Payload:         payload,
		EnableStreaming: true,
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}

// NewEventMessage 创建事件消息。
func NewEventMessage(channelID, sessionID string, eventType EventType, payload map[string]any, opts ...MessageOption) *Message {
	msg := &Message{
		ID:              NewMessageID(),
		Type:            MessageTypeEvent,
		ChannelID:       channelID,
		SessionID:       sessionID,
		Timestamp:       NowTimestamp(),
		OK:              true,
		EventType:       eventType,
		Payload:         payload,
		EnableStreaming: true,
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}

// String 返回 MessageType 的字符串表示。
func (mt MessageType) String() string {
	return string(mt)
}

// GoString 返回 MessageType 的 Go 语法表示。
func (mt MessageType) GoString() string {
	return fmt.Sprintf("schema.MessageType(%q)", string(mt))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 构建 MessageType 查找表
	mts := AllMessageTypes()
	messageTypeLookup = make(map[string]MessageType, len(mts))
	for _, mt := range mts {
		messageTypeLookup[string(mt)] = mt
	}
}
