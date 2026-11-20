package requestsanitizer

import (
	"encoding/json"
	"unicode/utf16"
	"unicode/utf8"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CleanUnicode 保留合法 UTF-16 代理对，替换 DOM 切片产生的孤立 UTF-16 半体。
// 递归清洗 dict/list；str 直接处理；其他类型原样返回。
// Python: request_sanitizer.py:13 clean_unicode
func CleanUnicode(value any) any {
	switch v := value.(type) {
	case string:
		return cleanString(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[CleanUnicode(k).(string)] = CleanUnicode(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = CleanUnicode(item)
		}
		return out
	default:
		return value
	}
}

// JSONArguments 把 ToolCall 参数归一为 JSON 字符串（dict 可直接序列化，
// provider 侧 function.arguments 必须是字符串）。
// Python: request_sanitizer.py:28 json_arguments
func JSONArguments(value any) string {
	if s, ok := value.(string); ok {
		return CleanUnicode(s).(string)
	}
	if value == nil {
		value = map[string]any{}
	}
	cleaned := CleanUnicode(value)
	b, err := json.Marshal(cleaned)
	if err != nil {
		// 序列化失败退回空对象，保持非变更式语义
		return "{}"
	}
	return string(b)
}

// SanitizeChatRequest 清洗整个 chat 请求体，并把每条消息 tool_calls 内
// 嵌套 function.arguments 归一为 JSON 字符串。
// Python: request_sanitizer.py:35 sanitize_chat_request
func SanitizeChatRequest(params map[string]any) map[string]any {
	result := CleanUnicode(params).(map[string]any)
	messages, _ := result["messages"].([]any)
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		calls, _ := message["tool_calls"].([]any)
		for _, callItem := range calls {
			call, ok := callItem.(map[string]any)
			if !ok {
				continue
			}
			function, ok := call["function"].(map[string]any)
			if ok {
				function["arguments"] = JSONArguments(function["arguments"])
			}
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cleanString 单字符串清洗：含孤立代理位时按 UTF-16-LE 语义修复，
// 成对代理位组合为码点，孤立半体替换为替换符。
// Python: value.encode("utf-16-le","surrogatepass").decode("utf-16-le","replace")
func cleanString(s string) string {
	// 快路径：无代理位（合法字符串必然无孤立代理位）
	needFix := false
	for _, r := range s {
		if r >= 0xD800 && r <= 0xDFFF {
			needFix = true
			break
		}
	}
	if !needFix {
		return s
	}

	// 逐 rune 扫描：成对代理位组合，孤立半体替换
	runes := []rune(s)
	var out []rune
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r < 0xD800 || r > 0xDFFF {
			out = append(out, r)
			continue
		}
		// 高代理位后跟低代理位 → 组合
		if r <= 0xDBFF && i+1 < len(runes) && runes[i+1] >= 0xDC00 && runes[i+1] <= 0xDFFF {
			combined := utf16.DecodeRune(r, runes[i+1])
			if combined != utf8.RuneError {
				out = append(out, combined)
			} else {
				out = append(out, utf8.RuneError)
			}
			i++
			continue
		}
		// 孤立代理位 → 替换符
		out = append(out, utf8.RuneError)
	}
	return string(out)
}
