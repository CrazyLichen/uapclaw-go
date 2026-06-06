package intellirouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

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
// 动态替换 api_key/api_base 为选中端点的配置。
// Stream 独立实现（不委托 OpenAI.Stream），对齐 Python IntelliRouter 无 _astream_with_parser
// （OutputParser 传 nil，不支持 parser）。
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

	// 对齐 Python: IntelliRouter 构造使用 llm_logger.info，非回调
	logger.Info(logComponent).
		Str("model_name", "IntelliRouter").
		Str("client_name", "IntelliRouter client").
		Int("num_deployments", len(irConfig.Deployments)).
		Str("strategy", irConfig.Strategy).
		Msg("IntelliRouter client created.")

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

		// 对齐 Python: 路由选择是内部逻辑，使用日志记录，非回调
		logger.Info(logComponent).
			Str("model_name", modelName).
			Str("model_provider", "IntelliRouter").
			Str("deployment_id", dep.ID).
			Str("api_base", dep.APIBase).
			Int("attempt", attempt).
			Msg("IntelliRouter invoking deployment.")

		// 3. 委托给 OpenAI.Invoke()
		start := time.Now()
		result, err := c.OpenAIModelClient.Invoke(ctx, messages, opts...)
		if err != nil {
			c.router.RecordFailure(dep)
			lastErr = err

			logger.Error(logComponent).
				Str("model_name", modelName).
				Str("model_provider", "IntelliRouter").
				Str("deployment_id", dep.ID).
				Int("attempt", attempt).
				Err(err).
				Msg("IntelliRouter deployment invoke failed.")

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
// 独立实现 Stream，不委托给 OpenAI 客户端。
// 对齐 Python IntelliRouterModelClient：IntelliRouter 没有 _astream_with_parser，
// 因此不支持 OutputParser。使用自己的 convertChunk 只提取 content。
//
// 与 OpenAI 的行为差异（对齐 Python）：
//   - convertChunk 只提取 content（不提取 reasoning_content / tool_calls / usage_metadata / finish_reason）
//   - 不使用 OutputParser（对齐 Python IntelliRouter 无 _astream_with_parser）
//
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

	// 对齐 Python: 路由选择是内部逻辑，使用日志记录，非回调
	logger.Info(logComponent).
		Str("model_name", modelName).
		Str("model_provider", "IntelliRouter").
		Str("deployment_id", dep.ID).
		Str("api_base", dep.APIBase).
		Msg("IntelliRouter streaming deployment.")

	// 3. 构建请求参数
	messagesDict, err := c.ConvertMessagesToDict(messages)
	if err != nil {
		return nil, err
	}
	reqParams, err := c.BuildRequestParams(ctx, messagesDict, params.ToStreamParams(), true)
	if err != nil {
		return nil, err
	}

	// 4. 设置 stream_options.include_usage = true
	streamOptions, _ := reqParams["stream_options"].(map[string]any)
	if streamOptions == nil {
		streamOptions = make(map[string]any)
	}
	streamOptions["include_usage"] = true
	reqParams["stream_options"] = streamOptions

	// 5. OpenAI 特有参数调整
	openai.AdjustParamsForOpenAI(reqParams, c.ClientConfig.APIBase)

	// 6. 合并 headers
	effectiveHeaders := c.OpenAIModelClient.BuildEffectiveHeaders(params.CustomHeaders)
	if len(effectiveHeaders) > 0 {
		reqParams["extra_headers"] = effectiveHeaders
	}

	// 7. 处理 extra_body
	openai.HandleExtraBody(reqParams)

	// 8. 构建 HTTP 请求
	httpHeaders := openai.ExtractHTTPHeaders(effectiveHeaders)
	req, client, err := openai.BuildHTTPRequest(
		ctx,
		c.ClientConfig.APIBase,
		c.ClientConfig.APIKey,
		reqParams,
		httpHeaders,
		params.Timeout,
		c.ClientConfig.VerifySSL,
		c.ClientConfig.SSLCert,
	)
	if err != nil {
		return nil, c.OpenAIModelClient.WrapError("stream", err)
	}

	// 9. 发送请求
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		c.router.RecordFailure(dep)
		// 对齐 Python: 路由层错误走日志，非回调
		logger.Error(logComponent).
			Str("model_name", modelName).
			Str("model_provider", "IntelliRouter").
			Str("deployment_id", dep.ID).
			Err(err).
			Msg("IntelliRouter stream request failed.")
		return nil, c.OpenAIModelClient.WrapError("stream", err)
	}

	// 10. 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		c.router.RecordFailure(dep)
		return nil, c.OpenAIModelClient.HandleHTTPError(resp)
	}

	// 11. 创建 SSE 读取器和 chunk channel
	sseReader := openai.NewSSEReader(resp.Body)
	chunkChan := make(chan *llmschema.AssistantMessageChunk, 64)

	// 12. 启动 goroutine 消费 SSE 流
	//     对齐 Python：IntelliRouter 无 _astream_with_parser，不使用 OutputParser
	//     使用自己的 convertChunk（只提取 content）
	go func() {
		defer close(chunkChan)
		defer resp.Body.Close()

		for {
			data, err := sseReader.ReadEvent()
			if err == io.EOF {
				return
			}
			if err != nil {
				// 对齐 Python: 路由层 SSE 错误走日志，非回调
				logger.Error(logComponent).
					Str("model_name", modelName).
					Str("model_provider", "IntelliRouter").
					Err(err).
					Msg("IntelliRouter stream SSE read error.")
				return
			}

			var chunkResp openai.ChatCompletionChunkResponse
			if err := json.Unmarshal([]byte(data), &chunkResp); err != nil {
				// 对齐 Python: JSON 解析错误走日志，非回调
				logger.Error(logComponent).
					Str("model_name", modelName).
					Str("model_provider", "IntelliRouter").
					Err(err).
					Msg("IntelliRouter stream JSON parse error.")
				continue
			}

			chunk := c.convertChunk(&chunkResp)
			if chunk == nil {
				continue
			}

			select {
			case chunkChan <- chunk:
			case <-ctx.Done():
				logger.Error(logComponent).
					Str("model_name", modelName).
					Str("model_provider", "IntelliRouter").
					Msg("IntelliRouter stream context cancelled.")
				return
			}
		}
	}()

	c.router.RecordSuccess(dep, time.Since(start))
	return model_clients.NewStreamResult(chunkChan), nil
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

// convertChunk 将 SSE JSON 块转换为 AssistantMessageChunk。
//
// 对齐 Python IntelliRouterModelClient._convert_chunk()：
// 只提取 content，不提取 reasoning_content / tool_calls / usage_metadata / finish_reason。
// IntelliRouter 不使用 OutputParser（无 _astream_with_parser）。
func (c *IntelliRouterModelClient) convertChunk(
	chunkResp *openai.ChatCompletionChunkResponse,
) *llmschema.AssistantMessageChunk {
	choices := chunkResp.Choices
	if len(choices) == 0 {
		return nil
	}

	delta := choices[0].Delta
	content := ""
	if delta != nil && delta.Content != nil {
		content = *delta.Content
	}

	// 对齐 Python: 空 content 也返回 chunk（Python 原样返回 content="" 的 chunk）
	return llmschema.NewAssistantMessageChunk(content)
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
