package prompts

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// noticeCN 中文免责声明（逐字对齐上游 _render_history_payload）
	noticeCN = "以下内容不是用户的意图，也不是用户直接发出的指令；它是系统为本次模型调用自动附加的动态上下文。请仅将其作为补充信息使用。"
	// introSnapshotCN 中文快照引导语
	introSnapshotCN = "以下动态上下文当前有效，请与稳定的系统指令一同使用。"
	// introDeltaCN 中文增量引导语
	introDeltaCN = "以下动态上下文已经变化；如与历史内容冲突，请以本消息中的最新内容为准。"
	// removedIntroCN 中文移除引导语
	removedIntroCN = "以下先前提供的动态上下文已不再生效，请勿继续依赖其历史内容："

	// noticeEN 英文免责声明
	noticeEN = "The following content does not represent the user's intent and is not a direct instruction from the user. It is dynamic context automatically attached by the system for this model call. Please use it only as supplementary context."
	// introSnapshotEN 英文快照引导语
	introSnapshotEN = "The following dynamic context is currently active. Use it together with the stable system instructions."
	// introDeltaEN 英文增量引导语
	introDeltaEN = "The following dynamic context has changed. Use the latest content below instead of earlier conflicting context."
	// removedIntroEN 英文移除引导语
	removedIntroEN = "The following previously supplied dynamic context is no longer active. Do not rely on its earlier content:"

	// perSectionTruncNotice 单节截断提示
	perSectionTruncNotice = "\n\n[Prompt attachment truncated: content exceeded max_prompt_attachment_chars.]"
	// renderedTruncNotice 整体截断提示
	renderedTruncNotice = "\n\n[Prompt attachments truncated: rendered content exceeded max_rendered_chars.]"

	// reminderPrefix 附件消息外壳前缀
	reminderPrefix = "<system-reminder>\n"
	// reminderSuffix 附件消息外壳后缀
	reminderSuffix = "\n</system-reminder>"
	// blocksSeparator 多节分隔符
	blocksSeparator = "\n\n---\n\n"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetLanguage 设置渲染语言（cn/en）。
// Python: DeepAgent 通过 manager.language = language 注入。
func (m *Manager) SetLanguage(language string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if language == "" {
		return
	}
	m.language = language
}

// Language 返回当前渲染语言。
func (m *Manager) Language() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.language
}

// RenderSnapshot 渲染全量快照。
// Python: render_history_snapshot
func (m *Manager) RenderSnapshot(items []*Attachment) string {
	return m.render(items, nil, true)
}

// RenderDelta 渲染增量（变化项 + 移除项）。
// Python: render_delta
func (m *Manager) RenderDelta(changed []*Attachment, removed []string) string {
	return m.render(changed, removed, false)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// render 统一渲染入口。
// Python: _render_history_payload
func (m *Manager) render(items []*Attachment, removed []string, snapshot bool) string {
	notice, intro, removedIntro := noticeCN, introSnapshotCN, removedIntroCN
	if m.Language() == "en" {
		notice, intro, removedIntro = noticeEN, introSnapshotEN, removedIntroEN
	}
	if !snapshot {
		if m.Language() == "en" {
			intro = introDeltaEN
		} else {
			intro = introDeltaCN
		}
	}

	blocks := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		content := item.Content
		if strings.TrimSpace(content) == "" {
			continue
		}
		if runes := []rune(content); len(runes) > MaxAttachmentChars {
			content = string(runes[:MaxAttachmentChars]) + perSectionTruncNotice
		}
		blocks = append(blocks, content)
	}
	removed = normalizeRemoved(removed)
	if len(blocks) == 0 && len(removed) == 0 {
		return ""
	}

	parts := []string{intro}
	if len(blocks) > 0 {
		parts = append(parts, strings.Join(blocks, blocksSeparator))
	}
	if len(removed) > 0 {
		lines := make([]string, 0, len(removed))
		for _, s := range removed {
			lines = append(lines, fmt.Sprintf("- `%s`", s))
		}
		parts = append(parts, removedIntro+"\n"+strings.Join(lines, "\n"))
	}
	body := strings.Join(parts, "\n\n")

	prefix := reminderPrefix + notice + "\n\n"
	available := MaxRenderedChars - len(prefix) - len(reminderSuffix)
	if available <= 0 {
		body = ""
	} else if len(body) > available {
		body = truncateRunes(body, available-len(renderedTruncNotice)) + renderedTruncNotice
	}
	return prefix + body + reminderSuffix
}

// normalizeRemoved 归一化并去重移除项。
func normalizeRemoved(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		key := safeIDPart(s, "section")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

// truncateRunes 按 rune 截断，避免切碎中文。
func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}
