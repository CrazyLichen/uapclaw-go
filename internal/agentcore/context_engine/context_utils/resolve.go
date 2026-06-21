package context_utils

import (
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractToolName 从 ToolCall 中提取工具名称。
//
// 优先返回 ToolCall.Name，为空时返回空字符串。
// Go 端 ToolCall 结构直接包含 Name 字段（与 Python 的 Function.Name 不同）。
//
// 对应 Python: ContextUtils.extract_tool_name()
func ExtractToolName(toolCall *llm_schema.ToolCall) string {
	if toolCall == nil {
		return ""
	}
	if toolCall.Name != "" {
		return toolCall.Name
	}
	return ""
}

// ResolveToolCallFromMessage 从 ToolMessage 回溯查找对应的 ToolCall 对象。
//
// 通过 ToolMessage 的 ToolCallID 匹配 AssistantMessage.ToolCalls 中的 ID，
// 从后往前遍历 contextMessages 查找最近的匹配。
//
// 对应 Python: ContextUtils.resolve_tool_call_from_message()
func ResolveToolCallFromMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) *llm_schema.ToolCall {
	tm, ok := message.(*llm_schema.ToolMessage)
	if !ok {
		return nil
	}
	toolCallID := tm.ToolCallID
	if toolCallID == "" {
		return nil
	}

	for i := len(contextMessages) - 1; i >= 0; i-- {
		am, ok := contextMessages[i].(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		for _, tc := range am.ToolCalls {
			if tc.ID == toolCallID {
				return tc
			}
		}
	}
	return nil
}

// ResolveToolNameFromMessage 从 ToolMessage 回溯查找对应的工具名称。
//
// 内部调用 ResolveToolCallFromMessage 找到 ToolCall，再调用 ExtractToolName 提取名称。
// 未找到时返回空字符串。
//
// 对应 Python: ContextUtils.resolve_tool_name_from_message()
func ResolveToolNameFromMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) string {
	toolCall := ResolveToolCallFromMessage(message, contextMessages)
	return ExtractToolName(toolCall)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
