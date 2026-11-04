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

// ManagerFor 取出 agent 绑定的动态提示词管理器；无管理器返回 nil。
//
// Python: getattr(agent, "prompt_attachment_manager", None)
func ManagerFor(agent any) *Manager {
	provider, ok := agent.(RailAttachmentProvider)
	if !ok {
		return nil
	}
	return provider.PromptAttachments()
}

// Deliver 把 rail 构建的动态提示词投递为历史附件。
//
// Python: rail.before_model_call 中 attachment_manager.add_from_prompt_section；
// 无管理器时返回 false，由调用方回落到 system_prompt_builder.add_section。
func Deliver(agent any, sessionID string, section *saprompt.PromptSection, kind AttachmentKind, source, language string) bool {
	if section == nil || sessionID == "" {
		return false
	}
	manager := ManagerFor(agent)
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

// DeliverText 把纯文本片段投递为历史附件（同 section 覆盖写）。
//
// Python: attachment_manager.bind_context(ctx).add_section(section=..., content=...,
// kind=..., source=..., priority=..., content_kind="text/markdown")
// 无管理器时返回 false，由调用方自行回落。
func DeliverText(
	agent any,
	sessionID, section, content string,
	kind AttachmentKind,
	source string,
	priority int,
) bool {
	if sessionID == "" || section == "" || content == "" {
		return false
	}
	manager := ManagerFor(agent)
	if manager == nil {
		return false
	}
	opts := []AttachmentOption{WithAttachmentContentKind("text/markdown")}
	if priority != 0 {
		opts = append(opts, WithAttachmentPriority(priority))
	}
	if _, err := manager.AddSection(sessionID, section, content, kind, source, opts...); err != nil {
		logger.Warn(logger.ComponentAgentCore).Err(err).Str("source", source).Msg("attachment delivery failed")
		return false
	}
	return true
}

// ClearDeliveredSection 删除会话里已投递的片段；无管理器时返回 false。
//
// Python: attachment_manager.bind_context(ctx).clear_section(section)
func ClearDeliveredSection(agent any, sessionID, section string) bool {
	if sessionID == "" || section == "" {
		return false
	}
	manager := ManagerFor(agent)
	if manager == nil {
		return false
	}
	if _, err := manager.ClearSection(sessionID, section); err != nil {
		logger.Warn(logger.ComponentAgentCore).Err(err).Str("section", section).Msg("attachment clear failed")
		return false
	}
	return true
}
