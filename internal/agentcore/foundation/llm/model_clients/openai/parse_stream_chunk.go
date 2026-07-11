package openai

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseStreamChunk 将 OpenAI ChatCompletionChunkResponse 转换为 AssistantMessageChunk。
//
// 对应 Python: OpenAIModelClient._parse_stream_chunk()
//
// 返回 nil 表示该 chunk 应被跳过（无 choices 且无 usage 的空块）。
//
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
