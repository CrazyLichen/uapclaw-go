package siliconflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SiliconFlowModelClient SiliconFlow 模型客户端。
//
// 嵌入 OpenAIModelClient 复用 HTTP 请求/响应解析/SSE 等基础能力，
// 覆写 Invoke/Stream，在调用前对消息中的 tool_calls 做清洗（sanitize）。
// Stream 独立实现（不委托 OpenAI.Stream），对齐 Python SiliconFlowModelClient._astream_with_parser。
//
// SiliconFlow API 兼容 OpenAI Chat Completion 协议，但对请求中的非标准字段严格：
//   - tool_calls 中的 type 必须为 "function"，其他值会报错
//   - tool_calls 中不能包含非标准扩展字段，否则 API 报错
//   - 本客户端在发送请求前对 assistant 消息的 tool_calls 做清洗，只保留标准字段
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/siliconflow_model_client.py (SiliconFlowModelClient)
type SiliconFlowModelClient struct {
	openai.OpenAIModelClient
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent siliconflow 包日志组件标识（AgentCore 层）。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSiliconFlowModelClient 创建 SiliconFlow 客户端。
//
// 构造流程与 DeepSeek 客户端一致：
//  1. 先构造 OpenAI 客户端（复用 baseHeaders 初始化等）
//  2. 覆盖 clientName 为 "SiliconFlow client"
//
// 对应 Python: SiliconFlowModelClient.__init__(model_config, model_client_config)
func NewSiliconFlowModelClient(
	modelConfig *llmschema.ModelRequestConfig,
	clientConfig *llmschema.ModelClientConfig,
) (*SiliconFlowModelClient, error) {
	// 1. 构造 OpenAI 客户端（复用 baseHeaders 初始化等）
	openaiClient, err := openai.NewOpenAIModelClient(modelConfig, clientConfig)
	if err != nil {
		return nil, err
	}

	// 2. 覆盖 clientName 为 SiliconFlow（OpenAI 构造函数设置的是 "OpenAI client"）
	embed, err := model_clients.NewBaseClientEmbed(
		modelConfig,
		clientConfig,
		model_clients.WithClientName("SiliconFlow client"),
	)
	if err != nil {
		return nil, err
	}
	openaiClient.BaseClientEmbed = *embed

	return &SiliconFlowModelClient{
		OpenAIModelClient: *openaiClient,
	}, nil
}

// Invoke 非流式调用 SiliconFlow API。
//
// 覆写 OpenAI 客户端的 Invoke，在委托前对消息中的 tool_calls 做清洗。
// SiliconFlow API 对非标准字段严格，需要只保留标准字段并强制 type="function"。
//
// 对应 Python: SiliconFlowModelClient.invoke()
func (c *SiliconFlowModelClient) Invoke(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	// 1. 预处理消息：基类转换 + sanitize tool_calls
	sanitizedMsgs, err := c.sanitizeMessages(messages)
	if err != nil {
		return nil, err
	}

	// 2. 委托给 OpenAI 客户端（Dicts 模式直接透传，不会二次转换）
	return c.OpenAIModelClient.Invoke(ctx, sanitizedMsgs, opts...)
}

// Stream 流式调用 SiliconFlow API。
//
// 独立实现 Stream，不委托给 OpenAI 客户端。
// 对齐 Python SiliconFlowModelClient：使用自己的 parseStreamChunk 解析流式块。
//
// 与 OpenAI 的行为差异（对齐 Python）：
//   - 不设置 stream_options.include_usage（SiliconFlow API 无此参数）
//   - 不保留无 choices 的 usage-only chunk（SiliconFlow _parse_stream_chunk 会丢弃）
//   - 不提取 prompt_token_ids / completion_token_ids / logprobs
//   - usage 包含费用信息（对齐 Python SiliconFlow 调用 _extract_cost_info）
//
// 对应 Python: SiliconFlowModelClient.stream() + SiliconFlowModelClient._astream_with_parser()
func (c *SiliconFlowModelClient) Stream(
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

	// 2. 构建请求参数
	messagesDict, err := c.ConvertMessagesToDict(sanitizedMsgs)
	if err != nil {
		return nil, err
	}
	reqParams, err := c.BuildRequestParams(ctx, messagesDict, params.ToStreamParams(), true)
	if err != nil {
		return nil, err
	}

	// 3. SiliconFlow 不需要 OpenAI 特有的参数调整（不调用 AdjustParamsForOpenAI）
	// 4. SiliconFlow 不设置 stream_options.include_usage（对齐 Python）

	// 5. 合并 headers
	effectiveHeaders := c.BuildEffectiveHeaders(params.CustomHeaders)
	if len(effectiveHeaders) > 0 {
		reqParams["extra_headers"] = effectiveHeaders
	}

	// 6. 处理 extra_body
	openai.HandleExtraBody(reqParams)

	// 6.5 对齐 Python: if tracer_record_data: await tracer_record_data(llm_params=params)
	// 请求发送前调用 tracer_record_data 回调，记录请求参数
	if params.TracerRecordData != nil {
		params.TracerRecordData(map[string]any{"llm_params": reqParams})
	}

	// 7. 构建 HTTP 请求
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

	// 8. 发送请求
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

	// 9. 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, c.HandleHTTPError(resp)
	}

	// 10. 创建 SSE 读取器和 chunk channel
	sseReader := openai.NewSSEReader(resp.Body)
	chunkChan := make(chan *llmschema.AssistantMessageChunk, 64)

	// 11. 启动 goroutine 消费 SSE 流
	//     使用自己的 parseStreamChunk（对齐 Python SiliconFlowModelClient._parse_stream_chunk）
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

// GenerateImage 生成图片（当前不支持）。
//
// SiliconFlow Chat Completion API 不支持图片生成。
func (c *SiliconFlowModelClient) GenerateImage(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateImageOption,
) (*llmschema.ImageGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("SiliconFlow client does not support image generation"),
	)
}

// GenerateSpeech 生成语音（当前不支持）。
func (c *SiliconFlowModelClient) GenerateSpeech(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateSpeechOption,
) (*llmschema.AudioGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("SiliconFlow client does not support speech generation"),
	)
}

// GenerateVideo 生成视频（当前不支持）。
func (c *SiliconFlowModelClient) GenerateVideo(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateVideoOption,
) (*llmschema.VideoGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("SiliconFlow client does not support video generation"),
	)
}

// Release 释放模型缓存（当前不支持）。
//
// SiliconFlow API 不支持 KV Cache 释放，仅 InferenceAffinity (vLLM) 客户端支持。
func (c *SiliconFlowModelClient) Release(
	_ context.Context,
	_ ...model_clients.ReleaseOption,
) (bool, error) {
	return false, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("SiliconFlow client does not support KV cache release"),
	)
}

// SupportsKVCacheRelease SiliconFlow 客户端不支持 KV Cache 释放。
func (c *SiliconFlowModelClient) SupportsKVCacheRelease() bool {
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册 SiliconFlow 客户端到全局注册表（2.6 回填点）。
func init() {
	registry := model_clients.GetClientRegistry()

	siliconFlowFactory := func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) model_clients.BaseModelClient {
		client, err := NewSiliconFlowModelClient(mc, cc)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("创建 SiliconFlow 客户端失败")
			return nil
		}
		return client
	}

	registry.Register("SiliconFlow", "llm", siliconFlowFactory)
}

// parseStreamChunk 将 SSE JSON 块转换为 AssistantMessageChunk。
//
// 对齐 Python SiliconFlowModelClient._parse_stream_chunk()，
// 与 OpenAI 的 ParseStreamChunk 有以下差异：
//   - 不保留无 choices 的 usage-only chunk（返回 nil，丢弃）
//   - 不提取 prompt_token_ids / completion_token_ids / logprobs
//   - usage 包含费用信息（对齐 Python SiliconFlow 调用 _extract_cost_info）
//   - 空 content + 空 reasoning_content + 空 tool_calls 时返回 nil（丢弃）
func (c *SiliconFlowModelClient) parseStreamChunk(
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

	// 对齐 Python SiliconFlow: 空 content + 空 reasoning + 空 tool_calls → 丢弃
	// 但如果有 finish_reason，仍需保留（Python 丢弃是已知行为，Go 保留 finish_reason）
	if content == "" && reasoningContent == "" && len(toolCalls) == 0 && finishReason == llmschema.FinishReasonNull {
		return nil
	}

	// 提取 usage（含费用信息，对齐 Python SiliconFlow 调用 _extract_cost_info）
	var usageMetadata *llmschema.UsageMetadata
	if chunkResp.Usage != nil {
		usageMetadata = buildSiliconFlowUsageMetadata(chunkResp.Usage, c.ModelConfig.ModelName)
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

// buildSiliconFlowUsageMetadata 构建 SiliconFlow 的 usage 元数据。
//
// 对齐 Python SiliconFlowModelClient: 包含 token 数 + 费用信息（_extract_cost_info），
// 不包含 cache_tokens。
func buildSiliconFlowUsageMetadata(
	usage *openai.ResponseUsage,
	modelName string,
) *llmschema.UsageMetadata {
	meta := llmschema.NewUsageMetadata()
	meta.ModelName = modelName
	meta.InputTokens = usage.PromptTokens
	meta.OutputTokens = usage.CompletionTokens
	meta.TotalTokens = usage.TotalTokens

	// 提取费用信息（对齐 Python SiliconFlow 调用 _extract_cost_info）
	inputCost, outputCost, totalCost := extractCostFromUsage(usage)
	meta.InputCost = inputCost
	meta.OutputCost = outputCost
	meta.TotalCost = totalCost

	return meta
}

// extractCostFromUsage 从 ResponseUsage 提取费用信息。
//
// 复用 openai 包的 extractCostFromUsage。
func extractCostFromUsage(usage *openai.ResponseUsage) (inputCost, outputCost, totalCost float64) {
	// 将 Usage 转为 map 以复用 ExtractCostInfo
	data, err := json.Marshal(usage)
	if err != nil {
		return 0, 0, 0
	}
	var usageMap map[string]any
	if err := json.Unmarshal(data, &usageMap); err != nil {
		return 0, 0, 0
	}
	return model_clients.ExtractCostInfo(usageMap)
}

// sanitizeMessages 对消息做预处理：先调用基类转换，再清洗 tool_calls。
//
// 处理流程：
//  1. 先调用 OpenAI 基类的 ConvertMessagesToDict 做标准转换
//  2. 对转换后的消息做 sanitizeToolCalls（只保留标准字段，强制 type="function"）
//  3. 包装为 Dicts 模式回传（Dicts 模式直接透传，零转换开销）
//
// 对应 Python: SiliconFlowModelClient._build_and_sanitize_params()
func (c *SiliconFlowModelClient) sanitizeMessages(
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

// sanitizeToolCalls 清洗消息中的 tool_calls，只保留 OpenAI 标准字段。
//
// SiliconFlow API 对请求中的非标准字段严格，遇到不认识的字段会报错。
// 本函数对每个 assistant 消息的 tool_calls 做清洗：
//   - 只保留标准字段：id、type、index、function.name、function.arguments
//   - 强制 type 为 "function"（某些 LLM 返回的 type 可能为空或其他值）
//   - 移除非标准扩展字段（LLM 返回的原始 tool_calls 可能包含额外字段）
//
// 原地修改 messages 中的 tool_calls 字段。
//
// 对应 Python: SiliconFlowModelClient._sanitize_tool_calls()
func (c *SiliconFlowModelClient) sanitizeToolCalls(messages []map[string]any) {
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
