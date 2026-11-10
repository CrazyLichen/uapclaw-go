package prompts

// ──────────────────────────── 结构体 ────────────────────────────

// RailAttachmentProvider 提供会话级动态提示词管理器（由 DeepAgent 实现）。
type RailAttachmentProvider interface {
	// PromptAttachments 返回管理器（可为 nil）
	PromptAttachments() *Manager
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ManagerFor 取出 agent 绑定的动态提示词管理器；无管理器返回 nil。
// Python: getattr(agent, "prompt_attachment_manager", None)
func ManagerFor(agent any) *Manager {
	provider, ok := agent.(RailAttachmentProvider)
	if !ok {
		return nil
	}
	return provider.PromptAttachments()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 说明：历史上此处还有三个「bool 返回值」式的投递助手，把解析会话与投递揉在一起，
// 投递失败静默丢弃且各 rail 各写一套。2026-11-05 起统一由 ContextWriter
// （attachment_writer.go，对齐上游 PromptAttachmentContextWriter）承担，
// 旧助手已删除，不得再引入同类包装。
