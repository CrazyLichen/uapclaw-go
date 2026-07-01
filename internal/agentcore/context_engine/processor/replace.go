package processor

import (
	"sort"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Replacement 消息替换描述，指定消息列表中某范围替换为新消息。
//
// 对应 Python: ContextUtils.replace_messages() 的参数封装
type Replacement struct {
	// StartIdx 被替换范围的起始索引（含）
	StartIdx int
	// EndIdx 被替换范围的结束索引（含）
	EndIdx int
	// Messages 替换后的消息列表
	Messages []llm_schema.BaseMessage
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ReplaceMessages 将消息列表中指定范围替换为新消息。
//
// 从后往前处理（避免索引偏移），每个 Replacement 将
// messages[startIdx:endIdx+1] 替换为 replacement.Messages。
//
// 对应 Python: ContextUtils.replace_messages() + DialogueCompressor._apply_replacements()
// ⤵️ 5.23-5.29 其他处理器均可复用此函数
func ReplaceMessages(messages []llm_schema.BaseMessage, replacements []Replacement) []llm_schema.BaseMessage {
	if len(replacements) == 0 {
		return messages
	}

	// 按 StartIdx 降序排序，从后往前替换避免索引偏移
	sorted := make([]Replacement, len(replacements))
	copy(sorted, replacements)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartIdx > sorted[j].StartIdx
	})

	updated := make([]llm_schema.BaseMessage, len(messages))
	copy(updated, messages)

	for _, r := range sorted {
		if r.StartIdx < 0 || r.EndIdx >= len(updated) || r.StartIdx > r.EndIdx {
			continue // 跳过无效索引
		}
		// messages[:start] + replacement + messages[end+1:]
		replacement := make([]llm_schema.BaseMessage, len(r.Messages))
		copy(replacement, r.Messages)
		updated = append(updated[:r.StartIdx], append(replacement, updated[r.EndIdx+1:]...)...)
	}

	return updated
}
