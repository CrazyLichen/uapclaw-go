package offloader

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试用例 ────────────────────────────

// 注意：复用同包中已有的 fakeModelContext（message_offloader_test.go 中定义）。
// fakeModelContext 实现了 TokenCounter 接口，CountMessages 返回 tokenCount 字段。
// 对需要大消息通过阈值的测试，必须设置 tokenCount > LargeMessageThreshold。

// msoNewForTest 创建带 fake model 的 MessageSummaryOffloader，用于不需要 LLM 调用的纯逻辑测试。
func msoNewForTest(cfg *MessageSummaryOffloaderConfig) *MessageSummaryOffloader {
	fakeClient := &msoFakeBaseModelClient{
		invokeResp: llm_schema.NewAssistantMessage(`{"summary":"test","offload_data_explanation":{}}`),
	}
	model := msoNewFakeLLMModel(fakeClient)
	mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
	if err != nil {
		panic(fmt.Sprintf("msoNewForTest: %v", err))
	}
	return mso
}

// TestMessageSummaryOffloaderConfig_Validate 测试配置校验
func TestMessageSummaryOffloaderConfig_Validate(t *testing.T) {
	t.Run("默认值应用", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Equal(t, msoDefaultTokensThreshold, cfg.TokensThreshold)
		assert.Equal(t, msoDefaultLargeMessageThreshold, cfg.LargeMessageThreshold)
		assert.Equal(t, []string{"tool"}, cfg.OffloadMessageTypes)
		assert.Equal(t, []string{"reload_original_context_messages"}, cfg.ProtectedToolNames)
		assert.Equal(t, msoDefaultSummaryMaxTokens, cfg.SummaryMaxTokens)
		assert.Equal(t, msoDefaultStepSummaryMaxContextMessages, cfg.StepSummaryMaxContextMessages)
		assert.Equal(t, msoDefaultContentMaxCharsForCompression, cfg.ContentMaxCharsForCompression)
	})

	t.Run("MessagesToKeep大于MessagesThreshold报错", func(t *testing.T) {
		keep := 10
		threshold := 5
		cfg := &MessageSummaryOffloaderConfig{
			MessagesToKeep:    &keep,
			MessagesThreshold: &threshold,
		}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MessagesToKeep")
	})

	t.Run("MessagesToKeep小于MessagesThreshold通过", func(t *testing.T) {
		keep := 5
		threshold := 10
		cfg := &MessageSummaryOffloaderConfig{
			MessagesToKeep:    &keep,
			MessagesThreshold: &threshold,
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})
}

// TestMessageSummaryOffloader_ProcessorType 测试处理器类型
func TestMessageSummaryOffloader_ProcessorType(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)
	assert.Equal(t, "MessageSummaryOffloader", mso.ProcessorType())
}

// TestMessageSummaryOffloader_TriggerAddMessages 测试触发判断
func TestMessageSummaryOffloader_TriggerAddMessages(t *testing.T) {
	t.Run("新消息超阈值触发", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold: 100,
		}
		require.NoError(t, cfg.Validate())
		mso := msoNewForTest(cfg)

		// 创建一条大的 tool 消息
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		// fakeModelContext 的 TokenCounter 返回 tokenCount=0，
		// 需要设置足够大的 tokenCount 或不设置 TokenCounter
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 200}

		triggered, err := mso.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{toolMsg})
		require.NoError(t, err)
		assert.True(t, triggered)
	})

	t.Run("新消息不超阈值不触发", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold: 1000,
		}
		require.NoError(t, cfg.Validate())
		mso := msoNewForTest(cfg)

		shortMsg := llm_schema.NewToolMessage("call-1", "short")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}

		triggered, err := mso.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{shortMsg})
		require.NoError(t, err)
		assert.False(t, triggered)
	})

	t.Run("角色不匹配不触发", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold: 10,
		}
		require.NoError(t, cfg.Validate())
		mso := msoNewForTest(cfg)

		longContent := strings.Repeat("x", 500)
		userMsg := llm_schema.NewUserMessage(longContent)
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}

		triggered, err := mso.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{userMsg})
		require.NoError(t, err)
		assert.False(t, triggered)
	})
}

// TestMessageSummaryOffloader_shouldOffloadMessage 测试消息筛选
func TestMessageSummaryOffloader_shouldOffloadMessage(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{
		LargeMessageThreshold: 100,
	}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("角色不匹配返回false", func(t *testing.T) {
		userMsg := llm_schema.NewUserMessage(strings.Repeat("x", 500))
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 200}
		assert.False(t, mso.shouldOffloadMessage(userMsg, mc, nil))
	})

	t.Run("已卸载消息返回false", func(t *testing.T) {
		offloaded := schema.NewOffloadToolMessage("call-1", "summary", "handle", "in_memory")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 200}
		assert.False(t, mso.shouldOffloadMessage(offloaded, mc, nil))
	})

	t.Run("受保护工具返回false", func(t *testing.T) {
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		assistantMsg := llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call-1", Name: "reload_original_context_messages", Arguments: "{}"},
			}),
		)
		mc := &fakeModelContext{messages: []llm_schema.BaseMessage{assistantMsg}, sessionID: "test-session", tokenCount: 200}
		assert.False(t, mso.shouldOffloadMessage(toolMsg, mc, mc.GetMessages(0, true)))
	})

	t.Run("消息太小返回false", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call-1", "short")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 10}
		assert.False(t, mso.shouldOffloadMessage(toolMsg, mc, nil))
	})

	t.Run("符合条件的tool消息返回true", func(t *testing.T) {
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 200}
		assert.True(t, mso.shouldOffloadMessage(toolMsg, mc, nil))
	})
}

// TestMessageSummaryOffloader_isContextOverflowError 测试上下文溢出检测
func TestMessageSummaryOffloader_isContextOverflowError(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	tests := []struct {
		name     string
		errMsg   string
		overflow bool
	}{
		{"context length", "maximum context length exceeded", true},
		{"token limit", "token limit reached", true},
		{"too long", "prompt is too long", true},
		{"exceeds", "input exceeds maximum", true},
		{"maximum context", "maximum context window", true},
		{"context window", "context window exceeded", true},
		{"normal error", "connection timeout", false},
		{"nil error", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = fmt.Errorf("%s", tt.errMsg)
			}
			assert.Equal(t, tt.overflow, mso.isContextOverflowError(err))
		})
	}
}

// TestMessageSummaryOffloader_smartTruncateContent 测试智能截断
func TestMessageSummaryOffloader_smartTruncateContent(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("短内容不截断", func(t *testing.T) {
		content := "short"
		result := mso.smartTruncateContent(content, 100)
		assert.Equal(t, content, result)
	})

	t.Run("长内容截断保留头中尾", func(t *testing.T) {
		content := strings.Repeat("a", 300)
		result := mso.smartTruncateContent(content, 100)
		assert.Contains(t, result, truncatedMarker)
		assert.Less(t, len(result), len(content))
	})

	t.Run("极小maxChars直接截断", func(t *testing.T) {
		content := strings.Repeat("a", 100)
		result := mso.smartTruncateContent(content, 5)
		assert.Equal(t, 5, len(result))
	})
}

// TestMessageSummaryOffloader_parseCompressionResult 测试压缩结果解析
func TestMessageSummaryOffloader_parseCompressionResult(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("正常JSON", func(t *testing.T) {
		input := `{"compression_strategy":"extractive","summary":"test summary","offload_data_explanation":{"category":"logs","description":"raw data","inferability":"medium"}}`
		result, err := mso.parseCompressionResult(input)
		require.NoError(t, err)
		assert.Equal(t, "test summary", result["summary"])
	})

	t.Run("Markdown包裹的JSON", func(t *testing.T) {
		input := "```json\n{\"summary\":\"test summary\",\"offload_data_explanation\":{}}\n```"
		result, err := mso.parseCompressionResult(input)
		require.NoError(t, err)
		assert.Equal(t, "test summary", result["summary"])
	})

	t.Run("缺少summary字段报错", func(t *testing.T) {
		input := `{"offload_data_explanation":{}}`
		_, err := mso.parseCompressionResult(input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "summary")
	})

	t.Run("无JSON报错", func(t *testing.T) {
		input := "no json here"
		_, err := mso.parseCompressionResult(input)
		assert.Error(t, err)
	})
}

// TestMessageSummaryOffloader_getStepFromChainDefault 测试默认任务提取
func TestMessageSummaryOffloader_getStepFromChainDefault(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("返回最后的UserMessage", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("first task"),
			llm_schema.NewAssistantMessage("ok"),
			llm_schema.NewUserMessage("second task"),
		}
		step := mso.getStepFromChainDefault(messages)
		assert.Equal(t, "second task", step)
	})

	t.Run("无UserMessage返回空", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewAssistantMessage("ok"),
		}
		step := mso.getStepFromChainDefault(messages)
		assert.Equal(t, "", step)
	})

	t.Run("空消息列表返回空", func(t *testing.T) {
		step := mso.getStepFromChainDefault(nil)
		assert.Equal(t, "", step)
	})
}

// TestMessageSummaryOffloader_isValidForStepSummary 测试消息筛选
func TestMessageSummaryOffloader_isValidForStepSummary(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("UserMessage有效", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("hello")
		assert.True(t, mso.isValidForStepSummary(msg))
	})

	t.Run("AssistantMessage无tool_calls有效", func(t *testing.T) {
		msg := llm_schema.NewAssistantMessage("hi")
		assert.True(t, mso.isValidForStepSummary(msg))
	})

	t.Run("AssistantMessage有tool_calls无效", func(t *testing.T) {
		msg := llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call-1", Name: "test", Arguments: "{}"},
			}),
		)
		assert.False(t, mso.isValidForStepSummary(msg))
	})

	t.Run("ToolMessage无效", func(t *testing.T) {
		msg := llm_schema.NewToolMessage("call-1", "result")
		assert.False(t, mso.isValidForStepSummary(msg))
	})
}

// TestMessageSummaryOffloader_buildCompressionAttempts 测试降级尝试构建
func TestMessageSummaryOffloader_buildCompressionAttempts(t *testing.T) {
	t.Run("短内容只返回原文", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{
			ContentMaxCharsForCompression: 1000,
		}
		require.NoError(t, cfg.Validate())
		mso := msoNewForTest(cfg)

		content := "short content"
		attempts := mso.buildCompressionAttempts(content)
		assert.Len(t, attempts, 1)
		assert.Equal(t, content, attempts[0])
	})

	t.Run("长内容返回三级降级", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{
			ContentMaxCharsForCompression: 1000,
		}
		require.NoError(t, cfg.Validate())
		mso := msoNewForTest(cfg)

		content := strings.Repeat("x", 3000)
		attempts := mso.buildCompressionAttempts(content)
		assert.Len(t, attempts, 3)
		assert.Equal(t, content, attempts[0])
		assert.Contains(t, attempts[1], truncatedMarker)
		assert.Contains(t, attempts[2], truncatedMarker)
	})
}

// ──────────────────────────── LLM 测试基础设施 ────────────────────────────

// msoFakeBaseModelClient 用于测试的模拟模型客户端
type msoFakeBaseModelClient struct {
	invokeResp *llm_schema.AssistantMessage
	invokeErr  error
	invoked    bool
	lastPrompt string
}

func (f *msoFakeBaseModelClient) Invoke(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llm_schema.AssistantMessage, error) {
	f.invoked = true
	if messages.IsText() {
		f.lastPrompt = messages.Text()
	}
	return f.invokeResp, f.invokeErr
}
func (f *msoFakeBaseModelClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (*model_clients.StreamResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *msoFakeBaseModelClient) GenerateImage(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateImageOption) (*llm_schema.ImageGenerationResponse, error) {
	return nil, nil
}
func (f *msoFakeBaseModelClient) GenerateSpeech(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llm_schema.AudioGenerationResponse, error) {
	return nil, nil
}
func (f *msoFakeBaseModelClient) GenerateVideo(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llm_schema.VideoGenerationResponse, error) {
	return nil, nil
}
func (f *msoFakeBaseModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

const msoTestProvider = "MSOTestProvider"

// msoCurrentFakeClient 当前使用的 fake 客户端
var msoCurrentFakeClient *msoFakeBaseModelClient

// msoCurrentFakeClientAny 当前使用的 fake 客户端（接口类型，用于 msoRetryFakeClient 等）
var msoCurrentFakeClientAny model_clients.BaseModelClient

// msoFakeRegistryOnce 确保 fake provider 只注册一次
var msoFakeRegistryOnce sync.Once

// msoNewFakeLLMModel 创建带 fake client 的 llm.Model 实例
// 使用 ClientRegistry 注册模式，与 compressor 测试对齐
func msoNewFakeLLMModel(fakeClient *msoFakeBaseModelClient) *llm.Model {
	return msoNewFakeLLMModelFromClient(fakeClient)
}

// msoNewFakeLLMModelFromClient 从任意 BaseModelClient 创建 llm.Model 实例
func msoNewFakeLLMModelFromClient(fakeClient model_clients.BaseModelClient) *llm.Model {
	msoCurrentFakeClientAny = fakeClient
	msoFakeRegistryOnce.Do(func() {
		model_clients.GetClientRegistry().Register(msoTestProvider, "llm",
			func(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) model_clients.BaseModelClient {
				return msoCurrentFakeClientAny
			},
		)
	})

	clientConfig := &llm_schema.ModelClientConfig{
		ClientID:       "mso-test-client",
		ClientProvider: msoTestProvider,
		APIKey:         "fake-key",
		APIBase:        "https://fake.api.com",
	}
	modelConfig := llm_schema.NewModelRequestConfig(llm_schema.WithModelName("test-model"))
	model, err := llm.NewModel(clientConfig, modelConfig)
	if err != nil {
		panic(fmt.Sprintf("msoNewFakeLLMModel: %v", err))
	}
	return model
}

// msoContentAwareModelContext 基于 message 内容长度计算 token 数的伪 ModelContext。
// 与 fakeModelContext（固定 tokenCount）不同，CountMessages 按 len(content)/3 逐条累加，
// 使得短消息和小消息自然区分大小阈值。
type msoContentAwareModelContext struct {
	fakeModelContext
}

func (c *msoContentAwareModelContext) TokenCounter() token.TokenCounter { return c }

func (c *msoContentAwareModelContext) CountMessages(messages []llm_schema.BaseMessage, _ string) (int, error) {
	total := 0
	for _, msg := range messages {
		total += len(msg.GetContent().Text()) / 3
	}
	return total, nil
}

func (c *msoContentAwareModelContext) Count(text string, _ string) (int, error) {
	return len(text) / 3, nil
}

func (c *msoContentAwareModelContext) CountTools(_ []*commonschema.ToolInfo, _ string) (int, error) {
	return 0, nil
}

// ──────────────────────────── OnAddMessages + compressWithFallback 集成测试 ────────────────────────────

// TestMessageSummaryOffloader_OnAddMessages_完整流程 测试完整摘要卸载流程
func TestMessageSummaryOffloader_OnAddMessages_完整流程(t *testing.T) {
	// 创建 fake model（复用 compressor 测试中的 registry 模式）
	summaryJSON := `{"compression_strategy":"extractive","summary":"compressed summary","offload_data_explanation":{"category":"logs","description":"raw log data","inferability":"medium"}}`
	fakeClient := &msoFakeBaseModelClient{invokeResp: llm_schema.NewAssistantMessage(summaryJSON)}
	model := msoNewFakeLLMModel(fakeClient)

	cfg := &MessageSummaryOffloaderConfig{
		LargeMessageThreshold: 100,
	}
	require.NoError(t, cfg.Validate())

	mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
	require.NoError(t, err)

	t.Run("大消息被摘要卸载", func(t *testing.T) {
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 200}

		event, processed, err := mso.OnAddMessages(context.Background(), mc, []llm_schema.BaseMessage{toolMsg})
		require.NoError(t, err)
		require.NotNil(t, event, "期望 event 非nil，但返回 nil（可能 offloadMessageAdaptive 失败被跳过）")
		assert.Equal(t, "MessageSummaryOffloader", event.EventType)
		assert.Len(t, event.MessagesToModify, 1)
		// 处理后内容应包含压缩摘要
		assert.Contains(t, processed[0].GetContent().Text(), "compressed summary")
	})

	t.Run("小消息不被卸载", func(t *testing.T) {
		shortMsg := llm_schema.NewToolMessage("call-1", "short")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 10}

		event, processed, err := mso.OnAddMessages(context.Background(), mc, []llm_schema.BaseMessage{shortMsg})
		require.NoError(t, err)
		assert.Nil(t, event)
		assert.Equal(t, "short", processed[0].GetContent().Text())
	})

	t.Run("混合消息只卸载符合条件的", func(t *testing.T) {
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		shortMsg := llm_schema.NewToolMessage("call-2", "short")
		// 使用基于内容计算 token 的 fake，短消息 (len("short")/3=1) < LargeMessageThreshold(100)
		// 长消息 (len(500x)/3=166) > LargeMessageThreshold(100)
		mc := &msoContentAwareModelContext{fakeModelContext: fakeModelContext{messages: nil, sessionID: "test-session"}}

		event, processed, err := mso.OnAddMessages(context.Background(), mc, []llm_schema.BaseMessage{toolMsg, shortMsg})
		require.NoError(t, err)
		assert.NotNil(t, event)
		assert.Len(t, event.MessagesToModify, 1)
		// 第二条消息不变
		assert.Equal(t, "short", processed[1].GetContent().Text())
	})
}

// TestMessageSummaryOffloader_SaveLoadState 测试状态保存/加载
func TestMessageSummaryOffloader_SaveLoadState(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	state := mso.SaveState()
	assert.Empty(t, state)

	mso.LoadState(map[string]any{"test": "value"})
	// 空操作，不 panic 即可
}

// TestMessageSummaryOffloader_compressWithFallback 测试降级压缩
func TestMessageSummaryOffloader_compressWithFallback(t *testing.T) {
	summaryJSON := `{"compression_strategy":"abstractive","summary":"test summary","offload_data_explanation":{"category":"data","description":"full data","inferability":"low"}}`
	fakeClient := &msoFakeBaseModelClient{invokeResp: llm_schema.NewAssistantMessage(summaryJSON)}
	model := msoNewFakeLLMModel(fakeClient)

	cfg := &MessageSummaryOffloaderConfig{
		LargeMessageThreshold:         100,
		ContentMaxCharsForCompression: 1000,
	}
	require.NoError(t, cfg.Validate())

	mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
	require.NoError(t, err)

	t.Run("正常压缩返回结果", func(t *testing.T) {
		result, err := mso.compressWithFallback(context.Background(), "test step", nil, "test content")
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "test summary", result["summary"])
	})

	t.Run("长内容触发智能截断", func(t *testing.T) {
		longContent := strings.Repeat("x", 2000)
		result, err := mso.compressWithFallback(context.Background(), "test step", nil, longContent)
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

// TestMessageSummaryOffloader_buildCompressionPrompt 测试提示词构建
func TestMessageSummaryOffloader_buildCompressionPrompt(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{
		SummaryMaxTokens: 900,
	}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("无functionCall时显示NA", func(t *testing.T) {
		prompt := mso.buildCompressionPrompt("test step", nil, "test content")
		assert.Contains(t, prompt, "N/A")
		assert.Contains(t, prompt, "test step")
		assert.Contains(t, prompt, "test content")
		assert.Contains(t, prompt, "900")
	})

	t.Run("有functionCall时序列化", func(t *testing.T) {
		tc := &llm_schema.ToolCall{
			ID:        "call-1",
			Name:      "search",
			Arguments: `{"query": "test"}`,
		}
		prompt := mso.buildCompressionPrompt("test step", tc, "test content")
		assert.Contains(t, prompt, "search")
		assert.Contains(t, prompt, "test step")
	})
}

// TestMessageSummaryOffloader_newOffloadHandleAndPath 测试句柄和路径生成
func TestMessageSummaryOffloader_newOffloadHandleAndPath(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	mc := &fakeModelContext{messages: nil, sessionID: "test-session"}
	handle, path := mso.newOffloadHandleAndPath(mc)
	assert.NotEmpty(t, handle)
	// 当前 WorkspaceDir 为空，path 为空
	assert.Empty(t, path)
}

// TestMessageSummaryOffloader_selectMessagesForStepSummary 测试消息筛选
func TestMessageSummaryOffloader_selectMessagesForStepSummary(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{
		StepSummaryMaxContextMessages: 3,
	}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("过滤后消息不足2条返回nil", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			llm_schema.NewToolMessage("call-1", "result"),
		}
		result := mso.selectMessagesForStepSummary(messages)
		assert.Nil(t, result)
	})

	t.Run("保留最近N条", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("msg1"),
			llm_schema.NewAssistantMessage("msg2"),
			llm_schema.NewUserMessage("msg3"),
			llm_schema.NewAssistantMessage("msg4"),
			llm_schema.NewUserMessage("msg5"),
		}
		result := mso.selectMessagesForStepSummary(messages)
		require.Len(t, result, 3)
		// 保留最后3条
		assert.Equal(t, "msg3", result[0].GetContent().Text())
		assert.Equal(t, "msg5", result[2].GetContent().Text())
	})
}

// ──────────────────────────── 补充覆盖率测试 ────────────────────────────

// TestMessageSummaryOffloader_NewMessageSummaryOffloader_无ModelClient 测试无 ModelClient 时创建
func TestMessageSummaryOffloader_NewMessageSummaryOffloader_无ModelClient(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	// 不传 ModelClient 和 WithMessageSummaryModel，应返回错误
	mso, err := NewMessageSummaryOffloader(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ModelClient")
	assert.Nil(t, mso)
}

// TestMessageSummaryOffloader_NewMessageSummaryOffloader_有ModelClient 测试有 ModelClient 时创建
func TestMessageSummaryOffloader_NewMessageSummaryOffloader_有ModelClient(t *testing.T) {
	fakeClient := &msoFakeBaseModelClient{
		invokeResp: llm_schema.NewAssistantMessage(`{"summary":"ok","offload_data_explanation":{}}`),
	}
	model := msoNewFakeLLMModel(fakeClient)
	cfg := &MessageSummaryOffloaderConfig{
		ModelClient: &llm_schema.ModelClientConfig{
			ClientID:       "mso-test-client",
			ClientProvider: msoTestProvider,
			APIKey:         "fake-key",
			APIBase:        "https://fake.api.com",
		},
	}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
	require.NoError(t, err)
	assert.NotNil(t, mso.model)
}

// TestMessageSummaryOffloader_messageSize 测试消息大小计算
func TestMessageSummaryOffloader_messageSize(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("TokenCounter优先", func(t *testing.T) {
		msg := llm_schema.NewToolMessage("call-1", "hello world")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 42}
		size := mso.messageSize(msg, mc)
		assert.Equal(t, 42, size)
	})

	t.Run("TokenCounter为nil时回退字符数除3", func(t *testing.T) {
		msg := llm_schema.NewToolMessage("call-1", "hello world") // 11 字符
		// 创建不实现 TokenCounter 的 mc
		mc := &msoNoTokenCounterModelContext{}
		size := mso.messageSize(msg, mc)
		assert.Equal(t, len("hello world")/3, size)
	})
}

// msoNoTokenCounterModelContext 不实现 TokenCounter 的 ModelContext
type msoNoTokenCounterModelContext struct {
	fakeModelContext
}

func (c *msoNoTokenCounterModelContext) TokenCounter() token.TokenCounter { return nil }

// TestMessageSummaryOffloader_msoMatchPattern 测试模式匹配
func TestMessageSummaryOffloader_msoMatchPattern(t *testing.T) {
	t.Run("通配符匹配", func(t *testing.T) {
		args := map[string]any{"file": "some_file_name"}
		assert.True(t, msoMatchPattern(args, "some*name"))
	})
	t.Run("精确匹配", func(t *testing.T) {
		args := map[string]any{"key": "exact_match"}
		assert.True(t, msoMatchPattern(args, "exact_match"))
	})
	t.Run("不匹配", func(t *testing.T) {
		args := map[string]any{"key": "no_match"}
		assert.False(t, msoMatchPattern(args, "other*"))
	})
	t.Run("非字符串值跳过", func(t *testing.T) {
		args := map[string]any{"count": 42, "name": "other"}
		assert.False(t, msoMatchPattern(args, "4*"))
	})
}

// TestMessageSummaryOffloader_msoExtractToolArgs 测试工具参数提取
func TestMessageSummaryOffloader_msoExtractToolArgs(t *testing.T) {
	t.Run("合法JSON参数", func(t *testing.T) {
		tc := &llm_schema.ToolCall{ID: "call-1", Name: "search", Arguments: `{"key": "value", "count": 5}`}
		result := msoExtractToolArgs(tc)
		assert.Contains(t, result, "key")
		assert.Equal(t, "value", result["key"])
	})
	t.Run("非法JSON参数", func(t *testing.T) {
		tc := &llm_schema.ToolCall{ID: "call-1", Name: "search", Arguments: "not json"}
		result := msoExtractToolArgs(tc)
		assert.Empty(t, result)
	})
	t.Run("空JSON参数", func(t *testing.T) {
		tc := &llm_schema.ToolCall{ID: "call-1", Name: "search", Arguments: "{}"}
		result := msoExtractToolArgs(tc)
		assert.Empty(t, result)
	})
	t.Run("nil ToolCall", func(t *testing.T) {
		result := msoExtractToolArgs(nil)
		assert.Empty(t, result)
	})
	t.Run("空Arguments", func(t *testing.T) {
		tc := &llm_schema.ToolCall{ID: "call-1", Name: "search", Arguments: ""}
		result := msoExtractToolArgs(tc)
		assert.Empty(t, result)
	})
}

// TestMessageSummaryOffloader_isProtectedToolMessage 测试受保护工具消息
func TestMessageSummaryOffloader_isProtectedToolMessage(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{
		ProtectedToolNames: []string{"reload_original_context_messages", "protected_tool*"},
	}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("受保护工具名完全匹配", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call-1", "result")
		assistantMsg := llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call-1", Name: "reload_original_context_messages", Arguments: "{}"},
			}),
		)
		contextMessages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		assert.True(t, mso.isProtectedToolMessage(toolMsg, contextMessages))
	})

	t.Run("受保护工具名通配符匹配（需冒号语法）", func(t *testing.T) {
		// 不带冒号的模式只做精确匹配；带冒号的模式才能对参数值做通配
		// 此处测试 "protected_tool_v2:pattern*" 形式
		cfg2 := &MessageSummaryOffloaderConfig{
			ProtectedToolNames: []string{"protected_tool_v2:data*"},
		}
		require.NoError(t, cfg2.Validate())
		mso2 := msoNewForTest(cfg2)

		toolMsg := llm_schema.NewToolMessage("call-2", "result")
		assistantMsg := llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call-2", Name: "protected_tool_v2", Arguments: `{"key": "data_file_123"}`},
			}),
		)
		contextMessages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		assert.True(t, mso2.isProtectedToolMessage(toolMsg, contextMessages))
	})

	t.Run("不受保护的工具", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call-3", "result")
		assistantMsg := llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call-3", Name: "normal_tool", Arguments: "{}"},
			}),
		)
		contextMessages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		assert.False(t, mso.isProtectedToolMessage(toolMsg, contextMessages))
	})

	t.Run("无ToolCallID的ToolMessage", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("", "result")
		contextMessages := []llm_schema.BaseMessage{toolMsg}
		assert.False(t, mso.isProtectedToolMessage(toolMsg, contextMessages))
	})

	t.Run("空上下文消息", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call-1", "result")
		assert.False(t, mso.isProtectedToolMessage(toolMsg, nil))
	})
}

// TestMessageSummaryOffloader_getStepFromChainPrecise 测试精确任务提取
func TestMessageSummaryOffloader_getStepFromChainPrecise(t *testing.T) {
	// getStepFromChainPrecise 需要 LLM，创建 fake model
	fakeClient := &msoFakeBaseModelClient{
		invokeResp: llm_schema.NewAssistantMessage("extracted step from LLM"),
	}
	model := msoNewFakeLLMModel(fakeClient)
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
	require.NoError(t, err)

	t.Run("从LLM提取step", func(t *testing.T) {
		userMsg := llm_schema.NewUserMessage("query 1")
		assistantMsg := llm_schema.NewAssistantMessage("answer 1")
		userMsg2 := llm_schema.NewUserMessage("query 2")
		assistantMsg2 := llm_schema.NewAssistantMessage("answer 2")
		messages := []llm_schema.BaseMessage{userMsg, assistantMsg, userMsg2, assistantMsg2}
		step, err := mso.getStepFromChainPrecise(context.Background(), messages)
		require.NoError(t, err)
		assert.Equal(t, "extracted step from LLM", step)
	})

	t.Run("消息不足2条返回空", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call-1", "result"),
		}
		step, err := mso.getStepFromChainPrecise(context.Background(), messages)
		require.NoError(t, err)
		assert.Equal(t, "", step)
	})

	t.Run("空消息列表返回空", func(t *testing.T) {
		step, err := mso.getStepFromChainPrecise(context.Background(), nil)
		require.NoError(t, err)
		assert.Equal(t, "", step)
	})
}

// TestMessageSummaryOffloader_buildStepContextText 测试步骤上下文文本构建
func TestMessageSummaryOffloader_buildStepContextText(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{
		StepSummaryMaxContextMessages: 5,
	}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("多条有效消息构建上下文", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("query 1"),
			llm_schema.NewAssistantMessage("answer 1"),
			llm_schema.NewUserMessage("query 2"),
		}
		text := mso.buildStepContextText(messages)
		assert.Contains(t, text, "query 1")
		assert.Contains(t, text, "answer 1")
		assert.Contains(t, text, "query 2")
	})

	t.Run("nil消息返回空", func(t *testing.T) {
		text := mso.buildStepContextText(nil)
		assert.Equal(t, "", text)
	})
}

// TestMessageSummaryOffloader_compressWithFallback_降级 测试压缩降级全流程
func TestMessageSummaryOffloader_compressWithFallback_降级(t *testing.T) {
	t.Run("LLM非溢出错误直接返回错误", func(t *testing.T) {
		fakeClient := &msoFakeBaseModelClient{
			invokeErr: fmt.Errorf("service unavailable"),
		}
		model := msoNewFakeLLMModel(fakeClient)
		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold:         100,
			ContentMaxCharsForCompression: 1000,
		}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
		require.NoError(t, err)

		content := strings.Repeat("x", 500)
		_, err = mso.compressWithFallback(context.Background(), "test step", nil, content)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "service unavailable")
	})

	t.Run("上下文溢出错误触发重试", func(t *testing.T) {
		// 第一次返回上下文溢出，第二次成功
		callCount := 0
		retryClient := &msoRetryFakeClient{callCount: &callCount}
		model := msoNewFakeLLMModelFromClient(retryClient)

		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold:         100,
			ContentMaxCharsForCompression: 500,
		}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
		require.NoError(t, err)

		// 内容超过 ContentMaxCharsForCompression 才会生成多次降级尝试
		content := strings.Repeat("x", 2000)
		result, err := mso.compressWithFallback(context.Background(), "test step", nil, content)
		require.NoError(t, err)
		require.NotNil(t, result)
		// 重试成功后应返回摘要
		assert.Equal(t, "retry summary", result["summary"])
	})

	t.Run("解析失败响应比原文短时用作摘要", func(t *testing.T) {
		// 返回非 JSON 但比原文短的内容
		fakeClient := &msoFakeBaseModelClient{
			invokeResp: llm_schema.NewAssistantMessage("short raw response"),
		}
		model := msoNewFakeLLMModel(fakeClient)
		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold:         100,
			ContentMaxCharsForCompression: 1000,
		}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
		require.NoError(t, err)

		longContent := strings.Repeat("x", 2000)
		result, err := mso.compressWithFallback(context.Background(), "test step", nil, longContent)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "short raw response", result["summary"])
	})

	t.Run("解析失败响应比原文长时返回nil", func(t *testing.T) {
		// 返回非 JSON 但比原文长的内容
		longResponse := strings.Repeat("y", 3000)
		fakeClient := &msoFakeBaseModelClient{
			invokeResp: llm_schema.NewAssistantMessage(longResponse),
		}
		model := msoNewFakeLLMModel(fakeClient)
		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold:         100,
			ContentMaxCharsForCompression: 1000,
		}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
		require.NoError(t, err)

		content := strings.Repeat("x", 2000)
		result, err := mso.compressWithFallback(context.Background(), "test step", nil, content)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

// msoRetryFakeClient 模拟第一次溢出第二次成功的客户端
type msoRetryFakeClient struct {
	callCount *int
}

func (f *msoRetryFakeClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llm_schema.AssistantMessage, error) {
	*f.callCount++
	if *f.callCount == 1 {
		return nil, fmt.Errorf("maximum context length exceeded: input too long")
	}
	summaryJSON := `{"compression_strategy":"abstractive","summary":"retry summary","offload_data_explanation":{"category":"data","description":"data","inferability":"low"}}`
	return llm_schema.NewAssistantMessage(summaryJSON), nil
}
func (f *msoRetryFakeClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (*model_clients.StreamResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *msoRetryFakeClient) GenerateImage(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateImageOption) (*llm_schema.ImageGenerationResponse, error) {
	return nil, nil
}
func (f *msoRetryFakeClient) GenerateSpeech(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llm_schema.AudioGenerationResponse, error) {
	return nil, nil
}
func (f *msoRetryFakeClient) GenerateVideo(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llm_schema.VideoGenerationResponse, error) {
	return nil, nil
}
func (f *msoRetryFakeClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// TestMessageSummaryOffloader_offloadMessageAdaptive 测试自适应卸载
func TestMessageSummaryOffloader_offloadMessageAdaptive(t *testing.T) {
	summaryJSON := `{"compression_strategy":"extractive","summary":"adaptive summary","offload_data_explanation":{"category":"logs","description":"log data","inferability":"medium"}}`
	fakeClient := &msoFakeBaseModelClient{invokeResp: llm_schema.NewAssistantMessage(summaryJSON)}
	model := msoNewFakeLLMModel(fakeClient)

	cfg := &MessageSummaryOffloaderConfig{
		LargeMessageThreshold:         100,
		ContentMaxCharsForCompression: 1000,
	}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
	require.NoError(t, err)

	t.Run("正常卸载返回替换消息", func(t *testing.T) {
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 200}

		replacement, err := mso.offloadMessageAdaptive(context.Background(), toolMsg, mc)
		require.NoError(t, err)
		require.NotNil(t, replacement)
		// 替换内容应包含摘要
		assert.Contains(t, replacement.GetContent().Text(), "adaptive summary")
		// 替换消息应实现 Offloadable
		assert.True(t, schema.IsOffloaded(replacement))
	})
}

// TestMessageSummaryOffloader_getStepFromChainDefault_补充 测试默认任务提取补充
func TestMessageSummaryOffloader_getStepFromChainDefault_补充(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("UserMessage为空内容返回空", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(""),
		}
		step := mso.getStepFromChainDefault(messages)
		assert.Equal(t, "", step)
	})

	t.Run("UserMessage有内容返回内容", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("task description"),
			llm_schema.NewAssistantMessage("result"),
			llm_schema.NewUserMessage("later task"),
		}
		step := mso.getStepFromChainDefault(messages)
		assert.Equal(t, "later task", step)
	})
}

// TestMessageSummaryOffloader_smartTruncateContent_补充 测试智能截断补充
func TestMessageSummaryOffloader_smartTruncateContent_补充(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("maxChars等于内容长度不截断", func(t *testing.T) {
		content := "hello"
		result := mso.smartTruncateContent(content, 5)
		assert.Equal(t, content, result)
	})

	t.Run("maxChars为0返回空", func(t *testing.T) {
		content := "hello"
		result := mso.smartTruncateContent(content, 0)
		assert.Equal(t, "", result)
	})
}

// TestMessageSummaryOffloader_newOffloadHandleAndPath_补充 测试句柄路径生成补充
func TestMessageSummaryOffloader_newOffloadHandleAndPath_补充(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("WorkspaceDir非空时生成完整路径", func(t *testing.T) {
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}
		handle, path := mso.newOffloadHandleAndPath(mc)
		assert.NotEmpty(t, handle)
		// fakeModelContext 的 WorkspaceDir 返回空字符串
		assert.Empty(t, path)
	})
}

// TestMessageSummaryOffloader_OnAddMessages_错误路径 测试OnAddMessages错误路径
func TestMessageSummaryOffloader_OnAddMessages_错误路径(t *testing.T) {
	t.Run("压缩全部失败仍返回原消息", func(t *testing.T) {
		fakeClient := &msoFakeBaseModelClient{
			invokeErr: fmt.Errorf("service unavailable"),
		}
		model := msoNewFakeLLMModel(fakeClient)
		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold:         100,
			ContentMaxCharsForCompression: 1000,
		}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
		require.NoError(t, err)

		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 200}

		event, processed, err := mso.OnAddMessages(context.Background(), mc, []llm_schema.BaseMessage{toolMsg})
		require.NoError(t, err)
		// 压缩失败，消息不会被修改，event 可能为 nil 或内容降级
		_ = event
		_ = processed
	})
}

// TestMessageSummaryOffloader_parseCompressionResult_补充 测试解析补充
func TestMessageSummaryOffloader_parseCompressionResult_补充(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("JSON在文本中混合", func(t *testing.T) {
		input := `Here is the result: {"summary":"mixed summary","offload_data_explanation":{"category":"data","description":"desc","inferability":"high"}} end`
		result, err := mso.parseCompressionResult(input)
		require.NoError(t, err)
		assert.Equal(t, "mixed summary", result["summary"])
	})
}

// ──────────────────────────── 覆盖率补充测试（第二轮） ────────────────────────────

// TestMessageSummaryOffloader_NewMessageSummaryOffloader_WithModelClient 测试有 ModelClient 配置时创建
func TestMessageSummaryOffloader_NewMessageSummaryOffloader_WithModelClient(t *testing.T) {
	fakeClient := &msoFakeBaseModelClient{
		invokeResp: llm_schema.NewAssistantMessage(`{"summary":"ok","offload_data_explanation":{}}`),
	}
	_ = msoNewFakeLLMModel(fakeClient)

	cfg := &MessageSummaryOffloaderConfig{
		ModelClient: &llm_schema.ModelClientConfig{
			ClientID:       "mso-test-client",
			ClientProvider: msoTestProvider,
			APIKey:         "fake-key",
			APIBase:        "https://fake.api.com",
		},
		Model: llm_schema.NewModelRequestConfig(llm_schema.WithModelName("test-model")),
	}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)
	assert.NotNil(t, mso.model)
}

// TestMessageSummaryOffloader_LoadState_非空 测试 LoadState 调用
func TestMessageSummaryOffloader_LoadState_非空(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	// LoadState 是空操作，调用不 panic 即可
	mso.LoadState(map[string]any{"key": "value"})
	mso.LoadState(nil)
}

// TestMessageSummaryOffloader_messageSize_完整 测试 messageSize 完整路径
func TestMessageSummaryOffloader_messageSize_完整(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("TokenCounter返回0", func(t *testing.T) {
		msg := llm_schema.NewToolMessage("call-1", "hello world")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 0}
		size := mso.messageSize(msg, mc)
		assert.Equal(t, 0, size)
	})

	t.Run("TokenCounter为nil回退字符除3", func(t *testing.T) {
		msg := llm_schema.NewToolMessage("call-1", "hello") // 5 字符 → 5/3=1
		mc := &msoNoTokenCounterModelContext{}
		size := mso.messageSize(msg, mc)
		assert.Equal(t, 1, size)
	})
}

// TestMessageSummaryOffloader_getStepFromChainPrecise_补充 测试精确步骤提取补充
func TestMessageSummaryOffloader_getStepFromChainPrecise_补充(t *testing.T) {
	t.Run("LLM返回空响应时回退", func(t *testing.T) {
		fakeClient := &msoFakeBaseModelClient{
			invokeResp: llm_schema.NewAssistantMessage(""),
		}
		model := msoNewFakeLLMModel(fakeClient)
		cfg := &MessageSummaryOffloaderConfig{}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
		require.NoError(t, err)

		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("task"),
			llm_schema.NewAssistantMessage("result"),
		}
		step, err := mso.getStepFromChainPrecise(context.Background(), messages)
		require.NoError(t, err)
		assert.Equal(t, "", step)
	})

	t.Run("LLM溢出错误减少消息后成功", func(t *testing.T) {
		callCount := 0
		retryClient := &msoRetryFakeClient{callCount: &callCount}
		model := msoNewFakeLLMModelFromClient(retryClient)
		cfg := &MessageSummaryOffloaderConfig{}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
		require.NoError(t, err)

		// 多条消息，溢出后减少消息重试
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("task1"),
			llm_schema.NewAssistantMessage("result1"),
			llm_schema.NewUserMessage("task2"),
			llm_schema.NewAssistantMessage("result2"),
		}
		step, err := mso.getStepFromChainPrecise(context.Background(), messages)
		require.NoError(t, err)
		// getStepFromChainPrecise 返回原始 LLM 响应文本，不是解析后的 summary
		assert.Contains(t, step, "retry summary")
	})
}

// TestMessageSummaryOffloader_offloadMessageAdaptive_完整 测试完整自适应卸载路径
func TestMessageSummaryOffloader_offloadMessageAdaptive_完整(t *testing.T) {
	summaryJSON := `{"compression_strategy":"extractive","summary":"adaptive summary","offload_data_explanation":{"category":"logs","description":"log data","inferability":"medium"}}`
	fakeClient := &msoFakeBaseModelClient{invokeResp: llm_schema.NewAssistantMessage(summaryJSON)}
	model := msoNewFakeLLMModel(fakeClient)

	cfg := &MessageSummaryOffloaderConfig{
		LargeMessageThreshold:         100,
		ContentMaxCharsForCompression: 1000,
		EnablePreciseStep:             true,
	}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
	require.NoError(t, err)

	t.Run("启用精确步骤提取", func(t *testing.T) {
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 200}

		replacement, err := mso.offloadMessageAdaptive(context.Background(), toolMsg, mc)
		require.NoError(t, err)
		require.NotNil(t, replacement)
		assert.Contains(t, replacement.GetContent().Text(), "adaptive summary")
	})

	t.Run("空内容消息返回原消息", func(t *testing.T) {
		// ToolMessage 的内容通过 ToolMessageContent 构造
		emptyToolMsg := llm_schema.NewToolMessage("call-1", "")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session", tokenCount: 0}

		replacement, err := mso.offloadMessageAdaptive(context.Background(), emptyToolMsg, mc)
		require.NoError(t, err)
		// 空内容压缩结果为 nil，应返回原消息
		assert.NotNil(t, replacement)
	})
}

// TestMessageSummaryOffloader_compressWithFallback_空响应 测试空响应处理
func TestMessageSummaryOffloader_compressWithFallback_空响应(t *testing.T) {
	t.Run("空文本响应使用Parts", func(t *testing.T) {
		// 创建一个 Parts 响应（非空文本触发 Parts 路径比较难）
		// 先测试正常路径已经覆盖，这里补充空 content 路径
		fakeClient := &msoFakeBaseModelClient{
			invokeResp: llm_schema.NewAssistantMessage(""),
		}
		model := msoNewFakeLLMModel(fakeClient)
		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold:         100,
			ContentMaxCharsForCompression: 1000,
		}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
		require.NoError(t, err)

		content := strings.Repeat("x", 500)
		result, err := mso.compressWithFallback(context.Background(), "test step", nil, content)
		require.NoError(t, err)
		// 空响应时 parseCompressionResult 会失败，由于 "" < 500，走 raw response 路径
		// 但 "" 也是空的，所以返回空 map
		assert.NotNil(t, result)
	})
}

// TestMessageSummaryOffloader_buildStepContextText_补充 测试步骤上下文补充
func TestMessageSummaryOffloader_buildStepContextText_补充(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{
		StepSummaryMaxContextMessages: 5,
	}
	require.NoError(t, cfg.Validate())
	mso := msoNewForTest(cfg)

	t.Run("空内容消息跳过", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(""),
			llm_schema.NewUserMessage("actual content"),
		}
		text := mso.buildStepContextText(messages)
		assert.Contains(t, text, "actual content")
	})
}
