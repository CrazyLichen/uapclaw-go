package rails

import (
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// AttachmentKindRuntime 运行时指引类附件
	AttachmentKindRuntime = hprompts.AttachmentRuntime
	// AttachmentKindTool 工具规则类附件
	AttachmentKindTool = hprompts.AttachmentTool
	// AttachmentKindSkill 技能类附件
	AttachmentKindSkill = hprompts.AttachmentSkill
	// AttachmentKindMemory 记忆类附件
	AttachmentKindMemory = hprompts.AttachmentMemory
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DeliverAttachment 把动态提示词投递为历史附件；无管理器时回落 section。
//
// 语义见 prompts.Deliver（供本包 rail 便捷调用）。
func DeliverAttachment(
	agent any,
	sessionID string,
	section *saprompt.PromptSection,
	kind hprompts.AttachmentKind,
	source, language string,
) bool {
	return hprompts.Deliver(agent, sessionID, section, kind, source, language)
}
