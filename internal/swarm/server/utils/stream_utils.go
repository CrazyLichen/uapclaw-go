package utils

import (
	"fmt"
	"reflect"
	"strings"
	"time"

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

// FindOption FindInteractionPayloads 可选参数。
type FindOption struct {
	// MaxDepth 最大递归深度，默认 8
	MaxDepth int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseStreamChunk 解析流式 chunk。
// 对齐 Python: _parse_stream_chunk(chunk) (stream_utils.py L10-44)
// 及 _parse_typed_chunk(chunk) (stream_utils.py L107-353)
//
// 处理 15+ 种 chunk.Type，返回 payload dict。
// converter 参数用于自定义 __interaction__ 类型的交互转换逻辑，
// 若 converter 为 nil，则 ParseInteractionPayload 回退为默认行为。
// hasStreamedContent 对齐 Python 的 _has_streamed_content，用于 answer 分支判断 chat.delta/chat.final。
func ParseStreamChunk(output *stream.OutputSchema, usage *UsageAccumulator, emittedAskUserIDs map[string]bool, converter InteractionConverterFunc, hasStreamedContent ...bool) map[string]any {
	if output == nil {
		return nil
	}

	hsc := len(hasStreamedContent) > 0 && hasStreamedContent[0]

	chunkType := output.Type
	payload, _ := output.Payload.(map[string]any)
	if payload == nil {
		payload = make(map[string]any)
	}

	// 注意：llm_usage/llm_reasoning/llm_output 三种类型在 ProcessMessageStreamImpl
	// 的 goroutine 中直接处理（需要跨 chunk 累加状态），不经过 ParseStreamChunk。

	switch chunkType {
	case "controller_output":
		// 对齐 Python L150-170: 先搜索 __interaction__ 载荷
		interactions := FindInteractionPayloads(payload)
		if len(interactions) > 0 {
			if p, ok := interactions[0].(map[string]any); ok {
				return ParseInteractionPayload(p, converter)
			}
		}
		// 未找到 __interaction__，检查内部 type
		innerType, _ := payload["type"].(string)
		switch innerType {
		case "task_completion":
			return nil
		case "task_failed":
			// 对齐 Python: 从 data 列表找 .text 字段
			errorMsg := "任务执行失败"
			if data, ok := payload["data"].([]any); ok {
				for _, item := range data {
					if m, ok := item.(map[string]any); ok {
						if text, ok := m["text"].(string); ok && text != "" {
							errorMsg = text
							break
						}
					}
				}
			}
			return map[string]any{
				"event_type": "chat.error",
				"error":      errorMsg,
			}
		default:
			return map[string]any{
				"event_type": "chat.delta",
				"content":    payload,
			}
		}

	case "content_chunk":
		// 对齐 Python L192-200: 空内容返回 nil
		content := ExtractStringFromPayload(payload, "content")
		if content == "" || strings.TrimSpace(content) == "" {
			return nil
		}
		return map[string]any{"event_type": "chat.delta", "content": content}

	case "answer":
		// 对齐 Python L202-231
		if payload["result_type"] == "error" {
			return map[string]any{
				"event_type": "chat.error",
				"error":      ExtractStringFromPayload(payload, "output"),
			}
		}
		var content string
		var isChunked bool
		if output, ok := payload["output"].(map[string]any); ok {
			content, _ = output["output"].(string)
			isChunked, _ = output["chunked"].(bool)
		} else {
			content = fmt.Sprintf("%v", payload["output"])
		}
		if content == "" || strings.TrimSpace(content) == "" {
			return nil
		}
		if hsc && !isChunked {
			return map[string]any{"event_type": "chat.final", "content": content}
		}
		if isChunked {
			return map[string]any{"event_type": "chat.delta", "content": content}
		}
		return map[string]any{"event_type": "chat.final", "content": content}

	case "tool_call":
		// 对齐 Python L233-239
		toolInfo := payload
		if sub, ok := payload["tool_call"].(map[string]any); ok {
			toolInfo = sub
		}
		return map[string]any{
			"event_type": "chat.tool_call",
			"tool_call":  toolInfo,
		}

	case "tool_update":
		// 对齐 Python L241-250: 提取 tool_update 子对象
		var updatePayload map[string]any
		if sub, ok := payload["tool_update"].(map[string]any); ok {
			updatePayload = sub
		} else {
			updatePayload = map[string]any{"content": fmt.Sprintf("%v", payload["tool_update"])}
		}
		ret := map[string]any{"event_type": "chat.tool_update"}
		for k, v := range updatePayload {
			ret[k] = v
		}
		return ret

	case "tool_result":
		// 对齐 Python L252-282: 提取 tool_result 子对象 + 字段映射
		resultInfo := payload
		if sub, ok := payload["tool_result"].(map[string]any); ok {
			resultInfo = sub
		}
		result := map[string]any{
			"result": ExtractStringFromPayload(resultInfo, "result"),
		}
		// tool_name: 优先 tool_name，回退 name
		if tn, ok := resultInfo["tool_name"].(string); ok && tn != "" {
			result["tool_name"] = tn
		} else if n, ok := resultInfo["name"].(string); ok {
			result["tool_name"] = n
		}
		// tool_call_id: 优先 tool_call_id，回退 toolCallId
		if tcid, ok := resultInfo["tool_call_id"].(string); ok && tcid != "" {
			result["tool_call_id"] = tcid
		} else if tcid2, ok := resultInfo["toolCallId"].(string); ok {
			result["tool_call_id"] = tcid2
		}
		// raw_output: 优先 raw_output，回退 rawOutput
		if ro, ok := resultInfo["raw_output"]; ok {
			result["raw_output"] = ro
		} else if ro2, ok := resultInfo["rawOutput"]; ok {
			result["raw_output"] = ro2
		}
		// success/status/is_error/summary
		for _, key := range []string{"success", "status", "is_error", "summary"} {
			if v, exists := resultInfo[key]; exists {
				result[key] = v
			}
		}
		ret := map[string]any{"event_type": "chat.tool_result"}
		for k, v := range result {
			ret[k] = v
		}
		return ret

	case "error":
		// 对齐 Python L284-290
		errorMsg := ExtractStringFromPayload(payload, "error")
		if errorMsg == "" {
			errorMsg = fmt.Sprintf("%v", payload)
		}
		return map[string]any{
			"event_type": "chat.error",
			"error":      errorMsg,
		}

	case "thinking":
		// 对齐 Python L292-297: processing_status 而非 thinking
		return map[string]any{
			"event_type":    "chat.processing_status",
			"is_processing": true,
			"current_task":  "thinking",
		}

	case "todo.updated":
		// 对齐 Python L299-305: 提取 todos 列表
		todos, _ := payload["todos"].([]any)
		if todos == nil {
			todos = []any{}
		}
		return map[string]any{
			"event_type": "todo.updated",
			"todos":      todos,
		}

	case "context.usage":
		// 对齐 Python L307-314: 透传 rate/context_max/tokens_used
		return map[string]any{
			"event_type":  "context.usage",
			"rate":        ExtractFloatFromPayload(payload, "rate"),
			"context_max": ExtractIntFromPayload(payload, "context_max"),
			"tokens_used": ExtractIntFromPayload(payload, "tokens_used"),
		}

	case "context.compression_state":
		// 对齐 Python L112-131: dot-namespace 类型
		return map[string]any{
			"event_type":        "chat.context_compression_state",
			"compression_state": payload,
		}

	case "ask_user_question":
		// 对齐 Python L316-320: ask_user_question 去重
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
		// 对齐 Python: 各特殊类型的处理
		return map[string]any{
			"event_type": "chat." + chunkType,
			"content":    payload,
		}

	default:
		// 对齐 Python L325-353: 保留原始类型名 + team.* 直传
		if innerEventType, ok := payload["event_type"].(string); ok {
			if strings.HasPrefix(innerEventType, "team.") {
				// team 命名空间事件直传
				result := map[string]any{}
				for k, v := range payload {
					result[k] = SerializeValue(v)
				}
				return result
			}
		}
		// payload 有内容 → 展开
		if len(payload) > 0 {
			result := map[string]any{
				"event_type": "chat." + chunkType,
			}
			for k, v := range payload {
				if k != "event_type" {
					result[k] = SerializeValue(v)
				}
			}
			return result
		}
		// payload 为空 → 作为 content
		return map[string]any{
			"event_type": "chat." + chunkType,
			"content":    fmt.Sprintf("%v", payload),
		}
	}
}

// ParseInteractionPayload 解析 __interaction__ 类型的 payload。
// 对齐 Python: _parse_interaction_payload(payload) (stream_utils.py L356-375)
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

// SerializeValue 将非 JSON 原生值序列化为前端安全的 payload。
// 对齐 Python: _serialize_value(value) (stream_utils.py L472-483)
func SerializeValue(value any) any {
	switch v := value.(type) {
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	default:
		return v
	}
}

// FindInteractionPayloads 递归搜索 __interaction__ 载荷。
// 对齐 Python: _find_interaction_payloads(obj, _depth=0, _seen=None) (stream_utils.py L378-432)
func FindInteractionPayloads(obj any, opts ...FindOption) []any {
	return findInteractionPayloadsRecursive(obj, 0, nil)
}

// FindInteractionPayload 返回第一个匹配的 __interaction__ 载荷。
// 对齐 Python: _find_interaction_payload(obj) (stream_utils.py L435-443)
func FindInteractionPayload(obj any, opts ...FindOption) any {
	matches := FindInteractionPayloads(obj, opts...)
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// findInteractionPayloadsRecursive 递归搜索 __interaction__ 载荷。
// 对齐 Python: _find_interaction_payloads 内部递归逻辑 (stream_utils.py L378-432)
func findInteractionPayloadsRecursive(obj any, depth int, seen map[uintptr]bool) []any {
	if obj == nil || depth > 8 {
		return nil
	}
	// 循环检测：对 map/slice/ptr 取地址作 seen 标记
	rv := reflect.ValueOf(obj)
	if rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice || rv.Kind() == reflect.Ptr {
		ptr := rv.Pointer()
		if seen == nil {
			seen = make(map[uintptr]bool)
		}
		if seen[ptr] {
			return nil
		}
		seen[ptr] = true
	}

	switch v := obj.(type) {
	case map[string]any:
		if v["type"] == "__interaction__" {
			return []any{v["payload"]}
		}
		if v["event_type"] == "chat.ask_user_question" {
			return []any{map[string]any{
				"id":    v["request_id"],
				"value": map[string]any{"questions": v["questions"]},
			}}
		}
		var found []any
		for _, val := range v {
			found = append(found, findInteractionPayloadsRecursive(val, depth+1, seen)...)
		}
		return found
	case []any:
		var found []any
		for _, val := range v {
			found = append(found, findInteractionPayloadsRecursive(val, depth+1, seen)...)
		}
		return found
	default:
		return nil
	}
}
