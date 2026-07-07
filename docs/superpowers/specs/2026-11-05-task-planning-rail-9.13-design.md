# 9.13 TaskPlanningRail + TodoTool 设计

## 概述

实现 9.13 TaskPlanningRail（任务规划 Rail）及其核心前置依赖 TodoTool 工具包。

TaskPlanningRail 是 DeepAgent 任务循环中"任务规划"与"任务执行"之间的桥梁，
负责：注册 todo 工具、注入规划提示词、模型切换、进度提醒、token 统计、Todo↔TaskPlan 状态同步。

## 流程位置

```
DeepAgent.Invoke()
  └─ TaskLoop（外层循环）
       ├─ BeforeTaskIteration ← TaskPlanningRail
       ├─ ReActAgent.Invoke()
       │    ├─ BeforeModelCall ← 注入 todo 提示词节 + 模型切换
       │    ├─ AfterModelCall  ← 累计 token 使用量
       │    └─ AfterToolCall   ← 进度提醒 + 刷新 todo 缓存
       ├─ AfterTaskIteration  ← Todo ↔ TaskPlan 同步
       └─ AfterInvoke         ← token 汇总 + 清理缓存
```

## 新增文件

| 文件 | 内容 | 约行数 |
|------|------|--------|
| `harness/tools/todo/todo.go` | TodoLockManager + TodoTool 基类 + 4 个工具 | ~700 |
| `harness/tools/todo/doc.go` | 包文档 | ~20 |
| `harness/tools/todo/todo_test.go` | TodoTool 完整测试 | ~600 |
| `harness/rails/task_planning.go` | TaskPlanningRail 结构体 + 5 个钩子 | ~450 |
| `harness/rails/task_planning_test.go` | TaskPlanningRail 测试 | ~500 |

## 修改文件（回填）

| 文件 | 修改内容 |
|------|---------|
| `harness/interfaces/deep_agent.go` | 新增 `SwitchModel(model *llm.Model)` 方法 |
| `harness/deep_agent.go` | 实现 SwitchModel；移 deepAgentRailProvider 定义到 rails/base.go |
| `harness/rails/base.go` | 新增导出接口 `DeepAgentRailProvider`（从 deep_agent.go 移入） |
| `harness/factory.go` | L518-520：NewBaseRail() 占位 → NewTaskPlanningRail() |
| `harness/rails/doc.go` | 文件目录新增 task_planning.go |
| `harness/tools/doc.go` 或父级 | 文件目录新增 todo/ 子包 |
| `IMPLEMENTATION_PLAN.md` | 9.13 状态 ☐ → ✅ |

## TaskPlanningRail 结构体

```go
type TaskPlanningRail struct {
    DeepAgentRail
    tools               []tool.Tool                   // 注册的 Todo 工具实例
    enableProgressRepeat bool                          // 是否注入定期进度提醒
    listToolCallInterval int                           // 进度提醒间隔
    toolCallCounts       map[string]int                // 按 session_id 工具调用计数
    todosCache           map[string][]schema.TodoItem  // 按 session_id todo 缓存
    modelSelection       map[*llm.Model]string         // 模型选择映射
    modelIDToModel       map[string]*llm.Model         // client_id → Model
    usageRecords         map[string]*schema.ModelUsageRecord  // 按 model_id token 记录
    defaultLLM           *llm.Model                    // 默认 LLM
    systemPromptBuilder  saprompt.SystemPromptBuilderInterface
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

## TodoTool 持久化

- 文件路径：`{workspace_dir}/{session_id}/todo.json`
- 读写：通过 `SysOperation.Fs().ReadFile()` / `WriteFile()`
- 加锁：`sync.Mutex` 按 session_id（对齐 Python TodoLockManager）
- 序列化：`[]TodoItem` → JSON，使用已有 `TodoItem.ToDict()`/`FromDict()`

## DeepAgentInterface 扩展

新增方法：
```go
SwitchModel(model *llm.Model)
```
实现中同时调用 `reactAgent.SetLLM(model)` + 同步 `reactAgent.config.ModelNameVal`。

## DeepAgentRailProvider 迁移

将 `deep_agent.go` 中的非导出接口 `deepAgentRailProvider` 移到 `rails/base.go`，
导出为 `DeepAgentRailProvider`，与 `DeepAgentRail` 定义在一起。

## 已有依赖（无需修改）

- `schema/task.go` — TodoItem/TodoStatus/TaskPlan/ModelUsageRecord ✅
- `prompts/sections/todo.go` — BuildTodoSection/BuildProgressReminderUserPrompt ✅
- `prompts/tools/todo.go` — 4 个工具元数据 Provider ✅
- `rails/base.go` — DeepAgentRail 基类 ✅
- `sys_operation.SysOperation.Fs()` — 文件系统操作接口 ✅
