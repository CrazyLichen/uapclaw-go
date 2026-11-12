package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/headers_helper"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/reasoning"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/utils/providererror"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/utils/requestsanitizer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenAIModelClient OpenAI 兼容协议的 LLM 模型客户端。
// 支持所有兼容 OpenAI Chat Completion API 的服务提供商，
// 包括 OpenAI 官方 API 和 OpenRouter 等第三方代理。
// Python: openjiuwen/core/foundation/llm/model_clients/openai_model_client.py (OpenAIModelClient)
type OpenAIModelClient struct {
	model_clients.BaseClientEmbed
	// baseHeaders 预构建的配置级请求头
	baseHeaders map[string]string
	// rejectingDisabledThinking 进程内已知拒绝「关闭思考」的模型名集合。
	// Python: openai_model_client.py:456 self._models_rejecting_disabled_thinking
	rejectingDisabledThinking *rejectingThinkingSet
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent openai 包统一使用 AgentCore 组件标识记录日志。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenAIModelClient 创建 OpenAI 兼容客户端。
// Python: OpenAIModelClient.__init__(model_config, model_client_config)
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
	baseHeaders := headers_helper.BuildBaseHeaders(clientConfig.CustomHeaders)

	// Python: 创建客户端前记录配置参数（Python 使用 llm_logger.info，非回调）
	finalTimeout := clientConfig.Timeout
	if finalTimeout <= 0 {
		// Python: schema/config.py:145 默认 360（>= first_chunk 300 不变量）
		finalTimeout = defaultRequestTimeoutSeconds
	}
	logger.Info(logComponent).
		Str("model_provider", clientConfig.ClientProvider).
		Float64("timeout", finalTimeout).
		Int("max_retries", clientConfig.MaxRetries).
		Msg("Before create openai client, model client config params ready.")

	return &OpenAIModelClient{
		BaseClientEmbed:           *embed,
		baseHeaders:               baseHeaders,
		rejectingDisabledThinking: newRejectingThinkingSet(),
	}, nil
}

// Invoke 非流式调用 LLM，返回完整的助手消息。
// Python: OpenAIModelClient.invoke()
func (c *OpenAIModelClient) Invoke(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	params := model_clients.NewInvokeParams(opts...)

	// Python: openai_model_client.py:1876-1880 Responses 分支优先于 chat-completions SDK 路径
	if c.usesResponsesAPI() {
		messagesDict, convErr := c.ConvertMessagesToDict(messages)
		if convErr != nil {
			return nil, convErr
		}
		return c.invokeResponsesAPI(ctx, messagesDict, params)
	}

	// 1. 转换消息格式
	messagesDict, err := c.ConvertMessagesToDict(messages)
	if err != nil {
		return nil, err
	}

	// 2. 构建请求参数
	reqParams, err := c.BuildRequestParams(ctx, messagesDict, params.ToInvokeParams(), false)
	if err != nil {
		return nil, err
	}

	// 2.5 reasoning plan 接入
	// Python: openai_model_client.py:1002-1016（_build_request_params 内的 apply_reasoning_plan）
	c.applyReasoningPlan(reqParams, params)

	// 3. 合并 headers
	effectiveHeaders := c.BuildEffectiveHeaders(params.CustomHeaders)
	if len(effectiveHeaders) > 0 {
		reqParams["extra_headers"] = effectiveHeaders
	}

	// 3.5 模型特有参数（MiniMax reasoning_split）
	// Python: openai_model_client.py:1914 _apply_model_specific_params
	ApplyModelSpecificParams(defaultModelParamRules, modelNameOf(reqParams), reqParams)

	// 4. 厂商私有字段搬进 extra_body（7 键）
	// Python: openai_model_client.py:1915 _move_openai_extra_body_extensions
	HandleExtraBody(reqParams)

	// 4.5 已知拒绝 disabled thinking 的模型先剥离（仅回退作用域内）
	// Python: openai_model_client.py:1916-1917
	if reasoning.IsDisabledThinkingFallbackAllowed(ctx) {
		reqParams = c.applyDisabledThinkingCache(reqParams)
	}

	// 5. OpenAI 特有参数调整（openai.com 的 temperature/top_p 互斥）
	// Python: openai_model_client.py:1025-1035（在 _build_request_params 内，plan 之后）
	AdjustParamsForOpenAI(reqParams, c.ClientConfig.APIBase)

	// 5.5 请求边界归一化（孤立代理位清洗 + tool_calls arguments 转 JSON 字符串）
	// Python: openai_model_client.py:1085 return sanitize_chat_request(params)
	//（_build_request_params 的最后一行，位于模型特有参数/extra_body 搬移之前）
	reqParams = requestsanitizer.SanitizeChatRequest(reqParams)

	// 6. tracer 回调（对齐上游 :1918，位于 disabled cache 之后）
	if params.TracerRecordData != nil {
		params.TracerRecordData(map[string]any{"llm_params": reqParams})
	}

	// 7. 构建 HTTP 请求
	httpHeaders := ExtractHTTPHeaders(effectiveHeaders)
	buildReq := func(sendParams map[string]any) (*http.Request, *http.Client, error) {
		return BuildHTTPRequestWithAuth(
			ctx,
			c.ClientConfig.APIBase,
			c.ClientConfig.APIKey,
			sendParams,
			httpHeaders,
			c.RequestTimeout(params.Timeout),
			c.ClientConfig.VerifySSL,
			c.ClientConfig.SSLCert,
			false,
			c.ClientConfig.AuthMode,
		)
	}

	// 先做一次构建以尽早暴露编码错误（不实际发送；发送在 step 8 由 send 重建）
	if _, _, err := buildReq(reqParams); err != nil {
		// 请求编码错误原样上抛（不包 MODEL_CALL_FAILED，跳过瞬时失败重试）
		// Python: openai_model_client.py:2011/2228 details=={"stage":"request_encoding"} 时 raise
		if IsRequestEncodingErr(err) {
			return nil, err
		}
		return nil, c.WrapError("invoke", err)
	}

	// 7. 触发 LLMInput 回调（对齐 Python trigger(LLM_INPUT)）
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMInput,
		ModelName:     reqParams["model"].(string),
		ModelProvider: c.ClientConfig.ClientProvider,
		IsStream:      false,
	})

	// 8. 发送（含 disabled-thinking 回退）
	// Python: openai_model_client.py:1941 _create_chat_completion_with_disabled_thinking_fallback
	var resp *http.Response
	sendErr := c.doWithDisabledThinkingFallback(ctx, reqParams, func(sendParams map[string]any) error {
		// 重试时用剥离后的参数重建请求
		sendReq, sendClient, buildErr := buildReq(sendParams)
		if buildErr != nil {
			return buildErr
		}
		httpResp, doErr := sendClient.Do(sendReq)
		if doErr != nil {
			return doErr
		}
		if httpResp.StatusCode != http.StatusOK {
			// 非 200 交给 HandleHTTPError 转成带状态码的错误，
			// 使 disabled-thinking 判据能读到 status。
			defer func() { _ = httpResp.Body.Close() }()
			return c.HandleHTTPError(httpResp)
		}
		resp = httpResp
		return nil
	})
	if sendErr != nil {
		// Python: P4: Invoke 错误记录完整上下文
		_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
			Event:         callback.LLMCallError,
			ModelName:     reqParams["model"].(string),
			ModelProvider: c.ClientConfig.ClientProvider,
			IsStream:      false,
			Error:         sendErr,
		})
		// 编码错误原样上抛；其余按 MODEL_CALL_FAILED 包装
		if IsRequestEncodingErr(sendErr) {
			return nil, sendErr
		}
		return nil, c.WrapError("invoke", sendErr)
	}
	defer func() { _ = resp.Body.Close() }()

	// 8. 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		return nil, c.HandleHTTPError(resp)
	}

	// Python: openai_model_client.py:2162 trigger(LLM_RESPONSE_STARTED,
	// retry_count=_sdk_retry_count(...))——响应头到达即触发，标记 TTFB。
	// Go 无 SDK 重试，retry_count 恒 nil（spec P-2）。
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMResponseStarted,
		ModelName:     reqParams["model"].(string),
		ModelProvider: c.ClientConfig.ClientProvider,
		IsStream:      false,
	})

	// 9. 解析响应
	var completionResp ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completionResp); err != nil {
		return nil, c.WrapError("invoke", fmt.Errorf("解析响应失败: %w", err))
	}

	// Python: 收到响应记录完整上下文（Python 使用 llm_logger.info，非回调）
	logger.Info(logComponent).
		Str("model_name", reqParams["model"].(string)).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Bool("is_stream", false).
		Msg("OpenAI API response received.")

	// Python: 解析响应前记录 output_parser（Python 使用 llm_logger.info，非回调）
	logger.Info(logComponent).
		Str("model_name", reqParams["model"].(string)).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Bool("is_stream", false).
		Str("output_parser", fmt.Sprintf("%v", params.OutputParser)).
		Msg("Before parse response with output parser.")

	// 10. 转换为 AssistantMessage
	assistantMsg, err := ParseResponse(&completionResp, c.ModelConfig, params.OutputParser)
	if err != nil {
		return nil, c.WrapError("invoke", err)
	}

	// 10.5 对齐 Python: if tracer_record_data: await tracer_record_data(llm_response=assistant_message)
	// 响应解析后调用 tracer_record_data 回调，记录响应结果
	if params.TracerRecordData != nil {
		params.TracerRecordData(map[string]any{"llm_response": assistantMsg})
	}

	// 触发 LLMOutput 回调（对齐 Python trigger(LLM_OUTPUT)）
	outputData := &callback.LLMCallEventData{
		Event:         callback.LLMOutput,
		ModelName:     reqParams["model"].(string),
		ModelProvider: c.ClientConfig.ClientProvider,
		IsStream:      false,
		Response:      assistantMsg,
	}
	if assistantMsg.UsageMetadata != nil {
		outputData.Usage = assistantMsg.UsageMetadata
	}
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, outputData)

	return assistantMsg, nil
}

// Stream 流式调用 LLM，返回纯 chunk channel。
// 调用方通过 `range chunkChan` 消费流式 chunk；
// goroutine 内部累积 final_message，流结束时通过
// tracer_record_data(llm_response=finalMessage) 传出。
// Python: OpenAIModelClient.stream()
func (c *OpenAIModelClient) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	params := model_clients.NewStreamParams(opts...)

	// Python: openai_model_client.py:2058-2062 Responses 分支优先于 chat-completions SDK 路径
	if c.usesResponsesAPI() {
		messagesDict, convErr := c.ConvertMessagesToDict(messages)
		if convErr != nil {
			return nil, convErr
		}
		return c.streamResponsesAPI(ctx, messagesDict, params)
	}

	// 1. 转换消息格式
	messagesDict, err := c.ConvertMessagesToDict(messages)
	if err != nil {
		return nil, err
	}

	// 2. 构建请求参数
	reqParams, err := c.BuildRequestParams(ctx, messagesDict, params.ToStreamParams(), true)
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

	// 3.5 reasoning plan 接入
	// Python: openai_model_client.py:1002-1016（_build_request_params 内）
	c.applyReasoningPlan(reqParams, params.ToStreamParams())

	// 4. 合并 headers
	effectiveHeaders := c.BuildEffectiveHeaders(params.CustomHeaders)
	if len(effectiveHeaders) > 0 {
		reqParams["extra_headers"] = effectiveHeaders
	}

	// 4.5 模型特有参数（MiniMax reasoning_split）
	// Python: openai_model_client.py:2106 _apply_model_specific_params
	ApplyModelSpecificParams(defaultModelParamRules, modelNameOf(reqParams), reqParams)

	// 5. 厂商私有字段搬进 extra_body（7 键）
	// Python: openai_model_client.py:2107 _move_openai_extra_body_extensions
	HandleExtraBody(reqParams)

	// 5.5 已知拒绝 disabled thinking 的模型先剥离（仅回退作用域内）
	// Python: openai_model_client.py:2108-2109
	if reasoning.IsDisabledThinkingFallbackAllowed(ctx) {
		reqParams = c.applyDisabledThinkingCache(reqParams)
	}

	// 6. OpenAI 特有参数调整
	AdjustParamsForOpenAI(reqParams, c.ClientConfig.APIBase)

	// 6.3 请求边界归一化
	// Python: openai_model_client.py:1085（_build_request_params 最后一行）
	reqParams = requestsanitizer.SanitizeChatRequest(reqParams)

	// 6.5 tracer 回调（对齐上游 :2110，位于 disabled cache 之后）
	if params.TracerRecordData != nil {
		params.TracerRecordData(map[string]any{"llm_params": reqParams})
	}

	// 7. 构建 HTTP 请求
	httpHeaders := ExtractHTTPHeaders(effectiveHeaders)
	buildReq := func(sendParams map[string]any) (*http.Request, *http.Client, error) {
		return BuildHTTPRequestWithAuth(
			ctx,
			c.ClientConfig.APIBase,
			c.ClientConfig.APIKey,
			sendParams,
			httpHeaders,
			c.RequestTimeout(params.Timeout),
			c.ClientConfig.VerifySSL,
			c.ClientConfig.SSLCert,
			true,
			c.ClientConfig.AuthMode,
		)
	}
	// 先做一次构建以尽早暴露编码错误（不实际发送；发送在 step 8 由 send 重建）
	if _, _, err := buildReq(reqParams); err != nil {
		// 请求编码错误原样上抛（不包 MODEL_CALL_FAILED，跳过瞬时失败重试）
		// Python: openai_model_client.py:2011/2228 details=={"stage":"request_encoding"} 时 raise
		if IsRequestEncodingErr(err) {
			return nil, err
		}
		return nil, c.WrapError("stream", err)
	}

	// 触发 LLMInput 回调（对齐 Python trigger(LLM_INPUT)）
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMInput,
		ModelName:     reqParams["model"].(string),
		ModelProvider: c.ClientConfig.ClientProvider,
		IsStream:      true,
	})

	// 8. 发送请求（含 disabled-thinking 回退）
	// Python: openai_model_client.py:2152 _create_chat_completion_with_disabled_thinking_fallback
	var resp *http.Response
	sendErr := c.doWithDisabledThinkingFallback(ctx, reqParams, func(sendParams map[string]any) error {
		// 每次都用当前参数重建请求（与 Invoke 一致；确保重试时用的是剥离后的参数）
		sendReq, sendClient, buildErr := buildReq(sendParams)
		if buildErr != nil {
			return buildErr
		}
		httpResp, doErr := sendClient.Do(sendReq)
		if doErr != nil {
			return doErr
		}
		if httpResp.StatusCode != http.StatusOK {
			defer func() { _ = httpResp.Body.Close() }()
			return c.HandleHTTPError(httpResp)
		}
		resp = httpResp
		return nil
	})
	if sendErr != nil {
		// Python: P5: Stream 错误记录完整上下文
		_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
			Event:         callback.LLMCallError,
			ModelName:     reqParams["model"].(string),
			ModelProvider: c.ClientConfig.ClientProvider,
			IsStream:      true,
			Error:         sendErr,
		})
		if IsRequestEncodingErr(sendErr) {
			return nil, sendErr
		}
		return nil, c.WrapError("stream", sendErr)
	}

	// Python: openai_model_client.py:2162 trigger(LLM_RESPONSE_STARTED)——
	// SSE 连接建立（响应头到达）即触发，标记 TTFB；retry_count 恒 nil（P-2）。
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMResponseStarted,
		ModelName:     reqParams["model"].(string),
		ModelProvider: c.ClientConfig.ClientProvider,
		IsStream:      true,
	})

	// 10. 创建 SSE 读取器和 chunk channel
	sseReader := NewSSEReader(resp.Body)
	chunkChan := make(chan *llmschema.AssistantMessageChunk, 64)

	// 11. 启动 goroutine 消费 SSE 流（对齐 Python _astream_with_parser）
	modelName := fmt.Sprintf("%v", reqParams["model"])
	go func() {
		defer close(chunkChan)
		defer func() { _ = resp.Body.Close() }()

		// Python: _astream_with_parser: 累积内容缓冲区
		accumulatedContent := ""
		// Python: _astream_with_parser: final_message 累积
		var finalMessage *llmschema.AssistantMessageChunk

		for {
			data, err := sseReader.ReadEvent()
			if err == io.EOF {
				// Python: if tracer_record_data: await tracer_record_data(llm_response=final_message)
				if params.TracerRecordData != nil {
					params.TracerRecordData(map[string]any{"llm_response": finalMessage})
				}
				// Python: 流结束时触发 LLMOutput 回调
				_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
					Event:         callback.LLMOutput,
					ModelName:     modelName,
					ModelProvider: c.ClientConfig.ClientProvider,
					IsStream:      true,
				})
				return
			}
			if err != nil {
				// Python: P5: Stream 错误记录完整上下文 + raise MODEL_CALL_FAILED
				c.FailStream(ctx, chunkChan, modelName, err)
				return
			}

			// 解析 JSON
			var chunkResp ChatCompletionChunkResponse
			if err := json.Unmarshal([]byte(data), &chunkResp); err != nil {
				// Python: JSON 解析错误走日志，非回调
				logger.Error(logComponent).
					Str("model_name", modelName).
					Str("model_provider", c.ClientConfig.ClientProvider).
					Err(err).
					Msg("Stream parser attempt error.")
				continue
			}

			// 流内错误帧：必须转成 StreamError，否则被 ParseStreamChunk 当空块丢弃
			if chunkResp.Error != nil && chunkResp.Error.Message != "" {
				c.FailStream(ctx, chunkChan, modelName, fmt.Errorf("stream error event: %s (code=%s, type=%s)",
					chunkResp.Error.Message, chunkResp.Error.Code, chunkResp.Error.Type))
				return
			}

			// 转换为 AssistantMessageChunk
			chunk := ParseStreamChunk(&chunkResp, c.ModelConfig)
			if chunk == nil {
				continue
			}

			// Python: _astream_with_parser: 应用 output_parser
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
						// Python: parser 错误走 llm_logger.debug，非回调
						logger.Error(logComponent).
							Str("model_name", modelName).
							Str("model_provider", c.ClientConfig.ClientProvider).
							Err(parseErr).
							Msg("Stream parser attempt error.")
					}
				}
			}

			// Python: 逐 chunk 触发 LLMResponseReceived 回调
			_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
				Event:         callback.LLMResponseReceived,
				ModelName:     modelName,
				ModelProvider: c.ClientConfig.ClientProvider,
				IsStream:      true,
			})

			// Python: final_message = final_message + parsed_chunk
			if finalMessage == nil {
				finalMessage = chunk
			} else {
				finalMessage = finalMessage.Merge(chunk)
			}

			// 发送到 channel（支持 context 取消）
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

// TranscribeAudio 实际实现已移至 transcription.go 文件。
// 参见 OpenAIModelClient.TranscribeAudio 方法。

// BuildEffectiveHeaders 合并配置级和请求级 headers。
// 导出以供 SiliconFlow/InferenceAffinity/IntelliRouter 等独立实现 Stream 的客户端复用。
func (c *OpenAIModelClient) BuildEffectiveHeaders(requestHeaders map[string]string) map[string]string {
	return headers_helper.MergeRequestHeaders(c.baseHeaders, requestHeaders)
}

// WrapError 包装错误为 MODEL_CALL_FAILED 异常。
// 导出以供 SiliconFlow/InferenceAffinity/IntelliRouter 等独立实现 Stream 的客户端复用。
func (c *OpenAIModelClient) WrapError(method string, err error) error {
	errDetail := fmt.Sprintf("%T: %v", err, err)
	if err.Error() == "" {
		errDetail = fmt.Sprintf("%T", err)
	}
	return exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf("OpenAI API async %s error: %s", method, errDetail)),
		// 保留 cause：文案不变，但 errors.Is 可命中原始错误（如 context.Canceled）
		exception.WithCause(err),
	)
}

// FailStream 上报流读取中断：Error 日志 + LLM_CALL_ERROR 回调 + 向通道投递哨兵 chunk。
// 调用后由调用方 return，defer 里的 close(chunkChan) 让消费方读到哨兵再结束。
// Python: openai_model_client._stream 的 except 分支——先 trigger(LLM_CALL_ERROR)，
// 再 llm_logger.error("... async stream error.", event_type=LLM_CALL_ERROR, ...)，
// 最后 raise build_error(StatusCode.MODEL_CALL_FAILED, ...)。
// 两个容易漏的点：
//  1. 必须自行打日志。Go 的 TriggerLLM 只是把事件派发给注册的观察者（默认只有
//     agent_teams 的 OTel handler，且仅在开启 tracing 的团队模式注册），单 agent
//     会话里等于空转——此前这条错误既不上抛也不落日志，表现为"流悄悄断在半截"。
//  2. 必须把错误交给消费方。channel 装不下 error，用 StreamError 哨兵 chunk 承载，
//     否则半截 tool_call 会被当成完整调用去执行（参数 JSON 半截 → 工具报参数非法）。
func (c *OpenAIModelClient) FailStream(
	ctx context.Context,
	chunkChan chan<- *llmschema.AssistantMessageChunk,
	modelName string,
	err error,
) {
	logger.Error(logComponent).
		Str("event_type", "LLM_CALL_ERROR").
		Str("model_name", modelName).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Bool("is_stream", true).
		Err(err).
		Msg("LLM async stream error")

	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMCallError,
		ModelName:     modelName,
		ModelProvider: c.ClientConfig.ClientProvider,
		IsStream:      true,
		Error:         err,
	})

	select {
	case chunkChan <- llmschema.NewAssistantMessageChunk("",
		llmschema.WithChunkStreamError(c.WrapError("stream", err))):
	case <-ctx.Done():
	}
}

// HandleHTTPError 处理非 200 HTTP 响应。
// 导出以供 SiliconFlow/InferenceAffinity/IntelliRouter 等独立实现 Stream 的客户端复用。
//
// HTTP 状态码通过 BaseError.Params()["http_status"] 携带（不改变错误具体类型，
// 避免破坏全仓 10+ 处 `err.(*exception.BaseError)` 类型断言）。
// disabled-thinking 判据从 params 读该键。
//
// Python: openai_model_client.py:1349-1370 依赖 status_code 做四层过滤
func (c *OpenAIModelClient) HandleHTTPError(resp *http.Response) error {
	params := httpErrorParams(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("OpenAI API HTTP %d (无法读取响应体)", resp.StatusCode)),
			exception.WithParams(params),
		)
	}

	// 尝试解析 OpenAI 错误格式
	var errResp ErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		// 保留 provider 的结构化错误码：供上层做精确分类（如拒图识别、瞬时失败重试）
		params["provider_code"] = errResp.Error.Code
		params["provider_type"] = errResp.Error.Type
		return exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("OpenAI API HTTP %d: %s", resp.StatusCode, errResp.Error.Message)),
			exception.WithParams(params),
		)
	}

	return exception.NewBaseError(
		exception.StatusModelCallFailed,
		// Python: openai_model_client.py:594 summarize_provider_error_text(body, status_code)。
		// 只折叠 msg 文案；params 结构化字段（http_status/provider_code/retry_after）是
		// ModelAnomalyDetectionRail.classifyTransientFailure 的契约，一字不动。
		exception.WithMsg(fmt.Sprintf("OpenAI API HTTP %d: %s",
			resp.StatusCode, providererror.SummarizeProviderErrorText(string(body), resp.StatusCode))),
		exception.WithParams(params),
	)
}

// httpErrorParams 汇总 HTTP 错误的结构化上下文，供上层做瞬时失败分类。
// 除状态码与 provider 错误码外，还保留重试决策相关头部：
// retry_after / retry_after_ms（服务端建议的退避时长）、x_should_retry。
// Python: 异常对象的 headers/response.headers 由 rail 侧读取；Go 侧扁平进 params。
func httpErrorParams(resp *http.Response) map[string]any {
	params := map[string]any{"http_status": resp.StatusCode}
	if resp.Header == nil {
		return params
	}
	add := func(key, header string) {
		if v := strings.TrimSpace(resp.Header.Get(header)); v != "" {
			params[key] = v
		}
	}
	add("retry_after", "Retry-After")
	add("retry_after_ms", "Retry-After-Ms")
	add("x_should_retry", "X-Should-Retry")
	return params
}

// ExtractHTTPHeaders 从 effective headers 提取用于 HTTP 请求的头部。
// extra_headers 是 OpenAI SDK 的概念，我们的 HTTP 请求直接设置头部。
// 导出以供 SiliconFlow/InferenceAffinity/IntelliRouter 等独立实现 Stream 的客户端复用。
func ExtractHTTPHeaders(effectiveHeaders map[string]string) map[string]string {
	if len(effectiveHeaders) == 0 {
		return nil
	}
	return effectiveHeaders
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册 OpenAI 和 OpenRouter 客户端到全局注册表。
// Python: OpenAIModelClient.__client_name__ = ["OpenAI", "OpenRouter"]
// 通过 __init_subclass__ 自动注册。
func init() {
	registry := model_clients.GetClientRegistry()

	// OpenAI 客户端工厂
	openAIFactory := func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		client, err := NewOpenAIModelClient(mc, cc)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("create OpenAI client failed")
			return nil, err
		}
		return client, nil
	}

	registry.Register("OpenAI", "llm", openAIFactory)
	registry.Register("OpenRouter", "llm", openAIFactory)
	// DeepSeek 与上游一致：无独立客户端，走 OpenAI 客户端 + endpoint profile
	// （reasoning_content 补齐在 BuildRequestParams 按模型名处理）。
	// Python: openai_model_client.py + utils/endpoint_profiles.py（deepseek profile）
	registry.Register("DeepSeek", "llm", openAIFactory)
}

// SupportsKVCacheAffinity 本客户端不支持 Ascend KV cache 亲和。
// Python: ModelClient.supports_kv_cache_affinity() 默认 False
func (c *OpenAIModelClient) SupportsKVCacheAffinity() bool { return false }

// BuildKVCacheAffinityInvokeKwargs 不支持亲和时返回空参数。
// Python: ModelClient.build_kv_cache_affinity_invoke_kwargs() 默认返回 {}
func (c *OpenAIModelClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return map[string]any{}
}

// ──────────────────────────── KV cache 能力 ────────────────────────────

// kvCacheMode 读取 KV cache 模式。
// Python: openai_model_client.py:533-539 _kv_cache_mode
func (c *OpenAIModelClient) kvCacheMode() string {
	if c.ClientConfig == nil || c.ClientConfig.Extensions == nil {
		return "none"
	}
	mode := c.ClientConfig.Extensions.KVCache.Mode
	if mode == "" {
		return "none"
	}
	return mode
}

// SupportsKVCacheRelease 判定是否支持 KV cache 释放。
//
// 上游通过 extensions.kv_cache.mode 判断：只有 affinity 模式（vLLM 亲和部署）
// 支持 release。原先由独立的 inference_affinity 包提供该能力，别名归一后
// InferenceAffinity 已并入本客户端，故能力改由配置驱动。
//
// Python: openai_model_client.py:542 supports_kv_cache_affinity
func (c *OpenAIModelClient) SupportsKVCacheRelease() bool {
	return c.kvCacheMode() == "affinity"
}

// ──────────────────────────── 工具调用清洗 ────────────────────────────

// sanitizeToolCalls 清洗消息中的 tool_calls，只保留 OpenAI 标准字段。
// SiliconFlow API 对请求中的非标准字段严格，遇到不认识的字段会报错。
// 本函数对每个 assistant 消息的 tool_calls 做清洗：
//   - 只保留标准字段：id、type、index、function.name、function.arguments
//   - 强制 type 为 "function"（某些 LLM 返回的 type 可能为空或其他值）
//   - 移除非标准扩展字段（LLM 返回的原始 tool_calls 可能包含额外字段）
//
// 原地修改 messages 中的 tool_calls 字段。
// Python: SiliconFlowModelClient._sanitize_tool_calls()
func (c *OpenAIModelClient) sanitizeToolCalls(messages []map[string]any) {
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
