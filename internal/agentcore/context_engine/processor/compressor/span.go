package compressor

import (
	"strings"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// compressionSpan 一次压缩的可压缩跨度：上下文被切成「保留前缀 / 待压缩中段 / 受保护尾巴」三段。
// 判定与执行共用同一份切分结果，避免"判定说可压、执行却切出空段"。
// Python: forked/compressor/base.py (PrefixCompactSpan)
type compressionSpan struct {
	// preservedPrefix 原样保留的前缀（如本轮用户消息之前的历史锚点）
	preservedPrefix []llm_schema.BaseMessage
	// messagesToCompress 会被摘要替换的中段
	messagesToCompress []llm_schema.BaseMessage
	// protectedTail 原样保留的尾巴（正在进行的回合、最近若干条）
	protectedTail []llm_schema.BaseMessage
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// internalUserMessagePrefixes 内部注入的用户消息前缀（不是真实用户输入）。
// 这些消息承载记忆块、状态重放、恢复上下文，若当成回合起点会把正在进行的回合切断。
// Python: forked/compressor/support/util.py (INTERNAL_USER_PREFIXES)
var internalUserMessagePrefixes = []string{
	"<system-reminder>",
	"<memory_block_current>",
	"<memory_block_dialogue>",
	"<memory_block_round>",
	"<memory_block_session>",
	"<recovered_context>",
	"[STATE_REINJECTION]",
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasTarget 判断跨度里是否存在可压缩内容。
// 中段为空表示没东西可压（例如最近一条用户消息就是唯一内容），压缩器应直接跳过。
func (s compressionSpan) hasTarget() bool {
	return len(s.messagesToCompress) > 0
}

// adjustKeepRecentForToolBoundaries 把"保留尾部"的起点往前挪到工具调用边界。
// 若切点落在 assistant(tool_calls) 与其 tool 结果之间，压缩后工具调用会失去配对，
// 上游请求直接判 400。做法：反复检查被保护尾部里的 tool 结果 id，
// 若某个 assistant 的 tool_call id 与之相交且在切点之前，就把切点前移到那条消息。
// Python: forked/compressor/base.py (adjust_keep_recent_for_tool_boundaries)
func adjustKeepRecentForToolBoundaries(messages []llm_schema.BaseMessage, keepRecent int) int {
	if keepRecent <= 0 || len(messages) == 0 {
		if keepRecent < 0 {
			return 0
		}
		return keepRecent
	}

	start := len(messages) - keepRecent
	if start < 0 {
		start = 0
	}
	for {
		protected := toolResultIDs(messages[start:])
		if len(protected) == 0 {
			break
		}
		adjusted := start
		for index, message := range messages[:start] {
			assistant, ok := message.(*llm_schema.AssistantMessage)
			if !ok {
				continue
			}
			for _, call := range assistant.ToolCalls {
				if _, hit := protected[call.ID]; hit && index < adjusted {
					adjusted = index
				}
			}
		}
		if adjusted == start {
			break
		}
		start = adjusted
	}
	return len(messages) - start
}

// findLastRealUserMessageIndex 返回最后一条"真实"用户消息的下标（跳过内部注入的用户消息）。
// 内部注入的（如记忆块、摘要提示）以已知前缀开头，不能当作回合起点，否则会把
// 正在进行的回合切断。
// Python: forked/compressor/base.py (find_last_real_user_message_index)
func findLastRealUserMessageIndex(messages []llm_schema.BaseMessage) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if isRealUserMessage(messages[index]) {
			return index
		}
	}
	return -1
}

// buildDialogueSpan 按"最近一条真实用户消息"切分对话跨度。
// 中段 = 最近用户消息之前的全部历史；尾巴 = 最近这条用户消息及其后续（正在进行的回合）。
// Python: forked/compressor/dialogue_compressor.py (_build_span)
func buildDialogueSpan(messages []llm_schema.BaseMessage, splitIndex int) compressionSpan {
	if splitIndex < 0 {
		splitIndex = 0
	}
	if splitIndex > len(messages) {
		splitIndex = len(messages)
	}
	return compressionSpan{
		messagesToCompress: messages[:splitIndex],
		protectedTail:      messages[splitIndex:],
	}
}

// buildCurrentRoundSpan 按"本轮用户消息之后、保留尾部之前"切分当前轮跨度。
// 前缀 = 本轮用户消息本身及其之前（含本轮提问，不能被压掉）；中段 = 本轮中段；尾巴 = 最近保留量。
// Python: forked/compressor/current_round_compressor.py (_build_span)
func buildCurrentRoundSpan(messages []llm_schema.BaseMessage, splitIndex int) compressionSpan {
	lastUser := findLastRealUserMessageIndex(messages)
	if lastUser < 0 {
		return compressionSpan{protectedTail: messages[splitIndex:]}
	}
	targetStart := lastUser + 1
	if splitIndex <= targetStart {
		return compressionSpan{
			preservedPrefix: messages[:targetStart],
			protectedTail:   messages[targetStart:],
		}
	}
	return compressionSpan{
		preservedPrefix:    messages[:targetStart],
		messagesToCompress: messages[targetStart:splitIndex],
		protectedTail:      messages[splitIndex:],
	}
}

// toolResultIDs 收集尾部消息里的工具调用结果 id。
func toolResultIDs(messages []llm_schema.BaseMessage) map[string]struct{} {
	ids := make(map[string]struct{}, len(messages))
	for _, message := range messages {
		if tool, ok := message.(*llm_schema.ToolMessage); ok && tool.ToolCallID != "" {
			ids[tool.ToolCallID] = struct{}{}
		}
	}
	return ids
}

// isRealUserMessage 判断是否为真实用户消息（非内部注入）。
func isRealUserMessage(message llm_schema.BaseMessage) bool {
	user, ok := message.(*llm_schema.UserMessage)
	if !ok {
		return false
	}
	content := user.GetContent().Text()
	if content == "" {
		// 多模态用户消息也是真实用户输入
		return len(user.GetContent().Parts()) > 0
	}
	trimmed := strings.TrimLeft(content, " \t\r\n")
	for _, prefix := range internalUserMessagePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return false
		}
	}
	return true
}
