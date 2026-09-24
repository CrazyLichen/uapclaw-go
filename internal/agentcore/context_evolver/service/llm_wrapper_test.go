package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockLLMClient 模拟 BaseModelClient，记录 Invoke 调用参数。
// 仅 Invoke 有实际行为，其他方法返回不支持错误。
type mockLLMClient struct {
	// invokeFn 自定义 Invoke 行为，nil 时返回默认成功响应
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
	// invokeCalls Invoke 调用次数
	invokeCalls int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// Invoke 实现 BaseModelClient.Invoke，记录调用并执行 invokeFn。
func (m *mockLLMClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	m.invokeCalls++
	if m.invokeFn != nil {
		return m.invokeFn(ctx, messages, opts...)
	}
	return llmschema.NewAssistantMessage("mock response"), nil
}

// Stream 实现 BaseModelClient.Stream，返回不支持。
func (m *mockLLMClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// GenerateImage 实现 BaseModelClient.GenerateImage，返回不支持。
func (m *mockLLMClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// GenerateSpeech 实现 BaseModelClient.GenerateSpeech，返回不支持。
func (m *mockLLMClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// GenerateVideo 实现 BaseModelClient.GenerateVideo，返回不支持。
func (m *mockLLMClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// TranscribeAudio 实现 BaseModelClient.TranscribeAudio，返回不支持。
func (m *mockLLMClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// Release 实现 BaseModelClient.Release，返回不支持。
func (m *mockLLMClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, fmt.Errorf("not supported in mock")
}

// SupportsKVCacheRelease 实现 BaseModelClient.SupportsKVCacheRelease。
func (m *mockLLMClient) SupportsKVCacheRelease() bool {
	return false
}

// TestNewOpenAILLMWrapper_构造成功 验证正常构造。
func TestNewOpenAILLMWrapper_构造成功(t *testing.T) {
	wrapper, err := NewOpenAILLMWrapper("gpt-5.2", "test-api-key", "https://api.openai.com/v1", 0.7, 2000)
	require.NoError(t, err)
	assert.NotNil(t, wrapper)
	assert.Equal(t, "gpt-5.2", wrapper.modelName)
	assert.Equal(t, 0.7, wrapper.temperature)
	assert.Equal(t, 2000, wrapper.maxTokens)
	assert.True(t, wrapper.isNewerModel) // gpt-5 属于 newer model
}

// TestNewOpenAILLMWrapper_较旧模型 验证非 newer model 标记。
func TestNewOpenAILLMWrapper_较旧模型(t *testing.T) {
	wrapper, err := NewOpenAILLMWrapper("gpt-3.5-turbo", "test-api-key", "https://api.openai.com/v1", 0.7, 2000)
	require.NoError(t, err)
	assert.False(t, wrapper.isNewerModel) // gpt-3.5 不属于 newer model
}

// TestNewOpenAILLMWrapper_o1模型 验证 o1 系列为 newer model。
func TestNewOpenAILLMWrapper_o1模型(t *testing.T) {
	wrapper, err := NewOpenAILLMWrapper("o1-preview", "test-api-key", "https://api.openai.com/v1", 0.7, 2000)
	require.NoError(t, err)
	assert.True(t, wrapper.isNewerModel)
}

// TestNewOpenAILLMWrapper_o3模型 验证 o3 系列为 newer model。
func TestNewOpenAILLMWrapper_o3模型(t *testing.T) {
	wrapper, err := NewOpenAILLMWrapper("o3-mini", "test-api-key", "https://api.openai.com/v1", 0.7, 2000)
	require.NoError(t, err)
	assert.True(t, wrapper.isNewerModel)
}

// TestNewOpenAILLMWrapper_gpt4模型 验证 gpt-4 系列为 newer model。
func TestNewOpenAILLMWrapper_gpt4模型(t *testing.T) {
	wrapper, err := NewOpenAILLMWrapper("gpt-4o", "test-api-key", "https://api.openai.com/v1", 0.7, 2000)
	require.NoError(t, err)
	assert.True(t, wrapper.isNewerModel)
}

// TestNewOpenAILLMWrapper_APIKey缺失 验证 API key 为空时返回错误。
func TestNewOpenAILLMWrapper_APIKey缺失(t *testing.T) {
	_, err := NewOpenAILLMWrapper("gpt-5.2", "", "https://api.openai.com/v1", 0.7, 2000)
	require.Error(t, err)
	// 验证是 exception 类型
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestNewOpenAILLMWrapper_默认BaseURL 验证 baseURL 为空时使用默认值。
func TestNewOpenAILLMWrapper_默认BaseURL(t *testing.T) {
	wrapper, err := NewOpenAILLMWrapper("gpt-5.2", "test-api-key", "", 0.7, 2000)
	require.NoError(t, err)
	assert.Equal(t, "https://api.openai.com/v1", wrapper.clientConfig.APIBase)
}

// newWrapperWithMock 创建带 mock client 的 OpenAILLMWrapper，用于 Generate 等方法测试。
func newWrapperWithMock(modelName string, isNewer bool, mockFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)) (*OpenAILLMWrapper, *mockLLMClient) {
	mock := &mockLLMClient{invokeFn: mockFn}
	return &OpenAILLMWrapper{
		modelName:    modelName,
		temperature:  0.7,
		maxTokens:    2000,
		isNewerModel: isNewer,
		client:       mock,
	}, mock
}

// TestOpenAILLMWrapper_Generate_基本调用 验证 Generate 正确构建 messages 并调用 client。
func TestOpenAILLMWrapper_Generate_基本调用(t *testing.T) {
	var lastMessages model_clients.MessagesParam
	wrapper, mock := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		lastMessages = messages
		return llmschema.NewAssistantMessage("mock response"), nil
	})

	result, err := wrapper.Generate(context.Background(), "Hello, how are you?")
	require.NoError(t, err)
	assert.Equal(t, "mock response", result)
	assert.Equal(t, 1, mock.invokeCalls)

	// 验证 messages 构建：应通过 NewDictsMessagesParam 传入
	assert.True(t, lastMessages.IsDicts())
	dicts := lastMessages.Dicts()
	assert.Equal(t, 1, len(dicts))
	assert.Equal(t, "user", dicts[0]["role"])
	assert.Equal(t, "Hello, how are you?", dicts[0]["content"])
}

// TestOpenAILLMWrapper_Generate_带SystemPrompt 验证 system prompt 正确添加到 messages 头部。
func TestOpenAILLMWrapper_Generate_带SystemPrompt(t *testing.T) {
	var lastMessages model_clients.MessagesParam
	wrapper, _ := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		lastMessages = messages
		return llmschema.NewAssistantMessage("mock response"), nil
	})

	result, err := wrapper.Generate(context.Background(), "Hello",
		cecontext.WithSystemPrompt("You are a helpful assistant"))
	require.NoError(t, err)
	assert.Equal(t, "mock response", result)

	// 应有 2 条 messages: system + user
	dicts := lastMessages.Dicts()
	assert.Equal(t, 2, len(dicts))
	assert.Equal(t, "system", dicts[0]["role"])
	assert.Equal(t, "You are a helpful assistant", dicts[0]["content"])
	assert.Equal(t, "user", dicts[1]["role"])
	assert.Equal(t, "Hello", dicts[1]["content"])
}

// TestOpenAILLMWrapper_Generate_覆盖Temperature 验证 GenerateOption 中的 Temperature 覆盖默认值。
func TestOpenAILLMWrapper_Generate_覆盖Temperature(t *testing.T) {
	var capturedTemp *float64
	wrapper, _ := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, _ model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		params := model_clients.NewInvokeParams(opts...)
		capturedTemp = params.Temperature
		return llmschema.NewAssistantMessage("response"), nil
	})

	_, err := wrapper.Generate(context.Background(), "Hello", cecontext.WithTemperature(0.3))
	require.NoError(t, err)
	require.NotNil(t, capturedTemp)
	assert.InDelta(t, 0.3, *capturedTemp, 0.001)
}

// TestOpenAILLMWrapper_Generate_默认Temperature 验证无覆盖时使用构造时的默认 temperature。
func TestOpenAILLMWrapper_Generate_默认Temperature(t *testing.T) {
	var capturedTemp *float64
	wrapper, _ := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, _ model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		params := model_clients.NewInvokeParams(opts...)
		capturedTemp = params.Temperature
		return llmschema.NewAssistantMessage("response"), nil
	})

	_, err := wrapper.Generate(context.Background(), "Hello")
	require.NoError(t, err)
	require.NotNil(t, capturedTemp)
	assert.InDelta(t, 0.7, *capturedTemp, 0.001) // 使用构造时默认值 0.7
}

// TestOpenAILLMWrapper_Generate_较新模型不传MaxTokens 验证 newer model 不传 max_tokens。
func TestOpenAILLMWrapper_Generate_较新模型不传MaxTokens(t *testing.T) {
	var capturedMaxTokens *int
	wrapper, _ := newWrapperWithMock("gpt-5.2", true, func(_ context.Context, _ model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		params := model_clients.NewInvokeParams(opts...)
		capturedMaxTokens = params.MaxTokens
		return llmschema.NewAssistantMessage("response"), nil
	})

	_, err := wrapper.Generate(context.Background(), "Hello")
	require.NoError(t, err)
	// 较新模型不传 maxTokens
	assert.Nil(t, capturedMaxTokens)
}

// TestOpenAILLMWrapper_Generate_较旧模型传MaxTokens 验证非 newer model 传 max_tokens。
func TestOpenAILLMWrapper_Generate_较旧模型传MaxTokens(t *testing.T) {
	var capturedMaxTokens *int
	wrapper, _ := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, _ model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		params := model_clients.NewInvokeParams(opts...)
		capturedMaxTokens = params.MaxTokens
		return llmschema.NewAssistantMessage("response"), nil
	})

	_, err := wrapper.Generate(context.Background(), "Hello")
	require.NoError(t, err)
	require.NotNil(t, capturedMaxTokens)
	assert.Equal(t, 2000, *capturedMaxTokens)
}

// TestOpenAILLMWrapper_Generate_覆盖MaxTokens 验证 GenerateOption 中的 MaxTokens 覆盖默认值。
func TestOpenAILLMWrapper_Generate_覆盖MaxTokens(t *testing.T) {
	var capturedMaxTokens *int
	wrapper, _ := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, _ model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		params := model_clients.NewInvokeParams(opts...)
		capturedMaxTokens = params.MaxTokens
		return llmschema.NewAssistantMessage("response"), nil
	})

	_, err := wrapper.Generate(context.Background(), "Hello", cecontext.WithMaxTokens(1000))
	require.NoError(t, err)
	require.NotNil(t, capturedMaxTokens)
	assert.Equal(t, 1000, *capturedMaxTokens)
}

// TestOpenAILLMWrapper_Generate_调用失败 验证 LLM 调用失败时返回 EvolvingMemory 错误码。
func TestOpenAILLMWrapper_Generate_调用失败(t *testing.T) {
	wrapper, _ := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("network error")
	})

	_, err := wrapper.Generate(context.Background(), "Hello")
	require.Error(t, err)
	// 验证错误码是 TOOLCHAIN_EVOLVING_MEMORY_LLM_GENERATION_EXECUTION_ERROR
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestOpenAILLMWrapper_Generate_BaseError透传 验证 BaseError 类型的错误直接透传。
func TestOpenAILLMWrapper_Generate_BaseError透传(t *testing.T) {
	originalErr := exception.NewBaseError(
		exception.StatusToolchainEvolvingMemoryConfigInvalid,
		exception.WithMsg("test base error"),
	)
	wrapper, _ := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, originalErr
	})

	_, err := wrapper.Generate(context.Background(), "Hello")
	require.Error(t, err)
	// BaseError 应直接透传，不被包装
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestOpenAILLMWrapper_Generate_空响应 验证 LLM 返回空内容时返回空字符串。
func TestOpenAILLMWrapper_Generate_空响应(t *testing.T) {
	wrapper, _ := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(""), nil
	})

	result, err := wrapper.Generate(context.Background(), "Hello")
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

// TestOpenAILLMWrapper_GenerateWithMessages_基本调用 验证 GenerateWithMessages 正确传递 messages。
func TestOpenAILLMWrapper_GenerateWithMessages_基本调用(t *testing.T) {
	var lastMessages model_clients.MessagesParam
	wrapper, _ := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		lastMessages = messages
		return llmschema.NewAssistantMessage("mock response"), nil
	})

	messages := []map[string]any{
		{"role": "system", "content": "You are helpful"},
		{"role": "user", "content": "Hello"},
	}

	result, err := wrapper.GenerateWithMessages(context.Background(), messages)
	require.NoError(t, err)
	assert.Equal(t, "mock response", result)

	// 验证 messages 直接透传（通过 NewDictsMessagesParam）
	dicts := lastMessages.Dicts()
	assert.Equal(t, 2, len(dicts))
	assert.Equal(t, "system", dicts[0]["role"])
	assert.Equal(t, "user", dicts[1]["role"])
}

// TestOpenAILLMWrapper_GenerateWithMessages_调用失败 验证错误处理。
func TestOpenAILLMWrapper_GenerateWithMessages_调用失败(t *testing.T) {
	wrapper, _ := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("API error")
	})

	_, err := wrapper.GenerateWithMessages(context.Background(), []map[string]any{{"role": "user", "content": "hi"}})
	require.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestOpenAILLMWrapper_GenerateWithMessages_覆盖MaxTokens 验证 MaxTokens 选项生效。
func TestOpenAILLMWrapper_GenerateWithMessages_覆盖MaxTokens(t *testing.T) {
	var capturedMaxTokens *int
	wrapper, _ := newWrapperWithMock("gpt-3.5-turbo", false, func(_ context.Context, _ model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		params := model_clients.NewInvokeParams(opts...)
		capturedMaxTokens = params.MaxTokens
		return llmschema.NewAssistantMessage("response"), nil
	})

	_, err := wrapper.GenerateWithMessages(context.Background(),
		[]map[string]any{{"role": "user", "content": "hi"}},
		cecontext.WithMaxTokens(500))
	require.NoError(t, err)
	require.NotNil(t, capturedMaxTokens)
	assert.Equal(t, 500, *capturedMaxTokens)
}

// TestIsNewerModel 验证 isNewerModel 辅助函数。
func TestIsNewerModel(t *testing.T) {
	tests := []struct {
		model    string
		expected bool
	}{
		{"gpt-4o", true},
		{"gpt-4-turbo", true},
		{"gpt-5.2", true},
		{"o1-preview", true},
		{"o3-mini", true},
		{"gpt-3.5-turbo", false},
		{"text-davinci-003", false},
		{"claude-3", false},
		{"GPT-4O", true}, // 大写也应匹配
	}

	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			result := isNewerModelCheck(tc.model)
			assert.Equal(t, tc.expected, result, "model=%s", tc.model)
		})
	}
}

// TestOpenAILLMWrapper_String 验证 String 方法。
func TestOpenAILLMWrapper_String(t *testing.T) {
	wrapper := &OpenAILLMWrapper{modelName: "gpt-5.2"}
	result := wrapper.String()
	assert.True(t, strings.Contains(result, "gpt-5.2"))
}
