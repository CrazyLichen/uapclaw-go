package schema

import (
	"encoding/json"
	"testing"
)

// TestNewToolCall 验证 NewToolCall 默认 Type 为 "function"，可选参数生效。
func TestNewToolCall(t *testing.T) {
	tc := NewToolCall("call_123", "get_weather", `{"city":"Beijing"}`)
	if tc.ID != "call_123" {
		t.Errorf("ID = %q, want %q", tc.ID, "call_123")
	}
	if tc.Type != "function" {
		t.Errorf("Type = %q, want %q", tc.Type, "function")
	}
	if tc.Name != "get_weather" {
		t.Errorf("Name = %q, want %q", tc.Name, "get_weather")
	}
	if tc.Arguments != `{"city":"Beijing"}` {
		t.Errorf("Arguments = %q, want %q", tc.Arguments, `{"city":"Beijing"}`)
	}
	if tc.Index != 0 {
		t.Errorf("Index = %d, want 0", tc.Index)
	}
}

// TestNewToolCall_WithOptions 验证 WithToolCallIndex 和 WithToolCallType 选项。
func TestNewToolCall_WithOptions(t *testing.T) {
	tc := NewToolCall("call_1", "search", `{}`,
		WithToolCallIndex(2),
		WithToolCallType("custom"),
	)
	if tc.Index != 2 {
		t.Errorf("Index = %d, want 2", tc.Index)
	}
	if tc.Type != "custom" {
		t.Errorf("Type = %q, want %q", tc.Type, "custom")
	}
}

// TestToolCall_ToOpenAIFormat 验证扁平格式转 OpenAI 嵌套格式。
func TestToolCall_ToOpenAIFormat(t *testing.T) {
	tc := NewToolCall("call_123", "get_weather", `{"city":"Beijing"}`, WithToolCallIndex(0))
	result := tc.ToOpenAIFormat()

	if result["id"] != "call_123" {
		t.Errorf("id = %v, want %q", result["id"], "call_123")
	}
	if result["type"] != "function" {
		t.Errorf("type = %v, want %q", result["type"], "function")
	}

	fn, ok := result["function"].(map[string]any)
	if !ok {
		t.Fatal("function 字段不是 map[string]any")
	}
	if fn["name"] != "get_weather" {
		t.Errorf("function.name = %v, want %q", fn["name"], "get_weather")
	}
	if fn["arguments"] != `{"city":"Beijing"}` {
		t.Errorf("function.arguments = %v, want %q", fn["arguments"], `{"city":"Beijing"}`)
	}
}

// TestToolCall_ToOpenAIFormat_OmitsEmpty 验证 ID 为空时不输出 id 字段。
func TestToolCall_ToOpenAIFormat_OmitsEmpty(t *testing.T) {
	tc := NewToolCall("", "search", `{}`)
	result := tc.ToOpenAIFormat()

	if _, ok := result["id"]; ok {
		t.Error("空 ID 不应输出 id 字段")
	}
}

// TestToolCall_UnmarshalJSON_FlatFormat 验证扁平格式 JSON 反序列化。
func TestToolCall_UnmarshalJSON_FlatFormat(t *testing.T) {
	jsonStr := `{"id":"call_1","type":"function","name":"search","arguments":"{}","index":1}`
	var tc ToolCall
	if err := json.Unmarshal([]byte(jsonStr), &tc); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if tc.ID != "call_1" {
		t.Errorf("ID = %q, want %q", tc.ID, "call_1")
	}
	if tc.Type != "function" {
		t.Errorf("Type = %q, want %q", tc.Type, "function")
	}
	if tc.Name != "search" {
		t.Errorf("Name = %q, want %q", tc.Name, "search")
	}
	if tc.Index != 1 {
		t.Errorf("Index = %d, want 1", tc.Index)
	}
}

// TestToolCall_UnmarshalJSON_OpenAIFormat 验证 OpenAI 嵌套格式自动转换为扁平格式。
func TestToolCall_UnmarshalJSON_OpenAIFormat(t *testing.T) {
	// OpenAI API 返回的嵌套格式
	jsonStr := `{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Beijing\"}"}}`
	var tc ToolCall
	if err := json.Unmarshal([]byte(jsonStr), &tc); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if tc.ID != "call_abc" {
		t.Errorf("ID = %q, want %q", tc.ID, "call_abc")
	}
	if tc.Type != "function" {
		t.Errorf("Type = %q, want %q", tc.Type, "function")
	}
	if tc.Name != "get_weather" {
		t.Errorf("Name = %q, want %q", tc.Name, "get_weather")
	}
	if tc.Arguments != `{"city":"Beijing"}` {
		t.Errorf("Arguments = %q, want %q", tc.Arguments, `{"city":"Beijing"}`)
	}
}

// TestToolCall_UnmarshalJSON_OpenAIFormat_DefaultType 验证 OpenAI 格式缺失 type 时默认为 "function"。
func TestToolCall_UnmarshalJSON_OpenAIFormat_DefaultType(t *testing.T) {
	jsonStr := `{"id":"call_1","function":{"name":"search","arguments":"{}"}}`
	var tc ToolCall
	if err := json.Unmarshal([]byte(jsonStr), &tc); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if tc.Type != "function" {
		t.Errorf("Type = %q, want %q", tc.Type, "function")
	}
}

// TestToolCall_MarshalJSON 验证 MarshalJSON 输出内部扁平格式。
func TestToolCall_MarshalJSON(t *testing.T) {
	tc := NewToolCall("call_1", "search", `{}`)
	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	// 验证输出是扁平格式（不应包含 "function" 嵌套键）
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("解析序列化结果失败: %v", err)
	}
	if _, ok := result["function"]; ok {
		t.Error("MarshalJSON 不应输出嵌套的 function 键，应输出扁平格式")
	}
	if result["name"] != "search" {
		t.Errorf("name = %v, want %q", result["name"], "search")
	}
}

// TestToolCall_RoundTrip 验证序列化→反序列化一致性（扁平格式）。
func TestToolCall_RoundTrip(t *testing.T) {
	original := NewToolCall("call_rt", "test_fn", `{"key":"value"}`, WithToolCallIndex(3))
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored ToolCall
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.ID != original.ID {
		t.Errorf("ID: got %q, want %q", restored.ID, original.ID)
	}
	if restored.Type != original.Type {
		t.Errorf("Type: got %q, want %q", restored.Type, original.Type)
	}
	if restored.Name != original.Name {
		t.Errorf("Name: got %q, want %q", restored.Name, original.Name)
	}
	if restored.Arguments != original.Arguments {
		t.Errorf("Arguments: got %q, want %q", restored.Arguments, original.Arguments)
	}
	if restored.Index != original.Index {
		t.Errorf("Index: got %d, want %d", restored.Index, original.Index)
	}
}
