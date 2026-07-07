# 9.13 TaskPlanningRail + TodoTool 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.13 TaskPlanningRail（任务规划 Rail）及其核心前置依赖 TodoTool 工具包，使 DeepAgent 支持任务规划、模型切换、进度提醒、token 统计和 Todo↔TaskPlan 状态同步。

**Architecture:** TaskPlanningRail 嵌入 DeepAgentRail，通过 7 个钩子方法（Init/Uninit/BeforeModelCall/AfterToolCall/AfterModelCall/AfterInvoke/AfterTaskIteration）与 DeepAgent 任务循环交互。TodoTool 包（harness/tools/todo/）提供 4 个工具的持久化实现，通过 SysOperation.Fs() 读写 JSON 文件。

**Tech Stack:** Go 1.22+, 标准库 sync/json/os, 内部包: foundation/tool, harness/schema, harness/prompts, sys_operation, single_agent/interfaces, runner

---

## 文件结构

### 新增文件
| 文件 | 职责 |
|------|------|
| `internal/agentcore/harness/tools/todo/doc.go` | 包文档 |
| `internal/agentcore/harness/tools/todo/todo.go` | TodoLockManager + TodoTool 基类 + TodoCreateTool/ListTool/GetTool/ModifyTool |
| `internal/agentcore/harness/tools/todo/todo_test.go` | TodoTool 完整测试 |
| `internal/agentcore/harness/rails/task_planning.go` | TaskPlanningRail 结构体 + 钩子方法 |
| `internal/agentcore/harness/rails/task_planning_test.go` | TaskPlanningRail 测试 |

### 修改文件
| 文件 | 修改内容 |
|------|---------|
| `internal/agentcore/harness/interfaces/deep_agent.go` | 新增 SwitchModel 方法 |
| `internal/agentcore/harness/deep_agent.go` | 实现 SwitchModel; 将 deepAgentRailProvider 移到 rails/base.go |
| `internal/agentcore/harness/rails/base.go` | 新增导出接口 DeepAgentRailProvider |
| `internal/agentcore/harness/factory.go` | L517-521: 占位替换为真正 TaskPlanningRail |
| `internal/agentcore/harness/rails/doc.go` | 文件目录新增条目 |
| `docs/superpowers/specs/IMPLEMENTATION_PLAN.md` | 9.13 状态更新 |

---

## Task 1: DeepAgentRailProvider 迁移 + DeepAgentInterface.SwitchModel 扩展

**Files:**
- Modify: `internal/agentcore/harness/rails/base.go`
- Modify: `internal/agentcore/harness/deep_agent.go`
- Modify: `internal/agentcore/harness/interfaces/deep_agent.go`

- [ ] **Step 1: 在 rails/base.go 新增导出接口 DeepAgentRailProvider**

在 `base.go` 文件末尾（非导出函数区块前）添加：

```go
// ──────────────────────────── 导出接口 ────────────────────────────

// DeepAgentRailProvider DeepAgentRail 提供者接口。
// 对齐 Python: isinstance(rail_inst, DeepAgentRail) 类型检查。
// 嵌入 DeepAgentRail 的子类自动满足此接口。
type DeepAgentRailProvider interface {
	SetSysOperation(op sys_operation.SysOperation)
	SetWorkspace(w *workspace.Workspace)
}
```

- [ ] **Step 2: 从 deep_agent.go 删除 deepAgentRailProvider 定义**

删除 `deep_agent.go` L1070-1076 的 `deepAgentRailProvider` 接口定义及其注释。

- [ ] **Step 3: 在 deep_agent.go 中将 deepAgentRailProvider 类型断言替换为 rails.DeepAgentRailProvider**

在 `ensureInitialized` 方法中，将：
```go
if provider, ok := r.(deepAgentRailProvider); ok {
```
替换为：
```go
if provider, ok := r.(rails.DeepAgentRailProvider); ok {
```

需要确认 `deep_agent.go` 已导入 `rails` 包（已导入）。

- [ ] **Step 4: 在 DeepAgentInterface 新增 SwitchModel 方法**

在 `harness/interfaces/deep_agent.go` 的 `DeepAgentInterface` 接口中添加：

```go
	// SwitchModel 切换内层 ReActAgent 的 LLM 模型实例。
	// 对齐 Python: ctx.agent.set_llm(target_model) + config.model_name 同步。
	SwitchModel(model *llm.Model)
```

需要添加 `llm` 包导入：
```go
import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	// ... 已有导入
)
```

- [ ] **Step 5: 在 *DeepAgent 上实现 SwitchModel 方法**

在 `deep_agent.go` 导出函数区块添加：

```go
// SwitchModel 切换内层 ReActAgent 的 LLM 模型实例。
// 同时更新 llm 实例和 config.model_name，对齐 Python:
//   ctx.agent.set_llm(target_model)
//   ctx.agent.config.model_name = target_model.model_config.model_name
func (d *DeepAgent) SwitchModel(model *llm.Model) {
	if d.reactAgent == nil || model == nil {
		return
	}
	d.reactAgent.SetLLM(model)
	// 同步 config.model_name
	if model.ModelConfig != nil && model.ModelConfig.ModelName != "" {
		d.reactAgent.MutateConfig(func(cfg *saconfig.ReActAgentConfig) {
			cfg.ModelNameVal = model.ModelConfig.ModelName
		})
	}
}
```

需要确认 ReActAgent 是否有 `MutateConfig` 方法或类似机制来安全更新 config。如果没有，需要添加或使用其他方式更新。检查 `single_agent/agents/` 下 ReActAgent 的 config 更新方式。

- [ ] **Step 6: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/...
```

- [ ] **Step 7: Commit**

```bash
git add internal/agentcore/harness/rails/base.go internal/agentcore/harness/deep_agent.go internal/agentcore/harness/interfaces/deep_agent.go
git commit -m "feat(harness): 迁移 DeepAgentRailProvider 到 rails/base.go + DeepAgentInterface.SwitchModel 扩展"
```

---

## Task 2: TodoTool 包 — doc.go + TodoLockManager + TodoTool 基类

**Files:**
- Create: `internal/agentcore/harness/tools/todo/doc.go`
- Create: `internal/agentcore/harness/tools/todo/todo.go`（部分：TodoLockManager + TodoTool 基类）

- [ ] **Step 1: 创建 todo/doc.go**

```go
// Package todo 提供 Todo 待办事项工具实现。
//
// 包含四个工具：
//   - TodoCreateTool：创建待办事项列表
//   - TodoListTool：列出活跃待办事项
//   - TodoGetTool：按 ID 获取待办事项详情
//   - TodoModifyTool：修改待办事项（update/delete/cancel/append/insert_after/insert_before）
//
// 持久化通过 SysOperation.Fs() 读写 JSON 文件，
// 文件路径为 {workspace_dir}/{session_id}/todo.json。
// TodoLockManager 保证同一 session 内的文件 I/O 互斥。
//
// 文件目录：
//
//	todo/
//	├── doc.go        # 包文档
//	└── todo.go       # TodoLockManager + TodoTool 基类 + 4 个工具实现
//
// 对应 Python 代码：openjiuwen/harness/tools/todo.py
package todo
```

- [ ] **Step 2: 创建 todo.go — TodoLockManager + TodoTool 基类**

在 `todo.go` 中实现：

1. **TodoLockManager**：基于 `sync.Mutex` 按 session_id 的锁管理器
   - `sessionLocks map[string]*sync.Mutex` + `globalMu sync.Mutex`
   - `Operation(sessionID string) func()` — 获取/创建 session 锁并加锁，返回解锁函数
   - `CleanupSession(sessionID string)` — 移除 session 锁

2. **TodoTool** 基类：嵌入 `tool.BaseTool`，持有 `sysOp`、`workspaceDir`、`lockMgr`
   - `LoadTodos(ctx, sessionID)` — 通过 `sysOp.Fs().ReadFile()` 读取 JSON，反序列化为 `[]TodoItem`
   - `SaveTodos(ctx, sessionID, todos)` — 序列化为 JSON，通过 `sysOp.Fs().WriteFile()` 写入
   - `CleanupSession(sessionID)` — 清理锁
   - `getFilePath(sessionID)` — 返回 `{workspaceDir}/{sessionID}/todo.json`

3. **NewTodoTool** 构造函数：创建 TodoTool 基类实例

关键：需要检查 `tool.BaseTool` 是否存在，如果不存在则直接实现 `tool.Tool` 接口（Card + Invoke + Stream）。

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/tools/todo/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/harness/tools/todo/
git commit -m "feat(harness): TodoLockManager + TodoTool 基类（持久化读写）"
```

---

## Task 3: TodoTool 包 — TodoCreateTool 实现

**Files:**
- Modify: `internal/agentcore/harness/tools/todo/todo.go`

- [ ] **Step 1: 实现 TodoCreateTool**

对齐 Python `TodoCreateTool.invoke()`：

1. `NewTodoCreateTool(sysOp, workspaceDir, language, agentID, lockMgr)` 构造函数
2. `Invoke(ctx, inputs, opts...)` 方法：
   - 从 `ToolCallOptions.Session` 获取 session_id
   - 从 `inputs["tasks"]` 获取任务数组
   - 调用 `createFromList(ctx, sessionID, tasksData)`
3. `Stream` 方法返回错误（不支持流式）
4. `createFromList` 内部方法：
   - 校验 tasks 非空
   - 遍历构造 TodoItem：第一个 `InProcess`，其余 `Pending`
   - 校验 content/activeForm/description 必填
   - 校验 id 唯一性（id 为空时用 uuid 兜底）
   - 调用 `SaveTodos` 持久化
5. `formatCreateResult` 格式化结果字符串

- [ ] **Step 2: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/tools/todo/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/tools/todo/todo.go
git commit -m "feat(harness): TodoCreateTool 创建待办事项"
```

---

## Task 4: TodoTool 包 — TodoListTool + TodoGetTool 实现

**Files:**
- Modify: `internal/agentcore/harness/tools/todo/todo.go`

- [ ] **Step 1: 实现 TodoListTool**

对齐 Python `TodoListTool.invoke()`：

1. `NewTodoListTool(...)` 构造函数
2. `Invoke` 方法：`LoadTodos` → 过滤活跃项（非 Completed/Cancelled）→ 返回简化视图 `{id, content, status, depends_on}`

- [ ] **Step 2: 实现 TodoGetTool**

对齐 Python `TodoGetTool.invoke()`：

1. `NewTodoGetTool(...)` 构造函数
2. `Invoke` 方法：`LoadTodos` → 按 `inputs["id"]` 查找 → 返回 `TodoItem.ToDict()` 完整详情

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/tools/todo/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/harness/tools/todo/todo.go
git commit -m "feat(harness): TodoListTool + TodoGetTool"
```

---

## Task 5: TodoTool 包 — TodoModifyTool 实现

**Files:**
- Modify: `internal/agentcore/harness/tools/todo/todo.go`

- [ ] **Step 1: 实现 TodoModifyTool**

对齐 Python `TodoModifyTool.invoke()`，支持 6 种 action：

1. `NewTodoModifyTool(...)` 构造函数
2. `Invoke` 方法：从 `inputs["action"]` 分派到对应内部方法
3. 内部方法：
   - `deleteTodos` — 按 ids 列表永久删除
   - `cancelTodos` — 按 ids 列表标记 Cancelled
   - `updateTodos` — 按 `inputs["todos"]` 部分更新字段
   - `appendTodos` — 在末尾追加新任务
   - `insertAfterTodos` — 在目标任务后插入
   - `insertBeforeTodos` — 在目标任务前插入
4. 校验方法：
   - `validateTodoDataStructure` — insert 操作的 todo_data 结构校验
   - `validateTargetTaskStatus` — 目标任务状态校验
   - `validateSingleInProgress` — 校验只有一个 InProgress
   - `validateSingleTodoItem` — 单个 todo 数据校验
   - `convertToTodoItem` — dict 转 TodoItem

5. **CreateTodosTool 工厂函数**：创建共享同一 TodoLockManager 的 4 个工具实例列表

- [ ] **Step 2: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/tools/todo/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/tools/todo/todo.go
git commit -m "feat(harness): TodoModifyTool + CreateTodosTool 工厂函数"
```

---

## Task 6: TodoTool 包测试

**Files:**
- Create: `internal/agentcore/harness/tools/todo/todo_test.go`

- [ ] **Step 1: 编写 TodoLockManager 测试**

- `TestTodoLockManager_Operation` — 同 session 加锁互斥
- `TestTodoLockManager_CleanupSession` — 清理锁

- [ ] **Step 2: 编写 TodoTool 基类测试**

- `TestTodoTool_LoadTodos` — 正常读取
- `TestTodoTool_LoadTodos_文件不存在` — 返回错误
- `TestTodoTool_SaveTodos` — 正常写入并回读验证
- `TestTodoTool_GetFilePath` — 路径拼接

使用 mock SysOperation（实现 FsOperation 接口，内存 map 存储文件内容）。

- [ ] **Step 3: 编写 TodoCreateTool 测试**

- `TestTodoCreateTool_Invoke` — 正常创建多个任务
- `TestTodoCreateTool_Invoke_首任务自动InProgress` — 第一个任务状态为 InProgress
- `TestTodoCreateTool_Invoke_空任务列表` — 返回错误
- `TestTodoCreateTool_Invoke_缺少Content` — 返回错误
- `TestTodoCreateTool_Invoke_重复ID` — 返回错误
- `TestTodoCreateTool_Invoke_缺少SessionID` — 返回错误

- [ ] **Step 4: 编写 TodoListTool 测试**

- `TestTodoListTool_Invoke` — 返回活跃任务
- `TestTodoListTool_Invoke_过滤已完成已取消` — 不返回 Completed/Cancelled

- [ ] **Step 5: 编写 TodoGetTool 测试**

- `TestTodoGetTool_Invoke` — 返回完整详情
- `TestTodoGetTool_Invoke_不存在` — 返回错误

- [ ] **Step 6: 编写 TodoModifyTool 测试**

- `TestTodoModifyTool_Update` — 部分更新字段
- `TestTodoModifyTool_Delete` — 永久删除
- `TestTodoModifyTool_Cancel` — 标记取消
- `TestTodoModifyTool_Append` — 末尾追加
- `TestTodoModifyTool_InsertAfter` — 目标后插入
- `TestTodoModifyTool_InsertBefore` — 目标前插入
- `TestTodoModifyTool_InvalidAction` — 无效 action 返回错误
- `TestTodoModifyTool_MultipleInProgress` — 多个 InProgress 返回错误

- [ ] **Step 7: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test -v ./internal/agentcore/harness/tools/todo/...
```

- [ ] **Step 8: Commit**

```bash
git add internal/agentcore/harness/tools/todo/todo_test.go
git commit -m "test(harness): TodoTool 完整测试覆盖"
```

---

## Task 7: TaskPlanningRail 结构体 + Init/Uninit

**Files:**
- Create: `internal/agentcore/harness/rails/task_planning.go`

- [ ] **Step 1: 实现 TaskPlanningRail 结构体和构造函数**

```go
type TaskPlanningRail struct {
    DeepAgentRail
    tools               []tool.Tool
    enableProgressRepeat bool
    listToolCallInterval int
    toolCallCounts       map[string]int
    todosCache           map[string][]schema.TodoItem
    modelSelection       []*schema.ModelSelectionEntry
    modelIDToModel       map[string]*llm.Model
    usageRecords         map[string]*schema.ModelUsageRecord
    defaultLLM          *llm.Model
    systemPromptBuilder saprompt.SystemPromptBuilderInterface
}
```

构造函数 `NewTaskPlanningRail(opts ...TaskPlanningOption)` 使用 Functional Options 模式，对齐 TaskCompletionRail 风格。

选项：
- `WithEnableProgressRepeat(bool)`
- `WithListToolCallInterval(int)`
- `WithModelSelection([]*schema.ModelSelectionEntry)`

- [ ] **Step 2: 实现 Init 方法**

对齐 Python `TaskPlanningRail.init(agent)`：

1. 类型断言 `agent` 为 `DeepAgentInterface`，获取 `DeepConfig()`
2. 获取 `systemPromptBuilder`
3. 设置 `sysOperation` 和 `workspace`（如果基类尚未设置）
4. 调用 `workspace.GetNodePath(WorkspaceNodeTODO)` 获取 workspace_dir
5. 获取 agent_id 和 language
6. 调用 `todo.CreateTodosTool(sysOp, workspaceDir, language, agentID)` 创建 4 个工具
7. 检查已有工具避免重复注册（对齐 Python 的 tool_configs found 逻辑）
8. 注册到 `runner.GetResourceMgr()` 和 `agent.AbilityManager()`
9. 保存 `self.tools = tools`
10. 构建 `modelIDToModel` 映射（从 modelSelection 遍历）

- [ ] **Step 3: 实现 Uninit 方法**

对齐 Python `TaskPlanningRail.uninit(agent)`：

1. 从 systemPromptBuilder 移除 "todo" section
2. 从 ability_manager 移除工具
3. 从 resource_mgr 移除工具

- [ ] **Step 4: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/rails/task_planning.go
git commit -m "feat(harness): TaskPlanningRail 结构体 + Init/Uninit"
```

---

## Task 8: TaskPlanningRail 钩子方法实现

**Files:**
- Modify: `internal/agentcore/harness/rails/task_planning.go`

- [ ] **Step 1: 实现 BeforeModelCall — 注入 todo 提示词节 + 模型切换**

对齐 Python `before_model_call()`：

1. 如果 `systemPromptBuilder == nil`，直接返回
2. 调用 `sections.BuildTodoSection(modelSelection, lang)` 构建 todo 节，注入到 builder
3. 如果有 `modelSelection`：
   - 首次调用时缓存 `defaultLLM`
   - 调用 `getInProgressModelID(ctx, cbc)` 获取当前 in_progress 任务的 selected_model_id
   - 从 `modelIDToModel` 查找目标 Model
   - 如果找到，通过 `DeepAgentInterface.SwitchModel(model)` 切换
   - 记录 Debug 日志

- [ ] **Step 2: 实现 AfterToolCall — 刷新 todo 缓存 + 进度提醒**

对齐 Python `after_tool_call()`：

1. 获取 TodoTool 基类实例（`findTodoTool()`）
2. 如果工具名以 "todo_" 开头，刷新 `todosCache[sessionID]`
3. 如果 `enableProgressRepeat` 为 true：
   - 递增 `toolCallCounts[sessionID]`
   - 每隔 `listToolCallInterval` 次：
     - `LoadTodos` 获取当前任务列表
     - 调用 `formatTaskContent(todos)` 格式化
     - 调用 `sections.BuildProgressReminderUserPrompt(tasks, inProgressTask, lang)` 构建提醒
     - 将提醒作为 UserMessage 追加到 `cbc.Context().GetMessages()`

- [ ] **Step 3: 实现 AfterModelCall — token 使用量累计**

对齐 Python `after_model_call()`：

1. 从 `cbc.Agent()` 获取当前 LLM 实例（通过 DeepAgentInterface.ReactAgent().GetLLM()）
2. 从 `cbc.Inputs()` 获取 `response` 的 `usage_metadata`（input_tokens/output_tokens）
3. 按 model_id 分桶累加到 `usageRecords`

- [ ] **Step 4: 实现 AfterInvoke — token 汇总 + 清理缓存**

对齐 Python `after_invoke()`：

1. 输出每个 `usageRecords` 条目的 Info 日志
2. 清空 `usageRecords`
3. 清理 `todosCache[sessionID]` 和 `toolCallCounts[sessionID]`
4. 调用 `findTodoTool().CleanupSession(sessionID)`

- [ ] **Step 5: 实现 AfterTaskIteration — Todo ↔ TaskPlan 同步**

对齐 Python `after_task_iteration()` → `_sync_todos_from_plan()`：

1. 通过 `DeepAgentInterface.LoadState(sess)` 获取 `DeepAgentState`
2. 获取 `state.TaskPlan`，如果为空或无任务则返回
3. `LoadTodos` 获取当前 todo 列表
4. 构建 `statusByTaskID` 映射：`plan.tasks[i].ID → plan.tasks[i].Status`
5. 遍历 todos，如果 todo 的 status 与 plan 中不一致则更新
6. 有变更时 `SaveTodos` 持久化

- [ ] **Step 6: 实现 GetCallbacks — 注册所有钩子**

```go
func (r *TaskPlanningRail) GetCallbacks() map[...]cb.PerAgentCallbackFunc {
    callbacks := r.DeepAgentRail.GetCallbacks()
    callbacks[CallbackBeforeModelCall] = ...
    callbacks[CallbackAfterToolCall] = ...
    callbacks[CallbackAfterModelCall] = ...
    callbacks[CallbackAfterInvoke] = ...
    callbacks[CallbackAfterTaskIteration] = ...
    return callbacks
}
```

- [ ] **Step 7: 实现内部辅助方法**

- `findTodoTool() *todo.TodoTool` — 查找第一个 TodoTool 基类实例
- `getInProgressModelID(ctx, cbc) (string, error)` — 从 todosCache/LoadTodos 查找 in_progress 任务的 selected_model_id
- `formatTaskContent(todos) (tasks string, inProgressTask string)` — 格式化 todo 为可读文本

- [ ] **Step 8: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/...
```

- [ ] **Step 9: Commit**

```bash
git add internal/agentcore/harness/rails/task_planning.go
git commit -m "feat(harness): TaskPlanningRail 钩子方法（BeforeModelCall/AfterToolCall/AfterModelCall/AfterInvoke/AfterTaskIteration）"
```

---

## Task 9: TaskPlanningRail 测试

**Files:**
- Create: `internal/agentcore/harness/rails/task_planning_test.go`

- [ ] **Step 1: 编写构造和 Init/Uninit 测试**

- `TestNewTaskPlanningRail` — 默认构造
- `TestNewTaskPlanningRail_WithOptions` — 选项构造
- `TestTaskPlanningRail_Init` — 工具注册
- `TestTaskPlanningRail_Uninit` — 工具移除

- [ ] **Step 2: 编写 BeforeModelCall 测试**

- `TestTaskPlanningRail_BeforeModelCall_注入TodoSection` — 验证提示词节注入
- `TestTaskPlanningRail_BeforeModelCall_模型切换` — 验证 SwitchModel 调用
- `TestTaskPlanningRail_BeforeModelCall_无模型选择时跳过` — 无 modelSelection 时不切换

- [ ] **Step 3: 编写 AfterToolCall 测试**

- `TestTaskPlanningRail_AfterToolCall_刷新缓存` — todo 工具调用后缓存刷新
- `TestTaskPlanningRail_AfterToolCall_进度提醒` — 到达间隔时注入 UserMessage
- `TestTaskPlanningRail_AfterToolCall_未达间隔跳过` — 未到达间隔不注入

- [ ] **Step 4: 编写 AfterModelCall 测试**

- `TestTaskPlanningRail_AfterModelCall_Token统计` — 按 model_id 分桶累加

- [ ] **Step 5: 编写 AfterInvoke 测试**

- `TestTaskPlanningRail_AfterInvoke_清理缓存` — 清理 todosCache/toolCallCounts/usageRecords

- [ ] **Step 6: 编写 AfterTaskIteration 测试**

- `TestTaskPlanningRail_AfterTaskIteration_TodoTaskPlan同步` — plan 状态变更同步到 todo 文件
- `TestTaskPlanningRail_AfterTaskIteration_无变更时跳过` — plan 和 todo 状态一致时不写入

- [ ] **Step 7: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test -v ./internal/agentcore/harness/rails/...
```

- [ ] **Step 8: Commit**

```bash
git add internal/agentcore/harness/rails/task_planning_test.go
git commit -m "test(harness): TaskPlanningRail 完整测试覆盖"
```

---

## Task 10: 回填 — factory.go + doc.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/harness/factory.go`
- Modify: `internal/agentcore/harness/rails/doc.go`
- Modify: `docs/superpowers/specs/IMPLEMENTATION_PLAN.md`（如果路径不同则修正）

- [ ] **Step 1: 回填 factory.go — 替换 TaskPlanningRail 占位**

将 L517-521 的：
```go
if params.EnableTaskPlanning && !alreadyProvidedByType(userProvidedTypes, "TaskPlanningRail") {
    // ⤵️ 9.19-9.24 回填：TaskPlanningRail 具体实例化（含 model_selection）
    agent.AddRail(agentinterfaces.NewBaseRail())
    logger.Debug(logComponent).Msg("已添加默认 TaskPlanningRail 占位，⤵️ 9.19-9.24 回填")
}
```
替换为：
```go
if params.EnableTaskPlanning && !alreadyProvidedByType(userProvidedTypes, "TaskPlanningRail") {
    tpRail := rails.NewTaskPlanningRail(
        rails.WithModelSelection(config.ModelSelection),
    )
    agent.AddRail(tpRail)
    logger.Debug(logComponent).Msg("已添加 TaskPlanningRail")
}
```

- [ ] **Step 2: 更新 rails/doc.go 文件目录**

在 `rails/doc.go` 的文件目录中添加 `task_planning.go` 条目，并更新包功能概述：

```go
// Package rails 提供 DeepAgent 扩展 Rail 实现。
//
// 在 single_agent/rail 基础上增加：
//   - DeepAgentRail 基类：扩展 AgentRail，增加 workspace/sys_operation 和 task-iteration hooks
//   - DeepAgentRailProvider 接口：DeepAgentRail 提供者接口，用于类型断言注入依赖
//   - ProgressiveToolRail：渐进式工具权限 Rail
//   - TaskCompletionRail：任务完成检测 Rail
//   - TaskPlanningRail：任务规划 Rail（注册 todo 工具、注入规划提示词、模型切换、进度提醒、token 统计、Todo↔TaskPlan 同步）
//
// 文件目录：
//
//	rails/
//	├── doc.go              # 包文档
//	├── base.go             # DeepAgentRail 基类 + DeepAgentRailProvider 接口
//	├── progressive.go      # ProgressiveToolRail 渐进式工具发现和可调用工具过滤
//	├── task_completion.go  # TaskCompletionRail 任务完成检测
//	└── task_planning.go    # TaskPlanningRail 任务规划
//
// 对应 Python 代码：openjiuwen/harness/rails/
package rails
```

- [ ] **Step 3: 创建/更新 tools/doc.go**

检查 `internal/agentcore/harness/tools/` 目录下是否有 `doc.go`。如果没有则创建：

```go
// Package tools 提供 DeepAgent Harness 工具实现。
//
// 子包：
//   - todo/：Todo 待办事项工具（create/list/get/modify）
//   - subagent/：会话子进程工具（spawn/cancel/list）
//   - tool_discovery/：渐进式工具发现元工具（search_tools/load_tools）
//
// 对应 Python 代码：openjiuwen/harness/tools/
package tools
```

注意：tools/ 目录可能不是 Go 包（只有子目录），如果子目录各自是独立包则 tools/ 下不需要 doc.go。需要检查目录结构。

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

将 9.13 行的状态标记从 `☐` 改为 `✅`。

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/...
```

- [ ] **Step 6: 运行全部测试**

```bash
cd /home/opensource/uap-claw-go && go test -v ./internal/agentcore/harness/...
```

- [ ] **Step 7: Commit**

```bash
git add internal/agentcore/harness/factory.go internal/agentcore/harness/rails/doc.go IMPLEMENTATION_PLAN.md
git commit -m "feat(harness): TaskPlanningRail 回填 — factory 实例化 + doc.go + 实现计划状态更新"
```

---

## Task 11: 最终验证 — 全量编译 + 覆盖率

- [ ] **Step 1: 全量编译**

```bash
cd /home/opensource/uap-claw-go && go build ./...
```

- [ ] **Step 2: 覆盖率检查**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/rails/... ./internal/agentcore/harness/tools/todo/...
```

目标：rails 包覆盖率 ≥ 85%，todo 包覆盖率 ≥ 85%。

- [ ] **Step 3: Commit（如有覆盖率补充修复）**

```bash
git add -A && git commit -m "test(harness): TaskPlanningRail + TodoTool 覆盖率补充"
```
