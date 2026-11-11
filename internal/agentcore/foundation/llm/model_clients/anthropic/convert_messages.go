package anthropic

import (
	"encoding/json"
)

// ConvertMessageSchemas 把 OpenAI 形状消息列表拆分为 (system_blocks, anthropic_messages)。
// Messages API 要求 system 作为顶层参数，其余消息在 user/assistant 间交替。
// OpenAI 的 assistant tool_calls 变 tool_use block；role:"tool" 变携带 tool_result 的 user。
// system turn 先原位排布再事后分区——能否留下取决于它最终的邻居。
// Python: anthropic_model_client.py:423-523 _convert_message_schemas
func ConvertMessageSchemas(messages []map[string]any, allowMidConversationSystem bool) ([]map[string]any, []map[string]any) {
	out := make([]map[string]any, 0, len(messages))
	pendingToolResults := make([]map[string]any, 0)

	flushToolResults := func() {
		if len(pendingToolResults) > 0 {
			// 拷贝后再清空，避免引用共享底层数组
			out = append(out, map[string]any{"role": "user", "content": pendingToolResults})
			pendingToolResults = make([]map[string]any, 0)
		}
	}

	for _, msg := range messages {
		role := strArg(msg, "role")

		// system：原位输出；_partition_system_messages 事后决定该位置 API 是否接受。
		if role == "system" {
			flushToolResults()
			out = append(out, map[string]any{
				"role":    "system",
				"content": ContentToText(msg["content"]),
			})
			continue
		}

		// tool：累积为 tool_result block；空结果补空 text（API 要求非空 content）。
		if role == "tool" {
			toolCallID := strArg(msg, "tool_call_id")
			resultBlocks := ContentToBlocks(msg["content"])
			if len(resultBlocks) == 0 {
				resultBlocks = []map[string]any{{"type": "text", "text": ""}}
			}
			pendingToolResults = append(pendingToolResults, map[string]any{
				"type":        "tool_result",
				"tool_use_id": toolCallID,
				"content":     resultBlocks,
			})
			continue
		}

		flushToolResults()

		if role == "assistant" {
			// 回放块存在 → 原样回放（thinking 签名必须原样带回）
			if preserved := PreservedContentBlocks(msg); len(preserved) > 0 {
				out = append(out, map[string]any{"role": "assistant", "content": preserved})
				continue
			}
			blocks := ContentToBlocks(msg["content"])
			toolCalls, _ := msg["tool_calls"].([]any)
			for _, tcAny := range toolCalls {
				tc, ok := tcAny.(map[string]any)
				if !ok {
					continue
				}
				fn := dictArg(tc, "function")
				// Python: args_str = fn.get("arguments", "{}") or "{}"；
				// 字符串则 json.loads，非字符串原样使用；解析失败 → _raw_arguments 兜底。
				var argsStr string
				var argsAny any
				if fn != nil {
					argsAny = fn["arguments"]
				}
				if s, isStr := argsAny.(string); isStr {
					if s == "" {
						s = "{}"
					}
					argsStr = s
				} else if argsStr == "" && argsAny == nil {
					argsStr = "{}"
				}
				argsObj := toolUseInput(argsAny, argsStr)
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    strArg(tc, "id"),
					"name":  strArg(fn, "name"),
					"input": argsObj,
				})
			}
			if len(blocks) == 0 {
				blocks = []map[string]any{{"type": "text", "text": ""}}
			}
			out = append(out, map[string]any{"role": "assistant", "content": blocks})
			continue
		}

		// user（或其他未识别 role → 按 user 处理）
		blocks := ContentToBlocks(msg["content"])
		if len(blocks) == 0 {
			blocks = []map[string]any{{"type": "text", "text": ""}}
		}
		out = append(out, map[string]any{"role": "user", "content": blocks})
	}

	flushToolResults()

	systemBlocks, kept := partitionSystemMessages(out, allowMidConversationSystem)
	return systemBlocks, kept
}

// partitionSystemMessages 把 system turn 拆成顶层 blocks 与留守项。
// 留守是这一步的全部意义：折叠进顶层 system 会把它提到整个对话之前，
// 其后的每个缓存 turn 全部重算——长 agent 运行中一条追加指令就是一次全量
// 缓存重写。留在原位的消息保住缓存前缀。
// Python: anthropic_model_client.py:388-421 _partition_system_messages
func partitionSystemMessages(converted []map[string]any, allowMidConversationSystem bool) ([]map[string]any, []map[string]any) {
	systemBlocks := make([]map[string]any, 0)
	kept := make([]map[string]any, 0, len(converted))
	for index, message := range converted {
		if role := strArg(message, "role"); role != "system" {
			kept = append(kept, message)
			continue
		}
		if allowMidConversationSystem && inlineSystemIsPlaceable(converted, index) {
			kept = append(kept, message)
			continue
		}
		systemBlocks = append(systemBlocks, ContentToBlocks(message["content"])...)
	}
	return systemBlocks, kept
}

// inlineSystemIsPlaceable 报告 system 消息是否可以留在原地。
// API 只接受 user turn 之后、且是末条或后邻 assistant 的 system；
// messages[0] 永远不行——那正是 leading system prompt 该去顶层参数的原因。
// 邻居读分区前列表是刻意的：与另一条 system 相邻时按邻居判定、双双回退，
// 而不是各自假设对方会被移走。
// Python: anthropic_model_client.py:366-384 _inline_system_is_placeable
func inlineSystemIsPlaceable(converted []map[string]any, index int) bool {
	if index == 0 {
		return false
	}
	if strArg(converted[index-1], "role") != "user" {
		return false
	}
	if index == len(converted)-1 {
		return true
	}
	return strArg(converted[index+1], "role") == "assistant"
}

// ApplyStaticCacheBreakpoints 给 tools 尾挂 ephemeral、system blocks 挂 5m。
// Python: anthropic_model_client.py:542-549 _apply_static_cache_breakpoints
func ApplyStaticCacheBreakpoints(systemBlocks []map[string]any, tools []map[string]any) {
	if len(tools) > 0 {
		tools[len(tools)-1]["cache_control"] = map[string]any{"type": "ephemeral"}
	}
	if len(systemBlocks) > 0 {
		MarkCacheControl(systemBlocks, "5m")
	}
}

// LastInputIsTransient 判断最后一条输入消息是否带 metadata['transient']。
// 瞬态消息（如每轮运行时预算提醒）追加在尾部、调用后剥离，必须位于最后
// 缓存断点之后：否则易变尾部锚定前缀，每轮都 miss。metadata 会被通用转换器
// 丢弃，所以从原始消息列表读（转换前）。
// Python: anthropic_model_client.py:552-569 _last_input_is_transient
func LastInputIsTransient(messages []map[string]any) bool {
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	var metadata map[string]any
	if m, ok := last["metadata"].(map[string]any); ok {
		metadata = m
	}
	if metadata == nil {
		return false
	}
	v, ok := metadata["transient"]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	default:
		return t != nil
	}
}

// ApplyMessagesCacheBreakpoint 把对话缓存前缀锚在最后一条「稳定」消息上。
// 取代顶层自动缓存（那总锚定最后一块）：尾是 transient 时锚到它前面那条，
// 让易变尾巴不进缓存、稳定单调增长的前缀跨轮命中。
// Python: anthropic_model_client.py:571-601 _apply_messages_cache_breakpoint
func ApplyMessagesCacheBreakpoint(anthropicMessages []map[string]any, excludeTail bool) {
	if len(anthropicMessages) == 0 {
		return
	}
	idx := len(anthropicMessages) - 1
	if excludeTail && idx >= 1 {
		idx--
	}
	// 中位 system turn 是纯文本、无块可挂：回走到最近有块的消息，
	// 而不是丢掉断点让该轮不进缓存。
	for idx >= 0 {
		if _, isList := anthropicMessages[idx]["content"].([]map[string]any); isList {
			break
		}
		idx--
	}
	if idx < 0 {
		return
	}
	blocks, ok := anthropicMessages[idx]["content"].([]map[string]any)
	if ok {
		MarkCacheControl(blocks, "5m")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolUseInput 归一 tool_use input：字符串 JSON 解析（失败 → _raw_arguments），
// 非字符串原样。Python: anthropic_model_client.py:508-518。
func toolUseInput(argsAny any, argsStr string) any {
	if argsAny != nil {
		if _, isStr := argsAny.(string); !isStr {
			return argsAny
		}
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(argsStr), &obj); err != nil {
		return map[string]any{"_raw_arguments": argsStr}
	}
	return obj
}
