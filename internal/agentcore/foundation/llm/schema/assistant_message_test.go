package schema

import (
	"encoding/json"
	"testing"
)

// TestNewAssistantMessage 验证默认 finish_reason="null"，role="assistant"。
func TestNewAssistantMessage(t *testing.T) {
	msg := NewAssistantMessage("你好")
	if msg.Role != RoleTypeAssistant {
		t.Errorf("Role = %v, want %v", msg.Role, RoleTypeAssistant)
	}
	if msg.Content.Text() != "你好" {
		t.Errorf("Content = %q, want %q", msg.Content.Text(), "你好")
	}
	if msg.FinishReason != FinishReasonNull {
		t.Errorf("FinishReason = %q, want %q", msg.FinishReason, FinishReasonNull)
	}
	if msg.IsFinished() {
		t.Error("IsFinished() 应为 false（finish_reason='null'）")
	}
	if len(msg.ToolCalls) != 0 {
		t.Errorf("ToolCalls 长度 = %d, want 0", len(msg.ToolCalls))
	}
	if msg.UsageMetadata != nil {
		t.Error("UsageMetadata 应为 nil")
	}
}

// TestAssistantMessage_IsFinished 验证 "null" → false，其他 → true。
func TestAssistantMessage_IsFinished(t *testing.T) {
	msg1 := NewAssistantMessage("test")
	if msg1.IsFinished() {
		t.Error("finish_reason='null' 时 IsFinished() 应为 false")
	}

	msg2 := NewAssistantMessage("test", WithFinishReason("stop"))
	if !msg2.IsFinished() {
		t.Error("finish_reason='stop' 时 IsFinished() 应为 true")
	}

	msg3 := NewAssistantMessage("test", WithFinishReason("length"))
	if !msg3.IsFinished() {
		t.Error("finish_reason='length' 时 IsFinished() 应为 true")
	}
}

// TestAssistantMessage_WithToolCalls 验证 WithToolCalls 选项。
func TestAssistantMessage_WithToolCalls(t *testing.T) {
	calls := []*ToolCall{
		NewToolCall("call_1", "search", `{"query":"test"}`),
		NewToolCall("call_2", "calculate", `{"expr":"1+1"}`, WithToolCallIndex(1)),
	}
	msg := NewAssistantMessage("", WithToolCalls(calls))
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("ToolCalls 长度 = %d, want 2", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", msg.ToolCalls[0].Name, "search")
	}
	if msg.ToolCalls[1].Name != "calculate" {
		t.Errorf("ToolCalls[1].Name = %q, want %q", msg.ToolCalls[1].Name, "calculate")
	}
}

// TestAssistantMessage_WithUsageMetadata 验证 WithAssistantUsageMetadata 选项。
func TestAssistantMessage_WithUsageMetadata(t *testing.T) {
	meta := &UsageMetadata{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		ModelName:    "gpt-4",
	}
	msg := NewAssistantMessage("test", WithAssistantUsageMetadata(meta))
	if msg.UsageMetadata == nil {
		t.Fatal("UsageMetadata 不应为 nil")
	}
	if msg.UsageMetadata.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", msg.UsageMetadata.InputTokens)
	}
}

// TestAssistantMessage_WithReasoningContent 验证 WithReasoningContent 选项。
func TestAssistantMessage_WithReasoningContent(t *testing.T) {
	msg := NewAssistantMessage("test", WithReasoningContent("思考过程..."))
	if msg.ReasoningContent != "思考过程..." {
		t.Errorf("ReasoningContent = %q, want %q", msg.ReasoningContent, "思考过程...")
	}
}

// TestAssistantMessage_UnmarshalJSON_FlatToolCalls 验证扁平 tool_calls 直接解析。
func TestAssistantMessage_UnmarshalJSON_FlatToolCalls(t *testing.T) {
	jsonStr := `{
		"role": "assistant",
		"content": "让我帮你搜索",
		"tool_calls": [
			{"id": "call_1", "type": "function", "name": "search", "arguments": "{\"query\":\"test\"}"}
		],
		"finish_reason": "stop"
	}`
	var msg AssistantMessage
	if err := json.Unmarshal([]byte(jsonStr), &msg); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if msg.Role != RoleTypeAssistant {
		t.Errorf("Role = %v, want %v", msg.Role, RoleTypeAssistant)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("ToolCalls 长度 = %d, want 1", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", msg.ToolCalls[0].Name, "search")
	}
	if msg.ToolCalls[0].Arguments != `{"query":"test"}` {
		t.Errorf("ToolCalls[0].Arguments = %q, want %q", msg.ToolCalls[0].Arguments, `{"query":"test"}`)
	}
	if msg.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", msg.FinishReason, "stop")
	}
}

// TestAssistantMessage_UnmarshalJSON_OpenAIToolCalls 验证 OpenAI 嵌套 tool_calls 自动转扁平。
func TestAssistantMessage_UnmarshalJSON_OpenAIToolCalls(t *testing.T) {
	// OpenAI API 返回的嵌套格式
	jsonStr := `{
		"role": "assistant",
		"content": null,
		"tool_calls": [
			{"id": "call_abc", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Beijing\"}"}, "index": 0}
		],
		"finish_reason": "tool_calls"
	}`
	var msg AssistantMessage
	if err := json.Unmarshal([]byte(jsonStr), &msg); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("ToolCalls 长度 = %d, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("ToolCalls[0].ID = %q, want %q", tc.ID, "call_abc")
	}
	if tc.Name != "get_weather" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", tc.Name, "get_weather")
	}
	if tc.Arguments != `{"city":"Beijing"}` {
		t.Errorf("ToolCalls[0].Arguments = %q, want %q", tc.Arguments, `{"city":"Beijing"}`)
	}
	if tc.Index != 0 {
		t.Errorf("ToolCalls[0].Index = %d, want 0", tc.Index)
	}
	if msg.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want %q", msg.FinishReason, "tool_calls")
	}
}

// TestAssistantMessage_UnmarshalJSON_DefaultFinishReason 验证缺失 finish_reason 时默认为 "null"。
func TestAssistantMessage_UnmarshalJSON_DefaultFinishReason(t *testing.T) {
	jsonStr := `{"role": "assistant", "content": "hello"}`
	var msg AssistantMessage
	if err := json.Unmarshal([]byte(jsonStr), &msg); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if msg.FinishReason != FinishReasonNull {
		t.Errorf("FinishReason = %q, want %q", msg.FinishReason, FinishReasonNull)
	}
}

// TestAssistantMessage_ToOpenAIDict 验证完整 ToOpenAIDict 输出。
func TestAssistantMessage_ToOpenAIDict(t *testing.T) {
	msg := NewAssistantMessage("你好",
		WithFinishReason("stop"),
		WithAssistantUsageMetadata(&UsageMetadata{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}),
	)
	result := msg.ToOpenAIDict()

	if result["role"] != "assistant" {
		t.Errorf("role = %v, want %q", result["role"], "assistant")
	}
	if result["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v, want %q", result["finish_reason"], "stop")
	}
	// 验证空字段不输出
	if _, ok := result["tool_calls"]; ok {
		t.Error("空 ToolCalls 不应输出 tool_calls 字段")
	}
	if _, ok := result["reasoning_content"]; ok {
		t.Error("空 ReasoningContent 不应输出 reasoning_content 字段")
	}
}

// TestAssistantMessage_ToOpenAIDict_ToolCalls 验证 tool_calls 嵌套格式输出。
func TestAssistantMessage_ToOpenAIDict_ToolCalls(t *testing.T) {
	calls := []*ToolCall{
		NewToolCall("call_1", "search", `{"query":"test"}`),
	}
	msg := NewAssistantMessage("", WithToolCalls(calls), WithFinishReason("tool_calls"))
	result := msg.ToOpenAIDict()

	toolCallsRaw, ok := result["tool_calls"].([]map[string]any)
	if !ok {
		t.Fatal("tool_calls 应为 []map[string]any")
	}
	if len(toolCallsRaw) != 1 {
		t.Fatalf("tool_calls 长度 = %d, want 1", len(toolCallsRaw))
	}
	tc := toolCallsRaw[0]
	// 验证是嵌套格式
	fn, ok := tc["function"].(map[string]any)
	if !ok {
		t.Fatal("tool_calls[0] 应包含嵌套的 function 对象")
	}
	if fn["name"] != "search" {
		t.Errorf("function.name = %v, want %q", fn["name"], "search")
	}
	if fn["arguments"] != `{"query":"test"}` {
		t.Errorf("function.arguments = %v, want %q", fn["arguments"], `{"query":"test"}`)
	}
}

// TestAssistantMessage_ToOpenAIDict_OmitsEmpty 验证空字段不输出。
func TestAssistantMessage_ToOpenAIDict_OmitsEmpty(t *testing.T) {
	msg := NewAssistantMessage("hello")
	result := msg.ToOpenAIDict()

	// 空字段不应出现
	for _, key := range []string{"name", "metadata", "tool_calls", "reasoning_content", "parser_content", "logprobs"} {
		if _, ok := result[key]; ok {
			t.Errorf("空字段 %q 不应出现在输出中", key)
		}
	}
	// finish_reason = "null" 应输出（不是空字段）
	if result["finish_reason"] != FinishReasonNull {
		t.Errorf("finish_reason = %v, want %q", result["finish_reason"], FinishReasonNull)
	}
}

// TestAssistantMessage_MarshalJSON 验证扁平格式序列化。
func TestAssistantMessage_MarshalJSON(t *testing.T) {
	msg := NewAssistantMessage("测试",
		WithToolCalls([]*ToolCall{NewToolCall("call_1", "fn", `{}`)}),
		WithFinishReason("stop"),
	)
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	// 验证 tool_calls 是扁平格式（不应有嵌套 function 键）
	tcRaw, ok := result["tool_calls"].([]any)
	if !ok || len(tcRaw) != 1 {
		t.Fatal("tool_calls 应为长度 1 的数组")
	}
	tcMap, ok := tcRaw[0].(map[string]any)
	if !ok {
		t.Fatal("tool_calls[0] 应为 map")
	}
	// 扁平格式：name 直接在顶层
	if _, ok := tcMap["function"]; ok {
		t.Error("MarshalJSON 输出的 tool_calls 不应包含嵌套 function 键")
	}
	if tcMap["name"] != "fn" {
		t.Errorf("tool_calls[0].name = %v, want %q", tcMap["name"], "fn")
	}
}

// TestAssistantMessage_JSONRoundTrip 验证完整序列化/反序列化一致性。
func TestAssistantMessage_JSONRoundTrip(t *testing.T) {
	original := NewAssistantMessage("让我帮你",
		WithToolCalls([]*ToolCall{
			NewToolCall("call_1", "search", `{"q":"test"}`, WithToolCallIndex(0)),
		}),
		WithFinishReason("tool_calls"),
		WithReasoningContent("思考中..."),
	)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored AssistantMessage
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.Role != original.Role {
		t.Errorf("Role: got %v, want %v", restored.Role, original.Role)
	}
	if restored.Content.Text() != original.Content.Text() {
		t.Errorf("Content: got %q, want %q", restored.Content.Text(), original.Content.Text())
	}
	if restored.FinishReason != original.FinishReason {
		t.Errorf("FinishReason: got %q, want %q", restored.FinishReason, original.FinishReason)
	}
	if restored.ReasoningContent != original.ReasoningContent {
		t.Errorf("ReasoningContent: got %q, want %q", restored.ReasoningContent, original.ReasoningContent)
	}
	if len(restored.ToolCalls) != 1 {
		t.Fatalf("ToolCalls 长度: got %d, want 1", len(restored.ToolCalls))
	}
	if restored.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCalls[0].Name: got %q, want %q", restored.ToolCalls[0].Name, "search")
	}
}

// TestAssistantMessage_MultiModalContent 验证多模态内容的序列化。
func TestAssistantMessage_MultiModalContent(t *testing.T) {
	msg := NewAssistantMessage("", WithFinishReason("stop"))
	msg.Content = NewMultiModalContent(
		ContentPart{Type: "text", Text: "描述"},
	)
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored AssistantMessage
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.Content.IsText() {
		t.Error("应为多模态内容")
	}
	if len(restored.Content.Parts()) != 1 {
		t.Errorf("Parts() 长度 = %d, want 1", len(restored.Content.Parts()))
	}
}

// TestAssistantMessage_WithParserContent 验证 WithParserContent 设置 ParserContent。
func TestAssistantMessage_WithParserContent(t *testing.T) {
	msg := NewAssistantMessage("test", WithParserContent(map[string]any{"key": "value"}))
	if msg.ParserContent == nil {
		t.Fatal("ParserContent 不应为 nil")
	}
	pc, ok := msg.ParserContent.(map[string]any)
	if !ok {
		t.Fatalf("ParserContent 类型应为 map[string]any，实际 %T", msg.ParserContent)
	}
	if pc["key"] != "value" {
		t.Errorf("ParserContent[key] = %v, want %q", pc["key"], "value")
	}
}

// TestAssistantMessage_WithPromptTokenIDs 验证 WithPromptTokenIDs 设置 PromptTokenIDs。
func TestAssistantMessage_WithPromptTokenIDs(t *testing.T) {
	ids := []int{1, 2, 3}
	msg := NewAssistantMessage("test", WithPromptTokenIDs(ids))
	if len(msg.PromptTokenIDs) != 3 {
		t.Fatalf("PromptTokenIDs 长度 = %d, want 3", len(msg.PromptTokenIDs))
	}
	for i, id := range msg.PromptTokenIDs {
		if id != ids[i] {
			t.Errorf("PromptTokenIDs[%d] = %d, want %d", i, id, ids[i])
		}
	}
}

// TestAssistantMessage_WithCompletionTokenIDs 验证 WithCompletionTokenIDs 设置 CompletionTokenIDs。
func TestAssistantMessage_WithCompletionTokenIDs(t *testing.T) {
	ids := []int{4, 5, 6, 7}
	msg := NewAssistantMessage("test", WithCompletionTokenIDs(ids))
	if len(msg.CompletionTokenIDs) != 4 {
		t.Fatalf("CompletionTokenIDs 长度 = %d, want 4", len(msg.CompletionTokenIDs))
	}
	for i, id := range msg.CompletionTokenIDs {
		if id != ids[i] {
			t.Errorf("CompletionTokenIDs[%d] = %d, want %d", i, id, ids[i])
		}
	}
}

// TestAssistantMessage_WithLogprobs 验证 WithLogprobs 设置 Logprobs。
func TestAssistantMessage_WithLogprobs(t *testing.T) {
	logprobs := map[string]any{"top_logprobs": []any{"a", "b"}}
	msg := NewAssistantMessage("test", WithLogprobs(logprobs))
	if msg.Logprobs == nil {
		t.Fatal("Logprobs 不应为 nil")
	}
	lp, ok := msg.Logprobs.(map[string]any)
	if !ok {
		t.Fatalf("Logprobs 类型应为 map[string]any，实际 %T", msg.Logprobs)
	}
	if _, exists := lp["top_logprobs"]; !exists {
		t.Error("Logprobs 应包含 top_logprobs 字段")
	}
}
