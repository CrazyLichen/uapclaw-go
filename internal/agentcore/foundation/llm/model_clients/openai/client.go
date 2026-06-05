package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent openai 包统一使用 AgentCore 组件标识记录日志。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// OpenAIModelClient OpenAI 兼容协议的 LLM 模型客户端。
//
// 支持所有兼容 OpenAI Chat Completion API 的服务提供商，
// 包括 OpenAI 官方 API 和 OpenRouter 等第三方代理。
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/openai_model_client.py (OpenAIModelClient)
type OpenAIModelClient struct {
	model_clients.BaseClientEmbed
	// baseHeaders 预构建的配置级请求头
	baseHeaders map[string]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenAIModelClient 创建 OpenAI 兼容客户端。
//
// 对应 Python: OpenAIModelClient.__init__(model_config, model_client_config)
func NewOpenAIModelClient(
	modelConfig *llmschema.ModelRequestConfig,
	clientConfig *llmschema.ModelClientConfig,
) (*OpenAIModelClient, error) {
	embed, err := model_clients.NewBaseClientEmbed(
		modelConfig,
		clientConfig,
		model_clients.WithClientName("OpenAI client"),
	)
	if err != nil {
		return nil, err
	}

	// 预构建配置级 headers
	baseHeaders := SanitizeHeaders(clientConfig.CustomHeaders)

	// 对齐 Python P1: 创建客户端前记录配置参数
	finalTimeout := clientConfig.Timeout
	if finalTimeout <= 0 {
		finalTimeout = 60.0
	}
	logger.Info(logComponent).
		Str("event_type", "LLM_CALL_START").
		Float64("timeout", finalTimeout).
		Int("max_retries", clientConfig.MaxRetries).
		Msg("Before create openai client, model client config params ready.")

	return &OpenAIModelClient{
		BaseClientEmbed: *embed,
		baseHeaders:     baseHeaders,
	}, nil
}

// Invoke 非流式调用 LLM，返回完整的助手消息。
//
// 对应 Python: OpenAIModelClient.invoke()
func (c *OpenAIModelClient) Invoke(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	params := model_clients.NewInvokeParams(opts...)

	// 1. 转换消息格式
	messagesDict, err := c.ConvertMessagesToDict(messages)
	if err != nil {
		return nil, err
	}

	// 2. 构建请求参数
	reqParams, err := c.BuildRequestParams(messagesDict, params.ToInvokeParams(), false)
	if err != nil {
		return nil, err
	}

	// 3. OpenAI 特有参数调整
	AdjustParamsForOpenAI(reqParams, c.ClientConfig.APIBase)

	// 4. 合并 headers
	effectiveHeaders := c.buildEffectiveHeaders(params.CustomHeaders)
	if len(effectiveHeaders) > 0 {
		reqParams["extra_headers"] = effectiveHeaders
	}

	// 5. 处理 extra_body
	HandleExtraBody(reqParams)

	// 6. 构建 HTTP 请求
	httpHeaders := extractHTTPHeaders(effectiveHeaders)
	req, client, err := BuildHTTPRequest(
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
		return nil, c.wrapError("invoke", err)
	}

	// 7. 发送请求
	logger.Info(logComponent).
		Str("event_type", "LLM_CALL_START").
		Str("model_name", reqParams["model"].(string)).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Bool("is_stream", false).
		Msg("OpenAI API invoke request sent")

	resp, err := client.Do(req)
	if err != nil {
		// 对齐 Python P4: Invoke 错误记录完整上下文
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("model_name", reqParams["model"].(string)).
			Str("model_provider", c.ClientConfig.ClientProvider).
			Any("messages", reqParams["messages"]).
			Any("tools", reqParams["tools"]).
			Any("temperature", reqParams["temperature"]).
			Any("top_p", reqParams["top_p"]).
			Any("max_tokens", reqParams["max_tokens"]).
			Bool("is_stream", false).
			Err(err).
			Msg("OpenAI API async invoke error.")
		return nil, c.wrapError("invoke", err)
	}
	defer resp.Body.Close()

	// 8. 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		return nil, c.handleHTTPError(resp)
	}

	// 9. 解析响应
	var completionResp ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completionResp); err != nil {
		return nil, c.wrapError("invoke", fmt.Errorf("解析响应失败: %w", err))
	}

	// 对齐 Python P2: 收到响应记录完整上下文
	logger.Info(logComponent).
		Str("event_type", "LLM_CALL_END").
		Str("model_name", reqParams["model"].(string)).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Any("messages", reqParams["messages"]).
		Any("tools", reqParams["tools"]).
		Any("temperature", reqParams["temperature"]).
		Any("top_p", reqParams["top_p"]).
		Any("max_tokens", reqParams["max_tokens"]).
		Bool("is_stream", false).
		Msg("OpenAI API response received.")

	// 对齐 Python P3: 解析响应前记录 output_parser
	logger.Info(logComponent).
		Str("event_type", "LLM_CALL_END").
		Str("model_name", reqParams["model"].(string)).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Bool("is_stream", false).
		Str("output_parser", fmt.Sprintf("%v", params.OutputParser)).
		Msg("Before parse response with output parser.")

	// 10. 转换为 AssistantMessage
	assistantMsg, err := ParseResponse(&completionResp, c.ModelConfig, params.OutputParser)
	if err != nil {
		return nil, c.wrapError("invoke", err)
	}

	logger.Info(logComponent).
		Str("event_type", "LLM_CALL_END").
		Str("model_name", reqParams["model"].(string)).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Bool("is_stream", false).
		Msg("OpenAI API response parsed")

	return assistantMsg, nil
}

// Stream 流式调用 LLM，返回流式结果。
//
// 对应 Python: OpenAIModelClient.stream()
func (c *OpenAIModelClient) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (*model_clients.StreamResult, error) {
	params := model_clients.NewStreamParams(opts...)

	// 1. 转换消息格式
	messagesDict, err := c.ConvertMessagesToDict(messages)
	if err != nil {
		return nil, err
	}

	// 2. 构建请求参数
	reqParams, err := c.BuildRequestParams(messagesDict, params.ToStreamParams(), true)
	if err != nil {
		return nil, err
	}

	// 3. 设置 stream_options.include_usage = true
	streamOptions, _ := reqParams["stream_options"].(map[string]any)
	if streamOptions == nil {
		streamOptions = make(map[string]any)
	}
	streamOptions["include_usage"] = true
	reqParams["stream_options"] = streamOptions

	// 4. OpenAI 特有参数调整
	AdjustParamsForOpenAI(reqParams, c.ClientConfig.APIBase)

	// 5. 合并 headers
	effectiveHeaders := c.buildEffectiveHeaders(params.CustomHeaders)
	if len(effectiveHeaders) > 0 {
		reqParams["extra_headers"] = effectiveHeaders
	}

	// 6. 处理 extra_body
	HandleExtraBody(reqParams)

	// 7. 构建 HTTP 请求
	httpHeaders := extractHTTPHeaders(effectiveHeaders)
	req, client, err := BuildHTTPRequest(
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
		return nil, c.wrapError("stream", err)
	}

	logger.Info(logComponent).
		Str("event_type", "LLM_CALL_START").
		Str("model_name", reqParams["model"].(string)).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Bool("is_stream", true).
		Msg("OpenAI API stream request sent")

	// 8. 发送请求
	resp, err := client.Do(req)
	if err != nil {
		// 对齐 Python P5: Stream 错误记录完整上下文
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("model_name", reqParams["model"].(string)).
			Str("model_provider", c.ClientConfig.ClientProvider).
			Any("messages", reqParams["messages"]).
			Any("tools", reqParams["tools"]).
			Any("temperature", reqParams["temperature"]).
			Any("top_p", reqParams["top_p"]).
			Any("max_tokens", reqParams["max_tokens"]).
			Bool("is_stream", true).
			Err(err).
			Msg("OpenAI API async stream error.")
		return nil, c.wrapError("stream", err)
	}

	// 9. 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, c.handleHTTPError(resp)
	}

	// 10. 创建 SSE 读取器和 chunk channel
	sseReader := NewSSEReader(resp.Body)
	chunkChan := make(chan *llmschema.AssistantMessageChunk, 64)

	// 11. 启动 goroutine 消费 SSE 流
	go func() {
		defer close(chunkChan)
		defer resp.Body.Close()

		for {
			data, err := sseReader.ReadEvent()
			if err == io.EOF {
				// 流正常结束
				return
			}
			if err != nil {
				// 流读取错误，记录日志但不中断（后续 chunk 不会被消费）
				// 对齐 Python P5: Stream 错误记录完整上下文
				logger.Error(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("model_name", fmt.Sprintf("%v", reqParams["model"])).
					Str("model_provider", c.ClientConfig.ClientProvider).
					Any("messages", reqParams["messages"]).
					Any("tools", reqParams["tools"]).
					Any("temperature", reqParams["temperature"]).
					Any("top_p", reqParams["top_p"]).
					Any("max_tokens", reqParams["max_tokens"]).
					Bool("is_stream", true).
					Err(err).
					Msg("OpenAI API async stream error.")
				return
			}

			// 解析 JSON
			var chunkResp ChatCompletionChunkResponse
			if err := json.Unmarshal([]byte(data), &chunkResp); err != nil {
				logger.Warn(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("model_name", fmt.Sprintf("%v", reqParams["model"])).
					Str("model_provider", c.ClientConfig.ClientProvider).
					Bool("is_stream", true).
					Str("data", data).
					Err(err).
					Msg("OpenAI API stream chunk parse error")
				continue
			}

			// 转换为 AssistantMessageChunk
			chunk := ParseStreamChunk(&chunkResp, c.ModelConfig)
			if chunk == nil {
				continue
			}

			// 发送到 channel（支持 context 取消）
			select {
			case chunkChan <- chunk:
			case <-ctx.Done():
				logger.Warn(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("model_name", fmt.Sprintf("%v", reqParams["model"])).
					Str("model_provider", c.ClientConfig.ClientProvider).
					Bool("is_stream", true).
					Msg("OpenAI API stream cancelled by context")
				return
			}
		}
	}()

	return model_clients.NewStreamResult(chunkChan), nil
}

// GenerateImage 生成图片（当前不支持）。
//
// OpenAI Chat Completion API 不支持图片生成，图片生成走独立的 API 接口。
// 后续由 2.8 DashScope 等客户端各自实现。
func (c *OpenAIModelClient) GenerateImage(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateImageOption,
) (*llmschema.ImageGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("OpenAI client does not support image generation"),
	)
}

// GenerateSpeech 生成语音（当前不支持）。
func (c *OpenAIModelClient) GenerateSpeech(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateSpeechOption,
) (*llmschema.AudioGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("OpenAI client does not support speech generation"),
	)
}

// GenerateVideo 生成视频（当前不支持）。
func (c *OpenAIModelClient) GenerateVideo(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateVideoOption,
) (*llmschema.VideoGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("OpenAI client does not support video generation"),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册 OpenAI 和 OpenRouter 客户端到全局注册表。
//
// 对应 Python: OpenAIModelClient.__client_name__ = ["OpenAI", "OpenRouter"]
// 通过 __init_subclass__ 自动注册。
func init() {
	registry := model_clients.GetClientRegistry()

	// OpenAI 客户端工厂
	openAIFactory := func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) model_clients.BaseModelClient {
		client, err := NewOpenAIModelClient(mc, cc)
		if err != nil {
			// 注册阶段无法返回 error，记录日志并返回 nil
			// 实际使用时 CreateModelClient 会正常创建
			logger.Error(logComponent).Err(err).Msg("创建 OpenAI 客户端失败")
			return nil
		}
		return client
	}

	registry.Register("OpenAI", "llm", openAIFactory)
	registry.Register("OpenRouter", "llm", openAIFactory)
}

// buildEffectiveHeaders 合并配置级和请求级 headers。
func (c *OpenAIModelClient) buildEffectiveHeaders(requestHeaders map[string]any) map[string]string {
	// 清洗请求级 headers
	reqHeaders := SanitizeHeaders(requestHeaders)
	return MergeHeaders(c.baseHeaders, reqHeaders)
}

// wrapError 包装错误为 MODEL_CALL_FAILED 异常。
func (c *OpenAIModelClient) wrapError(method string, err error) error {
	errDetail := fmt.Sprintf("%T: %v", err, err)
	if err.Error() == "" {
		errDetail = fmt.Sprintf("%T", err)
	}
	return exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf("OpenAI API async %s error: %s", method, errDetail)),
	)
}

// handleHTTPError 处理非 200 HTTP 响应。
func (c *OpenAIModelClient) handleHTTPError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("OpenAI API HTTP %d (无法读取响应体)", resp.StatusCode)),
		)
	}

	// 尝试解析 OpenAI 错误格式
	var errResp ErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("OpenAI API HTTP %d: %s", resp.StatusCode, errResp.Error.Message)),
		)
	}

	return exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf("OpenAI API HTTP %d: %s", resp.StatusCode, string(body))),
	)
}

// extractHTTPHeaders 从 effective headers 提取用于 HTTP 请求的头部。
// extra_headers 是 OpenAI SDK 的概念，我们的 HTTP 请求直接设置头部。
func extractHTTPHeaders(effectiveHeaders map[string]string) map[string]string {
	if len(effectiveHeaders) == 0 {
		return nil
	}
	return effectiveHeaders
}
