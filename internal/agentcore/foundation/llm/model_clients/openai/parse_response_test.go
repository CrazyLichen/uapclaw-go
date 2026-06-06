package openai

import (
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
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

// ──────────────────────────── ParseResponse 补充测试 ────────────────────────────

func TestParseResponse_WithOutputParser(t *testing.T) {
	// 测试带 OutputParser 的响应解析
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	content := `{"answer": 42}`
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-parser",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ResponseChoice{
			{
				Index: 0,
				Message: &ResponseMessage{
					Role:    "assistant",
					Content: &content,
				},
				FinishReason: "stop",
			},
		},
	}

	// 使用 mock parser
	result, err := ParseResponse(resp, modelConfig, &mockOutputParser{})
	if err != nil {
		t.Fatalf("ParseResponse 返回错误: %v", err)
	}
	if result.ParserContent == nil {
		t.Error("ParserContent 不应为 nil")
	}
}

func TestParseResponse_WithOutputParserError(t *testing.T) {
	// 测试 OutputParser 解析错误时不中断
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	content := "some text"
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-parser-err",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ResponseChoice{
			{
				Index: 0,
				Message: &ResponseMessage{
					Role:    "assistant",
					Content: &content,
				},
				FinishReason: "stop",
			},
		},
	}

	// 使用返回错误的 parser
	result, err := ParseResponse(resp, modelConfig, &errorOutputParser{})
	if err != nil {
		t.Fatalf("ParseResponse 不应返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if result.ParserContent != nil {
		t.Error("Parser 解析错误时 ParserContent 应为 nil")
	}
}

func TestParseResponse_NilMessage(t *testing.T) {
	// 测试 message 为 nil 的响应
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-nil-msg",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ResponseChoice{
			{
				Index:        0,
				Message:      nil,
				FinishReason: "stop",
			},
		},
	}

	result, err := ParseResponse(resp, modelConfig, nil)
	if err != nil {
		t.Fatalf("ParseResponse 返回错误: %v", err)
	}
	if result.Content.Text() != "" {
		t.Errorf("Content = %q, 期望空字符串", result.Content.Text())
	}
}

func TestParseResponse_NilContent(t *testing.T) {
	// 测试 content 为 nil 的响应（tool_calls 场景）
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-nil-content",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "gpt-4",
		Choices: []ResponseChoice{
			{
				Index: 0,
				Message: &ResponseMessage{
					Role:    "assistant",
					Content: nil,
				},
				FinishReason: "stop",
			},
		},
	}

	result, err := ParseResponse(resp, modelConfig, nil)
	if err != nil {
		t.Fatalf("ParseResponse 返回错误: %v", err)
	}
	if result.Content.Text() != "" {
		t.Errorf("Content = %q, 期望空字符串", result.Content.Text())
	}
}

func TestParseResponse_ToolCallsWithNilIndex(t *testing.T) {
	// 测试 tool_calls 的 Index 为 nil 时使用默认值
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-tc-no-index",
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
							ID:   "call_1",
							Type: "function",
							Function: ResponseFunction{
								Name:      "test_func",
								Arguments: "{}",
							},
							Index: nil, // nil Index
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
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls 数量 = %d, 期望 1", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Index != 0 {
		t.Errorf("Index = %d, 期望 0（nil 时使用默认值）", result.ToolCalls[0].Index)
	}
}

func TestParseResponse_WithLogprobs(t *testing.T) {
	// 测试带 logprobs 的响应
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-logprobs",
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
				Logprobs: map[string]any{
					"content": []any{
						map[string]any{
							"token":  "Hello",
							"logprob": -0.5,
						},
					},
				},
			},
		},
	}

	result, err := ParseResponse(resp, modelConfig, nil)
	if err != nil {
		t.Fatalf("ParseResponse 返回错误: %v", err)
	}
	if result.Logprobs == nil {
		t.Error("Logprobs 不应为 nil")
	}
}

func TestParseResponse_WithPromptTokenIDs(t *testing.T) {
	// 测试带 prompt_token_ids 的响应（vLLM 扩展）
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-tokenids",
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
				TokenIDs:     []int{100, 200, 300},
			},
		},
		PromptTokenIDs: []int{1, 2, 3},
	}

	result, err := ParseResponse(resp, modelConfig, nil)
	if err != nil {
		t.Fatalf("ParseResponse 返回错误: %v", err)
	}
	if len(result.PromptTokenIDs) != 3 {
		t.Errorf("len(PromptTokenIDs) = %d, 期望 3", len(result.PromptTokenIDs))
	}
	if len(result.CompletionTokenIDs) != 3 {
		t.Errorf("len(CompletionTokenIDs) = %d, 期望 3", len(result.CompletionTokenIDs))
	}
}

// mockOutputParser 成功解析的 mock parser
type mockOutputParser struct{}

func (p *mockOutputParser) Parse(input any) (any, error) {
	text, _ := input.(string)
	return map[string]any{"parsed": text}, nil
}

func (p *mockOutputParser) StreamParse(chunks <-chan *llmschema.AssistantMessageChunk) <-chan model_clients.StreamParsedResult {
	out := make(chan model_clients.StreamParsedResult)
	go func() { close(out) }()
	return out
}

// errorOutputParser 总是返回错误的 mock parser
type errorOutputParser struct{}

func (p *errorOutputParser) Parse(_ any) (any, error) {
	return nil, fmt.Errorf("parse error")
}

func (p *errorOutputParser) StreamParse(chunks <-chan *llmschema.AssistantMessageChunk) <-chan model_clients.StreamParsedResult {
	out := make(chan model_clients.StreamParsedResult)
	go func() { close(out) }()
	return out
}
