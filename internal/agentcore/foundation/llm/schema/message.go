package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseMessage 消息基类接口，所有消息类型均实现此接口。
//
// 对应 Python: BaseMessage (Pydantic BaseModel)，作为所有消息类型的基类。
// Go 端使用接口替代 Python 的类继承，实现统一的多态访问。
//
// 接口包含 getter + setter，支持消息创建后的字段修改
// （与当前代码中 msg.Content = ... 的直接赋值行为一致）。
// 具体类型仍可直接通过字段名访问（如 msg.Role），通过接口访问时使用 getter/setter。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/message.py (BaseMessage)
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

// ImageURL 图片 URL 信息，用于多模态消息中的图片分片。
//
// 对应 OpenAI API 的 image_url 对象格式。
type ImageURL struct {
	// URL 图片地址
	URL string `json:"url"`
	// Detail 图片细节级别，如 "auto"/"low"/"high"
	Detail string `json:"detail,omitempty"`
}

// ContentPart 多模态内容分片，表示文本或嵌入资源。
//
// 对应 Python: Union[str, dict] 中的元素。Go 端使用结构化定义，
// 后续新的多模态类型（如 audio、video）可扩展此结构体。
type ContentPart struct {
	// Type 内容类型，"text" 或 "image_url" 等
	Type string `json:"type"`
	// Text 文本内容（Type=="text" 时使用）
	Text string `json:"text,omitempty"`
	// ImageURL 图片 URL 信息（Type=="image_url" 时使用）
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// MessageContent 消息内容，支持纯文本和多模态两种格式。
//
// Python 端类型为 Union[str, List[Union[str, dict]]]。
// Go 端使用自定义类型封装序列化逻辑：
//
//   - 纯文本：text 字段有值，parts 为 nil
//   - 多模态：text 为空字符串，parts 包含多个 ContentPart
//
// 序列化规则：
//   - 纯文本 → JSON string
//   - 多模态 → JSON array
//
// 反序列化规则：
//   - JSON string → 纯文本
//   - JSON array → 多模态
type MessageContent struct {
	// text 纯文本内容（多模态时为空）
	text string
	// parts 多模态内容分片（纯文本时为 nil）
	parts []ContentPart
}

// DefaultMessage BaseMessage 接口的默认实现，提供 Role/Content/Name/Metadata 四个基础字段。
//
// 其他消息类型（UserMessage/SystemMessage/AssistantMessage/ToolMessage）
// 通过嵌入 DefaultMessage 复用基础字段和 BaseMessage 接口实现。
//
// 对应 Python: BaseMessage(role=..., content=..., name=..., metadata=...)
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

// UserMessage 用户消息，role 固定为 "user"。
//
// 对应 Python: UserMessage(BaseMessage)
type UserMessage struct {
	DefaultMessage
}

// SystemMessage 系统消息，role 固定为 "system"。
//
// 对应 Python: SystemMessage(BaseMessage)
type SystemMessage struct {
	DefaultMessage
}

// ──────────────────────────── 枚举 ────────────────────────────

// RoleType 消息角色类型枚举，标识消息发送者的身份。
//
// 对应 Python: BaseMessage.role 字段的字符串值
type RoleType int

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

// MessageOption DefaultMessage 构造选项函数。
type MessageOption func(*DefaultMessage)

// ──────────────────────────── 全局变量 ────────────────────────────

// roleTypeStrings RoleType 枚举值对应的字符串表示，与 Python 端保持一致。
var roleTypeStrings = [...]string{
	"system",
	"user",
	"assistant",
	"tool",
}

// roleTypeMap 字符串到 RoleType 的映射，用于 JSON 反序列化。
var roleTypeMap map[string]RoleType

// ──────────────────────────── 导出函数 ────────────────────────────

// GetRole 返回消息角色
func (m *DefaultMessage) GetRole() RoleType { return m.Role }

// SetRole 设置消息角色
func (m *DefaultMessage) SetRole(role RoleType) { m.Role = role }

// GetContent 返回消息内容
func (m *DefaultMessage) GetContent() MessageContent { return m.Content }

// SetContent 设置消息内容
func (m *DefaultMessage) SetContent(content MessageContent) { m.Content = content }

// GetName 返回消息发送者名称
func (m *DefaultMessage) GetName() string { return m.Name }

// SetName 设置消息发送者名称
func (m *DefaultMessage) SetName(name string) { m.Name = name }

// GetMetadata 返回附加元数据
func (m *DefaultMessage) GetMetadata() map[string]any { return m.Metadata }

// SetMetadata 设置附加元数据
func (m *DefaultMessage) SetMetadata(metadata map[string]any) { m.Metadata = metadata }

// String 实现 fmt.Stringer 接口，返回 RoleType 的字符串表示。
func (r RoleType) String() string {
	if int(r) >= 0 && int(r) < len(roleTypeStrings) {
		return roleTypeStrings[r]
	}
	return fmt.Sprintf("RoleType(%d)", int(r))
}

// MarshalJSON 实现 json.Marshaler 接口，将 RoleType 序列化为字符串。
func (r RoleType) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口，将字符串反序列化为 RoleType。
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

// NewTextContent 创建纯文本消息内容。
func NewTextContent(text string) MessageContent {
	return MessageContent{text: text}
}

// NewMultiModalContent 创建多模态消息内容。
func NewMultiModalContent(parts ...ContentPart) MessageContent {
	return MessageContent{parts: parts}
}

// IsText 是否为纯文本内容。
func (c MessageContent) IsText() bool {
	return c.parts == nil
}

// Text 返回文本内容（纯文本模式）。
func (c MessageContent) Text() string {
	return c.text
}

// Parts 返回多模态分片（多模态模式）。
func (c MessageContent) Parts() []ContentPart {
	return c.parts
}

// String 返回内容的字符串表示。
func (c MessageContent) String() string {
	if c.IsText() {
		return c.text
	}
	data, _ := json.Marshal(c.parts)
	return string(data)
}

// MarshalJSON 实现 json.Marshaler 接口 — 纯文本序列化为 string，多模态序列化为 array。
func (c MessageContent) MarshalJSON() ([]byte, error) {
	if c.IsText() {
		return json.Marshal(c.text)
	}
	return json.Marshal(c.parts)
}

// UnmarshalJSON 实现 json.Unmarshaler 接口 — string 反序列化为纯文本，array 反序列化为多模态。
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

// WithMessageName 设置消息发送者名称。
func WithMessageName(name string) MessageOption {
	return func(m *DefaultMessage) { m.Name = name }
}

// WithMetadata 设置附加元数据。
func WithMetadata(metadata map[string]any) MessageOption {
	return func(m *DefaultMessage) { m.Metadata = metadata }
}

// WithMultiModalContent 设置多模态内容。
func WithMultiModalContent(parts ...ContentPart) MessageOption {
	return func(m *DefaultMessage) { m.Content = NewMultiModalContent(parts...) }
}

// NewDefaultMessage 创建 DefaultMessage 实例。
//
// 对应 Python: BaseMessage(role=..., content=..., name=..., metadata=...)
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

// NewUserMessage 创建用户消息，role 固定为 "user"。
//
// 对应 Python: UserMessage(content=...)
func NewUserMessage(content string, opts ...MessageOption) *UserMessage {
	msg := NewDefaultMessage(RoleTypeUser, content, opts...)
	return &UserMessage{DefaultMessage: *msg}
}

// NewSystemMessage 创建系统消息，role 固定为 "system"。
//
// 对应 Python: SystemMessage(content=...)
func NewSystemMessage(content string, opts ...MessageOption) *SystemMessage {
	msg := NewDefaultMessage(RoleTypeSystem, content, opts...)
	return &SystemMessage{DefaultMessage: *msg}
}

// UnmarshalMessage 从 JSON 反序列化为对应消息类型。
// 根据 role 字段自动分派：
//   - "user" → *UserMessage
//   - "system" → *SystemMessage
//   - "assistant" → *AssistantMessage
//   - "tool" → *ToolMessage
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

// init 初始化 roleTypeMap，用于 JSON 反序列化
func init() {
	roleTypeMap = make(map[string]RoleType, len(roleTypeStrings))
	for i, s := range roleTypeStrings {
		roleTypeMap[s] = RoleType(i)
	}
}
