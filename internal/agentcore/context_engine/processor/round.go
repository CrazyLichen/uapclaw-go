package processor

import (
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GroupCompletedAPIRounds 将消息列表按已完成的 API 轮次分组，
// 返回每个轮次的 [start, end) 半开区间列表。
//
// 核心逻辑：
//   - 遇到 AssistantMessage 不含 tool_calls → 一轮完成
//   - 遇到 AssistantMessage 含 tool_calls → 收集 ID，等待 ToolMessage 回复
//   - 所有 pending tool_call_id 收到回复 → 一轮完成
//   - 遇到 UserMessage 且无 pending → 开始新轮次
//
// 对应 Python: openjiuwen/core/context_engine/context/session_memory_manager.py
//
//	(group_completed_api_rounds)
func GroupCompletedAPIRounds(messages []llm_schema.BaseMessage) [][2]int {
	var rounds [][2]int
	currentStart := -1
	var pendingToolCallIDs map[string]bool

	for index, message := range messages {
		if currentStart == -1 {
			currentStart = index
		} else if isUserMessage(message) && len(pendingToolCallIDs) == 0 {
			currentStart = index
		}

		if isAssistantMessage(message) {
			toolCalls := getToolCalls(message)
			if len(toolCalls) > 0 {
				pendingToolCallIDs = make(map[string]bool)
				hasValidID := false
				for _, tc := range toolCalls {
					if tc.ID != "" {
						pendingToolCallIDs[tc.ID] = true
						hasValidID = true
					}
				}
				if !hasValidID {
					// tool_calls 的 ID 全为空，直接视为一轮完成
					rounds = append(rounds, [2]int{currentStart, index + 1})
					currentStart = -1
					pendingToolCallIDs = nil
				}
				continue
			}
			// AssistantMessage 不含 tool_calls → 一轮完成
			rounds = append(rounds, [2]int{currentStart, index + 1})
			currentStart = -1
			pendingToolCallIDs = nil
			continue
		}

		if isToolMessage(message) && len(pendingToolCallIDs) > 0 {
			toolCallID := getToolCallID(message)
			if toolCallID != "" {
				delete(pendingToolCallIDs, toolCallID)
			}
			if len(pendingToolCallIDs) == 0 {
				rounds = append(rounds, [2]int{currentStart, index + 1})
				currentStart = -1
			}
		}
	}

	return rounds
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isUserMessage 判断消息是否为用户消息
func isUserMessage(msg llm_schema.BaseMessage) bool {
	return msg.GetRole() == llm_schema.RoleTypeUser
}

// isAssistantMessage 判断消息是否为助手消息
func isAssistantMessage(msg llm_schema.BaseMessage) bool {
	return msg.GetRole() == llm_schema.RoleTypeAssistant
}

// isToolMessage 判断消息是否为工具消息
func isToolMessage(msg llm_schema.BaseMessage) bool {
	return msg.GetRole() == llm_schema.RoleTypeTool
}

// getToolCalls 从 AssistantMessage 中获取 tool_calls
func getToolCalls(msg llm_schema.BaseMessage) []*llm_schema.ToolCall {
	am, ok := msg.(*llm_schema.AssistantMessage)
	if !ok {
		return nil
	}
	return am.ToolCalls
}

// getToolCallID 从 ToolMessage 中获取 tool_call_id
func getToolCallID(msg llm_schema.BaseMessage) string {
	tm, ok := msg.(*llm_schema.ToolMessage)
	if !ok {
		return ""
	}
	return tm.ToolCallID
}
