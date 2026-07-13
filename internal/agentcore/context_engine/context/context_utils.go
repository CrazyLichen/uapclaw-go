package context

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ContextMessageIDKey 消息元数据中 ID 的键名
	ContextMessageIDKey = "context_message_id"
	// DefaultContextMaxTokens 默认最大上下文 token 数
	DefaultContextMaxTokens = 200000
)

// ──────────────────────────── 全局变量 ────────────────────────────

var ModelDefaultContextWindowTokens = map[string]int{
	"glm-5":             200000,
	"glm-4-long":        200000,
	"glm-4":             128000,
	"glm-4-9b-chat-1m":  1048576,
	"gpt-5.4":           1100000,
	"gpt-4o":            128000,
	"gpt-4o-mini":       128000,
	"gpt-4-turbo":       128000,
	"gpt-3.5-turbo":     16384,
	"deepseek-v3":       128000,
	"deepseek-chat":     65536,
	"claude-opus-4.6":   1000000,
	"claude-sonnet-4.6": 1000000,
	"claude-haiku-4.6":  200000,
	"gemini-3.1-pro":    2000000,
	"gemini-2.5-pro":    1000000,
	"gemini-2.5-flash":  1000000,
	"llama-4-maverick":  1000000,
	"llama-4-scout":     10000000,
	"qwen-max":          32000,
	"qwen-plus":         131072,
	"qwen-turbo":        8192,
	"qwen-long":         1000000,
}

// ──────────────────────────── 导出函数 ────────────────────────────

func ValidateMessages(messages []llm_schema.BaseMessage) error {
	for i, msg := range messages {
		if msg == nil {
			return exception.NewBaseError(exception.StatusContextMessageInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("消息列表中索引 %d 的元素为 nil", i)),
			)
		}
	}
	return nil
}

func EnsureContextMessageIDs(messages []llm_schema.BaseMessage) []llm_schema.BaseMessage {
	for _, msg := range messages {
		metadata := msg.GetMetadata()
		if metadata == nil {
			metadata = make(map[string]any)
		}
		if _, exists := metadata[ContextMessageIDKey]; !exists {
			metadata[ContextMessageIDKey] = uuid.New().String()
			msg.SetMetadata(metadata)
		}
	}
	return messages
}

func ValidateAndFixContextWindow(window *iface.ContextWindow) {
	messages := window.ContextMessages
	if len(messages) == 0 {
		return
	}

	// 找到第一个非 ToolMessage 的索引
	firstNonToolIdx := -1
	for i, msg := range messages {
		if msg.GetRole() != llm_schema.RoleTypeTool {
			firstNonToolIdx = i
			break
		}
	}

	if firstNonToolIdx == -1 {
		// 全部是 ToolMessage → 清空
		window.ContextMessages = make([]llm_schema.BaseMessage, 0)
	} else if firstNonToolIdx > 0 {
		// 开头有 ToolMessage → 截掉开头的 ToolMessage
		window.ContextMessages = messages[firstNonToolIdx:]
	}
}

func ResolveContextMax(modelName string, fallbackContextWindowTokens int, modelContextWindowTokens map[string]int) int {
	// 优先级 1：fallback > 0 直接返回
	if fallbackContextWindowTokens > 0 {
		return fallbackContextWindowTokens
	}

	// 优先级 2 和 3：modelName 非空时查找
	if modelName != "" {
		// 优先级 2：自定义映射
		if modelContextWindowTokens != nil {
			if tokens, ok := modelContextWindowTokens[modelName]; ok {
				return tokens
			}
		}
		// 优先级 3：内置映射
		if tokens, ok := ModelDefaultContextWindowTokens[modelName]; ok {
			return tokens
		}
	}

	// 优先级 4：默认值
	return DefaultContextMaxTokens
}

func IsCompressionProcessor(p iface.ContextProcessor) bool {
	processorType := strings.ToLower(p.ProcessorType())
	return strings.Contains(processorType, "compressor") || strings.Contains(processorType, "compact")
}

func FormatReloadedMessages(offloadHandle string, messages []llm_schema.BaseMessage) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "reload messages with handle=%s:\n", offloadHandle)
	for i, msg := range messages {
		dump := messageToMap(msg)
		msgJSON, err := json.Marshal(dump)
		if err != nil {
			fmt.Fprintf(&sb, "message %d: {serialization failed}", i+1)
		} else {
			fmt.Fprintf(&sb, "message %d: %s", i+1, string(msgJSON))
		}
		if i != len(messages)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// ──────────────────────────── 导出函数 ────────────────────────────

// FindLastNDialogueRound 找到倒数第 n 轮对话的起始消息索引。
//
// 对应 Python: ContextUtils.find_last_n_dialogue_round()
func FindLastNDialogueRound(messages []llm_schema.BaseMessage, n int) int {
	rounds := processor.FindAllDialogueRound(messages)
	if len(rounds) == 0 {
		return -1
	}

	// rounds 从新到旧排列，取 min(n, len(rounds))-1 得到目标轮次索引
	targetIdx := n
	if targetIdx > len(rounds) {
		targetIdx = len(rounds)
	}
	targetRound := rounds[targetIdx-1]

	// 返回 user 消息索引，如果 userIdx 为 nil → 返回 -1
	userIdx := targetRound[0]
	if userIdx == nil {
		return -1
	}
	return *userIdx
}

// FindLastAIAbsentToolCall 从后往前查找最后一条不含 ToolCalls 的 AssistantMessage 索引。
//
// 对应 Python: ContextUtils.find_last_ai_message_without_tool_call()
func FindLastAIAbsentToolCall(messages []llm_schema.BaseMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.GetRole() == llm_schema.RoleTypeAssistant {
			am, ok := msg.(*llm_schema.AssistantMessage)
			if ok && len(am.ToolCalls) == 0 {
				return i
			}
		}
	}
	return -1
}

// FindMessageIndexByContextMessageID 根据消息元数据中的 context_message_id 查找消息索引。
//
// 返回第一条匹配的消息索引，未找到返回 -1。
//
// 对应 Python: ContextUtils.find_message_index_by_context_message_id()
func FindMessageIndexByContextMessageID(messages []llm_schema.BaseMessage, id string) int {
	for i, msg := range messages {
		metadata := msg.GetMetadata()
		if metadata == nil {
			continue
		}
		if msgID, ok := metadata[ContextMessageIDKey].(string); ok && msgID == id {
			return i
		}
	}
	return -1
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func messageToMap(msg llm_schema.BaseMessage) map[string]any {
	result := map[string]any{
		"role":    msg.GetRole().String(),
		"content": msg.GetContent().String(),
	}
	if name := msg.GetName(); name != "" {
		result["name"] = name
	}
	if metadata := msg.GetMetadata(); len(metadata) > 0 {
		result["metadata"] = metadata
	}
	// AssistantMessage 特有字段
	if am, ok := msg.(*llm_schema.AssistantMessage); ok {
		if len(am.ToolCalls) > 0 {
			toolCalls := make([]map[string]any, 0, len(am.ToolCalls))
			for _, call := range am.ToolCalls {
				tc := map[string]any{
					"id":   call.ID,
					"type": call.Type,
					"function": map[string]any{
						"name":      call.Name,
						"arguments": call.Arguments,
					},
				}
				toolCalls = append(toolCalls, tc)
			}
			result["tool_calls"] = toolCalls
		}
		if am.UsageMetadata != nil {
			result["usage_metadata"] = am.UsageMetadata
		}
		if am.FinishReason != "" && am.FinishReason != "null" {
			result["finish_reason"] = am.FinishReason
		}
		if am.ParserContent != nil {
			result["parser_content"] = am.ParserContent
		}
		if am.ReasoningContent != "" {
			result["reasoning_content"] = am.ReasoningContent
		}
		if len(am.PromptTokenIDs) > 0 {
			result["prompt_token_ids"] = am.PromptTokenIDs
		}
		if len(am.CompletionTokenIDs) > 0 {
			result["completion_token_ids"] = am.CompletionTokenIDs
		}
		if am.Logprobs != nil {
			result["logprobs"] = am.Logprobs
		}
	}
	// ToolMessage 特有字段
	if tm, ok := msg.(*llm_schema.ToolMessage); ok {
		if tm.ToolCallID != "" {
			result["tool_call_id"] = tm.ToolCallID
		}
	}
	return result
}
