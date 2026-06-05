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
