package responses

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"testing"
)

// TestParseResponse 完整 payload → AssistantMessage（content/reasoning/tool_calls/usage）。
// Python: responses_utils.py:190 parse_response
func TestParseResponse(t *testing.T) {
	payload := map[string]any{
		"id": "resp_1", "model": "gpt-5", "status": "completed",
		"output": []any{
			map[string]any{"type": "reasoning", "summary": []any{"thinking..."}},
			map[string]any{"type": "message", "content": []any{
				map[string]any{"type": "output_text", "text": "hello"},
			}},
			map[string]any{"type": "function_call", "id": "item_1", "call_id": "call_1",
				"name": "f1", "arguments": `{"a":1}`},
		},
		"usage": map[string]any{
			"input_tokens": 10, "output_tokens": 25,
			"input_tokens_details":  map[string]any{"cached_tokens": 6},
			"output_tokens_details": map[string]any{"reasoning_tokens": 12},
		},
	}
	msg, err := ParseResponse(payload, "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content.String() != "hello" || msg.ReasoningContent != "thinking..." {
		t.Fatalf("content/reasoning: %q/%q", msg.Content.String(), msg.ReasoningContent)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ResponseItemID != "item_1" ||
		msg.ToolCalls[0].ID != "call_1" || msg.ToolCalls[0].Arguments != `{"a":1}` {
		t.Fatalf("tool_calls: %+v", msg.ToolCalls)
	}
	if msg.FinishReason != "tool_calls" || msg.ResponseID != "resp_1" || msg.ResponseModel != "gpt-5" {
		t.Fatalf("finish/response_id/response_model: %q/%q/%q", msg.FinishReason, msg.ResponseID, msg.ResponseModel)
	}
	u := msg.UsageMetadata
	if u == nil || u.InputTokens != 10 || u.OutputTokens != 25 || u.TotalTokens != 35 {
		t.Fatalf("usage: %+v", u)
	}
	if u.ReasoningTokens == nil || *u.ReasoningTokens != 12 {
		t.Fatalf("reasoning_tokens: %v", u.ReasoningTokens)
	}
	if u.CacheReadTokens == nil || *u.CacheReadTokens != 6 || !u.CacheAuthoritative {
		t.Fatalf("cache 归一: %+v", u)
	}
	if u.CacheStatus != "observed" || u.CacheSource != "provider_usage" {
		t.Fatalf("cache 三态: %q/%q", u.CacheStatus, u.CacheSource)
	}
	if u.CacheTokens != 6 {
		t.Fatalf("legacy cache_tokens: %d", u.CacheTokens)
	}
	if len(msg.ProviderMetadata) != 0 {
		t.Fatalf("completed 无 provider_metadata 非敏感字段时不该有内容: %v", msg.ProviderMetadata)
	}
}

// TestParseResponse_Incomplete status=incomplete → finish_reason=length。
func TestParseResponse_Incomplete(t *testing.T) {
	payload := map[string]any{
		"id": "resp_2", "model": "m", "status": "incomplete",
		"output":             []any{},
		"incomplete_details": map[string]any{"reason": "max_output_tokens"},
		"output_text":        "partial",
	}
	msg, err := ParseResponse(payload, "m")
	if err != nil {
		t.Fatal(err)
	}
	if msg.FinishReason != "length" {
		t.Fatalf("finish: %q", msg.FinishReason)
	}
	// output_text 兜底
	if msg.Content.String() != "partial" {
		t.Fatalf("output_text 兜底: %q", msg.Content.String())
	}
	// provider_metadata 保留 incomplete_details
	if _, ok := msg.ProviderMetadata["incomplete_details"]; !ok {
		t.Fatalf("incomplete_details: %v", msg.ProviderMetadata)
	}
}

// TestParseResponse_无Usage 无 usage → UsageMetadata nil。
func TestParseResponse_无Usage(t *testing.T) {
	msg, err := ParseResponse(map[string]any{"id": "r", "output": []any{}}, "m")
	if err != nil {
		t.Fatal(err)
	}
	if msg.UsageMetadata != nil {
		t.Fatalf("usage 应为 nil: %+v", msg.UsageMetadata)
	}
}

// TestParseResponse_兼容PromptTokens Python/兼容双命名。
func TestParseResponse_兼容PromptTokens(t *testing.T) {
	msg, err := ParseResponse(map[string]any{
		"output": []any{},
		"usage": map[string]any{
			"prompt_tokens": 3, "completion_tokens": 7,
			"prompt_tokens_details":     map[string]any{"cached_tokens": 2, "cache_write_tokens": 4},
			"completion_tokens_details": map[string]any{"reasoning_tokens": 5},
		},
	}, "m")
	if err != nil {
		t.Fatal(err)
	}
	u := msg.UsageMetadata
	if u.InputTokens != 3 || u.OutputTokens != 7 || u.TotalTokens != 10 {
		t.Fatalf("双命名 tokens: %+v", u)
	}
	if u.CacheCreationTokens == nil || *u.CacheCreationTokens != 4 {
		t.Fatalf("cache_creation_tokens: %v", u.CacheCreationTokens)
	}
	if u.ReasoningTokens == nil || *u.ReasoningTokens != 5 {
		t.Fatalf("reasoning_tokens: %v", u.ReasoningTokens)
	}
}

// TestMessageFromStreamChunk nil→空 stop；finish 归一。
// Python: responses_utils.py:403 message_from_stream_chunk
func TestMessageFromStreamChunk(t *testing.T) {
	if msg := MessageFromStreamChunk(nil); msg.FinishReason != "stop" || msg.Content.String() != "" {
		t.Fatalf("nil chunk: %+v", msg)
	}
	chunk := newStreamChunkWithReason("null")
	msg := MessageFromStreamChunk(chunk)
	if msg.FinishReason != "stop" {
		t.Fatalf("null→stop: %q", msg.FinishReason)
	}
	withCalls := newStreamChunkWithReason("null")
	withCalls.ToolCalls = toolCallsForChunkTest()
	msg2 := MessageFromStreamChunk(withCalls)
	if msg2.FinishReason != "tool_calls" {
		t.Fatalf("tool_calls 优先: %q", msg2.FinishReason)
	}
}

// newStreamChunkWithReason 测试辅助：指定 finish_reason 的 chunk。
func newStreamChunkWithReason(reason string) *llmschema.AssistantMessageChunk {
	return llmschema.NewAssistantMessageChunk("text", llmschema.WithChunkFinishReason(reason))
}

// toolCallsForChunkTest 测试辅助：单条工具调用。
func toolCallsForChunkTest() []*llmschema.ToolCall {
	return []*llmschema.ToolCall{llmschema.NewToolCall("c1", "f", "{}")}
}
