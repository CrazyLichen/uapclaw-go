package utils

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UsageAccumulator usage 累加器。
// Python: process_message_stream_impl 中的 usage_accumulator (line 4514-4979)
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
// Python: 不同 adapter 对 interaction payload 的转换方式不同，
// 通过此函数参数实现多态，避免 utils 包依赖具体 adapter。
type InteractionConverterFunc func(payload any) map[string]any

// FindOption FindInteractionPayloads 可选参数。
type FindOption struct {
	// MaxDepth 最大递归深度，默认 8
	MaxDepth int
}

// ParseStreamChunkOption ParseStreamChunk 的可选参数。
type ParseStreamChunkOption struct {
	// Stage 当 payload 中无 stage 字段时的回退值（对应 Python _stage）
	Stage string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseStreamChunk 解析流式 chunk。
// Python: interface_deep._parse_stream_chunk(chunk, _has_streamed_content, _stage)
// 对应 Python 源码：interface_deep.py L4982-5276
//
// 处理 15+ 种 chunk.Type，返回 payload dict。
// converter 参数用于自定义 __interaction__ 类型的交互转换逻辑，
// 若 converter 为 nil，则 ParseInteractionPayload 回退为默认行为。
// hasStreamedContent 对齐 Python 的 _has_streamed_content，用于 answer 分支判断 chat.delta/chat.final。
func ParseStreamChunk(output *stream.OutputSchema, usage *UsageAccumulator, emittedAskUserIDs map[string]bool, converter InteractionConverterFunc, hasStreamedContent bool, opts ...ParseStreamChunkOption) map[string]any {
	if output == nil {
		return nil
	}

	var fallbackStage string
	if len(opts) > 0 {
		fallbackStage = opts[0].Stage
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
		// S-04: 对齐 Python — 先搜索 __interaction__，找到则解析
		// Python: stream_utils._parse_typed_chunk controller_output 分支
		// Python: interactions = _find_interaction_payloads(payload)
		// Python: if interactions: return _parse_interaction_payload(interactions)
		interactions := FindInteractionPayloads(payload)
		if len(interactions) > 0 {
			return parseControllerOutputInteractions(interactions, converter)
		}
		// 未找到 interaction，走 inner type 判断
		innerType, _ := payload["type"].(string)
		switch innerType {
		case "task_completion":
			return nil
		case "task_failed":
			// Python: 从 data 列表找 .text 字段
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
		// Python: L5033-5039: 空内容返回 nil
		content := ExtractStringFromPayload(payload, "content")
		if content == "" || strings.TrimSpace(content) == "" {
			return nil
		}
		return map[string]any{"event_type": "chat.delta", "content": content}

	case "answer":
		// Python: interface_deep._parse_stream_chunk L5041-5066
		if payload["result_type"] == "error" {
			// M-02: 默认消息对齐 Python "未知错误"
			errorMsg := ExtractStringFromPayload(payload, "output")
			if errorMsg == "" {
				errorMsg = "未知错误"
			}
			return map[string]any{
				"event_type": "chat.error",
				"error":      errorMsg,
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
		if hasStreamedContent && !isChunked {
			return map[string]any{"event_type": "chat.final", "content": content}
		}
		if isChunked {
			return map[string]any{"event_type": "chat.delta", "content": content}
		}
		return map[string]any{"event_type": "chat.final", "content": content}

	case "tool_call":
		// Python: L5068-5072
		toolInfo := payload
		if sub, ok := payload["tool_call"].(map[string]any); ok {
			toolInfo = sub
		}
		return map[string]any{
			"event_type": "chat.tool_call",
			"tool_call":  toolInfo,
		}

	case "tool_update":
		// Python: L5074-5087: 提取 tool_update 子对象
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
		// Python: L5089-5116: 提取 tool_result 子对象 + 字段映射
		resultInfo := payload
		if sub, ok := payload["tool_result"].(map[string]any); ok {
			resultInfo = sub
		}
		// M-01: 对齐 Python result_info.get("result", str(result_info)) fallback
		resultVal := ExtractStringFromPayload(resultInfo, "result")
		if resultVal == "" {
			resultVal = fmt.Sprintf("%v", resultInfo)
		}
		result := map[string]any{
			"result": resultVal,
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
		// success/status/is_error/summary 字段提取
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
		// Python: L5118-5124
		errorMsg := ExtractStringFromPayload(payload, "error")
		if errorMsg == "" {
			errorMsg = fmt.Sprintf("%v", payload)
		}
		return map[string]any{
			"event_type": "chat.error",
			"error":      errorMsg,
		}

	case "thinking":
		// Python: L5126-5131: processing_status 而非 thinking
		return map[string]any{
			"event_type":    "chat.processing_status",
			"is_processing": true,
			"current_task":  "thinking",
		}

	case "todo.updated":
		// Python: L5133-5135: 提取 todos 列表
		todos, _ := payload["todos"].([]any)
		if todos == nil {
			todos = []any{}
		}
		return map[string]any{
			"event_type": "todo.updated",
			"todos":      todos,
		}

	case "context.usage":
		// Python: L5137-5145: 透传 rate/context_max/tokens_used
		return map[string]any{
			"event_type":  "context.usage",
			"rate":        ExtractFloatFromPayload(payload, "rate"),
			"context_max": ExtractIntFromPayload(payload, "context_max"),
			"tokens_used": ExtractIntFromPayload(payload, "tokens_used"),
		}

	case "context.compression_state":
		// M-01: 对齐 Python — payload 所有键展开到顶层（不再只提取 5 个固定字段）
		// Python: interface_deep._parse_stream_chunk L5147-5157
		result := map[string]any{"event_type": "context.compression_state"}
		if payloadMap, ok := output.Payload.(map[string]any); ok {
			for k, v := range payloadMap {
				result[k] = SerializeValue(v)
			}
		}
		result["event_type"] = "context.compression_state" // 确保 event_type 正确
		return result

	case "ask_user_question":
		// S-05: 对齐 Python — payload 展开到顶层而非嵌套
		// Python: interface_deep._parse_stream_chunk L5159-5163
		requestID, _ := payload["request_id"].(string)
		if requestID != "" && emittedAskUserIDs[requestID] {
			return nil // 去重：已发送过的 ask_user
		}
		if requestID != "" {
			emittedAskUserIDs[requestID] = true
		}
		result := map[string]any{
			"event_type": "chat.ask_user_question",
		}
		for k, v := range payload {
			result[k] = SerializeValue(v)
		}
		return result

	case "__interaction__":
		// S-03: 对齐 Python — 先检查 activate_confirm，再走 converter
		// Python: interface_deep._parse_stream_chunk L5165-5179
		return ParseInteractionPayload(payload, converter)

	case "message":
		// S-04: 对齐 Python — harness.message 事件
		// Python: interface_deep._parse_stream_chunk L5182-5208
		content := ExtractStringFromPayload(payload, "content")
		if content == "" {
			content = fmt.Sprintf("%v", payload["content"])
		}
		if content == "" {
			content = fmt.Sprintf("%v", payload)
		}
		stage := ExtractStringFromPayload(payload, "stage")
		if stage == "" {
			stage = fallbackStage
		}
		result := map[string]any{
			"event_type": "harness.message",
			"content":    content,
			"stage":      stage,
		}
		// 透传 stages/pipeline/metadata
		if stages, ok := payload["stages"]; ok {
			result["stages"] = stages
		}
		if pipeline, ok := payload["pipeline"]; ok {
			result["pipeline"] = pipeline
		}
		if metadata, ok := payload["metadata"]; ok {
			result["metadata"] = metadata
		}
		return result

	case "stage_result":
		// S-04: 对齐 Python — harness.stage_result 事件
		// Python: interface_deep._parse_stream_chunk L5211-5227
		if _, ok := payload["stage"]; !ok && len(payload) == 0 {
			return nil
		}
		stage := ExtractStringFromPayload(payload, "stage")
		if stage == "" {
			stage = fallbackStage
		}
		return map[string]any{
			"event_type":      "harness.stage_result",
			"stage":           stage,
			"status":          ExtractStringFromPayload(payload, "status"),
			"error":           ExtractStringFromPayload(payload, "error"),
			"messages":        payload["messages"],
			"metrics":         payload["metrics"],
			"scope":           ExtractStringFromPayload(payload, "scope"),
			"parent_stage":    ExtractStringFromPayload(payload, "parent_stage"),
			"extension_stage": ExtractStringFromPayload(payload, "extension_stage"),
			"extension_name":  ExtractStringFromPayload(payload, "extension_name"),
			"task_id":         ExtractStringFromPayload(payload, "task_id"),
		}

	case "extension_ready":
		// S-04: 对齐 Python — harness.extension_ready 事件
		// Python: interface_deep._parse_stream_chunk L5230-5243
		return map[string]any{
			"event_type":             "harness.extension_ready",
			"extension_name":         ExtractStringFromPayload(payload, "extension_name"),
			"runtime_path":           ExtractStringFromPayload(payload, "runtime_path"),
			"session_runtime_path":   ExtractStringFromPayload(payload, "session_runtime_path"),
			"extension_runtime_path": ExtractStringFromPayload(payload, "extension_runtime_path"),
			"config_path":            ExtractStringFromPayload(payload, "config_path"),
			"runtime_extensions":     payload["runtime_extensions"],
			"verify_report":          payload["verify_report"],
			"components_summary":     payload["components_summary"],
		}

	case "harness_session_finished":
		// S-04: 对齐 Python — harness.session_finished 事件
		// Python: interface_deep._parse_stream_chunk L5245-5258
		if len(payload) > 0 {
			isTerminal := true
			if v, ok := payload["is_terminal"].(bool); ok {
				isTerminal = v
			}
			return map[string]any{
				"event_type":    "harness.session_finished",
				"pipeline":      ExtractStringFromPayload(payload, "pipeline"),
				"status":        ExtractStringFromPayload(payload, "status"),
				"results_count": ExtractIntFromPayload(payload, "results_count"),
				"is_terminal":   isTerminal,
			}
		}
		return map[string]any{
			"event_type":  "harness.session_finished",
			"status":      "success",
			"is_terminal": true,
		}

	case "activate_testing_guide":
		// S-04: 对齐 Python — chat.delta 事件
		// Python: interface_deep._parse_stream_chunk L5261-5266
		text := ExtractStringFromPayload(payload, "text")
		if text == "" {
			return nil
		}
		return map[string]any{"event_type": "chat.delta", "content": text}

	default:
		// Python: L5268-5276: payload 是 dict 时检查 traceId/invokeId
		if len(payload) > 0 {
			// 跳过含 traceId/invokeId 的内部帧
			if _, ok := payload["traceId"]; ok {
				return nil
			}
			if _, ok := payload["invokeId"]; ok {
				return nil
			}
			// team 命名空间事件直传
			if innerEventType, ok := payload["event_type"].(string); ok {
				if strings.HasPrefix(innerEventType, "team.") {
					result := map[string]any{}
					for k, v := range payload {
						result[k] = SerializeValue(v)
					}
					return result
				}
				// chat.tracer_agent 特殊处理（对齐 Python: _serialize_chunk_recursive 递归序列化）
				if innerEventType == "chat.tracer_agent" {
					result := map[string]any{"event_type": "chat." + chunkType}
					for k, v := range payload {
						result[k] = serializeChunkRecursive(v)
					}
					return result
				}
			}
			// 其他事件：透传所有键（对齐 Python: **{k: _serialize_value(v) for k, v in payload.items()}）
			result := map[string]any{"event_type": "chat." + chunkType}
			for k, v := range payload {
				result[k] = SerializeValue(v)
			}
			// 保留空白内容过滤
			content := ""
			if c, ok := result["content"].(string); ok && c != "" {
				content = c
			}
			if content == "" {
				if o, ok := result["output"].(string); ok && o != "" {
					content = o
				}
			}
			if content == "" || strings.TrimSpace(content) == "" {
				return nil
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
// Python: interface_deep._parse_stream_chunk L5165-5179
// 先检查 activate_confirm 交互类型（返回 harness.activate_interaction 事件），
// 否则委托 converter 进行交互转换；
// 若 converter 为 nil，回退为默认行为，直接返回 chat.interaction 事件。
func ParseInteractionPayload(payload map[string]any, converter InteractionConverterFunc) map[string]any {
	// S-03: 先检查 activate_confirm
	// Python: if isinstance(payload, dict) and payload.get("interaction_type") == "activate_confirm"
	if payload["interaction_type"] == "activate_confirm" {
		extRuntimePath := ExtractStringFromPayload(payload, "extension_runtime_path")
		if extRuntimePath == "" {
			extRuntimePath = ExtractStringFromPayload(payload, "runtime_path")
		}
		options := []string{"accept", "reject"}
		if opts, ok := payload["options"].([]any); ok && len(opts) > 0 {
			options = make([]string, 0, len(opts))
			for _, o := range opts {
				if s, ok := o.(string); ok {
					options = append(options, s)
				}
			}
		}
		return map[string]any{
			"event_type":             "harness.activate_interaction",
			"interaction_type":       "activate_confirm",
			"interaction_id":         ExtractStringFromPayload(payload, "interaction_id"),
			"extension_name":         ExtractStringFromPayload(payload, "extension_name"),
			"runtime_path":           ExtractStringFromPayload(payload, "runtime_path"),
			"session_runtime_path":   ExtractStringFromPayload(payload, "session_runtime_path"),
			"extension_runtime_path": extRuntimePath,
			"options":                options,
		}
	}
	if converter != nil {
		return converter(payload)
	}
	return map[string]any{
		"event_type":  "chat.interaction",
		"interaction": payload,
	}
}

// AccumulateUsage 累加 usage 信息。
// Python: usage_accumulator 的累加逻辑 (line 4580-4610)
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
		logger.Warn(logComponent).
			Str("key", key).
			Str("actual_type", fmt.Sprintf("%T", v)).
			Msg("ExtractIntFromPayload 类型断言失败，使用默认值 0")
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
		logger.Warn(logComponent).
			Str("key", key).
			Str("actual_type", fmt.Sprintf("%T", v)).
			Msg("ExtractFloatFromPayload 类型断言失败，使用默认值 0")
		return 0
	}
}

// SerializeValue 将非 JSON 原生值序列化为前端安全的 payload。
// Python: _serialize_value(value) (stream_utils.py L472-483)
func SerializeValue(value any) any {
	switch v := value.(type) {
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	default:
		return v
	}
}

// FindInteractionPayloads 递归搜索 __interaction__ 载荷。
// Python: _find_interaction_payloads(obj, _depth=0, _seen=None) (stream_utils.py L378-432)
func FindInteractionPayloads(obj any, opts ...FindOption) []any {
	return findInteractionPayloadsRecursive(obj, 0, nil)
}

// FindInteractionPayload 返回第一个匹配的 __interaction__ 载荷。
// Python: _find_interaction_payload(obj) (stream_utils.py L435-443)
func FindInteractionPayload(obj any, opts ...FindOption) any {
	matches := FindInteractionPayloads(obj, opts...)
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// serializeChunkRecursive 递归序列化嵌套 dict/list（对齐 Python: _serialize_chunk_recursive）。
// Python: stream_utils._serialize_chunk_recursive(v) — 对 dict/list 递归调用 _serialize_value
func serializeChunkRecursive(v any) any {
	switch val := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v2 := range val {
			result[k] = serializeChunkRecursive(v2)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, v2 := range val {
			result[i] = serializeChunkRecursive(v2)
		}
		return result
	default:
		return SerializeValue(val)
	}
}

// findInteractionPayloadsRecursive 递归搜索 __interaction__ 载荷。
// Python: _find_interaction_payloads 内部递归逻辑 (stream_utils.py L378-432)
func findInteractionPayloadsRecursive(obj any, depth int, seen map[uintptr]bool) []any {
	if obj == nil || depth > 8 {
		return nil
	}
	// 循环检测：对 map/slice/ptr 取地址作 seen 标记
	rv := reflect.ValueOf(obj)
	if rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice || rv.Kind() == reflect.Pointer {
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

// parseControllerOutputInteractions 解析 controller_output 中发现的 __interaction__ 载荷列表。
// Python: stream_utils._parse_interaction_payload(interactions)
//
// 先检查是否存在 activate_confirm 类型（返回 interaction.activate_confirm 事件），
// 否则委托 converter 进行交互转换；若 converter 为 nil，回退为默认行为。
func parseControllerOutputInteractions(interactions []any, converter InteractionConverterFunc) map[string]any {
	// S-04: 对齐 Python _parse_interaction_payload — 遍历 interactions 检查 activate_confirm
	// Python: for interaction in interactions:
	// Python:     interaction_type = interaction.get("__interaction__")
	// Python:     if interaction_type == "activate_confirm": return {...}
	for _, interaction := range interactions {
		if m, ok := interaction.(map[string]any); ok {
			if m["__interaction__"] == "activate_confirm" {
				extRuntimePath := ExtractStringFromPayload(m, "extension_runtime_path")
				if extRuntimePath == "" {
					extRuntimePath = ExtractStringFromPayload(m, "runtime_path")
				}
				options := []string{"accept", "reject"}
				if opts, ok := m["options"].([]any); ok && len(opts) > 0 {
					options = make([]string, 0, len(opts))
					for _, o := range opts {
						if s, ok := o.(string); ok {
							options = append(options, s)
						}
					}
				}
				return map[string]any{
					"event_type":             "interaction.activate_confirm",
					"interaction":            m,
					"interaction_type":       "activate_confirm",
					"interaction_id":         ExtractStringFromPayload(m, "interaction_id"),
					"extension_name":         ExtractStringFromPayload(m, "extension_name"),
					"runtime_path":           ExtractStringFromPayload(m, "runtime_path"),
					"session_runtime_path":   ExtractStringFromPayload(m, "session_runtime_path"),
					"extension_runtime_path": extRuntimePath,
					"options":                options,
				}
			}
		}
	}
	// 默认：委托 converter 转为 ask_user_question
	// Python: return convert_interactions_to_ask_user_question(interactions)
	if converter != nil {
		return converter(interactions)
	}
	return map[string]any{
		"event_type":  "chat.interaction",
		"interaction": interactions,
	}
}
