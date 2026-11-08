package openai

import (
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseStreamChunk 将 OpenAI ChatCompletionChunkResponse 转换为 AssistantMessageChunk。
// Python: OpenAIModelClient._parse_stream_chunk()
//
// 返回 nil 表示该 chunk 应被跳过（无 choices 且无 usage 的空块）。
// 处理逻辑：
//  1. 先检查 usage（即使无 choices 也要提取 usage）
//  2. 提取 prompt_token_ids（vLLM 扩展，可能出现在任何 chunk）
//  3. 无 choices 且无 usage/prompt_token_ids → 返回 nil
//  4. 有 choices → 提取 delta.content, delta.reasoning_content, delta.tool_calls
//  5. 提取 token_ids, logprobs
func ParseStreamChunk(
	chunk *ChatCompletionChunkResponse,
	modelConfig *llmschema.ModelRequestConfig,
) *llmschema.AssistantMessageChunk {
	// 提取 usage
	var usageMetadata *llmschema.UsageMetadata
	if chunk.Usage != nil {
		usageMetadata = buildUsageMetadata(chunk.Usage, modelConfig.ModelName)
	}

	// 提取 prompt_token_ids（vLLM 扩展）
	var promptTokenIDs []int
	if len(chunk.PromptTokenIDs) > 0 {
		promptTokenIDs = chunk.PromptTokenIDs
	}

	// 无 choices 的场景：usage-only 块或空块
	if len(chunk.Choices) == 0 {
		if usageMetadata != nil || len(promptTokenIDs) > 0 {
			return llmschema.NewAssistantMessageChunk(
				"",
				llmschema.WithChunkUsageMetadata(usageMetadata),
				llmschema.WithChunkFinishReason(llmschema.FinishReasonNull),
				llmschema.WithChunkPromptTokenIDs(promptTokenIDs),
			)
		}
		// 真正的空块，跳过
		return nil
	}

	choice := chunk.Choices[0]
	delta := choice.Delta

	// 提取 content
	content := ""
	if delta != nil && delta.Content != nil {
		content = *delta.Content
	}

	// 提取推理内容（四级回退：reasoning_details[0].text → reasoning_content
	// → reasoning（OpenRouter）→ reasoning_token_text）
	// Python: openai_model_client.py:2751-2763 _extract_reasoning_content
	reasoningContent := extractReasoningFromDelta(delta)

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
				llmschema.ToolCallDeltaOptions(index, tcDelta.Type)...,
			))
		}
	}

	// 提取 finish_reason
	finishReason := llmschema.FinishReasonNull
	if choice.FinishReason != nil && *choice.FinishReason != "" {
		finishReason = *choice.FinishReason
	}

	// 提取 completion_token_ids（vLLM 扩展）
	var completionTokenIDs []int
	if len(choice.TokenIDs) > 0 {
		completionTokenIDs = choice.TokenIDs
	} else if delta != nil && len(delta.TokenIDs) > 0 {
		completionTokenIDs = delta.TokenIDs
	}

	// 处理 logprobs
	logprobs := normalizeLogprobs(choice.Logprobs)

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
	if len(promptTokenIDs) > 0 {
		opts = append(opts, llmschema.WithChunkPromptTokenIDs(promptTokenIDs))
	}
	if len(completionTokenIDs) > 0 {
		opts = append(opts, llmschema.WithChunkCompletionTokenIDs(completionTokenIDs))
	}
	if logprobs != nil {
		opts = append(opts, llmschema.WithChunkLogprobs(logprobs))
	}

	return llmschema.NewAssistantMessageChunk(content, opts...)
}

// extractReasoningFromDelta 将 delta 转 map 后走统一四级回退抽取。
func extractReasoningFromDelta(delta *ChunkDelta) string {
	if delta == nil {
		return ""
	}
	data, err := json.Marshal(delta)
	if err != nil {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return ""
	}
	return model_clients.ExtractReasoningContent(fields)
}
