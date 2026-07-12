package adapter

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// usageAccumulator usage 累加器。
// 对齐 Python: process_message_stream_impl 中的 usage_accumulator (line 4514-4979)
type usageAccumulator struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	InputCost    float64
	OutputCost   float64
	TotalCost    float64
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseStreamChunk 解析流式 chunk。
// 对齐 Python: _parse_stream_chunk(chunk) (line 4981-5294)
//
// 处理 15+ 种 chunk.Type，返回 payload dict。
func (d *DeepAdapter) parseStreamChunk(output *stream.OutputSchema, usage *usageAccumulator, emittedAskUserIDs map[string]bool) map[string]any {
	if output == nil {
		return nil
	}

	chunkType := output.Type
	payload, _ := output.Payload.(map[string]any)
	if payload == nil {
		payload = make(map[string]any)
	}

	// 注意：llm_usage/llm_reasoning/llm_output 三种类型在 ProcessMessageStreamImpl
	// 的 goroutine 中直接处理（需要跨 chunk 累加状态），不经过 parseStreamChunk。

	switch chunkType {
	case "controller_output":
		// 对齐 Python: _parse_stream_chunk controller_output 分支 (line 5003-5070)
		// 内部 type 分发
		innerType, _ := payload["type"].(string)
		switch innerType {
		case "task_completion":
			// 跳过，由终止哨兵处理
			return nil
		case "task_failed":
			return map[string]any{
				"event_type": "chat.error",
				"error":      payload["error"],
			}
		default:
			return map[string]any{
				"event_type": "chat.delta",
				"content":    payload,
			}
		}

	case "content_chunk":
		return map[string]any{
			"event_type": "chat.delta",
			"content":    payload["content"],
		}

	case "answer":
		return map[string]any{
			"event_type": "chat.final",
			"content":    payload["content"],
		}

	case "tool_call":
		return map[string]any{
			"event_type": "chat.tool_call",
			"tool_call":  payload,
		}

	case "tool_update":
		return map[string]any{
			"event_type":  "chat.tool_update",
			"tool_update": payload,
		}

	case "tool_result":
		return map[string]any{
			"event_type":  "chat.tool_result",
			"tool_result": payload,
		}

	case "error":
		return map[string]any{
			"event_type": "chat.error",
			"error":      payload["error"],
		}

	case "thinking":
		return map[string]any{
			"event_type": "chat.thinking",
			"content":    payload["content"],
		}

	case "todo.updated":
		return map[string]any{
			"event_type": "todo.updated",
			"todo":       payload,
		}

	case "context.usage":
		return map[string]any{
			"event_type":   "chat.context_usage",
			"context_usage": payload,
		}

	case "context.compression_state":
		return map[string]any{
			"event_type":             "chat.context_compression_state",
			"compression_state":      payload,
		}

	case "ask_user_question":
		// 对齐 Python: ask_user_question 去重 (line 5205-5240)
		requestID, _ := payload["request_id"].(string)
		if requestID != "" && emittedAskUserIDs[requestID] {
			return nil // 去重：已发送过的 ask_user
		}
		if requestID != "" {
			emittedAskUserIDs[requestID] = true
		}
		return map[string]any{
			"event_type":       "chat.ask_user_question",
			"ask_user_question": payload,
		}

	case "__interaction__":
		return map[string]any{
			"event_type":  "chat.interaction",
			"interaction": payload,
		}

	case "message", "stage_result", "extension_ready", "harness_session_finished", "activate_testing_guide":
		// 对齐 Python: 各特殊类型的处理 (line 5240-5294)
		return map[string]any{
			"event_type": "chat." + chunkType,
			"content":    payload,
		}

	default:
		// 未知类型，透传
		return map[string]any{
			"event_type": "chat.delta",
			"content":    payload,
		}
	}
}

// accumulateUsage 累加 usage 信息。
// 对齐 Python: usage_accumulator 的累加逻辑 (line 4580-4610)
func (d *DeepAdapter) accumulateUsage(usage *usageAccumulator, payload any) {
	if payload == nil || usage == nil {
		return
	}
	m, ok := payload.(map[string]any)
	if !ok {
		return
	}
	usage.InputTokens += extractIntFromPayload(m, "input_tokens")
	usage.OutputTokens += extractIntFromPayload(m, "output_tokens")
	usage.TotalTokens += extractIntFromPayload(m, "total_tokens")
	usage.InputCost += extractFloatFromPayload(m, "input_cost")
	usage.OutputCost += extractFloatFromPayload(m, "output_cost")
	usage.TotalCost += extractFloatFromPayload(m, "total_cost")
}

// extractStringFromPayload 从 payload 提取字符串值。
func extractStringFromPayload(payload map[string]any, key string) string {
	v, ok := payload[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// extractIntFromPayload 从 payload 提取整数值。
func extractIntFromPayload(payload map[string]any, key string) int {
	v, ok := payload[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// extractFloatFromPayload 从 payload 提取浮点数值。
func extractFloatFromPayload(payload map[string]any, key string) float64 {
	v, ok := payload[key]
	if !ok {
		return 0
	}
	switch f := v.(type) {
	case float64:
		return f
	case int:
		return float64(f)
	default:
		return 0
	}
}
