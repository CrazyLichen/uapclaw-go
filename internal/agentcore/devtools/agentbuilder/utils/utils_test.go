package utils

import (
	"strings"
	"testing"
)

// TestExtractJSONFromText 对齐 Python TestExtractJsonFromText（7 用例）
func TestExtractJSONFromText(t *testing.T) {
	t.Run("json代码块提取", func(t *testing.T) {
		if got := ExtractJSONFromText("```json\n{\"key\": \"value\"}\n```"); got != "{\"key\": \"value\"}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("裸代码块提取", func(t *testing.T) {
		if got := ExtractJSONFromText("```\n{\"key\": \"value\"}\n```"); got != "{\"key\": \"value\"}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("无代码块返回原文", func(t *testing.T) {
		if got := ExtractJSONFromText("{\"key\": \"value\"}"); got != "{\"key\": \"value\"}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("空文本", func(t *testing.T) {
		if got := ExtractJSONFromText(""); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("None转空串等价", func(t *testing.T) {
		// Python: extract_json_from_text(None) 返回 None；Go 字符串零值即 ""
		if got := ExtractJSONFromText(""); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("json数组", func(t *testing.T) {
		if got := ExtractJSONFromText("```json\n[1, 2, 3]\n```"); got != "[1, 2, 3]" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("多行json", func(t *testing.T) {
		got := ExtractJSONFromText("```json\n{\"key1\": \"value1\",\n\"key2\": \"value2\"}\n```")
		if !strings.Contains(got, "key1") || !strings.Contains(got, "key2") {
			t.Errorf("got %q", got)
		}
	})
}

// TestFormatDialogHistory 对齐 Python TestFormatDialogHistory（5 用例）
func TestFormatDialogHistory(t *testing.T) {
	t.Run("单条消息", func(t *testing.T) {
		got := FormatDialogHistory([]map[string]string{{"role": "user", "content": "Hello"}})
		if got != "user: Hello" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("多条消息", func(t *testing.T) {
		history := []map[string]string{
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi! How can I help you?"},
		}
		got := FormatDialogHistory(history)
		expected := "user: Hello\nassistant: Hi! How can I help you?"
		if got != expected {
			t.Errorf("got %q, expected %q", got, expected)
		}
	})
	t.Run("空历史", func(t *testing.T) {
		if got := FormatDialogHistory(nil); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("自定义分隔符", func(t *testing.T) {
		history := []map[string]string{
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi"},
		}
		got := FormatDialogHistory(history, " | ")
		if got != "user: Hello | assistant: Hi" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("缺失键", func(t *testing.T) {
		history := []map[string]string{
			{"role": "user"},            // 无 content
			{"content": "Missing role"}, // 无 role
		}
		got := FormatDialogHistory(history)
		if !strings.Contains(got, "user: ") || !strings.Contains(got, "unknown: Missing role") {
			t.Errorf("got %q", got)
		}
	})
}

// TestValidateSessionID 对齐 Python TestValidateSessionId（8 用例）
func TestValidateSessionID(t *testing.T) {
	t.Run("字母数字有效", func(t *testing.T) {
		if !ValidateSessionID("abc123XYZ") {
			t.Error("应为有效")
		}
	})
	t.Run("下划线有效", func(t *testing.T) {
		if !ValidateSessionID("user_001_session") {
			t.Error("应为有效")
		}
	})
	t.Run("连字符有效", func(t *testing.T) {
		if !ValidateSessionID("session-12345") {
			t.Error("应为有效")
		}
	})
	t.Run("组合有效", func(t *testing.T) {
		if !ValidateSessionID("test-session-2024") {
			t.Error("应为有效")
		}
	})
	t.Run("特殊字符无效", func(t *testing.T) {
		if ValidateSessionID("session@123") {
			t.Error("应为无效")
		}
	})
	t.Run("空格无效", func(t *testing.T) {
		if ValidateSessionID("session 123") {
			t.Error("应为无效")
		}
	})
	t.Run("空串无效", func(t *testing.T) {
		if ValidateSessionID("") {
			t.Error("应为无效")
		}
	})
	t.Run("None转空串等价", func(t *testing.T) {
		// Python: validate_session_id(None) is False；Go 字符串零值即 ""
		if ValidateSessionID("") {
			t.Error("应为无效")
		}
	})
}

// TestMergeDictLists 对齐 Python TestMergeDictLists（6 用例）
func TestMergeDictLists(t *testing.T) {
	t.Run("唯一键合并", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		newItems := []map[string]any{{"id": "2", "name": "B"}}
		result := MergeDictLists(existing, newItems, "id")
		if len(result) != 2 || result[0]["id"] != "1" || result[1]["id"] != "2" {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("重复键保留首个", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		newItems := []map[string]any{{"id": "1", "name": "B"}}
		result := MergeDictLists(existing, newItems, "id")
		if len(result) != 1 || result[0]["name"] != "A" {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("空新增", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		result := MergeDictLists(existing, nil, "id")
		if len(result) != 1 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("空存量", func(t *testing.T) {
		newItems := []map[string]any{{"id": "1", "name": "A"}}
		result := MergeDictLists([]map[string]any{}, newItems, "id")
		if len(result) != 1 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("双空", func(t *testing.T) {
		result := MergeDictLists(nil, nil, "id")
		if len(result) != 0 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("缺失唯一键跳过", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		newItems := []map[string]any{{"name": "B"}} // 无 id 键
		result := MergeDictLists(existing, newItems, "id")
		if len(result) != 1 {
			t.Errorf("result=%v", result)
		}
	})
}
