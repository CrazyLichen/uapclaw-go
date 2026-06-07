package callback

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// LoggingLLMCallback 默认 LLM 日志回调，将事件数据记录到 zerolog。
//
// 此回调保持与原有散落在各 model_client 中的 logger.Info/Error 行为一致，
// 作为 CallbackFramework 的默认注册回调，确保不丢失任何日志。
func LoggingLLMCallback(ctx context.Context, data *LLMCallEventData) {
	switch data.Event {
	case LLMCallStarted, LLMInvokeInput, LLMStreamInput, LLMInput:
		logLLMStart(ctx, data)
	case LLMCallError:
		logLLMError(ctx, data)
	case LLMResponseReceived, LLMInvokeOutput, LLMStreamOutput, LLMOutput:
		logLLMEnd(ctx, data)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// logLLMStart 记录 LLM 调用开始日志。
func logLLMStart(_ context.Context, data *LLMCallEventData) {
	evt := logger.Info(logger.ComponentAgentCore).
		Str("event_type", string(data.Event)).
		Str("model_name", data.ModelName).
		Str("model_provider", data.ModelProvider).
		Bool("is_stream", data.IsStream)

	if data.Temperature != nil {
		evt = evt.Float64("temperature", *data.Temperature)
	}
	if data.TopP != nil {
		evt = evt.Float64("top_p", *data.TopP)
	}
	if data.MaxTokens != nil {
		evt = evt.Int("max_tokens", *data.MaxTokens)
	}
	if data.Messages != nil {
		evt = evt.Any("messages", data.Messages)
	}
	if data.Tools != nil {
		evt = evt.Any("tools", data.Tools)
	}
	for k, v := range data.Extra {
		evt = evt.Any(k, v)
	}

	evt.Msg("LLM call started.")
}

// logLLMError 记录 LLM 调用错误日志。
func logLLMError(_ context.Context, data *LLMCallEventData) {
	evt := logger.Error(logger.ComponentAgentCore).
		Str("event_type", string(data.Event)).
		Str("model_name", data.ModelName).
		Str("model_provider", data.ModelProvider).
		Bool("is_stream", data.IsStream)

	if data.Error != nil {
		evt = evt.Err(data.Error)
	}
	if data.Messages != nil {
		evt = evt.Any("messages", data.Messages)
	}
	if data.Tools != nil {
		evt = evt.Any("tools", data.Tools)
	}
	for k, v := range data.Extra {
		evt = evt.Any(k, v)
	}

	evt.Msg("LLM call error.")
}

// logLLMEnd 记录 LLM 调用结束日志。
func logLLMEnd(_ context.Context, data *LLMCallEventData) {
	evt := logger.Info(logger.ComponentAgentCore).
		Str("event_type", string(data.Event)).
		Str("model_name", data.ModelName).
		Str("model_provider", data.ModelProvider).
		Bool("is_stream", data.IsStream)

	if data.Usage != nil {
		evt = evt.Int("input_tokens", data.Usage.InputTokens).
			Int("output_tokens", data.Usage.OutputTokens).
			Int("total_tokens", data.Usage.TotalTokens)
	}
	if data.Response != nil {
		evt = evt.Any("response_type", fmt.Sprintf("%T", data.Response))
	}
	for k, v := range data.Extra {
		evt = evt.Any(k, v)
	}

	evt.Msg("LLM call completed.")
}
