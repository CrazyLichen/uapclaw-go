package browser_move

import (
	"encoding/json"
	"strings"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// unsupportedSchemaKeys OpenAI 兼容 API 不支持的 schema 关键字
// 对齐 Python: _UNSUPPORTED_SCHEMA_KEYS
var unsupportedSchemaKeys = map[string]bool{
	"$schema":     true,
	"$id":         true,
	"$defs":       true,
	"definitions": true,
	"$comment":    true,
	"$anchor":     true,
	"$vocabulary": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractJSONObject 从模型/工具文本中尽力提取 JSON 对象。
// 对齐 Python: extract_json_object(text)
func ExtractJSONObject(text any) map[string]any {
	// 如果输入已经是 map，直接返回
	if m, ok := text.(map[string]any); ok {
		return m
	}
	if text == nil {
		return map[string]any{}
	}

	raw := strings.TrimSpace(strVal(text))
	if raw == "" {
		return map[string]any{}
	}

	// 对齐 Python: Playwright 特殊标记处理
	// ### 结果 ... ### 执行 Playwright 代码
	markerResult := "### Result"
	markerRan := "### Ran Playwright code"
	if strings.Contains(raw, markerResult) && strings.Contains(raw, markerRan) {
		start := strings.Index(raw, markerResult) + len(markerResult)
		end := strings.Index(raw[start:], markerRan)
		if end > 0 {
			raw = strings.TrimSpace(raw[start : start+end])
		}
	}

	// 尝试直接 JSON 解析（最多两轮，对齐 Python 的 for _ in range(2)）
	for i := 0; i < 2; i++ {
		var parsed any
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			break
		}
		if m, ok := parsed.(map[string]any); ok {
			return m
		}
		if s, ok := parsed.(string); ok {
			raw = strings.TrimSpace(s)
			continue
		}
		break
	}

	// 尝试 ```json 代码块提取
	if strings.Contains(raw, "```json") {
		start := strings.Index(raw, "```json") + len("```json")
		end := strings.Index(raw[start:], "```")
		if end > 0 {
			block := strings.TrimSpace(raw[start : start+end])
			var parsed map[string]any
			if err := json.Unmarshal([]byte(block), &parsed); err == nil {
				return parsed
			}
		}
	}

	// 尝试首尾花括号匹配
	first := strings.Index(raw, "{")
	last := strings.LastIndex(raw, "}")
	if first >= 0 && last > first {
		snippet := raw[first : last+1]
		var parsed map[string]any
		if err := json.Unmarshal([]byte(snippet), &parsed); err == nil {
			return parsed
		}
	}

	return map[string]any{}
}

// SanitizeJSONSchema 递归清除 OpenAI 兼容 API 不支持的 schema 关键字。
// 对齐 Python: sanitize_json_schema(schema)
func SanitizeJSONSchema(schema any) any {
	m, ok := schema.(map[string]any)
	if !ok {
		return schema
	}

	// 折叠 anyOf/oneOf nullable 简写: [{type: X}, {type: "null"}] → type: X
	for _, kw := range []string{"anyOf", "oneOf"} {
		variants, ok := m[kw].([]any)
		if !ok || len(variants) != 2 {
			continue
		}
		nonNull := []any{}
		nullCount := 0
		for _, v := range variants {
			if vm, ok := v.(map[string]any); ok {
				if t, ok := vm["type"]; ok && t == "null" {
					nullCount++
				} else {
					nonNull = append(nonNull, v)
				}
			}
		}
		if nullCount == 1 && len(nonNull) == 1 {
			merged := map[string]any{}
			for k, v := range m {
				if k != kw {
					merged[k] = v
				}
			}
			if nm, ok := nonNull[0].(map[string]any); ok {
				for k, v := range nm {
					merged[k] = v
				}
			}
			m = merged
		}
	}

	// 移除不支持的关键字
	cleaned := map[string]any{}
	for k, v := range m {
		if !unsupportedSchemaKeys[k] {
			cleaned[k] = v
		}
	}

	// null 类型 → "object"
	if t, ok := cleaned["type"]; ok && t == nil {
		cleaned["type"] = "object"
	}

	// 递归处理 properties
	if props, ok := cleaned["properties"].(map[string]any); ok {
		newProps := map[string]any{}
		for k, v := range props {
			newProps[k] = SanitizeJSONSchema(v)
		}
		cleaned["properties"] = newProps
	}

	// 递归处理 items/additionalProperties/not
	for _, kw := range []string{"items", "additionalProperties", "not"} {
		if v, ok := cleaned[kw]; ok {
			cleaned[kw] = SanitizeJSONSchema(v)
		}
	}

	// 递归处理 anyOf/oneOf/allOf
	for _, kw := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := cleaned[kw].([]any); ok {
			newArr := make([]any, len(arr))
			for i, v := range arr {
				newArr[i] = SanitizeJSONSchema(v)
			}
			cleaned[kw] = newArr
		}
	}

	return cleaned
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strVal 将 any 转换为字符串
func strVal(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
