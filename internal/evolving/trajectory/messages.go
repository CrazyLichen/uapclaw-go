package trajectory

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TrimPromptToLastUser 只保留最后一条真实用户消息起的片段。
//
// Python: agent_evolving/trajectory/messages.py `_trim_prompt_to_last_user`
// 附件消息（prompts.IsPromptAttachmentUser）不算真实用户消息，不得作为切片起点；
// 真实用户之后的附件仍保留在返回片段里。
func TrimPromptToLastUser(prompt []llmschema.BaseMessage) []llmschema.BaseMessage {
	for i := len(prompt) - 1; i >= 0; i-- {
		msg := prompt[i]
		if msg == nil || msg.GetRole() != llmschema.RoleTypeUser {
			continue
		}
		if hprompts.IsPromptAttachmentUser(msg) {
			continue
		}
		out := make([]llmschema.BaseMessage, len(prompt)-i)
		copy(out, prompt[i:])
		return out
	}
	// 无真实用户消息时返回空片段（上游 `if last_user is None: return []`）
	return nil
}
