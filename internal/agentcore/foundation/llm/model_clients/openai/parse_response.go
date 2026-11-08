package openai

import (
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseResponse 将 OpenAI ChatCompletionResponse 转换为 schema.AssistantMessage。
// Python: OpenAIModelClient._parse_response()
//
// 处理逻辑：
//  1. 提取 choices[0].message → content, reasoning_content, tool_calls
//  2. 转换 tool_calls 为扁平 ToolCall 格式
//  3. 构建 UsageMetadata（含 cache_tokens、cost）
//  4. 应用 output_parser
//  5. 处理 finish_reason
//  6. 提取 logprobs（normalizeLogprobs）
//  7. 提取 prompt_token_ids, completion_token_ids
func ParseResponse(
	resp *ChatCompletionResponse,
	modelConfig *llmschema.ModelRequestConfig,
	parser model_clients.BaseOutputParser,
) (*llmschema.AssistantMessage, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("OpenAI API 响应无 choices")
	}

	choice := resp.Choices[0]
	message := choice.Message

	// 提取 content
	content := ""
	if message != nil && message.Content != nil {
		content = *message.Content
	}

	// 转换 tool_calls
	var toolCalls []*llmschema.ToolCall
	if message != nil && len(message.ToolCalls) > 0 {
		for idx, tc := range message.ToolCalls {
			index := idx
			if tc.Index != nil {
				index = *tc.Index
			}
			toolCalls = append(toolCalls, llmschema.NewToolCall(
				tc.ID,
				tc.Function.Name,
				tc.Function.Arguments,
				llmschema.WithToolCallIndex(index),
				llmschema.WithToolCallType(tc.Type),
			))
		}
	}

	// 提取推理内容（四级回退，见 ExtractReasoningContent）
	// Python: openai_model_client.py:2751-2763（_parse_response 内 L2804 调用）
	reasoningContent := extractReasoningFromMessage(message)

	// 构建 UsageMetadata
	var usageMetadata *llmschema.UsageMetadata
	if resp.Usage != nil {
		usageMetadata = buildUsageMetadata(resp.Usage, modelConfig.ModelName)
	}

	// 应用 output_parser
	var parserContent any

	// Python: 解析内容前记录（Python 使用 llm_logger.info，非回调）
	modelName := ""
	if modelConfig != nil {
		modelName = modelConfig.ModelName
	}
	logger.Info(logComponent).
		Str("model_name", modelName).
		Str("event_type", "llm_call_end").
		Str("response_content", content).
		Msg("Before parse content with parser.")

	logger.Info(logComponent).
		Str("model_name", modelName).
		Str("event_type", "llm_call_end").
		Str("parser", fmt.Sprintf("%v", parser)).
		Msg("Before parse content with parser config.")

	if parser != nil && content != "" {
		parsed, err := parser.Parse(content)
		if err == nil && parsed != nil {
			parserContent = parsed
			// Python: 解析成功记录（Python 使用 llm_logger.info，非回调）
			logger.Info(logComponent).
				Str("model_name", modelName).
				Str("event_type", "llm_call_end").
				Any("parser_content", parserContent).
				Msg("Parser parse success.")
		} else if err != nil {
			// Python: 解析错误记录（Python 使用 llm_logger.warning，非回调）
			logger.Error(logComponent).
				Str("model_name", modelName).
				Str("event_type", "llm_call_error").
				Err(err).
				Msg("Parser parse error.")
		}
	}

	// 处理 finish_reason：provider 有值原样保留；空值按 tool_calls/stop 兜底
	//
	// Python: openai_model_client.py:2877-2879
	//   finish_reason = getattr(choice, 'finish_reason', None) or None
	//   if not finish_reason:
	//       finish_reason = "tool_calls" if tool_calls else "stop"
	// 注意：不得把 "stop" 改写成 "tool_calls"——上游 docstring(2779-2786) 明确
	// "If the provider returns a value, it is preserved as-is"。
	finishReason := choice.FinishReason
	if finishReason == "" {
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
		} else {
			finishReason = "stop"
		}
	}

	// 提取 prompt_token_ids
	var promptTokenIDs []int
	if len(resp.PromptTokenIDs) > 0 {
		promptTokenIDs = resp.PromptTokenIDs
	}

	// 提取 completion_token_ids
	var completionTokenIDs []int
	if len(choice.TokenIDs) > 0 {
		completionTokenIDs = choice.TokenIDs
	}

	// 处理 logprobs
	logprobs := normalizeLogprobs(choice.Logprobs)

	return llmschema.NewAssistantMessage(
		content,
		llmschema.WithToolCalls(toolCalls),
		llmschema.WithAssistantUsageMetadata(usageMetadata),
		llmschema.WithFinishReason(finishReason),
		llmschema.WithReasoningContent(reasoningContent),
		llmschema.WithParserContent(parserContent),
		llmschema.WithPromptTokenIDs(promptTokenIDs),
		llmschema.WithCompletionTokenIDs(completionTokenIDs),
		llmschema.WithLogprobs(logprobs),
	), nil
}

// UsageToMap 将强类型 ResponseUsage 转为 map 形态供抽取函数消费。
// 导出以供 SiliconFlow / InferenceAffinity 复用（它们持有同一 ResponseUsage 类型）。
func UsageToMap(usage *ResponseUsage) map[string]any {
	if usage == nil {
		return nil
	}
	data, err := json.Marshal(usage)
	if err != nil {
		return nil
	}
	var usageMap map[string]any
	if err := json.Unmarshal(data, &usageMap); err != nil {
		return nil
	}
	return usageMap
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildUsageMetadata 从 OpenAI 响应的 Usage 构建 UsageMetadata。
// 统一委托 model_clients.BuildUsageMetadata（cache/reasoning 族字段一比一复刻
// openai_model_client.py:2817-2829 / 2940-2946 组装点），cost 仍在客户端侧追加
// （Python 同样在组装点单列 _extract_cost_info）。
func buildUsageMetadata(usage *ResponseUsage, modelName string) *llmschema.UsageMetadata {
	meta := model_clients.BuildUsageMetadata(UsageToMap(usage), modelName)

	// 提取费用信息
	inputCost, outputCost, totalCost := extractCostFromUsage(usage)
	meta.InputCost = inputCost
	meta.OutputCost = outputCost
	meta.TotalCost = totalCost

	return meta
}

// extractCostFromUsage 从 OpenAI Usage 对象提取费用信息。
// 复用 model_clients.ExtractCostInfo 逻辑，将 ResponseUsage 转为 map 后调用。
func extractCostFromUsage(usage *ResponseUsage) (inputCost, outputCost, totalCost float64) {
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

// normalizeLogprobs 将 provider 的 logprobs 对象转为可 JSON 序列化的形式。
// Python: OpenAIModelClient._normalize_logprobs()
func normalizeLogprobs(logprobs any) any {
	if logprobs == nil {
		return nil
	}
	// 如果已经是 map 或 slice，直接返回
	switch logprobs.(type) {
	case map[string]any, []any:
		return logprobs
	}
	// 尝试序列化再反序列化
	data, err := json.Marshal(logprobs)
	if err != nil {
		return nil
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
}

// extractReasoningFromMessage 将响应消息转 map 后走统一四级回退抽取。
func extractReasoningFromMessage(message *ResponseMessage) string {
	if message == nil {
		return ""
	}
	data, err := json.Marshal(message)
	if err != nil {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return ""
	}
	return model_clients.ExtractReasoningContent(fields)
}
