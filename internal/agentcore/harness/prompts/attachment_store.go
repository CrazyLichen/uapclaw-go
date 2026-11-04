package prompts

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Manager 会话级动态提示词片段存储。
//
// Python: PromptAttachmentManager（in-memory，per-session bucket）
type Manager struct {
	// mu 保护 items
	mu sync.RWMutex
	// histMu 历史同步独立锁（与 mu 不嵌套，避免 rail 写入等待历史 IO）
	histMu sync.Mutex
	// items session → section → item
	items map[string]map[string]*Attachment
}

// ──────────────────────────── 全局变量 ────────────────────────────

// unsafeIDPattern 匹配 id 中的非法字符
var unsafeIDPattern = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewManager 创建管理器。
func NewManager() *Manager {
	return &Manager{items: map[string]map[string]*Attachment{}}
}

// AddSection 新增或覆盖一个 section。
//
// Python: add_section + _add + _normalize_for_write
func (m *Manager) AddSection(sessionID, section, content string, kind AttachmentKind, source string, opts ...AttachmentOption) (*Attachment, error) {
	a := &Attachment{
		Section:     section,
		Content:     content,
		Kind:        kind,
		Priority:    DefaultAttachmentPriority,
		Source:      source,
		SessionID:   sessionID,
		ContentKind: DefaultAttachmentContentKind,
		Metadata:    map[string]any{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	if err := validateAttachment(a); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	bucket := m.items[a.SessionID]
	if bucket == nil {
		bucket = map[string]*Attachment{}
		m.items[a.SessionID] = bucket
	}
	now := utcNow()
	if existing, ok := bucket[a.Section]; ok && existing != nil {
		a.CreatedAt = existing.CreatedAt
	} else {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	a.ID = makeSectionID(a.SessionID, a.Section)
	a.ContentSHA256 = sha256Hex(a.Content)
	if a.Metadata == nil {
		a.Metadata = map[string]any{}
	}
	a.Metadata["section"] = a.Section
	if a.Source != "" {
		if _, ok := a.Metadata["source"]; !ok {
			a.Metadata["source"] = a.Source
		}
	}
	bucket[a.Section] = a
	cp := *a
	return &cp, nil
}

// ClearSection 删除一个 section，返回删除条数。
//
// Python: clear_section
func (m *Manager) ClearSection(sessionID, section string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket := m.items[sessionID]
	if bucket == nil {
		return 0, nil
	}
	if _, ok := bucket[section]; !ok {
		return 0, nil
	}
	delete(bucket, section)
	if len(bucket) == 0 {
		delete(m.items, sessionID)
	}
	return 1, nil
}

// ClearSource 清空某来源（可限定会话）。
//
// Python: clear_source
func (m *Manager) ClearSource(source, sessionID string) (int, error) {
	return m.RemoveByFilter(AttachmentFilter{Source: source, SessionID: sessionID})
}

// RemoveByFilter 按过滤器删除。
//
// Python: remove_by_filter
func (m *Manager) RemoveByFilter(f AttachmentFilter) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := 0
	for sid, bucket := range m.items {
		if f.SessionID != "" && sid != f.SessionID {
			continue
		}
		for section, item := range bucket {
			if item == nil {
				continue
			}
			if f.Section != "" && item.Section != f.Section {
				continue
			}
			if f.Kind != "" && item.Kind != f.Kind {
				continue
			}
			if f.Source != "" && item.Source != f.Source {
				continue
			}
			delete(bucket, section)
			removed++
		}
		if len(bucket) == 0 {
			delete(m.items, sid)
		}
	}
	return removed, nil
}

// Collect 返回会话可见（未过期）的附件，按 (priority, source, section) 稳定排序。
//
// Python: collect_for_session
func (m *Manager) Collect(sessionID string) ([]*Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket := m.items[sessionID]
	if bucket == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	out := make([]*Attachment, 0, len(bucket))
	for section, item := range bucket {
		if item == nil {
			continue
		}
		if item.ExpiresAt != "" {
			if t, err := time.Parse(time.RFC3339, item.ExpiresAt); err == nil && t.Before(now) {
				delete(bucket, section)
				continue
			}
		}
		cp := *item
		out = append(out, &cp)
	}
	if len(bucket) == 0 {
		delete(m.items, sessionID)
	}
	sortAttachments(out)
	return out, nil
}

// sortAttachments 按 (priority, source, section) 稳定排序。
//
// Python: _stable_sort
func sortAttachments(items []*Attachment) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		if items[i].Source != items[j].Source {
			return items[i].Source < items[j].Source
		}
		return items[i].Section < items[j].Section
	})
}

// GetByID 按 id 查询。
//
// Python: get_by_id
func (m *Manager) GetByID(id, sessionID string) (*Attachment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for sid, bucket := range m.items {
		if sessionID != "" && sid != sessionID {
			continue
		}
		for _, item := range bucket {
			if item != nil && item.ID == id {
				cp := *item
				return &cp, nil
			}
		}
	}
	return nil, nil
}

// ListByFilter 按过滤器查询；SessionID 为空时跨全部会话（对齐上游 session_id=None）。
//
// Python: list_by_filter
func (m *Manager) ListByFilter(f AttachmentFilter) ([]*Attachment, error) {
	m.mu.RLock()
	candidates := make([]*Attachment, 0, len(m.items))
	for sid, bucket := range m.items {
		if f.SessionID != "" && sid != f.SessionID {
			continue
		}
		for _, item := range bucket {
			if item == nil {
				continue
			}
			cp := *item
			candidates = append(candidates, &cp)
		}
	}
	m.mu.RUnlock()

	out := make([]*Attachment, 0, len(candidates))
	for _, item := range candidates {
		if f.Section != "" && item.Section != f.Section {
			continue
		}
		if f.Kind != "" && item.Kind != f.Kind {
			continue
		}
		if f.Source != "" && item.Source != f.Source {
			continue
		}
		out = append(out, item)
	}
	sortAttachments(out)
	return out, nil
}

// ClearSession 清空一个会话。
//
// Python: clear_session
func (m *Manager) ClearSession(sessionID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.items[sessionID])
	delete(m.items, sessionID)
	return n, nil
}

// ClearAll 清空全部会话。
//
// Python: clear_all
func (m *Manager) ClearAll() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for sid := range m.items {
		n += len(m.items[sid])
	}
	m.items = map[string]map[string]*Attachment{}
	return n, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// safeIDPart 归一化 id 片段。
//
// Python: _safe_id_part
func safeIDPart(raw, fallback string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback
	}
	s = unsafeIDPattern.ReplaceAllString(s, "_")
	s = strings.Trim(s, "._-")
	if s == "" {
		sum := sha256.Sum256([]byte(raw))
		return hex.EncodeToString(sum[:])[:12]
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// makeSectionID 生成 section id。
//
// Python: _make_section_id
func makeSectionID(sessionID, section string) string {
	return "session." + safeIDPart(sessionID, "session") + "." + safeIDPart(section, "section")
}

// utcNow 返回 UTC RFC3339 时间戳。
func utcNow() string { return time.Now().UTC().Format(time.RFC3339) }
