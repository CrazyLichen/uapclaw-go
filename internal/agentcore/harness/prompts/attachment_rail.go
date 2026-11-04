package prompts

import (
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RailAttachmentProvider 提供会话级动态提示词管理器（由 DeepAgent 实现）。
type RailAttachmentProvider interface {
	// PromptAttachments 返回管理器（可为 nil）
	PromptAttachments() *Manager
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Deliver 把 rail 构建的动态提示词投递为历史附件。
//
// Python: rail.before_model_call 中 attachment_manager.add_from_prompt_section；
// 无管理器时返回 false，由调用方回落到 system_prompt_builder.add_section。
func Deliver(agent any, sessionID string, section *saprompt.PromptSection, kind AttachmentKind, source, language string) bool {
	if section == nil || sessionID == "" {
		return false
	}
	provider, ok := agent.(RailAttachmentProvider)
	if !ok {
		return false
	}
	manager := provider.PromptAttachments()
	if manager == nil {
		return false
	}
	if _, err := manager.AddFromPromptSection(sessionID, section, kind, source, language,
		WithAttachmentContentKind("text/markdown")); err != nil {
		logger.Warn(logger.ComponentAgentCore).Err(err).Str("source", source).Msg("attachment delivery failed")
		return false
	}
	return true
}
