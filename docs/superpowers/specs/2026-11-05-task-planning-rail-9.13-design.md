# 9.13 TaskPlanningRail + TodoTool 设计

## 概述

实现 9.13 TaskPlanningRail（任务规划 Rail）及其核心前置依赖 TodoTool 工具包。

TaskPlanningRail 是 DeepAgent 任务循环中"任务规划"与"任务执行"之间的桥梁，
负责：注册 todo 工具、注入规划提示词、模型切换、进度提醒、token 统计、Todo↔TaskPlan 状态同步。

对应 Python 源码：
- `openjiuwen/harness/rails/task_planning_rail.py` — TaskPlanningRail（~407 行）
- `openjiuwen/harness/tools/todo.py` — TodoLockManager + TodoTool 基类 + 4 个工具（~781 行）

## 流程位置与作用

```
DeepAgent.Invoke()
  └─ TaskLoop（外层循环）
       ├─ BeforeTaskIteration ← TaskPlanningRail（不参与）
       ├─ ReActAgent.Invoke()（内层 ReAct 循环）
       │    ├─ BeforeModelCall ← 🎯 注入 todo 提示词节 + 模型切换
       │    ├─ AfterModelCall  ← 🎯 累计 token 使用量
       │    └─ AfterToolCall   ← 🎯 进度提醒 + 刷新 todo 缓存
       ├─ AfterTaskIteration  ← 🎯 Todo ↔ TaskPlan 双向同步
       └─ AfterInvoke         ← 🎯 token 汇总日志 + 清理缓存
```

优先级：90（高于 TaskCompletionRail 的 10），TaskPlanningRail 在模型调用前先注入提示词。

核心职责：

| 职责 | 钩子 | 说明 |
|------|------|------|
| 注册 todo 工具 | `Init(agent)` | 将 todo_create/list/get/modify 4 个工具注册到 Agent |
| 注入规划提示词 | `BeforeModelCall` | 向 system_prompt_builder 注入 todo 提示词节 |
| 模型切换 | `BeforeModelCall` | 根据当前 in_progress 任务的 selected_model_id 切换 LLM |
| 进度提醒 | `AfterToolCall` | 每隔 N 次工具调用注入 UserMessage 提醒模型回顾进度 |
| 缓存刷新 | `AfterToolCall` | todo 工具被调用后刷新内存缓存 |
| Token 统计 | `AfterModelCall` | 每次模型调用后累计 input/output token 使用量 |
| 状态同步 | `AfterTaskIteration` | 将 TaskPlan 状态同步回 todo.json |
| 资源清理 | `AfterInvoke` | 输出 token 汇总，清理 session 缓存和锁 |

## 新增文件

| 文件 | 内容 | 约行数 |
|------|------|--------|
| `harness/tools/todo/todo.go` | TodoLockManager + TodoTool 基类 + 4 个工具（单文件对齐 Python） | ~700 |
| `harness/tools/todo/doc.go` | 包文档 | ~20 |
| `harness/tools/todo/todo_test.go` | TodoTool 完整测试 | ~600 |
| `harness/rails/task_planning.go` | TaskPlanningRail 结构体 + 7 个钩子 | ~450 |
| `harness/rails/task_planning_test.go` | TaskPlanningRail 测试 | ~500 |

## 修改文件（回填）

| 文件 | 修改内容 |
|------|---------|
| `harness/interfaces/deep_agent.go` | 新增 `SwitchModel(model *llm.Model)` 方法 |
| `harness/deep_agent.go` | 实现 SwitchModel；删除 `deepAgentRailProvider` 定义 |
| `harness/rails/base.go` | 新增导出接口 `DeepAgentRailProvider`（从 deep_agent.go 移入） |
| `harness/factory.go` | L518-520：NewBaseRail() 占位 → NewTaskPlanningRail() |
| `harness/rails/doc.go` | 文件目录新增 task_planning.go |
| `harness/tools/doc.go` 或父级 | 文件目录新增 todo/ 子包 |
| `IMPLEMENTATION_PLAN.md` | 9.13 状态 ☐ → ✅ |

## 设计决策

### D1：TodoTool 文件组织 — 单文件对齐 Python

所有 4 个工具 + TodoLockManager + TodoTool 基类都放在 `harness/tools/todo/todo.go` 一个文件中，
与 Python `todo.py` 一比一对应。schema 和提示词一比一复制。

### D2：并发锁机制 — sync.RWMutex + sync.Mutex

```
全局 sync.RWMutex 保护 session→Mutex 映射
  ├─ 读锁（RLock）：查找/获取 session 级 Mutex（多 session 并发读不互斥）
  └─ 写锁（Lock）：新增/删除 session 级 Mutex

session 级 sync.Mutex 保证同 session 内文件串行读写
```

对齐 Python `TodoLockManager` 的 session 隔离语义，全局 RWMutex 优化多 session 并发读。

### D3：SwitchModel — 单一方法封装

在 `DeepAgentInterface` 上新增 `SwitchModel(model *llm.Model)` 一个方法，
内部同时调用 `reactAgent.SetLLM(model)` + 同步 `reactAgent.config.ModelNameVal`。
调用方只需关心传入目标 Model，实现细节封装在 DeepAgent 内部。

### D4：DeepAgentRailProvider 迁移到 rails/base.go

将 `deep_agent.go` 中的非导出接口 `deepAgentRailProvider` 移到 `rails/base.go`，
导出为 `DeepAgentRailProvider`，与 `DeepAgentRail` 定义在一起。
消除 rails 包对 harness 包的循环依赖。

### D5：Workspace 路径获取 — Init 时从 DeepAgent 传入

TaskPlanningRail.Init() 中从 `agent.deep_config.workspace` 获取 `WorkspaceNode.TODO` 路径，
传给 TodoTool 构造函数。与 Python `init()` 逻辑完全对齐。

### D6：进度提醒注入 — 追加 UserMessage 到 ModelContext

通过 `cbc.ModelContext().AddMessages(ctx, llmschema.NewUserMessage(prompt))` 注入。
与 Python `messages.append(UserMessage(content=prompt))` 完全对齐。
Go 端 `ModelContext.AddMessages` 已在 `react_invoke.go` 的 steering 注入中使用。

### D7：Todo 工具检测 — 前缀匹配 "todo_"

`AfterToolCall` 中通过 `ToolCallInputs.ToolName` 的 `strings.HasPrefix(toolName, "todo_")` 判断，
与 Python `tool_name.startswith("todo_")` 完全对齐。

### D8：Token 使用量获取 — ModelCallInputs.Response.UsageMetadata

```go
if inputs, ok := cbc.Inputs().(*interfaces.ModelCallInputs); ok {
    if inputs.Response != nil && inputs.Response.UsageMetadata != nil {
        inputTokens := inputs.Response.UsageMetadata.InputTokens
        outputTokens := inputs.Response.UsageMetadata.OutputTokens
    }
}
```

Go 端 `UsageMetadata` 比 Python 更丰富（含 CacheTokens/InputCost/OutputCost/TotalCost），
但 `InputTokens` + `OutputTokens` 完全对齐。

### D9：TaskPlan 获取 — DeepAgentInterface.LoadState

`cbc.Agent().(DeepAgentInterface).LoadState(sess).TaskPlan` 获取 TaskPlan。
`DeepAgentInterface.LoadState` 已存在，直接使用。

### D10：TodoModifyTool action 处理 — 单 invoke + switch 分支

在 Invoke() 方法中 switch action 分支处理，每个分支调用对应的私有方法
（deleteTodos/cancelTodos/updateTodos/appendTodos/insertAfterTodos/insertBeforeTodos）。
与 Python 的 invoke() 结构完全对齐。

## TaskPlanningRail 结构体

```go
type TaskPlanningRail struct {
    DeepAgentRail
    // tools 注册的 Todo 工具实例
    tools []tool.Tool
    // enableProgressRepeat 是否注入定期进度提醒
    enableProgressRepeat bool
    // listToolCallInterval 进度提醒间隔（工具调用次数）
    listToolCallInterval int
    // toolCallCounts 按 session_id 工具调用计数
    toolCallCounts map[string]int
    // todosCache 按 session_id todo 缓存
    todosCache map[string][]schema.TodoItem
    // modelSelection 模型选择映射（Model → 描述）
    modelSelection map[*llm.Model]string
    // modelIDToModel client_id → Model 反向映射
    modelIDToModel map[string]*llm.Model
    // usageRecords 按 model_id token 记录
    usageRecords map[string]*schema.ModelUsageRecord
    // defaultLLM 默认 LLM（首次 BeforeModelCall 时捕获）
    defaultLLM *llm.Model
    // systemPromptBuilder 系统提示词构建器
    systemPromptBuilder saprompt.SystemPromptBuilderInterface
}
```

## 钩子方法

| 钩子 | 功能 | Python 对齐 |
|------|------|-------------|
| `Init(agent)` | 注册 todo_create/list/get/modify | `init()` |
| `Uninit(agent)` | 移除 todo 工具 + todo 提示词节 | `uninit()` |
| `BeforeModelCall` | 注入 todo 提示词节 + 模型切换 | `before_model_call()` |
| `AfterToolCall` | 刷新 todo 缓存 + 进度提醒 | `after_tool_call()` |
| `AfterModelCall` | 累计 token 使用量 | `after_model_call()` |
| `AfterInvoke` | token 汇总 + 清理缓存 | `after_invoke()` |
| `AfterTaskIteration` | Todo ↔ TaskPlan 同步 | `after_task_iteration()` |
| `GetCallbacks` | 覆盖基类回调映射，声明全部 7 个钩子 | — |

## TodoTool 持久化

- 文件路径：`{workspace_dir}/{session_id}/todo.json`
- 读写：通过 `SysOperation.Fs().ReadFile()` / `WriteFile()`
- 加锁：全局 `sync.RWMutex` + session 级 `sync.Mutex`（见 D2）
- 序列化：`[]TodoItem` → JSON，使用已有 `TodoItem.ToDict()`/`FromDict()`

## TodoLockManager

```go
type TodoLockManager struct {
    // sessionLocks 按 session_id 的互斥锁
    sessionLocks map[string]*sync.Mutex
    // globalLock 保护 sessionLocks 映射的读写锁
    globalLock sync.RWMutex
}
```

方法：
- `Operation(sessionID string) *sync.Mutex` — 获取/创建 session 级 Mutex
- `CleanupSession(sessionID string)` — 删除 session 锁（会话结束时调用）

## TodoTool 基类

```go
type TodoTool struct {
    // card 工具元数据
    card ToolCard
    // workspace 工作空间目录路径
    workspace string
    // fs 文件系统操作接口
    fs FsInterface
    // lockManager 共享锁管理器
    lockManager *TodoLockManager
}
```

方法：
- `GetFilePath(sessionID string) string` — 返回 `{workspace}/{sessionID}/todo.json`
- `LoadTodos(sessionID string) ([]schema.TodoItem, error)` — 加载并反序列化
- `SaveTodos(sessionID string, todos []schema.TodoItem) error` — 序列化并保存
- `CleanupSession(sessionID string)` — 清理 session 锁

## 四个 Todo 工具

### TodoCreateTool

- 输入：`tasks` (JSON 数组，每项含 content/activeForm/description/selected_model_id/id)
- 逻辑：首个任务 `in_progress`，其余 `pending`；ID 优先使用传入值，无则 uuid 生成
- 输出：格式化的创建结果字符串
- 对齐 Python：`TodoCreateTool.invoke()` + `_create_from_list()`

### TodoListTool

- 输入：无（仅需 session_id）
- 逻辑：加载 todos，过滤非 completed/cancelled，返回简化视图
- 输出：`{"tasks": [{"id", "content", "status", "depends_on"}]}`
- 对齐 Python：`TodoListTool.invoke()`

### TodoGetTool

- 输入：`id` (任务 ID)
- 逻辑：加载 todos，按 ID 查找，返回完整详情
- 输出：`{"todo": TodoItem.ToDict()}`
- 对齐 Python：`TodoGetTool.invoke()`

### TodoModifyTool

- 输入：`action` + 对应参数
- 6 种 action：`update`/`delete`/`cancel`/`append`/`insert_after`/`insert_before`
- 单 invoke + switch 分支，每个分支调用私有方法
- 验证：`_validateSingleInProgress`（最多一个 in_progress）、`_validateTargetTaskStatus`、`_validateSingleTodoItem`
- 对齐 Python：`TodoModifyTool.invoke()` + 各 action 私有方法

## DeepAgentInterface 扩展

新增方法：
```go
SwitchModel(model *llm.Model)
```
实现中同时调用 `reactAgent.SetLLM(model)` + 同步 `reactAgent.config.ModelNameVal`。

## DeepAgentRailProvider 迁移

将 `deep_agent.go` 中的非导出接口 `deepAgentRailProvider` 移到 `rails/base.go`，
导出为 `DeepAgentRailProvider`，与 `DeepAgentRail` 定义在一起。

```go
// DeepAgentRailProvider DeepAgentRail 提供者接口。
// 对齐 Python: isinstance(rail_inst, DeepAgentRail) 类型检查。
type DeepAgentRailProvider interface {
    SetSysOperation(op sysop.SysOperation)
    SetWorkspace(w *workspace.Workspace)
}
```

## 已有依赖（无需修改）

- `schema/task.go` — TodoItem/TodoStatus/TaskPlan/ModelUsageRecord ✅
- `schema/state.go` — DeepAgentState.TaskPlan ✅
- `prompts/sections/todo.go` — BuildTodoSection/BuildProgressReminderUserPrompt ✅
- `prompts/tools/todo.go` — 4 个工具元数据 Provider ✅
- `rails/base.go` — DeepAgentRail 基类 ✅
- `sys_operation.SysOperation.Fs()` — 文件系统操作接口 ✅
- `DeepAgentInterface.LoadState` — 获取 DeepAgentState ✅
- `ModelContext.AddMessages` — 追加 UserMessage ✅
- `UsageMetadata.InputTokens/OutputTokens` — token 使用量 ✅
- `ToolCallInputs.ToolName` — 工具名称（`todo_` 前缀匹配） ✅
- `ModelCallInputs.Response.UsageMetadata` — 模型调用 token 元数据 ✅
