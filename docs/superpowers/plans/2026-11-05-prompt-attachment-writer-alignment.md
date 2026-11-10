# 提示词附件投递对齐实现计划（ContextWriter + 9 处 rail 统一）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补回 Python 的 `PromptAttachmentContextWriter`，把 9 处 rail 的附件投递统一成 `BindContext` 形状，删除 Go 自造的三个 `Deliver*` 助手与 `progressive` 的窄接口，并修掉附件 metadata 恢复的 4 处偏差、用真集成覆盖替换漂移的覆盖率豁免。

**Architecture:** 新增 `internal/agentcore/harness/prompts/attachment_writer.go`（`ContextWriter` 类型 + `Manager.BindContext` + 四级会话回落），9 个 rail 在 `Init` 里存一次 `*prompts.Manager` 字段、在钩子里 `writer := mgr.BindContext(cbc)` 后调方法；缺会话时统一返回 `ErrSessionRequired`，各 rail 按上游分支决定"跳过/回落/告警"。`progressive` 去掉 `managerAdapter` 直接持 `*prompts.Manager`。

**Tech Stack:** Go 1.x，`github.com/stretchr/testify`（suite/require/assert），构建标签 `sqlite_fts5 test integration`，自研 `cmd/integration-audit`。

**Spec:** `docs/superpowers/specs/2026-11-05-prompt-attachment-writer-alignment-design.md`

---

## 文件结构

| 文件 | 职责 | 动作 |
| --- | --- | --- |
| `internal/agentcore/harness/prompts/attachment_writer.go` | `ContextWriter` 类型、`Manager.BindContext`、`ErrSessionRequired`、`resolveSessionIDFromContext` | **新建** |
| `internal/agentcore/harness/prompts/attachment_writer_test.go` | 上述四个符号的单测 | **新建** |
| `internal/agentcore/harness/prompts/attachment_rail.go` | 只保留 `RailAttachmentProvider` + `ManagerFor` | 修改（删 3 个导出函数） |
| `internal/agentcore/harness/prompts/attachment_rail_test.go` | 只保留 `ManagerFor` 的用例 | 修改（删 3 个用例） |
| `internal/agentcore/harness/prompts/doc.go` | 文件树加 `attachment_writer.go` | 修改 |
| `internal/agentcore/harness/rails/agent_mode.go` | `mode_instructions` 走 Writer，删自写 `attachmentManager()` | 修改 |
| `internal/agentcore/harness/rails/memory/coding_memory_rail.go` | `coding_memory_context` 走 Writer + 告警 | 修改 |
| `internal/agentcore/harness/rails/memory/external_memory_rail.go` | `external_memory_prefetch` 走 Writer，删 `AddSection` 回落 | 修改 |
| `internal/agentcore/harness/rails/skills/skill_use_rail.go` | `skills.runtime_changes` 走 Writer | 修改 |
| `internal/agentcore/harness/rails/task_completion.go` | `goal_protocol` 走 Writer | 修改 |
| `internal/agentcore/harness/rails/progressive.go` | 去窄接口、直存 Manager、D1~D4 修复、裸字符串常量化为 `deferredToolCatalogFingerprintsStateKey`、新增 `deferredToolCatalogInitialFingerprintsStateKey` | 修改 |
| `internal/agentcore/harness/rails/progressive_test.go` | 改 fake 为真 Manager；新增 3 个恢复逻辑用例 | 修改 |
| `internal/agent_teams/rails/team_policy_rail.go` | `team.context` 走 Writer | 修改 |
| `internal/agent_teams/rails/team_plan_mode_rail.go` | **新增** `mode_instructions` 附件投递 + 非 plan 清除 + 无 manager 回落 | 修改 |
| `internal/agentcore/harness/rails/context_engineer/context_assemble_rail.go` | `context.heartbeat` 改投附件 | 修改 |
| `internal/agentcore/harness/rails/memory/doc.go` | 包描述从"注入 system prompt"改为"投递为历史附件" | 修改 |
| `tests/integration/swarm/server/command_context_default_mode_test.go` | 不带 mode 的 `command.context` 全链路 | **新建** |
| `tests/integration/swarm/server/doc.go` | 文件树加上述文件 | 修改 |
| `.integration-coverage-exempt` | 删 190-194 行漂移条目 | 修改 |

`prompts` 包已引 `single_agent/prompts`（别名 `saprompt`），而 `single_agent/interfaces` 不在其依赖里，因此 `attachment_writer.go` 新增 `import interfaces` **不会成环**（已用 `go list -deps` 双向验证；`interfaces` → `harness/prompts` 无路径）。

---

## Task 1: ContextWriter 基座

**Files:**
- Create: `internal/agentcore/harness/prompts/attachment_writer.go`
- Test: `internal/agentcore/harness/prompts/attachment_writer_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/agentcore/harness/prompts/attachment_writer_test.go`。

> 会话构造说明：`interfaces` 包**没有** `NewTestSession`（真实方法在
> `tests/integration/suite/session_suite.go:54`，属集成层）。单测统一用本文件内的
> `newStubSession`（见 Step 3 前的助手块），它是 `sessioninterfaces.SessionFacade`
> 的最小实现，`GetSessionID()` 返回构造值。

```go
package prompts

import (
	"testing"

	"github.com/stretchr/testify/require"

	interfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// TestBindContext_取会话ID 验证 BindContext 从 cbc.Session() 解析会话。
func TestBindContext_取会话ID(t *testing.T) {
	m := NewManager()
	sess := newStubSession("sess-1")
	cbc := interfaces.NewAgentCallbackContext(nil, nil, sess)

	w := m.BindContext(cbc)
	require.NotNil(t, w)
	require.Equal(t, "sess-1", w.SessionID())
}

// TestBindContext_会话ID从Inputs回落 验证 session 为空时回落到 InvokeInputs.Extra。
func TestBindContext_会话ID从Inputs回落(t *testing.T) {
	m := NewManager()
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{
		Extra: map[string]any{"session_id": "from-inputs"},
	}, nil)

	require.Equal(t, "from-inputs", m.BindContext(cbc).SessionID())
}

// TestBindContext_会话ID从ctxExtra回落 验证 inputs 与 session 都空时回落到 cbc.Extra()。
func TestBindContext_会话ID从ctxExtra回落(t *testing.T) {
	m := NewManager()
	cbc := interfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.SetExtra("session_id", "from-extra")

	require.Equal(t, "from-extra", m.BindContext(cbc).SessionID())
}

// TestBindContext_三个方法在缺会话时返错 验证 ErrSessionRequired。
func TestBindContext_三个方法在缺会话时返错(t *testing.T) {
	m := NewManager()
	w := m.BindContext(interfaces.NewAgentCallbackContext(nil, nil, nil))
	require.Empty(t, w.SessionID())

	_, err := w.AddSection("sec", "body", AttachmentRuntime, "src")
	require.ErrorIs(t, err, ErrSessionRequired)

	_, err = w.AddFromPromptSection(&saprompt.PromptSection{Name: "sec"}, AttachmentRuntime, "src")
	require.ErrorIs(t, err, ErrSessionRequired)

	_, err = w.ClearSection("sec")
	require.ErrorIs(t, err, ErrSessionRequired)
}

// TestContextWriter_AddSection写入附件 验证正常路径写到绑定会话。
func TestContextWriter_AddSection写入附件(t *testing.T) {
	m := NewManager()
	cbc := interfaces.NewAgentCallbackContext(nil, nil, newStubSession("sess-2"))

	got, err := m.BindContext(cbc).AddSection("sec", "body", AttachmentRuntime, "src",
		WithAttachmentPriority(88))
	require.NoError(t, err)
	require.Equal(t, "sess-2", got.SessionID)
	require.Equal(t, 88, got.Priority)

	items, err := m.ListByFilter(AttachmentFilter{SessionID: "sess-2", Section: "sec"})
	require.NoError(t, err)
	require.Len(t, items, 1)
}

// TestContextWriter_AddFromPromptSection渲染 验证按 language 渲染并继承 section 优先级。
func TestContextWriter_AddFromPromptSection渲染(t *testing.T) {
	m := NewManager()
	cbc := interfaces.NewAgentCallbackContext(nil, nil, newStubSession("sess-3"))
	section := &saprompt.PromptSection{
		Name:     "mode_instructions",
		Content:  map[string]string{"cn": "中文内容", "en": "english"},
		Priority: 84,
	}

	got, err := m.BindContext(cbc).AddFromPromptSection(section, AttachmentRuntime, "src", WithLanguage("cn"))
	require.NoError(t, err)
	require.Equal(t, "中文内容", got.Content)
	require.Equal(t, 84, got.Priority)
}

// TestContextWriter_AddFromPromptSection空内容短路 验证 nil 与空白内容返回 nil,nil。
func TestContextWriter_AddFromPromptSection空内容短路(t *testing.T) {
	m := NewManager()
	w := m.BindContext(interfaces.NewAgentCallbackContext(nil, nil, newStubSession("s")))

	got, err := w.AddFromPromptSection(nil, AttachmentRuntime, "src")
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = w.AddFromPromptSection(&saprompt.PromptSection{Name: "x", Content: map[string]string{"cn": "   "}},
		AttachmentRuntime, "src", WithLanguage("cn"))
	require.NoError(t, err)
	require.Nil(t, got)
}

// TestContextWriter_ClearSection 验证清空绑定会话的 section。
func TestContextWriter_ClearSection(t *testing.T) {
	m := NewManager()
	cbc := interfaces.NewAgentCallbackContext(nil, nil, newStubSession("sess-4"))
	w := m.BindContext(cbc)
	_, err := w.AddSection("sec", "body", AttachmentRuntime, "src")
	require.NoError(t, err)

	n, err := w.ClearSection("sec")
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
```

- [ ] **Step 2: 运行测试确认编译失败**

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/prompts/ -run TestBindContext -v`
Expected: FAIL —— `undefined: ErrSessionRequired`、`m.BindContext undefined`、`WithLanguage undefined`

- [ ] **Step 3: 写最小实现**

创建 `internal/agentcore/harness/prompts/attachment_writer.go`：

```go
package prompts

import (
	"errors"
	"strings"

	interfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextWriter 已绑定会话的附件写入器。
//
// 不增加任何能力：构造期把会话解析一次，方法内原样转调 Manager，
// 只是把 session_id 换成绑定值；会话为空时返回 ErrSessionRequired。
//
// Python: PromptAttachmentContextWriter（prompt_attachment_manager.py:191-265）
type ContextWriter struct {
	// manager 附件管理器
	manager *Manager
	// sessionID 构造期解析出的会话标识（可能为空）
	sessionID string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrSessionRequired 附件写入缺少会话标识。
//
// Python: raise ValueError("prompt attachment context requires session_id")（:261-264）
var ErrSessionRequired = errors.New("prompt attachment context requires session_id")

// ──────────────────────────── 导出函数 ────────────────────────────

// BindContext 返回绑定到 cbc 会话的写入器（构造期不报错，同上游）。
//
// Python: PromptAttachmentManager.bind_context（:276-279）
func (m *Manager) BindContext(cbc *interfaces.AgentCallbackContext) *ContextWriter {
	return &ContextWriter{manager: m, sessionID: resolveSessionIDFromContext(cbc)}
}

// SessionID 返回绑定的会话标识（可能为空）。
//
// Python: writer.session_id（:196）
func (w *ContextWriter) SessionID() string {
	if w == nil {
		return ""
	}
	return w.sessionID
}

// AddSection 在绑定会话里新增或覆盖一个 section。
//
// Python: PromptAttachmentContextWriter.add_section（:198-222）
func (w *ContextWriter) AddSection(
	section, content string,
	kind AttachmentKind,
	source string,
	opts ...AttachmentOption,
) (*Attachment, error) {
	if w == nil || w.manager == nil {
		return nil, ErrSessionRequired
	}
	if w.sessionID == "" {
		return nil, ErrSessionRequired
	}
	return w.manager.AddSection(w.sessionID, section, content, kind, source, opts...)
}

// AddFromPromptSection 把 PromptSection 渲染后写入绑定会话。
//
// promptSection 为 nil、或渲染结果全空白时返回 (nil, nil)，不报错。
//
// Python: PromptAttachmentContextWriter.add_from_prompt_section（:224-254）
func (w *ContextWriter) AddFromPromptSection(
	section *saprompt.PromptSection,
	kind AttachmentKind,
	source string,
	opts ...AttachmentOption,
) (*Attachment, error) {
	if section == nil {
		return nil, nil
	}
	content := renderSectionForWriter(section, opts)
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	return w.AddSection(section.Name, content, kind, source, opts...)
}

// ClearSection 删除绑定会话里的指定 section。
//
// Python: PromptAttachmentContextWriter.clear_section（:256-259）
func (w *ContextWriter) ClearSection(section string) (int, error) {
	if w == nil || w.manager == nil {
		return 0, ErrSessionRequired
	}
	if w.sessionID == "" {
		return 0, ErrSessionRequired
	}
	return w.manager.ClearSection(w.sessionID, section)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSessionIDFromContext 按上游顺序解析会话标识。
//
// 顺序：cbc.Session().GetSessionID() → InvokeInputs.Extra["session_id"/"_session_id"]
// → cbc.Extra()["session_id"/"_session_id"] → 空串。
//
// Python: _resolve_session_id_from_context（:141-158）
func resolveSessionIDFromContext(cbc *interfaces.AgentCallbackContext) string {
	if cbc == nil {
		return ""
	}
	if sess := cbc.Session(); sess != nil {
		if id := sess.GetSessionID(); id != "" {
			return id
		}
	}
	if inputs, ok := cbc.Inputs().(*interfaces.InvokeInputs); ok && inputs != nil {
		if id := extraSessionID(inputs.Extra); id != "" {
			return id
		}
	}
	return extraSessionID(cbc.Extra())
}

// extraSessionID 从字典里取 session_id / _session_id。
func extraSessionID(values map[string]any) string {
	if values == nil {
		return ""
	}
	for _, key := range []string{"session_id", "_session_id"} {
		if id, ok := values[key].(string); ok && id != "" {
			return id
		}
	}
	return ""
}
```

同文件补齐两个助手（`renderSectionForWriter` 与 `WithLanguage` 选项）：

```go
// WithLanguage 指定 PromptSection 渲染语言（仅 ContextWriter 使用）。
func WithLanguage(language string) AttachmentOption {
	return func(a *Attachment) {
		if a.Metadata == nil {
			a.Metadata = map[string]any{}
		}
		a.Metadata["language"] = language
	}
}

// renderSectionForWriter 按语言渲染 PromptSection；未指定语言时用 cn。
//
// Python: render(language) if callable(render) else str(content)（:241-242）
func renderSectionForWriter(section *saprompt.PromptSection, opts []AttachmentOption) string {
	language := "cn"
	probe := &Attachment{}
	for _, opt := range opts {
		if opt != nil {
			opt(probe)
		}
	}
	if v, ok := probe.Metadata["language"].(string); ok && v != "" {
		language = v
	}
	return section.Render(language)
}
```

同文件再加测试用会话桩（生产代码不含）：

```go
// stubSession 单测用最小会话门面（SessionFacade 全方法实现）。
type stubSession struct{ id string }

func newStubSession(id string) *stubSession { return &stubSession{id: id} }

func (s *stubSession) GetSessionID() string                     { return s.id }
func (s *stubSession) UpdateState(_ map[string]any)             {}
func (s *stubSession) GetState(_ state.StateKey) (any, error)   { return nil, nil }
func (s *stubSession) DumpState() map[string]any                { return nil }
func (s *stubSession) WriteStream(_ context.Context, _ any) error       { return nil }
func (s *stubSession) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (s *stubSession) GetEnv(_ string, _ ...any) any            { return nil }
func (s *stubSession) Interact(_ context.Context, _ any) error  { return nil }
func (s *stubSession) ClearSession(_ context.Context)           {}

var _ sessioninterfaces.SessionFacade = (*stubSession)(nil)
```

（需要 import `context`、`state "github.com/uapclaw/uapclaw-go/internal/agentcore/state"`、`sessioninterfaces ".../internal/agentcore/session/interfaces"`；`state.StateKey` 的实际导入路径以 `facade.go` 的 import 为准。）

> 说明：`WithLanguage` 通过 `Metadata["language"]` 传递渲染语言，`renderSectionForWriter` 再读回；`AddSection` 收到同一份 `opts`，`Metadata["language"]` 会一并写进附件 metadata。这是 Go 侧为承载语言参数引入的载体，**Python 无对应物**（上游是 `add_from_prompt_section(..., language=...)` 的显式关键字参数），登记为 spec §7 K5。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/prompts/ -run "TestBindContext|TestContextWriter" -v`
Expected: PASS（8 个用例）

- [ ] **Step 5: 确认包覆盖率仍 ≥85%**

Run: `go test -tags sqlite_fts5 -cover ./internal/agentcore/harness/prompts/`
Expected: `coverage: >= 85.0%`（改造前实测 89.4%）

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/prompts/attachment_writer.go internal/agentcore/harness/prompts/attachment_writer_test.go
git commit -m "feat(prompts): 新增 ContextWriter（bind_context 等价物）"
```

---

## Task 2: agent_mode 走 Writer

**Files:**
- Modify: `internal/agentcore/harness/rails/agent_mode.go:241-330`

- [ ] **Step 1: 加字段并在 Init 存 manager**

在 `AgentModeRail` 结构体（`agent_mode.go` 的字段区）加：

```go
	// attachmentManager 会话级动态提示词管理器（Init 时取一次）
	// Python: self.attachment_manager（agent_mode_rail.py:163/173）
	attachmentManager *hprompts.Manager
```

在 `Init`（`:148`）末尾、`return nil` 之前加：

```go
	// Python: self.attachment_manager = getattr(agent, "prompt_attachment_manager", None)
	r.attachmentManager = hprompts.ManagerFor(agent)
```

- [ ] **Step 2: 删除自写 attachmentManager() 方法**

删除 `agent_mode.go:241-247` 的整个方法：

```go
// attachmentManager 返回会话级动态提示词管理器（未注入时为 nil）。
//
// Python: self.attachment_manager（DeepAgent 共享给内层 ReActAgent 与各 rail）
func (r *AgentModeRail) attachmentManager() *hprompts.Manager {
	if provider, ok := r.agent.(interface{ PromptAttachments() *hprompts.Manager }); ok {
		return provider.PromptAttachments()
	}
	return nil
}
```

- [ ] **Step 3: 非 plan 分支改 Writer（含缺会话早退）**

`BeforeModelCall`（`:262` 起）里非 plan 分支现在是：

```go
	if planState.Mode != hschema.AgentModePlan.String() {
		// Python: L162: 移除 MODE_INSTRUCTIONS 节（有管理器时清空附件，产生移除 delta）
		if mgr := r.attachmentManager(); mgr != nil && sess != nil {
			if _, err := mgr.ClearSection(sess.GetSessionID(), sections.SectionModeInstructions); err != nil {
				logger.Warn(logComponent).Err(err).Msg("clear mode_instructions attachment failed")
			}
		} else if r.systemPromptBuilder != nil {
			r.systemPromptBuilder.RemoveSection(sections.SectionModeInstructions)
		}
```

改为：

```go
	if planState.Mode != hschema.AgentModePlan.String() {
		// Python: _sync_mode_attachment 首行 session is None → return（:368-370）
		if sess == nil || r.systemPromptBuilder == nil {
			return nil
		}
		// Python: L162/L307-314: 移除 MODE_INSTRUCTIONS 节
		r.systemPromptBuilder.RemoveSection(sections.SectionModeInstructions)
		if r.attachmentManager == nil {
			return nil
		}
		if _, err := r.attachmentManager.BindContext(cbc).ClearSection(sections.SectionModeInstructions); err != nil {
			logger.Warn(logComponent).
				Str("event_type", "agent_mode_attachment").
				Err(err).
				Msg("skip clearing mode_instructions attachment")
		}
		return nil
	}
```

> `syncTaskToolForModelToolInputs` / `filterHiddenTools` 的调用移到这段之后（Python 顺序：remove_section → \_sync_mode_attachment → \_sync_task_tool → filter）。

- [ ] **Step 4: plan 分支改 Writer**

plan 分支末尾（`sections.BuildPlanModeSection` 之后）现在是：

```go
	if mgr := r.attachmentManager(); mgr != nil && sess != nil {
		if _, err := mgr.AddFromPromptSection(sess.GetSessionID(), &section, hprompts.AttachmentRuntime,
			"agent_core.agent_mode_rail", r.language, hprompts.WithAttachmentContentKind("text/markdown")); err != nil {
			logger.Warn(logComponent).Err(err).Msg("agent_mode attachment failed, fallback to section")
			if r.systemPromptBuilder != nil {
				r.systemPromptBuilder.AddSection(section)
			}
		}
	} else if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.AddSection(section)
	}
```

改为：

```go
	// Python: _sync_mode_attachment 先移除旧节（:405）
	r.systemPromptBuilder.RemoveSection(sections.SectionModeInstructions)
	if r.attachmentManager == nil {
		r.systemPromptBuilder.AddSection(section)
	} else if _, err := r.attachmentManager.BindContext(cbc).AddFromPromptSection(
		&section, hprompts.AttachmentRuntime, "agent_core.agent_mode_rail",
		hprompts.WithLanguage(r.language), hprompts.WithAttachmentContentKind("text/markdown"),
	); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "agent_mode_attachment").
			Err(err).
			Msg("skip mode_instructions attachment, fallback to section")
		r.systemPromptBuilder.AddSection(section)
	}
```

- [ ] **Step 5: 跑单测**

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/rails/ -run TestAgentMode -v`
Expected: PASS，且 `grep -n "func (r \*AgentModeRail) attachmentManager" internal/agentcore/harness/rails/agent_mode.go` 无输出

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/rails/agent_mode.go
git commit -m "refactor(agent_mode): mode_instructions 附件走 ContextWriter"
```

---

## Task 3: coding_memory 走 Writer + 告警

**Files:**
- Modify: `internal/agentcore/harness/rails/memory/coding_memory_rail.go:390-425`

- [ ] **Step 1: 加字段**

`CodingMemoryRail` 结构体加：

```go
	// attachmentManager 会话级动态提示词管理器（Init 时取一次）
	// Python: self.attachment_manager（coding_memory_rail.py:92/105-107）
	attachmentManager *hprompts.Manager
```

`Init`（`:123`）里 `r.systemPromptBuilder = agent.SystemPromptBuilder()` 之后加：

```go
	r.attachmentManager = hprompts.ManagerFor(agent)
```

- [ ] **Step 2: 重写 upsertDynamicMemoryAttachment**

把 `:390-411` 的整个函数替换为：

```go
// upsertDynamicMemoryAttachment 写入（覆盖）动态记忆附件。
//
// manager 缺失时告警后跳过（Python 静默 return，Go 增强可观测性，spec §7 K1）；
// 会话缺失或写入失败时告警跳过，**不回落 system prompt**。
//
// Python: _upsert_dynamic_memory_attachment（coding_memory_rail.py:364-382）
func (r *CodingMemoryRail) upsertDynamicMemoryAttachment(cbc *agentinterfaces.AgentCallbackContext, content string) {
	if cbc == nil || content == "" {
		return
	}
	if r.attachmentManager == nil {
		logger.Warn(logComponent).
			Str("event_type", "coding_memory_attachment").
			Msg("prompt attachment manager is unavailable; skip memory attachment")
		return
	}
	if _, err := r.attachmentManager.BindContext(cbc).AddSection(
		dynamicMemorySection, content, hprompts.AttachmentMemory, dynamicMemorySource,
		hprompts.WithAttachmentPriority(dynamicMemoryPriority),
	); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "coding_memory_attachment").
			Err(err).
			Msg("skip memory attachment")
	}
}
```

- [ ] **Step 3: 重写 clearDynamicMemoryAttachment**

把 `:413-425` 替换为：

```go
// clearDynamicMemoryAttachment 清除动态记忆附件。
//
// Python: _clear_dynamic_memory_attachment（coding_memory_rail.py:384-391）
func (r *CodingMemoryRail) clearDynamicMemoryAttachment(cbc *agentinterfaces.AgentCallbackContext) {
	if cbc == nil || r.attachmentManager == nil {
		return
	}
	if _, err := r.attachmentManager.BindContext(cbc).ClearSection(dynamicMemorySection); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "coding_memory_attachment_clear").
			Err(err).
			Msg("skip clearing memory attachment")
	}
}
```

- [ ] **Step 4: 跑单测**

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/rails/memory/ -v`
Expected: PASS

- [ ] **Step 5: 补两个新用例**

在 `internal/agentcore/harness/rails/memory/coding_memory_rail_test.go` 末尾追加：

```go
// TestCodingMemoryRail_无Manager时告警跳过 验证缺 manager 不写 system prompt。
func TestCodingMemoryRail_无Manager时告警跳过(t *testing.T) {
	rail := NewCodingMemoryRail()
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, newStubSession("s"))

	require.NotPanics(t, func() { rail.upsertDynamicMemoryAttachment(cbc, "召回内容") })
}

// TestCodingMemoryRail_无会话时跳过投递 验证缺会话时附件数为 0。
func TestCodingMemoryRail_无会话时跳过投递(t *testing.T) {
	rail := NewCodingMemoryRail()
	rail.attachmentManager = hprompts.NewManager()
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)

	rail.upsertDynamicMemoryAttachment(cbc, "召回内容")

	items, err := rail.attachmentManager.ListByFilter(hprompts.AttachmentFilter{})
	require.NoError(t, err)
	require.Empty(t, items)
}
```

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/rails/memory/ -run TestCodingMemoryRail_无 -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/rails/memory/coding_memory_rail.go internal/agentcore/harness/rails/memory/coding_memory_rail_test.go
git commit -m "refactor(coding_memory): 附件走 ContextWriter，缺 manager 告警"
```

---

## Task 4: external_memory 走 Writer，删回落

**Files:**
- Modify: `internal/agentcore/harness/rails/memory/external_memory_rail.go:660-700`

- [ ] **Step 1: 加字段并在 Init 存 manager**

`ExternalMemoryRail` 结构体加：

```go
	// attachmentManager 会话级动态提示词管理器（Init 时取一次）
	// Python: self.attachment_manager（external_memory_rail.py:57/69）
	attachmentManager *hprompts.Manager
```

`Init`（`:142`）里 `r.registerProviderTools(agent)` 之前加：

```go
	r.attachmentManager = hprompts.ManagerFor(agent)
```

- [ ] **Step 2: 重写 injectMemoryContext，删 AddSection 回落**

把 `:672-697` 的整个函数替换为：

```go
// injectMemoryContext 将记忆上下文投递为历史附件。
//
// manager 缺失或会话缺失时告警后跳过，**不回落 system prompt**
// （Python external_memory_rail.py:181-200 无回落分支）。
func (r *ExternalMemoryRail) injectMemoryContext(cbc *agentinterfaces.AgentCallbackContext, rawContext string) {
	if rawContext == "" || r.systemPromptBuilder == nil || cbc == nil {
		return
	}
	// Python: fenced = self._build_memory_context_block(raw_context)
	fenced := buildMemoryContextBlock(rawContext)
	lang := r.systemPromptBuilder.Language()
	section := saprompt.PromptSection{
		Name:     externalMemoryPrefetchSection,
		Content:  map[string]string{lang: fenced},
		Priority: 55,
	}

	if r.attachmentManager == nil {
		logger.Warn(logComponent).
			Str("event_type", "external_memory_attachment").
			Msg("prompt attachment manager is unavailable; skip prefetch attachment")
		return
	}
	if _, err := r.attachmentManager.BindContext(cbc).AddFromPromptSection(
		&section, hprompts.AttachmentMemory, "agent_core.external_memory_rail",
		hprompts.WithLanguage(lang), hprompts.WithAttachmentContentKind("text/markdown"),
	); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "external_memory_attachment").
			Err(err).
			Msg("skip prefetch prompt attachment")
	}
}
```

- [ ] **Step 3: 补「不回落 system prompt」用例**

在 `internal/agentcore/harness/rails/memory/external_memory_rail_test.go` 末尾追加：

```go
// TestExternalMemoryRail_无Manager时不写系统提示词 验证删掉回落后的行为。
func TestExternalMemoryRail_无Manager时不写系统提示词(t *testing.T) {
	builder := saprompt.NewSystemPromptBuilder("cn")
	rail := &ExternalMemoryRail{systemPromptBuilder: builder}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, newStubSession("s"))

	rail.injectMemoryContext(cbc, "记忆上下文")

	require.False(t, builder.HasSection("external_memory_prefetch"))
}
```

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/rails/memory/ -run TestExternalMemoryRail_无Manager -v`
Expected: PASS

- [ ] **Step 4: 跑整个 memory 包**

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/rails/memory/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/memory/external_memory_rail.go internal/agentcore/harness/rails/memory/external_memory_rail_test.go
git commit -m "refactor(external_memory): 附件走 ContextWriter，删除 system prompt 回落"
```

---

## Task 5: skill_use 走 Writer

**Files:**
- Modify: `internal/agentcore/harness/rails/skills/skill_use_rail.go:82-84,376-381,925-975`

- [ ] **Step 1: 字段改成直存 Manager**

把结构体里的：

```go
	// attachmentManagerFor 会话级附件管理器获取函数（Init 中从 agent 取）
	// Python: self.attachment_manager（skill_use_rail.py:127/331）
	attachmentManagerFor func() *hprompts.Manager
```

改为：

```go
	// attachmentManager 会话级附件管理器（Init 中从 agent 取一次）
	// Python: self.attachment_manager（skill_use_rail.py:127/331）
	attachmentManager *hprompts.Manager
```

- [ ] **Step 2: Init 改为取一次**

`Init`（`:376-381`）里的：

```go
	// Python: self.attachment_manager = getattr(agent, "prompt_attachment_manager", None)
	if provider, ok := agent.(interface{ PromptAttachments() *hprompts.Manager }); ok {
		r.attachmentManagerFor = provider.PromptAttachments
	}
```

改为：

```go
	// Python: self.attachment_manager = getattr(agent, "prompt_attachment_manager", None)
	r.attachmentManager = hprompts.ManagerFor(agent)
```

- [ ] **Step 3: 改写入函数**

把 `:925-975` 的函数头与中段改掉：

```go
// updateRuntimeSkillAttachment 把技能新增/移除/进化经验写入变更附件。
// Python: _update_runtime_skill_attachment (:752-781)
func (r *SkillUseRail) updateRuntimeSkillAttachment(cbc *agentinterfaces.AgentCallbackContext, baseline []*skillpkg.Skill) error {
	if r.attachmentManager == nil || cbc == nil {
		return nil
	}
	writer := r.attachmentManager.BindContext(cbc)
	if writer.SessionID() == "" {
		return nil
	}
```

后半段的清/写：

```go
	content := r.buildRuntimeSkillChangeContent(additions, removals, baseline)
	if content == "" {
		if _, err := writer.ClearSection(runtimeSkillAttachmentSection); err != nil {
			return err
		}
		return nil
	}
	_, err := writer.AddSection(runtimeSkillAttachmentSection, content, hprompts.AttachmentSkill, runtimeSkillAttachmentSource)
	return err
```

- [ ] **Step 4: 跑单测**

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/rails/skills/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/skills/skill_use_rail.go
git commit -m "refactor(skill_use): skills.runtime_changes 走 ContextWriter"
```

---

## Task 6: task_completion 走 Writer

**Files:**
- Modify: `internal/agentcore/harness/rails/task_completion.go:584-625`

- [ ] **Step 1: 字段改成直存 Manager**

结构体里若有 `attachmentManagerFor func() *hprompts.Manager`，改为：

```go
	// attachmentManager 会话级附件管理器（Init 中从 agent 取一次）
	// Python: self.attachment_manager（task_completion_rail.py:142/228-230）
	attachmentManager *hprompts.Manager
```

并在 `Init`（若无则新增）里加 `r.attachmentManager = hprompts.ManagerFor(agent)`。

- [ ] **Step 2: 重写 syncGoalProtocolPrompt**

```go
// syncGoalProtocolPrompt 按 run_kind 挂/清 goal_protocol 附件。
//
// Python: _sync_goal_protocol_prompt (task_completion_rail.py L325)
func (r *TaskCompletionRail) syncGoalProtocolPrompt(_ context.Context, cbc *agentinterfaces.AgentCallbackContext, builder saprompt.SystemPromptBuilderInterface) {
	runKind := r.getRunKind(cbc)
	wantGoal := runKind == string(agentinterfaces.RunKindGoal) && r.goalManager != nil
	language := r.resolveGoalLanguage(builder)

	if r.attachmentManager == nil {
		// Python: 仅 want_goal 时告警（:342-347），非 goal 静默
		if wantGoal {
			logger.Warn(logComponent).
				Str("event_type", "task_completion_attachment").
				Msg("prompt attachment manager is unavailable; skip goal protocol attachment")
		}
		return
	}
	writer := r.attachmentManager.BindContext(cbc)
	if writer.SessionID() == "" {
		return
	}

	if wantGoal {
		section := sections.BuildGoalProtocolSection(language)
		if _, err := writer.AddFromPromptSection(
			&section, hprompts.AttachmentRuntime,
			"agent_core.task_completion_rail",
			hprompts.WithLanguage(language), hprompts.WithAttachmentContentKind("text/markdown"),
		); err != nil {
			logger.Warn(logComponent).
				Str("event_type", "task_completion_attachment").
				Err(err).
				Msg("skip goal protocol prompt attachment")
		}
		return
	}

	if _, err := writer.ClearSection(sections.SectionGoalProtocol); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "task_completion_attachment_clear").
			Err(err).
			Msg("skip clearing goal protocol prompt attachment")
	}
}
```

- [ ] **Step 3: 跑单测**

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/rails/ -run TestTaskCompletion -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/task_completion.go
git commit -m "refactor(task_completion): goal_protocol 走 ContextWriter"
```

---

## Task 7: progressive 去窄接口 + D1~D4 修复

**Files:**
- Modify: `internal/agentcore/harness/rails/progressive.go`
- Modify: `internal/agentcore/harness/rails/progressive_test.go`

- [ ] **Step 1: 删窄接口与 adapter**

删除 `progressive.go:63-88` 的三块（`promptAttachmentProvider`、`attachmentManager`、`managerAdapter`）以及 `:849-862` 的 `attachments()` 方法、`:960-975` 的三个 adapter 方法。

结构体加字段：

```go
	// attachmentManager 会话级附件管理器（Init 时取一次）
	// Python: init 里不存，每次 getattr（progressive_tool_rail.py:912）；
	// 这里按其余 rail 惯例存字段，行为等价
	attachmentManager *prompts.Manager
```

`Init` 末尾加：

```go
	r.attachmentManager = prompts.ManagerFor(agent)
```

- [ ] **Step 2: 改 syncDeferredToolAttachment 签名与调用**

`:975-990` 函数签名从 `manager attachmentManager` 改为 `w *prompts.ContextWriter`：

```go
func (r *ProgressiveToolRail) syncDeferredToolAttachment(
	_ context.Context,
	sess sessioninterfaces.SessionFacade,
	w *prompts.ContextWriter,
	current map[string]string,
) error {
	if sess == nil || w == nil || w.SessionID() == "" {
		return nil
	}
	sessionID := w.SessionID()
```

`BeforeModelCall`（`:274`）的调用点改为：

```go
	if r.attachmentManager != nil {
		if err := r.syncDeferredToolAttachment(ctx, cbc.Session(), r.attachmentManager.BindContext(cbc), current); err != nil {
			return err
		}
	}
```

- [ ] **Step 3: D2/D4 —— 指纹基线与初始指纹**

`:996-1002` 的状态读取处改为：

```go
	catalogState := catalogStateOf(sess)
	initialTools := initialStateTools(catalogState, currentTools)
	previousTools := previousStateTools(catalogState)
	initialFingerprints := initialFingerprintsOf(catalogState, currentFingerprints)
	previousFingerprints := previousFingerprintsOf(catalogState)
	initialized := catalogState["initialized"] == true
	previousLanguage := stringOf(catalogState["language"])
	version := intOf(catalogState["version"], 1)
	deltaCount := intOf(catalogState["delta_count"], 0)
```

`:1004-1022` 的附件恢复段改为：

```go
	// 热重载/新 Session：从附件 metadata 恢复上次目录，避免重复推送同一差异
	if existing := firstAttachment(w, deferredToolAttachmentSection, deferredToolAttachmentSource); existing != nil {
		if recovered, ok := normalizeDeferredCatalog(mapValueOf(existing.Metadata[deferredToolCatalogMetadataKey])); ok {
			previousTools = recovered
			initialized = true
			// Python: :1000-1001 指纹真的赋值（原实现 `_ = fingerprints` 丢弃）
			if fingerprints, ok := normalizeDeferredCatalog(mapValueOf(existing.Metadata[deferredToolCatalogFingerprintsMetadataKey])); ok {
				previousFingerprints = fingerprints
			}
			if value, ok := existing.Metadata["catalog_language"].(string); ok && value != "" {
				previousLanguage = value
			}
			if value, ok := existing.Metadata["catalog_version"].(int); ok && value > version {
				version = value
			}
			// Python: :1027-1028 非零才覆盖（0 是"刚发过 snapshot"，不能清掉累积计数）
			if value, ok := existing.Metadata["delta_count"].(int); ok && value != 0 {
				deltaCount = value
			}
		}
	}
```

- [ ] **Step 4: D3 —— baseline 拆两条并参与 updated 判定**

`:1024-1046` 改为：

```go
	baseline := initialTools
	baselineFingerprints := initialFingerprints
	if initialized {
		baseline = previousTools
		baselineFingerprints = previousFingerprints
	}
	added := make(map[string]string)
	updated := make(map[string]string)
	for name, description := range currentTools {
		existing, ok := baseline[name]
		if !ok {
			added[name] = description
			continue
		}
		if existing != description {
			updated[name] = description
			continue
		}
		// Python: :1054-1055 指纹变化同样算 updated
		if baselineFingerprints[name] != currentFingerprints[name] {
			updated[name] = description
		}
	}
```

- [ ] **Step 5: D4 —— saveCatalogState 写两个指纹键 + 常量**

常量区（`:100-115` 附近）加：

```go
	// deferredToolCatalogFingerprintsStateKey 会话状态键：上次已发布的目录指纹
	deferredToolCatalogFingerprintsStateKey = "fingerprints"
	// deferredToolCatalogInitialFingerprintsStateKey 会话状态键：初始目录指纹
	deferredToolCatalogInitialFingerprintsStateKey = "initial_fingerprints"
```

`saveCatalogState`（`:1125` 的裸字符串）改为：

```go
func (r *ProgressiveToolRail) saveCatalogState(
	sess sessioninterfaces.SessionFacade,
	initialTools, currentTools, initialFingerprints, currentFingerprints map[string]string,
	version, deltaCount int,
	language string,
) {
	if sess == nil {
		return
	}
	sess.UpdateState(map[string]any{deferredToolCatalogStateKey: map[string]any{
		"initialized":                              true,
		"version":                                  version,
		"delta_count":                              deltaCount,
		"language":                                 language,
		"initial_tools":                            initialTools,
		"tools":                                    currentTools,
		deferredToolCatalogInitialFingerprintsStateKey: initialFingerprints,
		deferredToolCatalogFingerprintsStateKey:        currentFingerprints,
		deferredToolRegistryRevisionMetadataKey:        registryRevisionOf(r.abilityManager()),
	}})
}
```

同步改三处 `saveCatalogState` 调用（`:1073`、`:1089`、`:1050` 附近的 `r.saveCatalogState(sess, initialTools, currentTools, currentFingerprints, version, 0, language)`）为传入 `initialFingerprints, currentFingerprints`。

- [ ] **Step 6: writeCatalogAttachment 改吃 writer**

```go
func (r *ProgressiveToolRail) writeCatalogAttachment(
	w *prompts.ContextWriter,
	language string,
	version int,
	mode string,
	content string,
	currentTools map[string]string,
	fingerprints map[string]string,
	deltaCount int,
) error {
	_, err := w.AddSection(
		deferredToolAttachmentSection,
		content,
		prompts.AttachmentTool,
		deferredToolAttachmentSource,
		prompts.WithAttachmentPriority(75),
		prompts.WithAttachmentContentKind("text/markdown"),
		prompts.WithAttachmentMetadata(map[string]any{
			"catalog_version":                          version,
			"catalog_mode":                             mode,
			"catalog_language":                         language,
			"delta_count":                              deltaCount,
			deferredToolCatalogMetadataKey:             currentTools,
			deferredToolCatalogFingerprintsMetadataKey: fingerprints,
			deferredToolRegistryRevisionMetadataKey:    registryRevisionOf(r.abilityManager()),
		}),
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "progressive_catalog_attachment").
			Err(err).
			Msg("failed to update deferred-tool attachment")
	}
	return err
}
```

三处调用点（`:1089`、`:1084` 附近）把 `(manager, sessionID, language, ...)` 改成 `(w, language, ...)`。

- [ ] **Step 7: 补两个非导出助手**

```go
// initialFingerprintsOf 读取初始指纹，缺失时用当前指纹播种。
// Python: initial_fingerprints（progressive_tool_rail.py:942-946）
func initialFingerprintsOf(catalogState map[string]any, current map[string]string) map[string]string {
	if normalized, ok := normalizeDeferredCatalog(mapValueOf(catalogState[deferredToolCatalogInitialFingerprintsStateKey])); ok {
		return normalized
	}
	normalized, _ := normalizeDeferredCatalog(current)
	return normalized
}

// previousFingerprintsOf 读取上次已发布的指纹。
// Python: previous_fingerprints（:956-960）
func previousFingerprintsOf(catalogState map[string]any) map[string]string {
	if normalized, ok := normalizeDeferredCatalog(mapValueOf(catalogState[deferredToolCatalogFingerprintsStateKey])); ok {
		return normalized
	}
	return map[string]string{}
}

// firstAttachment 取 section/source 下的首条附件（无则 nil）。
// Python: _get_deferred_tool_attachment（list_by_filter 取首条）
func firstAttachment(w *prompts.ContextWriter, section, source string) *prompts.Attachment {
	items, err := w.Manager().ListByFilter(prompts.AttachmentFilter{
		SessionID: w.SessionID(), Section: section, Source: source,
	})
	if err != nil || len(items) == 0 {
		return nil
	}
	return items[0]
}
```

`ContextWriter` 需要补一个只读访问器（在 `attachment_writer.go` 加）：

```go
// Manager 返回底层附件管理器。
func (w *ContextWriter) Manager() *Manager {
	if w == nil {
		return nil
	}
	return w.manager
}
```

`firstAttachment` 里要先判空：`if w == nil || w.Manager() == nil { return nil }`。

- [ ] **Step 8: 改测试 fake**

`progressive_test.go` 里若用 fake `attachmentManager`，改为构造真 Manager：

```go
mgr := prompts.NewManager()
rail.attachmentManager = mgr
```

断言处 `fake.assertSection(...)` 改为 `mgr.ListByFilter(...)`。

- [ ] **Step 9: 补三个恢复逻辑用例**

在 `progressive_test.go` 末尾追加：

```go
// TestSyncDeferredToolAttachment_DeltaCount零值不覆盖 验证附件 delta_count=0 不清掉累积计数。
func TestSyncDeferredToolAttachment_DeltaCount零值不覆盖(t *testing.T) {
	mgr := prompts.NewManager()
	mgr.AddSection("s1", "progressive_deferred_tools", "prev", prompts.AttachmentTool, "progressive_tool_rail",
		prompts.WithAttachmentMetadata(map[string]any{
			"catalog_tools":      map[string]string{"a": "A"},
			"catalog_version":    3,
			"delta_count":        0,
			"catalog_language":   "cn",
			"catalog_fingerprints": map[string]string{"a": "fa"},
		}))
	sess := newStatefulSession("s1",
		map[string]any{"initialized": true, "version": 3, "delta_count": 7,
			"language": "cn", "tools": map[string]any{"a": "A"}})

	rail := NewProgressiveToolRail(nil)
	rail.attachmentManager = mgr
	require.NoError(t, rail.syncDeferredToolAttachment(context.Background(), sess,
		mgr.BindContextForSession("s1"), map[string]string{"a": "A"}))

	state := sess.data[deferredToolCatalogStateKey].(map[string]any)
	require.Equal(t, 7, state["delta_count"], "delta_count=0 不应覆盖累积计数")
}

// TestSyncDeferredToolAttachment_指纹恢复参与基线 验证 description 不变但指纹变化判 updated。
func TestSyncDeferredToolAttachment_指纹恢复参与基线(t *testing.T) {
	mgr := prompts.NewManager()
	mgr.AddSection("s2", "progressive_deferred_tools", "prev", prompts.AttachmentTool, "progressive_tool_rail",
		prompts.WithAttachmentMetadata(map[string]any{
			"catalog_tools":        map[string]string{"a": "A"},
			"catalog_fingerprints": map[string]string{"a": "old-fingerprint"},
			"catalog_version":      2,
			"catalog_language":     "cn",
		}))
	sess := newStatefulSession("s2", map[string]any{"initialized": true, "version": 2, "language": "cn"})

	rail := NewProgressiveToolRail(nil)
	rail.attachmentManager = mgr
	rail.cachedFingerprints = map[string]string{"a": "new-fingerprint"}
	require.NoError(t, rail.syncDeferredToolAttachment(context.Background(), sess,
		mgr.BindContextForSession("s2"), map[string]string{"a": "A"}))

	items, _ := mgr.ListByFilter(prompts.AttachmentFilter{SessionID: "s2",
		Section: "progressive_deferred_tools"})
	require.Len(t, items, 1)
	require.Equal(t, "delta", items[0].Metadata["catalog_mode"], "指纹变化应判为 delta 而非跳过")
}

// TestSyncDeferredToolAttachment_初始指纹播种 验证无状态时写入 initial_fingerprints。
func TestSyncDeferredToolAttachment_初始指纹播种(t *testing.T) {
	mgr := prompts.NewManager()
	sess := newStatefulSession("s3", map[string]any{})
	rail := NewProgressiveToolRail(nil)
	rail.attachmentManager = mgr
	rail.cachedFingerprints = map[string]string{"a": "f1"}

	require.NoError(t, rail.syncDeferredToolAttachment(context.Background(), sess,
		mgr.BindContextForSession("s3"), map[string]string{"a": "A"}))

	state, ok := sess.data[deferredToolCatalogStateKey].(map[string]any)
	require.True(t, ok)
	require.NotNil(t, state[deferredToolCatalogInitialFingerprintsStateKey])
}
```

> 上面用例用到的测试助手要按真实情况实现（不要留空）：
>
> - `statefulSession`：`progressive.go` 的目录状态走 `sess.GetState(stateKey)` / `sess.UpdateState(...)`（不是 `DumpState`），所以测试会话必须真的存状态。在 `progressive_test.go` 里加一个
>   `type statefulSession struct { id string; mu sync.Mutex; data map[string]any }`，`GetSessionID()` 返回 `id`，`UpdateState(m)` 按 key 合并进 `data`，`GetState(k)` 用 `data[string(k)]` 走 `state.StateKey` 转换；其余方法同 `stubSession` 的空实现，并加 `var _ sessioninterfaces.SessionFacade = (*statefulSession)(nil)`。
>   用例改成 `newStatefulSession("s1", map[string]any{...})`，断言读 `sess.data[deferredToolCatalogStateKey]`。
> - `(*prompts.Manager) BindContextForSession(id string) *prompts.ContextWriter`：**生产代码**加在 `attachment_writer.go`——`func (m *Manager) BindContextForSession(sessionID string) *ContextWriter { return &ContextWriter{manager: m, sessionID: sessionID} }`，供无 cbc 的单元测试与 progressive 内部构造。
> - **指纹注入点**：`progressive.go:381-390` 的 `collectDeferredFingerprints()` 从 `r.toolSearchRegistry`（`AbilityManagerInterface`）读工具卡算指纹，**没有可直接注入的字段**。测试里不要新造字段，改成喂一个可变的 `AbilityManagerInterface` 假实现给 `r.toolSearchRegistry`：先注册工具 A（schema v1）跑一次建基线，再把同一工具的 schema 改成 v2 跑第二次，断言第二次 `catalog_mode == "delta"`。上面步骤里出现的 `rail.cachedFingerprints = ...` 一律理解为这一构造过程。

- [ ] **Step 10: 跑测试**

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/rails/ -run "TestSyncDeferredToolAttachment|TestProgressive" -v`
Expected: PASS

- [ ] **Step 11: 提交**

```bash
git add internal/agentcore/harness/rails/progressive.go internal/agentcore/harness/rails/progressive_test.go internal/agentcore/harness/prompts/attachment_writer.go
git commit -m "refactor(progressive): 去窄接口直存 Manager；修复 delta_count/指纹基线恢复"
```

---

## Task 8: team_policy 走 Writer

**Files:**
- Modify: `internal/agent_teams/rails/team_policy_rail.go:407-450`

- [ ] **Step 1: 改投递段**

把 `:438-448` 的 `DeliverText` 段改为：

```go
	writer := manager.BindContext(cbc)
	if _, err := writer.AddSection(
		teamContextAttachmentSection, content,
		hprompts.AttachmentRuntime, teamContextAttachmentSource,
		hprompts.WithAttachmentPriority(teamContextAttachmentPriority),
		hprompts.WithAttachmentContentKind("text/markdown"),
	); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "team_context_attachment").
			Str("member_name", r.memberName).
			Err(err).
			Msg("failed to queue team context attachment")
		return false
	}
```

`:424-427` 的 `sessionID` 相关保留（`HasHistorySnapshot` 仍需它），`sess.GetSessionID()` 保持不变。

- [ ] **Step 2: 跑团队测试**

Run: `go test -tags sqlite_fts5 ./internal/agent_teams/rails/ -run TestTeamPolicy -v`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agent_teams/rails/team_policy_rail.go
git commit -m "refactor(team_policy): team.context 附件走 ContextWriter"
```

---

## Task 9: team_plan_mode 补附件投递

**Files:**
- Modify: `internal/agent_teams/rails/team_plan_mode_rail.go:73-160`

- [ ] **Step 1: 加字段并在 Init 存 manager**

结构体加：

```go
	// attachmentManager 会话级附件管理器（Init 时取一次）
	// Python: self.attachment_manager（team_plan_mode_rail.py:50/56）
	attachmentManager *hprompts.Manager
```

`Init`（`:74`）末尾 `return r.specializePlanAgent()` 之前加：

```go
	r.attachmentManager = hprompts.ManagerFor(agent)
```

- [ ] **Step 2: 非 plan 分支补附件清除**

`BeforeModelCall` 里三处 `r.systemPromptBuilder.RemoveSection(harnesssections.SectionModeInstructions)` 之后都要补清附件。抽成一个方法：

```go
// clearModeInstructions 移除 system 节并清空附件。
// Python: _sync_plan_attachment 非 plan 分支（team_plan_mode_rail.py:87-92）
func (r *TeamPlanModeRail) clearModeInstructions(cbc *agentinterfaces.AgentCallbackContext) {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(harnesssections.SectionModeInstructions)
	}
	if r.attachmentManager == nil || cbc == nil {
		return
	}
	if _, err := r.attachmentManager.BindContext(cbc).ClearSection(harnesssections.SectionModeInstructions); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "team_plan_mode_attachment").
			Err(err).
			Msg("failed to clear mode_instructions attachment")
	}
}
```

三处调用点（`sess == nil`、`state == nil`、`state.PlanMode.Mode != "plan"`）都改为 `r.clearModeInstructions(cbc); return nil`。

- [ ] **Step 3: plan 分支改投附件**

`:155-159` 的 `r.systemPromptBuilder.AddSection(*section)` 改为：

```go
	if section == nil {
		return nil
	}
	// Python: 先移除 system 节，再投附件（:106-117）
	r.systemPromptBuilder.RemoveSection(harnesssections.SectionModeInstructions)
	if r.attachmentManager == nil {
		r.systemPromptBuilder.AddSection(*section)
		return nil
	}
	if _, err := r.attachmentManager.BindContext(cbc).AddFromPromptSection(
		section, hprompts.AttachmentRuntime, "agent_core.team_plan_mode_rail",
		hprompts.WithLanguage(language), hprompts.WithAttachmentContentKind("text/markdown"),
	); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "team_plan_mode_attachment").
			Err(err).
			Msg("failed to queue plan attachment")
		r.systemPromptBuilder.AddSection(*section)
	}
	return nil
```

- [ ] **Step 4: 跑团队测试**

Run: `go test -tags sqlite_fts5 ./internal/agent_teams/rails/ -run TestTeamPlanMode -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/rails/team_plan_mode_rail.go
git commit -m "feat(team_plan_mode): mode_instructions 走附件，无 manager 时回落 section"
```

---

## Task 10: context_assemble 的 heartbeat 改投附件

**Files:**
- Modify: `internal/agentcore/harness/rails/context_engineer/context_assemble_rail.go:104-200`

- [ ] **Step 1: 加字段并在 Init 存 manager**

结构体加：

```go
	// attachmentManager 会话级附件管理器（Init 时取一次）
	// Python: self.attachment_manager（context_assemble_rail.py:57/64）
	attachmentManager *hprompts.Manager
```

`Init`（`:60` 附近）加：

```go
	r.attachmentManager = hprompts.ManagerFor(agent)
```

- [ ] **Step 2: heartbeat 上下文改投附件**

把 `:170-177` 的：

```go
		if len(files) > 0 {
			contextSection := sections.BuildContextSection(files, lang)
			r.systemPromptBuilder.AddSection(contextSection)
		} else {
			r.systemPromptBuilder.RemoveSection(sections.SectionContext)
		}
```

改为：

```go
		// Python: 先移除 system 里的 context 节（:143）
		r.systemPromptBuilder.RemoveSection(sections.SectionContext)
		if isHeartbeat && len(files) > 0 {
			// Python: context.heartbeat 只走附件（_ATTACHMENT_CONTEXT_SECTIONS，:31-33/79-85）
			contextSection := sections.BuildContextSection(files, lang)
			if r.attachmentManager == nil {
				logger.Warn(logComponent).
					Str("event_type", "context_assemble_attachment").
					Msg("prompt attachment manager is unavailable; skip attachment sections")
			} else if _, err := r.attachmentManager.BindContext(cbc).AddFromPromptSection(
				&contextSection, hprompts.AttachmentFile, "agent_core.context_assemble_rail",
				hprompts.WithLanguage(lang), hprompts.WithAttachmentContentKind("text/markdown"),
			); err != nil {
				logger.Warn(logComponent).
					Str("event_type", "context_assemble_attachment").
					Err(err).
					Msg("skip attachment section")
			}
		} else if len(files) > 0 {
			contextSection := sections.BuildContextSection(files, lang)
			r.systemPromptBuilder.AddSection(contextSection)
		}
```

并删除 `:179-182` 那段解释「Go 仍保持 add_section」的过时注释。

- [ ] **Step 3: 跑测试**

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/rails/context_engineer/ -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/context_engineer/context_assemble_rail.go
git commit -m "feat(context_assemble): heartbeat 上下文改投历史附件"
```

---

## Task 11: 删除旧助手与 doc.go 同步

**Files:**
- Modify: `internal/agentcore/harness/prompts/attachment_rail.go`
- Modify: `internal/agentcore/harness/prompts/attachment_rail_test.go`
- Modify: `internal/agentcore/harness/prompts/doc.go`
- Modify: `internal/agentcore/harness/rails/memory/doc.go`

- [ ] **Step 1: 确认零调用方**

Run:
```bash
grep -rn "prompts\.Deliver\|hprompts\.Deliver\|DeliverText\|ClearDeliveredSection" internal tests cmd
```
Expected: **无输出**（Task 2~10 已全部改完）

- [ ] **Step 2: 删除三个函数**

从 `attachment_rail.go` 删除 `Deliver`（`:29-47`）、`DeliverText`（`:49-77`）、`ClearDeliveredSection`（`:79-95`），只保留 `RailAttachmentProvider` 与 `ManagerFor`。文件尾补段落注释说明保留原因。

- [ ] **Step 3: 删除对应测试**

从 `attachment_rail_test.go` 删除引用这三个函数的用例，只保留 `TestManagerFor` 系列。

Run: `go test -tags sqlite_fts5 ./internal/agentcore/harness/prompts/ -v`
Expected: PASS

- [ ] **Step 4: doc.go 文件树**

`prompts/doc.go` 的树里在 `attachment_store.go` 之后加：

```
//	├── attachment_writer.go   # 已绑定会话的附件写入器（bind_context 等价物）
```

并把 `attachment_rail.go` 那行描述改为 `# ManagerFor（getattr(agent, "prompt_attachment_manager") 等价物）`。

`rails/memory/doc.go` 第 6 行「在 BeforeModelCall 中注入记忆 section 到系统提示词」改为「在 BeforeModelCall 中把记忆内容投递为历史附件（`ContextWriter`）」。

- [ ] **Step 5: 编译与全量单测**

Run:
```bash
go build -tags sqlite_fts5 ./...
go vet -tags "sqlite_fts5 test integration" ./internal/... ./cmd/...
go test -tags sqlite_fts5 test ./internal/...
```
Expected: 全部通过

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/prompts/attachment_rail.go internal/agentcore/harness/prompts/attachment_rail_test.go internal/agentcore/harness/prompts/doc.go internal/agentcore/harness/rails/memory/doc.go
git commit -m "refactor(prompts): 删除 Deliver/DeliverText/ClearDeliveredSection，同步 doc.go"
```

---

## Task 12: 偏差 7 —— 集成用例替换行级豁免

**Files:**
- Create: `tests/integration/swarm/server/command_context_default_mode_test.go`
- Modify: `tests/integration/swarm/server/doc.go`
- Modify: `.integration-coverage-exempt:190-194`

- [ ] **Step 1: 写集成用例**

创建 `tests/integration/swarm/server/command_context_default_mode_test.go`：

```go
//go:build integration

package server_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// CommandContextDefaultModeSuite command.context 不带 mode 的缺省解析链路。
//
// 覆盖 runtime/request.go 的「mode == "" → agent」分支：请求由 AgentServer 入站
// 分发到 handleCommandContext → ApplyResolvedModeToRequest → AgentManager.GetAgent
// 真装配。该分支此前靠 .integration-coverage-exempt 的行级豁免放过，且豁免区间
// 已漂移到 agent_manager.go 的 GetChatAgent（2026-11-05 偏差 7）。
type CommandContextDefaultModeSuite struct {
	isuite.BaseIntegrationSuite
	// env 进程内 AgentServer 环境
	env *testhelpers.ServerEnv
}

// TestCommandContextDefaultModeSuite 套件入口。
func TestCommandContextDefaultModeSuite(t *testing.T) {
	suite.Run(t, new(CommandContextDefaultModeSuite))
}

// SetupSuite 启动进程内 AgentServer。
func (s *CommandContextDefaultModeSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)
}

// TestCommandContextWithoutModeUsesAgent 验证不带 mode 的 command.context 走 agent 而非 plan。
func (s *CommandContextDefaultModeSuite) TestCommandContextWithoutModeUsesAgent() {
	createID := s.env.Send(string(schema.ReqMethodSessionCreate), map[string]any{}, "", false)
	created := s.env.AwaitUnary(createID)
	s.Require().NotNil(created)
	sessionID, _ := created.Payload["session_id"].(string)
	s.Require().NotEmpty(sessionID, "session.create 应返回 session_id")

	// params 刻意不带 mode：命中 runtime/request.go 的缺省分支
	ctxID := s.env.Send(string(schema.ReqMethodCommandContext), map[string]any{}, sessionID, false)
	resp := s.env.AwaitUnary(ctxID)
	s.Require().NotNil(resp, "command.context 应有响应")
	s.True(resp.OK, "command.context 应成功: %+v", resp.Payload)

	_, hasUsage := resp.Payload["total_tokens"]
	_, hasWindow := resp.Payload["context_window"]
	s.True(hasUsage || hasWindow, "payload 应含上下文占用字段: %+v", resp.Payload)

	// 缺省 mode 必须不是 plan
	planID := s.env.Send(string(schema.ReqMethodSessionPlanStatus), map[string]any{}, sessionID, false)
	planResp := s.env.AwaitUnary(planID)
	s.Require().NotNil(planResp)
	mode, _ := planResp.Payload["mode"].(string)
	s.NotEqual("plan", mode, "缺省轮次不应进入 plan")
}
```

- [ ] **Step 2: doc.go 加文件树条目**

`tests/integration/swarm/server/doc.go` 的树里加：

```
//	├── command_context_default_mode_test.go # command.context 缺省 mode 解析链路
```

- [ ] **Step 3: 先证明覆盖，再删豁免**

Run:
```bash
go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/ \
  -run TestCommandContextDefaultModeSuite -v \
  -coverpkg=./internal/swarm/... -coverprofile=/tmp/ctx_default.cov
grep "runtime/request.go" /tmp/ctx_default.cov | head -20
```
Expected: 输出里能看到 `runtime/request.go` 的 `196`/`197`/`198` 三行 `count > 0`。
**若为 0，先排查（用例是否真打到了 handleCommandContext），不要进入下一步。**

- [ ] **Step 4: 删豁免条目**

删除 `.integration-coverage-exempt` 第 190-194 行整块：

```
# ── 2026-11-04 默认模式对齐上游 agent（normal）──
# agent_manager.go L802-L815：「mode 缺省 → agent」分支只在内部调用方构造无 mode
# 的 AgentRequest 时进入；网关/ACP/Web 都在 params 里 setdefault("mode")
# （app_gateway.py:1650 的 default_mode），集成层恒不走该分支。
# 行为由 agent_manager_test.go 的 TestResolveModeFromRequest 覆盖。
internal/swarm/server/runtime/agent_manager.go#L802-L815
```

Run: `grep -n "agent_manager.go#L802-L815" .integration-coverage-exempt`
Expected: 无输出

- [ ] **Step 5: 跑该目录全量集成**

Run: `go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add tests/integration/swarm/server/command_context_default_mode_test.go tests/integration/swarm/server/doc.go .integration-coverage-exempt
git commit -m "test(server): command.context 缺省 mode 集成覆盖，撤掉漂移的行级豁免"
```

---

## Task 13: 回归与验收

**Files:** 无新增

- [ ] **Step 1: 全量回归**

Run:
```bash
go build -tags sqlite_fts5 ./...
go vet -tags "sqlite_fts5 test integration" ./internal/... ./cmd/...
go test -tags "sqlite_fts5 test" ./internal/...
go test -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/attachment/... ./tests/integration/agentcore/harness/rails/... ./tests/integration/swarm/server/... ./tests/integration/swarm/gateway/... ./tests/integration/agent_teams/...
```
Expected: 全部 PASS

- [ ] **Step 2: 删除清单锚点验收**

Run:
```bash
grep -rn "prompts\.Deliver\|hprompts\.Deliver\|DeliverText\|ClearDeliveredSection" internal tests cmd
grep -rn "managerAdapter\|attachmentManager interface" internal
grep -rn "func (r \*AgentModeRail) attachmentManager" internal
grep -n "agent_manager.go#L802-L815" .integration-coverage-exempt
grep -rn "reflect\.Ptr\b\|reflect\.PtrTo" --include=*.go internal cmd tests
```
Expected: 前四条**全部无输出**；第五条只剩 `node_modules/flatted` 之下（第三方产物）

- [ ] **Step 3: 集成审计**

Run: `go run ./cmd/integration-audit -verify`
Expected: 无违规（豁免数不变或减少）

- [ ] **Step 4: 覆盖率**

Run:
```bash
go test -tags sqlite_fts5 -cover ./internal/agentcore/harness/prompts/
go test -tags sqlite_fts5 -cover ./internal/agentcore/harness/rails/ ./internal/agentcore/harness/rails/memory/ ./internal/agent_teams/rails/
```
Expected: 均 ≥85%

- [ ] **Step 5: 推送**

```bash
git push origin main
```

---

## 自审记录

**1. Spec 覆盖**

| Spec 节 | 对应任务 |
| --- | --- |
| §3.1 `ContextWriter` | Task 1 |
| §3.2 9 处统一形状 | Task 2~10 |
| §3.3 manager/会话缺失逐 rail 分支 | Task 2（agent_mode 回落）、Task 3（coding warn）、Task 4（external 不回落）、Task 5（skill 静默）、Task 6（task_completion want_goal）、Task 7（progressive 静默）、Task 8（team_policy false）、Task 9（team_plan_mode 回落）、Task 10（context_assemble warn） |
| §3.4 progressive 去窄接口 | Task 7 Step 1 |
| §3.5 D1~D4 | Task 7 Step 2~7 |
| §4 偏差 7 | Task 12 |
| §5 删除清单 | Task 11（`Deliver*`）、Task 7（`managerAdapter`/`attachmentManager`）、Task 2（`attachmentManager()`）、Task 7 Step 3（`_ = fingerprints`）、Task 12 Step 4（豁免） |
| §6 doc.go | Task 11 Step 4、Task 12 Step 2 |
| §7 已知偏差 | 无需实现（文档记录）；K5（`WithLanguage` 载体）已在 Task 1 Step 3 注明并需回填 spec |
| §8 测试矩阵 | Task 1、3、4、7、12 |

**2. 占位符扫描**：无 TBD / "later" / "similar to Task N"。测试基础设施已按真实代码校正：`interfaces` 包没有 `NewTestSession`（真实方法在 `tests/integration/suite/session_suite.go:54`），单测改用自带 `stubSession` / `statefulSession`；`ProgressiveToolRail` 没有 `cachedFingerprints` 注入点，改用 `toolSearchRegistry` 假实现构造指纹差异。

**3. 类型一致性**：`ContextWriter` 的方法名在 Task 1 定义（`AddSection`/`AddFromPromptSection`/`ClearSection`/`SessionID`/`Manager`），Task 2~7 引用一致；`WithLanguage` 在 Task 1 引入，Task 2/6/9/10 使用一致；`ErrSessionRequired` 仅在 Task 1 定义与测试中出现。

**4. 待回填 spec**：`WithLanguage` 通过 `Metadata["language"]` 传渲染语言是 Go 侧载体（Python 是显式关键字参数），属 §7 新增偏差 K5，实施时补进 spec。
