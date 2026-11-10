package prompts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Manager 会话级动态提示词片段存储。
// Python: PromptAttachmentManager（in-memory，per-session bucket）
type Manager struct {
	// mu 保护 items
	mu sync.RWMutex
	// histMu 历史同步独立锁（与 mu 不嵌套，避免 rail 写入等待历史 IO）
	histMu sync.Mutex
	// items session → section → item
	items map[string]map[string]*Attachment
	// language 渲染语言（cn/en）
	language string
}

// AttachmentUpdate 允许修改的字段，nil 表示不改。
// Python: PromptAttachmentUpdate（exclude_unset 语义）
type AttachmentUpdate struct {
	// Kind 片段类型
	Kind *AttachmentKind
	// Content 片段内容
	Content *string
	// Priority 渲染优先级
	Priority *int
	// Source 写入方
	Source *string
	// ExpiresAt 过期时间
	ExpiresAt *string
	// Metadata 元数据（整段替换，merge 语义由 UpdateMetadataByID 负责）
	Metadata map[string]any
	// ContentKind 内容类型
	ContentKind *string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// unsafeIDPattern 匹配 id 中的非法字符
var unsafeIDPattern = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewManager 创建管理器。
func NewManager() *Manager {
	// 偏差说明：上游构造函数默认 language="en"，本项目提示词语言缺省为 cn，
	// 由 DeepAgent/Adapter 在装配时通过 SetLanguage 覆写。
	return &Manager{items: map[string]map[string]*Attachment{}, language: "cn"}
}

// AddSection 新增或覆盖一个 section。
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
// Python: clear_source
func (m *Manager) ClearSource(source, sessionID string) (int, error) {
	return m.RemoveByFilter(AttachmentFilter{Source: source, SessionID: sessionID})
}

// RemoveByFilter 按过滤器删除；无过滤器且未 AllowAll 时拒绝（破坏性守卫）。
// Python: remove_by_filter + _validate_destructive_filter
func (m *Manager) RemoveByFilter(f AttachmentFilter) (int, error) {
	if f.SessionID == "" && f.Section == "" && f.Kind == "" && f.Source == "" && !f.AllowAll {
		return 0, fmt.Errorf("destructive prompt attachment operation requires at least one filter")
	}
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

// GetByID 按 id 查询。
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

// Find 查询指定会话内某个来源的 section，返回首条（无则 nil）。
// 供运行时 rail 恢复"上次已物化的 section"，避免重复推送同一差异。
func (m *Manager) Find(sessionID, section, source string) *Attachment {
	items, err := m.ListByFilter(AttachmentFilter{
		SessionID: sessionID,
		Section:   section,
		Source:    source,
	})
	if err != nil || len(items) == 0 {
		return nil
	}
	return items[0]
}

// ClearSession 清空一个会话。
// Python: clear_session
func (m *Manager) ClearSession(sessionID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.items[sessionID])
	delete(m.items, sessionID)
	return n, nil
}

// ClearAll 清空全部会话。
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

// UpdateByID 按 id 更新；id/section/session/created_at 不可变。
// Python: update_by_id（不存在抛 KeyError → Go 返回 error）
func (m *Manager) UpdateByID(id string, u AttachmentUpdate) (*Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sessionID, section, current, ok := m.findByIDUnlocked(id)
	if !ok {
		return nil, fmt.Errorf("prompt attachment not found: %s", id)
	}
	next := *current
	if u.Kind != nil {
		next.Kind = *u.Kind
	}
	if u.Content != nil {
		next.Content = *u.Content
	}
	if u.Priority != nil {
		next.Priority = *u.Priority
	}
	if u.Source != nil {
		next.Source = *u.Source
	}
	if u.ExpiresAt != nil {
		next.ExpiresAt = *u.ExpiresAt
	}
	if u.ContentKind != nil {
		next.ContentKind = *u.ContentKind
	}
	if u.Metadata != nil {
		next.Metadata = u.Metadata
	}
	next.ID = current.ID
	next.Section = current.Section
	next.SessionID = current.SessionID
	next.CreatedAt = current.CreatedAt
	next.UpdatedAt = utcNow()
	next.ContentSHA256 = sha256Hex(next.Content)
	if next.Metadata == nil {
		next.Metadata = map[string]any{}
	}
	next.Metadata["section"] = next.Section
	if next.Source != "" {
		if _, ok := next.Metadata["source"]; !ok {
			next.Metadata["source"] = next.Source
		}
	}
	m.items[sessionID][section] = &next
	cp := next
	return &cp, nil
}

// RemoveByID 按 id 删除，sessionID 非空时校验归属。
// Python: remove_by_id
func (m *Manager) RemoveByID(id, sessionID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	foundSession, section, _, ok := m.findByIDUnlocked(id)
	if !ok {
		return false, nil
	}
	if sessionID != "" && foundSession != sessionID {
		return false, nil
	}
	delete(m.items[foundSession], section)
	if len(m.items[foundSession]) == 0 {
		delete(m.items, foundSession)
	}
	return true, nil
}

// UpdateContentByID 只改内容（可带 content_kind）。
// Python: update_content_by_id
func (m *Manager) UpdateContentByID(id, content, sessionID string, contentKind *string) (*Attachment, error) {
	current, err := m.GetByID(id, sessionID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("prompt attachment not found: %s", id)
	}
	return m.UpdateByID(id, AttachmentUpdate{Content: &content, ContentKind: contentKind})
}

// UpdateMetadataByID 更新 metadata；merge=false 时整体替换。
// Python: update_metadata_by_id
func (m *Manager) UpdateMetadataByID(id string, metadata map[string]any, sessionID string, merge bool) (*Attachment, error) {
	current, err := m.GetByID(id, sessionID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("prompt attachment not found: %s", id)
	}
	next := map[string]any{}
	if merge {
		for k, v := range current.Metadata {
			next[k] = v
		}
	}
	for k, v := range metadata {
		next[k] = v
	}
	return m.UpdateByID(id, AttachmentUpdate{Metadata: next})
}

// ReplaceSource 先删同来源再整体重写。
// Python: replace_source
func (m *Manager) ReplaceSource(source string, items []*Attachment, sessionID string) ([]*Attachment, error) {
	if _, err := m.RemoveByFilter(AttachmentFilter{Source: source, SessionID: sessionID}); err != nil {
		return nil, err
	}
	added := make([]*Attachment, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		sid := item.SessionID
		if sessionID != "" {
			sid = sessionID
		}
		opts := []AttachmentOption{
			WithAttachmentMetadata(item.Metadata),
			WithAttachmentContentKind(item.ContentKind),
			WithAttachmentExpiresAt(item.ExpiresAt),
		}
		if item.Priority != 0 {
			opts = append(opts, WithAttachmentPriority(item.Priority))
		}
		got, err := m.AddSection(sid, item.Section, item.Content, item.Kind, source, opts...)
		if err != nil {
			return nil, err
		}
		added = append(added, got)
	}
	return added, nil
}

// AddFileReference 以文件引用形式新增一段（content 为摘要或路径）。
// Python: add_file_reference
func (m *Manager) AddFileReference(sessionID, filePath, summary, section, source string) (*Attachment, error) {
	meta := map[string]any{"file_path": filePath}
	if section == "" {
		section = "file_" + safeIDPart(filePath, "file")
	}
	if source == "" {
		source = "file_reference"
	}
	content := summary
	if content == "" {
		content = "File reference: " + filePath
	}
	return m.AddSection(sessionID, section, content, AttachmentFile, source,
		WithAttachmentMetadata(meta), WithAttachmentContentKind("text/markdown"))
}

// AddFromPromptSection 从 PromptSection 迁移一段（S2 rail 改造入口）。
// Python: PromptAttachmentContextWriter.add_from_prompt_section
func (m *Manager) AddFromPromptSection(sessionID string, section *saprompt.PromptSection, kind AttachmentKind, source, language string, opts ...AttachmentOption) (*Attachment, error) {
	if section == nil {
		return nil, nil
	}
	// Python: content = render(language)（:241）；只取指定语言，不做跨语言兜底
	content := section.Render(language)
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	allOpts := opts
	if section.Priority != 0 {
		allOpts = append(allOpts, WithAttachmentPriority(section.Priority))
	}
	return m.AddSection(sessionID, section.Name, content, kind, source, allOpts...)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// safeIDPart 归一化 id 片段。
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
// Python: _make_section_id
func makeSectionID(sessionID, section string) string {
	return "session." + safeIDPart(sessionID, "session") + "." + safeIDPart(section, "section")
}

// utcNow 返回 UTC RFC3339 时间戳。
func utcNow() string { return time.Now().UTC().Format(time.RFC3339) }

// sortAttachments 按 (priority, source, section) 稳定排序。
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

// findByIDUnlocked 定位 item（调用方持锁）。
func (m *Manager) findByIDUnlocked(id string) (string, string, *Attachment, bool) {
	for sid, bucket := range m.items {
		for section, item := range bucket {
			if item != nil && item.ID == id {
				return sid, section, item, true
			}
		}
	}
	return "", "", nil, false
}
