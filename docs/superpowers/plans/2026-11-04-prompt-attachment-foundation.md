# Prompt Attachment 地基（S1）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Go 侧建成与上游 `PromptAttachmentManager` 等价的动态提示词投递地基（存储 / 渲染 / 哈希增量 / 窗口投影），不迁移任何 rail 消费方。

**Architecture:** `internal/agentcore/harness/prompts` 新增 5 个文件承载 manager 与渲染；`context_engine` 的 `ContextWindow` 增加 mutator 链按请求投影 role；ReAct 在「写用户消息前」和「构建模型窗口前」两处调用 `Sync`，由内容哈希决定发 snapshot / delta / 不发。

**Tech Stack:** Go 1.26 + `encoding/json`（禁用 HTML 转义）+ `crypto/sha256` + testify；集成测试用 `//go:build integration` + `suite.Run`。

**Spec:** `docs/superpowers/specs/2026-11-04-prompt-attachment-foundation-design.md`

---

## 文件结构

| 文件 | 职责 |
| --- | --- |
| `internal/agentcore/harness/prompts/attachment_types.go`（新建） | 类型、枚举、常量、错误、`AttachmentOption` |
| `internal/agentcore/harness/prompts/attachment_digest.go`（新建） | 规范化 JSON + 内容哈希 + state 提取 |
| `internal/agentcore/harness/prompts/attachment_store.go`（新建） | `Manager` 存储：增删改查、归一化、过期、排序 |
| `internal/agentcore/harness/prompts/attachment_render.go`（新建） | snapshot / delta / removed 渲染与两级截断 |
| `internal/agentcore/harness/prompts/attachment_delivery.go`（新建） | `HistoryContext` 抽象、`Sync`、commit callbacks、窗口 mutator、provider 矩阵 |
| `internal/agentcore/context_engine/interface/window.go`（新建） | `WindowMutator` 类型 + `WithWindowMutators` option |
| `internal/agentcore/context_engine/interface/types.go`（改） | `ContextWindow.Mutators` 字段 |
| `internal/agentcore/context_engine/context/session_model_context.go`（改） | 窗口返回前应用 mutator 链 |
| `internal/agentcore/single_agent/agents/react_model_call.go`（改） | 窗口构建前 `Sync` |
| `internal/agentcore/single_agent/agents/react_invoke.go`（改） | 写用户/补充消息前 `Sync` |
| `internal/agentcore/harness/prompts/doc.go`（改） | 文件树补 5 个新文件 |
| `tests/integration/agentcore/attachment/`（新建目录） | 7 例集成测试 + `doc.go` + `testrail.go` |

依赖方向检查：`harness/prompts` → `context_engine/interface` → `foundation/llm/schema`，均无环；`harness/prompts` 已导入 `single_agent/prompts`（`saprompt`），`AddFromPromptSection` 不新增方向。

---

### Task 1: 类型、常量与哈希

**Files:**
- Create: `internal/agentcore/harness/prompts/attachment_types.go`
- Create: `internal/agentcore/harness/prompts/attachment_digest.go`
- Test: `internal/agentcore/harness/prompts/attachment_digest_test.go`

- [ ] **Step 1: 写失败测试**

```go
package prompts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashPromptAttachment_忽略非可见字段(t *testing.T) {
	a := &Attachment{Section: "todo", Kind: AttachmentRuntime, Content: "内容", ContentKind: "text/markdown", Priority: 10, Source: "a"}
	b := &Attachment{Section: "todo", Kind: AttachmentRuntime, Content: "内容", ContentKind: "text/markdown", Priority: 99, Source: "b"}
	require.Equal(t, HashPromptAttachment(a), HashPromptAttachment(b), "priority/source 变化不应改变哈希")
}

func TestHashPromptAttachment_内容变化改变哈希(t *testing.T) {
	a := &Attachment{Section: "todo", Kind: AttachmentRuntime, Content: "A", ContentKind: "text/plain"}
	b := &Attachment{Section: "todo", Kind: AttachmentRuntime, Content: "B", ContentKind: "text/plain"}
	c := &Attachment{Section: "todo", Kind: AttachmentRuntime, Content: "A", ContentKind: "text/markdown"}
	require.NotEqual(t, HashPromptAttachment(a), HashPromptAttachment(b))
	require.NotEqual(t, HashPromptAttachment(a), HashPromptAttachment(c), "content_kind 参与哈希")
}

func TestCanonicalJSON_不转义HTML且无尾换行(t *testing.T) {
	got, err := canonicalJSON(map[string]any{"s": "<a>&b", "z": 1, "a": 2})
	require.NoError(t, err)
	require.Equal(t, `{"a":2,"s":"<a>&b","z":1}`, string(got))
}
```

- [ ] **Step 2: 运行确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/prompts/ -run 'TestHashPromptAttachment|TestCanonicalJSON' -v`
Expected: FAIL — `undefined: Attachment` / `undefined: canonicalJSON`

- [ ] **Step 3: 写实现**

`attachment_types.go`：

```go
package prompts

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// Attachment 会话级动态提示词片段。
//
// Python: PromptAttachment (prompt_attachment_manager.py:36-53)
type Attachment struct {
	ID            string
	Section       string
	Kind          AttachmentKind
	Content       string
	Priority      int
	Source        string
	SessionID     string
	CreatedAt     string
	UpdatedAt     string
	ExpiresAt     string
	Metadata      map[string]any
	ContentKind   string
	ContentPath   string
	ContentSHA256 string
}

// AttachmentOption Attachment 写入选项。
type AttachmentOption func(*Attachment)

// AttachmentFilter 查询/删除过滤器，零值字段表示不参与过滤。
type AttachmentFilter struct {
	SessionID string
	Section   string
	Kind      AttachmentKind
	Source    string
}

// ──────────────────────────── 枚举 ────────────────────────────

// AttachmentKind 附件类型。
//
// Python: PromptAttachmentKind
type AttachmentKind string

const (
	AttachmentGeneric        AttachmentKind = "generic"
	AttachmentText           AttachmentKind = "text"
	AttachmentRuntime        AttachmentKind = "runtime"
	AttachmentMemory         AttachmentKind = "memory"
	AttachmentFile           AttachmentKind = "file"
	AttachmentTool           AttachmentKind = "tool"
	AttachmentSkill          AttachmentKind = "skill"
	AttachmentDiagnostic     AttachmentKind = "diagnostic"
	AttachmentTodoReminder   AttachmentKind = "todo_reminder"
	AttachmentWorkspaceDelta AttachmentKind = "workspace_delta"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultAttachmentPriority 默认优先级
	DefaultAttachmentPriority = 100
	// DefaultAttachmentContentKind 默认内容类型
	DefaultAttachmentContentKind = "text/plain"
	// MaxAttachmentChars 单节截断阈值
	MaxAttachmentChars = 12000
	// MaxRenderedChars 整体渲染截断阈值
	MaxRenderedChars = 48000

	// MetadataKeyAttachmentHistory 附件消息身份标记
	MetadataKeyAttachmentHistory = "_openjiuwen_prompt_attachment_history"
	// MetadataKeyMode 附件消息模式（snapshot/delta）
	MetadataKeyMode = "mode"
	// MetadataKeySessionID 附件消息会话 id
	MetadataKeySessionID = "session_id"
	// MetadataKeyState 交付账本（section → hash）
	MetadataKeyState = "state"
	// ModeSnapshot 全量快照
	ModeSnapshot = "snapshot"
	// ModeDelta 增量
	ModeDelta = "delta"

	// CommitCallbacksExtraKey 投递成功回调的 extra 键
	CommitCallbacksExtraKey = "_openjiuwen_prompt_attachment_commit_callbacks"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithAttachmentPriority 设置优先级。
func WithAttachmentPriority(p int) AttachmentOption {
	return func(a *Attachment) { a.Priority = p }
}

// WithAttachmentMetadata 设置元数据。
func WithAttachmentMetadata(m map[string]any) AttachmentOption {
	return func(a *Attachment) {
		if a.Metadata == nil {
			a.Metadata = map[string]any{}
		}
		for k, v := range m {
			a.Metadata[k] = v
		}
	}
}

// WithAttachmentContentKind 设置内容类型。
func WithAttachmentContentKind(k string) AttachmentOption {
	return func(a *Attachment) { a.ContentKind = k }
}

// WithAttachmentExpiresAt 设置过期时间（UTC RFC3339）。
func WithAttachmentExpiresAt(t string) AttachmentOption {
	return func(a *Attachment) { a.ExpiresAt = t }
}

// WithAttachmentContentPath 设置内容文件路径。
func WithAttachmentContentPath(p string) AttachmentOption {
	return func(a *Attachment) { a.ContentPath = p }
}

// validateAttachment 写入前校验。
func validateAttachment(a *Attachment) error {
	if a.SessionID == "" {
		return fmt.Errorf("prompt attachment requires session_id")
	}
	if a.Section == "" {
		return fmt.Errorf("prompt attachment requires section")
	}
	return nil
}
```

`attachment_digest.go`：

```go
package prompts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// canonicalJSON 输出键排序、无空白、不转义 HTML 的 JSON。
//
// 关键：Go 的 json.Marshal 会把 < > & 转成 \u003c 等，
// 与上游 json.dumps(ensure_ascii=False, sort_keys=True, separators=(",",":")) 不一致，
// 会导致同一内容算出不同哈希（每轮多发 delta）。
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// HashPromptAttachment 计算模型可见内容的稳定哈希。
//
// Python: hash_prompt_attachment（只覆盖 section/kind/content/content_kind）
func HashPromptAttachment(a *Attachment) string {
	if a == nil {
		return ""
	}
	payload := map[string]any{
		"section":      a.Section,
		"kind":         string(a.Kind),
		"content":      a.Content,
		"content_kind": a.ContentKind,
	}
	raw, err := canonicalJSON(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// stateBySection 生成 section → hash 账本。
func stateBySection(items []*Attachment) map[string]string {
	out := make(map[string]string, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out[item.Section] = HashPromptAttachment(item)
	}
	return out
}

// sortedSections 返回排序后的 section 列表（用于日志与断言稳定性）。
func sortedSections(state map[string]string) []string {
	out := make([]string, 0, len(state))
	for k := range state {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: 运行确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/prompts/ -run 'TestHashPromptAttachment|TestCanonicalJSON' -v`
Expected: PASS（3 个用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/prompts/attachment_types.go internal/agentcore/harness/prompts/attachment_digest.go internal/agentcore/harness/prompts/attachment_digest_test.go
git commit -m "feat(prompts): attachment 类型与内容哈希（规范化 JSON，禁 HTML 转义）"
```

---

### Task 2: 存储（增删改查 / 归一化 / 过期 / 排序）

**Files:**
- Create: `internal/agentcore/harness/prompts/attachment_store.go`
- Test: `internal/agentcore/harness/prompts/attachment_store_test.go`

- [ ] **Step 1: 写失败测试**

```go
package prompts

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSafeIDPart_边界(t *testing.T) {
	require.Equal(t, "session_1", safeIDPart("session 1", "x"))
	require.Len(t, safeIDPart(strings.Repeat("a", 200), "x"), 80)
	require.Len(t, safeIDPart("中文", "x"), 12, "全非法字符时回落 sha256[:12]")
}

func TestManager_AddSection_ID与归一化(t *testing.T) {
	m := NewManager()
	a, err := m.AddSection("s1", "mode_instructions", "内容", AttachmentRuntime, "src.a")
	require.NoError(t, err)
	require.Equal(t, "session.s1.mode_instructions", a.ID)
	require.Equal(t, DefaultAttachmentPriority, a.Priority)
	require.Equal(t, DefaultAttachmentContentKind, a.ContentKind)
	require.Equal(t, "mode_instructions", a.Metadata["section"])
	require.Equal(t, "src.a", a.Metadata["source"])
	require.NotEmpty(t, a.CreatedAt)
	require.Equal(t, sha256Hex(a.Content), a.ContentSHA256)
	_, err = m.AddSection("", "x", "c", AttachmentRuntime, "s")
	require.Error(t, err)
}

func TestManager_稳定排序与覆盖(t *testing.T) {
	m := NewManager()
	_, _ = m.AddSection("s1", "b", "B", AttachmentRuntime, "src")
	_, _ = m.AddSection("s1", "a", "A", AttachmentRuntime, "src")
	_, _ = m.AddSection("s1", "c", "C", AttachmentRuntime, "src", WithAttachmentPriority(1))
	items, err := m.Collect("s1")
	require.NoError(t, err)
	require.Equal(t, []string{"c", "a", "b"}, []string{items[0].Section, items[1].Section, items[2].Section})
	_, _ = m.AddSection("s1", "a", "A2", AttachmentRuntime, "src")
	items, _ = m.Collect("s1")
	require.Len(t, items, 3)
	require.Equal(t, "A2", items[1].Content)
}

func TestManager_过期剔除(t *testing.T) {
	m := NewManager()
	_, _ = m.AddSection("s1", "old", "x", AttachmentRuntime, "src",
		WithAttachmentExpiresAt(time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)))
	_, _ = m.AddSection("s1", "new", "y", AttachmentRuntime, "src")
	items, err := m.Collect("s1")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "new", items[0].Section)
}

func TestManager_并发安全(t *testing.T) {
	m := NewManager()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = m.AddSection("s1", "sec", "v", AttachmentRuntime, "src") }()
		go func() { defer wg.Done(); _, _ = m.Collect("s1") }()
	}
	wg.Wait()
}
```

- [ ] **Step 2: 运行确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/prompts/ -run 'TestSafeIDPart|TestManager_' -v`
Expected: FAIL — `undefined: NewManager` 等

- [ ] **Step 3: 写实现**

```go
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

// Manager 会话级动态提示词片段存储。
//
// Python: PromptAttachmentManager
type Manager struct {
	mu     sync.RWMutex
	histMu sync.Mutex
	items  map[string]map[string]*Attachment // session → section → item
}

var unsafeIDPattern = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// NewManager 创建管理器。
func NewManager() *Manager {
	return &Manager{items: map[string]map[string]*Attachment{}}
}

// safeIDPart 归一化 id 片段（对齐上游 _safe_id_part）。
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

// makeSectionID 生成 section id（对齐上游 _make_section_id）。
func makeSectionID(sessionID, section string) string {
	return "session." + safeIDPart(sessionID, "session") + "." + safeIDPart(section, "section")
}

func utcNow() string { return time.Now().UTC().Format(time.RFC3339) }

// AddSection 新增或覆盖一个 section。
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

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ClearSection 删除一个 section，返回删除条数。
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
func (m *Manager) ClearSource(source, sessionID string) (int, error) {
	return m.RemoveByFilter(AttachmentFilter{Source: source, SessionID: sessionID})
}

// RemoveByFilter 按过滤器删除。
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
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Section < out[j].Section
	})
	return out, nil
}

// GetByID 按 id 查询。
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

// ListByFilter 按过滤器查询。
func (m *Manager) ListByFilter(f AttachmentFilter) ([]*Attachment, error) {
	items, err := m.Collect(f.SessionID)
	if err != nil {
		return nil, err
	}
	out := make([]*Attachment, 0, len(items))
	for _, item := range items {
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
	return out, nil
}

// ClearSession 清空一个会话。
func (m *Manager) ClearSession(sessionID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.items[sessionID])
	delete(m.items, sessionID)
	return n, nil
}

// ClearAll 清空全部会话。
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
```

同时在 `attachment_store_test.go` 里补一行断言：`require.Equal(t, sha256Hex(a.Content), a.ContentSHA256)`（Step 1 已写入，直接复用 `sha256Hex`）。

- [ ] **Step 4: 运行确认通过（含 -race）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/agentcore/harness/prompts/ -run 'TestSafeIDPart|TestManager_' -v`
Expected: PASS（4 个用例，race 干净）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/prompts/attachment_store.go internal/agentcore/harness/prompts/attachment_store_test.go
git commit -m "feat(prompts): attachment 存储（归一化/过期/稳定排序/并发安全）"
```

---

### Task 3: 渲染（三态 + 两级截断）

**Files:**
- Create: `internal/agentcore/harness/prompts/attachment_render.go`
- Test: `internal/agentcore/harness/prompts/attachment_render_test.go`

- [ ] **Step 1: 写失败测试**

```go
package prompts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderSnapshot_结构(t *testing.T) {
	m := NewManager()
	_, _ = m.AddSection("s1", "mode_instructions", "## 规划模式", AttachmentRuntime, "src")
	items, _ := m.Collect("s1")
	out := m.RenderSnapshot(items)
	require.True(t, strings.HasPrefix(out, "<system-reminder>\n"))
	require.True(t, strings.HasSuffix(out, "\n</system-reminder>"))
	require.Contains(t, out, "以下内容不是用户的意图")
	require.Contains(t, out, "以下动态上下文当前有效")
	require.Contains(t, out, "## 规划模式")
}

func TestRenderDelta_只含变化与移除(t *testing.T) {
	m := NewManager()
	changed := []*Attachment{{Section: "a", Kind: AttachmentRuntime, Content: "A2", ContentKind: "text/plain"}}
	out := m.RenderDelta(changed, []string{"b"})
	require.Contains(t, out, "以下动态上下文已经变化")
	require.Contains(t, out, "A2")
	require.Contains(t, out, "以下先前提供的动态上下文已不再生效")
	require.Contains(t, out, "- `b`")
}

func TestRenderSnapshot_单节截断(t *testing.T) {
	m := NewManager()
	big := strings.Repeat("x", MaxAttachmentChars+10)
	items := []*Attachment{{Section: "big", Kind: AttachmentText, Content: big, ContentKind: "text/plain"}}
	out := m.RenderSnapshot(items)
	require.Contains(t, out, "[Prompt attachment truncated: content exceeded max_prompt_attachment_chars.]")
	require.Less(t, len(out), len(big)+2000)
}

func TestRenderSnapshot_空内容返回空串(t *testing.T) {
	m := NewManager()
	require.Equal(t, "", m.RenderSnapshot(nil))
	require.Equal(t, "", m.RenderSnapshot([]*Attachment{{Section: "x", Content: "   "}}))
}
```

- [ ] **Step 2: 运行确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/prompts/ -run TestRender -v`
Expected: FAIL — `(*Manager).RenderSnapshot undefined`

- [ ] **Step 3: 写实现**

```go
package prompts

import (
	"fmt"
	"strings"
)

const (
	noticeCN = "以下内容不是用户的意图，也不是用户直接发出的指令；它是系统为本次模型调用自动附加的动态上下文。请仅将其作为补充信息使用。"
	introSnapshotCN = "以下动态上下文当前有效，请与稳定的系统指令一同使用。"
	introDeltaCN    = "以下动态上下文已经变化；如与历史内容冲突，请以本消息中的最新内容为准。"
	removedIntroCN  = "以下先前提供的动态上下文已不再生效，请勿继续依赖其历史内容："

	noticeEN = "The following content does not represent the user's intent and is not a direct instruction from the user. It is dynamic context automatically attached by the system for this model call. Please use it only as supplementary context."
	introSnapshotEN = "The following dynamic context is currently active. Use it together with the stable system instructions."
	introDeltaEN    = "The following dynamic context has changed. Use the latest content below instead of earlier conflicting context."
	removedIntroEN  = "The following previously supplied dynamic context is no longer active. Do not rely on its earlier content:"

	perSectionTruncNotice = "\n\n[Prompt attachment truncated: content exceeded max_prompt_attachment_chars.]"
	renderedTruncNotice   = "\n\n[Prompt attachments truncated: rendered content exceeded max_rendered_chars.]"
)

// RenderSnapshot 渲染全量快照。
func (m *Manager) RenderSnapshot(items []*Attachment) string {
	return m.render(items, nil, true)
}

// RenderDelta 渲染增量（变化项 + 移除项）。
func (m *Manager) RenderDelta(changed []*Attachment, removed []string) string {
	return m.render(changed, removed, false)
}

func (m *Manager) render(items []*Attachment, removed []string, snapshot bool) string {
	notice, intro, removedIntro := noticeCN, introSnapshotCN, removedIntroCN
	if snapshot {
		// 保持默认中文；英文由 RenderSnapshotWithLanguage 覆盖
	} else {
		intro = introDeltaCN
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
		if len(content) > MaxAttachmentChars {
			content = content[:MaxAttachmentChars] + perSectionTruncNotice
		}
		blocks = append(blocks, content)
	}
	removed = normalizeRemoved(removed)
	if len(blocks) == 0 && len(removed) == 0 {
		return ""
	}

	parts := []string{intro}
	if len(blocks) > 0 {
		parts = append(parts, strings.Join(blocks, "\n\n---\n\n"))
	}
	if len(removed) > 0 {
		lines := make([]string, 0, len(removed))
		for _, s := range removed {
			lines = append(lines, fmt.Sprintf("- `%s`", s))
		}
		parts = append(parts, removedIntro+"\n"+strings.Join(lines, "\n"))
	}
	body := strings.Join(parts, "\n\n")

	prefix := "<system-reminder>\n" + notice + "\n\n"
	suffix := "\n</system-reminder>"
	if MaxRenderedChars > 0 {
		available := MaxRenderedChars - len(prefix) - len(suffix)
		if available <= 0 {
			body = ""
		} else if len(body) > available {
			body = truncateRunes(body, available-len(renderedTruncNotice)) + renderedTruncNotice
		}
	}
	return prefix + body + suffix
}

func normalizeRemoved(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = safeIDPart(s, "section")
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

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
```

- [ ] **Step 4: 运行确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/prompts/ -run TestRender -v`
Expected: PASS（4 个用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/prompts/attachment_render.go internal/agentcore/harness/prompts/attachment_render_test.go
git commit -m "feat(prompts): attachment 三态渲染与两级截断（文案逐字对齐上游）"
```

---

### Task 4: 历史同步 `Sync` 与 metadata 账本

**Files:**
- Create: `internal/agentcore/harness/prompts/attachment_delivery.go`
- Test: `internal/agentcore/harness/prompts/attachment_delivery_test.go`

- [ ] **Step 1: 写失败测试**

```go
package prompts

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

type fakeHistory struct {
	messages []llmschema.BaseMessage
}

func (f *fakeHistory) SessionID() string { return "s1" }
func (f *fakeHistory) GetMessages(size int, withHistory bool) ([]llmschema.BaseMessage, error) {
	return f.messages, nil
}
func (f *fakeHistory) AddMessages(_ context.Context, msgs []llmschema.BaseMessage, _ ...ceiface.Option) ([]llmschema.BaseMessage, error) {
	f.messages = append(f.messages, msgs...)
	return msgs, nil
}

func TestSync_首次快照之后增量(t *testing.T) {
	m := NewManager()
	h := &fakeHistory{}
	_, _ = m.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 1)
	require.Equal(t, ModeSnapshot, h.messages[0].GetMetadata()[MetadataKeyMode])

	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 1, "内容未变不应新增消息")

	_, _ = m.AddSection("s1", "mode", "B", AttachmentRuntime, "src")
	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 2)
	require.Equal(t, ModeDelta, h.messages[1].GetMetadata()[MetadataKeyMode])

	_, _ = m.ClearSection("s1", "mode")
	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 3)
	state := h.messages[2].GetMetadata()[MetadataKeyState].(map[string]string)
	require.Empty(t, state)
}

func TestSync_新管理器不重复投递(t *testing.T) {
	h := &fakeHistory{}
	m1 := NewManager()
	_, _ = m1.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	require.NoError(t, m1.Sync(context.Background(), h))

	m2 := NewManager()
	_, _ = m2.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	require.NoError(t, m2.Sync(context.Background(), h))
	require.Len(t, h.messages, 1, "重建后同内容不应重复投递")
}

func TestSync_基线丢失回退快照(t *testing.T) {
	h := &fakeHistory{}
	m := NewManager()
	_, _ = m.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	require.NoError(t, m.Sync(context.Background(), h))
	h.messages = nil // 模拟压缩把 snapshot 挤出窗口
	_, _ = m.AddSection("s1", "mode", "B", AttachmentRuntime, "src")
	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 1)
	require.Equal(t, ModeSnapshot, h.messages[0].GetMetadata()[MetadataKeyMode])
}
```

- [ ] **Step 2: 运行确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/prompts/ -run TestSync -v`
Expected: FAIL — `(*Manager).Sync undefined`

- [ ] **Step 3: 写实现**

```go
package prompts

import (
	"context"
	"sort"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// HistoryContext 同步所需的最小上下文能力（*context.SessionModelContext 天然满足）。
type HistoryContext interface {
	SessionID() string
	GetMessages(size int, withHistory bool) ([]llmschema.BaseMessage, error)
	AddMessages(ctx context.Context, messages []llmschema.BaseMessage, opts ...ceiface.Option) ([]llmschema.BaseMessage, error)
}

// Sync 把当前附件状态同步进会话历史（首次快照 / 之后增量）。
//
// 对齐上游 PromptAttachmentManager.sync_to_context + ReActAgent._sync_prompt_attachments：
// 失败只告警不阻断模型。
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
	switch {
	case !hasSnapshot:
		if len(items) == 0 {
			return nil
		}
		mode = ModeSnapshot
		rendered = m.RenderSnapshot(items)
	default:
		changedSections := diffSections(current, previous)
		removed := removedSections(previous, current)
		if len(changedSections) == 0 && len(removed) == 0 {
			return nil
		}
		mode = ModeDelta
		rendered = m.RenderDelta(pickSections(items, changedSections), removed)
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

// readHistoryState 从历史消息里读最后一次交付账本。
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
		raw, ok := meta[MetadataKeyState]
		if !ok {
			continue
		}
		if m, ok := raw.(map[string]string); ok {
			state = m
		}
		if meta[MetadataKeyMode] == ModeSnapshot {
			hasSnapshot = true
		}
	}
	return state, hasSnapshot
}

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

func pickSections(items []*Attachment, sections []string) []*Attachment {
	want := make(map[string]struct{}, len(sections))
	for _, s := range sections {
		want[s] = struct{}{}
	}
	out := make([]*Attachment, 0, len(sections))
	for _, item := range items {
		if _, ok := want[item.Section]; ok {
			out = append(out, item)
		}
	}
	return out
}
```

- [ ] **Step 4: 运行确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/prompts/ -run TestSync -v`
Expected: PASS（3 个用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/prompts/attachment_delivery.go internal/agentcore/harness/prompts/attachment_delivery_test.go
git commit -m "feat(prompts): attachment 历史同步（快照/增量/基线回退，失败不阻断）"
```

---

### Task 5: 窗口 mutator 基础设施

**Files:**
- Create: `internal/agentcore/context_engine/interface/window.go`
- Modify: `internal/agentcore/context_engine/interface/types.go:214-223`
- Modify: `internal/agentcore/context_engine/context/session_model_context.go:568-575`（`mc.statContextWindow(window)` 之前）
- Test: `internal/agentcore/context_engine/context/window_mutator_test.go`

- [ ] **Step 1: 写失败测试**

```go
package context

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

func TestGetContextWindow_应用Mutator(t *testing.T) {
	mc := newSessionModelContextForMutator(t)
	_, err := mc.AddMessages(context.Background(), []llmschema.BaseMessage{llmschema.NewUserMessage("hi")})
	require.NoError(t, err)

	marker := func(w *ceiface.ContextWindow) *ceiface.ContextWindow {
		cp := *w
		cp.ContextMessages = append([]llmschema.BaseMessage{}, w.ContextMessages...)
		cp.ContextMessages = append(cp.ContextMessages, llmschema.NewUserMessage("mutated"))
		return &cp
	}
	window, err := mc.GetContextWindow(context.Background(), []llmschema.BaseMessage{llmschema.NewSystemMessage("sys")}, nil, 0, 0, ceiface.WithWindowMutators(marker))
	require.NoError(t, err)
	require.Equal(t, "mutated", window.ContextMessages[len(window.ContextMessages)-1].GetContent().Text())
}
```

同文件补构造助手（`tokenCounter`/`workspace`/`sysOperation` 传 nil，`statMessages` 已支持 nil 计数器）：

```go
func newSessionModelContextForMutator(t *testing.T) *SessionModelContext {
	t.Helper()
	return NewSessionModelContext(
		"ctx-1", "sess-1",
		*ceschema.NewContextEngineConfig(),
		nil, nil, nil, nil, nil, nil,
	)
}
```

需在测试文件 import `ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"`。

- [ ] **Step 2: 运行确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/context_engine/context/ -run TestGetContextWindow_应用Mutator -v`
Expected: FAIL — `undefined: ceiface.WithWindowMutators`

- [ ] **Step 3: 写实现**

`interface/window.go`：

```go
package interface

// ──────────────────────────── 结构体 ────────────────────────────

// WindowMutator 最终窗口投影函数，只作用于本次请求，不得修改入参窗口。
//
// 对齐上游 get_context_window(window_mutators=[...])。
type WindowMutator func(*ContextWindow) *ContextWindow

// ──────────────────────────── 常量 ────────────────────────────

// WindowMutatorsExtraKey Option.Extra 中存放 mutator 链的键
const WindowMutatorsExtraKey = "window_mutators"

// ──────────────────────────── 导出函数 ────────────────────────────

// WithWindowMutators 追加窗口投影链。
func WithWindowMutators(mutators ...WindowMutator) Option {
	return func(o *ProcessorOption) {
		if o.Extra == nil {
			o.Extra = map[string]any{}
		}
		existing, _ := o.Extra[WindowMutatorsExtraKey].([]WindowMutator)
		o.Extra[WindowMutatorsExtraKey] = append(existing, mutators...)
	}
}

// WindowMutatorsFromOption 从 Options 中取出 mutator 链。
func WindowMutatorsFromOption(opts ...Option) []WindowMutator {
	po := NewProcessorOption(opts...)
	mutators, _ := po.Extra[WindowMutatorsExtraKey].([]WindowMutator)
	return mutators
}
```

`types.go` 的 `ContextWindow` 增加字段（放在 `Statistic` 之前）：

```go
	// Mutators 本次请求的窗口投影链（由 GetContextWindow 应用，不参与序列化）
	Mutators []WindowMutator `json:"-"`
```

`session_model_context.go` 在 `// 8. 统计上下文窗口` 之前插入：

```go
	// 7.5 应用窗口投影链（本次请求有效，不改历史）
	for _, mutate := range iface.WindowMutatorsFromOption(opts...) {
		if mutate == nil {
			continue
		}
		if projected := mutate(window); projected != nil {
			window = projected
		}
	}
```

- [ ] **Step 4: 运行确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/context_engine/... -run 'TestGetContextWindow' -v`
Expected: PASS（含既有窗口用例不回归）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/interface/window.go internal/agentcore/context_engine/interface/types.go internal/agentcore/context_engine/context/session_model_context.go internal/agentcore/context_engine/context/window_mutator_test.go
git commit -m "feat(context_engine): ContextWindow 增加 mutator 投影链（按请求生效）"
```

---

### Task 6: provider 矩阵与 attachment mutator

**Files:**
- Modify: `internal/agentcore/harness/prompts/attachment_delivery.go`（追加函数）
- Test: `internal/agentcore/harness/prompts/attachment_mutator_test.go`

- [ ] **Step 1: 写失败测试**

```go
package prompts

import (
	"testing"

	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

func TestUsesSystemAttachmentRole_矩阵(t *testing.T) {
	cases := []struct {
		name                                    string
		provider, legacy, endpoint, apiMode     string
		want                                    bool
	}{
		{"anthropic 保持 user", "anthropic", "", "", "", false},
		{"anthropic api_mode", "", "", "", "anthropic-messages", false},
		{"百炼 endpoint", "", "", "bailian", "", true},
		{"dashscope provider", "dashscope", "", "", "", true},
		{"openai 兼容回落 legacy", "", "bailian", "openai", "", true},
		{"普通 openai", "openai", "", "", "", false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, UsesSystemAttachmentRole(c.provider, c.legacy, c.endpoint, c.apiMode), c.name)
	}
}

func TestBuildWindowMutator_只换附件消息(t *testing.T) {
	attach := llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
		MetadataKeyAttachmentHistory: true,
		MetadataKeySessionID:         "s1",
	}))
	plain := llmschema.NewUserMessage("hi")
	window := ceiface.NewContextWindow()
	window.ContextMessages = []llmschema.BaseMessage{attach, plain}

	mutate := BuildWindowMutator("s1", true)
	out := mutate(window)
	require.IsType(t, &llmschema.SystemMessage{}, out.ContextMessages[0])
	require.IsType(t, &llmschema.UserMessage{}, out.ContextMessages[1])
	require.IsType(t, &llmschema.UserMessage{}, window.ContextMessages[0], "不得修改入参")
}
```

- [ ] **Step 2: 运行确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/prompts/ -run 'TestUsesSystemAttachmentRole|TestBuildWindowMutator' -v`
Expected: FAIL — `undefined: UsesSystemAttachmentRole`

- [ ] **Step 3: 写实现（追加到 `attachment_delivery.go`）**

```go
// UsesSystemAttachmentRole 判断当前路由是否把附件投递为 SystemMessage。
//
// 对齐上游 _uses_system_attachment_role：仅百炼/DashScope 需要，
// Anthropic 客户端会把 System 提到顶层 system 字段，故保持 UserMessage。
func UsesSystemAttachmentRole(provider, legacyProvider, endpointProfile, apiMode string) bool {
	norm := func(s string) string {
		return strings.ToLower(strings.TrimSpace(s))
	}
	provider, legacyProvider = norm(provider), norm(legacyProvider)
	endpointProfile, apiMode = norm(endpointProfile), norm(apiMode)

	if provider == "anthropic" {
		return false
	}
	switch apiMode {
	case "anthropic", "anthropic-messages", "messages":
		return false
	}
	backend := endpointProfile
	switch backend {
	case "", "openai", "openai-compatible":
		if legacyProvider != "" {
			backend = legacyProvider
		} else {
			backend = provider
		}
	}
	return backend == "bailian" || backend == "dashscope"
}

// BuildWindowMutator 构造附件消息的窗口投影（session 隔离 + role 替换）。
func BuildWindowMutator(sessionID string, useSystemRole bool) ceiface.WindowMutator {
	return func(w *ceiface.ContextWindow) *ceiface.ContextWindow {
		if w == nil || !useSystemRole {
			return w
		}
		out := make([]llmschema.BaseMessage, 0, len(w.ContextMessages))
		changed := false
		for _, msg := range w.ContextMessages {
			meta := msg.GetMetadata()
			if meta == nil || meta[MetadataKeyAttachmentHistory] != true {
				out = append(out, msg)
				continue
			}
			if sid, ok := meta[MetadataKeySessionID].(string); ok && sid != "" && sid != sessionID {
				out = append(out, msg)
				continue
			}
			sys := llmschema.NewSystemMessage(msg.GetContent().Text(), llmschema.WithMetadata(meta))
			out = append(out, sys)
			changed = true
		}
		if !changed {
			return w
		}
		cp := *w
		cp.ContextMessages = out
		return &cp
	}
}
```

需要在 import 中补 `strings` 与 `ceiface`、`llmschema`。

- [ ] **Step 4: 运行确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/prompts/ -run 'TestUsesSystemAttachmentRole|TestBuildWindowMutator' -v`
Expected: PASS（2 个用例，第二个含 3 条断言）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/prompts/attachment_delivery.go internal/agentcore/harness/prompts/attachment_mutator_test.go
git commit -m "feat(prompts): attachment 窗口投影与 provider 矩阵（百炼/DS 换 System）"
```

---

### Task 7: ReAct 两处装订点

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_model_call.go:114-132`
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:450-458`、`:672-682`
- Test: `internal/agentcore/single_agent/agents/attachment_hook_test.go`

- [ ] **Step 1: 写失败测试**

```go
package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
)

func TestSyncPromptAttachments_无管理器时跳过(t *testing.T) {
	require.NoError(t, syncPromptAttachments(context.Background(), nil, nil))
}

func TestSyncPromptAttachments_有管理器无上下文时跳过(t *testing.T) {
	require.NoError(t, syncPromptAttachments(context.Background(), prompts.NewManager(), nil))
}
```

- [ ] **Step 2: 运行确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/single_agent/agents/ -run TestSyncPromptAttachments -v`
Expected: FAIL — `undefined: syncPromptAttachments`

- [ ] **Step 3: 写实现**

新增 `react_attachment.go`（同包）：

```go
package agents

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
)

// syncPromptAttachments 把动态提示词同步进历史；失败不阻断模型。
//
// Python: ReActAgent._sync_prompt_attachments（react_agent.py:1148-1180）
func syncPromptAttachments(ctx context.Context, m *prompts.Manager, modelCtx ceiface.ModelContext) error {
	if m == nil || modelCtx == nil {
		return nil
	}
	if err := m.Sync(ctx, modelCtx); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "prompt_attachment_sync_failed").
			Err(err).
			Msg("prompt attachment sync failed")
	}
	return nil
}
```

`react_model_call.go` 在 `GetContextWindow` 调用前插入：

```go
	// 附件同步：保证本轮模型能看到最新的动态提示词（对齐 Python _build_model_context_window）
	_ = syncPromptAttachments(ctx, r.promptAttachments, modelCtx)
```

`react_invoke.go` 两处 `AddMessages(NewUserMessage(...))` 之前各插入：

```go
			_ = syncPromptAttachments(ctx, r.promptAttachments, modelCtx)
```

并在 `ReActAgent` 结构体上新增字段（`react_agent.go`）：

```go
	// promptAttachments 动态提示词管理器（由 DeepAgent 注入，可空）
	promptAttachments *prompts.Manager
```

补一个 setter（供 DeepAgent 注入，S2 使用）：

```go
// SetPromptAttachmentManager 注入动态提示词管理器（可为 nil）。
func (r *ReActAgent) SetPromptAttachmentManager(m *prompts.Manager) {
	r.promptAttachments = m
}
```

- [ ] **Step 4: 运行确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/single_agent/... -run 'TestSyncPromptAttachments|TestReAct' -v`
Expected: PASS（2 个新用例 + 既有 ReAct 用例不回归）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_attachment.go internal/agentcore/single_agent/agents/react_agent.go internal/agentcore/single_agent/agents/react_model_call.go internal/agentcore/single_agent/agents/react_invoke.go internal/agentcore/single_agent/agents/attachment_hook_test.go
git commit -m "feat(agents): ReAct 在写用户消息前与建窗口前同步 prompt attachment"
```

---

### Task 8: 集成测试 IT-1 / IT-2（全链路 + 顺序契约）

**Files:**
- Create: `tests/integration/agentcore/attachment/doc.go`
- Create: `tests/integration/agentcore/attachment/testrail.go`
- Create: `tests/integration/agentcore/attachment/attachment_flow_test.go`

- [ ] **Step 1: 建目录与上下文助手**

`doc.go`：

```go
//go:build integration

// Package attachment 提供 Prompt Attachment 地基的集成测试。
//
// 覆盖 snapshot/delta/removed 全链路、顺序契约、压缩后基线回退、
// provider 投影矩阵、重启恢复、多会话隔离、跨轮插入。
// 跨 internal/agentcore/{harness,context_engine,session} 三个二级子系统。
//
// 对应 Python：openjiuwen/harness/prompts/prompt_attachment_manager.py
//
// 文件目录：
//
//	attachment/
//	├── doc.go                        # 包文档
//	├── context_helper.go             # SessionFacade mock + 真实 ContextEngine 构造
//	├── attachment_flow_test.go       # IT-1 全链路 + IT-2 顺序契约
//	├── attachment_compact_test.go    # IT-3 压缩回退 + IT-5 重启恢复
//	└── attachment_provider_test.go   # IT-4 投影矩阵 + IT-6 隔离 + IT-7 跨轮插入
package attachment
```

`context_helper.go`：

```go
//go:build integration

package attachment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// mockSessionFacade 最小 SessionFacade 实现（对齐 tests/integration/agentcore/context_engine 同名 helper）。
type mockSessionFacade struct {
	sessionID string
	states    map[string]any
}

func (m *mockSessionFacade) GetSessionID() string { return m.sessionID }
func (m *mockSessionFacade) UpdateState(data map[string]any) {
	for k, v := range data {
		m.states[k] = v
	}
}
func (m *mockSessionFacade) GetState(key state.StateKey) (any, error) {
	v, ok := m.states[key.String()]
	if !ok {
		return nil, nil
	}
	return v, nil
}
func (m *mockSessionFacade) DumpState() map[string]any {
	out := make(map[string]any, len(m.states))
	for k, v := range m.states {
		out[k] = v
	}
	return out
}
func (m *mockSessionFacade) WriteStream(_ context.Context, _ any) error       { return nil }
func (m *mockSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (m *mockSessionFacade) GetEnv(_ string, _ ...any) any                    { return nil }
func (m *mockSessionFacade) Interact(_ context.Context, _ any) error          { return nil }
func (m *mockSessionFacade) ClearSession(_ context.Context) {
	m.states = make(map[string]any)
}

var _ sessioninterfaces.SessionFacade = (*mockSessionFacade)(nil)

// newContext 用真实 ContextEngine 建一个 SessionModelContext。
func newContext(t *testing.T, sessionID string) *cecontext.SessionModelContext {
	t.Helper()
	ce := context_engine.NewContextEngine(ceschema.NewContextEngineConfig())
	mc, err := ce.CreateContext(context.Background(), "ctx-"+sessionID, &mockSessionFacade{
		sessionID: sessionID,
		states:    map[string]any{},
	})
	require.NoError(t, err)
	require.NotNil(t, mc)
	typed, ok := mc.(*cecontext.SessionModelContext)
	require.True(t, ok, "CreateContext 应返回 *SessionModelContext")
	return typed
}
```

注：若 `CreateContext` 返回的是接口类型而非 `*SessionModelContext`，删除末尾的类型断言，直接把 `newContext` 返回类型改成 `iface.ModelContext`（`Sync`/`GetContextWindow`/`SetMessages` 都在该接口上）。

- [ ] **Step 2: 写 IT-1 / IT-2**

```go
//go:build integration

package attachment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
)

type FlowSuite struct {
	suite.Suite
}

func TestFlowSuite(t *testing.T) { suite.Run(t, new(FlowSuite)) }

func (s *FlowSuite) TestIT1_SnapshotDeltaRemoved() {
	m := prompts.NewManager()
	mc := newContext(s.T(), "s1")

	_, err := m.AddSection("s1", "mode_instructions", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(err)
	s.Require().NoError(m.Sync(context.Background(), mc))
	items, _ := mc.GetMessages(0, true)
	s.Require().Len(items, 1)

	s.Require().NoError(m.Sync(context.Background(), mc))
	items, _ = mc.GetMessages(0, true)
	s.Require().Len(items, 1, "内容未变不应新增")

	_, err = m.AddSection("s1", "mode_instructions", "B", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(err)
	s.Require().NoError(m.Sync(context.Background(), mc))
	items, _ = mc.GetMessages(0, true)
	s.Require().Len(items, 2)
	s.Require().Equal(prompts.ModeDelta, items[1].GetMetadata()[prompts.MetadataKeyMode])

	_, err = m.ClearSection("s1", "mode_instructions")
	s.Require().NoError(err)
	s.Require().NoError(m.Sync(context.Background(), mc))
	items, _ = mc.GetMessages(0, true)
	s.Require().Len(items, 3)
	s.Require().Equal(prompts.ModeDelta, items[2].GetMetadata()[prompts.MetadataKeyMode])
}

func (s *FlowSuite) TestIT2_顺序契约与幂等() {
	m := prompts.NewManager()
	mc := newContext(s.T(), "s1")

	_, _ = m.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))
	_, err := mc.AddMessages(context.Background(), []llmschema.BaseMessage{llmschema.NewUserMessage("真实用户")})
	s.Require().NoError(err)

	items, _ := mc.GetMessages(0, true)
	s.Require().Len(items, 2)
	s.Require().Equal(true, items[0].GetMetadata()[prompts.MetadataKeyAttachmentHistory])
	s.Require().Equal("真实用户", items[1].GetContent().Text())

	// 同轮重复 sync 不产生第三条
	s.Require().NoError(m.Sync(context.Background(), mc))
	items, _ = mc.GetMessages(0, true)
	s.Require().Len(items, 2)
}
```

- [ ] **Step 3: 运行**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/attachment/ -run 'TestFlowSuite' -v`
Expected: PASS（2 个用例）

- [ ] **Step 4: 提交**

```bash
git add tests/integration/agentcore/attachment/
git commit -m "test(integration): attachment IT-1 全链路 + IT-2 顺序契约"
```

---

### Task 9: 集成测试 IT-3 / IT-5（压缩回退 + 重启恢复）

**Files:**
- Create: `tests/integration/agentcore/attachment/attachment_compact_test.go`

- [ ] **Step 1: 写两个用例**

```go
//go:build integration

package attachment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
)

type CompactSuite struct{ suite.Suite }

func TestCompactSuite(t *testing.T) { suite.Run(t, new(CompactSuite)) }

func (s *CompactSuite) TestIT3_基线丢失回退快照() {
	m := prompts.NewManager()
	mc := newContext(s.T(), "s1")
	_, _ = m.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))

	// 清空消息等价于压缩把 snapshot 挤出窗口（保持真实上下文对象不变）
	mc.SetMessages(nil, true)

	_, _ = m.AddSection("s1", "mode", "B", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))
	items, _ := mc.GetMessages(0, true)
	s.Require().Len(items, 1)
	s.Require().Equal(prompts.ModeSnapshot, items[0].GetMetadata()[prompts.MetadataKeyMode])
	s.Require().Contains(items[0].GetContent().Text(), "B")
}

func (s *CompactSuite) TestIT5_重启后不重复投递() {
	mc := newContext(s.T(), "s1")
	m1 := prompts.NewManager()
	_, _ = m1.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m1.Sync(context.Background(), mc))

	// 新 Manager + 同一上下文（等价进程重启后从历史恢复账本）
	m2 := prompts.NewManager()
	_, _ = m2.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m2.Sync(context.Background(), mc))

	items, _ := mc.GetMessages(0, true)
	s.Require().Len(items, 1, "重启后同内容不得重复投递")
	s.Require().Equal(prompts.ModeSnapshot, items[0].GetMetadata()[prompts.MetadataKeyMode])
}
```

- [ ] **Step 2: 运行**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/attachment/ -run 'TestCompactSuite' -v`
Expected: PASS（2 个用例）

- [ ] **Step 3: 提交**

```bash
git add tests/integration/agentcore/attachment/attachment_compact_test.go
git commit -m "test(integration): attachment IT-3 压缩回退 + IT-5 重启恢复"
```

---

### Task 10: 集成测试 IT-4 / IT-6 / IT-7（投影、隔离、流式）

**Files:**
- Create: `tests/integration/agentcore/attachment/attachment_provider_test.go`

- [ ] **Step 1: 写三个用例**

```go
//go:build integration

package attachment

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
)

type ProviderSuite struct{ suite.Suite }

func TestProviderSuite(t *testing.T) { suite.Run(t, new(ProviderSuite)) }

func (s *ProviderSuite) TestIT4_投影矩阵且历史不变() {
	m := prompts.NewManager()
	mc := newContext(s.T(), "s1")
	_, _ = m.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))

	// 百炼：窗口内换 System，历史仍 User
	w, err := mc.GetContextWindow(context.Background(), []llmschema.BaseMessage{llmschema.NewSystemMessage("sys")}, nil, 0, 0,
		ceiface.WithWindowMutators(prompts.BuildWindowMutator("s1", true)))
	s.Require().NoError(err)
	s.Require().IsType(&llmschema.SystemMessage{}, w.ContextMessages[0])
	hist, _ := mc.GetMessages(0, true)
	s.Require().IsType(&llmschema.UserMessage{}, hist[0], "历史不得被改写")

	// Anthropic：保持 User
	w2, err := mc.GetContextWindow(context.Background(), []llmschema.BaseMessage{llmschema.NewSystemMessage("sys")}, nil, 0, 0,
		ceiface.WithWindowMutators(prompts.BuildWindowMutator("s1", false)))
	s.Require().NoError(err)
	s.Require().IsType(&llmschema.UserMessage{}, w2.ContextMessages[0])
}

func (s *ProviderSuite) TestIT6_多会话隔离() {
	m := prompts.NewManager()
	c1 := newContext(s.T(), "s1")
	c2 := newContext(s.T(), "s2")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = m.AddSection("s1", "a", "A", prompts.AttachmentRuntime, "r") }()
	go func() { defer wg.Done(); _, _ = m.AddSection("s2", "b", "B", prompts.AttachmentRuntime, "r") }()
	wg.Wait()

	s.Require().NoError(m.Sync(context.Background(), c1))
	s.Require().NoError(m.Sync(context.Background(), c2))

	m1, _ := c1.GetMessages(0, true)
	m2, _ := c2.GetMessages(0, true)
	s.Require().Len(m1, 1)
	s.Require().Len(m2, 1)
	s.Require().Contains(m1[0].GetContent().Text(), "A")
	s.Require().NotContains(m1[0].GetContent().Text(), "B")
	s.Require().Contains(m2[0].GetContent().Text(), "B")
}

func (s *ProviderSuite) TestIT7_流式两轮之间插入增量() {
	m := prompts.NewManager()
	mc := newContext(s.T(), "s1")

	_, _ = m.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))
	_, err := mc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("第1轮"),
		llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "回答1"),
	})
	s.Require().NoError(err)

	_, _ = m.AddSection("s1", "mode", "B", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))
	_, err = mc.AddMessages(context.Background(), []llmschema.BaseMessage{llmschema.NewUserMessage("第2轮")})
	s.Require().NoError(err)

	items, _ := mc.GetMessages(0, true)
	s.Require().Len(items, 5)
	s.Require().Equal(true, items[2].GetMetadata()[prompts.MetadataKeyAttachmentHistory],
		"第 3 条应为 delta 附件消息")
	s.Require().Equal(prompts.ModeDelta, items[2].GetMetadata()[prompts.MetadataKeyMode])
	s.Require().Equal("第2轮", items[4].GetContent().Text())
}
```

> IT-7 等价于「流式两轮之间插入 delta」：`Sync` 的调用点与流式无关，流式回归由既有流式用例覆盖；本用例锁定消息插入位置与轮次顺序。

- [ ] **Step 2: 运行（含 -race）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -race ./tests/integration/agentcore/attachment/ -v`
Expected: PASS（7 个用例，race 干净）

- [ ] **Step 3: 提交**

```bash
git add tests/integration/agentcore/attachment/attachment_provider_test.go
git commit -m "test(integration): attachment IT-4 投影矩阵 + IT-6 隔离 + IT-7 跨轮插入"
```

---

### Task 11: 文档、计划与验收

**Files:**
- Modify: `internal/agentcore/harness/prompts/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`（领域二十四新增 24.10 行）
- Modify: `docs/sync/defect-plan-2026-11-04.md`（B 组新增 B-17 行）

- [ ] **Step 1: 更新 doc.go 文件树**

在 `internal/agentcore/harness/prompts/doc.go` 的文件目录中加入：

```
//	├── attachment_types.go    # 动态提示词片段类型与常量
//	├── attachment_digest.go   # 规范化 JSON 与内容哈希
//	├── attachment_store.go    # 会话级存储（归一化/过期/稳定排序）
//	├── attachment_render.go   # snapshot/delta 渲染与两级截断
//	└── attachment_delivery.go # 历史同步、窗口投影、provider 矩阵
```

- [ ] **Step 2: 更新实现计划**

在 `IMPLEMENTATION_PLAN.md` 领域二十四表格追加：

```markdown
| 24.10 | ✅ | Prompt Attachment 地基（S1） | manager（存储/渲染/哈希增量）+ `ContextWindow` mutator 链 + ReAct 两处装订点 + provider 矩阵；7 例集成测试 | `internal/agentcore/harness/prompts/attachment_*.go` ← `openjiuwen/harness/prompts/prompt_attachment_manager.py` |
```

- [ ] **Step 3: 更新缺陷台账**

在 `docs/sync/defect-plan-2026-11-04.md` B 组表格末尾追加：

```markdown
| B-17 | 动态提示词仍靠 system prompt 改写，无附件投递层（上游已迁移） | `958fd614` | `harness/prompts/attachment_*.go` | P1 | ✅ |
```

- [ ] **Step 4: 全量验证**

```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/agentcore/harness/... ./internal/agentcore/context_engine/... ./internal/agentcore/single_agent/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/attachment/...
go run ./cmd/integration-audit -verify
```

Expected：全部通过；audit 对 `tests/integration/agentcore/attachment` 判定通过（跨 4 个二级子系统）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/prompts/doc.go IMPLEMENTATION_PLAN.md docs/sync/defect-plan-2026-11-04.md
git commit -m "docs: 登记 prompt attachment 地基（S1）完成与缺陷 B-17 关闭"
```

---

## 自审记录

- **Spec 覆盖**：§4.1 落位→文件表；§4.2 类型/存储→Task 1/2；§4.3 渲染→Task 3；§4.4 哈希→Task 1 + Task 4 判定；§4.5 装订点→Task 7；§4.6 投影→Task 5/6；§4.7 metadata→Task 1 常量 + Task 4；§4.8 错误处理→Task 7 的告警包装（写侧 error、读侧 warn）；§5 集成测试→Task 8/9/10；§6 单测→各 Task Step 1；§7 验收→Task 11。
- **未覆盖项**：`commit callbacks` 执行逻辑（上游暂无消费方）——仅在常量中预留键；如需执行，S2 接入时补 `Sync` 尾部回调，已在 spec §4.5 标注。
- **类型一致性**：`Attachment`/`Manager`/`Sync`/`WindowMutator`/`WithWindowMutators` 在 Task 1-7 中名称一致；`HistoryContext` 只在 Task 4 定义并被 Task 7 复用。
- **占位符检查**：已把原 `testrail.go` 空壳替换为可直接编译的 `context_helper.go`（含完整 `mockSessionFacade` 与 `NewSessionModelContext` 构造）；全文无待填内容。
