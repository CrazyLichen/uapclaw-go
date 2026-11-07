package openai

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── ParseStreamChunk 测试 ────────────────────────────

func TestParseStreamChunk_DeltaContent(t *testing.T) {
	// 正常块：delta content
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	chunk := &ChatCompletionChunkResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion.chunk",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ChunkChoice{
			{
				Index: 0,
				Delta: &ChunkDelta{
					Content: strPtr("Hello"),
				},
				FinishReason: nil,
			},
		},
	}

	result := ParseStreamChunk(chunk, modelConfig)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if result.Content.Text() != "Hello" {
		t.Errorf("Content = %q, want %q", result.Content.Text(), "Hello")
	}
	if result.FinishReason != llmschema.FinishReasonNull {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, llmschema.FinishReasonNull)
	}
}

func TestParseStreamChunk_UsageOnly(t *testing.T) {
	// 仅 usage 块（无 choices，有 usage）返回非 nil
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	chunk := &ChatCompletionChunkResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion.chunk",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ChunkChoice{},
		Usage: &ResponseUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}

	result := ParseStreamChunk(chunk, modelConfig)
	if result == nil {
		t.Fatal("仅 usage 块应返回非 nil")
	}
	if result.UsageMetadata == nil {
		t.Fatal("UsageMetadata 不应为 nil")
	}
	if result.UsageMetadata.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", result.UsageMetadata.InputTokens)
	}
}

func TestParseStreamChunk_EmptyChunk(t *testing.T) {
	// 空块（无 choices，无 usage）返回 nil
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	chunk := &ChatCompletionChunkResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion.chunk",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ChunkChoice{},
		Usage:   nil,
	}

	result := ParseStreamChunk(chunk, modelConfig)
	if result != nil {
		t.Errorf("空块应返回 nil, got %v", result)
	}
}

func TestParseStreamChunk_ToolCallsDelta(t *testing.T) {
	// 带 tool_calls delta 的块
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	chunk := &ChatCompletionChunkResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion.chunk",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ChunkChoice{
			{
				Index: 0,
				Delta: &ChunkDelta{
					ToolCalls: []ChunkToolCall{
						{
							ID:   "call_abc",
							Type: "function",
							Function: ChunkFunction{
								Name:      "get_weather",
								Arguments: "{\"ci",
							},
							Index: intPtr(0),
						},
					},
				},
				FinishReason: nil,
			},
		},
	}

	result := ParseStreamChunk(chunk, modelConfig)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("ID = %q, want %q", tc.ID, "call_abc")
	}
	if tc.Type != "function" {
		t.Errorf("Type = %q, want %q", tc.Type, "function")
	}
	if tc.Name != "get_weather" {
		t.Errorf("Name = %q, want %q", tc.Name, "get_weather")
	}
	if tc.Arguments != "{\"ci" {
		t.Errorf("Arguments = %q, want %q", tc.Arguments, "{\"ci")
	}
	if tc.Index != 0 {
		t.Errorf("Index = %d, want 0", tc.Index)
	}
}

// TestParseStreamChunk_缺省type的增量帧可聚合 验证真实 SSE 缺省 type 的增量帧仍能聚合。
// 真实 OpenAI 兼容 SSE 只在首个 delta 携带 type/id/name，后续帧只有 arguments
// （types.go:149 的 omitempty 决定缺省时不出现该字段）。Python 在流式解析里把
// type 硬编码为 "function"（openai_model_client.py:745-751），Go 若把空串透传，
// mergeToolCalls 的同调用判定（要求两侧 type == "function"）失效，一个调用的
// JSON 碎片会被拆成多个独立 ToolCall，最终逐个被当成工具执行。
func TestParseStreamChunk_缺省type的增量帧可聚合(t *testing.T) {
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))

	frames := []ChunkToolCall{
		{ID: "call_00_x", Type: "function", Function: ChunkFunction{Name: "build_team", Arguments: `{"name":`}, Index: intPtr(0)},
		{Function: ChunkFunction{Arguments: `"Team"}`}, Index: intPtr(0)},
	}

	var accumulated *llmschema.AssistantMessageChunk
	for i, frame := range frames {
		chunk := &ChatCompletionChunkResponse{
			Choices: []ChunkChoice{{Delta: &ChunkDelta{ToolCalls: []ChunkToolCall{frame}}}},
		}
		parsed := ParseStreamChunk(chunk, modelConfig)
		if parsed == nil {
			t.Fatalf("第 %d 帧解析结果不应为 nil", i)
		}
		if len(parsed.ToolCalls) != 1 {
			t.Fatalf("第 %d 帧 ToolCalls 数量 = %d，want 1", i, len(parsed.ToolCalls))
		}
		if got := parsed.ToolCalls[0].Type; got != "function" {
			t.Fatalf("第 %d 帧 Type = %q，want %q（缺省 type 必须补齐为 function）", i, got, "function")
		}
		if accumulated == nil {
			accumulated = parsed
		} else {
			accumulated = accumulated.Merge(parsed)
		}
	}

	if len(accumulated.ToolCalls) != 1 {
		t.Fatalf("增量帧应聚合为 1 个调用，实际 %d 个：%+v", len(accumulated.ToolCalls), accumulated.ToolCalls)
	}
	tc := accumulated.ToolCalls[0]
	if tc.Name != "build_team" {
		t.Errorf("Name = %q，want %q", tc.Name, "build_team")
	}
	if tc.Arguments != `{"name":"Team"}` {
		t.Errorf("Arguments = %q，want %q", tc.Arguments, `{"name":"Team"}`)
	}
	if tc.ID != "call_00_x" {
		t.Errorf("ID = %q，want %q", tc.ID, "call_00_x")
	}
}

func TestParseStreamChunk_ReasoningContent(t *testing.T) {
	// 带 reasoning_content 的块
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	chunk := &ChatCompletionChunkResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion.chunk",
		Created: 1677652288,
		Model:   "deepseek-r1",
		Choices: []ChunkChoice{
			{
				Index: 0,
				Delta: &ChunkDelta{
					ReasoningContent: strPtr("Let me think..."),
				},
				FinishReason: nil,
			},
		},
	}

	result := ParseStreamChunk(chunk, modelConfig)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if result.ReasoningContent != "Let me think..." {
		t.Errorf("ReasoningContent = %q, want %q", result.ReasoningContent, "Let me think...")
	}
}

func TestParseStreamChunk_FinishReason(t *testing.T) {
	// 带 finish_reason 的块
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	chunk := &ChatCompletionChunkResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion.chunk",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ChunkChoice{
			{
				Index: 0,
				Delta: &ChunkDelta{
					Content: strPtr(""),
				},
				FinishReason: strPtr("stop"),
			},
		},
	}

	result := ParseStreamChunk(chunk, modelConfig)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "stop")
	}
}

func TestParseStreamChunk_NilDelta(t *testing.T) {
	// delta 为 nil 的块（不应崩溃）
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	chunk := &ChatCompletionChunkResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion.chunk",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ChunkChoice{
			{
				Index:        0,
				Delta:        nil,
				FinishReason: strPtr("stop"),
			},
		},
	}

	result := ParseStreamChunk(chunk, modelConfig)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if result.Content.Text() != "" {
		t.Errorf("Content = %q, want empty", result.Content.Text())
	}
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "stop")
	}
}

// TestParseStreamChunk_Reasoning四种字段形态 四级回退在各字段形态下生效。
// Python: openai_model_client.py:2751-2763（_parse_stream_chunk 内 L2979 调用）
func TestParseStreamChunk_Reasoning四种字段形态(t *testing.T) {
	mk := func(delta *ChunkDelta) *ChatCompletionChunkResponse {
		return &ChatCompletionChunkResponse{Choices: []ChunkChoice{{Delta: delta}}}
	}
	cfg := llmschema.NewModelRequestConfig(llmschema.WithModelName("m"))

	// reasoning_content
	if got := ParseStreamChunk(mk(&ChunkDelta{ReasoningContent: ptrOf("a")}), cfg); got.ReasoningContent != "a" {
		t.Errorf("reasoning_content: %q", got.ReasoningContent)
	}
	// reasoning（OpenRouter）
	if got := ParseStreamChunk(mk(&ChunkDelta{Reasoning: ptrOf("b")}), cfg); got.ReasoningContent != "b" {
		t.Errorf("reasoning: %q", got.ReasoningContent)
	}
	// reasoning_token_text
	if got := ParseStreamChunk(mk(&ChunkDelta{ReasoningTokenText: ptrOf("c")}), cfg); got.ReasoningContent != "c" {
		t.Errorf("token_text: %q", got.ReasoningContent)
	}
	// reasoning_details[0].text
	if got := ParseStreamChunk(mk(&ChunkDelta{
		ReasoningDetails: []map[string]any{{"text": "d"}}}), cfg); got.ReasoningContent != "d" {
		t.Errorf("details: %q", got.ReasoningContent)
	}
}

func ptrOf(v string) *string { return &v }
