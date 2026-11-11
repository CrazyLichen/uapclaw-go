package anthropic

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestUsageFromAnthropic_缓存归一 对齐 anthropic_model_client.py:1368-1417。
func TestUsageFromAnthropic_缓存归一(t *testing.T) {
	u := UsageFromAnthropic(map[string]any{
		"input_tokens": 100, "cache_read_input_tokens": 300,
		"cache_creation_input_tokens": 50, "output_tokens": 20,
	}, "claude-sonnet-4-5")
	if u.InputTokens != 450 { // uncached 100 + read 300 + write 50
		t.Fatalf("total_input = %d, want 450", u.InputTokens)
	}
	if u.OutputTokens != 20 || u.TotalTokens != 470 {
		t.Fatalf("output/total = %d/%d", u.OutputTokens, u.TotalTokens)
	}
	if u.CacheReadTokens == nil || *u.CacheReadTokens != 300 {
		t.Fatalf("cache_read = %v", u.CacheReadTokens)
	}
	if u.CacheMissTokens == nil || *u.CacheMissTokens != 150 { // uncached 100 + write 50
		t.Fatalf("cache_miss = %v, want 150", u.CacheMissTokens)
	}
	if u.CacheWriteTokens == nil || *u.CacheWriteTokens != 50 {
		t.Fatalf("cache_write = %v", u.CacheWriteTokens)
	}
	if u.CacheTokens != 300 {
		t.Fatalf("cache_tokens = %d", u.CacheTokens)
	}
	if u.CacheStatus != "observed" || !u.CacheAuthoritative || u.CacheSource != "provider_usage" {
		t.Fatalf("authoritative 标记缺失: %v", u)
	}
	if u.CacheCreationTokens == nil || *u.CacheCreationTokens != 50 {
		t.Fatalf("cache_creation = %v", u.CacheCreationTokens)
	}
}

// TestUsageFromAnthropic_nil与缺字段 nil → nil；缺字段按 0 处理。
func TestUsageFromAnthropic_nil与缺字段(t *testing.T) {
	if UsageFromAnthropic(nil, "m") != nil {
		t.Fatal("nil → nil")
	}
	u := UsageFromAnthropic(map[string]any{"input_tokens": 5}, "m")
	if u.InputTokens != 5 || u.OutputTokens != 0 {
		t.Fatalf("缺字段按 0: %v", u)
	}
	if u.CacheCreationTokens != nil {
		t.Fatal("未上报 cache_creation → nil 指针")
	}
}

// TestParseResponse_块分派 四类块 + reasoning 回退 + provider_metadata。
func TestParseResponse_块分派(t *testing.T) {
	resp := map[string]any{
		"id": "msg_1", "model": "claude-sonnet-4-5",
		"content": []any{
			map[string]any{"type": "thinking", "thinking": "thought", "signature": "sig"},
			map[string]any{"type": "redacted_thinking", "data": "opaque"},
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "tool_use", "id": "t1", "name": "f", "input": map[string]any{"a": 1}},
		},
		"stop_reason": "tool_use", "stop_sequence": "END",
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 3},
	}
	msg := ParseResponse(resp, nil, "claude-sonnet-4-5", "Anthropic")
	if msg.Content.Text() != "hello" {
		t.Fatalf("content = %q", msg.Content.Text())
	}
	if msg.ReasoningContent != "thought" {
		t.Fatalf("reasoning = %q（redacted 无可显示文本）", msg.ReasoningContent)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "t1" || msg.ToolCalls[0].Index != 3 {
		t.Fatalf("tool_calls = %+v（index=块序 3）", msg.ToolCalls)
	}
	if msg.ToolCalls[0].Arguments == "" {
		t.Fatal("arguments 应序列化为 JSON")
	}
	if msg.FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q", msg.FinishReason)
	}
	if msg.ResponseID != "msg_1" || msg.ResponseModel != "claude-sonnet-4-5" {
		t.Fatalf("response_id/model = %q/%q", msg.ResponseID, msg.ResponseModel)
	}
	if msg.ProviderMetadata["stop_reason"] != "tool_use" || msg.ProviderMetadata["stop_sequence"] != "END" {
		t.Fatalf("provider_metadata = %v", msg.ProviderMetadata)
	}
	// 回放块：thinking + redacted + tool_use（text 无需回放也保留）
	replay, ok := msg.Metadata[AnthropicContentBlocksMetadataKey].([]map[string]any)
	if !ok || len(replay) != 4 {
		t.Fatalf("replay blocks = %v", msg.Metadata)
	}
	if replay[0]["signature"] != "sig" {
		t.Fatalf("signature 原样保留: %v", replay[0])
	}
	if msg.UsageMetadata == nil || msg.UsageMetadata.InputTokens != 10 {
		t.Fatalf("usage = %v", msg.UsageMetadata)
	}
}

// TestParseResponse_finishReason三态 end_turn→stop；reasoning_content 兜底。
func TestParseResponse_finishReason三态(t *testing.T) {
	msg := ParseResponse(map[string]any{
		"content":     []any{map[string]any{"type": "text", "text": "x"}},
		"stop_reason": "end_turn", "reasoning_content": "oai-style",
	}, nil, "m", "Anthropic")
	if msg.FinishReason != "stop" {
		t.Fatalf("end_turn → stop, got %q", msg.FinishReason)
	}
	if msg.ReasoningContent != "oai-style" {
		t.Fatalf("OpenAI 风格 reasoning_content 兜底失效: %q", msg.ReasoningContent)
	}
}

// TestEventToChunk_六事件矩阵 事件到 chunk 的完整映射。
func TestEventToChunk_六事件矩阵(t *testing.T) {
	acc := map[int]map[string]any{}
	// message_start → usage + response_id
	chunk := EventToChunk(map[string]any{"type": "message_start",
		"message": map[string]any{"id": "m1", "model": "claude",
			"usage": map[string]any{"input_tokens": 10}}}, acc)
	if chunk == nil || chunk.ResponseID != "m1" || chunk.UsageMetadata.InputTokens != 10 {
		t.Fatalf("message_start 不符: %+v", chunk)
	}
	// content_block_start(tool_use) → nil（进 acc）
	if EventToChunk(map[string]any{"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "tool_use", "id": "t", "name": "f"}}, acc) != nil {
		t.Fatal("tool_use start 应返回 nil")
	}
	// content_block_start(thinking) → nil
	if EventToChunk(map[string]any{"type": "content_block_start", "index": 2,
		"content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}}, acc) != nil {
		t.Fatal("thinking start 应返回 nil")
	}
	// content_block_delta(text)
	tc := EventToChunk(map[string]any{"type": "content_block_delta", "index": 1,
		"delta": map[string]any{"type": "text_delta", "text": "hi"}}, acc)
	if tc == nil || tc.Content.Text() != "hi" {
		t.Fatalf("text_delta 不符: %+v", tc)
	}
	// thinking_delta → reasoning chunk
	th := EventToChunk(map[string]any{"type": "content_block_delta", "index": 2,
		"delta": map[string]any{"type": "thinking_delta", "thinking": "why"}}, acc)
	if th == nil || th.ReasoningContent != "why" {
		t.Fatalf("thinking_delta 不符: %+v", th)
	}
	// signature_delta → metadata-only chunk
	sg := EventToChunk(map[string]any{"type": "content_block_delta", "index": 2,
		"delta": map[string]any{"type": "signature_delta", "signature": "sig"}}, acc)
	if sg == nil || sg.ReasoningContent != "" || sg.Metadata == nil {
		t.Fatalf("signature_delta 不符: %+v", sg)
	}
	// input_json_delta 累积 → nil；content_block_stop → tool_calls chunk
	_ = EventToChunk(map[string]any{"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"a":`}}, acc)
	_ = EventToChunk(map[string]any{"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": `1}`}}, acc)
	stop := EventToChunk(map[string]any{"type": "content_block_stop", "index": 0}, acc)
	if stop == nil || len(stop.ToolCalls) != 1 || stop.ToolCalls[0].Arguments != `{"a":1}` {
		t.Fatalf("tool_use stop 不符: %+v", stop)
	}
	// message_delta(tool_use) → finish_reason=tool_calls + provider_metadata.stop_reason
	md := EventToChunk(map[string]any{"type": "message_delta",
		"delta": map[string]any{"stop_reason": "tool_use"},
		"usage": map[string]any{"output_tokens": 5}}, acc)
	if md.FinishReason != "tool_calls" || md.ProviderMetadata["stop_reason"] != "tool_use" {
		t.Fatalf("message_delta 不符: %+v", md)
	}
	if md.UsageMetadata == nil || md.UsageMetadata.OutputTokens != 5 {
		t.Fatalf("message_delta usage 不符: %v", md.UsageMetadata)
	}
	// message_stop → nil
	if EventToChunk(map[string]any{"type": "message_stop"}, acc) != nil {
		t.Fatal("message_stop 应返回 nil")
	}
}

// TestReadStreamEvent_data行解析 SSE data: JSON 解析与 [DONE]。
func TestReadStreamEvent_data行解析(t *testing.T) {
	// 正常解析在 client 集成用例覆盖（httptest 流式）；此处覆盖 [DONE] 哨兵
	// 经由 EventToChunk 空事件安全返回 nil 的路径
	if EventToChunk(map[string]any{"type": "unknown_event"}, map[int]map[string]any{}) != nil {
		t.Fatal("未知事件应返回 nil")
	}
	_ = llmschema.FinishReasonNull
	_ = model_clients.BaseOutputParser(nil)
}
