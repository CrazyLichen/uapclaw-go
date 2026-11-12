package requestsanitizer

import (
	"testing"
)

// TestCleanUnicode 验证孤立 UTF-16 代理位替换与嵌套清洗。
// Python: request_sanitizer.py:13 clean_unicode
func TestCleanUnicode(t *testing.T) {
	in := string([]rune{0xD800}) + "isolated"
	got := CleanUnicode(in).(string)
	for _, r := range got {
		if r >= 0xD800 && r <= 0xDFFF {
			t.Fatalf("孤立代理位未被替换: %q", got)
		}
	}
	if CleanUnicode("正常文本") != "正常文本" {
		t.Fatal("合法文本应原样返回")
	}
	m := CleanUnicode(map[string]any{"k": []any{string([]rune{0xDC00})}}).(map[string]any)
	if s := m["k"].([]any)[0].(string); containsSurrogateStr(s) {
		t.Fatalf("嵌套清洗缺失: %q", s)
	}
	// 合法代理对保留（Python: preserve valid pairs）
	pair := "\U0001F600" // 😀 合法四字节字符
	if CleanUnicode(pair) != pair {
		t.Fatal("合法字符不应被改写")
	}
	// 非 str/dict/list 类型原样返回
	if CleanUnicode(42) != 42 || CleanUnicode(nil) != nil {
		t.Fatal("其他类型应原样返回")
	}
}

// TestJSONArguments 验证参数转 JSON 字符串。
// Python: request_sanitizer.py:28 json_arguments
func TestJSONArguments(t *testing.T) {
	if got := JSONArguments(`{"a":1}`); got != `{"a":1}` {
		t.Fatalf("字符串直通: %q", got)
	}
	if got := JSONArguments(nil); got != `{}` {
		t.Fatalf("nil 兜底空对象: %q", got)
	}
	if got := JSONArguments(map[string]any{"a": 1}); got != `{"a":1}` {
		t.Fatalf("dict→JSON: %q", got)
	}
}

// TestSanitizeChatRequest 验证 chat 请求构建出口的归一化。
// Python: request_sanitizer.py:35 sanitize_chat_request
func TestSanitizeChatRequest(t *testing.T) {
	params := map[string]any{"messages": []any{map[string]any{
		"role": "assistant",
		"tool_calls": []any{map[string]any{
			"id": "c1", "type": "function",
			"function": map[string]any{"name": "f", "arguments": map[string]any{"a": 1}},
		}},
	}}}
	got := SanitizeChatRequest(params)
	msg := got["messages"].([]any)[0].(map[string]any)
	call := msg["tool_calls"].([]any)[0].(map[string]any)
	fn := call["function"].(map[string]any)
	if fn["arguments"] != `{"a":1}` {
		t.Fatalf("arguments 未转 JSON 字符串: %v", fn["arguments"])
	}
	// 非 dict 消息跳过（防御分支）
	got2 := SanitizeChatRequest(map[string]any{"messages": []any{"not-a-dict"}})
	if len(got2["messages"].([]any)) != 1 {
		t.Fatal("非 dict 消息应原样保留")
	}
}

// containsSurrogateStr 测试辅助：字符串是否含代理位。
func containsSurrogateStr(s string) bool {
	for _, r := range s {
		if r >= 0xD800 && r <= 0xDFFF {
			return true
		}
	}
	return false
}
