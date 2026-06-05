package openai

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// strPtr 返回字符串指针的辅助函数
func strPtr(s string) *string {
	return &s
}

// intPtr 返回 int 指针的辅助函数
func intPtr(i int) *int {
	return &i
}

// ──────────────────────────── ParseResponse 测试 ────────────────────────────

func TestParseResponse_NormalResponse(t *testing.T) {
	// 正常响应：content 和 usage
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ResponseChoice{
			{
				Index: 0,
				Message: &ResponseMessage{
					Role:    "assistant",
					Content: strPtr("Hello, world!"),
				},
				FinishReason: "stop",
			},
		},
		Usage: &ResponseUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}

	result, err := ParseResponse(resp, modelConfig, nil)
	if err != nil {
		t.Fatalf("ParseResponse 返回错误: %v", err)
	}

	// 验证 content
	if result.Content.Text() != "Hello, world!" {
		t.Errorf("Content = %q, want %q", result.Content.Text(), "Hello, world!")
	}

	// 验证 finish_reason
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "stop")
	}

	// 验证 usage
	if result.UsageMetadata == nil {
		t.Fatal("UsageMetadata 不应为 nil")
	}
	if result.UsageMetadata.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", result.UsageMetadata.InputTokens)
	}
	if result.UsageMetadata.OutputTokens != 20 {
		t.Errorf("OutputTokens = %d, want 20", result.UsageMetadata.OutputTokens)
	}
	if result.UsageMetadata.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d, want 30", result.UsageMetadata.TotalTokens)
	}
	if result.UsageMetadata.ModelName != "test-model" {
		t.Errorf("ModelName = %q, want %q", result.UsageMetadata.ModelName, "test-model")
	}
}

func TestParseResponse_ToolCalls(t *testing.T) {
	// 带 tool_calls 的响应
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-456",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ResponseChoice{
			{
				Index: 0,
				Message: &ResponseMessage{
					Role:    "assistant",
					Content: strPtr(""),
					ToolCalls: []ResponseToolCall{
						{
							ID:   "call_abc",
							Type: "function",
							Function: ResponseFunction{
								Name:      "get_weather",
								Arguments: `{"city": "Beijing"}`,
							},
							Index: intPtr(0),
						},
						{
							ID:   "call_def",
							Type: "function",
							Function: ResponseFunction{
								Name:      "get_time",
								Arguments: `{"timezone": "UTC"}`,
							},
							Index: intPtr(1),
						},
					},
				},
				FinishReason: "stop",
			},
		},
	}

	result, err := ParseResponse(resp, modelConfig, nil)
	if err != nil {
		t.Fatalf("ParseResponse 返回错误: %v", err)
	}

	// 验证 tool_calls 字段
	if len(result.ToolCalls) != 2 {
		t.Fatalf("len(ToolCalls) = %d, want 2", len(result.ToolCalls))
	}

	// 验证第一个 ToolCall
	tc1 := result.ToolCalls[0]
	if tc1.ID != "call_abc" {
		t.Errorf("ToolCall[0].ID = %q, want %q", tc1.ID, "call_abc")
	}
	if tc1.Type != "function" {
		t.Errorf("ToolCall[0].Type = %q, want %q", tc1.Type, "function")
	}
	if tc1.Name != "get_weather" {
		t.Errorf("ToolCall[0].Name = %q, want %q", tc1.Name, "get_weather")
	}
	if tc1.Arguments != `{"city": "Beijing"}` {
		t.Errorf("ToolCall[0].Arguments = %q, want %q", tc1.Arguments, `{"city": "Beijing"}`)
	}
	if tc1.Index != 0 {
		t.Errorf("ToolCall[0].Index = %d, want 0", tc1.Index)
	}

	// 验证第二个 ToolCall
	tc2 := result.ToolCalls[1]
	if tc2.ID != "call_def" {
		t.Errorf("ToolCall[1].ID = %q, want %q", tc2.ID, "call_def")
	}
	if tc2.Name != "get_time" {
		t.Errorf("ToolCall[1].Name = %q, want %q", tc2.Name, "get_time")
	}
	if tc2.Arguments != `{"timezone": "UTC"}` {
		t.Errorf("ToolCall[1].Arguments = %q, want %q", tc2.Arguments, `{"timezone": "UTC"}`)
	}
	if tc2.Index != 1 {
		t.Errorf("ToolCall[1].Index = %d, want 1", tc2.Index)
	}

	// 有 tool_calls 时 finish_reason 应被修正为 "tool_calls"
	if result.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want %q（有 tool_calls 时应被修正）", result.FinishReason, "tool_calls")
	}
}

func TestParseResponse_ReasoningContent(t *testing.T) {
	// 带 reasoning_content 的响应（思维链，如 DeepSeek-R1）
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-789",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "deepseek-r1",
		Choices: []ResponseChoice{
			{
				Index: 0,
				Message: &ResponseMessage{
					Role:             "assistant",
					Content:          strPtr("The answer is 42."),
					ReasoningContent: strPtr("Let me think about this step by step..."),
				},
				FinishReason: "stop",
			},
		},
	}

	result, err := ParseResponse(resp, modelConfig, nil)
	if err != nil {
		t.Fatalf("ParseResponse 返回错误: %v", err)
	}

	if result.ReasoningContent != "Let me think about this step by step..." {
		t.Errorf("ReasoningContent = %q, want %q", result.ReasoningContent, "Let me think about this step by step...")
	}
}

func TestParseResponse_NoChoices(t *testing.T) {
	// 无 choices 返回错误
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-err",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ResponseChoice{},
	}

	result, err := ParseResponse(resp, modelConfig, nil)
	if err == nil {
		t.Error("无 choices 时应返回错误")
	}
	if result != nil {
		t.Errorf("无 choices 时结果应为 nil, got %v", result)
	}
}

func TestParseResponse_UsageWithCostInfo(t *testing.T) {
	// 带费用信息的 usage
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-cost",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ResponseChoice{
			{
				Index: 0,
				Message: &ResponseMessage{
					Role:    "assistant",
					Content: strPtr("Hello"),
				},
				FinishReason: "stop",
			},
		},
		Usage: &ResponseUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
			PromptTokensDetails: &ResponsePromptTokensDetails{
				CachedTokens: 30,
			},
			Cost:     0.05,
			UsageCost: 0.05,
		},
	}

	result, err := ParseResponse(resp, modelConfig, nil)
	if err != nil {
		t.Fatalf("ParseResponse 返回错误: %v", err)
	}

	if result.UsageMetadata == nil {
		t.Fatal("UsageMetadata 不应为 nil")
	}
	if result.UsageMetadata.CacheTokens != 30 {
		t.Errorf("CacheTokens = %d, want 30", result.UsageMetadata.CacheTokens)
	}
	if result.UsageMetadata.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", result.UsageMetadata.InputTokens)
	}
	if result.UsageMetadata.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", result.UsageMetadata.OutputTokens)
	}
}
