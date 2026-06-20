package schema

// ──────────────────────────── 结构体 ────────────────────────────

// ToolMessage 工具返回消息，关联 AssistantMessage.ToolCalls 中的 ID。
//
// ToolCallID 为必填字段，用于关联到 AssistantMessage.ToolCalls 中对应 ToolCall 的 ID。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/message.py (ToolMessage)
type ToolMessage struct {
	DefaultMessage
	// ToolCallID 关联的工具调用 ID，对应 ToolCall.ID
	ToolCallID string `json:"tool_call_id"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolMessage 创建工具返回消息，role 固定为 "tool"。
//
// 对应 Python: ToolMessage(tool_call_id=..., content=...)
func NewToolMessage(toolCallID, content string, opts ...MessageOption) *ToolMessage {
	msg := NewDefaultMessage(RoleTypeTool, content, opts...)
	return &ToolMessage{
		DefaultMessage: *msg,
		ToolCallID:     toolCallID,
	}
}
