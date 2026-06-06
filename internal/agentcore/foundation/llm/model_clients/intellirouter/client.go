package intellirouter

import (
	"context"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/callback"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IntelliRouterModelClient IntelliRouter 智能路由模型客户端。
//
// 嵌入 OpenAIModelClient 复用 HTTP 请求/响应解析/SSE 等基础能力，
// 覆写 Invoke/Stream，在调用时通过路由器选择部署端点，
// 动态替换 api_key/api_base 为选中端点的配置，然后委托给 OpenAI 客户端。
//
// Invoke 支持自动重试：失败时标记端点不健康并切换到下一个端点重试。
// Stream 不做重试（流一旦开始无法重试）。
//
// 对应 Python: IntelliRouterModelClient（intelli_router_model_client.py）
type IntelliRouterModelClient struct {
	openai.OpenAIModelClient
	// router 智能路由器实例
	router *ReliableRouter
	// config IntelliRouter 路由配置
	config *IntelliRouterClientConfig
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewIntelliRouterModelClient 创建 IntelliRouter 智能路由客户端。
//
// 构造流程：
//  1. 从 ModelClientConfig.Extra 提取 IntelliRouter 配置
//  2. 校验至少有一个 deployment
//  3. 获取或创建路由器（缓存共享）
//  4. 构造 BaseClientEmbed（WithSkipValidate 跳过 api_key/api_base 校验）
//  5. 构造 OpenAI 客户端（placeholder 配置，实际调用时动态替换）
//
// 对应 Python: IntelliRouterModelClient.__init__(model_config, model_client_config)
func NewIntelliRouterModelClient(
	modelConfig *llmschema.ModelRequestConfig,
	clientConfig *llmschema.ModelClientConfig,
) (*IntelliRouterModelClient, error) {
	// 1. 提取 IntelliRouter 配置
	irConfig := FromModelClientConfig(clientConfig)

	// 2. 校验至少有一个 deployment
	if len(irConfig.Deployments) == 0 {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg("IntelliRouter requires at least one deployment in intelli_router_deployments"),
		)
	}

	// 3. 获取或创建路由器（缓存共享）
	router := GetOrCreateRouter(irConfig)

	// 4. 构造 BaseClientEmbed（跳过 api_key/api_base 校验）
	embed, err := model_clients.NewBaseClientEmbed(
		modelConfig,
		clientConfig,
		model_clients.WithClientName("IntelliRouter client"),
		model_clients.WithSkipValidate(),
	)
	if err != nil {
		return nil, err
	}

	// 5. 构造 OpenAI 客户端（placeholder 配置，实际调用时动态替换 api_key/api_base）
	placeholderConfig := &llmschema.ModelClientConfig{
		ClientID:       clientConfig.ClientID,
		ClientProvider: clientConfig.ClientProvider,
		APIKey:         "placeholder",
		APIBase:        "http://placeholder",
		Timeout:        clientConfig.Timeout,
		MaxRetries:     clientConfig.MaxRetries,
		VerifySSL:      clientConfig.VerifySSL,
		SSLCert:        clientConfig.SSLCert,
		CustomHeaders:  clientConfig.CustomHeaders,
	}
	openaiClient, err := openai.NewOpenAIModelClient(modelConfig, placeholderConfig)
	if err != nil {
		return nil, err
	}
	openaiClient.BaseClientEmbed = *embed

	callback.GetCallbackFramework().Trigger(context.Background(), &callback.LLMCallEventData{
		Event:     callback.LLMCallStarted,
		ModelName: "IntelliRouter",
		Extra: map[string]any{
			"client_name":    "IntelliRouter client",
			"num_deployments": len(irConfig.Deployments),
			"strategy":       irConfig.Strategy,
		},
	})

	return &IntelliRouterModelClient{
		OpenAIModelClient: *openaiClient,
		router:            router,
		config:            irConfig,
	}, nil
}

// Invoke 非流式调用 IntelliRouter API。
//
// 覆写 OpenAI 客户端的 Invoke，通过路由器选择部署端点，
// 动态替换 api_key/api_base 后委托给 OpenAI.Invoke()。
// 支持自动重试：失败时标记端点不健康并切换到下一个端点重试。
//
// 对应 Python: IntelliRouterModelClient.invoke()
func (c *IntelliRouterModelClient) Invoke(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	params := model_clients.NewInvokeParams(opts...)
	modelName := params.Model
	if modelName == "" && c.ModelConfig != nil {
		modelName = c.ModelConfig.ModelName
	}

	var lastErr error
	maxRetries := c.router.numRetries

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// 1. 通过路由器选择部署端点
		dep, err := c.router.SelectDeployment(modelName)
		if err != nil {
			return nil, err
		}

		// 2. 动态替换 OpenAI 客户端的 api_key/api_base
		c.ClientConfig.APIKey = dep.APIKey
		c.ClientConfig.APIBase = dep.APIBase

		callback.GetCallbackFramework().Trigger(ctx, &callback.LLMCallEventData{
			Event:         callback.LLMCallStarted,
			ModelName:     modelName,
			ModelProvider: "IntelliRouter",
			IsStream:      false,
			Extra: map[string]any{
				"deployment_id": dep.ID,
				"api_base":      dep.APIBase,
				"attempt":       attempt,
			},
		})

		// 3. 委托给 OpenAI.Invoke()
		start := time.Now()
		result, err := c.OpenAIModelClient.Invoke(ctx, messages, opts...)
		if err != nil {
			c.router.RecordFailure(dep)
			lastErr = err

			callback.GetCallbackFramework().Trigger(ctx, &callback.LLMCallEventData{
				Event:         callback.LLMCallError,
				ModelName:     modelName,
				ModelProvider: "IntelliRouter",
				IsStream:      false,
				Error:         err,
				Extra: map[string]any{
					"deployment_id": dep.ID,
					"attempt":       attempt,
				},
			})

			continue // 重试下一个 deployment
		}

		c.router.RecordSuccess(dep, time.Since(start))
		return result, nil
	}

	// 所有重试都失败
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf("IntelliRouter: 所有部署端点均失败 (model=%s, retries=%d, last_error=%v)", modelName, maxRetries, lastErr)),
	)
}

// Stream 流式调用 IntelliRouter API。
//
// 覆写 OpenAI 客户端的 Stream，通过路由器选择部署端点，
// 动态替换 api_key/api_base 后委托给 OpenAI.Stream()。
// Stream 不做重试（流一旦开始无法重试）。
//
// 对应 Python: IntelliRouterModelClient.stream()
func (c *IntelliRouterModelClient) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (*model_clients.StreamResult, error) {
	params := model_clients.NewStreamParams(opts...)
	modelName := params.Model
	if modelName == "" && c.ModelConfig != nil {
		modelName = c.ModelConfig.ModelName
	}

	// 1. 通过路由器选择部署端点
	dep, err := c.router.SelectDeployment(modelName)
	if err != nil {
		return nil, err
	}

	// 2. 动态替换 api_key/api_base
	c.ClientConfig.APIKey = dep.APIKey
	c.ClientConfig.APIBase = dep.APIBase

	callback.GetCallbackFramework().Trigger(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMCallStarted,
		ModelName:     modelName,
		ModelProvider: "IntelliRouter",
		IsStream:      true,
		Extra: map[string]any{
			"deployment_id": dep.ID,
			"api_base":      dep.APIBase,
		},
	})

	// 3. 委托给 OpenAI.Stream()
	start := time.Now()
	result, err := c.OpenAIModelClient.Stream(ctx, messages, opts...)
	if err != nil {
		c.router.RecordFailure(dep)
		return nil, err
	}

	c.router.RecordSuccess(dep, time.Since(start))
	return result, nil
}

// GenerateImage 生成图片（当前不支持）。
//
// IntelliRouter 仅支持 Chat Completion，不支持图片生成。
func (c *IntelliRouterModelClient) GenerateImage(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateImageOption,
) (*llmschema.ImageGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("IntelliRouter client does not support image generation"),
	)
}

// GenerateSpeech 生成语音（当前不支持）。
func (c *IntelliRouterModelClient) GenerateSpeech(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateSpeechOption,
) (*llmschema.AudioGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("IntelliRouter client does not support speech generation"),
	)
}

// GenerateVideo 生成视频（当前不支持）。
func (c *IntelliRouterModelClient) GenerateVideo(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateVideoOption,
) (*llmschema.VideoGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("IntelliRouter client does not support video generation"),
	)
}

// Release 释放模型缓存（当前不支持）。
//
// IntelliRouter 不支持 KV Cache 释放，仅 InferenceAffinity (vLLM) 客户端支持。
func (c *IntelliRouterModelClient) Release(
	_ context.Context,
	_ ...model_clients.ReleaseOption,
) (bool, error) {
	return false, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("IntelliRouter client does not support KV cache release"),
	)
}

// GetRouterStats 获取路由器统计信息。
//
// 便捷方法，暴露内部路由器的统计信息供监控和调试使用。
func (c *IntelliRouterModelClient) GetRouterStats() map[string]any {
	return c.router.GetStats()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册 IntelliRouter 客户端到全局注册表（2.6 回填点）。
func init() {
	registry := model_clients.GetClientRegistry()

	intelliRouterFactory := func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) model_clients.BaseModelClient {
		client, err := NewIntelliRouterModelClient(mc, cc)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("创建 IntelliRouter 客户端失败")
			return nil
		}
		return client
	}

	registry.Register("intelli_router", "llm", intelliRouterFactory)
}
