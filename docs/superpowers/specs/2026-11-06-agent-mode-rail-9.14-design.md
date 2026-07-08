# 9.14 AgentModeRail 设计文档

## 1. 概述

实现 9.14 AgentModeRail，即 Agent 模式切换 Rail。这是 Plan 模式的三层防御系统，负责：
1. 注册 switch_mode / enter_plan_mode / exit_plan_mode 三个工具
2. Plan 模式下注入 MODE_INSTRUCTIONS 提示段，移除 Todo/Session 段，过滤隐藏工具
3. BeforeToolCall 三段式拦截（enter/exit 校验 → 非 plan 放行 → plan 白名单+路径+git 拦截）
4. AfterToolCall 动态注册/注销 task_tool

对齐 Python: `openjiuwen/harness/rails/agent_mode_rail.py` (552 行)

## 2. 在 Agent 会话流程中的位置

```
优先级排序（数值越小越先执行）：
  ProgressiveToolRail  →  priority=70  ✅ (9.11)
  TaskCompletionRail   →  priority=80  ✅ (9.12)
  AgentModeRail        →  priority=85  ☐ (9.14) ← 本实现
  TaskPlanningRail     →  priority=90  ✅ (9.13)
```

AgentModeRail 的 BeforeModelCall 会在 Plan 模式下移除 TaskPlanningRail(priority=90) 注入的 TODO 段。

## 3. 前置依赖

| 依赖项 | 状态 | 说明 |
|--------|------|------|
| 9.11 ProgressiveToolRail | ✅ | 已完成 |
| 9.12 TaskCompletionRail | ✅ | 已完成 |
| 9.13 TaskPlanningRail | ✅ | 已完成 |
| AgentMode 枚举 | ✅ | `schema/agent_mode.go` |
| PlanModeState / DeepAgentState | ✅ | `schema/state.go` |
| Plan 模式提示词段落 | ✅ | `prompts/sections/agent_mode.go` (与 Python 1:1 对齐) |
| 工具元数据 Provider | ✅ | `prompts/tools/agent_mode.go` (与 Python 1:1 对齐) |
| SwitchMode / RestoreModeAfterPlanExit / GetPlanFilePath | ✅ | `deep_agent.go` |
| TaskPlanningRail 的 todo 工具 | ✅ | `tools/todo/todo.go` |

## 4. 实现范围

由于选择方案 B（在 9.14 中一并实现工具逻辑），需要新建的文件：

### 4.1 AgentModeRail 主体

| 文件 | 职责 | 对齐 Python |
|------|------|-------------|
| `rails/agent_mode.go` | AgentModeRail 结构体 + 4 个回调钩子 | `agent_mode_rail.py` |
| `rails/agent_mode_test.go` | 单元测试 | — |

### 4.2 Agent Mode 工具执行逻辑

| 文件 | 职责 | 对齐 Python |
|------|------|-------------|
| `tools/agent_mode/doc.go` | 包文档 | — |
| `tools/agent_mode/switch_mode.go` | SwitchModeTool invoke 逻辑 | `agent_mode_tools.py` SwitchModeTool |
| `tools/agent_mode/enter_plan_mode.go` | EnterPlanModeTool invoke 逻辑 | `agent_mode_tools.py` EnterPlanModeTool |
| `tools/agent_mode/exit_plan_mode.go` | ExitPlanModeTool invoke 逻辑 | `agent_mode_tools.py` ExitPlanModeTool |
| `tools/agent_mode/slug.go` | generateWordSlug + resolvePlanFilePath + getOrCreatePlanSlug | `agent_mode_tools.py` L51-112 |
| `tools/agent_mode/slug_test.go` | slug 生成测试 | — |
| `tools/agent_mode/tool_test.go` | 工具 invoke 测试 | — |

### 4.3 Task Tool 动态注册

| 文件 | 职责 | 对齐 Python |
|------|------|-------------|
| `tools/subagent/task_tool.go` | TaskTool + CreateTaskTool | `tools/subagent/task_tool.py` |
| `tools/subagent/task_tool_test.go` | TaskTool 测试 | — |

### 4.4 需要更新的已有文件

| 文件 | 变更 |
|------|------|
| `rails/doc.go` | 添加 agent_mode.go 条目 |
| `tools/subagent/doc.go` | 添加 task_tool.go 条目 |

## 5. 详细设计

### 5.1 AgentModeRail 结构体

```go
type AgentModeRail struct {
    DeepAgentRail
    // allowedTools plan 模式白名单工具集合
    allowedTools map[string]struct{}
    // ownsTaskTool 是否拥有动态注册的 task_tool
    ownsTaskTool bool
    // taskTools 动态注册的 task_tool 实例列表
    taskTools []tool.Tool
    // ownedTaskToolNames 拥有的 task_tool 名称集合
    ownedTaskToolNames map[string]struct{}
    // tools 本 Rail 注册的工具列表（switch_mode/enter/exit）
    tools []tool.Tool
    // systemPromptBuilder 系统提示词构建器
    systemPromptBuilder saprompt.SystemPromptBuilderInterface
    // agent DeepAgent 引用
    agent hinterfaces.DeepAgentInterface
}
```

优先级：85（对齐 Python `AgentModeRail.priority = 85`）

### 5.2 回调钩子

#### 5.2.1 Init(agent)

对齐 Python: `AgentModeRail.init()` L105-124

- 将 agent 断言为 DeepAgentInterface，保存引用
- 获取 systemPromptBuilder 和 language
- 创建 SwitchModeTool / EnterPlanModeTool / ExitPlanModeTool
- 注册到 ability_manager

#### 5.2.2 Uninit(agent)

对齐 Python: `AgentModeRail.uninit()` L132-150

- 注销 3 个工具
- 若 ownsTaskTool 则注销 task_tool

#### 5.2.3 BeforeModelCall(ctx, cbc)

对齐 Python: `AgentModeRail.before_model_call()` L151-196

**非 plan 模式**：
1. 移除 MODE_INSTRUCTIONS 段
2. 从 ctx.inputs.tools 中过滤掉 `hiddenInNormal`（enter_plan_mode / exit_plan_mode）
3. 同步 task_tool 可见性

**plan 模式**：
1. 构建 MODE_INSTRUCTIONS 段（调用 BuildPlanModeSection）
2. 注入到 systemPromptBuilder
3. 移除 TODO 段和 SESSION_TOOLS 段
4. 从 ctx.inputs.tools 中过滤掉 `hiddenInPlan`（todo + session 工具）
5. 同步 task_tool 可见性

#### 5.2.4 BeforeToolCall(ctx, cbc) — 三段式

对齐 Python: `AgentModeRail.before_tool_call()` L232-329

**段 1**：enter_plan_mode / exit_plan_mode
- enter：校验当前已在 plan 模式（否则拒绝）
- exit：校验当前在 plan 模式（否则拒绝）

**段 2**：非 plan 模式 → 直接放行

**段 3**：plan 模式
- 3a. 硬拦截 todo/session 工具
- 3b. 白名单检查（不在 allowedTools 则拒绝）
- 3c. bash 工具 → git 写操作正则拦截
- 3d. write_file / edit_file → 路径必须为计划文件

#### 5.2.5 AfterToolCall(ctx, cbc)

对齐 Python: `AgentModeRail.after_tool_call()` L331-344

- enter_plan_mode 成功 → _registerTaskTool
- exit_plan_mode 成功 → _unregisterTaskTool

### 5.3 工具名常量

对齐 Python L44-56：

```go
var (
    todoToolNames    = map[string]struct{}{"todo_create": {}, "todo_list": {}, "todo_modify": {}}
    sessionToolNames = map[string]struct{}{"sessions_list": {}, "sessions_cancel": {}, "sessions_spawn": {}}
    hiddenInPlan     // = todoToolNames ∪ sessionToolNames
    hiddenInNormal   = map[string]struct{}{"enter_plan_mode": {}, "exit_plan_mode": {}}
    planFileWriteTools = map[string]struct{}{"write_file": {}, "edit_file": {}}
)
```

### 5.4 Git 写操作正则

对齐 Python L53-56：

```go
var gitWriteRE = regexp.MustCompile(
    `\bgit\s+(add|commit|push|pull|reset\s+--hard|checkout\s+--\.|clean\s+-[a-zA-Z]*f|` +
    `stash\s+(drop|clear)|branch\s+-D|merge|tag|amend|rebase)\b`)
```

### 5.5 Plan 模式白名单

对齐 Python L58-71：

```go
var defaultPlanModeAllowedTools = map[string]struct{}{
    "switch_mode": {}, "enter_plan_mode": {}, "exit_plan_mode": {},
    "ask_user": {}, "task_tool": {}, "read_file": {}, "grep": {},
    "list_files": {}, "glob": {}, "bash": {}, "write_file": {}, "edit_file": {},
}
```

### 5.6 SwitchModeTool invoke

对齐 Python: `SwitchModeTool.invoke()` L224-256

- 解析 mode 参数，校验 normal/plan
- 调用 agent.SwitchMode(sess, mode)
- 返回中英文消息：
  - 无效模式：`"无效模式 '{mode}'。支持模式：normal、plan。"` / `"Invalid mode '{mode}'. Supported modes: plan, normal."`
  - 切到 normal：`"已切换为 normal 模式。"` / `"Switched mode to normal."`
  - 切到 plan：`"已切换为 plan 模式。\n下一步：调用 enter_plan_mode 继续 Plan 工作流。"` / `"Switched mode to plan.\nNext step: call enter_plan_mode to continue the plan workflow."`

### 5.7 EnterPlanModeTool invoke

对齐 Python: `EnterPlanModeTool.invoke()` L288-320

- 若 state.plan_mode.plan_slug 非空且文件已存在 → 返回已存在消息
- 否则：生成 slug → resolvePlanFilePath → 创建 .plans/ 目录 → 保存 slug 到 state → 返回创建消息

中英文消息对齐 Python L114-138：
- 已存在 CN：`"计划文件已存在，路径：{plan_path}\n你可以阅读计划文件然后做增量修改。请按照提示词中的Plan工作流继续制定计划，初始理解-方案设计-审查-撰写计划-结束规划。\n"`
- 已存在 EN：`"Plan file already exists at: {plan_path}\nYou can read it and make incremental edits. Continue the 5-phase Plan workflow in your instructions, initial understanding-design-review-final plan-end."`
- 新创建 CN：`"计划文件已创建于：{plan_path}\n请按照提示词中的Plan工作流继续制定计划，初始理解-方案设计-审查-撰写计划-结束规划。\n除计划文件外，请勿编辑任何其他文件。\n"`
- 新创建 EN：`"Plan file created at: {plan_path}\nContinue the 5-phase Plan workflow in your instructions, initial understanding-design-review-final plan-end.DO NOT edit any files except the plan file.\n"`

### 5.8 ExitPlanModeTool invoke

对齐 Python: `ExitPlanModeTool.invoke()` L350-378

- 读取 plan 文件内容
- 若为空 → 返回空消息
- 否则：调用 agent.RestoreModeAfterPlanExit → 返回前缀 + 计划全文

中英文消息对齐 Python L140-162。

### 5.9 Slug 生成

对齐 Python: `agent_mode_tools.py` L51-112

- `GenerateWordSlug()` — adjective-verb-noun 三词组合（26×23×26 = 15,574 种）
- `ResolvePlanFilePath(workspaceRoot, slug)` — `<workspace>/.plans/<slug>.md`
- `GetOrCreatePlanSlug(workspaceRoot)` — 最多尝试 20 次生成不重复的 slug

词表对齐 Python L26-48（26 个形容词、23 个动词、26 个名词）。

### 5.10 TaskTool + CreateTaskTool

对齐 Python: `tools/subagent/task_tool.py` (159 行)

- `TaskTool` 结构体：subagent_type + task_description 两个参数
- `CreateTaskTool(parentAgent, availableAgents, language)` 工厂函数
  - 构建 availableAgents 描述字符串
  - 创建 TaskTool 实例
  - 返回工具列表

TaskTool 的提示词对齐 Python `prompts/tools/task_tool.py` 的中英文描述（已在上方完整列出）。

### 5.11 辅助方法

| 方法 | 职责 | 对齐 Python |
|------|------|-------------|
| `rejectTool(ctx, msg)` | 设置 skipTool + 注入错误结果 | `_reject_tool()` L476-488 |
| `isPlanFile(filePath, planPath)` | 路径比较（resolve 后判断） | `_is_plan_file()` L490-506 |
| `extractFilePath(ctx)` | 从 tool_args 提取 file_path | `_extract_file_path()` L508-523 |
| `extractBashCommand(ctx)` | 从 tool_args 提取 command | `_extract_bash_command()` L525-548 |
| `buildAvailableAgents(subagents)` | 格式化子 Agent 描述 | `_build_available_agents()` L410-432 |
| `registerTaskTool(agent)` | 注册 task_tool | `_register_task_tool()` L358-387 |
| `unregisterTaskTool(agent)` | 注销 task_tool | `_unregister_task_tool()` L389-408 |
| `isTaskToolRegistered()` | 检查 task_tool 是否已注册 | `_is_task_tool_registered()` L346-356 |
| `syncTaskToolForModelToolInputs(ctx)` | 同步 task_tool 在可见工具列表中的状态 | `_sync_task_tool_for_model_tool_inputs()` L198-230 |

## 6. 测试策略

### 6.1 AgentModeRail 测试

- `TestNewAgentModeRail` — 构造函数
- `TestNewAgentModeRail_自定义白名单` — 自定义 allowedTools
- `TestAgentModeRail_Init` — 注册工具
- `TestAgentModeRail_Uninit` — 注销工具
- `TestAgentModeRail_BeforeModelCall_Plan模式` — 注入段 + 过滤隐藏工具
- `TestAgentModeRail_BeforeModelCall_Normal模式` — 移除段 + 隐藏 enter/exit
- `TestAgentModeRail_BeforeToolCall_EnterPlanMode` — 校验
- `TestAgentModeRail_BeforeToolCall_ExitPlanMode` — 校验
- `TestAgentModeRail_BeforeToolCall_Plan模式白名单拒绝` — 白名单
- `TestAgentModeRail_BeforeToolCall_Git写操作拦截` — 正则
- `TestAgentModeRail_BeforeToolCall_计划文件路径校验` — write/edit 限制
- `TestAgentModeRail_AfterToolCall_注册TaskTool` — 动态注册
- `TestAgentModeRail_AfterToolCall_注销TaskTool` — 动态注销
- `TestIsPlanFile` — 路径比较
- `TestExtractFilePath` — 参数提取
- `TestExtractBashCommand` — 命令提取

### 6.2 Agent Mode 工具测试

- `TestGenerateWordSlug` — 格式校验
- `TestResolvePlanFilePath` — 路径计算
- `TestGetOrCreatePlanSlug` — 不重复
- `TestSwitchModeTool_Invoke` — 切换逻辑
- `TestEnterPlanModeTool_Invoke_新建` — 创建计划文件
- `TestEnterPlanModeTool_Invoke_已存在` — 幂等
- `TestExitPlanModeTool_Invoke_有内容` — 返回计划
- `TestExitPlanModeTool_Invoke_空计划` — 空消息

### 6.3 TaskTool 测试

- `TestCreateTaskTool` — 工厂函数
- `TestTaskTool_Invoke` — 调用逻辑

## 7. 需要回填的已有代码

| 位置 | 当前标记 | 回填内容 |
|------|---------|---------|
| `deep_agent.go:811` | `⤵️ 9.3 回填：resolve_plan_file_path 工具实现后补全` | resolvePlanFilePath 已在 slug.go 中实现，移除回填标记 |
| `rails/doc.go` | — | 添加 `agent_mode.go` 条目 |
| `tools/subagent/doc.go` | — | 添加 `task_tool.go` 条目 |

## 8. 提示词对齐确认

已逐行对比确认，以下内容与 Python 1:1 对齐：
- ✅ 三个工具名称（switch_mode / enter_plan_mode / exit_plan_mode）
- ✅ 三个工具的中英文描述
- ✅ switch_mode 的 input schema（mode 参数的 enum 和中英文描述）
- ✅ enter_plan_mode / exit_plan_mode 的空 input schema
- ✅ PLAN_MODE_PROMPT_CN（100 行，逐行一致）
- ✅ PLAN_MODE_PROMPT_EN（85 行，逐行一致）
- ✅ SectionName 常量值（18 个全部一致）
- ✅ Section priority（85）
- ✅ 工具结果消息（enter/exit/switch 的中英文消息，对齐 Python L114-184）

## 9. 实现步骤

1. 创建 `tools/agent_mode/` 目录及 doc.go
2. 实现 `slug.go`（GenerateWordSlug / ResolvePlanFilePath / GetOrCreatePlanSlug）+ 测试
3. 实现 `switch_mode.go`（SwitchModeTool invoke）+ 测试
4. 实现 `enter_plan_mode.go`（EnterPlanModeTool invoke）+ 测试
5. 实现 `exit_plan_mode.go`（ExitPlanModeTool invoke）+ 测试
6. 实现 `tools/subagent/task_tool.go`（TaskTool + CreateTaskTool）+ 测试
7. 实现 `rails/agent_mode.go`（AgentModeRail 主体）+ 测试
8. 更新 `rails/doc.go` 和 `tools/subagent/doc.go`
9. 回填 `deep_agent.go` 的 ⤵️ 标记
10. 更新 IMPLEMENTATION_PLAN.md 中 9.14 状态为 ✅
