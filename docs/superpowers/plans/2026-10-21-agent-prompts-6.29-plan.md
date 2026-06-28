# 6.29 Agent Prompts 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 PromptSection 和 SystemPromptBuilder 从 agents 包完全迁移到独立 prompts/ 子包，补齐所有缺失方法，对齐 Python 行为。

**Architecture:** 创建 `single_agent/prompts/` 包，将类型定义和方法从 `agents` 包迁移过去。agents 包改为导入 prompts 包。AddPromptBuilderSection 签名从 `map[string]string` 改为 `string` 并增加空内容移除逻辑。SystemPromptBuilder 增加 sectionsFilter 函数字段支持扩展。

**Tech Stack:** Go 1.22+, testify/assert, Go 标准 sort/strings 包

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `single_agent/prompts/doc.go` | Create | 包文档 |
| `single_agent/prompts/builder.go` | Create | PromptSection + SystemPromptBuilder + 常量 + 全部方法 |
| `single_agent/prompts/builder_test.go` | Create | prompts 包单元测试 |
| `single_agent/agents/react_agent.go` | Modify | 删除迁移的类型/构造函数，新增 prompts 导入，补齐 identity 常量 |
| `single_agent/agents/react_prompt.go` | Modify | 删除迁移的方法，AddPromptBuilderSection 签名+逻辑对齐 Python |
| `single_agent/agents/react_invoke.go:375` | Modify | AddPromptBuilderSection 调用适配新签名 |
| `single_agent/agents/doc.go` | Modify | 更新文件目录描述 |
| `single_agent/agents/react_agent_test.go` | Modify | 删除迁移的测试，适配新签名，增加空内容移除测试 |

---

### Task 1: 创建 prompts 包核心类型和方法

**Files:**
- Create: `internal/agentcore/single_agent/prompts/builder.go`
- Create: `internal/agentcore/single_agent/prompts/doc.go`
- Test: `internal/agentcore/single_agent/prompts/builder_test.go`

- [ ] **Step 1: 创建 builder.go**

```go
package prompts

import (
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptSection 系统提示词的单一节，支持多语言内容。
//
// 对应 Python: PromptSection (openjiuwen/core/single_agent/prompts/builder.py)
type PromptSection struct {
	// Name 节名称（同名称覆盖）
	Name string
	// Content 多语言内容映射：language → content
	Content map[string]string
	// Priority 优先级（数值越小越靠前）
	Priority int
}

// SystemPromptBuilder 基于节的系统提示词构建器。
//
// 本类仅提供通用的节注册和渲染功能。
// Agent 簇特定的提示词策略（如模式切换或提示词诊断）
// 应通过 sectionsFilter 函数字段在外部实现。
//
// 对应 Python: SystemPromptBuilder (openjiuwen/core/single_agent/prompts/builder.py)
type SystemPromptBuilder struct {
	// Language 当前语言（默认 "cn"）
	Language string
	// sectionsFilter 节过滤钩子，Build 时调用。nil 表示不过滤。
	// 用于 harness 层的 PromptMode（FULL/MINIMAL/NONE）过滤。
	sectionsFilter func([]PromptSection) []PromptSection
	// sections 已注册的节映射：name → PromptSection
	sections map[string]PromptSection
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SupportedLanguages 支持的语言列表
	SupportedLanguages = [2]string{"cn", "en"}
	// DefaultLanguage 默认提示词语言
	DefaultLanguage = "cn"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSystemPromptBuilder 创建系统提示词构建器（默认语言 "cn"，无过滤）。
func NewSystemPromptBuilder() *SystemPromptBuilder {
	return &SystemPromptBuilder{
		Language: DefaultLanguage,
		sections: make(map[string]PromptSection),
	}
}

// NewSystemPromptBuilderWithFilter 创建带自定义语言和过滤函数的构建器。
func NewSystemPromptBuilderWithFilter(lang string, filter func([]PromptSection) []PromptSection) *SystemPromptBuilder {
	return &SystemPromptBuilder{
		Language:       lang,
		sectionsFilter: filter,
		sections:       make(map[string]PromptSection),
	}
}

// NewPromptSection 创建提示节。
func NewPromptSection(name string, content map[string]string, priority int) PromptSection {
	return PromptSection{
		Name:     name,
		Content:  content,
		Priority: priority,
	}
}

// AddSection 添加或替换节（链式调用）。
//
// 对应 Python: SystemPromptBuilder.add_section(section)
func (b *SystemPromptBuilder) AddSection(section PromptSection) *SystemPromptBuilder {
	b.sections[section.Name] = section
	return b
}

// RemoveSection 移除指定名称的节（链式调用）。
//
// 对应 Python: SystemPromptBuilder.remove_section(name)
func (b *SystemPromptBuilder) RemoveSection(name string) *SystemPromptBuilder {
	delete(b.sections, name)
	return b
}

// GetAllSections 返回所有注册节的副本。
//
// 对应 Python: SystemPromptBuilder.get_all_sections()
func (b *SystemPromptBuilder) GetAllSections() map[string]PromptSection {
	result := make(map[string]PromptSection, len(b.sections))
	for k, v := range b.sections {
		result[k] = v
	}
	return result
}

// HasSection 检查节是否存在。
//
// 对应 Python: SystemPromptBuilder.has_section(name)
func (b *SystemPromptBuilder) HasSection(name string) bool {
	_, ok := b.sections[name]
	return ok
}

// GetSection 按名称获取单个节，不存在返回 nil。
//
// 对应 Python: SystemPromptBuilder.get_section(name)
func (b *SystemPromptBuilder) GetSection(name string) *PromptSection {
	if s, ok := b.sections[name]; ok {
		return &s
	}
	return nil
}

// Build 按优先级排序并拼接为完整系统提示词。
//
// 安全多次调用，每次从当前所有注册节生成完整提示词。
//
// 对应 Python: SystemPromptBuilder.build()
func (b *SystemPromptBuilder) Build() string {
	sections := b.getSectionsForBuild()
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].Priority < sections[j].Priority
	})

	parts := make([]string, 0, len(sections))
	for _, s := range sections {
		if content := s.Render(b.Language); content != "" {
			parts = append(parts, content)
		}
	}

	return strings.Join(parts, "\n\n")
}

// Render 渲染指定语言的内容。
//
// 回退顺序：精确匹配 → DefaultLanguage → map 中首个值 → 空字符串。
//
// 对应 Python: PromptSection.render(language)
func (s *PromptSection) Render(language string) string {
	if content, ok := s.Content[language]; ok {
		return content
	}
	if content, ok := s.Content[DefaultLanguage]; ok {
		return content
	}
	for _, v := range s.Content {
		return v
	}
	return ""
}

// CharCount 返回指定语言渲染后的字符数。
//
// 对应 Python: PromptSection.char_count(language)
func (s *PromptSection) CharCount(language string) int {
	return len(s.Render(language))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getSectionsForBuild 返回参与 Build 的节列表，应用 sectionsFilter 过滤。
//
// 对应 Python: SystemPromptBuilder._get_sections_for_build()
func (b *SystemPromptBuilder) getSectionsForBuild() []PromptSection {
	all := make([]PromptSection, 0, len(b.sections))
	for _, s := range b.sections {
		all = append(all, s)
	}
	if b.sectionsFilter != nil {
		return b.sectionsFilter(all)
	}
	return all
}
```

- [ ] **Step 2: 创建 doc.go**

```go
// Package prompts 提供基于节的系统提示词构建器，支持多语言内容和优先级排序。
//
// SystemPromptBuilder 将多个命名的、带优先级的、多语言的提示词片段（PromptSection）
// 组装为完整的系统提示词，供 LLM 调用使用。节按 Priority 升序排列，
// 按 Language 渲染，用 "\n\n" 拼接。
//
// 扩展机制：sectionsFilter 函数字段允许调用方在 Build 时过滤节，
// 例如 harness 层的 PromptMode（FULL/MINIMAL/NONE）过滤。
//
// 文件目录：
//
//	prompts/
//	├── doc.go           # 包文档
//	├── builder.go       # PromptSection + SystemPromptBuilder + 常量
//	└── builder_test.go  # 单元测试
//
// 对应 Python 代码：openjiuwen/core/single_agent/prompts/
package prompts
```

- [ ] **Step 3: 创建 builder_test.go**

```go
//go:build test

package prompts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSupportedLanguages 验证支持的语言列表
func TestSupportedLanguages(t *testing.T) {
	assert.Equal(t, [2]string{"cn", "en"}, SupportedLanguages)
}

// TestDefaultLanguage 验证默认语言
func TestDefaultLanguage(t *testing.T) {
	assert.Equal(t, "cn", DefaultLanguage)
}

// TestNewSystemPromptBuilder 验证系统提示词构建器构造
func TestNewSystemPromptBuilder(t *testing.T) {
	builder := NewSystemPromptBuilder()
	assert.NotNil(t, builder)
	assert.Equal(t, "cn", builder.Language)
	assert.NotNil(t, builder.sections)
	assert.Nil(t, builder.sectionsFilter)
}

// TestNewSystemPromptBuilderWithFilter 验证带过滤函数的构建器构造
func TestNewSystemPromptBuilderWithFilter(t *testing.T) {
	filter := func(sections []PromptSection) []PromptSection {
		return sections
	}
	builder := NewSystemPromptBuilderWithFilter("en", filter)
	assert.Equal(t, "en", builder.Language)
	assert.NotNil(t, builder.sectionsFilter)
}

// TestNewPromptSection 验证提示节构造
func TestNewPromptSection(t *testing.T) {
	section := NewPromptSection("test", map[string]string{"cn": "测试"}, 10)
	assert.Equal(t, "test", section.Name)
	assert.Equal(t, 10, section.Priority)
	assert.Equal(t, "测试", section.Content["cn"])
}

// TestPromptSection_Render 验证提示节渲染
func TestPromptSection_Render(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"cn": "中文", "en": "English"},
		Priority: 10,
	}

	// 匹配指定语言
	assert.Equal(t, "中文", section.Render("cn"))
	assert.Equal(t, "English", section.Render("en"))

	// 回退到默认语言
	assert.Equal(t, "中文", section.Render("fr"))

	// 空内容
	emptySection := PromptSection{Name: "empty", Content: map[string]string{}}
	assert.Equal(t, "", emptySection.Render("cn"))
}

// TestPromptSection_Render_回退到默认语言 验证无精确匹配时回退到 DefaultLanguage
func TestPromptSection_Render_回退到默认语言(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"cn": "中文", "en": "English"},
		Priority: 10,
	}
	assert.Equal(t, "中文", section.Render("fr"))
}

// TestPromptSection_Render_回退到首个值 验证无默认语言时回退到 map 中首个值
func TestPromptSection_Render_回退到首个值(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"en": "English"},
		Priority: 10,
	}
	assert.Equal(t, "English", section.Render("fr"))
}

// TestPromptSection_Render_空内容 验证空 Content 时返回空字符串
func TestPromptSection_Render_空内容(t *testing.T) {
	section := PromptSection{Name: "empty", Content: map[string]string{}}
	assert.Equal(t, "", section.Render("cn"))
}

// TestPromptSection_CharCount 验证字符数计算
func TestPromptSection_CharCount(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"cn": "中文内容"},
		Priority: 10,
	}
	assert.Equal(t, 4, section.CharCount("cn"))
}

// TestPromptSection_CharCount_多语言 验证不同语言字符数不同
func TestPromptSection_CharCount_多语言(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"cn": "中文", "en": "English"},
		Priority: 10,
	}
	assert.Equal(t, 2, section.CharCount("cn"))
	assert.Equal(t, 7, section.CharCount("en"))
}

// TestSystemPromptBuilder_AddSection 验证添加节
func TestSystemPromptBuilder_AddSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 20})
	builder.AddSection(PromptSection{Name: "b", Content: map[string]string{"cn": "BBB"}, Priority: 10})

	assert.True(t, builder.HasSection("a"))
	assert.True(t, builder.HasSection("b"))
}

// TestSystemPromptBuilder_AddSection_同名称覆盖 验证同名称节覆盖
func TestSystemPromptBuilder_AddSection_同名称覆盖(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "BBB"}, Priority: 20})
	assert.Equal(t, "BBB", builder.Build())
}

// TestSystemPromptBuilder_AddSection_链式调用 验证 AddSection 返回自身支持链式调用
func TestSystemPromptBuilder_AddSection_链式调用(t *testing.T) {
	builder := NewSystemPromptBuilder()
	result := builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	assert.Equal(t, builder, result)
}

// TestSystemPromptBuilder_RemoveSection 验证移除节
func TestSystemPromptBuilder_RemoveSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	assert.True(t, builder.HasSection("a"))

	builder.RemoveSection("a")
	assert.False(t, builder.HasSection("a"))
}

// TestSystemPromptBuilder_RemoveSection_不存在时不报错 验证移除不存在的节不 panic
func TestSystemPromptBuilder_RemoveSection_不存在时不报错(t *testing.T) {
	builder := NewSystemPromptBuilder()
	result := builder.RemoveSection("nonexistent")
	assert.Equal(t, builder, result)
}

// TestSystemPromptBuilder_GetAllSections 验证返回所有节的副本
func TestSystemPromptBuilder_GetAllSections(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	builder.AddSection(PromptSection{Name: "b", Content: map[string]string{"cn": "BBB"}, Priority: 20})

	sections := builder.GetAllSections()
	assert.Equal(t, 2, len(sections))
	assert.Contains(t, sections, "a")
	assert.Contains(t, sections, "b")
}

// TestSystemPromptBuilder_GetAllSections_副本隔离 验证修改返回值不影响原 builder
func TestSystemPromptBuilder_GetAllSections_副本隔离(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})

	sections := builder.GetAllSections()
	sections["a"] = PromptSection{Name: "a", Content: map[string]string{"cn": "MODIFIED"}, Priority: 99}

	// 原 builder 不受影响
	original := builder.GetAllSections()
	assert.Equal(t, 10, original["a"].Priority)
	assert.Equal(t, "AAA", original["a"].Content["cn"])
}

// TestSystemPromptBuilder_GetSection 验证按名称获取节
func TestSystemPromptBuilder_GetSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})

	section := builder.GetSection("a")
	assert.NotNil(t, section)
	assert.Equal(t, "a", section.Name)
	assert.Equal(t, "AAA", section.Content["cn"])
}

// TestSystemPromptBuilder_GetSection_不存在 验证不存在返回 nil
func TestSystemPromptBuilder_GetSection_不存在(t *testing.T) {
	builder := NewSystemPromptBuilder()
	section := builder.GetSection("nonexistent")
	assert.Nil(t, section)
}

// TestSystemPromptBuilder_HasSection 验证节存在检查
func TestSystemPromptBuilder_HasSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})

	assert.True(t, builder.HasSection("a"))
	assert.False(t, builder.HasSection("nonexistent"))
}

// TestSystemPromptBuilder_Build 验证构建结果按优先级排序
func TestSystemPromptBuilder_Build(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "low", Content: map[string]string{"cn": "LOW"}, Priority: 30})
	builder.AddSection(PromptSection{Name: "mid", Content: map[string]string{"cn": "MID"}, Priority: 20})
	builder.AddSection(PromptSection{Name: "high", Content: map[string]string{"cn": "HIGH"}, Priority: 10})

	result := builder.Build()
	assert.Equal(t, "HIGH\n\nMID\n\nLOW", result)
}

// TestSystemPromptBuilder_Build_跳过空内容 验证空内容节被跳过
func TestSystemPromptBuilder_Build_跳过空内容(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "empty", Content: map[string]string{}, Priority: 10})
	builder.AddSection(PromptSection{Name: "nonempty", Content: map[string]string{"cn": "VALUE"}, Priority: 20})
	assert.Equal(t, "VALUE", builder.Build())
}

// TestSystemPromptBuilder_Build_无节时返回空字符串 验证空构建器返回空字符串
func TestSystemPromptBuilder_Build_无节时返回空字符串(t *testing.T) {
	builder := NewSystemPromptBuilder()
	assert.Equal(t, "", builder.Build())
}

// TestSystemPromptBuilder_Build_单节 验证单节构建不添加换行
func TestSystemPromptBuilder_Build_单节(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "only", Content: map[string]string{"cn": "ONLY"}, Priority: 10})
	assert.Equal(t, "ONLY", builder.Build())
}

// TestSystemPromptBuilder_Build_过滤钩子 验证 sectionsFilter 过滤节
func TestSystemPromptBuilder_Build_过滤钩子(t *testing.T) {
	filter := func(sections []PromptSection) []PromptSection {
		result := make([]PromptSection, 0)
		for _, s := range sections {
			if s.Name == "a" {
				result = append(result, s)
			}
		}
		return result
	}
	builder := NewSystemPromptBuilderWithFilter("cn", filter)
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	builder.AddSection(PromptSection{Name: "b", Content: map[string]string{"cn": "BBB"}, Priority: 20})

	result := builder.Build()
	assert.Equal(t, "AAA", result)
}

// TestSystemPromptBuilder_Build_过滤钩子为nil 验证 sectionsFilter 为 nil 时不过滤
func TestSystemPromptBuilder_Build_过滤钩子为nil(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	builder.AddSection(PromptSection{Name: "b", Content: map[string]string{"cn": "BBB"}, Priority: 20})

	result := builder.Build()
	assert.Equal(t, "AAA\n\nBBB", result)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 运行 prompts 包测试验证通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -tags=test -v ./internal/agentcore/single_agent/prompts/...`
Expected: PASS，所有测试通过

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/single_agent/prompts/doc.go internal/agentcore/single_agent/prompts/builder.go internal/agentcore/single_agent/prompts/builder_test.go
git commit -m "feat(6.29): 创建 prompts 包 — PromptSection + SystemPromptBuilder + 全部方法"
```

---

### Task 2: 修改 agents 包 — 删除迁移的类型和方法

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_agent.go`
- Modify: `internal/agentcore/single_agent/agents/react_prompt.go`

- [ ] **Step 1: 修改 react_agent.go — 删除迁移的类型/构造函数，更新导入和常量**

将 react_agent.go 修改为：

```go
package agents

import (
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReActAgent ReAct 循环 Agent：Think → Act → Observe。
//
// 直接持有基础字段（card/abilityManager/callbackManager），
// 自行实现 Invoke/Stream，在方法体内显式调用回调骨架。
//
// 对应 Python: ReActAgent (openjiuwen/core/single_agent/agents/react_agent.py)
type ReActAgent struct {
	// card Agent 身份卡片
	card *agentschema.AgentCard
	// abilityManager 能力管理器
	abilityManager *ability.AbilityManager
	// callbackManager 回调管理器
	callbackManager *rail.AgentCallbackManager
	// config Agent 配置
	config *saconfig.ReActAgentConfig
	// contextEngine 上下文引擎
	contextEngine ceinterface.ContextEngine
	// llm LLM 模型实例（延迟初始化）
	llm *llm.Model
	// promptBuilder 系统提示词构建器
	promptBuilder *prompts.SystemPromptBuilder
	// llmOnce LLM 初始化同步原语
	llmOnce sync.Once
	// kvReleaseWarningLogged KV cache 释放不支持的一次性警告标记
	kvReleaseWarningLogged bool
	// hitlHandler HITL 中断处理器
	hitlHandler *interrupt.ToolInterruptHandler
	// skillUtil 技能工具（延迟初始化，Configure 时根据 sysOperationID 创建）
	// 对应 Python: self._skill_util
	skillUtil *skills.SkillUtil
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
	// defaultMaxIterations 默认最大迭代次数
	defaultMaxIterations = 5
	// identitySection identity 区段名称
	// 对应 Python: _IDENTITY_SECTION = "identity"
	identitySection = "identity"
	// identitySectionPriority identity 区段优先级
	// 对应 Python: _IDENTITY_SECTION_PRIORITY = 10
	identitySectionPriority = 10
	// skillsSection skills 区段名称
	// 对应 Python: _SKILLS_SECTION = "skills"
	skillsSection = "skills"
	// skillsSectionPriority skills 区段优先级
	// 对应 Python: _SKILLS_SECTION_PRIORITY = 90
	skillsSectionPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReActAgent 创建 ReActAgent 实例。
//
// 对应 Python: ReActAgent.__init__(card)
func NewReActAgent(
	card *agentschema.AgentCard,
	config *saconfig.ReActAgentConfig,
) *ReActAgent {
	agent := &ReActAgent{
		card:            card,
		abilityManager:  ability.NewAbilityManager(nil),
		callbackManager: rail.NewAgentCallbackManager(card.ID),
		config:          config,
		promptBuilder:   prompts.NewSystemPromptBuilder(),
	}

	// 初始化 HITL 中断处理器
	agent.hitlHandler = interrupt.NewToolInterruptHandler(agent)

	return agent
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

关键变更点：
1. 删除 `PromptSection` 和 `SystemPromptBuilder` 结构体定义（lines 19-39）
2. 新增 `import "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"`
3. `promptBuilder` 字段类型改为 `*prompts.SystemPromptBuilder`
4. 删除 `defaultLanguage` 常量
5. 新增 `identitySection` / `identitySectionPriority` 常量
6. 删除 `NewSystemPromptBuilder()` 和 `NewPromptSection()` 构造函数（lines 117-132）
7. `NewReActAgent` 中改为 `prompts.NewSystemPromptBuilder()`

- [ ] **Step 2: 修改 react_prompt.go — 删除迁移的方法，AddPromptBuilderSection 对齐 Python**

将 react_prompt.go 中删除以下方法（已迁移到 prompts 包）：
- `PromptSection.Render()` (lines 178-190)
- `SystemPromptBuilder.AddSection()` (lines 133-137)
- `SystemPromptBuilder.RemoveSection()` (lines 139-143)
- `SystemPromptBuilder.HasSection()` (lines 145-149)
- `SystemPromptBuilder.Build()` (lines 151-176)

修改 `AddPromptBuilderSection` 签名和逻辑：

```go
// AddPromptBuilderSection 添加或替换提示节，空内容时移除该节。
//
// 对应 Python: ReActAgent.add_prompt_builder_section(name, content, *, priority)
func (a *ReActAgent) AddPromptBuilderSection(name string, content string, priority int) {
	text := strings.TrimSpace(content)
	if text == "" {
		a.promptBuilder.RemoveSection(name)
		return
	}
	a.promptBuilder.AddSection(prompts.PromptSection{
		Name:     name,
		Content:  map[string]string{"cn": text, "en": text},
		Priority: priority,
	})
}
```

修改 `Configure` 方法中对 AddPromptBuilderSection 的调用：

```go
a.promptBuilder = prompts.NewSystemPromptBuilder()
if cfg.PromptTemplateName != "" {
	a.AddPromptBuilderSection(identitySection, cfg.PromptTemplateName, identitySectionPriority)
}
```

修改 `updateSkillPromptBuilderSection` 方法：

```go
func (a *ReActAgent) updateSkillPromptBuilderSection(renderedSystemPrompt string) {
	if renderedSystemPrompt == "" || a.skillUtil == nil || !a.skillUtil.HasSkill() {
		a.promptBuilder.RemoveSection(skillsSection)
		return
	}
	skillPrompt := a.skillUtil.GetSkillPrompt()
	a.AddPromptBuilderSection(skillsSection, skillPrompt, skillsSectionPriority)
}
```

在 react_prompt.go 的 import 中新增 `"strings"` 和 `"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"`，删除不再需要的 `"sort"`。

- [ ] **Step 3: 修改 react_invoke.go:375 — 适配 AddPromptBuilderSection 新签名**

将：
```go
a.AddPromptBuilderSection("identity", map[string]string{defaultLanguage: renderedPrompt}, 10)
```
改为：
```go
a.AddPromptBuilderSection(identitySection, renderedPrompt, identitySectionPriority)
```

- [ ] **Step 4: 修改 agents/doc.go — 更新文件目录描述**

```go
// Package agents 提供 Agent 的具体实现。
//
// 本包包含各种 Agent 模式的实现，如 ReActAgent（Reasoning + Acting）等。
// 每个 Agent 实现 single_agent/interfaces 包中定义的 Agent 接口，
// 由 BaseAgent 提供配置/管理能力，子类自行实现 Invoke/Stream。
//
// PromptSection 和 SystemPromptBuilder 类型已迁移至 single_agent/prompts 包。
//
// 文件目录：
//
//	agents/
//	├── doc.go               # 包文档
//	├── react_agent.go       # ReActAgent 结构体定义 + 构造函数
//	├── react_invoke.go      # Invoke/Stream 入口（含回调骨架）+ invokeImpl/streamImpl + ReAct 循环
//	├── react_model_call.go  # LLM 模型调用（callModel/railedModelCall/callLLMInvoke/callLLMStream）
//	├── react_prompt.go      # 系统提示词构建（AddPromptBuilderSection/Configure/updateSkillPromptBuilderSection）
//	└── react_helpers.go     # 内部辅助函数（initContext/getLLM/getTools/saveContexts 等）
//
// 对应 Python 代码：openjiuwen/core/single_agent/agents/
package agents
```

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/single_agent/agents/react_agent.go internal/agentcore/single_agent/agents/react_prompt.go internal/agentcore/single_agent/agents/react_invoke.go internal/agentcore/single_agent/agents/doc.go
git commit -m "refactor(6.29): 迁移 PromptSection/SystemPromptBuilder 至 prompts 包，AddPromptBuilderSection 对齐 Python"
```

---

### Task 3: 修改 agents 测试 — 删除迁移的测试，适配新签名

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_agent_test.go`

- [ ] **Step 1: 删除迁移到 prompts 包的测试函数**

从 react_agent_test.go 中删除以下测试（已迁移至 prompts/builder_test.go）：
- `TestNewSystemPromptBuilder` (lines 94-100)
- `TestNewPromptSection` (lines 102-108)
- `TestPromptSection_Render` (lines 110-128)
- `TestPromptSection_Render_任意语言回退` (lines 130-138)
- `TestSystemPromptBuilder_AddSection` (lines 140-148)
- `TestSystemPromptBuilder_AddSection_链式调用` (lines 150-155)
- `TestSystemPromptBuilder_AddSection_同名称覆盖` (lines 157-163)
- `TestSystemPromptBuilder_RemoveSection` (lines 165-173)
- `TestSystemPromptBuilder_RemoveSection_链式调用` (lines 175-180)
- `TestSystemPromptBuilder_HasSection_不存在` (lines 182-186)
- `TestSystemPromptBuilder_Build` (lines 188-197)
- `TestSystemPromptBuilder_Build_空构建器` (lines 199-203)
- `TestSystemPromptBuilder_Build_单节` (lines 205-210)
- `TestSystemPromptBuilder_Build_跳过空内容节` (lines 212-218)

- [ ] **Step 2: 修改 TestReActAgent_AddPromptBuilderSection — 适配新签名 + 增加空内容测试**

将：
```go
func TestReActAgent_AddPromptBuilderSection(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("prompt_test"),
		cschema.WithDescription("提示词测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.AddPromptBuilderSection("identity", map[string]string{"cn": "我是测试Agent"}, 10)
	assert.True(t, agent.promptBuilder.HasSection("identity"))
}
```
改为：
```go
func TestReActAgent_AddPromptBuilderSection(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("prompt_test"),
		cschema.WithDescription("提示词测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.AddPromptBuilderSection("identity", "我是测试Agent", 10)
	assert.True(t, agent.promptBuilder.HasSection("identity"))
}

// TestReActAgent_AddPromptBuilderSection_空内容时移除 验证空内容时移除节
func TestReActAgent_AddPromptBuilderSection_空内容时移除(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("prompt_empty"),
		cschema.WithDescription("空内容提示词测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.AddPromptBuilderSection("identity", "内容", 10)
	assert.True(t, agent.promptBuilder.HasSection("identity"))

	// 空内容应移除
	agent.AddPromptBuilderSection("identity", "", 10)
	assert.False(t, agent.promptBuilder.HasSection("identity"))
}

// TestReActAgent_AddPromptBuilderSection_空白内容时移除 验证全空白内容时移除节
func TestReActAgent_AddPromptBuilderSection_空白内容时移除(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("prompt_ws"),
		cschema.WithDescription("空白内容提示词测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.AddPromptBuilderSection("identity", "   ", 10)
	assert.False(t, agent.promptBuilder.HasSection("identity"))
}
```

- [ ] **Step 3: 运行 agents 包测试验证通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -tags=test -v ./internal/agentcore/single_agent/agents/...`
Expected: PASS，所有测试通过

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/single_agent/agents/react_agent_test.go
git commit -m "test(6.29): 删除迁移的 prompts 测试，适配 AddPromptBuilderSection 新签名，增加空内容移除测试"
```

---

### Task 4: 编译验证 + 覆盖率检查 + IMPLEMENTATION_PLAN 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` (6.29 状态)

- [ ] **Step 1: 检查编译残留进程**

Run: `pgrep -f 'go (build|test)'`
Expected: 无残留进程（如有则 kill）

- [ ] **Step 2: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功，无错误

- [ ] **Step 3: 全量测试验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -tags=test -cover ./internal/agentcore/single_agent/...`
Expected: 所有测试通过，prompts 包覆盖率 ≥ 85%

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md — 6.29 状态改为 ✅**

将 IMPLEMENTATION_PLAN.md 中 `| 6.29 | ☐ |` 改为 `| 6.29 | ✅ |`

- [ ] **Step 5: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 6.29 Agent Prompts 标记为已完成 ✅"
```
