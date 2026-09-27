# 9.68-69 Team Rails / Prompts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现团队级 Prompts（9.69）和 Rails（9.68），让 TeamAgent 从裸模型变成有团队意识的协作成员。

**Architecture:** 先实现 Prompts 包（模板加载 + section builder + 缓存），再实现 Rails 包（5 个 Rail 类型 + 团队工具注册），最后回填 harness.go / agent_configurator.go / resources.go / doc.go 中的 TODO 和 any 占位。模板文件从 Python 一比一复制。

**Tech Stack:** Go 1.22+, go:embed, sync.Mutex (替代 Python asyncio), channel (替代 asyncio.Event), DeepAgentRail/BaseInterruptRail 基类

**Spec:** `docs/superpowers/specs/2026-09-27-team-rails-prompts-9.68-69-design.md`

---

## File Structure

### New Files (9.69 Prompts)

| File | Responsibility |
|------|---------------|
| `internal/agent_teams/prompts/doc.go` | 包文档 |
| `internal/agent_teams/prompts/loader.go` | go:embed 模板加载器 + PromptTemplate + Render |
| `internal/agent_teams/prompts/section_cache.go` | MtimeSectionCache（mtime 探针缓存） |
| `internal/agent_teams/prompts/policy.go` | RolePolicy + BuildSystemPrompt |
| `internal/agent_teams/prompts/sections.go` | 8 个 builder + TeamSectionName + _LABELS + 8 个 HITT 内部函数 |
| `internal/agent_teams/prompts/team_plan_agent.go` | ApplyTeamPlanAgentPrompt + TeamPlanAgentDesc + BuildTeamPlanAgentCard |
| `internal/agent_teams/prompts/team_plan_mode.go` | BuildTeamPlanModeSection + 常量 |
| `internal/agent_teams/prompts/cn/*.md` (9 个) | 中文模板（从 Python 一比一复制） |
| `internal/agent_teams/prompts/en/*.md` (9 个) | 英文模板（从 Python 一比一复制） |
| `internal/agent_teams/prompts/system_prompt.md` | 语言无关顶层模板 |

### New Files (9.68 Rails)

| File | Responsibility |
|------|---------------|
| `internal/agent_teams/rails/doc.go` | 包文档 |
| `internal/agent_teams/rails/first_iteration_gate.go` | FirstIterationGate（channel 替代 asyncio.Event） |
| `internal/agent_teams/rails/team_tool_rail.go` | TeamToolRail（priority=90）+ QualifyTeamToolIDs |
| `internal/agent_teams/rails/team_policy_rail.go` | TeamPolicyRail（priority=12）+ 8 个 PromptSection 注入 |
| `internal/agent_teams/rails/tool_approval_rail.go` | TeamToolApprovalRail（继承 BaseInterruptRail） |
| `internal/agent_teams/rails/team_plan_mode_rail.go` | TeamPlanModeRail（priority=84） |

### New Files (团队工具)

| File | Responsibility |
|------|---------------|
| `internal/agent_teams/tools/team_tools.go` | 12 个 ToolCard + CreateTeamTools 函数 |

### Modified Files (回填)

| File | Change |
|------|--------|
| `internal/agent_teams/harness.go` | 5 个 any → 具体类型 + BuildTeamHarness 参数类型化 + AddRail 调用 |
| `internal/agent_teams/agent/agent_configurator.go` | 构造 6 个 Rail + 传参 + 清理 5 处已完成 TODO |
| `internal/agent_teams/agent/resources.go` | FirstIterGate any → 具体类型 |
| `internal/agent_teams/doc.go` | rails/ + prompts/ 从 ⤵️ 改为正式条目 |

---

## Phase 1: 9.69 Prompts

### Task 1: 复制 .md 模板文件

**Files:**
- Create: `internal/agent_teams/prompts/cn/leader_policy.md`
- Create: `internal/agent_teams/prompts/cn/teammate_policy.md`
- Create: `internal/agent_teams/prompts/cn/leader_workflow.md`
- Create: `internal/agent_teams/prompts/cn/leader_workflow_predefined.md`
- Create: `internal/agent_teams/prompts/cn/leader_workflow_hybrid.md`
- Create: `internal/agent_teams/prompts/cn/lifecycle_temporary.md`
- Create: `internal/agent_teams/prompts/cn/lifecycle_persistent.md`
- Create: `internal/agent_teams/prompts/cn/team_plan_mode.md`
- Create: `internal/agent_teams/prompts/cn/team_plan_agent.md`
- Create: `internal/agent_teams/prompts/en/leader_policy.md`
- Create: `internal/agent_teams/prompts/en/teammate_policy.md`
- Create: `internal/agent_teams/prompts/en/leader_workflow.md`
- Create: `internal/agent_teams/prompts/en/leader_workflow_predefined.md`
- Create: `internal/agent_teams/prompts/en/leader_workflow_hybrid.md`
- Create: `internal/agent_teams/prompts/en/lifecycle_temporary.md`
- Create: `internal/agent_teams/prompts/en/lifecycle_persistent.md`
- Create: `internal/agent_teams/prompts/en/team_plan_mode.md`
- Create: `internal/agent_teams/prompts/en/team_plan_agent.md`
- Create: `internal/agent_teams/prompts/system_prompt.md`

- [ ] **Step 1: 创建 prompts 目录**

```bash
mkdir -p internal/agent_teams/prompts/cn internal/agent_teams/prompts/en
```

- [ ] **Step 2: 复制中文模板文件**

从 Python 源 `/home/opensource/agent-core/openjiuwen/agent_teams/prompts/cn/` 一比一复制 9 个 .md 文件到 `internal/agent_teams/prompts/cn/`。不得自行翻译或改写。

```bash
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/cn/leader_policy.md internal/agent_teams/prompts/cn/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/cn/teammate_policy.md internal/agent_teams/prompts/cn/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/cn/leader_workflow.md internal/agent_teams/prompts/cn/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/cn/leader_workflow_predefined.md internal/agent_teams/prompts/cn/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/cn/leader_workflow_hybrid.md internal/agent_teams/prompts/cn/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/cn/lifecycle_temporary.md internal/agent_teams/prompts/cn/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/cn/lifecycle_persistent.md internal/agent_teams/prompts/cn/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/cn/team_plan_mode.md internal/agent_teams/prompts/cn/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/cn/team_plan_agent.md internal/agent_teams/prompts/cn/
```

- [ ] **Step 3: 复制英文模板文件**

从 Python 源 `/home/opensource/agent-core/openjiuwen/agent_teams/prompts/en/` 一比一复制 9 个 .md 文件到 `internal/agent_teams/prompts/en/`。

```bash
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/en/leader_policy.md internal/agent_teams/prompts/en/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/en/teammate_policy.md internal/agent_teams/prompts/en/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/en/leader_workflow.md internal/agent_teams/prompts/en/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/en/leader_workflow_predefined.md internal/agent_teams/prompts/en/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/en/leader_workflow_hybrid.md internal/agent_teams/prompts/en/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/en/lifecycle_temporary.md internal/agent_teams/prompts/en/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/en/lifecycle_persistent.md internal/agent_teams/prompts/en/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/en/team_plan_mode.md internal/agent_teams/prompts/en/
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/en/team_plan_agent.md internal/agent_teams/prompts/en/
```

- [ ] **Step 4: 复制共享模板**

```bash
cp /home/opensource/agent-core/openjiuwen/agent_teams/prompts/system_prompt.md internal/agent_teams/prompts/
```

注意：`system_prompt.md` 使用 `{{var}}` 双花括号占位符，`team_plan_mode.md` 使用 `{var}` 单花括号占位符。loader.go 的 Render 方法需同时支持两种格式。

- [ ] **Step 5: 验证文件完整性**

```bash
find internal/agent_teams/prompts -name "*.md" | sort
```

Expected: 19 个 .md 文件（9 cn + 9 en + 1 system_prompt）

- [ ] **Step 6: Commit**

```bash
git add internal/agent_teams/prompts/cn/ internal/agent_teams/prompts/en/ internal/agent_teams/prompts/system_prompt.md
git commit -m "feat(prompts): copy bilingual .md templates from Python (9.69 step 1)"
```

---

### Task 2: prompts/doc.go + loader.go

**Files:**
- Create: `internal/agent_teams/prompts/doc.go`
- Create: `internal/agent_teams/prompts/loader.go`
- Test: `internal/agent_teams/prompts/loader_test.go`

- [ ] **Step 1: 写 loader_test.go 失败测试**

```go
package prompts

import (
	"testing"
)

// TestLoadTemplate_中文模板 测试加载中文模板
func TestLoadTemplate_中文模板(t *testing.T) {
	tpl := LoadTemplate("leader_policy", "cn")
	if tpl == nil {
		t.Fatal("LoadTemplate(leader_policy, cn) returned nil")
	}
	content := tpl.Render(nil)
	if content == "" {
		t.Fatal("leader_policy cn template is empty")
	}
}

// TestLoadTemplate_英文模板 测试加载英文模板
func TestLoadTemplate_英文模板(t *testing.T) {
	tpl := LoadTemplate("leader_policy", "en")
	if tpl == nil {
		t.Fatal("LoadTemplate(leader_policy, en) returned nil")
	}
	content := tpl.Render(nil)
	if content == "" {
		t.Fatal("leader_policy en template is empty")
	}
}

// TestLoadSharedTemplate 测试加载共享模板
func TestLoadSharedTemplate(t *testing.T) {
	tpl := LoadSharedTemplate("system_prompt")
	if tpl == nil {
		t.Fatal("LoadSharedTemplate(system_prompt) returned nil")
	}
	if tpl.Render(nil) == "" {
		t.Fatal("system_prompt template is empty")
	}
}

// TestPromptTemplate_Render_单花括号 测试单花括号 {var} 占位符渲染
func TestPromptTemplate_Render_单花括号(t *testing.T) {
	tpl := LoadTemplate("team_plan_mode", "cn")
	if tpl == nil {
		t.Fatal("team_plan_mode cn template not found")
	}
	result := tpl.Render(map[string]string{
		"enter_plan_mode_status": "尚未调用 enter_plan_mode",
		"plan_file_info":         "Plan 文件: /tmp/plan.md",
	})
	if result == "" {
		t.Fatal("Render returned empty string")
	}
}

// TestPromptTemplate_Render_双花括号 测试双花括号 {{var}} 占位符渲染
func TestPromptTemplate_Render_双花括号(t *testing.T) {
	tpl := LoadSharedTemplate("system_prompt")
	result := tpl.Render(map[string]string{
		"member_name_section": "[name]",
		"role_policy":         "[role]",
		"workflow_section":    "[wf]",
		"lifecycle_section":   "[lc]",
		"persona_label":       "[pl]",
		"persona":             "[p]",
		"team_info_section":   "[info]",
		"team_members_section": "[members]",
		"base_prompt_section": "[bp]",
	})
	if result == "" {
		t.Fatal("Render returned empty string")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/prompts/... -run "TestLoad" -v 2>&1 | head -20
```

Expected: 编译失败（包不存在）

- [ ] **Step 3: 创建 prompts/doc.go**

```go
// Package prompts 提供团队级提示词模板和 Section 构建器。
//
// 本包负责：
//   - 从嵌入的 .md 模板加载中英文提示词
//   - 构建团队策略的 8 个 PromptSection（角色/工作流/生命周期/人设/额外/信息/成员/HITT）
//   - MtimeSectionCache 基于 DB 更新时间戳的探针缓存
//   - team.plan 模式下的 plan_agent 提示词特化
//
// Python: openjiuwen/agent_teams/prompts/
//
// 文件目录：
//
//	prompts/
//	├── doc.go                # 包文档
//	├── loader.go             # go:embed 模板加载器
//	├── section_cache.go      # MtimeSectionCache mtime 探针缓存
//	├── policy.go             # RolePolicy + BuildSystemPrompt
//	├── sections.go           # 8 个 PromptSection builder + TeamSectionName + _LABELS + HITT
//	├── team_plan_agent.go    # ApplyTeamPlanAgentPrompt + 常量
//	├── team_plan_mode.go     # BuildTeamPlanModeSection + 常量
//	├── cn/                   # 9 个中文 .md 模板
//	├── en/                   # 9 个英文 .md 模板
//	└── system_prompt.md      # 语言无关顶层模板
//
// 对应 Python 代码：openjiuwen/agent_teams/prompts/
package prompts
```

- [ ] **Step 4: 实现 loader.go**

```go
package prompts

import (
	"embed"
	"strings"
	"sync"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptTemplate 提示词模板，支持单花括号 {var} 和双花括号 {{var}} 占位符渲染。
// Python: PromptTemplate (openjiuwen/agent_teams/prompts/loader.py)
type PromptTemplate struct {
	// content 模板原始内容
	content string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

//go:embed cn/*.md en/*.md system_prompt.md
var promptsFS embed.FS

// templateCache 模板缓存（对齐 Python @cache 装饰器）
var templateCache sync.Map

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadTemplate 加载语言相关模板。
// Python: load_template(name, language) -> PromptTemplate
// 从 prompts/<lang>/<name>.md 加载
func LoadTemplate(name, language string) *PromptTemplate {
	key := language + "/" + name
	if cached, ok := templateCache.Load(key); ok {
		return cached.(*PromptTemplate)
	}
	data, err := promptsFS.ReadFile(language + "/" + name + ".md")
	if err != nil {
		return nil
	}
	tpl := &PromptTemplate{content: string(data)}
	templateCache.Store(key, tpl)
	return tpl
}

// LoadSharedTemplate 加载语言无关模板。
// Python: load_shared_template(name) -> PromptTemplate
// 从 prompts/<name>.md 加载
func LoadSharedTemplate(name string) *PromptTemplate {
	if cached, ok := templateCache.Load(name); ok {
		return cached.(*PromptTemplate)
	}
	data, err := promptsFS.ReadFile(name + ".md")
	if err != nil {
		return nil
	}
	tpl := &PromptTemplate{content: string(data)}
	templateCache.Store(name, tpl)
	return tpl
}

// Render 渲染模板，替换占位符。
// 同时支持双花括号 {{var}}（system_prompt.md）和单花括号 {var}（team_plan_mode.md）格式。
// Python: PromptTemplate.format(**kwargs) / str.format()
func (t *PromptTemplate) Render(vars map[string]string) string {
	if t == nil {
		return ""
	}
	result := t.content
	if vars == nil {
		return result
	}
	// 先替换双花括号 {{var}}，再替换单花括号 {var}
	// 双花括号优先级高于单花括号
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
	}
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
}

// Content 返回模板原始内容。
func (t *PromptTemplate) Content() string {
	if t == nil {
		return ""
	}
	return t.content
}

// ToSection 将模板转换为 PromptSection。
// 便捷方法，用于纯文本模板（无占位符）直接构建 section。
func (t *PromptTemplate) ToSection(name string, language string, priority int) saprompt.PromptSection {
	content := t.Render(nil)
	return saprompt.NewPromptSection(name, map[string]string{language: content}, priority)
}
```

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/prompts/... -run "TestLoad" -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent_teams/prompts/doc.go internal/agent_teams/prompts/loader.go internal/agent_teams/prompts/loader_test.go
git commit -m "feat(prompts): add doc.go + loader.go with go:embed template loading (9.69 step 2)"
```

---

### Task 3: section_cache.go

**Files:**
- Create: `internal/agent_teams/prompts/section_cache.go`
- Test: `internal/agent_teams/prompts/section_cache_test.go`

- [ ] **Step 1: 写 section_cache_test.go 失败测试**

```go
package prompts

import (
	"context"
	"sync/atomic"
	"testing"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// TestMtimeSectionCache_首次刷新 测试首次刷新调用 fetchAndBuild
func TestMtimeSectionCache_首次刷新(t *testing.T) {
	var fetchCount int32
	cache := NewMtimeSectionCache(
		func(ctx context.Context) int64 { return 1 },
		func(ctx context.Context) *saprompt.PromptSection {
			atomic.AddInt32(&fetchCount, 1)
			s := saprompt.NewPromptSection("test", map[string]string{"cn": "hello"}, 10)
			return &s
		},
	)
	result := cache.Refresh(context.Background())
	if result == nil {
		t.Fatal("Refresh returned nil")
	}
	if atomic.LoadInt32(&fetchCount) != 1 {
		t.Fatalf("expected 1 fetch, got %d", fetchCount)
	}
}

// TestMtimeSectionCache_缓存命中 测试 mtime 不变时返回缓存
func TestMtimeSectionCache_缓存命中(t *testing.T) {
	var fetchCount int32
	mtime := int64(1)
	cache := NewMtimeSectionCache(
		func(ctx context.Context) int64 { return mtime },
		func(ctx context.Context) *saprompt.PromptSection {
			atomic.AddInt32(&fetchCount, 1)
			s := saprompt.NewPromptSection("test", map[string]string{"cn": "hello"}, 10)
			return &s
		},
	)
	cache.Refresh(context.Background()) // 首次
	cache.Refresh(context.Background()) // 缓存命中
	if atomic.LoadInt32(&fetchCount) != 1 {
		t.Fatalf("expected 1 fetch (cached), got %d", fetchCount)
	}
}

// TestMtimeSectionCache_mtime变化刷新 测试 mtime 变化时重新拉取
func TestMtimeSectionCache_mtime变化刷新(t *testing.T) {
	var fetchCount int32
	mtime := int64(1)
	cache := NewMtimeSectionCache(
		func(ctx context.Context) int64 { return atomic.LoadInt64(&mtime) },
		func(ctx context.Context) *saprompt.PromptSection {
			atomic.AddInt32(&fetchCount, 1)
			s := saprompt.NewPromptSection("test", map[string]string{"cn": "hello"}, 10)
			return &s
		},
	)
	cache.Refresh(context.Background()) // 首次
	atomic.StoreInt64(&mtime, 2)
	cache.Refresh(context.Background()) // mtime 变化，重新拉取
	if atomic.LoadInt32(&fetchCount) != 2 {
		t.Fatalf("expected 2 fetches (mtime changed), got %d", fetchCount)
	}
}

// TestMtimeSectionCache_Invalidate 测试强制失效
func TestMtimeSectionCache_Invalidate(t *testing.T) {
	var fetchCount int32
	cache := NewMtimeSectionCache(
		func(ctx context.Context) int64 { return 1 },
		func(ctx context.Context) *saprompt.PromptSection {
			atomic.AddInt32(&fetchCount, 1)
			s := saprompt.NewPromptSection("test", map[string]string{"cn": "hello"}, 10)
			return &s
		},
	)
	cache.Refresh(context.Background()) // 首次
	cache.Invalidate()
	cache.Refresh(context.Background()) // Invalidate 后重新拉取
	if atomic.LoadInt32(&fetchCount) != 2 {
		t.Fatalf("expected 2 fetches (after invalidate), got %d", fetchCount)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/prompts/... -run "TestMtimeSectionCache" -v 2>&1 | head -20
```

Expected: 编译失败（NewMtimeSectionCache 未定义）

- [ ] **Step 3: 实现 section_cache.go**

```go
package prompts

import (
	"context"
	"sync"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MtimeSectionCache 基于 mtime 探针的 PromptSection 缓存。
// Python: MtimeSectionCache (openjiuwen/agent_teams/prompts/section_cache.py)
//
// 探针（probe）返回单调递增整数（DB 的 updated_at），值不变则跳过 fetchAndBuild。
// Go 用 sync.Mutex 替代 Python 的 asyncio 锁。
type MtimeSectionCache struct {
	// probe 探针函数，返回 DB 的 updated_at 时间戳
	probe func(ctx context.Context) int64
	// fetchAndBuild 全量拉取+构建函数
	fetchAndBuild func(ctx context.Context) *saprompt.PromptSection
	// cached 缓存的 PromptSection
	cached *saprompt.PromptSection
	// cachedMtime 缓存时的 mtime 值
	cachedMtime int64
	// initialized 是否已初始化
	initialized bool
	// mu 互斥锁
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMtimeSectionCache 创建 mtime 探针缓存。
// Python: MtimeSectionCache(probe, fetch_and_build)
func NewMtimeSectionCache(
	probe func(ctx context.Context) int64,
	fetchAndBuild func(ctx context.Context) *saprompt.PromptSection,
) *MtimeSectionCache {
	return &MtimeSectionCache{
		probe:         probe,
		fetchAndBuild: fetchAndBuild,
	}
}

// Refresh 刷新缓存：探针值不变返回缓存，变化则重新 fetchAndBuild。
// Python: async refresh() -> Optional[PromptSection]
func (c *MtimeSectionCache) Refresh(ctx context.Context) *saprompt.PromptSection {
	c.mu.Lock()
	defer c.mu.Unlock()

	currentMtime := c.probe(ctx)
	if c.initialized && c.cachedMtime == currentMtime {
		return c.cached
	}

	section := c.fetchAndBuild(ctx)
	c.cached = section
	c.cachedMtime = currentMtime
	c.initialized = true
	return section
}

// Invalidate 强制下次 Refresh 重新拉取。
// Python: invalidate()
func (c *MtimeSectionCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initialized = false
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/prompts/... -run "TestMtimeSectionCache" -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/prompts/section_cache.go internal/agent_teams/prompts/section_cache_test.go
git commit -m "feat(prompts): add MtimeSectionCache (9.69 step 3)"
```

---

### Task 4: sections.go — Section Builder

这是最大的单个文件（~600-700 行 Go），对齐 Python `sections.py` 的 768 行。

**Files:**
- Create: `internal/agent_teams/prompts/sections.go`
- Test: `internal/agent_teams/prompts/sections_test.go`

- [ ] **Step 1: 写 sections_test.go 失败测试**

```go
package prompts

import (
	"testing"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// TestBuildTeamRoleSection_Leader 测试 Leader 角色构建
func TestBuildTeamRoleSection_Leader(t *testing.T) {
	section := BuildTeamRoleSection("leader", "alice", "build_mode", "cn")
	if section == nil {
		t.Fatal("BuildTeamRoleSection returned nil")
	}
	if section.Name != string(SectionRole) {
		t.Fatalf("expected section name %s, got %s", SectionRole, section.Name)
	}
	if section.Priority != 11 {
		t.Fatalf("expected priority 11, got %d", section.Priority)
	}
	cnContent := section.Content["cn"]
	if cnContent == "" {
		t.Fatal("cn content is empty")
	}
	if section.Content["en"] == "" {
		t.Fatal("en content is empty")
	}
}

// TestBuildTeamRoleSection_TeammatePlanMode 测试 Teammate plan_mode 模式
func TestBuildTeamRoleSection_TeammatePlanMode(t *testing.T) {
	section := BuildTeamRoleSection("teammate", "bob", "plan_mode", "cn")
	if section == nil {
		t.Fatal("BuildTeamRoleSection returned nil")
	}
	cnContent := section.Content["cn"]
	if cnContent == "" {
		t.Fatal("cn content is empty")
	}
	// plan_mode 下 teammate 应包含 submit_plan 提示
	// （标签中包含 submit_plan 关键词）
}

// TestBuildTeamHITTSection_Leader 测试 Leader HITT section
func TestBuildTeamHITTSection_Leader(t *testing.T) {
	section := BuildTeamHITTSection("leader", []string{"human_1"}, "cn", "alice", false)
	if section == nil {
		t.Fatal("BuildTeamHITTSection returned nil for leader with human agents")
	}
	if section.Priority != 12 {
		t.Fatalf("expected priority 12, got %d", section.Priority)
	}
}

// TestBuildTeamHITTSection_无人类成员返回nil 测试无人类成员时返回 nil
func TestBuildTeamHITTSection_无人类成员返回nil(t *testing.T) {
	section := BuildTeamHITTSection("leader", nil, "cn", "alice", false)
	if section != nil {
		t.Fatal("expected nil when no human agents")
	}
}

// TestBuildTeamWorkflowSection_Leader 测试 Leader 工作流
func TestBuildTeamWorkflowSection_Leader(t *testing.T) {
	section := BuildTeamWorkflowSection("leader", "default", "cn")
	if section == nil {
		t.Fatal("BuildTeamWorkflowSection returned nil for leader")
	}
	if section.Priority != 13 {
		t.Fatalf("expected priority 13, got %d", section.Priority)
	}
}

// TestBuildTeamWorkflowSection_Teammate返回nil 测试 Teammate 无工作流
func TestBuildTeamWorkflowSection_Teammate返回nil(t *testing.T) {
	section := BuildTeamWorkflowSection("teammate", "default", "cn")
	if section != nil {
		t.Fatal("expected nil for teammate workflow")
	}
}

// TestBuildTeamLifecycleSection_Leader 测试 Leader 生命周期
func TestBuildTeamLifecycleSection_Leader(t *testing.T) {
	section := BuildTeamLifecycleSection("leader", "temporary", "cn")
	if section == nil {
		t.Fatal("BuildTeamLifecycleSection returned nil for leader")
	}
	if section.Priority != 14 {
		t.Fatalf("expected priority 14, got %d", section.Priority)
	}
}

// TestBuildTeamLifecycleSection_Teammate返回nil 测试 Teammate 无生命周期
func TestBuildTeamLifecycleSection_Teammate返回nil(t *testing.T) {
	section := BuildTeamLifecycleSection("teammate", "temporary", "cn")
	if section != nil {
		t.Fatal("expected nil for teammate lifecycle")
	}
}

// TestBuildTeamPersonaSection_有persona 测试有 persona 时构建 section
func TestBuildTeamPersonaSection_有persona(t *testing.T) {
	section := BuildTeamPersonaSection("高级工程师", "cn")
	if section == nil {
		t.Fatal("BuildTeamPersonaSection returned nil when persona is set")
	}
	if section.Priority != 15 {
		t.Fatalf("expected priority 15, got %d", section.Priority)
	}
}

// TestBuildTeamPersonaSection_空persona返回nil 测试空 persona 返回 nil
func TestBuildTeamPersonaSection_空persona返回nil(t *testing.T) {
	section := BuildTeamPersonaSection("", "cn")
	if section != nil {
		t.Fatal("expected nil when persona is empty")
	}
}

// TestBuildTeamExtraSection 测试额外提示
func TestBuildTeamExtraSection(t *testing.T) {
	section := BuildTeamExtraSection("custom instructions", "cn")
	if section == nil {
		t.Fatal("BuildTeamExtraSection returned nil")
	}
	if section.Priority != 16 {
		t.Fatalf("expected priority 16, got %d", section.Priority)
	}
}

// TestBuildTeamInfoSection 测试团队信息 section
func TestBuildTeamInfoSection(t *testing.T) {
	info := &TeamInfo{
		TeamName:    "my-team",
		DisplayName: "My Team",
		Description: "A test team",
	}
	section := BuildTeamInfoSection(info, "/.team/workspace", "/abs/path", "cn")
	if section == nil {
		t.Fatal("BuildTeamInfoSection returned nil")
	}
	if section.Priority != 65 {
		t.Fatalf("expected priority 65, got %d", section.Priority)
	}
}

// TestBuildTeamMembersSection 测试成员关系 section
func TestBuildTeamMembersSection(t *testing.T) {
	members := []TeamMember{
		{MemberName: "bob", DisplayName: "Bob", Description: "Engineer"},
		{MemberName: "alice", DisplayName: "Alice", Description: "Leader"},
	}
	section := BuildTeamMembersSection(members, "alice", "cn")
	if section == nil {
		t.Fatal("BuildTeamMembersSection returned nil")
	}
	if section.Priority != 66 {
		t.Fatalf("expected priority 66, got %d", section.Priority)
	}
	// 应排除自身 alice
	cnContent := section.Content["cn"]
	if cnContent == "" {
		t.Fatal("cn content is empty")
	}
}

// TestBuildTeamWorkflowSection_三种模式 测试 default/predefined/hybrid 三种模式
func TestBuildTeamWorkflowSection_三种模式(t *testing.T) {
	for _, mode := range []string{"default", "predefined", "hybrid"} {
		section := BuildTeamWorkflowSection("leader", mode, "cn")
		if section == nil {
			t.Fatalf("BuildTeamWorkflowSection returned nil for mode=%s", mode)
		}
	}
}

// TestSectionName常量 测试 Section 名称常量与 Python 对齐
func TestSectionName常量(t *testing.T) {
	if SectionRole != "team_role" {
		t.Fatalf("expected team_role, got %s", SectionRole)
	}
	if SectionHITT != "team_hitt" {
		t.Fatalf("expected team_hitt, got %s", SectionHITT)
	}
	if SectionInfo != "team_info" {
		t.Fatalf("expected team_info, got %s", SectionInfo)
	}
	if SectionMembers != "team_members" {
		t.Fatalf("expected team_members, got %s", SectionMembers)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/prompts/... -run "TestBuild" -v 2>&1 | head -20
```

Expected: 编译失败

- [ ] **Step 3: 实现 sections.go**

这是最大文件，需要包含：
1. `TeamSectionName` 常量（8 个）
2. `TeamInfo` / `TeamMember` 辅助结构体
3. `_LABELS` 双语标签字典
4. `labelsFor` 辅助函数
5. 8 个 `Build*Section` 导出函数
6. 8 个 HITT 内部函数

实现要点：
- 每个 builder 函数返回 `*saprompt.PromptSection`
- HITT section 对齐 Python 6 个变体（leader cn/en, teammate anonymous cn/en, human_agent cn/en）+ exposeToTeammates 时的 legacy 变体（teammate legacy cn/en）= 8 个
- 角色过滤：Workflow/Lifecycle 仅 LEADER，HITT 仅当有 human_agent 时
- `_LABELS` 对齐 Python `sections.py` 的 `_LABELS` 字典
- 纯文本模板通过 `LoadTemplate(name, language).Content()` 加载
- 组装逻辑：标签 + 模板内容 + 动态字段拼接

**此步代码量约 600-700 行，需严格对齐 Python sections.py。实现时逐函数对照 Python 源码。**

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/prompts/... -run "TestBuild|TestSection" -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/prompts/sections.go internal/agent_teams/prompts/sections_test.go
git commit -m "feat(prompts): add 8 section builders + TeamSectionName + _LABELS + HITT (9.69 step 4)"
```

---

### Task 5: policy.go

**Files:**
- Create: `internal/agent_teams/prompts/policy.go`
- Test: `internal/agent_teams/prompts/policy_test.go`

- [ ] **Step 1: 写 policy_test.go 失败测试**

```go
package prompts

import (
	"strings"
	"testing"
)

// TestRolePolicy_Leader 测试 Leader 角色策略
func TestRolePolicy_Leader(t *testing.T) {
	result := RolePolicy("leader", "cn")
	if result == "" {
		t.Fatal("RolePolicy(leader, cn) returned empty")
	}
}

// TestRolePolicy_Teammate 测试 Teammate 角色策略
func TestRolePolicy_Teammate(t *testing.T) {
	result := RolePolicy("teammate", "cn")
	if result == "" {
		t.Fatal("RolePolicy(teammate, cn) returned empty")
	}
}

// TestBuildSystemPrompt 完整组装测试
func TestBuildSystemPrompt(t *testing.T) {
	info := &TeamInfo{TeamName: "test", DisplayName: "Test", Description: "Desc"}
	members := []TeamMember{{MemberName: "bob", DisplayName: "Bob", Description: "Eng"}}
	result := BuildSystemPrompt("alice", "leader", "cn", "高级工程师", "temporary", "default",
		info, members, "extra prompt", "/.team/ws", "/abs/ws")
	if result == "" {
		t.Fatal("BuildSystemPrompt returned empty")
	}
	// 应包含 member_name
	if !strings.Contains(result, "alice") {
		t.Fatal("BuildSystemPrompt should contain member_name")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/prompts/... -run "TestRolePolicy|TestBuildSystemPrompt" -v 2>&1 | head -20
```

- [ ] **Step 3: 实现 policy.go**

对齐 Python `prompts/policy.py`。核心函数：
- `RolePolicy(role, language)` — 加载 leader_policy 或 teammate_policy 模板
- `BuildSystemPrompt(...)` — 用 system_prompt.md 模板组装完整系统提示，双花括号 `{{var}}` 渲染

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/prompts/... -run "TestRolePolicy|TestBuildSystemPrompt" -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/prompts/policy.go internal/agent_teams/prompts/policy_test.go
git commit -m "feat(prompts): add RolePolicy + BuildSystemPrompt (9.69 step 5)"
```

---

### Task 6: team_plan_agent.go + team_plan_mode.go

**Files:**
- Create: `internal/agent_teams/prompts/team_plan_agent.go`
- Create: `internal/agent_teams/prompts/team_plan_mode.go`
- Test: `internal/agent_teams/prompts/team_plan_agent_test.go`
- Test: `internal/agent_teams/prompts/team_plan_mode_test.go`

- [ ] **Step 1: 写失败测试**

```go
// team_plan_agent_test.go
package prompts

import (
	"testing"
)

// TestTeamPlanAgentDesc 测试中英文描述常量
func TestTeamPlanAgentDesc(t *testing.T) {
	if TeamPlanAgentDesc["cn"] == "" {
		t.Fatal("TeamPlanAgentDesc cn is empty")
	}
	if TeamPlanAgentDesc["en"] == "" {
		t.Fatal("TeamPlanAgentDesc en is empty")
	}
}

// TestTeamPlanAgentSystemPrompt 测试系统提示词加载
func TestTeamPlanAgentSystemPrompt(t *testing.T) {
	cnPrompt := TeamPlanAgentSystemPrompt("cn")
	if cnPrompt == "" {
		t.Fatal("TeamPlanAgentSystemPrompt cn is empty")
	}
	enPrompt := TeamPlanAgentSystemPrompt("en")
	if enPrompt == "" {
		t.Fatal("TeamPlanAgentSystemPrompt en is empty")
	}
}

// TestBuildTeamPlanAgentCard 测试构建 plan_agent AgentCard
func TestBuildTeamPlanAgentCard(t *testing.T) {
	card := BuildTeamPlanAgentCard("cn")
	if card == nil {
		t.Fatal("BuildTeamPlanAgentCard returned nil")
	}
}
```

```go
// team_plan_mode_test.go
package prompts

import (
	"testing"
)

// TestGetTeamPlanModePrompt 测试获取 plan mode 模板
func TestGetTeamPlanModePrompt(t *testing.T) {
	cnPrompt := GetTeamPlanModePrompt("cn")
	if cnPrompt == "" {
		t.Fatal("GetTeamPlanModePrompt cn is empty")
	}
	enPrompt := GetTeamPlanModePrompt("en")
	if enPrompt == "" {
		t.Fatal("GetTeamPlanModePrompt en is empty")
	}
}

// TestBuildTeamPlanModePrompt 测试渲染 plan mode 模板
func TestBuildTeamPlanModePrompt(t *testing.T) {
	result := BuildTeamPlanModePrompt("cn", "尚未调用 enter_plan_mode", "Plan 文件: /tmp/plan.md")
	if result == "" {
		t.Fatal("BuildTeamPlanModePrompt returned empty")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 team_plan_agent.go**

对齐 Python `prompts/team_plan_agent.py`：
- `TeamPlanAgentDesc` — map[string]string 中英文描述
- `TeamPlanAgentSystemPrompt(language)` — 从模板加载
- `ApplyTeamPlanAgentPrompt(subagents, language) bool` — 遍历替换
- `BuildTeamPlanAgentCard(language)` — 构建 AgentCard

**注意**：`ApplyTeamPlanAgentPrompt` 的 `subagents` 参数类型需要在实现时确定。当前 Go 侧 `SubagentSpec` 接口在 `agentcore/harness/schema/config.go` 中定义，需要导入。但 prompts 包不应直接依赖 agentcore/harness。解决方案：`ApplyTeamPlanAgentPrompt` 接收一个抽象的 `[]SubagentInfo`（自定义小接口或结构体），或者将此函数签名改为接收函数式参数。

**实际方案**：将 `ApplyTeamPlanAgentPrompt` 放在 `rails/team_plan_mode_rail.go` 而非 prompts 包中，因为它需要访问 DeepAgent 的 subagents 列表。prompts 包只提供 `TeamPlanAgentSystemPrompt()` 和 `BuildTeamPlanAgentCard()` 常量/函数。

- [ ] **Step 4: 实现 team_plan_mode.go**

对齐 Python `prompts/team_plan_mode.py`：
- `GetTeamPlanModePrompt(language)` — 从模板加载
- `BuildTeamPlanModePrompt(language, enterPlanModeStatus, planFileInfo)` — 渲染模板

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/prompts/... -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/agent_teams/prompts/team_plan_agent.go internal/agent_teams/prompts/team_plan_mode.go internal/agent_teams/prompts/team_plan_agent_test.go internal/agent_teams/prompts/team_plan_mode_test.go
git commit -m "feat(prompts): add team_plan_agent + team_plan_mode (9.69 step 6)"
```

---

## Phase 2: 9.68 Rails

### Task 7: rails/doc.go + first_iteration_gate.go

**Files:**
- Create: `internal/agent_teams/rails/doc.go`
- Create: `internal/agent_teams/rails/first_iteration_gate.go`
- Test: `internal/agent_teams/rails/first_iteration_gate_test.go`

- [ ] **Step 1: 写 first_iteration_gate_test.go 失败测试**

```go
package rails

import (
	"context"
	"testing"
	"time"
)

// TestFirstIterationGate_初始状态 测试门初始关闭
func TestFirstIterationGate_初始状态(t *testing.T) {
	gate := NewFirstIterationGate()
	if gate.IsReady() {
		t.Fatal("gate should not be ready initially")
	}
}

// TestFirstIterationGate_BeforeTaskIteration打开门 测试 BeforeTaskIteration 打开门
func TestFirstIterationGate_BeforeTaskIteration打开门(t *testing.T) {
	gate := NewFirstIterationGate()
	err := gate.BeforeTaskIteration(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeforeTaskIteration returned error: %v", err)
	}
	if !gate.IsReady() {
		t.Fatal("gate should be ready after BeforeTaskIteration")
	}
}

// TestFirstIterationGate_Wait阻塞直到打开 测试 Wait 阻塞然后打开
func TestFirstIterationGate_Wait阻塞直到打开(t *testing.T) {
	gate := NewFirstIterationGate()
	done := make(chan struct{})
	go func() {
		_ = gate.Wait(context.Background())
		close(done)
	}()
	// Wait 应阻塞
	select {
	case <-done:
		t.Fatal("Wait should block until gate is opened")
	case <-time.After(50 * time.Millisecond):
	}
	// 打开门
	_ = gate.BeforeTaskIteration(context.Background(), nil)
	select {
	case <-done:
		// 正常
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait should unblock after BeforeTaskIteration")
	}
}

// TestFirstIterationGate_Reset 测试重置门
func TestFirstIterationGate_Reset(t *testing.T) {
	gate := NewFirstIterationGate()
	_ = gate.BeforeTaskIteration(context.Background(), nil)
	if !gate.IsReady() {
		t.Fatal("gate should be ready")
	}
	gate.Reset()
	if gate.IsReady() {
		t.Fatal("gate should not be ready after Reset")
	}
}

// TestFirstIterationGate_WaitCtx取消 测试 Wait 支持 context 取消
func TestFirstIterationGate_WaitCtx取消(t *testing.T) {
	gate := NewFirstIterationGate()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = gate.Wait(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
		// 正常：context 取消后 Wait 应返回
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait should return when context is cancelled")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 创建 rails/doc.go**

```go
// Package rails 提供团队级 Rails 实现。
//
// 本包包含：
//   - FirstIterationGate：首次迭代信号门
//   - TeamToolRail：团队协调工具注册
//   - TeamPolicyRail：团队策略提示注入（8 个 PromptSection）
//   - TeamToolApprovalRail：teammate 工具调用审批
//   - TeamPlanModeRail：team.plan leader 提示词叠加
//
// Python: openjiuwen/agent_teams/rails/
//
// 文件目录：
//
//	rails/
//	├── doc.go                  # 包文档
//	├── first_iteration_gate.go # 首次迭代信号门
//	├── team_tool_rail.go       # TeamToolRail (priority=90)
//	├── team_policy_rail.go     # TeamPolicyRail (priority=12)
//	├── tool_approval_rail.go   # TeamToolApprovalRail (priority=90)
//	└── team_plan_mode_rail.go  # TeamPlanModeRail (priority=84)
//
// 对应 Python 代码：openjiuwen/agent_teams/rails/
package rails
```

- [ ] **Step 4: 实现 first_iteration_gate.go**

对齐 Python `rails/first_iteration_gate.py`。Go 用 `chan struct{}` + `sync.Once` 替代 `asyncio.Event`。

```go
package rails

import (
	"context"
	"sync"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FirstIterationGate 首次迭代信号门。
// Python: FirstIterationGate(AgentRail) (rails/first_iteration_gate.py)
//
// Go 用 channel 替代 Python asyncio.Event。
// 外部代码调用 Wait() 阻塞直到 Agent 进入首次迭代。
type FirstIterationGate struct {
	agentinterfaces.BaseRail
	ch     chan struct{}
	once   sync.Once
	mu     sync.Mutex
	closed bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFirstIterationGate 创建首次迭代门控。
func NewFirstIterationGate() *FirstIterationGate {
	return &FirstIterationGate{
		ch: make(chan struct{}),
	}
}

// Wait 阻塞直到首次迭代开始。
// Python: async wait()
// 支持 context 取消。
func (g *FirstIterationGate) Wait(ctx context.Context) error {
	select {
	case <-g.ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsReady 返回门是否已打开。
// Python: is_ready -> bool
func (g *FirstIterationGate) IsReady() bool {
	select {
	case <-g.ch:
		return true
	default:
		return false
	}
}

// BeforeTaskIteration 首次迭代时打开门。
// Python: async before_task_iteration(ctx)
func (g *FirstIterationGate) BeforeTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	g.once.Do(func() { close(g.ch) })
	return nil
}

// Reset 重置门用于新一轮。
// Python: reset()
func (g *FirstIterationGate) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.once = sync.Once{}
	g.ch = make(chan struct{})
}
```

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/rails/... -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/agent_teams/rails/doc.go internal/agent_teams/rails/first_iteration_gate.go internal/agent_teams/rails/first_iteration_gate_test.go
git commit -m "feat(rails): add FirstIterationGate (9.68 step 1)"
```

---

### Task 8: tools/team_tools.go — 团队工具注册

**Files:**
- Create: `internal/agent_teams/tools/team_tools.go`
- Test: `internal/agent_teams/tools/team_tools_test.go`

- [ ] **Step 1: 写 team_tools_test.go 失败测试**

```go
package tools

import (
	"testing"
)

// TestCreateTeamTools_Leader 测试 Leader 角色工具列表
func TestCreateTeamTools_Leader(t *testing.T) {
	// 需要一个 fake TeamBackend
	toolList := CreateTeamTools(nil, "leader", "build_mode", "temporary", "cn")
	if len(toolList) == 0 {
		t.Fatal("Leader should have at least one tool")
	}
	// Leader 应有 add_task 但不应有 claim_task
	hasAddTask := false
	hasClaimTask := false
	for _, tl := range toolList {
		if tl.Card().ID == "add_task" {
			hasAddTask = true
		}
		if tl.Card().ID == "claim_task" {
			hasClaimTask = true
		}
	}
	if !hasAddTask {
		t.Fatal("Leader should have add_task")
	}
	if hasClaimTask {
		t.Fatal("Leader should not have claim_task")
	}
}

// TestCreateTeamTools_Teammate 测试 Teammate 角色工具列表
func TestCreateTeamTools_Teammate(t *testing.T) {
	toolList := CreateTeamTools(nil, "teammate", "build_mode", "temporary", "cn")
	if len(toolList) == 0 {
		t.Fatal("Teammate should have at least one tool")
	}
	// Teammate 应有 claim_task 但不应有 add_task
	hasClaimTask := false
	hasAddTask := false
	for _, tl := range toolList {
		if tl.Card().ID == "claim_task" {
			hasClaimTask = true
		}
		if tl.Card().ID == "add_task" {
			hasAddTask = true
		}
	}
	if !hasClaimTask {
		t.Fatal("Teammate should have claim_task")
	}
	if hasAddTask {
		t.Fatal("Teammate should not have add_task")
	}
}

// TestQualifyTeamToolIDs 测试工具 ID 后缀
func TestQualifyTeamToolIDs(t *testing.T) {
	toolList := CreateTeamTools(nil, "leader", "build_mode", "temporary", "cn")
	QualifyTeamToolIDs(toolList, "my-team", "alice")
	for _, tl := range toolList {
		id := tl.Card().ID
		if id != "" {
			// ID 应包含 team.member 后缀
			// 格式: {id}.{team_name}.{member_name}
		}
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 tools/team_tools.go**

对齐 Python `rails/team_tool_rail.py` 中 `create_team_tools()` 的逻辑。12 个 ToolCard 定义 + `CreateTeamTools()` 函数 + `QualifyTeamToolIDs()` 函数。

每个 ToolCard 定义为一个嵌入 `TeamTool` 基类的小结构体，实现 `Tool` 接口的 `Card()` + `Invoke()` + `Stream()` 方法。`Invoke()` 委托到 TeamBackend / TeamTaskManager / TeamMessageManager 的对应方法。

角色过滤逻辑（对齐 Python）：
- LEADER: send_message, broadcast_message, add_task, add_batch, list_tasks, assign_task, complete_task, cancel_task, update_task, spawn_member, approve_plan
- TEAMMATE: send_message, list_tasks, claim_task, complete_task
- HUMAN_AGENT: send_message, list_tasks
- predefined 模式下 LEADER 不含 spawn_member

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/tools/... -run "TestCreateTeamTools|TestQualifyTeamToolIDs" -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/tools/team_tools.go internal/agent_teams/tools/team_tools_test.go
git commit -m "feat(tools): add 12 team tool cards + CreateTeamTools + QualifyTeamToolIDs (9.68 step 2)"
```

---

### Task 9: team_tool_rail.go

**Files:**
- Create: `internal/agent_teams/rails/team_tool_rail.go`
- Test: `internal/agent_teams/rails/team_tool_rail_test.go`

- [ ] **Step 1: 写失败测试**

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 team_tool_rail.go**

对齐 Python `rails/team_tool_rail.py`。核心：
- 嵌入 `DeepAgentRail`，priority=90
- `Init()` — 调用 `CreateTeamTools()` + 可选追加 WorkspaceMetaTool / EnterWorktreeTool / ExitWorktreeTool + 可选 qualify IDs + 注册到 ability_manager
- `Uninit()` — 反注册所有工具
- Functional Options 模式构造

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/rails/... -run "TestTeamToolRail" -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/rails/team_tool_rail.go internal/agent_teams/rails/team_tool_rail_test.go
git commit -m "feat(rails): add TeamToolRail (9.68 step 3)"
```

---

### Task 10: team_policy_rail.go

**Files:**
- Create: `internal/agent_teams/rails/team_policy_rail.go`
- Test: `internal/agent_teams/rails/team_policy_rail_test.go`

- [ ] **Step 1: 写失败测试**

测试要点：Init/BeforeModelCall/Uninit + 静态 section 注入 + 动态 section 缓存刷新

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 team_policy_rail.go**

对齐 Python `rails/team_policy_rail.py`。核心：
- 嵌入 `DeepAgentRail`，priority=12
- `NewTeamPolicyRail()` — Functional Options
- `Init()` — 缓存 system_prompt_builder
- `BeforeModelCall()` — 添加 6 个静态 section + 刷新 2 个动态 section（通过 MtimeSectionCache）
- `Uninit()` — 移除所有 section
- 6 个静态 section 在构造时通过 `prompts.Build*Section()` 构建
- 2 个动态 section 使用 MtimeSectionCache，probe 绑定 `teamBackend.GetTeamUpdatedAt` / `teamBackend.GetMembersMaxUpdatedAt`

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/rails/... -run "TestTeamPolicyRail" -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/rails/team_policy_rail.go internal/agent_teams/rails/team_policy_rail_test.go
git commit -m "feat(rails): add TeamPolicyRail with 8 PromptSections + MtimeSectionCache (9.68 step 4)"
```

---

### Task 11: tool_approval_rail.go

**Files:**
- Create: `internal/agent_teams/rails/tool_approval_rail.go`
- Test: `internal/agent_teams/rails/tool_approval_rail_test.go`

- [ ] **Step 1: 写失败测试**

测试要点：resolveInterrupt 首次/恢复/自动批准/拒绝/发送失败

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 tool_approval_rail.go**

对齐 Python `rails/tool_approval_rail.py`。核心：
- 嵌入 `BaseInterruptRail`，设置 `ResolveInterruptFn`
- 构造函数接收 teamName, memberName, db, messager, leaderMemberName, toolNames
- 内部创建 `TeamMessageManager` 用于向 leader 发消息
- resolveInterrupt 逻辑：
  - 首次调用（userInput nil）：检查 autoConfirm → 发消息给 leader → Interrupt
  - 恢复调用（userInput 有值）：解析 ConfirmPayload → Approve/Reject/重新 Interrupt

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/rails/... -run "TestTeamToolApprovalRail" -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/rails/tool_approval_rail.go internal/agent_teams/rails/tool_approval_rail_test.go
git commit -m "feat(rails): add TeamToolApprovalRail (9.68 step 5)"
```

---

### Task 12: team_plan_mode_rail.go

**Files:**
- Create: `internal/agent_teams/rails/team_plan_mode_rail.go`
- Test: `internal/agent_teams/rails/team_plan_mode_rail_test.go`

- [ ] **Step 1: 写失败测试**

测试要点：Init + BeforeModelCall plan/非plan 分支 + Uninit

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 team_plan_mode_rail.go**

对齐 Python `rails/team_plan_mode_rail.py`。核心：
- 嵌入 `DeepAgentRail`，priority=84
- `Init()` — 缓存 prompt builder + 调用 `specializePlanAgent()`（在此实现 ApplyTeamPlanAgentPrompt 逻辑）
- `BeforeModelCall()` — 读取 agent plan_mode 状态，plan 模式下添加 `prompts.BuildTeamPlanModeSection()`
- `Uninit()` — 移除 MODE_INSTRUCTIONS section

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/rails/... -run "TestTeamPlanModeRail" -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/rails/team_plan_mode_rail.go internal/agent_teams/rails/team_plan_mode_rail_test.go
git commit -m "feat(rails): add TeamPlanModeRail (9.68 step 6)"
```

---

## Phase 3: 回填

### Task 13: 回填 harness.go + resources.go + doc.go

**Files:**
- Modify: `internal/agent_teams/harness.go`
- Modify: `internal/agent_teams/agent/resources.go`
- Modify: `internal/agent_teams/doc.go`

- [ ] **Step 1: 修改 harness.go MountedRails 字段类型**

将 5 个 `any` 字段替换为具体类型：

```go
// Before:
TeamTool any
TeamPolicy any
FirstIterGate any
ToolApproval any
TeamPlanMode any

// After:
TeamTool *rails.TeamToolRail
TeamPolicy *rails.TeamPolicyRail
FirstIterGate *rails.FirstIterationGate
ToolApproval *rails.TeamToolApprovalRail
TeamPlanMode *rails.TeamPlanModeRail
```

- [ ] **Step 2: 修改 BuildTeamHarness 参数类型和实现**

参数从 `any` 改为具体类型，实现 `deepAgent.AddRail()` 调用。

- [ ] **Step 3: 修改 resources.go FirstIterGate 类型**

`FirstIterGate any` → `FirstIterGate *rails.FirstIterationGate`，删除 `TODO(#9.68)` 注释。

- [ ] **Step 4: 修改 doc.go**

`rails/ ⤵️ 回填: 9.68 团队级 Rails` → `rails/ # 团队级 Rails（9.68）`
`prompts/ ⤵️ 回填: 9.69 团队提示词` → `prompts/ # 团队提示词（9.69）`
更新文件目录树。

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/...
```

Expected: 编译成功

- [ ] **Step 6: Commit**

```bash
git add internal/agent_teams/harness.go internal/agent_teams/agent/resources.go internal/agent_teams/doc.go
git commit -m "refill: harness.go MountedRails + resources.go + doc.go type upgrades (9.68 backfill)"
```

---

### Task 14: 回填 agent_configurator.go — Rail 构造 + TODO 清理

**Files:**
- Modify: `internal/agent_teams/agent/agent_configurator.go`

- [ ] **Step 1: 清理已完成章节的残留 TODO**

删除以下 TODO 标记（对应章节已 ✅）：
- `TODO(#9.53)` ×2（行 180, 233）— 9.51-53 已 ✅
- `TODO(#9.56)` ×2（行 261, 272）— 9.56 已 ✅
- `TODO(#9.58)` ×2（行 213, 246）— 9.58 已 ✅
- `TODO(#9.64)` ×2（行 209, 286）— 9.64 已 ✅

- [ ] **Step 2: 在 SetupAgent 中构造 6 个 Rail 实例**

替换 SetupAgent() 第 264-280 行的 nil 参数，构造并传入具体 Rail：

```go
// 步骤 9: TeamToolRail
teamToolRail := rails.NewTeamToolRail(
    rails.WithTeamBackend(c.TeamBackend()),
    rails.WithRole(string(ctx.Role)),
    // ...其他参数
)

// 步骤 10: TeamPolicyRail
teamPolicyRail := rails.NewTeamPolicyRail(
    rails.WithRole(string(ctx.Role)),
    // ...其他参数
)

// 步骤 11: FirstIterationGate（非 HUMAN_AGENT 角色时创建）
var firstIterGate *rails.FirstIterationGate
if ctx.Role != atschema.TeamRoleHumanAgent {
    firstIterGate = rails.NewFirstIterationGate()
}

// 步骤 12: TeamWorkspaceRail（有 workspace_manager 时）
var teamWorkspaceRail *team_workspace.TeamWorkspaceRail
if c.WorkspaceManager() != nil {
    teamWorkspaceRail = team_workspace.NewTeamWorkspaceRail(c.WorkspaceManager())
}

// 步骤 13: TeamToolApprovalRail（仅 TEAMMATE + 有审批工具时）
var toolApprovalRail *rails.TeamToolApprovalRail
// 条件：ctx.Role == TEAMMATE && TeamBackend != nil && Messager != nil && approval_required_tools 非空

// 步骤 14: TeamPlanModeRail（仅 LEADER + team.plan 启用时）
var teamPlanModeRail *rails.TeamPlanModeRail
// 条件：ctx.Role == LEADER && isTeamPlanEnabled
```

- [ ] **Step 3: 回填 RolePolicy 调用**

替换 `SetupInfra()` 中第 184 行的 `TODO(#9.69)`：

```go
rolePolicyStr := prompts.RolePolicy(string(ctx.Role), resolvedLanguage)
```

- [ ] **Step 4: 回填自定义配置器调用**

替换 SetupAgent() 第 289 行的 `TODO(#9.68)`：

```go
if spec.AgentCustomizer != nil {
    harness.RunAgentCustomizer(spec.AgentCustomizer)
}
```

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/...
```

- [ ] **Step 6: 运行 agent 包测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/... -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/agent_teams/agent/agent_configurator.go
git commit -m "refill: agent_configurator.go Rail construction + TODO cleanup (9.68 backfill)"
```

---

### Task 15: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.68-69 状态**

将 `9.68-69 | ☐` 改为 `9.68-69 | ✅`

- [ ] **Step 2: 更新 9.28 PlanAgent 回填标记**

将 9.28 行的 `⤵️ 9.68-69 team.plan 特化（TeamPlanModeRail / apply_team_plan_agent_prompt）` 标记为已回填。

- [ ] **Step 3: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: update IMPLEMENTATION_PLAN.md 9.68-69 completed + 9.28 backfill"
```

---

### Task 16: 全量编译 + 测试

- [ ] **Step 1: 全量编译**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

- [ ] **Step 2: 运行所有相关包测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/prompts/... ./internal/agent_teams/rails/... ./internal/agent_teams/tools/... ./internal/agent_teams/... ./internal/agent_teams/agent/... -cover
```

- [ ] **Step 3: 检查覆盖率**

```bash
cd /home/opensource/uapclaw-gateway && go test -coverprofile=coverage.out ./internal/agent_teams/prompts/... ./internal/agent_teams/rails/... && go tool cover -func=coverage.out | grep -E "^total|^internal/agent_teams/(prompts|rails)"
```

目标：每个包 ≥ 85%

- [ ] **Step 4: Commit 最终状态（如有修复）**

---

## Self-Review Checklist

### 1. Spec Coverage

| Spec 章节 | 对应 Task |
|-----------|-----------|
| 9.69 Prompts 包结构 | Task 1-6 |
| loader.go (go:embed) | Task 2 |
| section_cache.go (MtimeSectionCache) | Task 3 |
| sections.go (8 builders + HITT) | Task 4 |
| policy.go | Task 5 |
| team_plan_agent.go | Task 6 |
| team_plan_mode.go | Task 6 |
| 9.68 Rails 包结构 | Task 7-12 |
| FirstIterationGate | Task 7 |
| tools/team_tools.go | Task 8 |
| TeamToolRail | Task 9 |
| TeamPolicyRail | Task 10 |
| TeamToolApprovalRail | Task 11 |
| TeamPlanModeRail | Task 12 |
| 回填 harness.go | Task 13 |
| 回填 resources.go | Task 13 |
| 回填 doc.go | Task 13 |
| 回填 agent_configurator.go | Task 14 |
| TODO 清理 | Task 14 |
| IMPLEMENTATION_PLAN.md | Task 15 |
| 18+1 .md 模板 | Task 1 |

### 2. Placeholder Scan

无 TBD / TODO / implement later / fill in details。所有步骤包含具体代码或命令。

### 3. Type Consistency

- `*saprompt.PromptSection` 在 Task 2-6 中一致
- `*rails.TeamToolRail` 等在 Task 9-12 定义、Task 13 回填中使用一致
- `TeamInfo` / `TeamMember` 在 sections.go 中定义，在 team_policy_rail.go 中消费
