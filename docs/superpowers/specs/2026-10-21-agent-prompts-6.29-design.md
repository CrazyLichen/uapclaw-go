# 6.29 Agent Prompts 设计文档

## 1. 概述

6.29 Agent Prompts 提供基于节的系统提示词构建器（`SystemPromptBuilder`），将多个命名的、带优先级的、多语言的提示词片段（`PromptSection`）组装为完整的系统提示词，供 LLM 调用使用。

对应 Python 路径：`openjiuwen/core/single_agent/prompts/`

### 流程位置

在 Agent 会话流程中，Agent Prompts 处于 Configure → Invoke 之间：

```
ReActAgent.Invoke() 入口
  │
  ├── Configure()          — 初始化 promptBuilder，注册 identity section (priority=10)
  │
  ├── buildRenderedSystemPrompt(inputs)    ← 【6.29 核心入口】
  │     ├── 从 prompt_template 过滤 role="system" 的消息
  │     ├── renderSystemMessages()：用 PromptTemplate 替换占位符（如 {query}）
  │     └── 返回拼接后的字符串
  │
  ├── addPromptBuilderSection("identity", renderedSystemPrompt, priority=10)
  │     └── 若 content 为空则 removeSection，否则 addSection
  │
  ├── updateSkillPromptBuilderSection(renderedSystemPrompt)
  │     ├── 若无技能则 removeSection("skills")
  │     └── 否则 addPromptBuilderSection("skills", skillPrompt, priority=90)
  │
  ├── promptBuilder.Build()     ← 【6.29 核心输出】
  │     ├── 按 priority 排序所有 sections
  │     ├── 按 language 渲染每个 section
  │     └── 用 "\n\n" 拼接为完整系统提示词
  │
  └── 将最终系统提示词传给 LLM 调用
```

### 作用

Agent Prompts 是 ReActAgent 构建 LLM 系统提示词的核心机制。主要包括：
- **identity**（身份描述，priority=10）— Agent 的角色和行为定义
- **skills**（技能描述，priority=90）— Agent 可用的技能工具描述
- 支持自定义扩展（其他 rails 可动态注册/移除 section）

## 2. 方案选择

### 决策：方案 A — 完全迁移至独立 prompts/ 子包

将 `PromptSection` 和 `SystemPromptBuilder` 从 `agents` 包完全迁移到新 `prompts/` 子包，`agents` 包改为导入 `prompts` 包。

**选择理由：**
- Python 中是独立包，需对齐结构
- `PromptSection`/`SystemPromptBuilder` 被 harness 层、agent_teams 层广泛复用（20+ rails 通过 `system_prompt_builder` 注入 section）
- 职责清晰：`prompts` 包负责节组装，`agents` 包负责 Agent 业务逻辑

### 关键设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 包结构 | 独立 prompts/ 子包 | 与 Python 对齐，支持跨包复用 |
| 扩展钩子 | 函数字段注入 `sectionsFilter` | Go 不用继承，函数字段比接口更轻量 |
| 空内容移除 | 对齐 Python：空内容时 RemoveSection | 调用方通过传空内容"关闭"某个 section |
| 渲染方法归属 | 保留在 agents 包 ReActAgent 上 | 与 Python 一致 |
| 辅助方法 | 全部补齐 | CharCount/GetAllSections/GetSection/SupportedLanguages |
| 区段常量 | 保留在 agents 包 | Python 中定义在 react_agent.py 模块级别 |

## 3. 详细设计

### 3.1 prompts 包结构

```
single_agent/prompts/
├── doc.go              # 包文档
├── builder.go          # PromptSection + SystemPromptBuilder + 常量
└── builder_test.go     # 单元测试
```

### 3.2 builder.go

#### 常量

```go
const (
    // SupportedLanguages 支持的语言列表
    SupportedLanguages = [2]string{"cn", "en"}
    // DefaultLanguage 默认提示词语言
    DefaultLanguage = "cn"
)
```

#### PromptSection

```go
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

// Render 渲染指定语言的内容。
// 回退顺序：精确匹配 → DefaultLanguage → map 中首个值 → 空字符串
func (s *PromptSection) Render(language string) string

// CharCount 返回指定语言渲染后的字符数。
func (s *PromptSection) CharCount(language string) int
```

#### SystemPromptBuilder

```go
// SystemPromptBuilder 基于节的系统提示词构建器。
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

// AddSection 添加或替换节（链式调用）。
func (b *SystemPromptBuilder) AddSection(section PromptSection) *SystemPromptBuilder

// RemoveSection 移除指定名称的节（链式调用）。
func (b *SystemPromptBuilder) RemoveSection(name string) *SystemPromptBuilder

// GetAllSections 返回所有注册节的副本。
func (b *SystemPromptBuilder) GetAllSections() map[string]PromptSection

// HasSection 检查节是否存在。
func (b *SystemPromptBuilder) HasSection(name string) bool

// GetSection 按名称获取单个节，不存在返回 nil。
func (b *SystemPromptBuilder) GetSection(name string) *PromptSection

// Build 按优先级排序并拼接为完整系统提示词。
func (b *SystemPromptBuilder) Build() string
```

#### 构造函数

```go
// NewSystemPromptBuilder 创建系统提示词构建器（默认语言 "cn"，无过滤）。
func NewSystemPromptBuilder() *SystemPromptBuilder

// NewSystemPromptBuilderWithFilter 创建带自定义语言和过滤函数的构建器。
func NewSystemPromptBuilderWithFilter(lang string, filter func([]PromptSection) []PromptSection) *SystemPromptBuilder

// NewPromptSection 创建提示节。
func NewPromptSection(name string, content map[string]string, priority int) PromptSection
```

#### Build 逻辑

```go
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

// getSectionsForBuild 返回参与 Build 的节列表，应用 sectionsFilter 过滤。
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

### 3.3 agents 包改动

#### react_agent.go

**删除：** `PromptSection` 和 `SystemPromptBuilder` 结构体定义

**新增导入：**
```go
import "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
```

**常量补齐（对齐 Python react_agent.py:65-68）：**
```go
const (
    // identitySection identity 区段名称
    identitySection = "identity"
    // identitySectionPriority identity 区段优先级
    identitySectionPriority = 10
    // skillsSection skills 区段名称
    skillsSection = "skills"
    // skillsSectionPriority skills 区段优先级
    skillsSectionPriority = 90
)
```

**删除 `defaultLanguage` 常量**（改用 `prompts.DefaultLanguage`）

**NewReActAgent 中：** `promptBuilder: NewSystemPromptBuilder()` → `promptBuilder: prompts.NewSystemPromptBuilder()`

#### react_prompt.go

**删除：** `PromptSection.Render()`、`SystemPromptBuilder.AddSection/RemoveSection/HasSection/Build` 方法定义

**AddPromptBuilderSection 签名和逻辑对齐 Python：**
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

**签名变化：** `content` 参数从 `map[string]string` 改为 `string`（对齐 Python，内部构造 `{"cn": text, "en": text}` map）

**Configure 改动：**
```go
a.promptBuilder = prompts.NewSystemPromptBuilder()
if cfg.PromptTemplateName != "" {
    a.AddPromptBuilderSection(identitySection, cfg.PromptTemplateName, identitySectionPriority)
}
```

**updateSkillPromptBuilderSection 改动：**
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

### 3.4 doc.go

#### prompts/doc.go

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

#### agents/doc.go 更新

文件目录中移除 PromptSection/SystemPromptBuilder 相关描述，说明类型已迁移至 prompts 包。

### 3.5 日志对齐

Python `builder.py` 本身**没有 logger 调用**（纯数据结构）。prompts 包不需要日志。

Python `react_agent.py` 的 `_render_system_messages` 中有：
```python
logger.warning("Failed to render system message placeholder: %s", e)
```

Go 对应（在 agents 包的 `buildRenderedSystemPrompt` 中）：
```go
logger.Warn(logger.ComponentAgentCore).
    Str("event_type", "render_system_message_error").
    Err(err).
    Msg("渲染系统消息占位符失败")
```

## 4. 测试设计

### 4.1 prompts/builder_test.go

#### PromptSection 测试

| 测试函数 | 场景 |
|---------|------|
| `TestPromptSection_Render` | 精确语言匹配返回对应内容 |
| `TestPromptSection_Render_回退到默认语言` | 请求的语言不存在，回退到 DefaultLanguage |
| `TestPromptSection_Render_回退到首个值` | 请求语言和默认语言都不存在，返回 map 中首个值 |
| `TestPromptSection_Render_空内容` | Content 为空 map 时返回空字符串 |
| `TestPromptSection_CharCount` | 返回渲染后字符数 |
| `TestPromptSection_CharCount_多语言` | 不同语言字符数不同 |

#### SystemPromptBuilder 测试

| 测试函数 | 场景 |
|---------|------|
| `TestNewSystemPromptBuilder` | 默认语言为 "cn"，sections 为空 |
| `TestSystemPromptBuilder_AddSection` | 添加节后 HasSection 返回 true |
| `TestSystemPromptBuilder_AddSection_同名覆盖` | 同名节覆盖旧节 |
| `TestSystemPromptBuilder_AddSection_链式调用` | 返回 *SystemPromptBuilder 可链式调用 |
| `TestSystemPromptBuilder_RemoveSection` | 移除节后 HasSection 返回 false |
| `TestSystemPromptBuilder_RemoveSection_不存在时不报错` | 移除不存在的节不 panic |
| `TestSystemPromptBuilder_GetAllSections` | 返回所有节的副本 |
| `TestSystemPromptBuilder_GetAllSections_副本隔离` | 修改返回值不影响原 builder |
| `TestSystemPromptBuilder_GetSection` | 按名称获取节 |
| `TestSystemPromptBuilder_GetSection_不存在` | 返回 nil |
| `TestSystemPromptBuilder_HasSection` | 存在/不存在两种情况 |
| `TestSystemPromptBuilder_Build` | 按 priority 排序，用 "\n\n" 拼接 |
| `TestSystemPromptBuilder_Build_跳过空内容` | Render 返回空字符串的节被跳过 |
| `TestSystemPromptBuilder_Build_无节时返回空字符串` | sections 为空 |
| `TestSystemPromptBuilder_Build_过滤钩子` | sectionsFilter 过滤掉部分节 |
| `TestSystemPromptBuilder_Build_过滤钩子为nil` | sectionsFilter 为 nil 时不过滤 |
| `TestNewSystemPromptBuilderWithFilter` | 自定义语言和过滤函数 |

#### 常量测试

| 测试函数 | 场景 |
|---------|------|
| `TestSupportedLanguages` | 包含 "cn" 和 "en" |
| `TestDefaultLanguage` | 值为 "cn" |

### 4.2 agents 包测试改动

- `AddPromptBuilderSection` 测试：适配新签名（string 而非 map[string]string），增加空内容移除测试
- `Configure` 测试：改用新常量引用
- 直接构造 `PromptSection` 的测试：改为 `prompts.NewPromptSection()` 或 `prompts.PromptSection{}`

## 5. 变更影响范围

| 文件 | 变更类型 |
|------|---------|
| `single_agent/prompts/doc.go` | **新增** |
| `single_agent/prompts/builder.go` | **新增** — 从 react_agent.go 迁移类型 + 补齐方法 |
| `single_agent/prompts/builder_test.go` | **新增** |
| `single_agent/agents/react_agent.go` | **修改** — 删除类型定义，改导入 prompts 包，补齐 identity 常量 |
| `single_agent/agents/react_prompt.go` | **修改** — 删除迁移方法，AddPromptBuilderSection 签名+逻辑对齐 Python |
| `single_agent/agents/doc.go` | **修改** — 更新文件目录描述 |
| `single_agent/agents/react_agent_test.go` | **修改** — 适配新签名（如有） |
| `single_agent/agents/react_prompt_test.go` | **修改** — 适配新签名 + 增加空内容移除测试 |

## 6. 回填清单

以下已有内容在迁移过程中**不能丢失**：

| 内容 | 迁移去向 |
|------|---------|
| `PromptSection` 结构体 | → prompts 包 |
| `SystemPromptBuilder` 结构体 | → prompts 包 |
| `PromptSection.Render()` 方法 | → prompts 包 |
| `SystemPromptBuilder.AddSection/RemoveSection/HasSection/Build` | → prompts 包 |
| `AddPromptBuilderSection` 方法 | 保留在 agents 包，签名和逻辑对齐 Python |
| `updateSkillPromptBuilderSection` 方法 | 保留在 agents 包，逻辑微调 |
| `Configure` 中 promptBuilder 初始化 | 保留在 agents 包，改用新包引用 |
| `defaultLanguage` 常量 | 删除，改用 `prompts.DefaultLanguage` |
| `skillsSection/skillsSectionPriority` 常量 | 保留在 agents 包 |
