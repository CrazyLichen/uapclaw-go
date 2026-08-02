package utils

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UsageAccumulator usage 累加器。
// 对齐 Python: process_message_stream_impl 中的 usage_accumulator (line 4514-4979)
type UsageAccumulator struct {
	// InputTokens 输入 token 数
	InputTokens int
	// OutputTokens 输出 token 数
	OutputTokens int
	// TotalTokens 总 token 数
	TotalTokens int
	// InputCost 输入成本
	InputCost float64
	// OutputCost 输出成本
	OutputCost float64
	// TotalCost 总成本
	TotalCost float64
}

// InteractionConverterFunc 交互转换函数，用于自定义 __interaction__ 类型 chunk 的解析逻辑。
// 对齐 Python: 不同 adapter 对 interaction payload 的转换方式不同，
// 通过此函数参数实现多态，避免 utils 包依赖具体 adapter。
type InteractionConverterFunc func(payload any) map[string]any

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseStreamChunk 解析流式 chunk。
// 对齐 Python: _parse_stream_chunk(chunk) (line 4981-5294)
//
// 处理 15+ 种 chunk.Type，返回 payload dict。
// converter 参数用于自定义 __interaction__ 类型的交互转换逻辑，
// 若 converter 为 nil，则 ParseInteractionPayload 回退为默认行为。
func ParseStreamChunk(output *stream.OutputSchema, usage *UsageAccumulator, emittedAskUserIDs map[string]bool, converter InteractionConverterFunc) map[string]any {
	if output == nil {
		return nil
	}

	chunkType := output.Type
	payload, _ := output.Payload.(map[string]any)
	if payload == nil {
		payload = make(map[string]any)
	}

	// 注意：llm_usage/llm_reasoning/llm_output 三种类型在 ProcessMessageStreamImpl
	// 的 goroutine 中直接处理（需要跨 chunk 累加状态），不经过 ParseStreamChunk。

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
			"event_type":    "chat.context_usage",
			"context_usage": payload,
		}

	case "context.compression_state":
		return map[string]any{
			"event_type":        "chat.context_compression_state",
			"compression_state": payload,
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
			"event_type":        "chat.ask_user_question",
			"ask_user_question": payload,
		}

	case "__interaction__":
		return ParseInteractionPayload(payload, converter)

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

// ParseInteractionPayload 解析 __interaction__ 类型的 payload。
// 若 converter 不为 nil，委托 converter 进行交互转换；
// 否则回退为默认行为，直接返回 chat.interaction 事件。
func ParseInteractionPayload(payload map[string]any, converter InteractionConverterFunc) map[string]any {
	if converter != nil {
		return converter(payload)
	}
	return map[string]any{
		"event_type":  "chat.interaction",
		"interaction": payload,
	}
}

// AccumulateUsage 累加 usage 信息。
// 对齐 Python: usage_accumulator 的累加逻辑 (line 4580-4610)
func AccumulateUsage(usage *UsageAccumulator, payload map[string]any) {
	if payload == nil || usage == nil {
		return
	}
	usage.InputTokens += ExtractIntFromPayload(payload, "input_tokens")
	usage.OutputTokens += ExtractIntFromPayload(payload, "output_tokens")
	usage.TotalTokens += ExtractIntFromPayload(payload, "total_tokens")
	usage.InputCost += ExtractFloatFromPayload(payload, "input_cost")
	usage.OutputCost += ExtractFloatFromPayload(payload, "output_cost")
	usage.TotalCost += ExtractFloatFromPayload(payload, "total_cost")
}

// ExtractStringFromPayload 从 payload 提取字符串值。
func ExtractStringFromPayload(payload map[string]any, key string) string {
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

// ExtractIntFromPayload 从 payload 提取整数值。
func ExtractIntFromPayload(payload map[string]any, key string) int {
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

// ExtractFloatFromPayload 从 payload 提取浮点数值。
func ExtractFloatFromPayload(payload map[string]any, key string) float64 {
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

// ParseDictChunk 解析 dict 类型 chunk。
// 对齐 Python: _parse_dict_chunk(chunk)
// 当前为 stub，后续将补充完整实现。
func ParseDictChunk(chunk map[string]any) map[string]any {
	if chunk == nil {
		return nil
	}
	return chunk
}

// ParseTypedChunk 解析带 type 字段的 chunk。
// 对齐 Python: _parse_typed_chunk(chunk)
// 当前为 stub，后续将补充完整实现。
func ParseTypedChunk(chunk map[string]any) map[string]any {
	if chunk == nil {
		return nil
	}
	chunkType, _ := chunk["type"].(string)
	if chunkType == "" {
		return chunk
	}
	return chunk
}

// ParseEventTypedChunk 解析带 event_type 的 typed chunk。
// 对齐 Python: _parse_event_typed_chunk(chunk)
// 当前为 stub，后续将补充完整实现。
func ParseEventTypedChunk(chunk map[string]any) map[string]any {
	if chunk == nil {
		return nil
	}
	return chunk
}

// ParseResponseChunk 解析响应 chunk。
// 对齐 Python: _parse_response_chunk(chunk)
// 当前为 stub，后续将补充完整实现。
func ParseResponseChunk(chunk map[string]any) map[string]any {
	if chunk == nil {
		return nil
	}
	return chunk
}

// ──────────────────────────── 非导出函数 ────────────────────────────
