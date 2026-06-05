package openai

import (
	"encoding/json"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseResponse 将 OpenAI ChatCompletionResponse 转换为 schema.AssistantMessage。
//
// 对应 Python: OpenAIModelClient._parse_response()
//
// 处理逻辑：
//  1. 提取 choices[0].message → content, reasoning_content, tool_calls
//  2. 转换 tool_calls 为扁平 ToolCall 格式
//  3. 构建 UsageMetadata（含 cache_tokens、cost）
//  4. 应用 output_parser（2.16 节后完整实现）
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

	// 提取 reasoning_content
	reasoningContent := ""
	if message != nil && message.ReasoningContent != nil {
		reasoningContent = *message.ReasoningContent
	}

	// 构建 UsageMetadata
	var usageMetadata *llmschema.UsageMetadata
	if resp.Usage != nil {
		usageMetadata = buildUsageMetadata(resp.Usage, modelConfig.ModelName)
	}

	// 应用 output_parser（2.16 节后完整实现，当前仅做基础处理）
	var parserContent any

	// 对齐 Python P7: 解析内容前记录
	modelName := ""
	if modelConfig != nil {
		modelName = modelConfig.ModelName
	}
	modelProvider := "" // ParseResponse 无法直接获取 model_provider，记录模型名即可
	log := logger.GetLogger(logger.ComponentGateway)
	log.Info().
		Str("event_type", "LLM_CALL_END").
		Str("model_name", modelName).
		Str("model_provider", modelProvider).
		Str("response_content", content).
		Bool("is_stream", false).
		Msg("Before parse content with parser.")

	// 对齐 Python P8: 解析内容配置前记录
	log.Info().
		Str("event_type", "LLM_CALL_END").
		Str("model_name", modelName).
		Str("model_provider", modelProvider).
		Bool("is_stream", false).
		Str("parser", fmt.Sprintf("%v", parser)).
		Msg("Before parse content with parser config.")

	if parser != nil && content != "" {
		parsed, err := parser.Parse(content)
		if err == nil && parsed != nil {
			parserContent = parsed
			// 对齐 Python P9: 解析成功记录
			log.Info().
				Str("event_type", "LLM_CALL_END").
				Str("model_name", modelName).
				Str("model_provider", modelProvider).
				Bool("is_stream", false).
				Any("parser_content", parserContent).
				Msg("Parser parse success.")
		} else if err != nil {
			// 对齐 Python P10: 解析错误记录
			log.Warn().
				Str("event_type", "LLM_CALL_ERROR").
				Str("model_name", modelName).
				Str("model_provider", modelProvider).
				Bool("is_stream", false).
				Err(err).
				Msg("Parser parse error.")
		}
	}

	// 处理 finish_reason
	finishReason := choice.FinishReason
	if len(toolCalls) > 0 && finishReason == "stop" {
		finishReason = "tool_calls"
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildUsageMetadata 从 OpenAI 响应的 Usage 构建 UsageMetadata。
func buildUsageMetadata(usage *ResponseUsage, modelName string) *llmschema.UsageMetadata {
	meta := llmschema.NewUsageMetadata()
	meta.ModelName = modelName
	meta.InputTokens = usage.PromptTokens
	meta.OutputTokens = usage.CompletionTokens
	meta.TotalTokens = usage.TotalTokens

	// 提取 cache_tokens
	if usage.PromptTokensDetails != nil {
		meta.CacheTokens = usage.PromptTokensDetails.CachedTokens
	}

	// 提取费用信息
	inputCost, outputCost, totalCost := extractCostFromUsage(usage)
	meta.InputCost = inputCost
	meta.OutputCost = outputCost
	meta.TotalCost = totalCost

	return meta
}

// extractCostFromUsage 从 OpenAI Usage 对象提取费用信息。
//
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
//
// 对应 Python: OpenAIModelClient._normalize_logprobs()
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
