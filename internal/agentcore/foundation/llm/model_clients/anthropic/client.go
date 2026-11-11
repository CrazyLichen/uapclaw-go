package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/headers_helper"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmreasoning "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/reasoning"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/utils/providererror"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AnthropicModelClient Anthropic Messages API 客户端。
// 不引 anthropic SDK（spec P-3）：手写 HTTP + SSE；SDK keepalive 语义由
// transport_pool 五元组分桶 + 100/20/60s 参数替代。
// Python: anthropic_model_client.py:618 AnthropicModelClient
type AnthropicModelClient struct {
	model_clients.BaseClientEmbed
	// baseHeaders 预构建的配置级请求头
	baseHeaders map[string]string
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件（anthropic 属 agentcore LLM 基础层）。
var logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAnthropicModelClient 创建 Anthropic Messages 客户端。
// Python: AnthropicModelClient.__init__
func NewAnthropicModelClient(
	modelConfig *llmschema.ModelRequestConfig,
	clientConfig *llmschema.ModelClientConfig,
) (*AnthropicModelClient, error) {
	embed, err := model_clients.NewBaseClientEmbed(
		modelConfig,
		clientConfig,
		model_clients.WithClientName("Anthropic client"),
	)
	if err != nil {
		return nil, err
	}
	// api_key 必填（Python: _validate_config :635-641）
	if strings.TrimSpace(clientConfig.APIKey) == "" {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg("model client config api_key is required for Anthropic client."),
		)
	}
	baseHeaders := headers_helper.BuildBaseHeaders(clientConfig.CustomHeaders)
	logger.Info(logComponent).
		Str("model_provider", clientConfig.ClientProvider).
		Float64("timeout", clientConfig.Timeout).
		Int("max_retries", 0).
		Str("base_url", NormalizeAnthropicBaseURL(clientConfig.APIBase)).
		Msg("Before create anthropic client, model client config params ready.")
	return &AnthropicModelClient{BaseClientEmbed: *embed, baseHeaders: baseHeaders}, nil
}

// NormalizeAnthropicBaseURL 剥尾部 /v1 与 /v1/messages，防 /v1/v1 双拼。
// 调用方常传 OpenAI 形状 api_base（https://openrouter.ai/api/v1），
// SDK 会在 base_url 后拼 /v1/messages。
// Python: anthropic_model_client.py:687-703 _normalize_base_url
func NormalizeAnthropicBaseURL(apiBase string) string {
	if apiBase == "" {
		return ""
	}
	b := strings.TrimRight(apiBase, "/")
	if strings.HasSuffix(b, "/v1/messages") {
		b = b[:len(b)-len("/v1/messages")]
	} else if strings.HasSuffix(b, "/v1") {
		b = b[:len(b)-3]
	}
	return b
}

// Invoke 非流式调用 Messages API。
// Python: anthropic_model_client.py:1003-1113 invoke。
// 步骤序：BuildAnthropicParams → 合并 headers → tracer → trigger(LLM_INPUT)
// → 发请求 → trigger(LLM_RESPONSE_STARTED) → ParseResponse → tracer
// → trigger(LLM_OUTPUT)；错误路径 trigger(LLM_CALL_ERROR) + MODEL_CALL_FAILED。
func (c *AnthropicModelClient) Invoke(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	params := model_clients.NewInvokeParams(opts...)

	messagesDict, sourceMessages, err := c.prepareMessages(messages)
	if err != nil {
		return nil, err
	}

	// 步骤 1-14：构建 Anthropic 形状请求
	reqParams, err := c.BuildAnthropicParams(ctx, messagesDict, sourceMessages, params, false)
	if err != nil {
		return nil, err
	}

	// 合并 headers（Python _build_request_headers :679-685）
	effectiveHeaders := headers_helper.MergeRequestHeaders(c.baseHeaders, params.CustomHeaders)
	if len(effectiveHeaders) > 0 {
		reqParams["extra_headers"] = effectiveHeaders
	}

	if params.TracerRecordData != nil {
		params.TracerRecordData(map[string]any{"llm_params": reqParams})
	}

	modelName := strArg(reqParams, "model")
	triggerLLMInput(ctx, reqParams, c.ClientConfig.ClientProvider, false)

	resp, httpErr := c.doMessagesRequest(ctx, reqParams, params.Timeout)
	if httpErr != nil {
		return nil, c.wrapProviderError(ctx, "invoke", modelName, false, httpErr)
	}
	defer func() { _ = resp.Body.Close() }()

	// trigger(LLM_RESPONSE_STARTED)：响应头到达即触发，标记 TTFB；retry_count 恒 nil（P-2）
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event: callback.LLMResponseStarted, ModelName: modelName,
		ModelProvider: c.ClientConfig.ClientProvider, IsStream: false,
	})

	if resp.StatusCode != http.StatusOK {
		return nil, c.wrapHTTPError(ctx, "invoke", modelName, false, resp)
	}

	var response map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, c.WrapError("invoke", fmt.Errorf("解析响应失败: %w", err))
	}

	logger.Info(logComponent).
		Str("model_name", modelName).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Bool("is_stream", false).
		Msg("Anthropic API response received.")

	assistantMsg := ParseResponse(response, params.OutputParser, c.ModelConfig.ModelName, c.ClientConfig.ClientProvider)

	if params.TracerRecordData != nil {
		params.TracerRecordData(map[string]any{"llm_response": assistantMsg})
	}

	outputData := &callback.LLMCallEventData{
		Event: callback.LLMOutput, ModelName: modelName,
		ModelProvider: c.ClientConfig.ClientProvider, IsStream: false,
		Response: assistantMsg.Content,
	}
	if assistantMsg.UsageMetadata != nil {
		outputData.Usage = assistantMsg.UsageMetadata
	}
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, outputData)

	return assistantMsg, nil
}

// Stream 流式调用 Messages API，返回纯 chunk channel。
// Python: anthropic_model_client.py:1115-1270 stream。
// 跨流累积：tool_use 参数、thinking 签名（供下一轮原样回放）、usage、stop_reason。
func (c *AnthropicModelClient) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	params := model_clients.NewStreamParams(opts...)

	messagesDict, sourceMessages, err := c.prepareMessages(messages)
	if err != nil {
		return nil, err
	}

	reqParams, err := c.BuildAnthropicParams(ctx, messagesDict, sourceMessages, params.ToStreamParams(), true)
	if err != nil {
		return nil, err
	}

	effectiveHeaders := headers_helper.MergeRequestHeaders(c.baseHeaders, params.CustomHeaders)
	if len(effectiveHeaders) > 0 {
		reqParams["extra_headers"] = effectiveHeaders
	}

	if params.TracerRecordData != nil {
		params.TracerRecordData(map[string]any{"llm_params": reqParams})
	}

	modelName := strArg(reqParams, "model")
	triggerLLMInput(ctx, reqParams, c.ClientConfig.ClientProvider, true)

	resp, httpErr := c.doMessagesRequest(ctx, reqParams, params.Timeout)
	if httpErr != nil {
		return nil, c.wrapProviderError(ctx, "stream", modelName, true, httpErr)
	}

	// trigger(LLM_RESPONSE_STARTED)（Python :1188）
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event: callback.LLMResponseStarted, ModelName: modelName,
		ModelProvider: c.ClientConfig.ClientProvider, IsStream: true,
	})

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, c.wrapHTTPError(ctx, "stream", modelName, true, resp)
	}

	chunkChan := make(chan *llmschema.AssistantMessageChunk, 64)
	provider := c.ClientConfig.ClientProvider
	parser := params.OutputParser
	tracer := params.TracerRecordData

	go func() {
		defer close(chunkChan)
		defer func() { _ = resp.Body.Close() }()

		// 跨事件累积状态（Python stream 内的 local 状态）
		currentText := ""
		toolUseAcc := map[int]map[string]any{}
		var lastUsage *llmschema.UsageMetadata
		finalStopReason := ""
		accumulatedForParser := ""

		for {
			event, readErr := readStreamEvent(resp.Body)
			if readErr != nil {
				if readErr == errStreamDone {
					break
				}
				// 流中断：Python except 分支（:1232-1251）
				c.failStream(ctx, chunkChan, modelName, readErr)
				return
			}
			chunk := EventToChunk(event, toolUseAcc)
			if chunk == nil {
				continue
			}
			if chunk.UsageMetadata != nil {
				lastUsage = chunk.UsageMetadata
			}
			if chunk.FinishReason != "" && chunk.FinishReason != llmschema.FinishReasonNull {
				finalStopReason = chunk.FinishReason
			}
			if chunk.Content.Text() != "" {
				currentText += chunk.Content.Text()
				if parser != nil {
					accumulatedForParser += chunk.Content.Text()
					if parsed, perr := parser.Parse(accumulatedForParser); perr == nil && parsed != nil {
						chunk.ParserContent = parsed
						accumulatedForParser = ""
					} else if perr != nil {
						logger.Debug(logComponent).
							Str("model_name", modelName).
							Str("model_provider", provider).
							Bool("is_stream", true).
							Msg("Anthropic stream parser attempt error.")
					}
				}
			}
			_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
				Event: callback.LLMResponseReceived, ModelName: modelName,
				ModelProvider: provider, IsStream: true,
			})
			select {
			case chunkChan <- chunk:
			case <-ctx.Done():
				return
			}
		}

		// 尾部 usage chunk（usage 迟到时的防御性补发）
		if lastUsage != nil {
			select {
			case chunkChan <- llmschema.NewAssistantMessageChunk("",
				llmschema.WithChunkUsageMetadata(lastUsage),
				llmschema.WithChunkFinishReason(orNull(finalStopReason))):
			case <-ctx.Done():
			}
		}

		if tracer != nil {
			tracer(map[string]any{"llm_response": map[string]any{"text": currentText}})
		}
	}()

	return chunkChan, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// prepareMessages 把 MessagesParam 转成 dict 列表并保留源消息（供 replay 回填与 transient 判定）。
func (c *AnthropicModelClient) prepareMessages(messages model_clients.MessagesParam) ([]map[string]any, []llmschema.BaseMessage, error) {
	messagesDict, err := c.ConvertMessagesToDict(messages)
	if err != nil {
		return nil, nil, err
	}
	switch {
	case messages.IsText():
		return messagesDict, nil, nil
	case messages.IsMessages():
		return messagesDict, messages.Messages(), nil
	default:
		// dict 输入：无 BaseMessage metadata 可取，transient/replay 由 dict 本身承载
		return messagesDict, nil, nil
	}
}

// doMessagesRequest 发送 POST /v1/messages（x-api-key + anthropic-version）。
// 共享 Transport 池按五元组复用（23.1-D1）；Client 只作用于本请求超时。
func (c *AnthropicModelClient) doMessagesRequest(ctx context.Context, params map[string]any, timeout *float64) (*http.Response, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("请求编码失败: %w", err)
	}
	apiURL := NormalizeAnthropicBaseURL(c.ClientConfig.APIBase) + messagesPath

	effectiveTimeout := c.ClientConfig.Timeout
	if timeout != nil && *timeout > 0 {
		effectiveTimeout = *timeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(effectiveTimeout*float64(time.Second)))
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", anthropicVersion)
	// 鉴权：auth_mode=none/custom_headers 时省略 x-api-key（对齐 openai 侧省略 Authorization 语义）
	if c.ClientConfig.AuthMode != llmschema.AuthModeNone && c.ClientConfig.AuthMode != llmschema.AuthModeCustomHeaders {
		req.Header.Set("x-api-key", c.ClientConfig.APIKey)
	}
	if extraHeaders, ok := params["extra_headers"].(map[string]string); ok {
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}
	}
	client := &http.Client{Transport: model_clients.GetSharedTransport(
		c.ClientConfig.AuthMode, c.ClientConfig.APIKey,
		NormalizeAnthropicBaseURL(c.ClientConfig.APIBase),
		c.ClientConfig.VerifySSL, c.ClientConfig.SSLCert)}
	return client.Do(req)
}

// triggerLLMInput 触发 LLM_INPUT（temperature/top_p 读 extra_body 或顶层，:238-244）。
func triggerLLMInput(ctx context.Context, params map[string]any, provider string, isStream bool) {
	evt := &callback.LLMCallEventData{
		Event: callback.LLMInput, ModelName: strArg(params, "model"),
		ModelProvider: provider, IsStream: isStream,
		Messages: params["messages"], Tools: params["tools"],
	}
	if t, ok := SamplingFromAnthropicParams(params, "temperature").(float64); ok {
		evt.Temperature = &t
	}
	if p, ok := SamplingFromAnthropicParams(params, "top_p").(float64); ok {
		evt.TopP = &p
	}
	if mt := intOf(params["max_tokens"]); mt > 0 {
		evt.MaxTokens = &mt
	}
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, evt)
}

// wrapHTTPError 非 200 响应 → 触发 LLM_CALL_ERROR + 摘要化错误。
func (c *AnthropicModelClient) wrapHTTPError(ctx context.Context, method, modelName string, isStream bool, resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	summarized := providererror.SummarizeProviderErrorText(string(body), resp.StatusCode)
	err := exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf("Anthropic API HTTP %d: %s", resp.StatusCode, summarized)),
		exception.WithParams(map[string]any{"http_status": resp.StatusCode}),
	)
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event: callback.LLMCallError, ModelName: modelName,
		ModelProvider: c.ClientConfig.ClientProvider, IsStream: isStream, Error: err,
	})
	logger.Error(logComponent).
		Str("event_type", "LLM_CALL_ERROR").
		Str("model_name", modelName).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Bool("is_stream", isStream).
		Err(err).
		Msg("Anthropic API " + method + " error.")
	return err
}

// wrapProviderError 传输层错误 → LLM_CALL_ERROR + MODEL_CALL_FAILED。
// Python: format_provider_exception(e, include_exc_type=False)（:1094/:1257 文案逐字）。
func (c *AnthropicModelClient) wrapProviderError(ctx context.Context, method, modelName string, isStream bool, err error) error {
	detail := providererror.FormatProviderException(err, fmt.Sprintf("%T", err), 0, false)
	wrapped := exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf("Anthropic API async %s error: %s", method, detail)),
		exception.WithCause(err),
	)
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event: callback.LLMCallError, ModelName: modelName,
		ModelProvider: c.ClientConfig.ClientProvider, IsStream: isStream, Error: err,
	})
	logger.Error(logComponent).
		Str("event_type", "LLM_CALL_ERROR").
		Str("model_name", modelName).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Bool("is_stream", isStream).
		Str("exception", detail).
		Msg("Anthropic API async " + method + " error.")
	return wrapped
}

// failStream 流读取中断：Error 日志 + LLM_CALL_ERROR 回调 + 哨兵 chunk。
// 对齐 openai.FailStream 语义（Python stream 的 except 分支）。
func (c *AnthropicModelClient) failStream(ctx context.Context, chunkChan chan<- *llmschema.AssistantMessageChunk, modelName string, err error) {
	logger.Error(logComponent).
		Str("event_type", "LLM_CALL_ERROR").
		Str("model_name", modelName).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Bool("is_stream", true).
		Err(err).
		Msg("Anthropic API async stream error")
	_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
		Event: callback.LLMCallError, ModelName: modelName,
		ModelProvider: c.ClientConfig.ClientProvider, IsStream: true, Error: err,
	})
	select {
	case chunkChan <- llmschema.NewAssistantMessageChunk("",
		llmschema.WithChunkStreamError(c.WrapError("stream", err))):
	case <-ctx.Done():
	}
}

// WrapError 包装为 MODEL_CALL_FAILED（文案对齐 Python :1094）。
func (c *AnthropicModelClient) WrapError(method string, err error) error {
	detail := fmt.Sprintf("%T: %v", err, err)
	if err.Error() == "" {
		detail = fmt.Sprintf("%T", err)
	}
	return exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf("Anthropic API async %s error: %s", method, detail)),
		exception.WithCause(err),
	)
}

// orNull finish_reason 空值回退 "null"（流式哨兵）。
func orNull(reason string) string {
	if reason == "" {
		return llmschema.FinishReasonNull
	}
	return reason
}

// ensureReasoningImports 编译期引用（reasoning 由 BuildAnthropicParams 使用）。
var _ = llmreasoning.ApplyReasoningPlan

// init 注册 Anthropic 客户端。
// Python: AnthropicModelClient.__client_name__ = [ProviderType.Anthropic.value]
func init() {
	model_clients.GetClientRegistry().Register("Anthropic", "llm",
		func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			client, err := NewAnthropicModelClient(mc, cc)
			if err != nil {
				logger.Error(logComponent).Err(err).Msg("create Anthropic client failed")
				return nil, err
			}
			return client, nil
		})
}

// Release Anthropic 无 KV cache 释放语义（对齐 OpenAIModelClient 桩）。
// Python: BaseModelClient.release 仅 InferenceAffinity 覆写。
func (c *AnthropicModelClient) Release(ctx context.Context, opts ...model_clients.ReleaseOption) (bool, error) {
	return false, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("release is not supported by AnthropicModelClient"),
	)
}

// TranscribeAudio Anthropic 无音频转写端点（对齐 openai transcription 不可用桩）。
func (c *AnthropicModelClient) TranscribeAudio(ctx context.Context, audioPath string, opts ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("transcribe_audio is not supported by AnthropicModelClient"),
	)
}

// SupportsKVCacheRelease 仅 InferenceAffinity 返回 true。
func (c *AnthropicModelClient) SupportsKVCacheRelease() bool { return false }

// SupportsKVCacheAffinity 仅 InferenceAffinity 客户端返回 true。
func (c *AnthropicModelClient) SupportsKVCacheAffinity() bool { return false }

// BuildKVCacheAffinityInvokeKwargs 不支持亲和时返回空参数（对齐 OpenAIModelClient 同名桩）。
func (c *AnthropicModelClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return map[string]any{}
}

// ──────────────────────────── 不支持的多模态方法（镜像 OpenAI 客户端桩风格）────────────────────────────

// GenerateImage Anthropic 客户端不支持图片生成。
// Python: anthropic_model_client.py:1617-1619
func (c *AnthropicModelClient) GenerateImage(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("generate_image is not supported by AnthropicModelClient"),
	)
}

// GenerateSpeech Anthropic 客户端不支持语音生成。
// Python: anthropic_model_client.py:1621-1625
func (c *AnthropicModelClient) GenerateSpeech(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("generate_speech is not supported by AnthropicModelClient"),
	)
}

// GenerateVideo Anthropic 客户端不支持视频生成。
// Python: anthropic_model_client.py:1627-1631
func (c *AnthropicModelClient) GenerateVideo(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("generate_video is not supported by AnthropicModelClient"),
	)
}
