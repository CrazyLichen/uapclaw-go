package openai

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/utils/providererror"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/utils/responses"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/security"
)

// ──────────────────────────── 常量 ────────────────────────────

// responsesLogComponent Responses 分流日志组件标识。
const responsesLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 非导出函数 ────────────────────────────

// usesResponsesAPI 是否走 Responses 端点（/v1/responses）。
// Python: openai_model_client.py:1540 _uses_responses_api
func (c *OpenAIModelClient) usesResponsesAPI() bool {
	return c.ClientConfig.APIMode == llmschema.APIModeResponses
}

// buildResponsesRequestBody 构建 Responses 请求体。
// 采样参数开关（send_sampling_params/send_max_output_tokens）决定 temperature/top_p/max_tokens
// 是否进 body；reasoning/extra_body/tool_choice/parallel_tool_calls/include_reasoning_encrypted_content
// 从 Extra 弹出；model_dump 余量（ModelConfig.Extra 非排除键）并入 extra_body。
// Python: openai_model_client.py:1546 _build_responses_request_body
func (c *OpenAIModelClient) buildResponsesRequestBody(messages []map[string]any, params *model_clients.InvokeParams) (map[string]any, error) {
	finalModel := params.Model
	if finalModel == "" && c.ModelConfig != nil {
		finalModel = c.ModelConfig.ModelName
	}
	if finalModel == "" {
		return nil, c.WrapError("invoke", fmt.Errorf("The model cannot be empty."))
	}

	// kwargs 开关弹出（Python :1563-1564）
	extra := params.Extra
	sendSamplingParams := popBool(extra, "send_sampling_params")
	sendMaxOutputTokens := popBool(extra, "send_max_output_tokens")

	// 采样参数仅在开关开启时进 body（Python :1566-1575）
	finalTemperature := temperatureOrNull(params.Temperature, c.ModelConfig, sendSamplingParams)
	finalTopP := topPOrNull(params.TopP, c.ModelConfig, sendSamplingParams)

	// max_tokens 缺省回落 model_config（Python :1576）
	var maxTokens any
	if sendMaxOutputTokens {
		if params.MaxTokens != nil {
			maxTokens = *params.MaxTokens
		} else if c.ModelConfig != nil && c.ModelConfig.MaxTokens != nil {
			maxTokens = *c.ModelConfig.MaxTokens
		}
	}

	// stop 缺省回落 model_config（Python :1577）
	var stop any
	if params.Stop != nil {
		stop = *params.Stop
	} else if c.ModelConfig != nil && c.ModelConfig.Stop != nil {
		stop = *c.ModelConfig.Stop
	}

	// kwargs 弹出（Python :1579-1583）。reasoning 形态不校验、any 直通——
	// Python 接受 Union[ReasoningConfig, dict]，build_request_body 仅判 None（:70-79），
	// 形态合法性由服务端校验。
	reasoning := extra["reasoning"]
	delete(extra, "reasoning")
	extraBody, _ := extra["extra_body"].(map[string]any)
	delete(extra, "extra_body")
	toolChoice := popDefault(extra, "tool_choice", "auto")
	parallelToolCalls := popDefault(extra, "parallel_tool_calls", true)
	includeReasoning := popBool(extra, "include_reasoning_encrypted_content")

	// model_dump 余量：排除 6 键后的 model_config 字段并入 request_extra（Python :1585-1589）
	requestExtra := modelDumpExtra(c.ModelConfig)
	// 余量再被 kwargs 覆盖（Python request_extra.update(kwargs)）：kwargs reasoning
	// 后写覆盖 config 值；其余弹出键（tool_choice 等）经合并顺序最终取值与 Python 一致
	if reasoning != nil {
		requestExtra["reasoning"] = reasoning
	}
	nonInternal := map[string]bool{
		"output_parser": true, "tracer_record_data": true, "custom_headers": true,
		"session_id": true, "request_purpose": true, "context_operation_id": true,
		"send_sampling_params": true, "send_max_output_tokens": true,
		"reasoning": true, "extra_body": true, "tool_choice": true,
		"parallel_tool_calls": true, "include_reasoning_encrypted_content": true,
	}
	for k, v := range extra {
		if !nonInternal[k] {
			requestExtra[k] = v
		}
	}

	tools := []map[string]any{}
	if len(params.Tools) > 0 {
		tools = c.ConvertToolsToDict(params.Tools)
	}

	// extra_body 与 model_dump 余量合并，extra_body 键覆盖（Python :1603）
	mergedExtra := make(map[string]any, len(requestExtra)+len(extraBody))
	for k, v := range requestExtra {
		mergedExtra[k] = v
	}
	for k, v := range extraBody {
		mergedExtra[k] = v
	}

	// Python :1591-1604 转调 responses_utils.build_request_body
	return responses.BuildRequestBody(responses.BuildRequestBodyInput{
		Model:                            finalModel,
		Messages:                         messages,
		Tools:                            tools,
		Temperature:                      finalTemperature,
		TopP:                             finalTopP,
		MaxTokens:                        maxTokens,
		Stop:                             stop,
		Reasoning:                        reasoning,
		IncludeReasoningEncryptedContent: includeReasoning,
		ToolChoice:                       toolChoice,
		ParallelToolCalls:                parallelToolCalls,
		ExtraBody:                        mergedExtra,
	})
}

// makeResponsesTransport 构造 Responses transport（SSL/代理/超时）。
// Python: openai_model_client.py:1606 _make_responses_transport
func (c *OpenAIModelClient) makeResponsesTransport(timeout *float64) *responses.Transport {
	// verify：verify_ssl 时 strict SSL context（证书路径），否则 False（Python :1607-1611）
	var verify any = false
	if c.ClientConfig.VerifySSL {
		verify = c.ClientConfig.SSLCert
	}
	timeoutSeconds := c.ClientConfig.Timeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultRequestTimeoutSeconds
	}
	if timeout != nil {
		timeoutSeconds = *timeout
	}
	return responses.NewTransport(responses.TransportOptions{
		BaseURL:        c.ClientConfig.APIBase,
		TimeoutSeconds: timeoutSeconds,
		Verify:         verify,
		Proxy:          globalProxyURL(c.ClientConfig.APIBase),
	})
}

// invokeResponsesAPI 非流式 Responses 调用。
// Python: openai_model_client.py:1640 _invoke_responses_api
func (c *OpenAIModelClient) invokeResponsesAPI(
	ctx context.Context,
	messages []map[string]any,
	params *model_clients.InvokeParams,
) (*llmschema.AssistantMessage, error) {
	// 弹 session_id / 丢弃 request_purpose、context_operation_id（Python :1657-1659）
	sessionID := popString(params.Extra, "session_id")
	delete(params.Extra, "request_purpose")
	delete(params.Extra, "context_operation_id")

	body, err := c.buildResponsesRequestBody(messages, params)
	if err != nil {
		return nil, c.WrapError("invoke", err)
	}
	if params.TracerRecordData != nil {
		params.TracerRecordData(map[string]any{"llm_params": body})
	}

	requestHeaders := c.BuildEffectiveHeaders(params.CustomHeaders)
	triggerLLMInputFromResponsesBody(ctx, body, c.ClientConfig.ClientProvider, false)

	transport := c.makeResponsesTransport(params.Timeout)
	resp, err := transport.CreateResponse(ctx, responses.StreamRequest{
		Body:         body,
		AccessToken:  c.ClientConfig.APIKey,
		ModelName:    stringArgOf(body, "model"),
		SessionID:    sessionID,
		ExtraHeaders: requestHeaders,
	})
	if err != nil {
		c.emitResponsesError(ctx, body, messages, params.Tools, err, false)
		return nil, c.wrapResponsesError("invoke", err)
	}

	// output_parser 解析（失败 Warn 日志 + 返回 None，Python :1620-1632 / :1702-1703）
	if params.OutputParser != nil && resp.Content.String() != "" {
		parsed, parseErr := params.OutputParser.Parse(resp.Content.String())
		if parseErr != nil {
			logResponsesParseError(c.ModelConfig.ModelName, c.ClientConfig.ClientProvider, parseErr)
		} else {
			resp.ParserContent = parsed
		}
	}

	if params.TracerRecordData != nil {
		params.TracerRecordData(map[string]any{"llm_response": resp})
	}
	triggerLLMOutputFromResponsesMessage(ctx, body, c.ClientConfig.ClientProvider, resp, false)
	return resp, nil
}

// streamResponsesAPI 流式 Responses 调用。
// Python: openai_model_client.py:1719 _stream_responses_api
func (c *OpenAIModelClient) streamResponsesAPI(
	ctx context.Context,
	messages []map[string]any,
	params *model_clients.StreamParams,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	// 弹 session_id / 丢弃 request_purpose、context_operation_id（Python :1736-1738）
	sessionID := popString(params.Extra, "session_id")
	delete(params.Extra, "request_purpose")
	delete(params.Extra, "context_operation_id")

	body, err := c.buildResponsesRequestBody(messages, params.ToStreamParams())
	if err != nil {
		return nil, c.WrapError("stream", err)
	}
	if params.TracerRecordData != nil {
		params.TracerRecordData(map[string]any{"llm_params": body})
	}

	requestHeaders := c.BuildEffectiveHeaders(params.CustomHeaders)
	triggerLLMInputFromResponsesBody(ctx, body, c.ClientConfig.ClientProvider, true)

	transport := c.makeResponsesTransport(params.Timeout)
	chunkChan, err := transport.StreamResponse(ctx, responses.StreamRequest{
		Body:         body,
		AccessToken:  c.ClientConfig.APIKey,
		ModelName:    stringArgOf(body, "model"),
		SessionID:    sessionID,
		ExtraHeaders: requestHeaders,
	})
	if err != nil {
		c.emitResponsesError(ctx, body, messages, params.Tools, err, true)
		return nil, c.wrapResponsesError("stream", err)
	}

	out := make(chan *llmschema.AssistantMessageChunk)
	go func() {
		defer close(out)
		var finalMessage *llmschema.AssistantMessageChunk
		accumulatedForParser := ""
		for chunk := range chunkChan {
			if chunk == nil {
				continue
			}
			// 流内错误（transport 读失败 / response.failed 事件）→
			// emit + MODEL_CALL_FAILED 包装 + 哨兵 chunk（Python :1791-1796 的生成器 raise 等价物）
			if chunk.StreamError != nil {
				c.emitResponsesError(ctx, body, messages, params.Tools, chunk.StreamError, true)
				select {
				case out <- llmschema.NewAssistantMessageChunk("",
					llmschema.WithChunkStreamError(c.wrapResponsesError("stream", chunk.StreamError))):
				case <-ctx.Done():
				}
				return
			}

			// 每帧 trigger LLM_RESPONSE_RECEIVED（Python :1776-1780）
			c.triggerResponseReceived(ctx, stringArgOf(body, "model"))

			// output_parser 按累积文本尝试解析，成功挂 ParserContent 并清空累积
			//（Python :1781-1788；流式解析失败静默，Python :1634-1638）
			if params.OutputParser != nil && chunk.Content.String() != "" {
				accumulatedForParser += chunk.Content.String()
				parsed, parseErr := params.OutputParser.Parse(accumulatedForParser)
				if parseErr == nil && parsed != nil {
					chunk.ParserContent = parsed
					accumulatedForParser = ""
				}
			}

			// final = final + chunk（Python :1789）
			if finalMessage == nil {
				finalMessage = chunk
			} else {
				finalMessage = finalMessage.Merge(chunk)
			}
			out <- chunk
		}

		if params.TracerRecordData != nil {
			params.TracerRecordData(map[string]any{"llm_response": finalMessage})
		}
		// LLM_OUTPUT（is_stream=True，取 final 字段；Python :1801-1810）
		c.triggerResponsesOutput(ctx, body, finalMessage, true)
	}()
	return out, nil
}

// emitResponsesError CALL_ERROR 回调 + Error 日志。
// error_message 仅在错误自身无消息时填充（Python :1827-1829）。
// Python: openai_model_client.py:1812 _emit_responses_error
func (c *OpenAIModelClient) emitResponsesError(
	ctx context.Context,
	body map[string]any,
	messages, tools any,
	callErr error,
	isStream bool,
) {
	errorMessage := ""
	if strings.TrimSpace(callErr.Error()) == "" {
		errorMessage = callErr.Error()
	}
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, llmCallEventDataOf(
		body, c.ClientConfig.ClientProvider, isStream, callErr, errorMessage))
	logger.Error(responsesLogComponent).
		Str("event_type", "LLM_CALL_ERROR").
		Str("model_name", stringArgOf(body, "model")).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Any("messages", messages).
		Any("tools", tools).
		Bool("is_stream", isStream).
		Str("exception", providerErrorDetail(callErr)).
		Msg("Responses API call error.")
}

// wrapResponsesError 包装为 MODEL_CALL_FAILED。
// 文案对齐 Python :1699/:1795（Responses API invoke/stream error: <detail>）。
func (c *OpenAIModelClient) wrapResponsesError(method string, err error) error {
	return exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf("Responses API %s error: %v", method, err)),
		exception.WithCause(err),
	)
}

// triggerLLMInputFromResponsesBody LLM_INPUT 回调（字段取自 Responses body）。
// Python: openai_model_client.py:1675-1685/:1754-1764 trigger(LLMCallEvents.LLM_INPUT, ...)
func triggerLLMInputFromResponsesBody(ctx context.Context, body map[string]any, provider string, isStream bool) {
	evt := &callback.LLMCallEventData{
		Event: callback.LLMInput, ModelName: stringArgOf(body, "model"),
		ModelProvider: provider, IsStream: isStream,
		Messages: body["input"], Tools: body["tools"],
	}
	if t, ok := body["temperature"].(float64); ok {
		evt.Temperature = &t
	}
	if p, ok := body["top_p"].(float64); ok {
		evt.TopP = &p
	}
	if mt, ok := body["max_output_tokens"].(int); ok && mt > 0 {
		evt.MaxTokens = &mt
	}
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, evt)
}

// triggerResponseReceived 每帧 LLM_RESPONSE_RECEIVED 回调。
// Python: openai_model_client.py:1776-1780
func (c *OpenAIModelClient) triggerResponseReceived(ctx context.Context, modelName string) {
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event: callback.LLMResponseReceived, ModelName: modelName,
		ModelProvider: c.ClientConfig.ClientProvider,
	})
}

// triggerLLMOutputFromResponsesMessage 非流式 LLM_OUTPUT 回调。
// Python: openai_model_client.py:1708-1716
func triggerLLMOutputFromResponsesMessage(ctx context.Context, body map[string]any, provider string, resp *llmschema.AssistantMessage, isStream bool) {
	evt := &callback.LLMCallEventData{
		Event: callback.LLMOutput, ModelName: stringArgOf(body, "model"),
		ModelProvider: provider, IsStream: isStream,
		Response: resp.Content.String(),
		Usage:    resp.UsageMetadata,
	}
	if len(resp.ToolCalls) > 0 {
		evt.Extra = map[string]any{"tool_calls": resp.ToolCalls}
	}
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, evt)
}

// triggerResponsesOutput 流式 LLM_OUTPUT 回调（final 聚合字段；Python :1801-1810）。
func (c *OpenAIModelClient) triggerResponsesOutput(ctx context.Context, body map[string]any, final *llmschema.AssistantMessageChunk, isStream bool) {
	evt := &callback.LLMCallEventData{
		Event: callback.LLMOutput, ModelName: stringArgOf(body, "model"),
		ModelProvider: c.ClientConfig.ClientProvider, IsStream: isStream,
	}
	if final != nil {
		evt.Response = final.Content.String()
		evt.Usage = final.UsageMetadata
		if len(final.ToolCalls) > 0 {
			evt.Extra = map[string]any{"tool_calls": final.ToolCalls}
		}
	}
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, evt)
}

// logResponsesParseError 输出解析失败 Warn 日志。
// Python: openai_model_client.py:1624-1631（_parse_responses_content）
func logResponsesParseError(modelName, provider string, parseErr error) {
	logger.Warn(responsesLogComponent).
		Str("event_type", "LLM_CALL_ERROR").
		Str("model_name", modelName).
		Str("model_provider", provider).
		Bool("is_stream", false).
		Str("exception", fmt.Sprintf("%v", parseErr)).
		Msg("OpenAI Responses output parser error.")
}

// providerErrorDetail provider 异常格式化（不灌 HTML）。
// Python: format_provider_exception
func providerErrorDetail(err error) string {
	return providererror.FormatProviderException(err, fmt.Sprintf("%T", err), 0, false)
}

// ──────────────────────────── 非导出函数（小工具） ────────────────────────────

// popBool 从 Extra 弹出 bool（缺省 false）。
func popBool(extra map[string]any, key string) bool {
	v, ok := extra[key].(bool)
	if ok {
		delete(extra, key)
	}
	return v
}

// popString 从 Extra 弹出 string。
func popString(extra map[string]any, key string) string {
	v, _ := extra[key].(string)
	delete(extra, key)
	return v
}

// popDefault 从 Extra 弹出指定类型缺省值。
func popDefault(extra map[string]any, key string, def any) any {
	v, ok := extra[key]
	if ok {
		delete(extra, key)
		return v
	}
	return def
}

// temperatureOrNull 采样参数开关开启时取（方法参数 → model_config 缺省），否则 nil。
// Python: openai_model_client.py:1566-1570
func temperatureOrNull(param *float64, config *llmschema.ModelRequestConfig, enabled bool) any {
	if !enabled {
		return nil
	}
	if param != nil {
		return *param
	}
	if config != nil && config.Temperature != nil {
		return *config.Temperature
	}
	return nil
}

// topPOrNull 同 temperatureOrNull 的 top_p 版本。
// Python: openai_model_client.py:1571-1575
func topPOrNull(param *float64, config *llmschema.ModelRequestConfig, enabled bool) any {
	if !enabled {
		return nil
	}
	if param != nil {
		return *param
	}
	if config != nil && config.TopP != nil {
		return *config.TopP
	}
	return nil
}

// modelDumpExtra 模拟 model_dump 排除 6 键后的余量（Python :1585-1589）。
// Python model_dump(exclude={model_name,model,temperature,top_p,max_tokens,stop},
// exclude_none=True) 会带上 ModelRequestConfig 其余类型化字段：context_window 与
// reasoning（config.py:269-274，二者均不在排除名单）；extra="allow" 的动态字段
// 对应 Go 的 config.Extra。reasoning 形态不校验，any 直通（含 ReasoningConfig 指针）。
func modelDumpExtra(config *llmschema.ModelRequestConfig) map[string]any {
	extra := map[string]any{}
	if config == nil {
		return extra
	}
	if config.ContextWindow != nil {
		extra["context_window"] = *config.ContextWindow
	}
	if config.Reasoning != nil && !isTypedNil(config.Reasoning) {
		extra["reasoning"] = config.Reasoning
	}
	excluded := map[string]bool{
		"model_name": true, "model": true, "temperature": true,
		"top_p": true, "max_tokens": true, "stop": true,
	}
	for k, v := range config.Extra {
		if !excluded[k] {
			extra[k] = v
		}
	}
	return extra
}

// isTypedNil 判断 any 承载的指针/map/slice 是否为类型化 nil（避免序列化成 null）。
// 对齐 responses.isNilValue 语义（跨包不可引用，本地等价实现）。
func isTypedNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func:
		return rv.IsNil()
	}
	return false
}

// stringArgOf 从 body 取 string 字段。
func stringArgOf(body map[string]any, key string) string {
	s, _ := body[key].(string)
	return s
}

// llmCallEventDataOf 构造 CALL_ERROR 事件数据。
func llmCallEventDataOf(body map[string]any, provider string, isStream bool, callErr error, errorMessage string) *callback.LLMCallEventData {
	data := &callback.LLMCallEventData{
		Event: callback.LLMCallError, ModelName: stringArgOf(body, "model"),
		ModelProvider: provider, IsStream: isStream,
		Error: callErr,
	}
	if errorMessage != "" {
		data.Extra = map[string]any{"error_message": errorMessage}
	}
	return data
}

// globalProxyURL 全局代理 URL（NO_PROXY 命中返回空）。
// Python: security/url_utils.py:34 UrlUtils.get_global_proxy_url(api_base)
func globalProxyURL(apiBase string) string {
	return security.GetGlobalProxyURL(apiBase)
}
