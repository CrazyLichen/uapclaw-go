package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseMessage 消息基接口，统一角色、内容、名称与元数据的读写入口。
type BaseMessage interface {
	// GetRole 获取消息角色
	GetRole() RoleType
	// SetRole 设置消息角色
	SetRole(role RoleType)
	// GetContent 获取消息内容
	GetContent() MessageContent
	// SetContent 设置消息内容
	SetContent(content MessageContent)
	// GetName 获取消息发送者名称
	GetName() string
	// SetName 设置消息发送者名称
	SetName(name string)
	// GetMetadata 获取附加元数据
	GetMetadata() map[string]any
	// SetMetadata 设置附加元数据
	SetMetadata(metadata map[string]any)
}

// ImageURL 图片 URL 信息
type ImageURL struct {
	// URL 图片地址
	URL string `json:"url"`
	// Detail 图片细节级别，如 "auto"/"low"/"high"
	Detail string `json:"detail,omitempty"`
}

// InputAudio 音频输入内容块数据（对应 OpenAI input_audio content block）
type InputAudio struct {
	// Data base64 编码的音频数据
	Data string `json:"data"`
	// Format 音频格式（mp3/wav/m4a 等）
	Format string `json:"format"`
}

// VideoURL 视频内容块 URL（对应 OpenAI video_url content block）
type VideoURL struct {
	// URL 视频 URL（http URL 或 data:URI）
	URL string `json:"url"`
}

// ContentPart 多模态消息内容分片
type ContentPart struct {
	// Type 内容类型，"text"、"image_url"、"input_audio"、"video_url" 等
	Type string `json:"type"`
	// Text 文本内容（Type=="text" 时使用）
	Text string `json:"text,omitempty"`
	// ImageURL 图片 URL 信息（Type=="image_url" 时使用）
	ImageURL *ImageURL `json:"image_url,omitempty"`
	// InputAudio 音频输入信息（Type=="input_audio" 时使用）
	InputAudio *InputAudio `json:"input_audio,omitempty"`
	// VideoURL 视频 URL 信息（Type=="video_url" 时使用）
	VideoURL *VideoURL `json:"video_url,omitempty"`
	// Width 图像宽度（像素），生产方缩图后标注，供 token 计费免嗅探
	Width *int `json:"width,omitempty"`
	// Height 图像高度（像素）
	Height *int `json:"height,omitempty"`
	// Dimensions 图像尺寸 "WxH"，与 Width/Height 二选一
	Dimensions string `json:"dimensions,omitempty"`
}

// MessageContent 消息内容容器：纯文本走 text，多模态走 parts，两者互斥。
type MessageContent struct {
	// text 纯文本内容（多模态时为空）
	text string
	// parts 多模态内容分片（纯文本时为 nil）
	parts []ContentPart
}

// DefaultMessage 通用消息实现，是各类角色消息的公共载体。
type DefaultMessage struct {
	// Role 消息角色
	Role RoleType `json:"role"`
	// Content 消息内容
	Content MessageContent `json:"content"`
	// Name 消息发送者名称（可选，function calling 场景）
	Name string `json:"name,omitempty"`
	// Metadata 附加元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// UserMessage 用户消息，复用 DefaultMessage 的字段与行为。
type UserMessage struct {
	DefaultMessage
}

// SystemMessage 系统消息，复用 DefaultMessage 的字段与行为。
type SystemMessage struct {
	DefaultMessage
}

// MessageOption 构造 DefaultMessage 时的可选项，用于注入角色、内容等字段。
type MessageOption func(*DefaultMessage)

// ──────────────────────────── 枚举 ────────────────────────────

// RoleType 消息角色枚举，取值与 OpenAI role 字段一一对应。
type RoleType int

// ──────────────────────────── 常量 ────────────────────────────

const (
	// RoleTypeSystem 系统消息
	RoleTypeSystem RoleType = iota
	// RoleTypeUser 用户消息
	RoleTypeUser
	// RoleTypeAssistant 助手消息
	RoleTypeAssistant
	// RoleTypeTool 工具返回消息
	RoleTypeTool
)

const (
	// MetadataKeyMessageSourceKind 消息来源细分（query / steering / empty_response / attachment 等）
	// Python: OPENJIUWEN_MESSAGE_SOURCE_KIND_METADATA (core/foundation/llm/schema/message.py:12)
	MetadataKeyMessageSourceKind = "_openjiuwen_message_source_kind"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var roleTypeStrings = [...]string{
	"system",
	"user",
	"assistant",
	"tool",
}

var roleTypeMap map[string]RoleType

// ──────────────────────────── 导出函数 ────────────────────────────

// GetRole 返回消息角色。
func (m *DefaultMessage) GetRole() RoleType { return m.Role }

// SetRole 设置消息角色。
func (m *DefaultMessage) SetRole(role RoleType) { m.Role = role }

// GetContent 返回消息内容。
func (m *DefaultMessage) GetContent() MessageContent { return m.Content }

// SetContent 设置消息内容。
func (m *DefaultMessage) SetContent(content MessageContent) { m.Content = content }

// GetName 返回消息名称（工具消息里指工具名）。
func (m *DefaultMessage) GetName() string { return m.Name }

// SetName 设置消息名称。
func (m *DefaultMessage) SetName(name string) { m.Name = name }

// GetMetadata 返回消息附带的元数据。
func (m *DefaultMessage) GetMetadata() map[string]any { return m.Metadata }

// SetMetadata 设置消息附带的元数据。
func (m *DefaultMessage) SetMetadata(metadata map[string]any) { m.Metadata = metadata }

// String 返回角色对应的字符串名称，超出枚举范围时返回带原始值的占位文本。
func (r RoleType) String() string {
	if int(r) >= 0 && int(r) < len(roleTypeStrings) {
		return roleTypeStrings[r]
	}
	return fmt.Sprintf("RoleType(%d)", int(r))
}

// MarshalJSON 把角色序列化成它的字符串名称。
func (r RoleType) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

// UnmarshalJSON 从字符串名称还原角色，大小写不敏感，未知名称直接报错。
func (r *RoleType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("RoleType 反序列化失败: %w", err)
	}
	rt, ok := roleTypeMap[strings.ToLower(s)]
	if !ok {
		return fmt.Errorf("未知的 RoleType: %q", s)
	}
	*r = rt
	return nil
}

// NewTextContent 构造纯文本消息内容。
func NewTextContent(text string) MessageContent {
	return MessageContent{text: text}
}

// NewMultiModalContent 构造多模态消息内容。
func NewMultiModalContent(parts ...ContentPart) MessageContent {
	return MessageContent{parts: parts}
}

// IsText 判断该内容是不是纯文本（没有多模态分段）。
func (c MessageContent) IsText() bool {
	return c.parts == nil
}

// Text 返回纯文本内容。
func (c MessageContent) Text() string {
	return c.text
}

// Parts 返回多模态分段内容。
func (c MessageContent) Parts() []ContentPart {
	return c.parts
}

// String 返回内容的可读形式：纯文本直接返回，多模态转成 JSON。
func (c MessageContent) String() string {
	if c.IsText() {
		return c.text
	}
	data, _ := json.Marshal(c.parts)
	return string(data)
}

// MarshalJSON 按内容形态分别序列化成字符串或数组。
func (c MessageContent) MarshalJSON() ([]byte, error) {
	if c.IsText() {
		return json.Marshal(c.text)
	}
	return json.Marshal(c.parts)
}

// UnmarshalJSON 兼容两种消息内容形态：纯文本字符串，或多模态分段数组。
func (c *MessageContent) UnmarshalJSON(data []byte) error {
	// 尝试解析为字符串（纯文本）
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		c.text = s
		c.parts = nil
		return nil
	}

	// 尝试解析为 ContentPart 数组（多模态）
	var parts []ContentPart
	if err := json.Unmarshal(data, &parts); err == nil {
		c.text = ""
		c.parts = parts
		return nil
	}

	return fmt.Errorf("MessageContent 反序列化失败: 既不是字符串也不是内容分片数组")
}

// WithMessageName 设置消息名称。
func WithMessageName(name string) MessageOption {
	return func(m *DefaultMessage) { m.Name = name }
}

// WithMetadata 设置消息的附加元数据。
func WithMetadata(metadata map[string]any) MessageOption {
	return func(m *DefaultMessage) { m.Metadata = metadata }
}

// WithMultiModalContent 把消息内容设置为多模态内容分片。
func WithMultiModalContent(parts ...ContentPart) MessageOption {
	return func(m *DefaultMessage) { m.Content = NewMultiModalContent(parts...) }
}

// NewDefaultMessage 构造通用消息，opts 可覆盖名称、元数据与多模态内容。
func NewDefaultMessage(role RoleType, content string, opts ...MessageOption) *DefaultMessage {
	msg := &DefaultMessage{
		Role:    role,
		Content: NewTextContent(content),
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}

// NewUserMessage 构造用户消息。
func NewUserMessage(content string, opts ...MessageOption) *UserMessage {
	msg := NewDefaultMessage(RoleTypeUser, content, opts...)
	return &UserMessage{DefaultMessage: *msg}
}

// NewSystemMessage 构造系统消息。
func NewSystemMessage(content string, opts ...MessageOption) *SystemMessage {
	msg := NewDefaultMessage(RoleTypeSystem, content, opts...)
	return &SystemMessage{DefaultMessage: *msg}
}

// BaseMessageToMap 把 BaseMessage 转成 map 表示（OpenAI 风格）。
// BaseMessage 接口没有 ToMap 方法，只能从接口字段手工构建；
// 供轨迹构建、会话信号等消费方复用，避免 role/content/tool_calls 映射多处漂移。
// AssistantMessage 额外输出扁平格式的 tool_calls。
func BaseMessageToMap(msg BaseMessage) map[string]any {
	if msg == nil {
		return map[string]any{}
	}
	result := map[string]any{
		"role":    msg.GetRole().String(),
		"content": msg.GetContent().String(),
	}
	if name := msg.GetName(); name != "" {
		result["name"] = name
	}
	if meta := msg.GetMetadata(); len(meta) > 0 {
		result["metadata"] = meta
	}
	if am, ok := msg.(*AssistantMessage); ok && len(am.ToolCalls) > 0 {
		calls := make([]map[string]any, 0, len(am.ToolCalls))
		for _, tc := range am.ToolCalls {
			tcMap := map[string]any{
				"name":      tc.Name,
				"arguments": tc.Arguments,
			}
			if tc.ID != "" {
				tcMap["id"] = tc.ID
			}
			if tc.Type != "" {
				tcMap["type"] = tc.Type
			}
			if tc.Index > 0 {
				tcMap["index"] = tc.Index
			}
			calls = append(calls, tcMap)
		}
		result["tool_calls"] = calls
	}
	return result
}

// UnmarshalMessage 先读 role 再按类型反序列化，返回对应的具体消息。
func UnmarshalMessage(data []byte) (BaseMessage, error) {
	var peek struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("UnmarshalMessage 解析 role 失败: %w", err)
	}
	switch peek.Role {
	case "user":
		var m UserMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalMessage 反序列化 UserMessage 失败: %w", err)
		}
		return &m, nil
	case "system":
		var m SystemMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalMessage 反序列化 SystemMessage 失败: %w", err)
		}
		return &m, nil
	case "assistant":
		var m AssistantMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalMessage 反序列化 AssistantMessage 失败: %w", err)
		}
		return &m, nil
	case "tool":
		var m ToolMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalMessage 反序列化 ToolMessage 失败: %w", err)
		}
		return &m, nil
	default:
		return nil, fmt.Errorf("UnmarshalMessage 不支持的角色: %q", peek.Role)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	roleTypeMap = make(map[string]RoleType, len(roleTypeStrings))
	for i, s := range roleTypeStrings {
		roleTypeMap[s] = RoleType(i)
	}
}
