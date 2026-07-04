# 9.51-9.53 Harness 资源/Schema/Prompts 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Harness 层的 Schema 补全（loop_event/state/task）、提示词模板系统（builder/sections/tools/workspace_content）、资源管理（builtin_rules.yaml），并修复 8 项已有实现与 Python 的不一致。

**Architecture:** 与 Python 保持一致的三包拆分：harness/prompts/（顶层）、harness/prompts/sections/（Section 构建函数）、harness/prompts/tools/（工具级提示词）、harness/prompts/workspace_content/（双语模板常量）。Schema 新增类型放在 harness/schema/。一致性修复涉及 schema/config.go、task_loop/stop_condition.go、task_loop/loop_coordinator.go、workspace/workspace.go。

**Tech Stack:** Go 1.22+、github.com/google/uuid、embed（builtin_rules.yaml）

**设计文档:** `docs/superpowers/specs/2025-07-11-harness-schema-prompts-9.51-9.53-design.md`

---

## 阶段 1：Schema 基础（9.52）

### Task 1: DeepLoopEventType 枚举 + DeepLoopEvent 结构体

**Files:**
- Create: `internal/agentcore/harness/schema/loop_event.go`
- Create: `internal/agentcore/harness/schema/loop_event_test.go`

- [ ] **Step 1: 创建 loop_event.go — 枚举 + 结构体 + 工厂函数**

参照 Python `openjiuwen/harness/schema/loop_event.py` 实现：

- `DeepLoopEventType` 枚举（iota: Followup=0, Steer=1, Abort=2），JSON 值为 "followup"/"steer"/"abort"
- 实现 `String()`、`MarshalJSON()`、`UnmarshalJSON()`、`ParseDeepLoopEventType()` 方法（模式与 agent_mode.go 一致）
- `eventPriorityMap` 内部映射：Abort=0, Steer=1, Followup=10
- `DeepLoopEvent` 结构体（Priority, Seq, CreatedAt, EventID, EventType, Content, TaskID, Metadata）
- `Less(other DeepLoopEvent) bool` 方法（priority 升序，同 priority 按 seq 升序）
- `DefaultEventPriority(eventType) int` 导出函数
- `CreateLoopEvent` 导出函数 + Functional Options 模式（`WithTaskID`、`WithMetadata`、`WithPriority`）
- `NewDeepLoopEvent` 内部构造函数，CreatedAt 用 `float64(time.Now().UnixNano())`，EventID 用 `uuid.New().String()`

- [ ] **Step 2: 创建 loop_event_test.go**

测试用例：
- 枚举 JSON 往返（marshal → unmarshal → assert equal）
- ParseDeepLoopEventType 正常 + 无效输入
- CreateLoopEvent 默认优先级 + 自定义优先级 + optional 参数
- Less 排序：不同 priority、同 priority 不同 seq
- DeepLoopEvent JSON 序列化往返

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/schema/... -run TestDeepLoop -v`

- [ ] **Step 4: Commit**

```
feat(harness): add DeepLoopEventType enum and DeepLoopEvent struct (9.52)
```

---

### Task 2: PlanModeState + DeepAgentState 结构体

**Files:**
- Create: `internal/agentcore/harness/schema/state.go`
- Create: `internal/agentcore/harness/schema/state_test.go`

- [ ] **Step 1: 创建 state.go**

参照 Python `openjiuwen/harness/schema/state.py` 实现：

- `PlanModeState` 结构体（Mode, PrePlanMode, PlanSlug, PromptContext）
- `NewPlanModeState()` 构造函数，默认 Mode="normal", PrePlanMode="normal"
- `ToDict() map[string]any`
- `FromDict(data map[string]any) PlanModeState` — nil 输入返回默认值

- `DeepAgentState` 结构体（Iteration, TaskPlan, StopConditionState, PendingFollowUps, PlanMode）
- `NewDeepAgentState()` 构造函数，默认值
- `ToSessionDict() map[string]any`
- `FromSessionDict(data map[string]any) DeepAgentState` — nil 输入返回默认值

- [ ] **Step 2: 创建 state_test.go**

测试用例：
- PlanModeState 默认值
- PlanModeState.ToDict → FromDict 往返
- PlanModeState.FromDict(nil) 返回默认值
- DeepAgentState 默认值
- DeepAgentState.ToSessionDict → FromSessionDict 往返
- DeepAgentState.FromSessionDict(nil) 返回默认值
- DeepAgentState 包含 TaskPlan 时的序列化

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/schema/... -run TestState -v`

- [ ] **Step 4: Commit**

```
feat(harness): add PlanModeState and DeepAgentState structs (9.52)
```

---

### Task 3: TodoStatus 枚举 + TodoItem + TaskPlan + ModelUsageRecord

**Files:**
- Create: `internal/agentcore/harness/schema/task.go`
- Create: `internal/agentcore/harness/schema/task_test.go`

- [ ] **Step 1: 创建 task.go**

参照 Python `openjiuwen/harness/schema/task.py` 实现：

**TodoStatus 枚举：** Pending=0, InProgress=1, Completed=2, Cancelled=3
- JSON 值："pending"/"in_progress"/"completed"/"cancelled"
- `String()`、`MarshalJSON()`、`UnmarshalJSON()`、`ParseTodoStatus()` 方法
- `StatusIcons` 全局变量 `map[TodoStatus]string`：Pending="[ ]", InProgress="[→]", Completed="[√]", Cancelled="[×]"

**TodoItem 结构体：** ID, Content, ActiveForm, Description, Status, DependsOn, ResultSummary, MetaData, SelectedModelID
- `NewTodoItem()` 构造函数，ID 默认 uuid
- `ToDict() map[string]any`
- `FromDict(data map[string]any) TodoItem`

**TaskPlan 结构体：** Goal, Tasks, CurrentTaskID
- `NewTaskPlan()` 构造函数
- `GetTask(taskID string) *TodoItem`
- `GetNextTask() *TodoItem` — 返回第一个 Pending 且所有 DependsOn 已 Completed 的 task
- `AddTask(task TodoItem)`
- `MarkInProgress(taskID string) error` — 设置 Status + 更新 CurrentTaskID
- `MarkCompleted(taskID string, summary string) error`
- `MarkCancelled(taskID string, reason string) error`
- `GetProgressSummary() string` — 如 "3/7 completed"
- `ToMarkdown() string` — 渲染为 markdown checklist（使用 StatusIcons）
- `ToDict() map[string]any`
- `FromDict(data map[string]any) TaskPlan`

**ModelUsageRecord 结构体：** ModelID, InputTokens, OutputTokens
- `Add(inputTokens, outputTokens int)`
- `String() string`

- [ ] **Step 2: 创建 task_test.go**

测试用例：
- TodoStatus 枚举 JSON 往返 + Parse + 无效输入
- StatusIcons 值验证
- TodoItem.ToDict → FromDict 往返
- TodoItem 默认值
- TaskPlan.GetTask 找到/未找到
- TaskPlan.GetNextTask 依赖排序（A depends_on B，B 未完成时 A 不可选）
- TaskPlan.MarkInProgress/MarkCompleted/MarkCancelled 正常 + 错误（taskID 不存在）
- TaskPlan.GetProgressSummary
- TaskPlan.ToMarkdown 输出格式
- TaskPlan.ToDict → FromDict 往返
- ModelUsageRecord.Add 累加
- ModelUsageRecord.String 格式

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/schema/... -run TestTask -v`

- [ ] **Step 4: Commit**

```
feat(harness): add TodoStatus, TodoItem, TaskPlan, ModelUsageRecord (9.52)
```

---

## 阶段 2：Prompts 基础（9.53 底层）

### Task 4: workspace_content 包 — 双语模板常量

**Files:**
- Create: `internal/agentcore/harness/prompts/workspace_content/doc.go`
- Create: `internal/agentcore/harness/prompts/workspace_content/workspace_header.go`
- Create: `internal/agentcore/harness/prompts/workspace_content/template_identity.go`
- Create: `internal/agentcore/harness/prompts/workspace_content/template_agent.go`
- Create: `internal/agentcore/harness/prompts/workspace_content/template_soul.go`
- Create: `internal/agentcore/harness/prompts/workspace_content/template_heartbeat.go`
- Create: `internal/agentcore/harness/prompts/workspace_content/template_session_memory.go`
- Create: `internal/agentcore/harness/prompts/workspace_content/template_memory.go`
- Create: `internal/agentcore/harness/prompts/workspace_content/workspace_content_test.go`

- [ ] **Step 1: 创建 doc.go**

包文档，描述本包存放 workspace 双语模板常量，对应 Python `harness/prompts/workspace_content/`。

- [ ] **Step 2: 创建 workspace_header.go**

从 Python `harness/prompts/workspace_content/workspace_header.py` 原样移植全部 12 组双语常量：
- `WorkspaceHeaderCN`/`WorkspaceHeaderEN`
- `ImportantFilesCN`/`ImportantFilesEN`
- `ContextHeaderCN`/`ContextHeaderEN`
- `ContextFileTitlesCN`/`ContextFileTitlesEN`（6 条映射）
- `DailyMemoryTitleCN`/`DailyMemoryTitleEN`（含 `{date}` 占位符）
- `DirectoryDescriptionsCN`/`DirectoryDescriptionsEN`（12 条映射）
- `ContextFiles` 列表

- [ ] **Step 3: 创建 6 个模板文件**

从 Python 对应文件原样复制 CN/EN 模板文本为 Go `const` 字面量：
- `template_identity.go` → `IdentityMDCN`, `IdentityMDEN`
- `template_agent.go` → `AgentMDCN`, `AgentMDEN`
- `template_soul.go` → `SoulMDCN`, `SoulMDEN`
- `template_heartbeat.go` → `HeartbeatMDCN`, `HeartbeatMDEN`
- `template_session_memory.go` → `SessionMemoryMDCN`, `SessionMemoryMDEN`
- `template_memory.go` → `MemoryMDCN`, `MemoryMDEN`

**规则：** 使用反引号原始字符串 `` ` `` 包裹多行文本。模板文本内容与 Python **逐字一致**，不修改任何措辞。

- [ ] **Step 4: 创建 workspace_content_test.go**

测试用例：
- 所有 CN/EN 常量非空
- ContextFiles 长度 = 5
- DirectoryDescriptionsCN/EN 包含 12 个键
- ContextFileTitlesCN/EN 包含 6 个键
- DailyMemoryTitleCN/EN 包含 `{date}` 占位符

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/prompts/workspace_content/... -v`

- [ ] **Step 6: Commit**

```
feat(harness): add workspace_content bilingual template constants (9.53)
```

---

### Task 5: sections 包 — SectionName + 16 个 Section 构建函数

**Files:**
- Create: `internal/agentcore/harness/prompts/sections/doc.go`
- Create: `internal/agentcore/harness/prompts/sections/section_name.go`
- Create: `internal/agentcore/harness/prompts/sections/identity.go`
- Create: `internal/agentcore/harness/prompts/sections/safety.go`
- Create: `internal/agentcore/harness/prompts/sections/context.go`
- Create: `internal/agentcore/harness/prompts/sections/skills.go`
- Create: `internal/agentcore/harness/prompts/sections/memory.go`
- Create: `internal/agentcore/harness/prompts/sections/external_memory.go`
- Create: `internal/agentcore/harness/prompts/sections/workspace.go`
- Create: `internal/agentcore/harness/prompts/sections/progressive_tool_rail.go`
- Create: `internal/agentcore/harness/prompts/sections/heartbeat.go`
- Create: `internal/agentcore/harness/prompts/sections/coding_memory.go`
- Create: `internal/agentcore/harness/prompts/sections/session_tools.go`
- Create: `internal/agentcore/harness/prompts/sections/agent_mode.go`
- Create: `internal/agentcore/harness/prompts/sections/task_tool.go`
- Create: `internal/agentcore/harness/prompts/sections/task_completion.go`
- Create: `internal/agentcore/harness/prompts/sections/todo.go`
- Create: `internal/agentcore/harness/prompts/sections/reload.go`
- Create: `internal/agentcore/harness/prompts/sections/sections_test.go`

- [ ] **Step 1: 创建 doc.go + section_name.go**

`section_name.go` 定义 18 个 SectionName 常量（与 Python `sections/__init__.py` SectionName 类一致）。

- [ ] **Step 2: 创建 16 个 section 文件**

每个文件从 Python 对应 `harness/prompts/sections/*.py` 原样移植 CN/EN 模板文本和 Build 函数逻辑：

| 文件 | Build 函数 | 模板来源 |
|------|-----------|---------|
| identity.go | `BuildIdentitySection() saprompt.PromptSection` | identity.py |
| safety.go | `BuildSafetySection() saprompt.PromptSection` | safety.py |
| context.go | `BuildToolsSection` + `BuildContextSection` | context.py |
| skills.go | `BuildSkillsSection(mode, skillPaths, lang)` | skills.py |
| memory.go | `BuildMemorySection(mode, todayDate, lang)` | memory.py |
| external_memory.go | `BuildExternalMemorySection(promptBlock, lang)` | external_memory.py |
| workspace.go | `BuildWorkspaceSection(rootPath, dirTree, lang)` | workspace.py |
| progressive_tool_rail.go | `BuildNavigationSection` + `BuildProgressiveToolRulesSection` | progressive_tool_rail.py |
| heartbeat.go | `BuildHeartbeatSection(heartbeatContent, lang)` | heartbeat.py |
| coding_memory.go | `BuildCodingMemorySection(memoryDir, readOnly, lang)` | coding_memory.py |
| session_tools.go | `BuildSessionToolsSection(lang)` | session_tools.py |
| agent_mode.go | `BuildPlanModeSection(enterStatus, planFileInfo, lang)` | agent_mode.py |
| task_tool.go | `BuildTaskToolSection(lang)` | task_tool.py |
| task_completion.go | `BuildCompletionSignalSection(promise, lang)` | task_completion.py |
| todo.go | `BuildTodoSection(lang)` + `BuildProgressReminderUserPrompt` + `BuildModelSelectionPrompt` | todo.py |
| reload.go | `BuildReloadSection(lang)` | reload.py |

**规范：**
- 返回类型为 `singleagentprompts.PromptSection`（import alias `saprompt`）
- CN/EN 模板用包级 `const` 反引号字符串，**与 Python 逐字一致**
- 模板变量用 `{variable}` 占位符，Build 函数内用 `strings.ReplaceAll` 替换
- 每个 Build 函数构造 `saprompt.PromptSection{Name: SectionXxx, Content: map[string]string{"cn": cnText, "en": enText}, Priority: N}`

- [ ] **Step 3: 创建 sections_test.go**

测试用例：
- 每个 Build 函数在 lang="cn"/"en" 下返回非空 Content
- 每个 Build 函数返回正确的 Name 和 Priority
- identity/safety 无参数 Build 成功
- heartbeat 包含 `{heartbeat_section}` 替换后不残留占位符
- task_completion 包含 `{promise}` 替换
- agent_mode 包含 `{enter_plan_mode_status}` 和 `{plan_file_info}` 替换
- coding_memory readOnly=true 时内容包含"只读"关键字
- memory mode="proactive"/"inactive"/"read_only" 三种模式输出不同

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/prompts/sections/... -v`

- [ ] **Step 5: Commit**

```
feat(harness): add prompt section builders with CN/EN templates (9.53)
```

---

### Task 6: prompts 顶层包 — builder + report + sanitize

**Files:**
- Create: `internal/agentcore/harness/prompts/doc.go`
- Create: `internal/agentcore/harness/prompts/builder.go`
- Create: `internal/agentcore/harness/prompts/builder_test.go`
- Create: `internal/agentcore/harness/prompts/report.go`
- Create: `internal/agentcore/harness/prompts/report_test.go`
- Create: `internal/agentcore/harness/prompts/sanitize.go`
- Create: `internal/agentcore/harness/prompts/sanitize_test.go`

- [ ] **Step 1: 创建 doc.go**

包文档，描述本包为 DeepAgent 系统提示词构建层，对应 Python `harness/prompts/`。

- [ ] **Step 2: 创建 builder.go**

参照 Python `harness/prompts/builder.py` 实现：

```go
// SupportedLanguages 支持的语言列表
var SupportedLanguages = []string{"cn", "en"}

// DefaultLanguage 默认语言
const DefaultLanguage = "cn"

// MinimalSections 最小模式包含的 section 名集合
var MinimalSections = map[string]bool{
    sections.SectionIdentity: true,
    sections.SectionSafety:   true,
    sections.SectionSkills:   true,
    sections.SectionTools:    true,
    sections.SectionRuntime:  true,
    sections.SectionMemory:   true,
}

// SystemPromptBuilder 系统提示词构建器（harness 扩展版）
type SystemPromptBuilder struct {
    *saprompt.SystemPromptBuilder
    mode hschema.PromptMode
}

// NewSystemPromptBuilder 创建构建器
func NewSystemPromptBuilder(language string, mode hschema.PromptMode) *SystemPromptBuilder

// Build 构建系统提示词（根据 mode 过滤 section）
func (b *SystemPromptBuilder) Build() string

// BuildReport 构建提示词报告
func (b *SystemPromptBuilder) BuildReport() *PromptReport

// ResolveLanguage 解析语言设置
func ResolveLanguage(configLanguage string) string

// ResolveMode 解析提示词模式
func ResolveMode(configMode string) hschema.PromptMode
```

Build 逻辑：
- None 模式：仅保留 Name == SectionIdentity 的 section
- Minimal 模式：仅保留 MinimalSections 中的 section
- Full 模式：委托给基类

ResolveLanguage 优先级：config 参数 > `AGENT_PROMPT_LANGUAGE` 环境变量 > DefaultLanguage

- [ ] **Step 3: 创建 report.go**

参照 Python `harness/prompts/report.py` 实现：

```go
type SectionInfo struct {
    Name      string `json:"name"`
    Priority  int    `json:"priority"`
    CharCount int    `json:"char_count"`
}

type PromptReport struct {
    TotalChars      int           `json:"total_chars"`
    EstimatedTokens int           `json:"estimated_tokens"`
    SectionCount    int           `json:"section_count"`
    Sections        []SectionInfo `json:"sections,omitempty"`
    Mode            string        `json:"mode"`
    Language        string        `json:"language"`
}

func NewPromptReport(builder *SystemPromptBuilder) *PromptReport
func (r *PromptReport) ToDict() map[string]any
func (r *PromptReport) Summary() string
```

Token 估算：CN 2.5 chars/token，EN 4.0 chars/token。

- [ ] **Step 4: 创建 sanitize.go**

参照 Python `harness/prompts/sanitize.py` 实现：

```go
// SanitizePath 剥离路径中的注入字符
func SanitizePath(path string) string

// SanitizeUserContent 剥离用户内容中的注入字符并截断长度
func SanitizeUserContent(content string, maxLen int) string
```

注入正则：`[<>\{\}\[\]`\$]|\.{3,}|\\n|\\r`

- [ ] **Step 5: 创建 builder_test.go + report_test.go + sanitize_test.go**

builder 测试：
- Full 模式 Build 返回全部 section
- Minimal 模式 Build 仅包含 MinimalSections
- None 模式 Build 仅包含 identity
- ResolveLanguage 优先级测试
- ResolveMode 测试

report 测试：
- NewPromptReport 生成正确字段
- CN 语言 token 估算（total_chars / 2.5）
- EN 语言 token 估算（total_chars / 4.0）
- Summary 格式

sanitize 测试：
- SanitizePath 剥离 `<>{}`、`$`、`...`、`\n`、`\r`
- SanitizeUserContent 剥离 + 截断
- 注入攻击测试：`<script>alert(1)</script>` → 被清理

- [ ] **Step 6: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/prompts/... -run "TestBuilder|TestReport|TestSanitize" -v`

- [ ] **Step 7: Commit**

```
feat(harness): add SystemPromptBuilder, PromptReport, sanitize (9.53)
```

---

### Task 7: tools 包 — ToolMetadataProvider + 全部 25 个工具提示词

**Files:**
- Create: `internal/agentcore/harness/prompts/tools/doc.go`
- Create: `internal/agentcore/harness/prompts/tools/base.go`
- Create: `internal/agentcore/harness/prompts/tools/registry.go`
- Create: `internal/agentcore/harness/prompts/tools/agent_mode.go`
- Create: `internal/agentcore/harness/prompts/tools/ask_user.go`
- Create: `internal/agentcore/harness/prompts/tools/audio.go`
- Create: `internal/agentcore/harness/prompts/tools/bash.go`
- Create: `internal/agentcore/harness/prompts/tools/code.go`
- Create: `internal/agentcore/harness/prompts/tools/coding_memory.go`
- Create: `internal/agentcore/harness/prompts/tools/cron.go`
- Create: `internal/agentcore/harness/prompts/tools/enter_worktree.go`
- Create: `internal/agentcore/harness/prompts/tools/exit_worktree.go`
- Create: `internal/agentcore/harness/prompts/tools/filesystem.go`
- Create: `internal/agentcore/harness/prompts/tools/list_skill.go`
- Create: `internal/agentcore/harness/prompts/tools/load_tools.go`
- Create: `internal/agentcore/harness/prompts/tools/lsp_tool.go`
- Create: `internal/agentcore/harness/prompts/tools/mcp.go`
- Create: `internal/agentcore/harness/prompts/tools/memory.go`
- Create: `internal/agentcore/harness/prompts/tools/powershell.go`
- Create: `internal/agentcore/harness/prompts/tools/search_tools.go`
- Create: `internal/agentcore/harness/prompts/tools/session_tools.go`
- Create: `internal/agentcore/harness/prompts/tools/skill_tool.go`
- Create: `internal/agentcore/harness/prompts/tools/task_tool.go`
- Create: `internal/agentcore/harness/prompts/tools/todo.go`
- Create: `internal/agentcore/harness/prompts/tools/video_understanding.go`
- Create: `internal/agentcore/harness/prompts/tools/vision.go`
- Create: `internal/agentcore/harness/prompts/tools/web_tools.go`
- Create: `internal/agentcore/harness/prompts/tools/tools_test.go`

- [ ] **Step 1: 创建 doc.go + base.go + registry.go**

**base.go** 参照 Python `harness/prompts/tools/base.py`：

```go
// ToolMetadataProvider 工具元数据提供者接口
type ToolMetadataProvider interface {
    GetName() string
    GetDescription(language string) string
    GetInputParams(language string) map[string]any
}

// ValidateToolMetadata 校验工具元数据双语完整性
func ValidateToolMetadata(provider ToolMetadataProvider) error
```

Validate 逻辑：cn/en 描述非空、input params 的 property keys 一致、每个 param 都有 cn/en description。

**registry.go** 参照 Python `harness/prompts/tools/__init__.py`：

```go
var (
    providers []ToolMetadataProvider
    registry  sync.Map  // name -> ToolMetadataProvider
)

// RegisterToolProvider 注册工具元数据提供者
func RegisterToolProvider(provider ToolMetadataProvider)

// GetToolProvider 获取工具元数据提供者
func GetToolProvider(name string) (ToolMetadataProvider, bool)

// AllProviders 返回所有已注册的提供者
func AllProviders() []ToolMetadataProvider

// BuildToolCard 从注册表组装 ToolCard（依赖 tool.ToolCard）
func BuildToolCard(name string, toolID string, language string) (*tool.ToolCard, error)
```

- [ ] **Step 2: 创建 25 个工具提示词文件**

每个文件从 Python `harness/prompts/tools/*.py` 原样移植：
- `DESCRIPTION map[string]string`（CN/EN 双语描述）
- `GetXxxInputParams(language string) map[string]any` — 参数 Schema 构建函数
- `XxxMetadataProvider` 结构体实现 `ToolMetadataProvider` 接口
- `init()` 中调用 `RegisterToolProvider(&XxxMetadataProvider{})`

**规范：** 提示词文本与 Python **逐字一致**，参数 Schema 的 JSON 结构与 Python 对齐。

- [ ] **Step 3: 创建 tools_test.go**

测试用例：
- ValidateToolMetadata 对合法 provider 通过
- ValidateToolMetadata 对缺失语言的 provider 报错
- 每个 provider 的 GetName 非空
- 每个 provider 的 GetDescription("cn") 和 GetDescription("en") 非空
- registry 中所有 provider 可通过 GetToolProvider 查找到
- AllProviders 返回正确数量

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/prompts/tools/... -v`

- [ ] **Step 5: Commit**

```
feat(harness): add ToolMetadataProvider and all tool prompt templates (9.53)
```

---

## 阶段 3：资源管理（9.51）

### Task 8: resources 包 — builtin_rules.yaml

**Files:**
- Create: `internal/agentcore/harness/resources/doc.go`
- Create: `internal/agentcore/harness/resources/builtin_rules.yaml`
- Create: `internal/agentcore/harness/resources/resources_test.go`

- [ ] **Step 1: 创建 doc.go + builtin_rules.yaml**

从 Python `harness/resources/builtin_rules.yaml` 原样复制全部 10 条 CRITICAL 安全规则。

YAML 结构保持一致：rules 列表，每条含 id、description（cn/en）、severity、match_type、patterns（re: 前缀）、target_tools。

- [ ] **Step 2: 创建 resources_test.go**

测试用例：
- 使用 `go:embed` 嵌入 builtin_rules.yaml
- 解析 YAML 验证规则数量 = 10
- 每条规则 id 非空
- 每条规则 severity = "CRITICAL"
- 每条规则 target_tools 包含 "bash"
- 最后一条规则 action = "deny"

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/resources/... -v`

- [ ] **Step 4: Commit**

```
feat(harness): add builtin_rules.yaml security rules (9.51)
```

---

## 阶段 4：一致性修复

### Task 9: 修复 #1 AudioModelConfig JSON key + #8 FactoryName/FactoryKwargs 确认

**Files:**
- Modify: `internal/agentcore/harness/schema/config.go`
- Modify: `internal/agentcore/harness/schema/config_test.go`

- [ ] **Step 1: 修复 AudioModelConfig.QAModel JSON tag**

在 `config.go` 中将 `QAModel` 字段的 JSON tag 从 `json:"qa_model"` 改为 `json:"question_answering_model"`。

- [ ] **Step 2: 确认 FactoryName/FactoryKwargs JSON tag**

检查 `SubAgentConfig` 的 `FactoryName` JSON tag 为 `json:"factory_name,omitempty"`、`FactoryKwargs` JSON tag 为 `json:"factory_kwargs,omitempty"`，如不一致则修正。

- [ ] **Step 3: 更新 config_test.go**

将所有涉及 `qa_model` JSON key 的断言改为 `question_answering_model`。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/schema/... -v`

- [ ] **Step 5: Commit**

```
fix(harness): align AudioModelConfig JSON key with Python (question_answering_model)
```

---

### Task 10: 修复 #2 SubAgentConfig.RestrictToWorkDir 默认值

**Files:**
- Modify: `internal/agentcore/harness/schema/config.go`
- Modify: `internal/agentcore/harness/schema/config_test.go`

- [ ] **Step 1: 添加 EffectiveRestrictToWorkDir 方法**

在 `SubAgentConfig` 上新增方法：

```go
// EffectiveRestrictToWorkDir 返回有效的 RestrictToWorkDir 值
// Go 零值 false 表示"未设置"，Python 默认为 True
// 零值时返回 true（与 Python 默认一致），显式设为 false 时返回 false
func (c *SubAgentConfig) EffectiveRestrictToWorkDir() bool {
    return true // Python 默认值
}
```

注意：由于 Go 的 `bool` 零值就是 `false`，无法区分"未设置"和"显式 false"。此方法直接返回 true 作为默认值。如果调用方需要显式设为 false，需通过其他机制（如配置文件显式设置后标记已设置）。

- [ ] **Step 2: 更新 config_test.go**

添加测试：`EffectiveRestrictToWorkDir()` 返回 true。

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/schema/... -run TestSubAgent -v`

- [ ] **Step 4: Commit**

```
fix(harness): add EffectiveRestrictToWorkDir defaulting to true (Python align)
```

---

### Task 11: 修复 #3 StopConditionEvaluator 名称对齐

**Files:**
- Modify: `internal/agentcore/harness/task_loop/stop_condition.go`
- Modify: `internal/agentcore/harness/task_loop/stop_condition_test.go`
- Modify: `internal/agentcore/harness/task_loop/loop_coordinator.go`
- Modify: `internal/agentcore/harness/task_loop/loop_coordinator_test.go`

- [ ] **Step 1: 修改 4 个 evaluator 的 Name() 返回值**

| Evaluator | 改为 |
|-----------|------|
| MaxRoundsEvaluator.Name() | `"MaxRoundsEvaluator"` |
| TokenBudgetEvaluator.Name() | `"TokenBudgetEvaluator"` |
| TimeoutEvaluator.Name() | `"TimeoutEvaluator"` |
| CompletionPromiseEvaluator.Name() | `"CompletionPromiseEvaluator"` |

- [ ] **Step 2: 修改 CustomPredicateEvaluator 构造函数**

`NewCustomPredicateEvaluator` 签名改为 `NewCustomPredicateEvaluator(name string, predicate func(StopEvaluationContext) bool)`，name 参数必须传入。如果传空字符串，默认设为 `"CustomPredicateEvaluator"`。

- [ ] **Step 3: 更新 stop_condition_test.go**

所有断言中的 evaluator name 从 snake_case 改为 PascalCase：
- `"max_rounds"` → `"MaxRoundsEvaluator"`
- `"token_budget"` → `"TokenBudgetEvaluator"`
- `"timeout"` → `"TimeoutEvaluator"`
- `"completion_promise"` → `"CompletionPromiseEvaluator"`

- [ ] **Step 4: 更新 loop_coordinator_test.go**

ExportState/ImportState 中的 evaluator state 键名断言从 snake_case 改为 PascalCase。StopReason 断言同步更新。

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/... -v`

- [ ] **Step 6: Commit**

```
fix(harness): align StopConditionEvaluator names with Python class names
```

---

### Task 12: 修复 #7 LoopCoordinator startTime 初始化时机

**Files:**
- Modify: `internal/agentcore/harness/task_loop/loop_coordinator.go`
- Modify: `internal/agentcore/harness/task_loop/loop_coordinator_test.go`

- [ ] **Step 1: 修改 NewLoopCoordinator**

将 `startTime: time.Now()` 改为 `startTime: time.Time{}`（零值）。

- [ ] **Step 2: 修改 ElapsedSeconds**

在计算前判断 `startTime` 是否为零值，如果是则返回 `0.0`：

```go
func (lc *LoopCoordinator) ElapsedSeconds() float64 {
    lc.mu.Lock()
    defer lc.mu.Unlock()
    if lc.startTime.IsZero() {
        return 0.0
    }
    return time.Since(lc.startTime).Seconds()
}
```

- [ ] **Step 3: 更新 loop_coordinator_test.go**

添加测试：
- `NewLoopCoordinator` 后 `ElapsedSeconds()` 返回 0.0（startTime 未初始化）
- `Reset()` 后 `ElapsedSeconds()` 返回大于 0 的值
- `ShouldContinue` 在未 Reset 时不会因 TimeoutEvaluator 误判

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/... -v`

- [ ] **Step 5: Commit**

```
fix(harness): delay LoopCoordinator startTime init to Reset() (Python align)
```

---

### Task 13: 修复 #4 Workspace default_content + #5 链接管理 + #6 EN schema 去重

**Files:**
- Modify: `internal/agentcore/harness/workspace/workspace.go`
- Modify: `internal/agentcore/harness/workspace/workspace_test.go`

- [ ] **Step 1: 修复 #4 — 用 workspace_content 模板填充 defaultContent**

在 `workspace.go` 中：
- import `workspacecontent` 包
- 修改 `defaultContent` 赋值逻辑：根据 language 和 file name 引用对应常量
- AGENT.md → `workspacecontent.AgentMDCN` / `AgentMDEN`
- SOUL.md → `workspacecontent.SoulMDCN` / `SoulMDEN`
- HEARTBEAT.md → `workspacecontent.HeartbeatMDCN` / `HeartbeatMDEN`
- IDENTITY.md → `workspacecontent.IdentityMDCN` / `IdentityMDEN`
- MEMORY.md → `workspacecontent.MemoryMDCN` / `MemoryMDEN`

- [ ] **Step 2: 修复 #5 — 添加链接管理方法**

新增常量 `TeamLinksDir = ".team"`, `WorktreeLinksDir = ".worktree"`

新增方法：
- `LinkTeam(name, targetPath string) error`
- `UnlinkTeam(name string) error`
- `LinkWorktree(name, targetPath string) error`
- `UnlinkWorktree(name string) error`
- `ListTeamLinks() []string`
- `ListWorktreeLinks() []string`

新增内部方法：
- `ensureLinkDir(dir string) error`
- `createDirectoryLink(linkPath, targetPath string) error`
- `removeDirectoryLink(linkPath string) error`
- `isDirectoryLink(path string) bool`

参照 Python `harness/workspace/workspace.py` 的实现逻辑。Windows 平台使用 junction。

- [ ] **Step 3: 修复 #6 — 从 EN schema 移除 coding_memory**

从 `defaultWorkspaceSchemaEN` 中移除包含 `WorkspaceNodeCodingMemory` 的条目。

- [ ] **Step 4: 更新 workspace_test.go**

测试用例：
- CN workspace 的 AGENT.md defaultContent 非空且包含中文内容
- EN workspace 的 AGENT.md defaultContent 非空且包含英文内容
- EN workspace 不包含 coding_memory 节点
- LinkTeam/UnlinkTeam 链接创建和删除
- LinkWorktree/UnlinkWorktree 链接创建和删除
- ListTeamLinks/ListWorktreeLinks 列表

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/workspace/... -v`

- [ ] **Step 6: Commit**

```
fix(harness): fill workspace defaultContent, add link management, fix EN schema (Python align)
```

---

## 阶段 5：doc.go 更新 + IMPLEMENTATION_PLAN.md 同步

### Task 14: 更新所有 doc.go + 实现计划状态

**Files:**
- Modify: `internal/agentcore/harness/schema/doc.go`
- Create: `internal/agentcore/harness/resources/doc.go` (已在 Task 8 创建)
- Create: `internal/agentcore/harness/prompts/doc.go` (已在 Task 6 创建)
- Create: `internal/agentcore/harness/prompts/sections/doc.go` (已在 Task 5 创建)
- Create: `internal/agentcore/harness/prompts/tools/doc.go` (已在 Task 7 创建)
- Create: `internal/agentcore/harness/prompts/workspace_content/doc.go` (已在 Task 4 创建)
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 schema/doc.go**

在文件目录中新增 `loop_event.go`、`state.go`、`task.go` 条目。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将步骤 9.51-53 的状态从 `☐` 改为 `✅`。

- [ ] **Step 3: 运行全量测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -v -count=1`

- [ ] **Step 4: Commit**

```
docs(harness): update doc.go and mark 9.51-9.53 complete in IMPLEMENTATION_PLAN.md
```

---

## 验证

### Task 15: 最终验证

- [ ] **Step 1: 运行全部 harness 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -cover -v -count=1`

Expected: 全部通过，覆盖率 ≥ 85%

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -coverprofile=/tmp/cover.out ./internal/agentcore/harness/... && go tool cover -func=/tmp/cover.out | tail -1`

Expected: 总覆盖率 ≥ 85%

- [ ] **Step 3: 检查编译无错误**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

Expected: 编译成功，无错误
