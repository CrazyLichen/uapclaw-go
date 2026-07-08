# 9.13 TaskPlanningRail + TodoTool 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.13 TaskPlanningRail（任务规划 Rail）及其核心前置依赖 TodoTool 工具包，使 DeepAgent 支持任务规划、模型切换、进度提醒、token 统计和 Todo↔TaskPlan 状态同步。

**Architecture:** TaskPlanningRail 嵌入 DeepAgentRail，通过 7 个钩子方法与 DeepAgent 任务循环交互。Bridge 事件（BeforeModelCall/AfterModelCall/AfterToolCall）中 cbc.Agent() 是 ReActAgent，通过最小接口断言（modelSwitcher）获取模型切换能力；Deep 事件（AfterTaskIteration）中 cbc.Agent() 是 DeepAgent，通过最小接口断言（deepStateLoader）获取状态加载能力。TodoTool 包提供 4 个工具的持久化实现。

**Tech Stack:** Go 1.22+, 标准库 sync/json, 内部包: foundation/tool, harness/schema, harness/prompts, sys_operation, single_agent/interfaces, runner

**关键约束：**
1. 涉及 tool 的提示词必须与 Python 一比一复刻（`prompts/tools/todo.go` 和 `prompts/sections/todo.go` 已实现）
2. Rail 中最小接口断言失败必须记录日志（分层注册后理论上都能满足，但防御性日志不可省略）
3. TodoTool 的包装参考已有 tool（如 `harness/tools/subagent/session_tools.go`），使用 `tool.NewTool` + `BuildToolCard` 模式

---

## 文件结构

### 新增文件
| 文件 | 职责 |
|------|------|
| `internal/agentcore/harness/tools/todo/doc.go` | 包文档 |
| `internal/agentcore/harness/tools/todo/todo.go` | TodoLockManager + TodoTool 基类 + TodoCreateTool/ListTool/GetTool/ModifyTool + CreateTodosTool 工厂 |
| `internal/agentcore/harness/tools/todo/todo_test.go` | TodoTool 完整测试 |
| `internal/agentcore/harness/rails/task_planning.go` | TaskPlanningRail 结构体 + 最小接口 + 7 个钩子方法 |
| `internal/agentcore/harness/rails/task_planning_test.go` | TaskPlanningRail 测试 |

### 修改文件
| 文件 | 修改内容 |
|------|---------|
| `internal/agentcore/single_agent/agents/react_helpers.go` | 新增 `ReActAgent.SwitchModel(model)` 方法 |
| `internal/agentcore/harness/deep_agent.go` | 将 `deepAgentRailProvider` 移到 rails/base.go，引用新导出名 |
| `internal/agentcore/harness/rails/base.go` | 新增导出接口 `DeepAgentRailProvider` |
| `internal/agentcore/harness/factory.go` | L518-520: 占位替换为真正 TaskPlanningRail |
| `internal/agentcore/harness/rails/doc.go` | 文件目录新增 task_planning.go |
| `internal/agentcore/harness/tools/doc.go` | 文件目录新增 todo/ 子包 |
| `IMPLEMENTATION_PLAN.md` | 9.13 状态 ☐ → ✅ |

---

## Task 1: ReActAgent.SwitchModel 方法

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_helpers.go`
- Modify: `internal/agentcore/single_agent/agents/react_helpers_test.go`

- [ ] **Step 1: 在 react_helpers.go 新增 SwitchModel 方法**

在 `SetLLM` 方法后添加：

```go
// SwitchModel 切换模型并同步配置。
// 对齐 Python: ctx.agent.set_llm(target_model) + ctx.agent.config.model_name = target_model.model_config.model_name
// 封装 SetLLM + Config.ModelNameVal 同步，供 TaskPlanningRail 通过 modelSwitcher 最小接口调用。
func (a *ReActAgent) SwitchModel(model *llm.Model) {
	a.SetLLM(model)
	if a.config != nil && model != nil && model.ModelConfig != nil {
		a.config.ModelNameVal = model.ModelConfig.ModelName
	}
}
```

- [ ] **Step 2: 编写 SwitchModel 测试**

```go
func TestReActAgent_SwitchModel(t *testing.T) {
	card := agentschema.NewAgentCard("test", "test", "test")
	config := saconfig.NewReActAgentConfig(saconfig.WithModelName("old-model"))
	agent := agents.NewReActAgent(card, config)

	newModel := llm.NewModel(/* 合法 ModelClientConfig + ModelRequestConfig */)
	agent.SwitchModel(newModel)

	got, err := agent.GetLLM()
	assert.NoError(t, err)
	assert.Equal(t, newModel, got)
	assert.Equal(t, newModel.ModelConfig.ModelName, config.ModelNameVal)
}
```

- [ ] **Step 3: 运行测试**

Run: `go test -run TestReActAgent_SwitchModel ./internal/agentcore/single_agent/agents/ -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_helpers.go internal/agentcore/single_agent/agents/react_helpers_test.go
git commit -m "feat: 新增 ReActAgent.SwitchModel 封装 SetLLM + Config 同步"
```

---

## Task 2: DeepAgentRailProvider 迁移

**Files:**
- Modify: `internal/agentcore/harness/rails/base.go`
- Modify: `internal/agentcore/harness/deep_agent.go`

- [ ] **Step 1: 在 rails/base.go 新增导出接口 DeepAgentRailProvider**

在 `DeepAgentRail` 结构体定义之后、导出函数区块内添加：

```go
// ──────────────────────────── 导出接口 ────────────────────────────

// DeepAgentRailProvider DeepAgentRail 提供者接口。
// 对齐 Python: isinstance(rail_inst, DeepAgentRail) 类型检查。
// 嵌入 DeepAgentRail 的子类自动满足此接口。
type DeepAgentRailProvider interface {
	SetSysOperation(op sysop.SysOperation)
	SetWorkspace(w *workspace.Workspace)
}
```

- [ ] **Step 2: 在 deep_agent.go 删除非导出接口 deepAgentRailProvider，改用 rails.DeepAgentRailProvider**

1. 删除 `deepAgentRailProvider` 接口定义（L1070-1076）
2. 将 `deepAgentRailProvider` 的使用处（L1621）改为 `rails.DeepAgentRailProvider`
3. 确保编译通过

- [ ] **Step 3: 运行编译验证**

Run: `go build ./internal/agentcore/harness/...`
Expected: 无错误

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/base.go internal/agentcore/harness/deep_agent.go
git commit -m "refactor: 迁移 deepAgentRailProvider 到 rails/base.go 导出为 DeepAgentRailProvider"
```

---

## Task 3: TodoTool 包 — doc.go + TodoLockManager + TodoTool 基类

**Files:**
- Create: `internal/agentcore/harness/tools/todo/doc.go`
- Create: `internal/agentcore/harness/tools/todo/todo.go`（TodoLockManager + TodoTool 基类部分）

**Python 参考**: `openjiuwen/harness/tools/todo.py` L24-182

- [ ] **Step 1: 创建 doc.go**

```go
// Package todo 提供 Todo 任务管理工具集。
//
// 包含 4 个工具（TodoCreateTool/TodoListTool/TodoGetTool/TodoModifyTool），
// 以及共享的 TodoLockManager（并发安全）和 TodoTool 基类（持久化读写）。
//
// 文件目录：
//
//	todo/
//	├── doc.go           # 包文档
//	├── todo.go          # TodoLockManager + TodoTool 基类 + 4 个工具 + 工厂函数
//	└── todo_test.go     # 完整测试
//
// 对应 Python 代码：openjiuwen/harness/tools/todo.py
package todo
```

- [ ] **Step 2: 创建 todo.go — TodoLockManager + TodoTool 基类**

按 Go 编码规范声明顺序，编写 TodoLockManager（sync.RWMutex + session 级 sync.Mutex）和 TodoTool 基类（LoadTodos/SaveTodos/GetFilePath/CleanupSession）。

关键实现要点：
- `TodoLockManager.Operation(sessionID)` 返回 `*sync.Mutex`，获取时用 RLock，创建时用 Lock
- `TodoTool.LoadTodos` 通过 `fs.ReadFile` 读取 JSON，`TodoItem{}.FromDict()` 反序列化
- `TodoTool.SaveTodos` 通过 `json.Marshal` + `fs.WriteFile` 写入，使用 `TodoItem.ToDict()` 序列化
- 错误码使用 `exception.NewWithCode(StatusCodeToolTodosLoadFailed, ...)`

- [ ] **Step 3: 编译验证**

Run: `go build ./internal/agentcore/harness/tools/todo/...`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/todo/
git commit -m "feat(todo): TodoLockManager + TodoTool 基类持久化读写"
```

---

## Task 4: TodoTool 包 — TodoCreateTool 实现

**Files:**
- Modify: `internal/agentcore/harness/tools/todo/todo.go`

**Python 参考**: `openjiuwen/harness/tools/todo.py` L184-320

- [ ] **Step 1: 实现 TodoCreateTool**

使用 `tool.NewTool` + `prompts/tools.BuildToolCard("todo_create", ...)` 模式，对齐 Python `TodoCreateTool.invoke()` + `_create_from_list()`。

关键逻辑：
- 输入：`tasks` JSON 数组，每项含 `content`/`activeForm`/`description`/`selected_model_id`/`id`
- 首个任务 `in_progress`，其余 `pending`
- ID 优先使用传入值，无则 `uuid.New().String()`
- 校验：`content`/`activeForm`/`description` 不可为空，ID 不可重复
- 输出：格式化的创建结果（对齐 Python `_format_create_result`）

- [ ] **Step 2: 编译验证**

Run: `go build ./internal/agentcore/harness/tools/todo/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/tools/todo/todo.go
git commit -m "feat(todo): TodoCreateTool 实现"
```

---

## Task 5: TodoTool 包 — TodoListTool + TodoGetTool 实现

**Files:**
- Modify: `internal/agentcore/harness/tools/todo/todo.go`

**Python 参考**: `openjiuwen/harness/tools/todo.py` L323-449

- [ ] **Step 1: 实现 TodoListTool**

对齐 Python `TodoListTool.invoke()`：加载 todos，过滤掉 `Completed`/`Cancelled`，返回简化视图 `{"tasks": [{"id", "content", "status", "depends_on"}]}`。

- [ ] **Step 2: 实现 TodoGetTool**

对齐 Python `TodoGetTool.invoke()`：按 ID 查找，返回完整详情 `{"todo": TodoItem.ToDict()}`。

- [ ] **Step 3: 编译验证**

Run: `go build ./internal/agentcore/harness/tools/todo/...`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/todo/todo.go
git commit -m "feat(todo): TodoListTool + TodoGetTool 实现"
```

---

## Task 6: TodoTool 包 — TodoModifyTool 实现（6 种 action）

**Files:**
- Modify: `internal/agentcore/harness/tools/todo/todo.go`

**Python 参考**: `openjiuwen/harness/tools/todo.py` L452-753

- [ ] **Step 1: 实现 TodoModifyTool**

单 invoke + switch 分支，对齐 Python `TodoModifyTool.invoke()`。6 种 action：
- `update`：批量更新字段（content/activeForm/description/status/selected_model_id）
- `delete`：按 ID 列表删除
- `cancel`：按 ID 列表标记取消
- `append`：追加新任务（校验 ID 不重复）
- `insert_after`：在目标任务后插入（目标状态须 in_progress/pending）
- `insert_before`：在目标任务前插入（目标状态须 pending）

校验逻辑（对齐 Python）：
- `validateSingleInProgress`：最多一个 in_progress
- `validateTargetTaskStatus`：目标状态须在允许列表内
- `validateSingleTodoItem`：必填字段 content/activeForm/description/status/id

- [ ] **Step 2: 实现 CreateTodosTool 工厂函数**

对齐 Python `create_todos_tool()`：创建共享 `TodoLockManager`，构造 4 个工具实例。

- [ ] **Step 3: 编译验证**

Run: `go build ./internal/agentcore/harness/tools/todo/...`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/todo/todo.go
git commit -m "feat(todo): TodoModifyTool 6种action + CreateTodosTool 工厂"
```

---

## Task 7: TodoTool 包测试

**Files:**
- Create: `internal/agentcore/harness/tools/todo/todo_test.go`

- [ ] **Step 1: 编写 TodoLockManager 测试**

测试：Operation 返回同 session 同一把锁、不同 session 不同锁、CleanupSession 删除锁。

- [ ] **Step 2: 编写 TodoTool 基类测试**

测试：LoadTodos 从 JSON 文件加载、SaveTodos 写入并读回验证、文件不存在时返回错误。

- [ ] **Step 3: 编写 TodoCreateTool 测试**

测试：创建多任务（首个 in_progress）、ID 去重校验、必填字段校验、格式化输出。

- [ ] **Step 4: 编写 TodoListTool/TodoGetTool 测试**

测试：ListTool 过滤 completed/cancelled、GetTool 按 ID 查找、ID 不存在返回错误。

- [ ] **Step 5: 编写 TodoModifyTool 测试**

测试 6 种 action：update/delete/cancel/append/insert_after/insert_before，以及边界校验（多 in_progress、目标状态不允许、ID 重复）。

- [ ] **Step 6: 运行测试并确认覆盖率**

Run: `go test -cover ./internal/agentcore/harness/tools/todo/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/tools/todo/todo_test.go
git commit -m "test(todo): TodoLockManager + TodoTool 完整测试"
```

---

## Task 8: TaskPlanningRail 结构体 + Init/Uninit + 最小接口

**Files:**
- Create: `internal/agentcore/harness/rails/task_planning.go`

**Python 参考**: `openjiuwen/harness/rails/task_planning_rail.py` L32-149

- [ ] **Step 1: 定义最小接口 + TaskPlanningRail 结构体 + 构造函数**

在 `task_planning.go` 中：

1. 非导出最小接口（rails 包内）：
```go
type modelSwitcher interface {
	SwitchModel(model *llm.Model)
	GetLLM() (*llm.Model, error)
}

type deepStateLoader interface {
	LoadState(sess sessioninterfaces.SessionFacade) *hschema.DeepAgentState
}
```

2. TaskPlanningRail 结构体（嵌入 DeepAgentRail）+ NewTaskPlanningRail 构造函数 + Option 模式
3. 优先级 90（对齐 Python `priority = 90`）

- [ ] **Step 2: 实现 Init/Uninit**

- `Init(agent)`：注册 4 个 todo 工具到 agent 的 ability_manager + runner 资源管理器，获取 workspace 目录，设置 systemPromptBuilder
- `Uninit(agent)`：移除 todo 工具 + 移除 todo 提示词节

对齐 Python `init()` / `uninit()`。

- [ ] **Step 3: 编译验证**

Run: `go build ./internal/agentcore/harness/rails/...`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/task_planning.go
git commit -m "feat(rails): TaskPlanningRail 结构体 + Init/Uninit + 最小接口"
```

---

## Task 9: TaskPlanningRail 钩子方法实现

**Files:**
- Modify: `internal/agentcore/harness/rails/task_planning.go`

**Python 参考**: `openjiuwen/harness/rails/task_planning_rail.py` L153-401

- [ ] **Step 1: 实现 BeforeModelCall**

对齐 Python `before_model_call()`：
1. 注入 todo 提示词节：`sections.BuildTodoSection()` → `systemPromptBuilder.AddSection()`
2. 模型切换：断言 `modelSwitcher`，`getInProgressModelID()` → `switcher.SwitchModel(targetModel)`
3. 断言失败记录 Warn 日志

- [ ] **Step 2: 实现 AfterToolCall**

对齐 Python `after_tool_call()`：
1. todo 工具检测：`strings.HasPrefix(toolName, "todo_")` → 刷新 `todosCache`
2. 进度提醒：每 N 次工具调用注入 UserMessage（`cbc.ModelContext().AddMessages()`）

- [ ] **Step 3: 实现 AfterModelCall**

对齐 Python `after_model_call()`：
1. 断言 `modelSwitcher` → `GetLLM()` 获取 model_id
2. 从 `ModelCallInputs.Response.UsageMetadata` 获取 token 使用量
3. 累加到 `usageRecords`

- [ ] **Step 4: 实现 AfterTaskIteration**

对齐 Python `after_task_iteration()` + `_sync_todos_from_plan()`：
1. 断言 `deepStateLoader` → `LoadState()` 获取 TaskPlan
2. 对比 TaskPlan.tasks 与 todos 文件状态，如有差异则 `SaveTodos()` 同步
3. 断言失败记录 Debug 日志

- [ ] **Step 5: 实现 AfterInvoke**

对齐 Python `after_invoke()`：
1. 输出 token 汇总日志
2. 清理 `todosCache`/`toolCallCounts`/`usageRecords`
3. 调用 `todoTool.CleanupSession()` 清理锁

- [ ] **Step 6: 实现 GetCallbacks**

覆盖基类 `GetCallbacks()`，声明全部 7 个钩子映射（Init/Uninit/BeforeModelCall/AfterToolCall/AfterModelCall/AfterInvoke/AfterTaskIteration），对齐 TaskCompletionRail 的 `GetCallbacks` 模式。

- [ ] **Step 7: 编译验证**

Run: `go build ./internal/agentcore/harness/rails/...`

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/harness/rails/task_planning.go
git commit -m "feat(rails): TaskPlanningRail 7个钩子方法实现"
```

---

## Task 10: TaskPlanningRail 测试

**Files:**
- Create: `internal/agentcore/harness/rails/task_planning_test.go`

- [ ] **Step 1: 编写 NewTaskPlanningRail 构造测试**

测试：默认值、Option 覆盖、优先级 90。

- [ ] **Step 2: 编写 BeforeModelCall 测试**

测试：提示词注入、模型切换（mock modelSwitcher）、无 modelSelection 时跳过切换、断言失败记录日志。

- [ ] **Step 3: 编写 AfterToolCall 测试**

测试：todo_ 前缀匹配刷新缓存、进度提醒注入（mock ModelContext）、非 todo 工具不触发。

- [ ] **Step 4: 编写 AfterModelCall 测试**

测试：token 累加、UsageMetadata 为 nil 时跳过、model_id 读取。

- [ ] **Step 5: 编写 AfterTaskIteration 测试**

测试：TaskPlan → todos 状态同步（mock deepStateLoader）、无 TaskPlan 时跳过、断言失败记录日志。

- [ ] **Step 6: 编写 AfterInvoke 测试**

测试：token 汇总日志、缓存清理、CleanupSession 调用。

- [ ] **Step 7: 运行测试并确认覆盖率**

Run: `go test -cover ./internal/agentcore/harness/rails/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/harness/rails/task_planning_test.go
git commit -m "test(rails): TaskPlanningRail 完整测试"
```

---

## Task 11: 回填 — factory.go + doc.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/harness/factory.go`
- Modify: `internal/agentcore/harness/rails/doc.go`
- Modify: `internal/agentcore/harness/tools/doc.go`（或父级）
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: factory.go 替换 TaskPlanningRail 占位**

将 L518-520 的 `agentinterfaces.NewBaseRail()` 占位替换为真正的 `rails.NewTaskPlanningRail(...)` 构造，传入 `enableProgressRepeat`、`listToolCallInterval`、`modelSelection` 参数。

- [ ] **Step 2: 更新 rails/doc.go 文件目录**

在文件目录树中新增 `task_planning.go` 条目。

- [ ] **Step 3: 更新 tools/doc.go 文件目录**

新增 `todo/` 子包条目。

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

将 9.13 状态从 `☐` 改为 `✅`。

- [ ] **Step 5: 运行全量编译验证**

Run: `go build ./internal/agentcore/...`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/factory.go internal/agentcore/harness/rails/doc.go internal/agentcore/harness/tools/doc.go IMPLEMENTATION_PLAN.md
git commit -m "feat: 9.13 TaskPlanningRail 回填 factory/doc/plan"
```

---

## Task 12: 最终验证 — 全量编译 + 覆盖率

- [ ] **Step 1: 全量编译**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 2: 运行全部测试**

Run: `go test ./internal/agentcore/harness/tools/todo/... ./internal/agentcore/harness/rails/... -cover`
Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 3: 确认日志同步**

对照 Python `task_planning_rail.py` 中的所有 `logger.` 调用，确认 Go 代码中等价位置有对应日志。

- [ ] **Step 4: 提交最终状态**

```bash
git commit -m "chore: 9.13 TaskPlanningRail 最终验证通过"
```
