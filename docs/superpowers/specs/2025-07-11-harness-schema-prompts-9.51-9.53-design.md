# 9.51-9.53 Harness 资源/Schema/Prompts 实现设计

> 实现计划步骤 9.51（资源管理）、9.52（Schema 补全）、9.53（提示词模板），同时修复 8 项已有实现与 Python 的不一致问题。

## 1. 范围

### 1.1 本次实现内容

| 步骤 | 内容 | Python 对应 | Go 目标位置 |
|------|------|------------|------------|
| 9.51 | 资源管理 | `harness/resources/` | `internal/agentcore/harness/resources/` |
| 9.52 | Schema 补全 | `harness/schema/`（loop_event/state/task） | `internal/agentcore/harness/schema/` |
| 9.53 | 提示词模板 | `harness/prompts/`（builder/sections/tools/workspace_content） | `internal/agentcore/harness/prompts/` |

### 1.2 一致性修复

修复已有 Go 实现与 Python 源码的 8 项不一致（3 HIGH + 3 MEDIUM + 2 确认）。

### 1.3 不包含

- 9.4 TaskLoopController（后续步骤）
- 9.38-49 Harness 工具集功能实现（tools/ 仅含提示词模板，不含工具执行逻辑）
- 9.1 DeepAgent 主体
- 9.8-9.24 Rails 实现

## 2. 9.51 资源管理

### 2.1 目录结构

```
internal/agentcore/harness/resources/
├── doc.go
└── builtin_rules.yaml
```

### 2.2 builtin_rules.yaml

完整移植 Python `harness/resources/builtin_rules.yaml` 的 10 条 CRITICAL 安全规则：

| 规则 ID | 描述 | 严重度 | action |
|---------|------|--------|--------|
| shell_fs_recursive_or_forced_delete | 递归/强制删除关键路径 | CRITICAL | （默认） |
| shell_disk_partition_or_raw_device_write | 格式化/分区/裸设备写入 | CRITICAL | （默认） |
| shell_download_and_execute | 下载后管道到 shell | CRITICAL | （默认） |
| shell_obfuscated_or_dynamic_execution | Base64 解码执行/eval/iex | CRITICAL | （默认） |
| shell_reverse_shell_or_bind_shell | 反向/绑定 shell | CRITICAL | （默认） |
| shell_privilege_escalation | sudo/su/doas/pkexec | CRITICAL | （默认） |
| shell_data_exfiltration | curl/wget/scp/rsync 上传 | CRITICAL | （默认） |
| shell_remote_execution_or_lateral_movement | Invoke-Command/Enter-PSSession | CRITICAL | （默认） |
| shell_fork_bomb_or_resource_abuse | Fork 炸弹/kill -9 -1 | CRITICAL | （默认） |
| shell_system_shutdown_or_reboot | shutdown/reboot/halt | CRITICAL | **deny** |

所有规则 target tools: `[bash, mcp_exec_command, create_terminal]`，match_type: command，patterns 使用 `re:` 前缀正则。

## 3. 9.52 Schema 补全

### 3.1 新增文件

目录：`internal/agentcore/harness/schema/`

#### 3.1.1 loop_event.go — 外层循环事件

对应 Python `schema/loop_event.py`。

**DeepLoopEventType 枚举：**

| 常量 | 值 | 优先级 |
|------|-----|--------|
| DeepLoopEventTypeFollowup | iota 0 → JSON "followup" | 10 |
| DeepLoopEventTypeSteer | iota 1 → JSON "steer" | 1 |
| DeepLoopEventTypeAbort | iota 2 → JSON "abort" | 0 |

实现 `String()`、`MarshalJSON()`、`UnmarshalJSON()`、`ParseDeepLoopEventType()` 方法（与 AgentMode 模式一致）。

**DeepLoopEvent 结构体：**

```go
type DeepLoopEvent struct {
    Priority  int                    `json:"priority"`
    Seq       int                    `json:"seq"`
    CreatedAt float64                `json:"created_at"`
    EventID   string                 `json:"event_id"`
    EventType DeepLoopEventType      `json:"event_type"`
    Content   string                 `json:"content"`
    TaskID    string                 `json:"task_id,omitempty"`
    Metadata  map[string]any         `json:"metadata,omitempty"`
}
```

- 实现 `Less(other DeepLoopEvent) bool` 方法（按 priority 升序，同 priority 按 seq 升序），用于优先队列排序
- `CreatedAt` 使用 `time.Now().UnixNano()` 初始化（Go 无 time.monotonic 直接导出，用 UnixNano 替代）
- `EventID` 使用 `uuid.New().String()` 初始化

**导出函数：**

- `defaultEventPriority(eventType DeepLoopEventType) int`
- `createLoopEvent(seq int, eventType DeepLoopEventType, content string, opts ...LoopEventOption) DeepLoopEvent`

使用 Functional Options 模式支持可选的 taskID、metadata、priority 参数。

#### 3.1.2 state.go — 运行时可变状态

对应 Python `schema/state.py`。

**PlanModeState 结构体：**

```go
type PlanModeState struct {
    Mode          string `json:"mode"`            // 默认 "normal"
    PrePlanMode   string `json:"pre_plan_mode"`   // 默认 "normal"
    PlanSlug      string `json:"plan_slug,omitempty"`
    PromptContext string `json:"prompt_context,omitempty"`
}
```

方法：`ToDict() map[string]any`、`FromDict(data map[string]any) PlanModeState`（nil 输入返回默认值）

**DeepAgentState 结构体：**

```go
type DeepAgentState struct {
    Iteration           int              `json:"iteration"`
    TaskPlan            *TaskPlan        `json:"task_plan,omitempty"`
    StopConditionState  map[string]any   `json:"stop_condition_state,omitempty"`
    PendingFollowUps    []string         `json:"pending_follow_ups,omitempty"`
    PlanMode            PlanModeState    `json:"plan_mode"`
}
```

方法：`ToSessionDict() map[string]any`、`FromSessionDict(data map[string]any) DeepAgentState`

#### 3.1.3 task.go — 任务计划与待办项

对应 Python `schema/task.py`。

**TodoStatus 枚举：**

| 常量 | 值 | 图标 |
|------|-----|------|
| TodoStatusPending | iota 0 → JSON "pending" | `[ ]` |
| TodoStatusInProgress | iota 1 → JSON "in_progress" | `[→]` |
| TodoStatusCompleted | iota 2 → JSON "completed" | `[√]` |
| TodoStatusCancelled | iota 3 → JSON "cancelled" | `[×]` |

**STATUS_ICONS：** `map[TodoStatus]string` 全局变量。

**TodoItem 结构体：**

```go
type TodoItem struct {
    ID              string         `json:"id"`
    Content         string         `json:"content"`
    ActiveForm      string         `json:"active_form"`
    Description     string         `json:"description"`
    Status          TodoStatus     `json:"status"`
    DependsOn       []string       `json:"depends_on,omitempty"`
    ResultSummary   string         `json:"result_summary,omitempty"`
    MetaData        map[string]any `json:"meta_data,omitempty"`
    SelectedModelID string         `json:"selected_model_id,omitempty"`
}
```

方法：`ToDict() map[string]any`、`FromDict(data map[string]any) TodoItem`

**TaskPlan 结构体：**

```go
type TaskPlan struct {
    Goal         string     `json:"goal"`
    Tasks        []TodoItem `json:"tasks,omitempty"`
    CurrentTaskID string   `json:"current_task_id,omitempty"`
}
```

方法：
- `GetTask(taskID string) *TodoItem`
- `GetNextTask() *TodoItem` — 返回第一个 Pending 且依赖已满足的 task
- `AddTask(task TodoItem)`
- `MarkInProgress(taskID string) error`
- `MarkCompleted(taskID string, summary string) error`
- `MarkCancelled(taskID string, reason string) error`
- `GetProgressSummary() string` — 如 "3/7 completed"
- `ToMarkdown() string` — 渲染为 markdown checklist
- `ToDict() map[string]any`
- `FromDict(data map[string]any) TaskPlan`

**ModelUsageRecord 结构体：**

```go
type ModelUsageRecord struct {
    ModelID      string `json:"model_id"`
    InputTokens  int    `json:"input_tokens"`
    OutputTokens int    `json:"output_tokens"`
}
```

方法：`Add(inputTokens, outputTokens int)`、`String() string`

### 3.2 修改文件

- `schema/doc.go` — 更新文件目录列表，新增 loop_event.go、state.go、task.go

## 4. 9.53 提示词模板

### 4.1 整体结构（与 Python 一致的三包拆分）

```
internal/agentcore/harness/prompts/
├── doc.go
├── builder.go              # SystemPromptBuilder 扩展版
├── builder_test.go
├── report.go               # PromptReport
├── report_test.go
├── sanitize.go             # 注入防御
├── sanitize_test.go
├── sections/               # Section 构建函数
│   ├── doc.go
│   ├── section_name.go     # SectionName 常量
│   ├── identity.go
│   ├── safety.go
│   ├── context.go
│   ├── skills.go
│   ├── memory.go
│   ├── external_memory.go
│   ├── workspace.go
│   ├── progressive_tool_rail.go
│   ├── heartbeat.go
│   ├── coding_memory.go
│   ├── session_tools.go
│   ├── agent_mode.go
│   ├── task_tool.go
│   ├── task_completion.go
│   ├── todo.go
│   └── reload.go
├── tools/                  # 工具级提示词
│   ├── doc.go
│   ├── base.go             # ToolMetadataProvider 接口
│   ├── registry.go         # 全局注册表 + BuildToolCard
│   ├── agent_mode.go
│   ├── ask_user.go
│   ├── audio.go
│   ├── bash.go
│   ├── code.go
│   ├── coding_memory.go
│   ├── cron.go
│   ├── enter_worktree.go
│   ├── exit_worktree.go
│   ├── filesystem.go
│   ├── list_skill.go
│   ├── load_tools.go
│   ├── lsp_tool.go
│   ├── mcp.go
│   ├── memory.go
│   ├── powershell.go
│   ├── search_tools.go
│   ├── session_tools.go
│   ├── skill_tool.go
│   ├── task_tool.go
│   ├── todo.go
│   ├── video_understanding.go
│   ├── vision.go
│   └── web_tools.go
└── workspace_content/      # 双语模板常量
    ├── doc.go
    ├── workspace_header.go
    ├── template_identity.go
    ├── template_agent.go
    ├── template_soul.go
    ├── template_heartbeat.go
    ├── template_session_memory.go
    └── template_memory.go
```

### 4.2 顶层包 — harness/prompts/

#### builder.go

**SystemPromptBuilder** 扩展 `single_agent/prompts.SystemPromptBuilder`：

```go
type SystemPromptBuilder struct {
    *saprompt.SystemPromptBuilder        // 嵌入基类
    mode                          PromptMode
}
```

- `MinimalSections` 常量集合 = `{IDENTITY, SAFETY, SKILLS, TOOLS, RUNTIME, MEMORY}`
- `NewSystemPromptBuilder(language string, mode PromptMode) *SystemPromptBuilder`
- `Build() string`：
  - None 模式：仅返回 identity section
  - Minimal 模式：仅包含 MinimalSections 中的 section
  - Full 模式：返回全部 section（委托给基类）
- `BuildReport() *PromptReport`

**包级函数：**

- `resolveLanguage(configLanguage string) string` — 优先级：config 参数 > `AGENT_PROMPT_LANGUAGE` 环境变量 > `DEFAULT_LANGUAGE`("cn")
- `resolveMode(configMode string) PromptMode`

**包级常量：**

- `SUPPORTED_LANGUAGES = []string{"cn", "en"}`
- `DEFAULT_LANGUAGE = "cn"`

#### report.go

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
```

- `NewPromptReport(builder *SystemPromptBuilder) *PromptReport` — token 估算：CN 2.5 chars/token，EN 4.0 chars/token
- `ToDict() map[string]any`
- `Summary() string` — 一行摘要

#### sanitize.go

- `sanitizePath(path string) string` — 剥离 `<>{[]}$`、`...`、`\n`、`\r` 等注入字符
- `sanitizeUserContent(content string, maxLen int) string` — 剥离注入字符 + 截断长度

### 4.3 sections 包 — harness/prompts/sections/

#### section_name.go

```go
const (
    SectionIdentity            = "identity"
    SectionSafety              = "safety"
    SectionSkills              = "skills"
    SectionTools               = "tools"
    SectionTodo                = "todo"
    SectionTaskTool            = "task_tool"
    SectionToolNavigation      = "tool_navigation"
    SectionProgressiveToolRules = "progressive_tool_rules"
    SectionRuntime             = "runtime"
    SectionMemory              = "memory"
    SectionSessionTools        = "session_tools"
    SectionModeInstructions    = "mode_instructions"
    SectionWorkspace           = "workspace"
    SectionHeartbeat           = "heartbeat"
    SectionContext             = "context"
    SectionExternalMemory      = "external_memory"
    SectionCompletionSignal    = "completion_signal"
    SectionVerificationContract = "verification_contract"
)
```

#### Section 文件规范

每个 section 文件：
- 导出 `BuildXxxSection(...) prompts.PromptSection` 函数
- CN/EN 双语模板文本作为包级 `const` 字面量，**原样从 Python 复制**
- 模板变量用 `{variable}` 占位符
- 需要动态数据的 section 通过函数参数传入

| 文件 | 导出函数签名 | 说明 |
|------|-------------|------|
| identity.go | `BuildIdentitySection() prompts.PromptSection` | 无参数，固定文本 |
| safety.go | `BuildSafetySection() prompts.PromptSection` | 无参数，固定文本 |
| context.go | `BuildToolsSection(toolDescs string, lang string) prompts.PromptSection` + `BuildContextSection(files map[string]string, lang string) prompts.PromptSection` | 工具清单+上下文文件 |
| skills.go | `BuildSkillsSection(mode string, skillPaths []string, lang string) prompts.PromptSection` | all/auto_list/no_skill 三模式 |
| memory.go | `BuildMemorySection(mode string, lang string) prompts.PromptSection` | proactive/inactive/read_only 三模式 |
| external_memory.go | `BuildExternalMemorySection(promptBlock string, lang string) prompts.PromptSection` | passthrough |
| workspace.go | `BuildWorkspaceSection(rootPath string, dirTree string, lang string) prompts.PromptSection` | 工作空间目录树 |
| progressive_tool_rail.go | `BuildNavigationSection(entries []string, lang string) prompts.PromptSection` + `BuildProgressiveToolRulesSection(lang string) prompts.PromptSection` | 导航+规则 |
| heartbeat.go | `BuildHeartbeatSection(heartbeatContent string, lang string) prompts.PromptSection` | 心跳任务 |
| coding_memory.go | `BuildCodingMemorySection(memoryDir string, readOnly bool, lang string) prompts.PromptSection` | 编码记忆 |
| session_tools.go | `BuildSessionToolsSection(lang string) prompts.PromptSection` | 会话工具 |
| agent_mode.go | `BuildPlanModeSection(enterStatus string, planFileInfo string, lang string) prompts.PromptSection` | Plan 模式指令 |
| task_tool.go | `BuildTaskToolSection(lang string) prompts.PromptSection` | 子代理委派 |
| task_completion.go | `BuildCompletionSignalSection(promise string, lang string) prompts.PromptSection` | 完成信号 |
| todo.go | `BuildTodoSection(lang string) prompts.PromptSection` | 待办跟踪 |
| reload.go | `BuildReloadSection(lang string) prompts.PromptSection` | 上下文压缩 |

### 4.4 tools 包 — harness/prompts/tools/

#### base.go — ToolMetadataProvider 接口

```go
// ToolMetadataProvider 工具元数据提供者接口
type ToolMetadataProvider interface {
    GetName() string
    GetDescription(language string) string
    GetInputParams(language string) map[string]any
}

// ValidateToolMetadata 校验工具元数据的双语完整性
func ValidateToolMetadata(provider ToolMetadataProvider) error
```

#### registry.go — 全局注册表

```go
var (
    providers []ToolMetadataProvider
    registry  map[string]ToolMetadataProvider
)

// BuildToolCard 从注册表查找 provider 并组装 ToolCard
func BuildToolCard(name string, toolID string, language string, opts ...ToolCardOption) (*tool.ToolCard, error)

// BuildToolsSection 将工具描述列表组装为 PromptSection
func BuildToolsSection(toolDescriptions []string, language string) prompts.PromptSection

// RegisterToolProvider 动态注册工具元数据提供者
func RegisterToolProvider(provider ToolMetadataProvider)
```

#### 工具文件规范

每个工具文件：
- 导出 `DESCRIPTION map[string]string`（CN/EN 双语描述文本）
- 导出参数 Schema 构建函数 `GetXxxInputParams(language string) map[string]any`
- 导出 `XxxMetadataProvider` 结构体实现 `ToolMetadataProvider` 接口
- 所有提示词文本**原样从 Python 复制**

### 4.5 workspace_content 包 — harness/prompts/workspace_content/

#### workspace_header.go

12 组双语常量：
- `WorkspaceHeaderCN/EN`
- `ImportantFilesCN/EN`
- `ContextHeaderCN/EN`
- `ContextFileTitlesCN/EN`（6 个文件的标题映射）
- `DailyMemoryTitleCN/EN`（含 `{date}` 占位符）
- `DirectoryDescriptionsCN/EN`（12 个目录的描述映射）
- `ContextFiles`（固定列表：AGENT.md, SOUL.md, HEARTBEAT.md, USER.md, IDENTITY.md）

#### 模板文件

| 文件 | 导出常量 |
|------|---------|
| template_identity.go | `IdentityMDCN`, `IdentityMDEN` |
| template_agent.go | `AgentMDCN`, `AgentMDEN` |
| template_soul.go | `SoulMDCN`, `SoulMDEN` |
| template_heartbeat.go | `HeartbeatMDCN`, `HeartbeatMDEN` |
| template_session_memory.go | `SessionMemoryMDCN`, `SessionMemoryMDEN` |
| template_memory.go | `MemoryMDCN`, `MemoryMDEN` |

所有模板文本原样从 Python 对应文件复制。

## 5. 一致性修复

### 5.1 🔴 HIGH — AudioModelConfig JSON key

**文件：** `harness/schema/config.go`

**改动：** `AudioModelConfig.QAModel` 的 JSON tag 从 `json:"qa_model"` 改为 `json:"question_answering_model"`

**影响：** `config_test.go` 中 JSON 序列化/反序列化断言需同步更新

### 5.2 🔴 HIGH — SubAgentConfig.RestrictToWorkDir 默认值

**文件：** `harness/schema/config.go`

**改动：**
- `RestrictToWorkDir` 保持 `bool` 值类型
- `NewSubAgentConfig()` 构造函数中设置 `RestrictToWorkDir: true`
- 新增 `EffectiveRestrictToWorkDir() bool` 方法：若字段为零值（false），返回 true（Python 默认值）

**注意：** 这里用 "零值表示未设置" 的约定：Go 中 `false` 是零值，但 Python 默认是 `True`。Effective 方法在零值时返回 Python 默认值 true，显式设为 false 时返回 false。

### 5.3 🔴 HIGH — StopConditionEvaluator 名称对齐

**文件：** `harness/task_loop/stop_condition.go`

**改动：**

| Evaluator | 当前 Name() | 修改为 |
|-----------|-------------|--------|
| MaxRoundsEvaluator | `"max_rounds"` | `"MaxRoundsEvaluator"` |
| TokenBudgetEvaluator | `"token_budget"` | `"TokenBudgetEvaluator"` |
| TimeoutEvaluator | `"timeout"` | `"TimeoutEvaluator"` |
| CompletionPromiseEvaluator | `"completion_promise"` | `"CompletionPromiseEvaluator"` |
| CustomPredicateEvaluator | 用户传入 name | 构造函数签名改为 `NewCustomPredicateEvaluator(name string, predicate func(StopEvaluationContext) bool)`，默认 name 为 `"CustomPredicateEvaluator"` |

**影响：** `loop_coordinator.go` 的 `ExportState`/`ImportState` 键名变化、`StopReason()` 返回值变化、所有相关测试需更新

### 5.4 🔴 HIGH — Workspace default_content 填充

**文件：** `harness/workspace/workspace.go`

**改动：** `defaultContent` 字段不再硬编码空字符串，改为引用 `workspace_content` 包的模板常量：

- `AGENT.md` → `workspacecontent.AgentMDCN` / `AgentMDEN`
- `SOUL.md` → `workspacecontent.SoulMDCN` / `SoulMDEN`
- `HEARTBEAT.md` → `workspacecontent.HeartbeatMDCN` / `HeartbeatMDEN`
- `IDENTITY.md` → `workspacecontent.IdentityMDCN` / `IdentityMDEN`
- `MEMORY.md` → `workspacecontent.MemoryMDCN` / `MemoryMDEN`

**依赖：** 需先完成 9.53 的 workspace_content 包

### 5.5 🟡 MEDIUM — Workspace 链接管理方法

**文件：** `harness/workspace/workspace.go`

**新增常量：**

```go
const (
    TeamLinksDir     = ".team"
    WorktreeLinksDir = ".worktree"
)
```

**新增方法：**

| 方法 | 签名 | 说明 |
|------|------|------|
| LinkTeam | `(name string, targetPath string) error` | 创建 .team/{name} 符号链接 |
| UnlinkTeam | `(name string) error` | 删除 .team/{name} 链接 |
| LinkWorktree | `(name string, targetPath string) error` | 创建 .worktree/{name} 符号链接 |
| UnlinkWorktree | `(name string) error` | 删除 .worktree/{name} 链接 |
| ListTeamLinks | `() []string` | 列出 .team/ 下所有链接 |
| ListWorktreeLinks | `() []string` | 列出 .worktree/ 下所有链接 |

**新增内部方法：** `ensureLinkDir`、`createDirectoryLink`、`removeDirectoryLink`、`isDirectoryLink`

Windows 平台使用 junction 代替 symlink（`createWindowsJunction`）。

### 5.6 🟡 MEDIUM — Workspace EN schema 去除 coding_memory

**文件：** `harness/workspace/workspace.go`

**改动：** 从 `defaultWorkspaceSchemaEN` 中移除 `WorkspaceNodeCodingMemory` 节点，与 Python 保持一致

### 5.7 🟡 MEDIUM — LoopCoordinator startTime 初始化时机

**文件：** `harness/task_loop/loop_coordinator.go`

**改动：**
- `NewLoopCoordinator` 不再设置 `startTime: time.Now()`，改为 `startTime: time.Time{}`（零值）
- `Reset()` 中设置 `startTime: time.Now()`
- `ElapsedSeconds()` 中判断：若 `startTime` 为零值则返回 `0.0`

### 5.8 🟡 MEDIUM — SubAgentConfig.factory 字段确认

**文件：** `harness/schema/config.go`

**改动：** 确认 `FactoryName` 和 `FactoryKwargs` 的 JSON tag 与 Python 一致：
- `FactoryName` → `json:"factory_name,omitempty"`
- `FactoryKwargs` → `json:"factory_kwargs,omitempty"`

## 6. 实现顺序

```
阶段 1：Schema 基础（9.52）
  ├── loop_event.go + test
  ├── state.go + test
  └── task.go + test

阶段 2：Prompts 基础（9.53 底层）
  ├── workspace_content/ 全部文件
  ├── sections/section_name.go + 全部 section 文件
  ├── prompts/ 顶层（builder/report/sanitize）
  └── tools/ 全部文件

阶段 3：资源（9.51）
  └── resources/builtin_rules.yaml

阶段 4：一致性修复（依赖阶段 2 的 workspace_content）
  ├── config.go（#1 JSON key + #2 默认值 + #8 factory 确认）
  ├── stop_condition.go（#3 evaluator 名称）
  ├── loop_coordinator.go（#7 startTime）
  ├── workspace.go（#4 default_content + #5 链接管理 + #6 EN schema）
  └── 所有受影响测试的更新

阶段 5：doc.go 更新
  └── 所有新增/修改包的 doc.go 同步
```

## 7. 测试要求

- 每个新增 .go 文件必须有对应 _test.go
- 枚举类型：JSON 往返测试、Parse 测试、无效输入测试
- 结构体：序列化往返测试（ToDict/FromDict 或 JSON marshal/unmarshal）
- TaskPlan：依赖排序、状态变更、边界情况测试
- SystemPromptBuilder：Full/Minimal/None 三种模式构建测试
- PromptReport：token 估算准确性测试
- sanitize：注入攻击测试、长度截断测试
- ToolMetadataProvider：Validate 双语完整性测试
- 一致性修复：每个修复项需有对应测试覆盖
- 整体覆盖率目标 ≥ 85%
