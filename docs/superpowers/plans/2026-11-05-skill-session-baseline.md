# Skill 会话基线 + skill_tool 对齐 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Go 侧 SkillUseRail 的会话技能基线（`skill_use` state）、运行时变更附件、`GetSkillsForSession` 视图与清基线触发链接上，并把 `skill_tool` 与上游 HEAD 全量对齐。

**Architecture:** 全部改动在既有包内完成——`harness/rails/skills`（rail 侧基线/附件/同步，不新建包）、`harness/tools/skills`（工具侧 session 透传、路径安全、目录树、媒体提示，新增 `markdown_media.go`）、`swarm/server/adapter`（`RefreshSkillRails` + 三处触发点）。逐文件对齐 `openjiuwen/harness/rails/skills/skill_use_rail.py`（HEAD `5a35c39f`），方法归属与步骤顺序不改。

**Tech Stack:** Go 1.22、testify、`internal/agentcore/session/{interfaces,state}`、`internal/agentcore/harness/prompts`（附件管理器）。

**上游基准：** `~/opensource/agent-core`（0.1.20 / `5a35c39f`）。行号均指 `openjiuwen/harness/rails/skills/skill_use_rail.py`，另有标注者除外。

**设计文档：** `docs/superpowers/specs/2026-11-05-skill-session-baseline-design.md`

---

## 文件结构

| 文件 | 职责 | 动作 |
|---|---|---|
| `internal/agentcore/harness/rails/skills/skill_use_rail.go` | rail：基线存取、提示词节来源、附件、同步入口、init 对齐 | 修改 |
| `internal/agentcore/harness/rails/skills/skill_use_rail_baseline_test.go` | 基线/附件/同步单测（新文件，避免现有测试文件过长） | 创建 |
| `internal/agentcore/harness/tools/skills/skill_tool.go` | skill_tool：session 透传、路径安全、layout、`content` 渲染、媒体提示 | 修改 |
| `internal/agentcore/harness/tools/skills/markdown_media.go` | markdown/HTML/裸路径媒体检测（对齐上游同名模块） | 创建 |
| `internal/agentcore/harness/tools/skills/markdown_media_test.go` | 媒体检测单测 | 创建 |
| `internal/agentcore/harness/tools/skills/skill_layout.go` | 目录树 + 嵌套子技能 + layout 元数据 + 附录渲染 | 创建 |
| `internal/agentcore/harness/tools/skills/skill_layout_test.go` | 目录树/嵌套/附录单测 | 创建 |
| `internal/agentcore/harness/tools/skills/list_skill.go` | list_skill：getter 签名改带 session | 修改 |
| `internal/agentcore/harness/tools/skills/doc.go` | 文件树补 `markdown_media.go` / `skill_layout.go` | 修改 |
| `internal/swarm/server/adapter/deep_adapter_skills.go` | `RefreshSkillRails`（新文件，adapter 技能刷新集中处） | 创建 |
| `internal/swarm/server/adapter/deep_adapter_mcp.go` | MCP 热同步后调 `RefreshSkillRails` | 修改 |
| `internal/swarm/server/adapter/deep_adapter_config.go` | 配置热更新后调 `RefreshSkillRails` | 修改 |
| `internal/swarm/agents/harness/tools/skill_toolkit.go` | install/uninstall 成功后 reload + 清基线 | 修改 |
| `tests/integration/agentcore/harness/skills/skill_baseline_test.go` + `doc.go` | 跨 harness rail + tools + session 的集成用例 | 创建 |
| `IMPLEMENTATION_PLAN.md` | 9.19-23 原文追加回填段 | 修改 |

**关键类型约定（后续任务全部沿用）**

```go
// 包 rails/skills 内（skill_use_rail.go）
type skillBaselineState struct {
    SchemaVersion  int                  `json:"schema_version"`
    BaselineSkills []skillBaselineEntry `json:"baseline_skills"`
}

type skillBaselineEntry struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Directory   string `json:"directory"`
}

// 包 tools/skills 内（skill_tool.go）
// skillGetter 返回会话可见技能视图；session 为 nil 时由 rail 退化为当前加载集合
type skillGetter func(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill
```

**通用命令（每个 Task 开头先确认无并发编译进程）**

```bash
pgrep -f 'go (build|test)' || echo "no go process"
export GOPROXY=https://goproxy.cn,direct
```

---

### Task 1: 基线数据契约与存取（ensure/load/save/clear）

**Files:**
- Modify: `internal/agentcore/harness/rails/skills/skill_use_rail.go`（常量区 + 结构体区 + 非导出函数区）
- Test: `internal/agentcore/harness/rails/skills/skill_use_rail_baseline_test.go`（新建）

- [ ] **Step 1: 写失败测试（基线首次落盘 + 二次不覆盖）**

新建 `internal/agentcore/harness/rails/skills/skill_use_rail_baseline_test.go`：

```go
package skills

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
)

// ──────────────────────────── 结构体 ────────────────────────────

// skillFakeSession 测试用会话门面（仅实现基线所需能力，其余空实现）
type skillFakeSession struct {
	// id 会话 id
	id string
	// stateMap 状态表
	stateMap map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

func newSkillFakeSession(id string) *skillFakeSession {
	return &skillFakeSession{id: id, stateMap: map[string]any{}}
}

func (f *skillFakeSession) GetSessionID() string { return f.id }

func (f *skillFakeSession) UpdateState(data map[string]any) {
	for k, v := range data {
		f.stateMap[k] = v
	}
}

func (f *skillFakeSession) GetState(key state.StateKey) (any, error) {
	return f.stateMap[key.String()], nil
}

func (f *skillFakeSession) DumpState() map[string]any { return f.stateMap }

func (f *skillFakeSession) WriteStream(context.Context, any) error       { return nil }
func (f *skillFakeSession) WriteCustomStream(context.Context, any) error { return nil }
func (f *skillFakeSession) GetEnv(string, ...any) any                    { return nil }
func (f *skillFakeSession) Interact(context.Context, any) error          { return nil }
func (f *skillFakeSession) ClearSession(context.Context)                 {}

// ──────────────────────────── 非导出函数 ────────────────────────────

func newRailWithSkills(skills ...*skillpkg.Skill) *SkillUseRail {
	r := NewSkillUseRail([]string{"/tmp/skills"})
	r.skills = skills
	return r
}

func testSkill(name string) *skillpkg.Skill {
	return &skillpkg.Skill{Name: name, Description: name + " desc", Directory: "/tmp/skills/" + name}
}

func TestEnsureSessionBaseline_首次落盘且二次不覆盖(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	sess := newSkillFakeSession("s1")

	require.NoError(t, r.ensureSessionBaseline(sess))

	raw, err := sess.GetState(state.StringKey(skillSessionStateKey))
	require.NoError(t, err)
	require.NotNil(t, raw, "首次调用必须落盘基线")

	// 第二次：rail 侧技能集合变化，基线保持不变（快照语义）
	r.skills = []*skillpkg.Skill{testSkill("beta")}
	require.NoError(t, r.ensureSessionBaseline(sess))

	baseline := r.loadSessionBaseline(sess)
	require.Len(t, baseline, 1)
	assert.Equal(t, "alpha", baseline[0].Name)
}

func TestClearSessionBaseline_nil会话不panic(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	assert.NoError(t, r.ClearSessionBaseline(nil))
}

func TestClearSessionBaseline_清除后视图回落到当前集合(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	sess := newSkillFakeSession("s1")
	require.NoError(t, r.ensureSessionBaseline(sess))

	require.NoError(t, r.ClearSessionBaseline(sess))
	assert.Equal(t, []*skillpkg.Skill{r.skills[0]}, r.GetSkillsForSession(sess))
}

func TestGetSkillsForSession_基线并集新发现(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	sess := newSkillFakeSession("s1")
	require.NoError(t, r.ensureSessionBaseline(sess))

	// 会话中途新增 beta（应作为 runtime addition 可见）
	r.skills = []*skillpkg.Skill{testSkill("alpha"), testSkill("beta")}
	names := []string{}
	for _, s := range r.GetSkillsForSession(sess) {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"alpha", "beta"}, names)
}

func TestLoadSessionState_schema版本不符则忽略(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	sess := newSkillFakeSession("s1")
	sess.stateMap[skillSessionStateKey] = map[string]any{
		"schema_version":  999,
		"baseline_skills": []any{map[string]any{"name": "alpha", "directory": "/tmp/skills/alpha"}},
	}

	_, ok := r.loadSessionState(sess)
	assert.False(t, ok, "版本不符必须整份忽略")
	assert.Empty(t, r.loadSessionBaseline(sess))
}

func TestLoadSessionBaseline_非法条目跳过(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	sess := newSkillFakeSession("s1")
	sess.stateMap[skillSessionStateKey] = map[string]any{
		"schema_version": 1,
		"baseline_skills": []any{
			map[string]any{"name": "alpha", "description": "d", "directory": "/tmp/skills/alpha"},
			map[string]any{"name": "", "directory": "/tmp/skills/x"},   // 缺 name
			map[string]any{"name": "gamma"},                            // 缺 directory
			"not-a-map",                                               // 类型非法
		},
	}

	baseline := r.loadSessionBaseline(sess)
	require.Len(t, baseline, 1)
	assert.Equal(t, "alpha", baseline[0].Name)
}

// 编译期断言：假会话满足门面
var _ sessioninterfaces.SessionFacade = (*skillFakeSession)(nil)
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/harness/rails/skills/ -run 'TestEnsureSessionBaseline|TestClearSessionBaseline|TestGetSkillsForSession|TestLoadSession' -v
```

Expected: 编译失败 `undefined: skillSessionStateKey`。

- [ ] **Step 3: 实现常量、结构与五个方法**

在 `skill_use_rail.go` 的常量区新增：

```go
const (
	// skillSessionStateKey 会话技能基线的 session state 键
	// Python: SkillUseRail._SESSION_STATE_KEY (skill_use_rail.py:65)
	skillSessionStateKey = "skill_use"
	// skillSessionStateSchemaVersion 基线结构版本
	// Python: SkillUseRail._SESSION_STATE_SCHEMA_VERSION (:66)
	skillSessionStateSchemaVersion = 1
	// runtimeSkillAttachmentSection 运行时技能变更附件节名
	// Python: SkillUseRail._RUNTIME_ATTACHMENT_SECTION (:67)
	runtimeSkillAttachmentSection = "skills.runtime_changes"
	// runtimeSkillAttachmentSource 附件来源标识
	runtimeSkillAttachmentSource = "skill_use_rail"
)
```

在结构体区新增两个类型（放在 `SkillUseRail` 之后、`SkillUseRailOption` 之前）：

```go
// skillBaselineState 会话技能基线持久化结构
// Python: _save_session_baseline 写入的 dict (:733-750)
type skillBaselineState struct {
	// SchemaVersion 结构版本，不匹配则整份忽略
	SchemaVersion int `json:"schema_version"`
	// BaselineSkills 首次快照时的技能列表
	BaselineSkills []skillBaselineEntry `json:"baseline_skills"`
}

// skillBaselineEntry 单条基线技能
type skillBaselineEntry struct {
	// Name 技能名
	Name string `json:"name"`
	// Description 技能描述
	Description string `json:"description"`
	// Directory 技能目录绝对路径（skill_tool 读文件必需）
	Directory string `json:"directory"`
}
```

在非导出函数区新增（位置紧邻 `refreshSkillPrompt` 之后）：

```go
// ensureSessionBaseline 首次使用时落盘会话基线。
// Python: _ensure_session_baseline (:692-698)
func (r *SkillUseRail) ensureSessionBaseline(sess sessioninterfaces.SessionFacade) error {
	if sess == nil {
		return nil
	}
	if _, ok := r.loadSessionState(sess); ok {
		return nil
	}
	return r.saveSessionBaseline(sess, r.skills)
}

// loadSessionBaseline 读取会话基线；无有效 state 时返回空切片。
// Python: _load_session_baseline (:700-721)
func (r *SkillUseRail) loadSessionBaseline(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill {
	stateMap, ok := r.loadSessionState(sess)
	if !ok {
		return nil
	}
	raw, exists := stateMap["baseline_skills"]
	if !exists {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		logger.Warn(logger.ComponentAgentCore).Msg("invalid persisted baseline_skills, ignoring")
		return nil
	}
	skills := make([]*skillpkg.Skill, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringField(entry, "name")
		directory := stringField(entry, "directory")
		if name == "" || directory == "" {
			continue
		}
		skills = append(skills, &skillpkg.Skill{
			Name:        name,
			Description: stringField(entry, "description"),
			Directory:   directory,
		})
	}
	return skills
}

// loadSessionState 读取并校验 skill_use state。
// Python: _load_session_state (:722-731)
func (r *SkillUseRail) loadSessionState(sess sessioninterfaces.SessionFacade) (map[string]any, bool) {
	if sess == nil {
		return nil, false
	}
	raw, err := sess.GetState(state.StringKey(skillSessionStateKey))
	if err != nil || raw == nil {
		return nil, false
	}
	stateMap, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	if schemaVersion(stateMap["schema_version"]) != skillSessionStateSchemaVersion {
		logger.Warn(logger.ComponentAgentCore).
			Any("schema_version", stateMap["schema_version"]).
			Msg("unsupported persisted skill state schema, ignoring")
		return nil, false
	}
	return stateMap, true
}

// saveSessionBaseline 把技能列表写入会话基线。
// Python: _save_session_baseline (:733-750)
func (r *SkillUseRail) saveSessionBaseline(sess sessioninterfaces.SessionFacade, skills []*skillpkg.Skill) error {
	if sess == nil {
		return nil
	}
	entries := make([]any, 0, len(skills))
	for _, skill := range skills {
		if skill == nil {
			continue
		}
		entries = append(entries, map[string]any{
			"name":        skill.Name,
			"description": skill.Description,
			"directory":   skill.Directory,
		})
	}
	sess.UpdateState(map[string]any{
		skillSessionStateKey: map[string]any{
			"schema_version":  skillSessionStateSchemaVersion,
			"baseline_skills": entries,
		},
	})
	return nil
}

// GetSkillsForSession 返回会话可见技能视图：基线 ∪ 当前新发现（按 name 去重）。
// Python: get_skills_for_session (:673-687)
func (r *SkillUseRail) GetSkillsForSession(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill {
	if _, ok := r.loadSessionState(sess); !ok {
		out := make([]*skillpkg.Skill, len(r.skills))
		copy(out, r.skills)
		return out
	}
	baseline := r.loadSessionBaseline(sess)
	known := make(map[string]struct{}, len(baseline))
	merged := make([]*skillpkg.Skill, 0, len(baseline)+len(r.skills))
	for _, skill := range baseline {
		known[skill.Name] = struct{}{}
		merged = append(merged, skill)
	}
	for _, skill := range r.skills {
		if skill == nil {
			continue
		}
		if _, ok := known[skill.Name]; ok {
			continue
		}
		merged = append(merged, skill)
	}
	return merged
}

// ClearSessionBaseline 清除会话基线，下次同步时按当前集合重拍。
// Python: JiuWenSwarmDeepAdapter._clear_skill_session_baseline (interface_deep.py:11261-11274)
func (r *SkillUseRail) ClearSessionBaseline(sess sessioninterfaces.SessionFacade) error {
	if sess == nil {
		return nil
	}
	sess.UpdateState(map[string]any{skillSessionStateKey: nil})
	return nil
}
```

在同文件非导出函数区（`normalizeNameList` 附近）新增两个小助手：

```go
// stringField 读取 map 的字符串字段，缺失或非字符串返回空串。
func stringField(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// schemaVersion 归一化版本号：落盘回读可能是 int 或 float64。
func schemaVersion(v any) int {
	switch typed := v.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	}
	return -1
}
```

新增导入（与现有 import 合并，按字母序）：

```go
sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
```

- [ ] **Step 4: 运行确认通过 + 覆盖率**

```bash
go test ./internal/agentcore/harness/rails/skills/ -run 'TestEnsureSessionBaseline|TestClearSessionBaseline|TestGetSkillsForSession|TestLoadSession' -v -cover
```

Expected: 6 个用例 PASS，改动行全覆盖。

- [ ] **Step 5: 提交**

```bash
gofmt -l internal/agentcore/harness/rails/skills/
git add internal/agentcore/harness/rails/skills/skill_use_rail.go internal/agentcore/harness/rails/skills/skill_use_rail_baseline_test.go
git commit -m "feat(skill): persist per-session skill baseline in SkillUseRail"
```

---

### Task 2: 提示词节改走基线 + 同步入口

**Files:**
- Modify: `internal/agentcore/harness/rails/skills/skill_use_rail.go:367-401`（BeforeInvoke / BeforeModelCall）、`:763-786`（buildSkillsSection）
- Test: `internal/agentcore/harness/rails/skills/skill_use_rail_baseline_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `skill_use_rail_baseline_test.go`：

```go
func TestBeforeInit_SyncSkillPromptUsesBaseline(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	r.skillsDir = []string{t.TempDir()} // 空目录 → 签名恒为空，跳过增量刷新
	r.skillMode = SkillModeAll
	r.systemPromptBuilder = newMockSystemPromptBuilder("cn")

	sess := newSkillFakeSession("s1")
	require.NoError(t, r.ensureSessionBaseline(sess))

	// 基线落盘后新增 beta：提示词节仍只应包含 alpha
	r.skills = []*skillpkg.Skill{testSkill("alpha"), testSkill("beta")}
	require.NoError(t, r.syncSkillPromptAndAttachment(context.Background(), newCBC(t, sess)))

	section := r.systemPromptBuilder.GetSection(sections.SectionSkills)
	require.NotNil(t, section)
	assert.Contains(t, section.Content["cn"], "alpha")
	assert.NotContains(t, section.Content["cn"], "beta", "系统提示词必须取自基线")
}
```

同文件新增辅助（放在 `newRailWithSkills` 之后）：

```go
// newCBC 构造带会话的回调上下文。
func newCBC(t *testing.T, sess sessioninterfaces.SessionFacade) *agentinterfaces.AgentCallbackContext {
	t.Helper()
	return agentinterfaces.NewAgentCallbackContext(nil, nil, sess)
}
```

> `NewAgentCallbackContext(agent, inputs, sess)` 签名见 `single_agent/interfaces/callback.go:393`；`BeforeModelCall` 内 `cbc.Inputs()` 为 nil 时类型断言自然失败，`currentQuery` 保持空串，不影响该用例。

补充导入：`"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"`、`agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"`。

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/harness/rails/skills/ -run TestBeforeInit_SyncSkillPromptUsesBaseline -v
```

Expected: `undefined: (*SkillUseRail).syncSkillPromptAndAttachment`。

- [ ] **Step 3: 实现同步入口并改造两个回调**

替换 `BeforeInvoke` 与 `BeforeModelCall`：

```go
// BeforeInvoke 刷新技能并落盘基线。
// Python: SkillUseRail.before_invoke (:437-442)
func (r *SkillUseRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.refreshSkillPrompt(ctx)
	var sess sessioninterfaces.SessionFacade
	if cbc != nil {
		sess = cbc.Session()
	}
	if err := r.ensureSessionBaseline(sess); err != nil {
		return err
	}
	return r.syncSkillPromptAndAttachment(ctx, cbc)
}

// BeforeModelCall 用会话基线渲染稳定 skills 节并刷新变更附件。
// Python: SkillUseRail.before_model_call (:475-485)
func (r *SkillUseRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 记录当前查询：技能预算按与查询的关键词重叠度排序。
	// Python: _extract_query_from_messages (:657-671)
	if cbc != nil {
		if inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs); ok && inputs != nil {
			r.currentQuery = extractQueryFromMessages(inputs.Messages)
		}
	}
	if r.systemPromptBuilder == nil {
		return nil
	}
	return r.syncSkillPromptAndAttachment(context.Background(), cbc)
}
```

新增（紧跟 `refreshSkillPrompt` 之后）：

```go
// syncSkillPromptAndAttachment 刷新稳定 skills 节与变更附件。
// Python: _sync_skill_prompt_and_attachment (:486-508)
func (r *SkillUseRail) syncSkillPromptAndAttachment(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}
	var sess sessioninterfaces.SessionFacade
	if cbc != nil {
		sess = cbc.Session()
	}
	if err := r.refreshSkillPromptIfChanged(ctx); err != nil {
		return err
	}
	// 进化记录可能在不改 SKILL.md 的情况下变化，需与技能快照解耦刷新。
	// 部分 task_loop 路径不经 BeforeInvoke，故此处也要保证基线存在。
	if err := r.ensureSessionBaseline(sess); err != nil {
		return err
	}
	var baseline []*skillpkg.Skill
	if _, ok := r.loadSessionState(sess); ok {
		baseline = r.loadSessionBaseline(sess)
	} else {
		baseline = r.skills
	}
	r.fetchEvolutionTextsFor(append(append([]*skillpkg.Skill{}, baseline...), r.skills...))

	section := r.buildSkillsSectionFor(baseline)
	if section == nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionSkills)
	} else {
		r.systemPromptBuilder.AddSection(*section)
	}
	return r.updateRuntimeSkillAttachment(cbc, baseline)
}

// refreshSkillPromptIfChanged 目录或 mtime 签名变化时才重建技能集合。
// Python: _refresh_skill_prompt_if_changed (:510-516)
func (r *SkillUseRail) refreshSkillPromptIfChanged(ctx context.Context) error {
	if r.skillsSignatureEquals() {
		return nil
	}
	r.refreshSkillPrompt(ctx)
	return nil
}

// skillsSignatureEquals 比较当前磁盘签名与上次快照。
// Python: _build_skills_snapshot_signature (:518-524)
func (r *SkillUseRail) skillsSignatureEquals() bool {
	sig := r.buildSkillsSnapshotSignature()
	if len(sig) != len(r.lastSignature) {
		return false
	}
	for i := range sig {
		if sig[i] != r.lastSignature[i] {
			return false
		}
	}
	return true
}

// buildSkillsSnapshotSignature 生成 (绝对路径, mtime) 序列签名。
// Python: _build_skills_snapshot_signature (:518-524)
func (r *SkillUseRail) buildSkillsSnapshotSignature() []string {
	roots := r.normalizeSkillDirs()
	sig := make([]string, 0, len(roots))
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			sig = append(sig, filepath.Join(root, entry.Name())+"@"+info.ModTime().UTC().Format(time.RFC3339Nano))
		}
	}
	sort.Strings(sig)
	return sig
}
```

在 `refreshSkillPrompt` 末尾补一行记录签名：

```go
func (r *SkillUseRail) refreshSkillPrompt(ctx context.Context) {
	_ = r.prepareSkills(ctx)
	r.fetchEvolutionTexts(ctx)
	// Python: refresh_skill_prompt 末尾重算签名 (:430-435)
	r.lastSignature = r.buildSkillsSnapshotSignature()
}
```

结构体加字段（`currentQuery` 之后）：

```go
	// lastSignature 上次技能目录签名（增量刷新判断）
	// Python: self._skills_snapshot_signature
	lastSignature []string
```

`buildSkillsSection` 与 `buildAllModeSection` 改为带参版本 `buildSkillsSectionFor` / `buildAllModeSectionFor`（Python 侧是同一方法带可选 `skills` 参数，Go 用显式入参表达同一语义），旧无参版本删除，调用点全部更新：

```go
// buildSkillsSection 按给定技能集合构建 PromptSection。
// Python: _build_skills_section (:526-558)
func (r *SkillUseRail) buildSkillsSectionFor(skills []*skillpkg.Skill) *saprompt.PromptSection {
	if r.skillMode == SkillModeAll {
		return r.buildAllModeSectionFor(skills)
	}
	return sections.BuildSkillsSection("auto_list", "", r.language())
}

// buildAllModeSectionFor 用给定集合构建 all 模式 PromptSection。
func (r *SkillUseRail) buildAllModeSectionFor(skills []*skillpkg.Skill) *saprompt.PromptSection {
	budgeted := applySkillBudget(skills, r.currentQuery, r.maxSkills, r.maxTotalChars)
	var bodyLines []string
	for idx, skill := range budgeted {
		bodyLines = append(bodyLines, sections.BuildSkillLine(idx, skill.Name, r.getSkillDescription(skill), ""))
	}
	return sections.BuildSkillsSection("all", sections.BuildSkillLines(bodyLines), r.language())
}
```

删除旧 `buildSkillsSection()`（无参）与 `buildAllModeSection()`（无参），并替换全部旧调用点（`grep -n "buildSkillsSection()\|buildAllModeSection()" -r internal/agentcore/harness/rails/skills/`）。

`fetchEvolutionTexts` 增加"按给定列表"版本（Python `_fetch_evolution_texts(skills)` 可选参，:444-462）：

```go
// fetchEvolutionTextsFor 按给定技能列表抓取演化经验文本（同名去重）。
// Python: _fetch_evolution_texts (:444-462)
func (r *SkillUseRail) fetchEvolutionTextsFor(skills []*skillpkg.Skill) {
	if r.evolutionStore == nil {
		return
	}
	seen := map[string]struct{}{}
	for _, skill := range skills {
		if skill == nil {
			continue
		}
		if _, ok := seen[skill.Name]; ok {
			continue
		}
		seen[skill.Name] = struct{}{}
		text, err := r.evolutionStore.FormatDescExperienceText(skill.Name)
		if err != nil {
			logger.Warn(logger.ComponentAgentCore).Str("skill_name", skill.Name).Err(err).Msg("failed to fetch evolution text")
			continue
		}
		r.evolutionTexts[skill.Name] = text
	}
}
```

> `FormatDescExperienceText` 的 Go 签名以 `grep -n "func (s \*EvolutionStore) FormatDescExperienceText" internal/evolving/checkpointing/*.go` 为准，ctx 若必填则补 `context.Background()`；现有 `fetchEvolutionTexts` 已调用同一方法，照抄其调用形态。

- [ ] **Step 4: 运行确认通过**

```bash
go test ./internal/agentcore/harness/rails/skills/ -v -cover
```

Expected: 新旧用例全部 PASS，`skill_use_rail.go` 覆盖率 ≥ 85%。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/agentcore/harness/rails/skills/skill_use_rail.go
git add internal/agentcore/harness/rails/skills/
git commit -m "feat(skill): render skills section from session baseline"
```

---

### Task 3: 运行时技能变更附件

**Files:**
- Modify: `internal/agentcore/harness/rails/skills/skill_use_rail.go`（非导出函数区）
- Test: `internal/agentcore/harness/rails/skills/skill_use_rail_baseline_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestUpdateRuntimeSkillAttachment_新增技能写入附件(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	r.skillMode = SkillModeAll
	r.systemPromptBuilder = newMockSystemPromptBuilder("cn")

	sess := newSkillFakeSession("s1")
	require.NoError(t, r.ensureSessionBaseline(sess))
	r.skills = []*skillpkg.Skill{testSkill("alpha"), testSkill("beta")}

	mgr := prompts.NewManager()
	r.attachmentManagerFor = func() *prompts.Manager { return mgr }

	cbc := newCBC(t, sess)
	require.NoError(t, r.updateRuntimeSkillAttachment(cbc, r.loadSessionBaseline(sess)))

	att := mgr.Find("s1", runtimeSkillAttachmentSection, runtimeSkillAttachmentSource)
	require.NotNil(t, att)
	assert.Contains(t, att.Content, "beta")
	assert.NotContains(t, att.Content, "alpha", "基线内技能不进变更附件")
}

func TestBuildRuntimeSkillChangeContent_无变化返回空串(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	r.systemPromptBuilder = newMockSystemPromptBuilder("en")
	content := r.buildRuntimeSkillChangeContent(nil, nil, []*skillpkg.Skill{testSkill("alpha")})
	assert.Equal(t, "", content)
}

func TestBuildRuntimeSkillChangeContent_英文包含移除段(t *testing.T) {
	r := newRailWithSkills()
	r.systemPromptBuilder = newMockSystemPromptBuilder("en")
	content := r.buildRuntimeSkillChangeContent(
		[]*skillpkg.Skill{testSkill("beta")},
		[]*skillpkg.Skill{testSkill("alpha")},
		[]*skillpkg.Skill{testSkill("alpha")},
	)
	assert.Contains(t, content, "Skill environment status update")
	assert.Contains(t, content, "Newly available skills:")
	assert.Contains(t, content, "Unavailable skills (removed from the environment):")
	assert.Contains(t, content, "alpha")
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/harness/rails/skills/ -run 'TestUpdateRuntimeSkillAttachment|TestBuildRuntimeSkillChangeContent' -v
```

Expected: `undefined: (*SkillUseRail).updateRuntimeSkillAttachment`。

- [ ] **Step 3: 实现附件写入与文案**

结构体加字段（`systemPromptBuilder` 之后）与测试注入点：

```go
	// attachmentManagerFor 附件管理器获取函数（生产实现取 agent.PromptAttachments）
	// Python: self.attachment_manager（Init 中从 agent 取）
	attachmentManagerFor func() *hprompts.Manager
```

新增导入：

```go
hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
```

`Init` 内（`r.systemPromptBuilder = agent.SystemPromptBuilder()` 之后）注入：

```go
	// Python: self.attachment_manager = getattr(agent, "prompt_attachment_manager", None)
	if provider, ok := agent.(interface{ PromptAttachments() *hprompts.Manager }); ok {
		r.attachmentManagerFor = provider.PromptAttachments
	}
```

新增方法：

```go
// updateRuntimeSkillAttachment 把技能新增/移除/进化经验写入变更附件。
// Python: _update_runtime_skill_attachment (:752-781)
func (r *SkillUseRail) updateRuntimeSkillAttachment(cbc *agentinterfaces.AgentCallbackContext, baseline []*skillpkg.Skill) error {
	if r.attachmentManagerFor == nil || cbc == nil {
		return nil
	}
	mgr := r.attachmentManagerFor()
	if mgr == nil || cbc.Session() == nil {
		return nil
	}
	sessionID := cbc.Session().GetSessionID()
	if sessionID == "" {
		return nil
	}

	baselineByName := make(map[string]struct{}, len(baseline))
	for _, skill := range baseline {
		baselineByName[skill.Name] = struct{}{}
	}
	currentByName := make(map[string]struct{}, len(r.skills))
	var additions []*skillpkg.Skill
	for _, skill := range r.skills {
		currentByName[skill.Name] = struct{}{}
		if _, ok := baselineByName[skill.Name]; !ok {
			additions = append(additions, skill)
		}
	}
	var removals []*skillpkg.Skill
	for _, skill := range baseline {
		if _, ok := currentByName[skill.Name]; !ok {
			removals = append(removals, skill)
		}
	}

	content := r.buildRuntimeSkillChangeContent(additions, removals, baseline)
	if content == "" {
		if _, err := mgr.ClearSection(sessionID, runtimeSkillAttachmentSection); err != nil {
			return err
		}
		return nil
	}
	_, err := mgr.AddSection(sessionID, runtimeSkillAttachmentSection, content, hprompts.AttachmentSkill, runtimeSkillAttachmentSource)
	return err
}

// buildRuntimeSkillChangeContent 渲染技能变更 + 进化经验，全空返回空串。
// Python: _build_runtime_skill_change_content (:783-849)
func (r *SkillUseRail) buildRuntimeSkillChangeContent(additions, removals, baseline []*skillpkg.Skill) string {
	isEnglish := strings.HasPrefix(strings.ToLower(r.language()), "en")

	var evolutionSkills []*skillpkg.Skill
	seen := map[string]struct{}{}
	for _, skill := range append(append([]*skillpkg.Skill{}, baseline...), additions...) {
		if skill == nil {
			continue
		}
		if _, ok := seen[skill.Name]; ok {
			continue
		}
		if strings.TrimSpace(r.evolutionTexts[skill.Name]) == "" {
			continue
		}
		seen[skill.Name] = struct{}{}
		evolutionSkills = append(evolutionSkills, skill)
	}

	if len(additions) == 0 && len(removals) == 0 && len(evolutionSkills) == 0 {
		return ""
	}

	var lines []string
	if isEnglish {
		lines = append(lines, "Skill environment status update. Invoke relevant skills only when needed for the current task.")
		if len(additions) > 0 {
			lines = append(lines, "Newly available skills:")
			for idx, skill := range additions {
				lines = append(lines, sections.BuildSkillLine(idx, skill.Name, skill.Description, ""))
			}
		}
		if len(removals) > 0 {
			lines = append(lines, "Unavailable skills (removed from the environment):")
			for _, skill := range removals {
				lines = append(lines, "- "+skill.Name)
			}
		}
		if len(evolutionSkills) > 0 {
			lines = append(lines, "Skill evolution experience reference:")
			for _, skill := range evolutionSkills {
				lines = append(lines, "[Skill: "+skill.Name+"]")
				lines = append(lines, strings.TrimSpace(r.evolutionTexts[skill.Name]))
			}
		}
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "Skill 环境状态更新。请根据当前任务需要，按需调用相关 Skill。")
	if len(additions) > 0 {
		lines = append(lines, "新增可用 Skill：")
		for idx, skill := range additions {
			lines = append(lines, sections.BuildSkillLine(idx, skill.Name, skill.Description, ""))
		}
	}
	if len(removals) > 0 {
		lines = append(lines, "已移除、当前不可用的 Skill：")
		for _, skill := range removals {
			lines = append(lines, "- "+skill.Name)
		}
	}
	if len(evolutionSkills) > 0 {
		lines = append(lines, "Skill 演进经验参考：")
		for _, skill := range evolutionSkills {
			lines = append(lines, "[Skill: "+skill.Name+"]")
			lines = append(lines, strings.TrimSpace(r.evolutionTexts[skill.Name]))
		}
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: 运行确认通过**

```bash
go test ./internal/agentcore/harness/rails/skills/ -v -cover
```

Expected: 全部 PASS。若 `prompts.NewManager()` 名称不同，用 `grep -n "func NewManager" internal/agentcore/harness/prompts/*.go` 确认后替换。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/agentcore/harness/rails/skills/
git add internal/agentcore/harness/rails/skills/
git commit -m "feat(skill): emit skills.runtime_changes attachment on skill view changes"
```

---

### Task 4: skill_tool / list_skill 的 session 透传

**Files:**
- Modify: `internal/agentcore/harness/tools/skills/skill_tool.go:55-90`、`list_skill.go:29-70`、`internal/agentcore/harness/rails/skills/skill_use_rail.go:265-292`
- Test: `internal/agentcore/harness/tools/skills/skills_test.go`

- [ ] **Step 1: 写失败测试**

在 `skills_test.go` 追加：

```go
func TestSkillTool_按会话视图查找技能(t *testing.T) {
	all := []*skills.Skill{{Name: "alpha", Directory: t.TempDir()}}
	sessionOnly := []*skills.Skill{{Name: "beta", Directory: t.TempDir()}}

	op := &fakeSysOperation{fsOp: &fakeFsOperation{
		readFileResult: &result.ReadFileResult{
			BaseResult: result.BaseResult{Code: 0, Message: "success"},
			Data:       &result.ReadFileData{Content: "# beta", Mode: "text"},
		},
	}}
	skillTool := NewSkillTool(op, func(sess sessioninterfaces.SessionFacade) []*skills.Skill {
		if sess != nil {
			return sessionOnly
		}
		return all
	}, "cn", "agent", true)

	sess := &fakeSkillSession{id: "s1"}
	result, err := skillTool.Invoke(context.Background(), map[string]any{"skill_name": "beta"}, toolfoundation.WithToolSession(sess))
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
}
```

同时在 `skills_test.go` 内加最小假会话（9 方法，同 Task 1 的 `skillFakeSession`，改名为 `fakeSkillSession`）与 `sessioninterfaces` 导入；`toolfoundation` 为 `internal/agentcore/foundation/tool` 别名，按文件现有别名统一。

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/harness/tools/skills/ -run TestSkillTool_按会话视图查找技能 -v
```

Expected: 编译失败（`NewSkillTool` 期望 `func() []*Skill`）。

- [ ] **Step 3: 改签名并接 session**

`skill_tool.go`：

```go
// skillGetter 返回会话可见技能视图。
// Python: get_skills=lambda session=None: rail.get_skills_for_session(session)
type skillGetter func(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill

// SkillTool 查看特定技能内容。
// Python: SkillTool (harness/tools/skills/skill_tool.py)
type SkillTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// operation 系统操作
	operation sys_operation.SysOperation
	// getSkills 技能视图获取函数
	getSkills skillGetter
	// language 语言
	language string
	// enableReadImageMultimodal 读图多模态是否可用（决定媒体提示档位）
	enableReadImageMultimodal bool
}

// NewSkillTool 创建 SkillTool 实例。
// Python: SkillTool.__init__ (skill_tool.py:337-361)
func NewSkillTool(
	operation sys_operation.SysOperation,
	getSkills skillGetter,
	language string,
	agentID string,
	enableReadImageMultimodal bool,
) *SkillTool {
	card, _ := ptools.BuildToolCard("skill_tool", "SkillTool", language, nil, agentID)
	return &SkillTool{
		card:                      card,
		operation:                 operation,
		getSkills:                 getSkills,
		language:                  language,
		enableReadImageMultimodal: enableReadImageMultimodal,
	}
}
```

`Invoke` 内取会话并传给 getter：

```go
	opts := tool.NewToolCallOptions(options...)
	var sess sessioninterfaces.SessionFacade
	if opts != nil {
		sess = opts.Session
	}
	skill := t.getSkillByName(skillName, sess)
```

```go
// getSkillByName 按名称在会话视图中查找技能。
// Python: SkillTool._get_skill_by_name (skill_tool.py:456-465)
func (t *SkillTool) getSkillByName(name string, sess sessioninterfaces.SessionFacade) *skillpkg.Skill {
	if name == "" || t.getSkills == nil {
		return nil
	}
	for _, s := range t.getSkills(sess) {
		if s != nil && s.Name == name {
			return s
		}
	}
	return nil
}
```

> `Invoke` 的形参名以现文件为准（`opts ...tool.ToolOption`）；`NewToolCallOptions` 返回 `*ToolCallOptions`（`foundation/tool/base.go:140`）。

`list_skill.go` 同改：字段 `getSkills skillGetter`、构造参 `getSkills skillGetter`、`Invoke` 取 `opts.Session` 后传给 `dumpAllSkills(sess)` 与 `routeSkills(ctx, query)`/`selectSkillsByNames`；所有内部方法增加 `sess sessioninterfaces.SessionFacade` 形参并透传。

`skill_use_rail.go` 构造处：

```go
	tools = append(tools, skilltools.NewSkillTool(
		op,
		func(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill { return r.GetSkillsForSession(sess) },
		language,
		agentID,
		enableImageMultimodal,
	))
```

> `enableImageMultimodal` 需从 `if r.includeTools` 块内提取到块外变量（两个工具都要用），并在 `includeTools == false` 时取 `false` 以外的语义：Python 由 `build_read_image_multimodal_resolver(agent)` 求值，Go 用 `r.enableImageMultimodal` 指针的默认值 `true`。

`list_skill` 构造处同样改为闭包 `func(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill { return r.GetSkillsForSession(sess) }`。

- [ ] **Step 4: 更新全部旧调用点并跑包测试**

```bash
grep -rn "NewSkillTool(\|NewListSkillTool(" internal/ | grep -v "_test.go:.*skillGetter"
go test ./internal/agentcore/harness/tools/skills/ ./internal/agentcore/harness/rails/skills/ -v -cover
```

Expected: 全部 PASS；`skills_test.go` 中 6 处旧调用已改为 `func(sess sessioninterfaces.SessionFacade) []*skills.Skill { return testSkills }`。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/agentcore/harness/tools/skills/ internal/agentcore/harness/rails/skills/
git add internal/agentcore/harness/tools/skills/ internal/agentcore/harness/rails/skills/
git commit -m "feat(skill): pass session through skill_tool and list_skill getters"
```

---

### Task 5: 媒体检测模块（markdown_media.go）

**Files:**
- Create: `internal/agentcore/harness/tools/skills/markdown_media.go`
- Test: `internal/agentcore/harness/tools/skills/markdown_media_test.go`
- Modify: `internal/agentcore/harness/tools/skills/doc.go`

- [ ] **Step 1: 写失败测试**

新建 `markdown_media_test.go`：

```go
package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarkdownHasImageReference(t *testing.T) {
	cases := map[string]bool{
		"![shot](./assets/a.PNG)":          true,
		`<img src="b.webp">`:               true,
		"see reference.png for details":    true,
		"![doc](./guide.md)":               false,
		"[link](https://x.com/a.mp4)":      false,
		"plain text without media":         false,
	}
	for input, want := range cases {
		assert.Equal(t, want, markdownHasImageReference(input), input)
	}
}

func TestMarkdownHasVideoReference(t *testing.T) {
	assert.True(t, markdownHasVideoReference("[demo](./assets/demo.mp4)"))
	assert.True(t, markdownHasVideoReference(`<video src="x.mov"></video>`))
	assert.False(t, markdownHasVideoReference("![shot](a.png)"))
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/harness/tools/skills/ -run 'TestMarkdownHas' -v
```

Expected: `undefined: markdownHasImageReference`。

- [ ] **Step 3: 实现（对齐 markdown_media.py:1-77）**

```go
package skills

import (
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// mediaImageExtensions 图片扩展名
	mediaImageExtensions = map[string]struct{}{".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}}
	// mediaVideoExtensions 视频扩展名
	mediaVideoExtensions = map[string]struct{}{".mp4": {}, ".mov": {}, ".webm": {}, ".mkv": {}}
	// markdownImagePattern markdown 图片语法
	markdownImagePattern = regexp.MustCompile(`!\[([^\]]*)\]\((.*?)\)`)
	// markdownLinkPattern markdown 链接语法（排除图片）
	markdownLinkPattern = regexp.MustCompile(`(?m)(!?)\[([^\]]*)\]\((.*?)\)`)
	// htmlMediaSrcPattern HTML img/video 的 src
	htmlMediaSrcPattern = regexp.MustCompile(`(?i)<(?:video|img)\b[^>]*?\bsrc=(['"])(.*?)\1`)
	// bareMediaPathPattern 裸媒体路径
	bareMediaPathPattern = regexp.MustCompile(`(?i)(?:^|[\s（(])([\w./-]+\.(?:png|jpe?g|gif|webp|mp4|mov|webm|mkv))(?:$|[\s）)])`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// markdownHasImageReference 判断文本是否含图片引用。
// Python: markdown_has_image_reference (markdown_media.py:70-73)
func markdownHasImageReference(text string) bool { return hasMediaReference(text, mediaImageExtensions) }

// markdownHasVideoReference 判断文本是否含视频引用。
// Python: markdown_has_video_reference (markdown_media.py:75-77)
func markdownHasVideoReference(text string) bool { return hasMediaReference(text, mediaVideoExtensions) }

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasMediaReference 按扩展名集合判定是否存在媒体引用。
func hasMediaReference(text string, extensions map[string]struct{}) bool {
	for _, candidate := range iterMediaReferenceURLs(text) {
		if _, ok := extensions[mediaURLSuffix(candidate)]; ok {
			return true
		}
	}
	return false
}

// iterMediaReferenceURLs 依次产出 markdown 图片/链接、HTML src、裸路径候选。
// Python: _iter_media_reference_urls (markdown_media.py:59-66)
func iterMediaReferenceURLs(text string) []string {
	var urls []string
	for _, m := range markdownImagePattern.FindAllStringSubmatch(text, -1) {
		urls = append(urls, strings.TrimSpace(m[2]))
	}
	for _, m := range markdownLinkPattern.FindAllStringSubmatch(text, -1) {
		if m[1] == "!" {
			continue
		}
		urls = append(urls, strings.TrimSpace(m[3]))
	}
	for _, m := range htmlMediaSrcPattern.FindAllStringSubmatch(text, -1) {
		urls = append(urls, strings.TrimSpace(m[2]))
	}
	for _, m := range bareMediaPathPattern.FindAllStringSubmatch(text, -1) {
		urls = append(urls, strings.TrimSpace(m[1]))
	}
	return urls
}

// mediaURLSuffix 取 URL 路径部分的扩展名（忽略 query/fragment，解码常见转义）。
// Python: _extension_for_markdown_url (markdown_media.py:31-36)
func mediaURLSuffix(rawURL string) string {
	decoded := strings.TrimSpace(rawURL)
	decoded = strings.ReplaceAll(decoded, "%20", " ")
	for _, sep := range []string{"?", "#"} {
		if idx := strings.Index(decoded, sep); idx >= 0 {
			decoded = decoded[:idx]
		}
	}
	decoded = strings.ReplaceAll(decoded, "\\", "/")
	return strings.ToLower(path.Ext(decoded))
}

// 保留 utf8 引用，避免 goimports 误删（扩展名可能含非 ASCII 路径）
var _ = utf8.RuneSelf
```

> 若 `utf8` 无实际用途，删除该导入与末尾 `var _`（规则 8：不留消警代码）。实现时优先删除。

`doc.go` 文件树加一行（放在 `list_skill.go` 之后）：

```
//	├── markdown_media.go   # markdown/HTML 媒体引用检测
```

- [ ] **Step 4: 运行确认通过**

```bash
go test ./internal/agentcore/harness/tools/skills/ -run 'TestMarkdownHas' -v
```

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/agentcore/harness/tools/skills/
git add internal/agentcore/harness/tools/skills/
git commit -m "feat(skill): add markdown media reference detection"
```

---

### Task 6: skill_tool 路径安全 + 目录树 + content 渲染

**Files:**
- Create: `internal/agentcore/harness/tools/skills/skill_layout.go`、`skill_layout_test.go`
- Modify: `internal/agentcore/harness/tools/skills/skill_tool.go:82-153`、`prompts/tools/skill_tool.go:14-18`（描述文案）、`doc.go`

- [ ] **Step 1: 写失败测试**

`skill_layout_test.go`：

```go
package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 目录构造：skill/
//   SKILL.md
//   designer/SKILL.md
//   assets/logo.png（应被跳过）
//   output/x.txt（应被跳过）

func buildSkillFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# demo"), 0o644))
	for _, dir := range []string{"designer", "assets", "output"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir, "sub"), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "designer", "SKILL.md"), []byte("# sub"), 0o644))
	return root
}

func TestBuildSkillDirectoryAsciiTree_跳过目录且含根名(t *testing.T) {
	root := buildSkillFixture(t)
	tree, truncated, errText := buildSkillDirectoryAsciiTree(root)
	require.Len(t, tree, 1)
	assert.False(t, truncated)
	assert.Equal(t, "", errText)
	assert.Contains(t, tree[0], filepath.Base(root)+"/")
	assert.Contains(t, tree[0], "designer/")
	assert.NotContains(t, tree[0], "assets/")
	assert.NotContains(t, tree[0], "output/")
}

func TestCollectNestedSkillNames_仅含SKILLMD的子目录(t *testing.T) {
	root := buildSkillFixture(t)
	names, truncated := collectNestedSkillNames(root)
	assert.False(t, truncated)
	assert.Equal(t, []string{"designer"}, names)
}

func TestIsSafeRelativeFilePath(t *testing.T) {
	safe := []string{"SKILL.md", "designer/SKILL.md", "a/b/c.md"}
	unsafe := []string{"/etc/passwd", `C:\Windows\win.ini`, "../outside.md", "a/../../b.md", `a\..\b.md`}
	for _, p := range safe {
		assert.True(t, isSafeRelativeFilePath(p), p)
	}
	for _, p := range unsafe {
		assert.False(t, isSafeRelativeFilePath(p), p)
	}
}

func TestFormatLayoutAppendixForModel_含父技能名示例(t *testing.T) {
	layout := map[string]any{
		"directory_tree":                 []string{"demo/\n└── designer/"},
		"discovered_skill_names":         []string{"designer"},
		"discovered_skill_names_truncated": false,
	}
	text := formatLayoutAppendixForModel(layout, "demo")
	assert.Contains(t, text, "## Directory layout")
	assert.Contains(t, text, "## Nested skills")
	assert.Contains(t, text, `skill_tool(skill_name="demo", relative_file_path="designer/SKILL.md")`)
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/harness/tools/skills/ -run 'TestBuildSkillDirectoryAsciiTree|TestCollectNestedSkillNames|TestIsSafeRelativeFilePath|TestFormatLayoutAppendix' -v
```

Expected: `undefined: buildSkillDirectoryAsciiTree`。

- [ ] **Step 3: 实现 skill_layout.go（对齐 skill_tool.py:94-332）**

```go
package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// skillTreeMaxDepth 目录树最大深度
	// Python: _TREE_MAX_DEPTH (skill_tool.py:21)
	skillTreeMaxDepth = 4
	// skillTreeMaxLines 目录树最大行数
	// Python: _TREE_MAX_LINES (skill_tool.py:22)
	skillTreeMaxLines = 200
	// skillNestedNamesMax 嵌套子技能名上限
	// Python: _MAX_NESTED_SKILL_NAMES (skill_tool.py:23)
	skillNestedNamesMax = 50
)

// ──────────────────────────── 全局变量 ────────────────────────────

// skillTreeSkipDirs 目录树跳过的目录名
// Python: _TREE_SKIP_DIR_NAMES (skill_tool.py:24-29)
var skillTreeSkipDirs = map[string]struct{}{
	"output": {}, "temp": {}, "assets": {}, "node_modules": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// isSafeRelativeFilePath 判断技能内相对路径是否安全（拒绝绝对路径与 .. 穿越）。
// Python: _is_safe_relative_file_path (skill_tool.py:94-104)
func isSafeRelativeFilePath(filePath string) bool {
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return false
	}
	// Windows 盘符：C:/ 或 C:
	if len(normalized) >= 2 && normalized[1] == ':' {
		return false
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// buildSkillDirectoryAsciiTree 构建技能根目录 ASCII 树。
// Python: _build_skill_directory_ascii_tree (skill_tool.py:164-196)
func buildSkillDirectoryAsciiTree(skillRoot string) (tree []string, truncated bool, errText string) {
	info, err := os.Stat(skillRoot)
	if err != nil {
		return nil, false, "skill directory does not exist"
	}
	if !info.IsDir() {
		return nil, false, "skill path is not a directory"
	}
	rootName := filepath.Base(skillRoot)
	if rootName == "" || rootName == "." {
		rootName = "."
	}
	lines := []string{rootName + "/"}
	truncated = appendSkillTreeLines(skillRoot, "", 0, &lines, map[string]struct{}{})
	return []string{strings.Join(lines, "\n")}, truncated, ""
}

// appendSkillTreeLines 递归追加树行，返回是否因行数上限截断。
// Python: _append_skill_ascii_tree_lines (skill_tool.py:114-161)
func appendSkillTreeLines(dir, prefix string, depth int, out *[]string, visited map[string]struct{}) bool {
	if depth > skillTreeMaxDepth || len(*out) >= skillTreeMaxLines {
		return len(*out) >= skillTreeMaxLines
	}
	key := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		key = resolved
	}
	if _, ok := visited[key]; ok {
		return false
	}
	visited[key] = struct{}{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	type child struct {
		path  string
		name  string
		isDir bool
	}
	children := make([]child, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		isDir := entry.IsDir()
		if skipSkillTreeName(name, isDir) {
			continue
		}
		children = append(children, child{path: filepath.Join(dir, name), name: name, isDir: isDir})
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].isDir != children[j].isDir {
			return children[i].isDir
		}
		return strings.ToLower(children[i].name) < strings.ToLower(children[j].name)
	})

	for idx, c := range children {
		if len(*out) >= skillTreeMaxLines {
			return true
		}
		isLast := idx == len(children)-1
		connector, childPrefix := "├── ", "│   "
		if isLast {
			connector, childPrefix = "└── ", "    "
		}
		display := c.name
		if c.isDir {
			display += "/"
		}
		*out = append(*out, prefix+connector+display)
		if c.isDir && appendSkillTreeLines(c.path, prefix+childPrefix, depth+1, out, visited) {
			return true
		}
	}
	return false
}

// skipSkillTreeName 判断是否跳过该条目（隐藏文件/指定目录）。
// Python: _skill_tree_skip_name (skill_tool.py:106-112)
func skipSkillTreeName(name string, isDir bool) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if isDir {
		if _, ok := skillTreeSkipDirs[name]; ok {
			return true
		}
	}
	return false
}

// collectNestedSkillNames 收集含 SKILL.md 的子目录相对路径（POSIX 分隔）。
// Python: _collect_nested_skill_names (skill_tool.py:198-264)
func collectNestedSkillNames(skillRoot string) (names []string, truncated bool) {
	info, err := os.Stat(skillRoot)
	if err != nil || !info.IsDir() {
		return nil, false
	}
	resolvedRoot := skillRoot
	if resolved, err := filepath.EvalSymlinks(skillRoot); err == nil {
		resolvedRoot = resolved
	}
	visited := map[string]struct{}{}
	var walk func(dir string)
	walk = func(dir string) {
		if truncated {
			return
		}
		if len(names) >= skillNestedNamesMax {
			truncated = true
			return
		}
		key := dir
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			key = resolved
		}
		if _, ok := visited[key]; ok {
			return
		}
		visited[key] = struct{}{}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		sort.Slice(entries, func(i, j int) bool {
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})
		for _, entry := range entries {
			if len(names) >= skillNestedNamesMax {
				truncated = true
				return
			}
			if !entry.IsDir() || skipSkillTreeName(entry.Name(), true) {
				continue
			}
			childPath := filepath.Join(dir, entry.Name())
			rel, err := filepath.Rel(resolvedRoot, childPath)
			if err != nil || rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
				continue
			}
			if _, err := os.Stat(filepath.Join(childPath, defaultSkillFileName)); err == nil {
				names = append(names, strings.ReplaceAll(rel, "\\", "/"))
				if len(names) >= skillNestedNamesMax {
					truncated = true
					return
				}
			}
			walk(childPath)
		}
	}
	walk(resolvedRoot)
	return names, truncated
}

// skillLayoutMetadata 组装 skill_tool 成功时的目录布局字段。
// Python: _skill_layout_metadata (skill_tool.py:267-281)
func skillLayoutMetadata(skillDirectory string) map[string]any {
	meta := map[string]any{}
	if tree, truncated, errText := buildSkillDirectoryAsciiTree(skillDirectory); len(tree) > 0 {
		meta["directory_tree"] = tree
		meta["directory_tree_truncated"] = truncated
		if errText != "" {
			meta["directory_tree_partial_errors"] = errText
		}
	}
	names, nestedTruncated := collectNestedSkillNames(skillDirectory)
	meta["discovered_skill_names"] = names
	meta["discovered_skill_names_truncated"] = nestedTruncated
	return meta
}

// formatLayoutAppendixForModel 渲染目录布局附录（供 data.content 使用）。
// Python: _format_layout_appendix_for_model (skill_tool.py:283-330)
func formatLayoutAppendixForModel(layout map[string]any, skillName string) string {
	var parts []string
	if treeText := firstTreeBlock(layout["directory_tree"]); treeText != "" {
		suffix := ""
		if truncated, _ := layout["directory_tree_truncated"].(bool); truncated {
			suffix = "\n…(truncated)"
		}
		parts = append(parts, "## Directory layout\n```\n"+treeText+suffix+"\n```")
	}
	names, _ := layout["discovered_skill_names"].([]string)
	if len(names) > 0 {
		var lines []string
		for _, name := range names {
			if strings.TrimSpace(name) == "" {
				continue
			}
			lines = append(lines, "- "+name+"/SKILL.md")
		}
		if len(lines) > 0 {
			note := ""
			if truncated, _ := layout["discovered_skill_names_truncated"].(bool); truncated {
				note = "\n…(truncated)"
			}
			parent := strings.TrimSpace(skillName)
			if parent == "" {
				parent = "<this skill>"
			}
			example := names[0]
			parts = append(parts,
				"## Nested skills\n"+
					"Directories inside this skill. They are **not** top-level skills — "+
					"`skill_tool(skill_name=\""+example+"\")` will not find them. Keep this "+
					"skill's name and point relative_file_path at the one you want:\n"+
					"`skill_tool(skill_name=\""+parent+"\", relative_file_path=\""+example+"/SKILL.md\")`\n"+
					strings.Join(lines, "\n")+note)
		}
	}
	return strings.Join(parts, "\n\n")
}

// firstTreeBlock 取目录树文本块（Python 侧为单元素列表）。
func firstTreeBlock(raw any) string {
	switch typed := raw.(type) {
	case []string:
		if len(typed) > 0 {
			return strings.TrimSpace(typed[0])
		}
	case string:
		return strings.TrimSpace(typed)
	}
	return ""
}
```

`doc.go` 文件树加：

```
//	├── skill_layout.go     # 技能目录树/嵌套子技能/布局附录
```

- [ ] **Step 4: 改造 skill_tool.Invoke（对齐 skill_tool.py:381-447）**

替换 `Invoke` 主体（保留函数签名与 `Stream`/`Card`）：

```go
func (t *SkillTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	skillName := strings.TrimSpace(fmt.Sprintf("%v", inputs["skill_name"]))
	relativeFilePath := defaultSkillFileName
	if raw := strings.TrimSpace(fmt.Sprintf("%v", inputs["relative_file_path"])); raw != "" {
		relativeFilePath = raw
	}

	// Python: skill_tool.py:385-393 路径安全校验
	if !isSafeRelativeFilePath(relativeFilePath) {
		return map[string]any{
			"success": false,
			"error":   "Invalid relative_file_path: absolute paths and '..' traversal components are not allowed",
		}, nil
	}

	callOpts := tool.NewToolCallOptions(opts...)
	var sess sessioninterfaces.SessionFacade
	if callOpts != nil {
		sess = callOpts.Session
	}

	skill := t.getSkillByName(skillName, sess)
	if skill == nil {
		logger.Warn(logComponent).Str("skill_name", skillName).Msg("SkillTool skill not found")
		return map[string]any{"success": false, "error": fmt.Sprintf("Skill not found: %s", skillName)}, nil
	}

	filePath := filepath.Join(skill.Directory, filepath.FromSlash(strings.ReplaceAll(relativeFilePath, "\\", "/")))
	readFileResult, readErr := t.operation.Fs().ReadFile(ctx, filePath)
	if readErr != nil {
		logger.Error(logComponent).Str("skill_name", skillName).Str("file_path", filePath).Err(readErr).Msg("SkillTool error reading file")
		return map[string]any{"success": false, "error": readErr.Error()}, nil
	}
	if readFileResult.Code != 0 {
		logger.Error(logComponent).Str("skill_name", skillName).Str("file_path", filePath).Str("message", readFileResult.Message).Msg("SkillTool failed to read file")
		return map[string]any{"success": false, "error": readFileResult.Message}, nil
	}

	body := ""
	if readFileResult.Data != nil {
		body = readFileResult.Data.Content
	}
	// Python: hint 模式对 markdown 媒体引用加提示（skill_tool.py:412-419）
	if skillMarkdownHasMedia(body) {
		body = applySkillToolMarkdownImagesHint(body, t.enableReadImageMultimodal)
	}

	layout := skillLayoutMetadata(skill.Directory)
	data := map[string]any{
		"skill_directory": skill.Directory,
		"skill_content":   body,
	}
	for key, value := range layout {
		data[key] = value
	}
	// content 必须无条件设置：AbilityManager 优先取 data["content"]，
	// 否则退化为 JSON dump，把正文与附录一起埋掉（Python: skill_tool.py:432-436）
	if appendix := formatLayoutAppendixForModel(layout, skill.Name); appendix != "" {
		data["content"] = strings.TrimRight(body, "\n") + "\n\n" + appendix
	} else {
		data["content"] = body
	}

	return map[string]any{"success": true, "data": data}, nil
}
```

同文件新增媒体提示助手（对齐 `skill_tool.py:30-93`）：

```go
const (
	// skillToolMarkdownImagesHint 图片提示（读图多模态可用）
	skillToolMarkdownImagesHint = "Embedded figures in this skill are markdown links (paths/URLs) only; pixel data is not " +
		"attached. Call read_file on the image path under skills/<skill-name>/… when you need " +
		"to inspect a reference screenshot."
	// skillToolMarkdownImagesVisionHint 图片提示（读图多模态关闭）
	skillToolMarkdownImagesVisionHint = "Embedded figures in this skill are markdown links (paths/URLs) only; pixel data is not " +
		"attached. read_file native image multimodal input is disabled. If a vision tool is " +
		"configured, call visual_question_answering on the image path under " +
		"skills/<skill-name>/… when you need to inspect a reference screenshot."
	// skillToolMarkdownVideosHint 视频提示
	skillToolMarkdownVideosHint = "Embedded videos in this skill are link references only. Skill videos are consumed in " +
		"branch mode (multimodal_skill_mode=branch); do not read_file skill videos on the " +
		"main agent loop as it makes the context window too large."
)

// applySkillToolMarkdownImagesHint 剥离旧提示后按媒体类型前置提示（最多一次）。
// Python: apply_skill_tool_markdown_images_hint (skill_tool.py:68-86)
func applySkillToolMarkdownImagesHint(body string, enableReadImageMultimodal bool) string {
	normalized := stripSkillToolInjectedHints(body)
	var hints []string
	if markdownHasImageReference(normalized) {
		if enableReadImageMultimodal {
			hints = append(hints, skillToolMarkdownImagesHint)
		} else {
			hints = append(hints, skillToolMarkdownImagesVisionHint)
		}
	}
	if markdownHasVideoReference(normalized) {
		hints = append(hints, skillToolMarkdownVideosHint)
	}
	if len(hints) == 0 {
		return normalized
	}
	return strings.Join(hints, "\n\n") + "\n\n" + normalized
}

// skillMarkdownHasMedia 判断技能正文是否含图片或视频引用。
// Python: skill_markdown_has_media (skill_tool.py:88-92)
func skillMarkdownHasMedia(content string) bool {
	return markdownHasImageReference(content) || markdownHasVideoReference(content)
}

// stripSkillToolInjectedHints 反复剥离已注入的三类提示前缀。
// Python: _strip_skill_tool_injected_hints (skill_tool.py:47-66)
func stripSkillToolInjectedHints(body string) string {
	s := body
	for {
		stripped := false
		for _, hint := range []string{skillToolMarkdownImagesHint, skillToolMarkdownImagesVisionHint, skillToolMarkdownVideosHint} {
			prefix := hint + "\n\n"
			if strings.HasPrefix(s, prefix) {
				s = strings.TrimPrefix(s, prefix)
				stripped = true
			}
		}
		if !stripped {
			break
		}
	}
	return s
}
```

工具描述文案（`internal/agentcore/harness/prompts/tools/skill_tool.go:14-18`）改为与上游一致：

```go
var skillToolDescription = map[string]string{
	"cn": "使用此工具查看特定技能的内容。成功时默认附带技能根目录的 ASCII 目录树（directory_tree）" +
		"以及嵌套子技能相对路径列表（discovered_skill_names，含 SKILL.md 的子目录）；" +
		"加载子技能请再次调用本工具，并设置 relative_file_path（如 designer/SKILL.md）。",
	"en": "Use this tool to view the skill contents of a certain skill. " +
		"On success it always includes an ASCII directory_tree of the skill root " +
		"and discovered_skill_names (relative paths of nested dirs that contain SKILL.md). " +
		"To load a nested skill, call again with relative_file_path " +
		"(e.g. designer/SKILL.md).",
}
```

- [ ] **Step 5: 运行确认通过 + 覆盖率**

```bash
gofmt -w internal/agentcore/harness/tools/skills/
go test ./internal/agentcore/harness/tools/skills/ -v -cover
```

Expected: 全部 PASS，覆盖率 ≥ 85%。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/tools/skills/ internal/agentcore/harness/prompts/tools/skill_tool.go
git commit -m "feat(skill): align skill_tool with directory layout, path safety and content rendering"
```

---

### Task 7: rail init 对齐（R-1/R-2/R-3）

**Files:**
- Modify: `internal/agentcore/harness/rails/skills/skill_use_rail.go:104`（priority）、`:296-341`（注册逻辑）
- Test: `internal/agentcore/harness/rails/skills/skill_use_rail_baseline_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestInit_已存在的工具不再覆盖注册(t *testing.T) {
	owner := &fakeToolCard{name: "read_file"}
	am := newMockAbilityManager()
	am.abilities["read_file"] = owner

	agent := newMockAgentWith(t, am, newMockSystemPromptBuilder("cn"))
	r := NewSkillUseRail([]string{t.TempDir()}, WithIncludeTools(true))

	require.NoError(t, r.Init(context.Background(), agent))
	_, ok := r.ownedToolNames["read_file"]
	assert.False(t, ok, "他人拥有的工具不得被本 rail 认领")
}

func TestSkillUseRailPriority_与上游一致(t *testing.T) {
	r := NewSkillUseRail([]string{t.TempDir()})
	assert.Equal(t, 95, r.Priority())
}
```

> Task 4 的 `fakeSysOperation`/`fakeFsOperation`/`result` 均为 `skills_test.go` 既有类型与导入，直接复用；`toolfoundation` 为该文件对 `internal/agentcore/foundation/tool` 的别名（若不存在，用 `tool` 并避免与局部变量重名）。
>
> `fakeToolCard`/`newMockAgentWith` 按现有 `skill_use_rail_test.go` 的 mock 形态实现：`newMockAbilityManager()` 已存在；若测试里没有构造 agent 的助手，直接复用现有用例中的构造方式（`grep -n "Init(context.Background()" internal/agentcore/harness/rails/skills/skill_use_rail_test.go` 看现成写法）。

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/harness/rails/skills/ -run 'TestInit_已存在的工具不再覆盖注册|TestSkillUseRailPriority' -v
```

Expected: priority 断言失败（当前 100）；owner 用例失败（当前无条件注册）。

- [ ] **Step 3: 改 priority 与注册逻辑**

```go
	// skillUseRailPriority Rail 优先级
	// Python: SkillUseRail.priority = 95（必须晚于持有 read_file/code/bash 的 rail 初始化，
	// 否则 init 的 owner 检查没有意义）(skill_use_rail.py:57-61)
	skillUseRailPriority = 95
```

`Init` 中删除 ResourceMgr 预删除块，改为"已被他人拥有则跳过"（对齐 `skill_use_rail.py:377-386`）：

```go
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range tools {
		name := t.Card().Name
		// Python: 已由其他 rail（SysOperation 等）拥有的能力不抢注册——
		// 对方的实例携带本 rail 无法复现的策略（只读、deny 规则、多模态覆盖）
		if am != nil {
			if existing := am.Get(name); existing != nil {
				logger.Debug(logger.ComponentAgentCore).Str("tool_name", name).Msg("ability already owned by another rail, skip fallback registration")
				continue
			}
		}
		toolID := t.Card().ID
		if resourceMgr != nil && toolID != "" {
			if err := resourceMgr.AddTool(t); err != nil {
				logger.Warn(logger.ComponentAgentCore).Str("tool_id", toolID).Err(err).Msg("failed to add tool resource to resource_mgr")
				continue
			}
			r.ownedToolIDs[toolID] = struct{}{}
		}
		if am != nil {
			if result := am.Add(t.Card()); result.Added {
				r.ownedToolNames[name] = struct{}{}
			}
		}
	}
```

> `am.Get(name)` 的方法名以实际 AbilityManager 接口为准（`grep -n "Get(" internal/agentcore/single_agent/ability/ability_manager.go | head`）；若为 `GetAbility`/`GetCard`，替换为实际名。

- [ ] **Step 4: 运行 rail 与相关包测试（含只读/审批回归）**

```bash
go test ./internal/agentcore/harness/rails/... -v -cover
go test ./internal/agentcore/harness/... ./internal/swarm/server/adapter/... -count=1
```

Expected: 全部 PASS。若 `security` 或 `agent_mode` 包有用例依赖注册顺序，按失败信息调整断言（不得改生产语义）。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/agentcore/harness/rails/skills/
git add internal/agentcore/harness/rails/skills/
git commit -m "fix(skill): defer to rails that already own read_file/code/bash (priority 95)"
```

---

### Task 8: 触发链 RefreshSkillRails + 三处挂接

**Files:**
- Create: `internal/swarm/server/adapter/deep_adapter_skills.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_mcp.go:53-100`、`internal/swarm/server/adapter/deep_adapter_config.go:91`（函数末尾）、`internal/swarm/agents/harness/tools/skill_toolkit.go`（InstallSkill/UninstallSkill 成功后）
- Test: `internal/swarm/server/adapter/deep_adapter_skills_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
package adapter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
)

func TestRefreshSkillRails_无rail时安全返回(t *testing.T) {
	d := &DeepAdapter{}
	assert.NoError(t, d.RefreshSkillRails(context.Background()))
}

func TestRefreshSkillRails_更新disabled并重载(t *testing.T) {
	rail := skillrails.NewSkillUseRail([]string{t.TempDir()})
	d := &DeepAdapter{skillRail: rail}
	assert.NoError(t, d.RefreshSkillRails(context.Background()))
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/swarm/server/adapter/ -run TestRefreshSkillRails -v
```

Expected: `d.RefreshSkillRails undefined`。

- [ ] **Step 3: 实现 RefreshSkillRails**

新建 `internal/swarm/server/adapter/deep_adapter_skills.go`：

```go
package adapter

import (
	"context"
	"path/filepath"

	skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RefreshSkillRails 技能环境变化后重载 rail 并清会话基线。
//
// Python: JiuWenSwarmDeepAdapter.refresh_skill_rails (interface_deep.py:11154-11260)
func (d *DeepAdapter) RefreshSkillRails(ctx context.Context) error {
	rail, ok := d.skillRail.(*skillrails.SkillUseRail)
	if !ok || rail == nil {
		return nil
	}

	// 1. 重算扫描根：绑定包目录前置 + workspace 技能目录（去重）
	dirs := d.skillScanDirs()
	if len(dirs) == 0 {
		return nil
	}
	rail.SetSkillsDir(dirs)

	// 2. 更新 disabled 列表
	if d.skillManager != nil {
		rail.SetDisabledSkills(d.skillManager.ListExecutionDisabledSkills())
	}

	// 3. 重载技能
	if err := rail.ReloadSkills(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("skill rail reload failed")
	}

	// 4. 清会话基线，强制下轮按新集合重拍。
	// Python: _clear_skill_session_baseline (interface_deep.py:11261-11274)
	// 无活跃会话时 no-op（上游 getattr(instance, "_loop_session", None) 为 None 直接 return）
	if d.instance != nil {
		if sess := d.instance.LoopSession(); sess != nil {
			if err := rail.ClearSessionBaseline(sess); err != nil {
				logger.Warn(logComponent).Err(err).Msg("clear skill session baseline failed")
			}
		}
	}

	logger.Info(logComponent).
		Int("scan_dirs", len(dirs)).
		Msg("skill rails refreshed")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// skillScanDirs 汇总技能扫描根：绑定包目录在前，workspace 技能目录在后，去重。
// Python: _skill_scan_dirs (interface_deep.py:11190-11220)
func (d *DeepAdapter) skillScanDirs() []string {
	seen := map[string]struct{}{}
	var dirs []string
	appendDir := func(raw string) {
		if raw == "" {
			return
		}
		resolved := raw
		if abs, err := filepath.Abs(raw); err == nil {
			resolved = abs
		}
		if _, ok := seen[resolved]; ok {
			return
		}
		seen[resolved] = struct{}{}
		dirs = append(dirs, raw)
	}

	if bound, ok := d.instanceOverrides["skills"].([]any); ok {
		for _, item := range bound {
			if text, ok := item.(string); ok {
				appendDir(text)
			}
		}
	} else if text, ok := d.instanceOverrides["skills"].(string); ok {
		appendDir(text)
	}
	if ws := workspace.AgentSkillsDir(); ws != "" {
		appendDir(ws)
	}
	return dirs
}
```

rail 侧补两个 setter（`skill_use_rail.go` 导出函数区）：

```go
// SetSkillsDir 更新技能扫描根（热刷新用）。
// Python: refresh_skill_rails 直接赋值 self._skill_rail.skills_dir (:11214-11216)
func (r *SkillUseRail) SetSkillsDir(dirs []string) {
	r.skillsDir = dirs
}

// SetDisabledSkills 更新禁用名单（热刷新用）。
// Python: refresh_skill_rails 赋值 disabled_skills (:11217-11218)
func (r *SkillUseRail) SetDisabledSkills(names []string) {
	r.disabledSkills = normalizeNameSet(names)
}
```

三处挂接：

`deep_adapter_mcp.go` `syncMcpServersForRuntime` 的 `to_check` 循环之后、`return nil` 之前：

```go
	// MCP 技能包可能随服务增删变化，需重载技能 rail 并清会话基线。
	// Python: agent_ws_server MCP 卸载/断开后 refresh_skill_rails
	if err := d.RefreshSkillRails(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("refresh skill rails after mcp sync failed")
	}
```

`deep_adapter_config.go` `updateRuntimeConfig` 末尾（`ctx` 与函数末尾位置以现文件为准）：

```go
	// 配置热更新可能改动 skill_mode / 禁用开关，重载技能 rail。
	// Python: agent_manager.refresh_skill_rails 在配置变更后调用
	if err := d.RefreshSkillRails(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("refresh skill rails after runtime config update failed")
	}
```

`skill_toolkit.go`：`InstallSkill` 与 `UninstallSkill` 成功返回前，取会话并清基线：

```go
	// 安装/卸载后技能目录已变，立刻重载 rail 并清本会话基线。
	// Python: skill_installer 写盘后由 refresh_skill_rails 统一处理
	if rail, ok := tk.skillRailHandle().(*skillrails.SkillUseRail); ok && rail != nil {
		if err := rail.ReloadSkills(ctx); err == nil {
			if opts := tool.NewToolCallOptions(options...); opts != nil {
				_ = rail.ClearSessionBaseline(opts.Session)
			}
		}
	}
```

> `tk.skillRailHandle()` 需按 SkillToolkit 现有字段实现（`grep -n "type SkillToolkit struct" -A 30 internal/swarm/agents/harness/tools/skill_toolkit.go`）；若 toolkit 无 rail 引用，则改为经 `tk.onSkillsChanged func(ctx, sess)` 回调注入，由 adapter 构造 toolkit 时赋值为 `d.RefreshSkillRails`——二者择一，禁止留空实现。

- [ ] **Step 4: 运行测试**

```bash
go test ./internal/swarm/server/adapter/ ./internal/swarm/agents/harness/tools/ -v -cover
```

Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
gofmt -w internal/swarm/server/adapter/ internal/swarm/agents/harness/tools/ internal/agentcore/harness/rails/skills/
git add internal/swarm/server/adapter/ internal/swarm/agents/harness/tools/ internal/agentcore/harness/rails/skills/
git commit -m "feat(skill): refresh skill rails and drop session baseline on skill environment changes"
```

---

### Task 9: 集成测试 + 全量回归 + 计划回填

**Files:**
- Create: `tests/integration/agentcore/harness/skill_baseline_test.go`
- Modify: `IMPLEMENTATION_PLAN.md:601`（9.19-23 行追加，不新增行号）

- [ ] **Step 1: 写集成用例（跨 rail + tool + session 三子系统）**

新建 `tests/integration/agentcore/harness/skills/skill_baseline_test.go`（新目录，需同时建 `doc.go`，包名 `skills_test` 用基准包名 `skills`：按规则 1，`tests/integration` 下非 `_test.go` 的 `doc.go` 用基准包名）：

```go
//go:build integration

package skills_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	skilltools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/skills"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// itAgent 集成测试用 agent：只覆盖 SkillUseRail.Init / 附件注入用到的方法，
// 其余方法由嵌入的接口提供（未被调用，不会触发）。
type itAgent struct {
	agentinterfaces.BaseAgent
	builder   saprompt.SystemPromptBuilderInterface
	ability   agentinterfaces.AbilityManagerInterface
	attachMgr *prompts.Manager
}

// itSession 集成测试用会话门面。
type itSession struct {
	id       string
	stateMap map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

func (a *itAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return a.builder }
func (a *itAgent) AbilityManager() agentinterfaces.AbilityManagerInterface    { return a.ability }
func (a *itAgent) Card() *agentschema.AgentCard                              { return &agentschema.AgentCard{ID: "it-agent"} }
func (a *itAgent) PromptAttachments() *prompts.Manager                       { return a.attachMgr }

func (s *itSession) GetSessionID() string { return s.id }

func (s *itSession) UpdateState(data map[string]any) {
	for k, v := range data {
		s.stateMap[k] = v
	}
}

func (s *itSession) GetState(key state.StateKey) (any, error) { return s.stateMap[key.String()], nil }
func (s *itSession) DumpState() map[string]any                { return s.stateMap }
func (s *itSession) WriteStream(context.Context, any) error   { return nil }
func (s *itSession) WriteCustomStream(context.Context, any) error { return nil }
func (s *itSession) GetEnv(string, ...any) any                { return nil }
func (s *itSession) Interact(context.Context, any) error      { return nil }
func (s *itSession) ClearSession(context.Context)             {}

func TestSkillBaselineLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSkill(t, root, "alpha")

	rail := skillrails.NewSkillUseRail([]string{root}, skillrails.WithSkillMode(skillrails.SkillModeAll))
	rail.SetSysOperation(sysop.NewLocalSysOperation(&sysop.SysOperationCard{}))
	agent := &itAgent{
		builder:   saprompt.NewSystemPromptBuilder(),
		ability:   ability.NewAbilityManager(nil),
		attachMgr: prompts.NewManager(),
	}
	require.NoError(t, rail.Init(ctx, agent))

	sess := &itSession{id: "sess-1", stateMap: map[string]any{}}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, sess)

	// ① 首次 BeforeInvoke：基线落盘为 {alpha}，skill_tool 经会话视图可读
	require.NoError(t, rail.BeforeInvoke(ctx, cbc))
	assert.Equal(t, []string{"alpha"}, namesOf(rail.GetSkillsForSession(sess)))

	skillTool := skilltools.NewSkillTool(
		rail.SysOperation(),
		func(s sessioninterfaces.SessionFacade) []*skillpkg.Skill { return rail.GetSkillsForSession(s) },
		"cn", "it-agent", true,
	)
	res, err := skillTool.Invoke(ctx, map[string]any{"skill_name": "alpha"}, tool.WithToolSession(sess))
	require.NoError(t, err)
	require.Equal(t, true, res["success"])
	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, data["content"], "# alpha")
	assert.Contains(t, data["content"], "## Directory layout")

	// ② 会话中途新增 beta：视图 = 基线 ∪ 新增，并产生运行时变更附件
	writeSkill(t, root, "beta")
	require.NoError(t, rail.BeforeInvoke(ctx, cbc))
	assert.Equal(t, []string{"alpha", "beta"}, namesOf(rail.GetSkillsForSession(sess)))
	att := agent.attachMgr.Find("sess-1", "skills.runtime_changes", "skill_use_rail")
	require.NotNil(t, att, "新增技能必须写入运行时附件")
	assert.Contains(t, att.Content, "beta")

	// ③ 清基线后重拍：移除 alpha 后视图只剩 beta（禁用/卸载即时生效）
	require.NoError(t, os.RemoveAll(filepath.Join(root, "alpha")))
	require.NoError(t, rail.ClearSessionBaseline(sess))
	require.NoError(t, rail.BeforeInvoke(ctx, cbc))
	assert.Equal(t, []string{"beta"}, namesOf(rail.GetSkillsForSession(sess)))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeSkill 在技能根下写一个最小技能。
func writeSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+name), 0o644))
}

// namesOf 提取技能名序列。
func namesOf(skills []*skillpkg.Skill) []string {
	out := make([]string, 0, len(skills))
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return out
}
```

同目录新建 `doc.go`（基准包名，非 `_test.go`）：

```go
// Package skills 提供技能会话基线的跨子系统集成测试。
//
// 覆盖链路：SkillUseRail（基线落盘/提示词节/运行时附件）→ SkillTool（会话视图读文件）
// → session state（`skill_use` 读写）→ PromptAttachmentManager（`skills.runtime_changes`）。
//
// 对应 Python：openjiuwen/harness/rails/skills/skill_use_rail.py
package skills
```


- [ ] **Step 2: 运行集成用例**

```bash
go test -tags=integration ./tests/integration/agentcore/harness/skills/ -run TestSkillBaselineLifecycle -v
go run ./cmd/integration-audit -verify 2>&1 | tail -20
```

Expected: 用例 PASS；`integration-audit` 该目录不报"跨度不足"（跨 `harness/rails` + `harness/tools` + `session` 三子系统）。若报灰区，按规则 7 补第二个用例覆盖 `skill_tool` 调用路径（`tools` 子系统行数 > 30）。

- [ ] **Step 3: 全量回归**

```bash
pgrep -f 'go (build|test)' || true
go build ./...
go vet ./...
go test ./internal/... -count=1
```

Expected: 全绿。失败项若与本次改动无关，先 `git stash` 复验是否为存量问题并记录。

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md（只回填 9.19-23 原文）**

在 9.19-23 行的 Skill 段落末尾追加（不改动其他内容、不新增行号）：

```
；⤴️ 2026-11-05 回填 skill baseline：会话基线（`skill_use` state，schema v1）+ `GetSkillsForSession` 合并视图（基线∪新增）+ `skills.runtime_changes` 附件（中英双语，含演进经验）+ `ClearSessionBaseline` + 三处触发链（技能安装/卸载、MCP 热同步、配置热更新）+ `skill_tool` 全量对齐（路径安全、目录树 `directory_tree`、嵌套子技能 `discovered_skill_names`、`content` 纯文本渲染、媒体提示 hint）+ init 对齐（priority 95、owner 检查、删 ResourceMgr 预删除）；⤵️ `multimodal_skill_mode=attach/branch` 未移植（branch 依赖 mobile_gui skill_branch）；⤵️ `mount_skill_root`/`validate_skill_source` 随 MoA 章节；⤵️ `TeamSkillUseRail` 见 25.10
```

- [ ] **Step 5: 提交**

```bash
git add tests/integration/agentcore/harness/skills/ IMPLEMENTATION_PLAN.md
git commit -m "test(skill): cover session baseline lifecycle end to end"
```

---

## 验收清单

- [ ] `go build ./...`、`go vet ./...` 通过
- [ ] `go test ./internal/... -count=1` 全绿
- [ ] 改动文件覆盖率 ≥ 85%（`go test -cover ./internal/agentcore/harness/rails/skills/ ./internal/agentcore/harness/tools/skills/`）
- [ ] `go test -tags=integration ./tests/integration/agentcore/harness/skills/` 通过，`integration-audit -verify` 无新灰区
- [ ] `grep -rn "Go 侧未移植该子特性" internal/` 为空
- [ ] `grep -rn "func() \[\]\*skillpkg.Skill" internal/agentcore/harness/tools/skills/` 为空（旧签名已删）
- [ ] `IMPLEMENTATION_PLAN.md` 仅 9.19-23 行内容变化（`git diff --stat` 确认无其他行）
- [ ] 远端 CI 四 job 全绿（规则 6）
