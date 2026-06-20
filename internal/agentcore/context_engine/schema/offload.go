package schema

import (
	"encoding/json"
	"fmt"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// Offloadable 卸载消息接口，等价 Python isinstance(msg, OffloadMixin)。
//
// 4 个 Offload 子类型均实现此接口，用于防重复卸载检查：
//
//	_, ok := msg.(Offloadable)
//
// 对应 Python: isinstance(message, OffloadMixin)
type Offloadable interface {
	llm_schema.BaseMessage
	// GetOffloadInfo 获取卸载元数据
	GetOffloadInfo() OffloadInfo
}

// ──────────────────────────── 结构体 ────────────────────────────

// OffloadInfo 卸载元数据，等价 Python OffloadMixin。
//
// 当对话消息被卸载到外部存储时，OffloadInfo 携带检索句柄和存储后端类型，
// 使 LLM 可以通过 reloader_tool 按需取回原始内容。
//
// 对应 Python: openjiuwen/core/context_engine/schema/messages.py (OffloadMixin)
type OffloadInfo struct {
	// OffloadType 存储后端类型："in_memory" 或 "filesystem"
	OffloadType string `json:"offload_type"`
	// OffloadHandle 唯一检索标识：内存模式为 UUID hex，文件系统模式为文件路径
	OffloadHandle string `json:"offload_handle"`
	// Metadata 附加元数据（原始 token 数、时间戳、内容类型等）
	Metadata map[string]any `json:"metadata,omitempty"`
}

// OffloadUserMessage 卸载的用户消息占位符。
//
// 对应 Python: OffloadUserMessage(UserMessage, OffloadMixin)
type OffloadUserMessage struct {
	llm_schema.UserMessage
	OffloadInfo
}

// OffloadAssistantMessage 卸载的助手消息占位符。
//
// 对应 Python: OffloadAssistantMessage(AssistantMessage, OffloadMixin)
type OffloadAssistantMessage struct {
	llm_schema.AssistantMessage
	OffloadInfo
}

// OffloadSystemMessage 卸载的系统消息占位符。
//
// 对应 Python: OffloadSystemMessage(SystemMessage, OffloadMixin)
type OffloadSystemMessage struct {
	llm_schema.SystemMessage
	OffloadInfo
}

// OffloadToolMessage 卸载的工具消息占位符。
//
// 对应 Python: OffloadToolMessage(ToolMessage, OffloadMixin)
type OffloadToolMessage struct {
	llm_schema.ToolMessage
	OffloadInfo
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetOffloadInfo 实现 Offloadable 接口
func (m *OffloadUserMessage) GetOffloadInfo() OffloadInfo      { return m.OffloadInfo }
func (m *OffloadAssistantMessage) GetOffloadInfo() OffloadInfo  { return m.OffloadInfo }
func (m *OffloadSystemMessage) GetOffloadInfo() OffloadInfo     { return m.OffloadInfo }
func (m *OffloadToolMessage) GetOffloadInfo() OffloadInfo       { return m.OffloadInfo }

// NewOffloadUserMessage 创建卸载的用户消息
func NewOffloadUserMessage(content, handle, offloadType string, opts ...llm_schema.MessageOption) *OffloadUserMessage {
	msg := llm_schema.NewUserMessage(content, opts...)
	return &OffloadUserMessage{
		UserMessage: *msg,
		OffloadInfo: OffloadInfo{
			OffloadType:   offloadType,
			OffloadHandle: handle,
		},
	}
}

// NewOffloadSystemMessage 创建卸载的系统消息
func NewOffloadSystemMessage(content, handle, offloadType string, opts ...llm_schema.MessageOption) *OffloadSystemMessage {
	msg := llm_schema.NewSystemMessage(content, opts...)
	return &OffloadSystemMessage{
		SystemMessage: *msg,
		OffloadInfo: OffloadInfo{
			OffloadType:   offloadType,
			OffloadHandle: handle,
		},
	}
}

// NewOffloadToolMessage 创建卸载的工具消息
func NewOffloadToolMessage(toolCallID, content, handle, offloadType string, opts ...llm_schema.MessageOption) *OffloadToolMessage {
	msg := llm_schema.NewToolMessage(toolCallID, content, opts...)
	return &OffloadToolMessage{
		ToolMessage: *msg,
		OffloadInfo: OffloadInfo{
			OffloadType:   offloadType,
			OffloadHandle: handle,
		},
	}
}

// NewOffloadAssistantMessage 创建卸载的助手消息
func NewOffloadAssistantMessage(content, handle, offloadType string, opts ...llm_schema.AssistantMessageOption) *OffloadAssistantMessage {
	msg := llm_schema.NewAssistantMessage(content, opts...)
	return &OffloadAssistantMessage{
		AssistantMessage: *msg,
		OffloadInfo: OffloadInfo{
			OffloadType:   offloadType,
			OffloadHandle: handle,
		},
	}
}

// NewOffloadMessage 工厂函数，根据 role 自动分派创建对应 Offload 子类型。
// 等价 Python: create_offload_message(role, content, offload_handle, offload_type, **kwargs)
func NewOffloadMessage(role llm_schema.RoleType, content, handle, offloadType string, opts ...llm_schema.MessageOption) Offloadable {
	switch role {
	case llm_schema.RoleTypeAssistant:
		// AssistantMessage 使用 AssistantMessageOption，此处使用默认构造
		return NewOffloadAssistantMessage(content, handle, offloadType)
	case llm_schema.RoleTypeTool:
		return NewOffloadToolMessage("", content, handle, offloadType, opts...)
	case llm_schema.RoleTypeSystem:
		return NewOffloadSystemMessage(content, handle, offloadType, opts...)
	default:
		return NewOffloadUserMessage(content, handle, offloadType, opts...)
	}
}

// IsOffloaded 检查消息是否为已卸载的占位符。
// 等价 Python: isinstance(message, OffloadMixin)，用于处理器防重复卸载。
func IsOffloaded(msg llm_schema.BaseMessage) bool {
	_, ok := msg.(Offloadable)
	return ok
}

// MarshalJSON 实现 json.Marshaler 接口，合并 AssistantMessage 和 OffloadInfo 字段。
func (m *OffloadAssistantMessage) MarshalJSON() ([]byte, error) {
	// 序列化内层 AssistantMessage（调用其自定义 MarshalJSON）
	inner, err := json.Marshal(&m.AssistantMessage)
	if err != nil {
		return nil, fmt.Errorf("OffloadAssistantMessage 序列化内层失败: %w", err)
	}
	// 序列化 OffloadInfo
	outer, err := json.Marshal(&m.OffloadInfo)
	if err != nil {
		return nil, fmt.Errorf("OffloadAssistantMessage 序列化 OffloadInfo 失败: %w", err)
	}
	// 合并两个 JSON 对象
	var baseMap, extraMap map[string]json.RawMessage
	if err := json.Unmarshal(inner, &baseMap); err != nil {
		return nil, fmt.Errorf("OffloadAssistantMessage 合并解析内层失败: %w", err)
	}
	if err := json.Unmarshal(outer, &extraMap); err != nil {
		return nil, fmt.Errorf("OffloadAssistantMessage 合并解析 OffloadInfo 失败: %w", err)
	}
	for k, v := range extraMap {
		baseMap[k] = v
	}
	return json.Marshal(baseMap)
}

// UnmarshalJSON 实现 json.Unmarshaler 接口，解析 AssistantMessage 和 OffloadInfo 字段。
func (m *OffloadAssistantMessage) UnmarshalJSON(data []byte) error {
	// 先反序列化 AssistantMessage 部分
	if err := json.Unmarshal(data, &m.AssistantMessage); err != nil {
		return fmt.Errorf("OffloadAssistantMessage 反序列化内层失败: %w", err)
	}
	// 再反序列化 OffloadInfo 部分
	if err := json.Unmarshal(data, &m.OffloadInfo); err != nil {
		return fmt.Errorf("OffloadAssistantMessage 反序列化 OffloadInfo 失败: %w", err)
	}
	return nil
}

// UnmarshalOffloadMessage 从 JSON 反序列化为对应 Offload 子类型。
// 根据 role 字段自动分派。
func UnmarshalOffloadMessage(data []byte) (Offloadable, error) {
	var peek struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("UnmarshalOffloadMessage 解析 role 失败: %w", err)
	}
	switch peek.Role {
	case "user":
		var m OffloadUserMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalOffloadMessage 反序列化 OffloadUserMessage 失败: %w", err)
		}
		return &m, nil
	case "system":
		var m OffloadSystemMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalOffloadMessage 反序列化 OffloadSystemMessage 失败: %w", err)
		}
		return &m, nil
	case "assistant":
		var m OffloadAssistantMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalOffloadMessage 反序列化 OffloadAssistantMessage 失败: %w", err)
		}
		return &m, nil
	case "tool":
		var m OffloadToolMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalOffloadMessage 反序列化 OffloadToolMessage 失败: %w", err)
		}
		return &m, nil
	default:
		return nil, fmt.Errorf("UnmarshalOffloadMessage 不支持的角色: %q", peek.Role)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
