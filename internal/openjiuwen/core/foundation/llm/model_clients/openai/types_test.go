package openai

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── ChatCompletionResponse 反序列化测试 ────────────────────────────

func TestChatCompletionResponse_Deserialization(t *testing.T) {
	// 完整响应：choices、message、content、tool_calls、usage
	jsonStr := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello, world!",
					"tool_calls": [
						{
							"id": "call_abc",
							"type": "function",
							"function": {
								"name": "get_weather",
								"arguments": "{\"city\": \"Beijing\"}"
							},
							"index": 0
						}
					]
				},
				"finish_reason": "tool_calls"
			}
		],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 20,
			"total_tokens": 30,
			"prompt_tokens_details": {
				"cached_tokens": 5
			}
		}
	}`

	var resp ChatCompletionResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	// 验证基本字段
	if resp.ID != "chatcmpl-123" {
		t.Errorf("ID = %q, want %q", resp.ID, "chatcmpl-123")
	}
	if resp.Object != "chat.completion" {
		t.Errorf("Object = %q, want %q", resp.Object, "chat.completion")
	}
	if resp.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", resp.Model, "gpt-4")
	}

	// 验证 choices
	if len(resp.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if choice.Index != 0 {
		t.Errorf("Choice.Index = %d, want 0", choice.Index)
	}
	if choice.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want %q", choice.FinishReason, "tool_calls")
	}

	// 验证 message
	if choice.Message == nil {
		t.Fatal("Message 不应为 nil")
	}
	if choice.Message.Role != "assistant" {
		t.Errorf("Role = %q, want %q", choice.Message.Role, "assistant")
	}
	if choice.Message.Content == nil || *choice.Message.Content != "Hello, world!" {
		t.Errorf("Content = %v, want %q", choice.Message.Content, "Hello, world!")
	}

	// 验证 tool_calls
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(choice.Message.ToolCalls))
	}
	tc := choice.Message.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_abc")
	}
	if tc.Type != "function" {
		t.Errorf("ToolCall.Type = %q, want %q", tc.Type, "function")
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("Function.Name = %q, want %q", tc.Function.Name, "get_weather")
	}
	if tc.Function.Arguments != `{"city": "Beijing"}` {
		t.Errorf("Function.Arguments = %q, want %q", tc.Function.Arguments, `{"city": "Beijing"}`)
	}
	if tc.Index == nil || *tc.Index != 0 {
		t.Errorf("ToolCall.Index = %v, want 0", tc.Index)
	}

	// 验证 usage
	if resp.Usage == nil {
		t.Fatal("Usage 不应为 nil")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 20 {
		t.Errorf("CompletionTokens = %d, want 20", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d, want 30", resp.Usage.TotalTokens)
	}
	if resp.Usage.PromptTokensDetails == nil || resp.Usage.PromptTokensDetails.CachedTokens != 5 {
		t.Errorf("CachedTokens 不等于 5")
	}
}

// ──────────────────────────── ChatCompletionChunkResponse 反序列化测试 ────────────────────────────

func TestChatCompletionChunkResponse_DeltaContent(t *testing.T) {
	// 带 delta content 的流式块
	jsonStr := `{
		"id": "chatcmpl-123",
		"object": "chat.completion.chunk",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [
			{
				"index": 0,
				"delta": {
					"content": "Hello"
				},
				"finish_reason": null
			}
		]
	}`

	var chunk ChatCompletionChunkResponse
	if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if len(chunk.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(chunk.Choices))
	}
	delta := chunk.Choices[0].Delta
	if delta == nil {
		t.Fatal("Delta 不应为 nil")
	}
	if delta.Content == nil || *delta.Content != "Hello" {
		t.Errorf("Content = %v, want %q", delta.Content, "Hello")
	}
}

func TestChatCompletionChunkResponse_DeltaToolCalls(t *testing.T) {
	// 带 delta tool_calls 的流式块
	jsonStr := `{
		"id": "chatcmpl-123",
		"object": "chat.completion.chunk",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [
			{
				"index": 0,
				"delta": {
					"tool_calls": [
						{
							"id": "call_abc",
							"type": "function",
							"function": {
								"name": "get_weather",
								"arguments": "{\"ci"
							},
							"index": 0
						}
					]
				},
				"finish_reason": null
			}
		]
	}`

	var chunk ChatCompletionChunkResponse
	if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	delta := chunk.Choices[0].Delta
	if len(delta.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(delta.ToolCalls))
	}
	tc := delta.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("ID = %q, want %q", tc.ID, "call_abc")
	}
	if tc.Type != "function" {
		t.Errorf("Type = %q, want %q", tc.Type, "function")
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("Name = %q, want %q", tc.Function.Name, "get_weather")
	}
	if tc.Function.Arguments != "{\"ci" {
		t.Errorf("Arguments = %q, want %q", tc.Function.Arguments, "{\"ci")
	}
	if tc.Index == nil || *tc.Index != 0 {
		t.Errorf("Index = %v, want 0", tc.Index)
	}
}

func TestChatCompletionChunkResponse_UsageOnly(t *testing.T) {
	// 仅携带 usage 的流式块（最后一个块）
	jsonStr := `{
		"id": "chatcmpl-123",
		"object": "chat.completion.chunk",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 20,
			"total_tokens": 30
		}
	}`

	var chunk ChatCompletionChunkResponse
	if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if chunk.Usage == nil {
		t.Fatal("Usage 不应为 nil")
	}
	if chunk.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", chunk.Usage.PromptTokens)
	}
	if len(chunk.Choices) != 0 {
		t.Errorf("len(Choices) = %d, want 0", len(chunk.Choices))
	}
}

// ──────────────────────────── ErrorResponse 反序列化测试 ────────────────────────────

func TestErrorResponse_Deserialization(t *testing.T) {
	jsonStr := `{
		"error": {
			"message": "Incorrect API key provided",
			"type": "invalid_request_error",
			"code": "invalid_api_key"
		}
	}`

	var errResp ErrorResponse
	if err := json.Unmarshal([]byte(jsonStr), &errResp); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if errResp.Error.Message != "Incorrect API key provided" {
		t.Errorf("Message = %q, want %q", errResp.Error.Message, "Incorrect API key provided")
	}
	if errResp.Error.Type != "invalid_request_error" {
		t.Errorf("Type = %q, want %q", errResp.Error.Type, "invalid_request_error")
	}
	if errResp.Error.Code != "invalid_api_key" {
		t.Errorf("Code = %q, want %q", errResp.Error.Code, "invalid_api_key")
	}
}
