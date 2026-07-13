package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

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

type ImageURL struct {
	// URL 图片地址
	URL string `json:"url"`
	// Detail 图片细节级别，如 "auto"/"low"/"high"
	Detail string `json:"detail,omitempty"`
}

type ContentPart struct {
	// Type 内容类型，"text" 或 "image_url" 等
	Type string `json:"type"`
	// Text 文本内容（Type=="text" 时使用）
	Text string `json:"text,omitempty"`
	// ImageURL 图片 URL 信息（Type=="image_url" 时使用）
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type MessageContent struct {
	// text 纯文本内容（多模态时为空）
	text string
	// parts 多模态内容分片（纯文本时为 nil）
	parts []ContentPart
}

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

type UserMessage struct {
	DefaultMessage
}

type SystemMessage struct {
	DefaultMessage
}

// ──────────────────────────── 枚举 ────────────────────────────

type RoleType int

type MessageOption func(*DefaultMessage)

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

// ──────────────────────────── 全局变量 ────────────────────────────

var roleTypeStrings = [...]string{
	"system",
	"user",
	"assistant",
	"tool",
}

var roleTypeMap map[string]RoleType

// ──────────────────────────── 导出函数 ────────────────────────────

func (m *DefaultMessage) GetRole() RoleType { return m.Role }

func (m *DefaultMessage) SetRole(role RoleType) { m.Role = role }

func (m *DefaultMessage) GetContent() MessageContent { return m.Content }

func (m *DefaultMessage) SetContent(content MessageContent) { m.Content = content }

func (m *DefaultMessage) GetName() string { return m.Name }

func (m *DefaultMessage) SetName(name string) { m.Name = name }

func (m *DefaultMessage) GetMetadata() map[string]any { return m.Metadata }

func (m *DefaultMessage) SetMetadata(metadata map[string]any) { m.Metadata = metadata }

func (r RoleType) String() string {
	if int(r) >= 0 && int(r) < len(roleTypeStrings) {
		return roleTypeStrings[r]
	}
	return fmt.Sprintf("RoleType(%d)", int(r))
}

func (r RoleType) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

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

func NewTextContent(text string) MessageContent {
	return MessageContent{text: text}
}

func NewMultiModalContent(parts ...ContentPart) MessageContent {
	return MessageContent{parts: parts}
}

func (c MessageContent) IsText() bool {
	return c.parts == nil
}

func (c MessageContent) Text() string {
	return c.text
}

func (c MessageContent) Parts() []ContentPart {
	return c.parts
}

func (c MessageContent) String() string {
	if c.IsText() {
		return c.text
	}
	data, _ := json.Marshal(c.parts)
	return string(data)
}

func (c MessageContent) MarshalJSON() ([]byte, error) {
	if c.IsText() {
		return json.Marshal(c.text)
	}
	return json.Marshal(c.parts)
}

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

func WithMessageName(name string) MessageOption {
	return func(m *DefaultMessage) { m.Name = name }
}

func WithMetadata(metadata map[string]any) MessageOption {
	return func(m *DefaultMessage) { m.Metadata = metadata }
}

func WithMultiModalContent(parts ...ContentPart) MessageOption {
	return func(m *DefaultMessage) { m.Content = NewMultiModalContent(parts...) }
}

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

func NewUserMessage(content string, opts ...MessageOption) *UserMessage {
	msg := NewDefaultMessage(RoleTypeUser, content, opts...)
	return &UserMessage{DefaultMessage: *msg}
}

func NewSystemMessage(content string, opts ...MessageOption) *SystemMessage {
	msg := NewDefaultMessage(RoleTypeSystem, content, opts...)
	return &SystemMessage{DefaultMessage: *msg}
}

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
