package processor

import (
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// DialogueRound 对话轮次，[0]=userIdx, [1]=assistantIdx（nil 表示不完整轮次）。
//
// 一轮对话定义为：user 消息 → 下一条不含 tool_calls 的 assistant 消息。
// 不完整轮次（有 user 无 assistant）的 assistantIdx 为 nil。
//
// 对应 Python: ContextUtils.find_all_dialogue_round() 返回的单个 [user_idx, assistant_idx]
type DialogueRound [2]*int

// ──────────────────────────── 导出函数 ────────────────────────────

// GetToolCallID 从消息中提取 tool_call_id。
//
// 仅 ToolMessage 类型有此字段，其他类型返回空字符串。
func GetToolCallID(msg llm_schema.BaseMessage) string {
	return getToolCallID(msg)
}

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
//	(已完成API轮次分组)
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

// FindAllDialogueRound 查找所有对话轮次边界。
//
// 从后往前扫描消息列表，识别 user → assistant(无 tool_calls) 的轮次。
// 返回从新到旧排列的轮次列表。连续的 user 消息被视为同组的起始。
//
// 对应 Python: ContextUtils.find_all_dialogue_round()
func FindAllDialogueRound(messages []llm_schema.BaseMessage) []DialogueRound {
	var rounds []DialogueRound
	i := len(messages) - 1

	findContiguousUserGroupStart := func(userIdx int) int {
		for userIdx-1 >= 0 && isUserMessage(messages[userIdx-1]) {
			userIdx--
		}
		return userIdx
	}

	for i >= 0 {
		// 查找该轮的 assistant（可能不存在）
		assistantIdx := (*int)(nil)
		roundEnd := i

		// 跳过非 assistant 消息
		for i >= 0 && !isAssistantMessage(messages[i]) {
			i--
		}

		if i >= 0 {
			msg := messages[i]
			hasToolCalls := len(getToolCalls(msg)) > 0

			if !hasToolCalls {
				idx := i
				assistantIdx = &idx
			}
			i--
		} else {
			// 未找到 assistant，将剩余部分视为不完整轮次
			i = roundEnd
		}

		// 查找该轮的 user 消息
		for i >= 0 && !isUserMessage(messages[i]) {
			i--
		}

		if i < 0 {
			break
		}

		foundUserIdx := i
		userIdx := findContiguousUserGroupStart(foundUserIdx)

		// 首轮：查找尾部不完整轮次（最后一个 user 之后还有 user）
		if len(rounds) == 0 {
			for lastIdx := len(messages) - 1; lastIdx > foundUserIdx; lastIdx-- {
				if isUserMessage(messages[lastIdx]) {
					startIdx := findContiguousUserGroupStart(lastIdx)
					rounds = append(rounds, DialogueRound{&startIdx, nil})
					break
				}
			}
		}

		rounds = append(rounds, DialogueRound{&userIdx, assistantIdx})
		i = userIdx - 1
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
