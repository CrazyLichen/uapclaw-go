package deepseek

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent deepseek 包日志组件标识（AgentCore 层）。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// DeepSeekModelClient DeepSeek 模型客户端。
//
// 嵌入 OpenAIModelClient 复用 HTTP 请求/响应解析/SSE 等基础能力，
// 覆写 Invoke/Stream，在委托前为所有 assistant 消息补充 reasoning_content。
//
// DeepSeek API 兼容 OpenAI Chat Completion 协议，但对 reasoning_content 有额外要求：
//   - 有工具调用的多轮对话中，assistant 消息必须包含 reasoning_content 字段
//   - 不传会返回 400 错误
//   - 本客户端对所有 assistant 消息统一兜底补空字符串
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/deepseek_model_client.py (DeepSeekModelClient)
type DeepSeekModelClient struct {
	openai.OpenAIModelClient
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDeepSeekModelClient 创建 DeepSeek 客户端。
//
// 构造流程与 DashScope 客户端一致：
//  1. 先构造 OpenAI 客户端（复用 baseHeaders 初始化等）
//  2. 覆盖 clientName 为 "DeepSeek client"
//
// 对应 Python: DeepSeekModelClient.__init__(model_config, model_client_config)
func NewDeepSeekModelClient(
	modelConfig *llmschema.ModelRequestConfig,
	clientConfig *llmschema.ModelClientConfig,
) (*DeepSeekModelClient, error) {
	// 1. 构造 OpenAI 客户端（复用 baseHeaders 初始化等）
	openaiClient, err := openai.NewOpenAIModelClient(modelConfig, clientConfig)
	if err != nil {
		return nil, err
	}

	// 2. 覆盖 clientName 为 DeepSeek（OpenAI 构造函数设置的是 "OpenAI client"）
	embed, err := model_clients.NewBaseClientEmbed(
		modelConfig,
		clientConfig,
		model_clients.WithClientName("DeepSeek client"),
	)
	if err != nil {
		return nil, err
	}
	openaiClient.BaseClientEmbed = *embed

	return &DeepSeekModelClient{
		OpenAIModelClient: *openaiClient,
	}, nil
}

// Invoke 非流式调用 DeepSeek API。
//
// 覆写 OpenAI 客户端的 Invoke，在委托前为所有 assistant 消息补充 reasoning_content。
// DeepSeek API 要求有工具调用场景下 assistant 消息必须包含 reasoning_content 字段，
// 否则返回 400 错误。本方法对所有 assistant 消息统一兜底补空字符串。
//
// 对应 Python: DeepSeekModelClient 继承 OpenAIModelClient.invoke()
// （Python 通过动态分发自动走覆写的 _convert_messages_to_dict）
func (c *DeepSeekModelClient) Invoke(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	// 1. 预处理消息：基类转换 + 补充 reasoning_content
	enrichedMsgs, err := c.enrichMessagesWithReasoningContent(messages)
	if err != nil {
		return nil, err
	}

	// 2. 委托给 OpenAI 客户端（Dicts 模式直接透传，不会二次转换）
	return c.OpenAIModelClient.Invoke(ctx, enrichedMsgs, opts...)
}

// Stream 流式调用 DeepSeek API。
//
// 覆写 OpenAI 客户端的 Stream，在委托前为所有 assistant 消息补充 reasoning_content。
// 逻辑与 Invoke 一致，详见 Invoke 注释。
//
// 对应 Python: DeepSeekModelClient 继承 OpenAIModelClient.stream()
func (c *DeepSeekModelClient) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	// 1. 预处理消息：基类转换 + 补充 reasoning_content
	enrichedMsgs, err := c.enrichMessagesWithReasoningContent(messages)
	if err != nil {
		return nil, err
	}

	// 2. 委托给 OpenAI 客户端
	return c.OpenAIModelClient.Stream(ctx, enrichedMsgs, opts...)
}

// GenerateImage 生成图片（当前不支持）。
//
// DeepSeek Chat Completion API 不支持图片生成。
func (c *DeepSeekModelClient) GenerateImage(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateImageOption,
) (*llmschema.ImageGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("DeepSeek client does not support image generation"),
	)
}

// GenerateSpeech 生成语音（当前不支持）。
func (c *DeepSeekModelClient) GenerateSpeech(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateSpeechOption,
) (*llmschema.AudioGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("DeepSeek client does not support speech generation"),
	)
}

// GenerateVideo 生成视频（当前不支持）。
func (c *DeepSeekModelClient) GenerateVideo(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateVideoOption,
) (*llmschema.VideoGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("DeepSeek client does not support video generation"),
	)
}

// Release 释放模型缓存（当前不支持）。
//
// DeepSeek API 不支持 KV Cache 释放，仅 InferenceAffinity (vLLM) 客户端支持。
func (c *DeepSeekModelClient) Release(
	_ context.Context,
	_ ...model_clients.ReleaseOption,
) (bool, error) {
	return false, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("DeepSeek client does not support KV cache release"),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册 DeepSeek 客户端到全局注册表（2.6 回填点）。
func init() {
	registry := model_clients.GetClientRegistry()

	deepSeekFactory := func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) model_clients.BaseModelClient {
		client, err := NewDeepSeekModelClient(mc, cc)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("创建 DeepSeek 客户端失败")
			return nil
		}
		return client
	}

	registry.Register("DeepSeek", "llm", deepSeekFactory)
}

// enrichMessagesWithReasoningContent 为所有 assistant 消息补充 reasoning_content 空字符串。
//
// DeepSeek API 要求：有工具调用场景下，所有 role=assistant 的消息必须包含 reasoning_content
// 字段，否则 API 返回 400 错误。本方法对所有 assistant 消息统一兜底补空字符串，
// 已有 reasoning_content 的不覆盖。
//
// 处理流程：
//  1. 先调用 OpenAI 基类的 ConvertMessagesToDict 做标准转换
//  2. 遍历结果，为所有 assistant 消息补充 reasoning_content
//  3. 包装为 Dicts 模式回传（Dicts 模式直接透传，零转换开销）
//
// 对应 Python: DeepSeekModelClient._convert_messages_to_dict()
func (c *DeepSeekModelClient) enrichMessagesWithReasoningContent(
	messages model_clients.MessagesParam,
) (model_clients.MessagesParam, error) {
	// 1. 先调用基类转换
	result, err := c.ConvertMessagesToDict(messages)
	if err != nil {
		return model_clients.MessagesParam{}, err
	}

	// 2. 为所有 assistant 消息补充 reasoning_content
	for _, msg := range result {
		if role, ok := msg["role"].(string); ok && role == "assistant" {
			if _, exists := msg["reasoning_content"]; !exists {
				msg["reasoning_content"] = ""
			}
		}
	}

	// 3. 包装为 Dicts 模式（直接透传，不二次转换）
	return model_clients.NewDictsMessagesParam(result), nil
}
