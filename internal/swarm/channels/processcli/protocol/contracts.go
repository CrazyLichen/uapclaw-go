package protocol

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ProtocolRevision 协议修订号。
	// Python: contracts.py:13 PROTOCOL_REVISION = 1
	ProtocolRevision = 1
)

// ──────────────────────────── 全局变量 ────────────────────────────

// StableEventTypes 在 Process CLI 传输边界补齐稳定公开字段的事件类型。
// Python: contracts.py:14-21 STABLE_EVENT_TYPES
var StableEventTypes = []string{
	"chat.delta",
	"chat.final",
	"chat.tool_call",
	"chat.tool_result",
	"interaction.requested",
	"host_tool.requested",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PublicEventPayload 返回去引用的 payload，同时为既有宿主保留旧字段。
// Python: contracts.py:77-127 public_event_payload
func PublicEventPayload(eventType string, payload any) any {
	if !isStableEventType(eventType) {
		return payload
	}
	source, ok := payload.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	result := jsonObjectFromAny(source)

	switch eventType {
	case "chat.delta", "chat.final":
		// Python: :82-83 result["text"] = _text(result)
		result["text"] = contractText(result)
	case "interaction.requested":
		// Python: :84-95
		interaction, _ := result["interaction"].(map[string]any)
		if interaction == nil {
			interaction = map[string]any{}
		}
		sourceValue := asString(interaction["source"])
		switch sourceValue {
		case "permission_interrupt":
			result["kind"] = "permission"
		case "confirm_interrupt":
			result["kind"] = "confirmation"
		default:
			result["kind"] = "question"
		}
		result["questions"] = contractQuestions(interaction)
	case "chat.tool_call", "chat.tool_result":
		// Python: :96-126
		call, _ := result["tool_call"].(map[string]any)
		if call == nil {
			call = result
		}
		arguments := call["arguments"]
		if argStr, isStr := arguments.(string); isStr {
			arguments = parseToolArguments(argStr)
		}
		argMap, isMap := arguments.(map[string]any)
		if !isMap {
			argMap = nil
		}
		// Python: status 三态
		status := "completed"
		if eventType == "chat.tool_call" {
			status = "started"
		} else if success, isBool := result["success"].(bool); isBool && !success {
			status = "failed"
		}
		var resultValue any
		if eventType == "chat.tool_result" {
			resultValue = result["result"]
		}
		tool := map[string]any{
			"call_id": firstNonEmptyString(
				asString(call["tool_call_id"]), asString(result["tool_call_id"])),
			"name": firstNonEmptyString(
				asString(call["name"]), asString(result["tool_name"])),
			"status": status,
			"result": resultValue,
		}
		if argMap != nil {
			tool["arguments"] = ThawJson(argMap)
		} else {
			tool["arguments"] = nil
		}
		result["tool"] = tool
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// contractText 依次取 delta/content/text/message/answer 的第一个字符串值。
// Python: contracts.py:35-40 _text
//
// 注意与 machine_result._text 的差别：此处只判 isinstance(value, str)，
// 空串也算命中（上游此函数不判非空）。
func contractText(payload map[string]any) string {
	for _, key := range []string{"delta", "content", "text", "message", "answer"} {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}

// contractQuestions 规范化交互卡片的问题列表。
// Python: contracts.py:43-74 _questions
func contractQuestions(interaction map[string]any) []any {
	result := make([]any, 0)
	rawQuestions, _ := interaction["questions"].([]any)
	for index, rawQuestion := range rawQuestions {
		question, ok := rawQuestion.(map[string]any)
		if !ok {
			continue
		}
		options := make([]any, 0)
		rawOptions, _ := question["options"].([]any)
		for _, rawOption := range rawOptions {
			switch option := rawOption.(type) {
			case map[string]any:
				// Python: value = str(option.get("value", option.get("label", "")))
				value := asString(option["value"])
				if value == "" {
					value = asString(option["label"])
				}
				label := asString(option["label"])
				if label == "" {
					label = value
				}
				options = append(options, map[string]any{"value": value, "label": label})
			case string:
				options = append(options, map[string]any{"value": option, "label": option})
			}
		}
		// Python: allow_custom_input 默认值 = source != "permission_interrupt"
		defaultAllowCustom := asString(interaction["source"]) != "permission_interrupt"
		allowCustom := defaultAllowCustom
		if raw, ok := question["allow_custom_input"].(bool); ok {
			allowCustom = raw
		}
		var cardID any
		if raw, ok := question["card_id"].(string); ok {
			cardID = raw
		}
		questionText := firstNonEmptyString(
			asString(question["question"]), asString(question["title"]))
		result = append(result, map[string]any{
			"question_id":       fmt.Sprintf("q%d", index),
			"question":          questionText,
			"options":           options,
			"card_id":           cardID,
			"allow_custom_input": allowCustom,
		})
	}
	return result
}

// parseToolArguments 解析工具参数 JSON 字符串（拒绝 NaN/Inf）。
// Python: contracts.py:100-110 parse_constant / parse_float 校验
func parseToolArguments(raw string) any {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	if !hasFiniteNumbers(value) {
		return nil
	}
	return normalizeJsonNumbers(value)
}

// hasFiniteNumbers 递归检查所有数字是否为有限值。
func hasFiniteNumbers(value any) bool {
	switch v := value.(type) {
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return false
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
		return true
	case map[string]any:
		for _, item := range v {
			if !hasFiniteNumbers(item) {
				return false
			}
		}
		return true
	case []any:
		for _, item := range v {
			if !hasFiniteNumbers(item) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

// normalizeJsonNumbers 把 json.Number 还原为 float64（与 Python json.loads 一致）。
func normalizeJsonNumbers(value any) any {
	switch v := value.(type) {
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return nil
		}
		return f
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = normalizeJsonNumbers(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizeJsonNumbers(item)
		}
		return out
	default:
		return value
	}
}

// isStableEventType 是否为稳定事件类型。
func isStableEventType(eventType string) bool {
	for _, v := range StableEventTypes {
		if v == eventType {
			return true
		}
	}
	return false
}

// firstNonEmptyString 返回第一个非空字符串。
func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
