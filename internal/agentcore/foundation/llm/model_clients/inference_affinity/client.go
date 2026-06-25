package inferenceaffinity

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InferenceAffinityModelClient InferenceAffinity (vLLM) 模型客户端。
//
// 嵌入 OpenAIModelClient 复用 HTTP 请求/响应解析/SSE 等基础能力，
// 覆写 Invoke/Stream，在调用前对消息中的 tool_calls 做清洗（sanitize），
// 并注入 cache_sharing/cache_salt 参数以支持 vLLM KV Cache 共享。
// Stream 独立实现（不委托 OpenAI.Stream），对齐 Python InferenceAffinityModelClient._astream_with_parser。
//
// InferenceAffinity API 兼容 OpenAI Chat Completion 协议，但有以下特殊要求：
//   - tool_calls 中的 type 必须为 "function"，其他值会报错
//   - tool_calls 中不能包含非标准扩展字段，否则 API 报错
//   - 本客户端在发送请求前对 assistant 消息的 tool_calls 做清洗，只保留标准字段
//   - 支持 cache_sharing/cache_salt 参数，用于 vLLM KV Cache 共享
//   - 支持 Release() 方法，释放 vLLM KV Cache
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/inference_affinity_model_client.py (InferenceAffinityModelClient)
type InferenceAffinityModelClient struct {
	openai.OpenAIModelClient
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent inferenceaffinity 包日志组件标识（AgentCore 层）。
const logComponent = logger.ComponentAgentCore

// releaseKVCachePath vLLM KV Cache 释放 API 路径。
const releaseKVCachePath = "/release_kv_cache"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInferenceAffinityModelClient 创建 InferenceAffinity 客户端。
//
// 构造流程与 SiliconFlow/DeepSeek 客户端一致：
//  1. 先构造 OpenAI 客户端（复用 baseHeaders 初始化等）
//  2. 覆盖 clientName 为 "InferenceAffinity client"
//
// 对应 Python: InferenceAffinityModelClient.__init__(model_config, model_client_config)
func NewInferenceAffinityModelClient(
	modelConfig *llmschema.ModelRequestConfig,
	clientConfig *llmschema.ModelClientConfig,
) (*InferenceAffinityModelClient, error) {
	// 1. 构造 OpenAI 客户端（复用 baseHeaders 初始化等）
	openaiClient, err := openai.NewOpenAIModelClient(modelConfig, clientConfig)
	if err != nil {
		return nil, err
	}

	// 2. 覆盖 clientName 为 InferenceAffinity（OpenAI 构造函数设置的是 "OpenAI client"）
	embed, err := model_clients.NewBaseClientEmbed(
		modelConfig,
		clientConfig,
		model_clients.WithClientName("InferenceAffinity client"),
	)
	if err != nil {
		return nil, err
	}
	openaiClient.BaseClientEmbed = *embed

	return &InferenceAffinityModelClient{
		OpenAIModelClient: *openaiClient,
	}, nil
}

// Invoke 非流式调用 InferenceAffinity API。
//
// 覆写 OpenAI 客户端的 Invoke，在委托前对消息中的 tool_calls 做清洗，
// 并注入 cache_sharing/cache_salt 参数。
//
// 对应 Python: InferenceAffinityModelClient.invoke()
func (c *InferenceAffinityModelClient) Invoke(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	params := model_clients.NewInvokeParams(opts...)

	// 1. 预处理消息：基类转换 + sanitize tool_calls
	sanitizedMsgs, err := c.sanitizeMessages(messages)
	if err != nil {
		return nil, err
	}

	// 2. 注入 cache 参数
	cacheOpts := c.injectCacheOptions(params.Extra, opts)

	// 3. 委托给 OpenAI 客户端（Dicts 模式直接透传，不会二次转换）
	return c.OpenAIModelClient.Invoke(ctx, sanitizedMsgs, cacheOpts...)
}

// Stream 流式调用 InferenceAffinity API。
//
// 独立实现 Stream，不委托给 OpenAI 客户端。
// 对齐 Python InferenceAffinityModelClient._astream_with_parser：
// 使用自己的 parseStreamChunk 解析流式块。
//
// 与 OpenAI 的行为差异（对齐 Python）：
//   - 不设置 stream_options.include_usage（InferenceAffinity API 无此参数）
//   - 不保留无 choices 的 usage-only chunk（InferenceAffinity _parse_stream_chunk 会丢弃）
//   - 不提取 prompt_token_ids / completion_token_ids / logprobs
//   - usage 不包含费用信息（对齐 Python InferenceAffinity 不调用 _extract_cost_info）
//   - 空 content + 空 reasoning_content + 空 tool_calls 时返回 nil（丢弃）
//
// 对应 Python: InferenceAffinityModelClient.stream() + InferenceAffinityModelClient._astream_with_parser()
func (c *InferenceAffinityModelClient) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	params := model_clients.NewStreamParams(opts...)

	// 1. 预处理消息：基类转换 + sanitize tool_calls
	sanitizedMsgs, err := c.sanitizeMessages(messages)
	if err != nil {
		return nil, err
	}

	// 2. 注入 cache 参数并重建 params（确保 cache_sharing/cache_salt 包含在请求体中）
	cacheOpts := c.injectCacheStreamOptions(params.Extra, opts)
	params = model_clients.NewStreamParams(cacheOpts...)

	// 3. 构建请求参数
	messagesDict, err := c.ConvertMessagesToDict(sanitizedMsgs)
	if err != nil {
		return nil, err
	}
	reqParams, err := c.BuildRequestParams(ctx, messagesDict, params.ToStreamParams(), true)
	if err != nil {
		return nil, err
	}

	// 4. InferenceAffinity 不需要 OpenAI 特有的参数调整（不调用 AdjustParamsForOpenAI）
	// 5. InferenceAffinity 不设置 stream_options.include_usage（对齐 Python）

	// 6. 合并 headers
	effectiveHeaders := c.BuildEffectiveHeaders(params.CustomHeaders)
	if len(effectiveHeaders) > 0 {
		reqParams["extra_headers"] = effectiveHeaders
	}

	// 7. 处理 extra_body
	openai.HandleExtraBody(reqParams)

	// 7.5 对齐 Python: if tracer_record_data: await tracer_record_data(llm_params=params)
	// 请求发送前调用 tracer_record_data 回调，记录请求参数
	if params.TracerRecordData != nil {
		params.TracerRecordData(map[string]any{"llm_params": reqParams})
	}

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
		return nil, c.WrapError("stream", err)
	}

	// 触发 LLMInput 回调（对齐 Python trigger(LLM_INPUT)）
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMInput,
		ModelName:     fmt.Sprintf("%v", reqParams["model"]),
		ModelProvider: c.ClientConfig.ClientProvider,
		IsStream:      true,
	})

	// 9. 发送请求
	resp, err := client.Do(req)
	if err != nil {
		_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
			Event:         callback.LLMCallError,
			ModelName:     fmt.Sprintf("%v", reqParams["model"]),
			ModelProvider: c.ClientConfig.ClientProvider,
			IsStream:      true,
			Error:         err,
		})
		return nil, c.WrapError("stream", err)
	}

	// 10. 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, c.HandleHTTPError(resp)
	}

	// 11. 创建 SSE 读取器和 chunk channel
	sseReader := openai.NewSSEReader(resp.Body)
	chunkChan := make(chan *llmschema.AssistantMessageChunk, 64)

	// 12. 启动 goroutine 消费 SSE 流
	//     使用自己的 parseStreamChunk（对齐 Python InferenceAffinityModelClient._parse_stream_chunk）
	modelName := fmt.Sprintf("%v", reqParams["model"])
	go func() {
		defer close(chunkChan)
		defer func() { _ = resp.Body.Close() }()

		accumulatedContent := ""
		var finalMessage *llmschema.AssistantMessageChunk

		for {
			data, err := sseReader.ReadEvent()
			if err == io.EOF {
				// 对齐 Python: if tracer_record_data: await tracer_record_data(llm_response=final_message)
				if params.TracerRecordData != nil {
					params.TracerRecordData(map[string]any{"llm_response": finalMessage})
				}
				// 对齐 Python: 流结束时触发 LLMOutput 回调
				_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
					Event:         callback.LLMOutput,
					ModelName:     modelName,
					ModelProvider: c.ClientConfig.ClientProvider,
					IsStream:      true,
				})
				return
			}
			if err != nil {
				_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
					Event:         callback.LLMCallError,
					ModelName:     modelName,
					ModelProvider: c.ClientConfig.ClientProvider,
					IsStream:      true,
					Error:         err,
				})
				return
			}

			var chunkResp openai.ChatCompletionChunkResponse
			if err := json.Unmarshal([]byte(data), &chunkResp); err != nil {
				// 对齐 Python: JSON 解析错误走日志，非回调
				logger.Error(logComponent).
					Str("model_name", modelName).
					Str("model_provider", c.ClientConfig.ClientProvider).
					Err(err).
					Msg("Stream parser attempt error.")
				continue
			}

			chunk := c.parseStreamChunk(&chunkResp)
			if chunk == nil {
				continue
			}

			// 对齐 Python _astream_with_parser: 应用 output_parser
			if params.OutputParser != nil {
				if chunk.Content.Text() != "" {
					accumulatedContent += chunk.Content.Text()
				}
				if accumulatedContent != "" {
					parsed, parseErr := params.OutputParser.Parse(accumulatedContent)
					if parseErr == nil && parsed != nil {
						chunk.ParserContent = parsed
						accumulatedContent = "" // 清空缓冲区，增量输出
					} else if parseErr != nil {
						// 对齐 Python: parser 错误走 llm_logger.debug，非回调
						logger.Error(logComponent).
							Str("model_name", modelName).
							Str("model_provider", c.ClientConfig.ClientProvider).
							Err(parseErr).
							Msg("Stream parser attempt error.")
					}
				}
			}

			// 对齐 OpenAI: 逐 chunk 触发 LLMResponseReceived 回调
			_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
				Event:         callback.LLMResponseReceived,
				ModelName:     modelName,
				ModelProvider: c.ClientConfig.ClientProvider,
				IsStream:      true,
			})
			// 对齐 Python: final_message = final_message + parsed_chunk
			if finalMessage == nil {
				finalMessage = chunk
			} else {
				finalMessage = finalMessage.Merge(chunk)
			}

			select {
			case chunkChan <- chunk:
			case <-ctx.Done():
				_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
					Event:         callback.LLMCallError,
					ModelName:     modelName,
					ModelProvider: c.ClientConfig.ClientProvider,
					IsStream:      true,
				})
				return
			}
		}
	}()

	return chunkChan, nil
}

// Release 释放 vLLM KV Cache。
//
// 调用 {api_base}/release_kv_cache 接口，释放指定会话的 KV Cache。
// 对齐 Python: 200 响应即使非 JSON 也返回 true；非 200 返回 false；异常返回 error。
//
// 对应 Python: InferenceAffinityModelClient.release()
func (c *InferenceAffinityModelClient) Release(
	ctx context.Context,
	opts ...model_clients.ReleaseOption,
) (bool, error) {
	params := model_clients.NewReleaseParams(opts...)

	// 1. 构建请求体
	releaseBody, err := c.buildReleaseRequestBody(params)
	if err != nil {
		return false, err
	}

	// 2. 构建 HTTP 请求
	apiURL := strings.TrimRight(c.ClientConfig.APIBase, "/") + releaseKVCachePath
	bodyBytes, err := json.Marshal(releaseBody)
	if err != nil {
		return false, fmt.Errorf("序列化 release 请求体失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return false, fmt.Errorf("创建 release HTTP 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.ClientConfig.APIKey)

	// 3. 构建 HTTP 客户端
	client, err := c.buildReleaseHTTPClient(nil)
	if err != nil {
		return false, fmt.Errorf("构建 HTTP 客户端失败: %w", err)
	}

	// 4. 发送请求（对齐 Python: release 使用 llm_logger，非回调）
	logger.Info(logComponent).
		Str("model_name", params.Model).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Str("session_id", params.SessionID).
		Int("messages_released_index", params.MessagesReleasedIndex).
		Msg("Releasing KV cache.")

	resp, err := client.Do(req)
	if err != nil {
		logger.Error(logComponent).
			Str("model_name", params.Model).
			Str("model_provider", c.ClientConfig.ClientProvider).
			Str("session_id", params.SessionID).
			Err(err).
			Msg("KV cache release request failed.")
		return false, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("Release error: %s", err.Error())),
		)
	}
	defer func() { _ = resp.Body.Close() }()

	// 5. 处理响应
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		logger.Info(logComponent).
			Str("model_name", params.Model).
			Str("model_provider", c.ClientConfig.ClientProvider).
			Str("session_id", params.SessionID).
			Msg("KV cache release successful.")
		return true, nil
	}

	// 非 200 响应
	logger.Error(logComponent).
		Str("model_name", params.Model).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Str("session_id", params.SessionID).
		Int("status_code", resp.StatusCode).
		Str("response_body", string(respBody)).
		Msg("KV cache release failed.")
	return false, nil
}

// GenerateImage 生成图片（当前不支持）。
//
// InferenceAffinity Chat Completion API 不支持图片生成。
func (c *InferenceAffinityModelClient) GenerateImage(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateImageOption,
) (*llmschema.ImageGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("InferenceAffinity client does not support image generation"),
	)
}

// GenerateSpeech 生成语音（当前不支持）。
func (c *InferenceAffinityModelClient) GenerateSpeech(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateSpeechOption,
) (*llmschema.AudioGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("InferenceAffinity client does not support speech generation"),
	)
}

// GenerateVideo 生成视频（当前不支持）。
func (c *InferenceAffinityModelClient) GenerateVideo(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateVideoOption,
) (*llmschema.VideoGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("InferenceAffinity client does not support video generation"),
	)
}

// SupportsKVCacheRelease InferenceAffinity 客户端支持 KV Cache 释放。
func (c *InferenceAffinityModelClient) SupportsKVCacheRelease() bool {
	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册 InferenceAffinity 客户端到全局注册表（2.6 回填点）。
func init() {
	registry := model_clients.GetClientRegistry()

	inferenceAffinityFactory := func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) model_clients.BaseModelClient {
		client, err := NewInferenceAffinityModelClient(mc, cc)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("创建 InferenceAffinity 客户端失败")
			return nil
		}
		return client
	}

	registry.Register("InferenceAffinity", "llm", inferenceAffinityFactory)
}

// parseStreamChunk 将 SSE JSON 块转换为 AssistantMessageChunk。
//
// 对齐 Python InferenceAffinityModelClient._parse_stream_chunk()，
// 与 OpenAI 的 ParseStreamChunk 有以下差异：
//   - 不保留无 choices 的 usage-only chunk（返回 nil，丢弃）
//   - 不提取 prompt_token_ids / completion_token_ids / logprobs
//   - usage 不包含费用信息（对齐 Python InferenceAffinity 不调用 _extract_cost_info）
//   - 空 content + 空 reasoning_content + 空 tool_calls 时返回 nil（丢弃）
func (c *InferenceAffinityModelClient) parseStreamChunk(
	chunkResp *openai.ChatCompletionChunkResponse,
) *llmschema.AssistantMessageChunk {
	// 对齐 Python: 无 choices 时直接返回 nil（丢弃 usage-only chunk）
	if len(chunkResp.Choices) == 0 {
		return nil
	}

	choice := chunkResp.Choices[0]
	delta := choice.Delta

	// 提取 content
	content := ""
	if delta != nil && delta.Content != nil {
		content = *delta.Content
	}

	// 提取 reasoning_content
	var reasoningContent string
	if delta != nil && delta.ReasoningContent != nil {
		reasoningContent = *delta.ReasoningContent
	}

	// 解析 tool_calls delta
	var toolCalls []*llmschema.ToolCall
	if delta != nil && len(delta.ToolCalls) > 0 {
		for _, tcDelta := range delta.ToolCalls {
			index := 0
			if tcDelta.Index != nil {
				index = *tcDelta.Index
			}
			toolCalls = append(toolCalls, llmschema.NewToolCall(
				tcDelta.ID,
				tcDelta.Function.Name,
				tcDelta.Function.Arguments,
				llmschema.WithToolCallIndex(index),
				llmschema.WithToolCallType(tcDelta.Type),
			))
		}
	}

	// 提取 finish_reason
	finishReason := llmschema.FinishReasonNull
	if choice.FinishReason != nil && *choice.FinishReason != "" {
		finishReason = *choice.FinishReason
	}

	// 对齐 Python InferenceAffinity: 空 content + 空 reasoning + 空 tool_calls → 丢弃
	// 但如果有 finish_reason，仍需保留（Python 丢弃是已知行为，Go 保留 finish_reason）
	if content == "" && reasoningContent == "" && len(toolCalls) == 0 && finishReason == llmschema.FinishReasonNull {
		return nil
	}

	// 提取 usage（不含费用信息，对齐 Python InferenceAffinity 不调用 _extract_cost_info）
	var usageMetadata *llmschema.UsageMetadata
	if chunkResp.Usage != nil {
		usageMetadata = buildInferenceAffinityUsageMetadata(chunkResp.Usage, c.ModelConfig.ModelName)
	}

	opts := []llmschema.AssistantMessageChunkOption{
		llmschema.WithChunkFinishReason(finishReason),
	}
	if len(toolCalls) > 0 {
		opts = append(opts, llmschema.WithChunkToolCalls(toolCalls))
	}
	if usageMetadata != nil {
		opts = append(opts, llmschema.WithChunkUsageMetadata(usageMetadata))
	}
	if reasoningContent != "" {
		opts = append(opts, llmschema.WithChunkReasoningContent(reasoningContent))
	}

	return llmschema.NewAssistantMessageChunk(content, opts...)
}

// buildInferenceAffinityUsageMetadata 构建 InferenceAffinity 的 usage 元数据。
//
// 对齐 Python InferenceAffinityModelClient: 仅包含 token 数，不包含费用信息，
// 不包含 cache_tokens（对齐 Python InferenceAffinity._parse_stream_chunk 中的 usage 构建）。
func buildInferenceAffinityUsageMetadata(
	usage *openai.ResponseUsage,
	modelName string,
) *llmschema.UsageMetadata {
	meta := llmschema.NewUsageMetadata()
	meta.ModelName = modelName
	meta.InputTokens = usage.PromptTokens
	meta.OutputTokens = usage.CompletionTokens
	meta.TotalTokens = usage.TotalTokens
	return meta
}

// sanitizeMessages 对消息做预处理：先调用基类转换，再清洗 tool_calls。
//
// 处理流程：
//  1. 先调用 OpenAI 基类的 ConvertMessagesToDict 做标准转换
//  2. 对转换后的消息做 sanitizeToolCalls（只保留标准字段，强制 type="function"）
//  3. 包装为 Dicts 模式回传（Dicts 模式直接透传，零转换开销）
//
// 对应 Python: InferenceAffinityModelClient._build_and_sanitize_params()
func (c *InferenceAffinityModelClient) sanitizeMessages(
	messages model_clients.MessagesParam,
) (model_clients.MessagesParam, error) {
	// 1. 先调用基类转换
	result, err := c.ConvertMessagesToDict(messages)
	if err != nil {
		return model_clients.MessagesParam{}, err
	}

	// 2. 对消息做 sanitize tool_calls
	c.sanitizeToolCalls(result)

	// 3. 包装为 Dicts 模式（直接透传，不二次转换）
	return model_clients.NewDictsMessagesParam(result), nil
}

// injectCacheOptions 注入 cache_sharing/cache_salt 参数到 Invoke 选项。
//
// 从 Extra 中读取 session_id 和 enable_cache_sharing，
// 满足条件时注入 cache_sharing=true 和 cache_salt=session_id。
//
// 对应 Python: InferenceAffinityModelClient._build_and_sanitize_params() 中的 cache 逻辑
func (c *InferenceAffinityModelClient) injectCacheOptions(
	extra map[string]any,
	originalOpts []model_clients.InvokeOption,
) []model_clients.InvokeOption {
	// 复制原始选项
	result := make([]model_clients.InvokeOption, 0, len(originalOpts)+1)
	result = append(result, originalOpts...)

	// 检查是否需要注入 cache 参数
	enableCacheSharing, _ := extra["enable_cache_sharing"].(bool)
	sessionID, _ := extra["session_id"].(string)

	if enableCacheSharing && sessionID != "" {
		// 合并 cache 参数到 Extra
		mergedExtra := make(map[string]any)
		for k, v := range extra {
			mergedExtra[k] = v
		}
		mergedExtra["cache_sharing"] = true
		mergedExtra["cache_salt"] = sessionID
		result = append(result, model_clients.WithInvokeExtra(mergedExtra))
	}

	return result
}

// injectCacheStreamOptions 注入 cache_sharing/cache_salt 参数到 Stream 选项。
//
// 逻辑与 injectCacheOptions 一致，但操作 StreamOption。
func (c *InferenceAffinityModelClient) injectCacheStreamOptions(
	extra map[string]any,
	originalOpts []model_clients.StreamOption,
) []model_clients.StreamOption {
	// 复制原始选项
	result := make([]model_clients.StreamOption, 0, len(originalOpts)+1)
	result = append(result, originalOpts...)

	// 检查是否需要注入 cache 参数
	enableCacheSharing, _ := extra["enable_cache_sharing"].(bool)
	sessionID, _ := extra["session_id"].(string)

	if enableCacheSharing && sessionID != "" {
		// 合并 cache 参数到 Extra
		mergedExtra := make(map[string]any)
		for k, v := range extra {
			mergedExtra[k] = v
		}
		mergedExtra["cache_sharing"] = true
		mergedExtra["cache_salt"] = sessionID
		result = append(result, model_clients.WithStreamExtra(mergedExtra))
	}

	return result
}

// buildReleaseRequestBody 构建 Release 请求体。
//
// 对应 Python: InferenceAffinityModelClient.release() 中的 release_params 构建
func (c *InferenceAffinityModelClient) buildReleaseRequestBody(
	params *model_clients.ReleaseParams,
) (map[string]any, error) {
	// 1. 确定模型名称
	model := params.Model
	if model == "" && c.ModelConfig != nil {
		model = c.ModelConfig.ModelName
	}

	// 2. 转换并清洗消息
	var messagesDict []map[string]any
	if !params.Messages.IsEmpty() {
		converted, err := c.ConvertMessagesToDict(params.Messages)
		if err != nil {
			return nil, err
		}
		messagesDict = converted
		// 清洗 tool_calls
		c.sanitizeToolCalls(messagesDict)
	}

	// 3. 构建请求体
	releaseBody := map[string]any{
		"model":                   model,
		"cache_salt":              params.SessionID,
		"cache_sharing":           true,
		"messages":                messagesDict,
		"messages_released_index": params.MessagesReleasedIndex,
	}

	// 4. 添加可选的 tools
	if len(params.Tools) > 0 {
		releaseBody["tools"] = c.ConvertToolsToDict(params.Tools)
	}

	if params.ToolsReleasedIndex != nil {
		releaseBody["tools_released_index"] = *params.ToolsReleasedIndex
	}

	return releaseBody, nil
}

// buildReleaseHTTPClient 构建 Release 请求用的 HTTP 客户端。
//
// 复用与 OpenAI 客户端一致的 SSL/代理/超时配置。
func (c *InferenceAffinityModelClient) buildReleaseHTTPClient(
	timeout *float64,
) (*http.Client, error) {
	transport := &http.Transport{}

	// SSL 配置
	if !c.ClientConfig.VerifySSL {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else if c.ClientConfig.SSLCert != "" {
		caCert, err := os.ReadFile(c.ClientConfig.SSLCert)
		if err != nil {
			return nil, fmt.Errorf("读取 SSL 证书失败: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("解析 SSL 证书失败: %s", c.ClientConfig.SSLCert)
		}
		transport.TLSClientConfig = &tls.Config{
			RootCAs: caCertPool,
		}
	}

	// 代理配置：优先环境变量
	transport.Proxy = http.ProxyFromEnvironment

	// 构建超时时间
	clientTimeout := 60.0 // 默认 60 秒
	if c.ClientConfig.Timeout > 0 {
		clientTimeout = c.ClientConfig.Timeout
	}
	if timeout != nil && *timeout > 0 {
		clientTimeout = *timeout
	}

	return &http.Client{
		Transport: transport,
		Timeout:   time.Duration(clientTimeout * float64(time.Second)),
	}, nil
}

// sanitizeToolCalls 清洗消息中的 tool_calls，只保留 OpenAI 标准字段。
//
// InferenceAffinity (vLLM) API 对请求中的非标准字段严格，遇到不认识的字段会报错。
// 本函数对每个 assistant 消息的 tool_calls 做清洗：
//   - 只保留标准字段：id、type、index、function.name、function.arguments
//   - 强制 type 为 "function"（某些 LLM 返回的 type 可能为空或其他值）
//   - 移除非标准扩展字段（LLM 返回的原始 tool_calls 可能包含额外字段）
//
// 原地修改 messages 中的 tool_calls 字段。
//
// 对应 Python: InferenceAffinityModelClient._sanitize_tool_calls()
func (c *InferenceAffinityModelClient) sanitizeToolCalls(messages []map[string]any) {
	for _, msg := range messages {
		// 仅处理 assistant 消息
		if role, ok := msg["role"].(string); !ok || role != "assistant" {
			continue
		}

		// 获取 tool_calls 列表（支持 []map[string]any 和 []any 两种类型）
		// Go 中 map[string]any 存储的 slice 可能是 []map[string]any（如 ToOpenAIFormat 输出）
		// 或 []any（如 JSON 反序列化或 Dicts 模式手动构造），两种都需要处理
		var toolCalls []map[string]any
		switch tc := msg["tool_calls"].(type) {
		case []map[string]any:
			toolCalls = tc
		case []any:
			for _, item := range tc {
				if dict, ok := item.(map[string]any); ok {
					toolCalls = append(toolCalls, dict)
				}
			}
		default:
			continue
		}
		if len(toolCalls) == 0 {
			continue
		}

		cleaned := make([]map[string]any, 0, len(toolCalls))
		for _, tcDict := range toolCalls {
			// 跳过非 map 类型的 tool_call
			if tcDict == nil {
				continue
			}

			// 提取 function 部分
			funcPart, _ := tcDict["function"].(map[string]any)
			if funcPart == nil {
				funcPart = make(map[string]any)
			}
			name, _ := funcPart["name"].(string)
			args, _ := funcPart["arguments"].(string)

			// 只保留标准字段，强制 type="function"
			cleanedTC := map[string]any{
				"id":   tcDict["id"],
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": args,
				},
			}

			// 保留 index（如果存在）
			if idx, ok := tcDict["index"]; ok {
				cleanedTC["index"] = idx
			}

			cleaned = append(cleaned, cleanedTC)
		}
		msg["tool_calls"] = cleaned
	}
}
