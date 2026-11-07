package prompts

import (
	"context"
	"sort"
	"strings"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HistoryContext 同步所需的最小上下文能力。
// *context.SessionModelContext 天然满足；单测可用极简 fake 实现。
type HistoryContext interface {
	// SessionID 返回会话 id
	SessionID() string
	// GetMessages 读取消息（withHistory=true 含历史）
	GetMessages(size int, withHistory bool) ([]llmschema.BaseMessage, error)
	// AddMessages 追加消息
	AddMessages(ctx context.Context, messages []llmschema.BaseMessage, opts ...ceiface.Option) ([]llmschema.BaseMessage, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Sync 把当前附件状态同步进会话历史（首次快照 / 之后增量）。
// Python: PromptAttachmentManager.sync_to_context + ReActAgent._sync_prompt_attachments
// 失败由调用方决定是否告警；本方法不 panic。
func (m *Manager) Sync(ctx context.Context, hc HistoryContext) error {
	if hc == nil {
		return nil
	}
	m.histMu.Lock()
	defer m.histMu.Unlock()

	sessionID := hc.SessionID()
	items, err := m.Collect(sessionID)
	if err != nil {
		return err
	}
	current := stateBySection(items)
	previous, hasSnapshot := readHistoryState(hc, sessionID)

	mode := ""
	var rendered string
	if !hasSnapshot {
		if len(items) == 0 {
			return nil
		}
		mode = ModeSnapshot
		rendered = m.RenderSnapshot(items)
	} else {
		changed := diffSections(current, previous)
		removed := removedSections(previous, current)
		if len(changed) == 0 && len(removed) == 0 {
			return nil
		}
		mode = ModeDelta
		rendered = m.RenderDelta(pickSections(items, changed), removed)
	}
	if rendered == "" {
		return nil
	}

	msg := llmschema.NewUserMessage(rendered, llmschema.WithMetadata(map[string]any{
		MetadataKeyAttachmentHistory: true,
		MetadataKeyMode:              mode,
		MetadataKeySessionID:         sessionID,
		MetadataKeyState:             current,
	}))
	if _, err := hc.AddMessages(ctx, []llmschema.BaseMessage{msg}); err != nil {
		return err
	}
	return nil
}

// HasHistorySnapshot 判断会话历史里是否已有完整快照。
// 运行时 rail 用它决定"上下文压缩/重建后必须改发全量快照"，避免孤立 delta。
// Python: PromptAttachmentManager.has_history_snapshot
func (m *Manager) HasHistorySnapshot(hc HistoryContext, sessionID string) bool {
	if hc == nil {
		return false
	}
	_, hasSnapshot := readHistoryState(hc, sessionID)
	return hasSnapshot
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readHistoryState 从历史消息里读最后一次交付账本。
// Python: _read_history_state。state 兼容 string 与 JSON 反序列化后的 any 两种形态。
func readHistoryState(hc HistoryContext, sessionID string) (map[string]string, bool) {
	msgs, err := hc.GetMessages(0, true)
	if err != nil {
		return nil, false
	}
	state := map[string]string{}
	hasSnapshot := false
	for _, msg := range msgs {
		meta := msg.GetMetadata()
		if meta == nil || meta[MetadataKeyAttachmentHistory] != true {
			continue
		}
		if sid, ok := meta[MetadataKeySessionID].(string); ok && sid != "" && sid != sessionID {
			continue
		}
		if parsed := parseState(meta[MetadataKeyState]); parsed != nil {
			state = parsed
		}
		if meta[MetadataKeyMode] == ModeSnapshot {
			hasSnapshot = true
		}
	}
	return state, hasSnapshot
}

// parseState 兼容 map[string]string 与 map[string]any（JSON 反序列化形态）。
func parseState(raw any) map[string]string {
	switch v := raw.(type) {
	case nil:
		return nil
	case map[string]string:
		out := make(map[string]string, len(v))
		for k, val := range v {
			out[k] = val
		}
		return out
	case map[string]any:
		out := make(map[string]string, len(v))
		for k, val := range v {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
		return out
	default:
		return nil
	}
}

// diffSections 返回内容变化的 section（排序稳定）。
func diffSections(current, previous map[string]string) []string {
	out := make([]string, 0)
	for section, hash := range current {
		if previous[section] != hash {
			out = append(out, section)
		}
	}
	sort.Strings(out)
	return out
}

// removedSections 返回被移除的 section（排序稳定）。
func removedSections(previous, current map[string]string) []string {
	out := make([]string, 0)
	for section := range previous {
		if _, ok := current[section]; !ok {
			out = append(out, section)
		}
	}
	sort.Strings(out)
	return out
}

// pickSections 按 section 列表挑出对应条目。
func pickSections(items []*Attachment, sections []string) []*Attachment {
	want := make(map[string]struct{}, len(sections))
	for _, s := range sections {
		want[s] = struct{}{}
	}
	out := make([]*Attachment, 0, len(sections))
	for _, item := range items {
		if item == nil {
			continue
		}
		if _, ok := want[item.Section]; ok {
			out = append(out, item)
		}
	}
	return out
}

// ──────────────────────────── 附件消息识别 ────────────────────────────

// IsPromptAttachmentUser 判断消息是否为附件投递产生的伪用户消息。
// Python: agent_evolving/trajectory/messages.py:104-119 `_is_prompt_attachment_user`
// 判定顺序：metadata 标记优先；其次内容含 "<system-reminder>"（兼容丢失 metadata 的形态）。
func IsPromptAttachmentUser(msg llmschema.BaseMessage) bool {
	if msg == nil {
		return false
	}
	if meta := msg.GetMetadata(); meta != nil {
		if marked, ok := meta[MetadataKeyAttachmentHistory].(bool); ok && marked {
			return true
		}
	}
	return strings.Contains(msg.GetContent().Text(), "<system-reminder>")
}

// IsPromptAttachmentHistory 判断是否为附件历史消息（marker + 合法 mode 双重判定）。
// 用于观测层区分「稳定系统提示」与「附件投影后的 system」。
func IsPromptAttachmentHistory(msg llmschema.BaseMessage) bool {
	if msg == nil {
		return false
	}
	meta := msg.GetMetadata()
	if meta == nil {
		return false
	}
	if marked, ok := meta[MetadataKeyAttachmentHistory].(bool); !ok || !marked {
		return false
	}
	mode, _ := meta[MetadataKeyMode].(string)
	return mode == ModeSnapshot || mode == ModeDelta
}
