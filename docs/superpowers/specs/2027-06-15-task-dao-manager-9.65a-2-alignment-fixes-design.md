# 9.65a-2 差异修复设计文档

## 1. 概述

本章节修复 9.65a-2（TaskDao + TaskManager）实现与 Python 原版之间的 5 项差异，同时完成命名对齐。

**修复范围**：InMemory 实现、SQL 实现、TaskDao 接口、TeamTaskManager 业务层。

**不修改的部分**：
- FSM 状态转换表（已正确对齐）
- messager 事件发布（9.65-1 已回填完成）
- reset_task 返回值（Manager 层效果等价，无需修复）

## 2. 差异修复清单

### 2.1 差异 1：loadEndpointsAndValidate 拒绝状态范围

**问题**：InMemory 的 `loadEndpointsAndValidate` 仅拒绝上游（DependsOnID）的 COMPLETED/CANCELLED 两种终态。Python 的 `TASK_DEPENDENCY_REJECT_STATUSES` 包含 4 种状态（COMPLETED / CANCELLED / CLAIMED / PLAN_APPROVED），且检查对象是**源任务（TaskID，即被阻塞的下游任务）**。

**Python 参考**：
```python
# openjiuwen/agent_teams/tools/database/graph.py
TASK_DEPENDENCY_REJECT_STATUSES = frozenset({
    TaskStatus.COMPLETED, TaskStatus.CANCELLED,
    TaskStatus.CLAIMED, TaskStatus.PLAN_APPROVED,
})

# task_dao.py _load_endpoints_and_validate
src_status = taskMap[e.TaskID].Status
if src_status in TASK_DEPENDENCY_REJECT_STATUSES:
    raise _MutationFailure(f"Cannot add dependency to {e.TaskID} in terminal or executing status: {src_status}")
```

**修复**：

| 文件 | 修改 |
|------|------|
| `database/memory_impl.go` L962-992 | 将检查对象改为 `downstream.Status`（e.TaskID），拒绝范围扩展为 4 种状态 |

```go
// 修复后
srcStatus := downstream.Status
if srcStatus == fsm.TaskStatusCompleted || srcStatus == fsm.TaskStatusCancelled ||
   srcStatus == fsm.TaskStatusClaimed || srcStatus == fsm.TaskStatusPlanApproved {
    mc.failReason = "cannot add dependency to " + edge.TaskID + " in terminal or executing status: " + srcStatus
    mc.rollbackStagedTasks()
    return
}
```

**注意**：`sql_task_dao.go:231-246` 已经正确对齐（检查 `e.TaskID` + 4 种状态），无需修改。

### 2.2 差异 2：Add 方法有依赖时的原子路径

**问题**：Go 的 `Add` 有依赖时先 `CreateTask` 再调 `AddTaskWithBidirectionalDependencies`（两步非原子），如果第二步失败（环检测）则数据不一致。Python 有依赖时直接走 `mutate_dependency_graph`（一步原子）。

**Python 参考**：
```python
# task_manager.py add()
if dependencies:
    mutation = await self.db.task.mutate_dependency_graph(
        team_name=self.team_name,
        new_tasks=[NewTaskSpec(task_id=task_id, title=title, content=content, initial_status=status)],
        add_edges=[(task_id, dep_id) for dep_id in dependencies],
    )
    # 从 refreshed_tasks 中提取新任务的最终状态
    for refreshed in mutation.refreshed_tasks:
        if refreshed.task_id == task_id:
            status = refreshed.status
            break
else:
    success = await self.db.task.create_task(...)
```

**修复**：

| 文件 | 修改 |
|------|------|
| `database/models.go` L164 | `RefreshedTasks []string` → `RefreshedTasks []*TeamTaskBase`（对齐 Python 返回对象列表） |
| `database/memory_impl.go` | `refreshStatus` 返回 `[]*TeamTaskBase` 而非 `[]string` |
| `database/sql_task_dao.go` | SQL MutateDependencyGraph 的 refreshed 返回值同步修改 |
| `tools/task_manager.go` Add | 有依赖时走 `MutateDependencyGraph`；无依赖时仍走 `CreateTask` |

**RefreshedTasks 类型修改的连锁影响**：

| 调用方 | 当前用法 | 修改后 |
|--------|---------|--------|
| `CompleteTask` 返回值 | `[]string` | `[]string`（保持 ID 列表，Manager 层只需 ID） |
| `CancelTask` 返回值 | `[]string` | `[]string`（保持 ID 列表） |
| `publishUnblockedEvents` | 接收 `[]string` | 接收 `[]string`（保持不变） |
| `GraphMutationResult.RefreshedTasks` | `[]string` | `[]*TeamTaskBase`（改为对象列表） |

> **设计决策**：`CompleteTask` / `CancelTask` 返回值保持 `[]string`（unblocked task ID 列表），因为 Manager 层和事件发布只需要 ID。仅 `GraphMutationResult.RefreshedTasks` 改为 `[]*TeamTaskBase`，因为 `Add` 方法需要从中提取新任务的最终状态。

**Manager 层 Add 方法修复后的结构**：

```go
func (tm *TeamTaskManager) Add(ctx context.Context, title, content string, opts ...TaskAddOption) (*database.TeamTaskBase, error) {
    cfg := &taskAddConfig{}
    for _, opt := range opts { opt(cfg) }
    taskID := cfg.taskID
    if taskID == "" { taskID = generateTaskID(tm.teamName) }

    if len(cfg.dependencies) > 0 {
        // 有依赖：走 MutateDependencyGraph 原子路径（对齐 Python）
        newTaskSpec := database.NewTaskSpec{
            TaskID: taskID, Title: title, Content: content,
            InitialStatus: fsm.TaskStatusPending,
        }
        edges := make([]database.EdgeSpec, 0, len(cfg.dependencies))
        for _, depID := range cfg.dependencies {
            edges = append(edges, database.EdgeSpec{TaskID: taskID, DependsOnID: depID})
        }
        result := tm.db.Task().MutateDependencyGraph(ctx, tm.teamName, []database.NewTaskSpec{newTaskSpec}, edges)
        if !result.Ok {
            return nil, fmt.Errorf("创建任务失败: %s", result.Reason)
        }
        // 从 RefreshedTasks 中提取新任务的最终状态（可能 PENDING→BLOCKED）
        task := findRefreshedTask(result.RefreshedTasks, taskID)
        if task == nil { task, _ = tm.db.Task().GetTask(ctx, taskID) }
        tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{...})
        return task, nil
    }
    // 无依赖：走 CreateTask
    task := &database.TeamTaskBase{...}
    ok, err := tm.db.Task().CreateTask(ctx, task)
    // ...
}
```

### 2.3 差异 3：AddWithPriority 双向依赖 + 命名对齐 + AddAsTopPriority 修复

#### 3a. TaskDao 接口修改

**修改前**：
```go
AddTaskWithBidirectionalDependencies(ctx context.Context, teamName string, task *TeamTaskBase, dependsOnIDs []string) GraphMutationResult
```

**修改后**（命名对齐 Python: `dependencies` + `dependent_task_ids`）：
```go
AddTaskWithBidirectionalDependencies(ctx context.Context, teamName string, task *TeamTaskBase, dependencies []string, dependentTaskIDs []string) GraphMutationResult
```

**InMemory 实现**：
```go
func (db *InMemoryTeamDatabase) AddTaskWithBidirectionalDependencies(_ context.Context, teamName string, task *TeamTaskBase, dependencies []string, dependentTaskIDs []string) GraphMutationResult {
    newTaskSpec := NewTaskSpec{TaskID: task.TaskID, Title: task.Title, Content: task.Content, InitialStatus: task.Status}
    if newTaskSpec.InitialStatus == "" { newTaskSpec.InitialStatus = fsm.TaskStatusPending }

    edges := make([]EdgeSpec, 0, len(dependencies)+len(dependentTaskIDs))
    // dependencies：新任务依赖上游 → 边方向 (new_task, upstream)
    for _, depID := range dependencies {
        edges = append(edges, EdgeSpec{TaskID: task.TaskID, DependsOnID: depID})
    }
    // dependentTaskIDs：下游依赖新任务 → 边方向 (existing_task, new_task)
    for _, depID := range dependentTaskIDs {
        edges = append(edges, EdgeSpec{TaskID: depID, DependsOnID: task.TaskID})
    }
    return db.MutateDependencyGraph(context.Background(), teamName, []NewTaskSpec{newTaskSpec}, edges)
}
```

**SQL 实现同步修改**。

#### 3b. TeamTaskManager.AddWithPriority 修改

**修改前**：
```go
func (tm *TeamTaskManager) AddWithPriority(ctx context.Context, taskID, title, content string, dependsOnIDs []string, newTasksSpec []database.NewTaskSpec) (database.GraphMutationResult, error)
```

**修改后**（命名对齐 Python，删 newTasksSpec，用 Option 模式替代 Python 关键字参数）：
```go
type TaskAddWithPriorityOption func(*taskAddWithPriorityConfig)
type taskAddWithPriorityConfig struct {
    taskID           string
    dependencies     []string    // 对齐 Python: dependencies
    dependentTaskIDs []string    // 对齐 Python: dependent_task_ids
}

func WithPriorityTaskID(taskID string) TaskAddWithPriorityOption { ... }
func WithPriorityDependencies(deps []string) TaskAddWithPriorityOption { ... }
func WithPriorityDependentTaskIDs(ids []string) TaskAddWithPriorityOption { ... }

func (tm *TeamTaskManager) AddWithPriority(ctx context.Context, title, content string, opts ...TaskAddWithPriorityOption) (*database.TeamTaskBase, error) {
    cfg := &taskAddWithPriorityConfig{}
    for _, opt := range opts { opt(cfg) }
    taskID := cfg.taskID
    if taskID == "" { taskID = generateTaskID(tm.teamName) }

    // 初始状态逻辑（对齐 Python: 有 dependencies → BLOCKED，否则 PENDING）
    initialStatus := fsm.TaskStatusPending
    if len(cfg.dependencies) > 0 {
        initialStatus = fsm.TaskStatusBlocked
    }

    task := &database.TeamTaskBase{
        TaskID: taskID, TeamName: tm.teamName, Title: title, Content: content, Status: initialStatus,
    }
    result := tm.db.Task().AddTaskWithBidirectionalDependencies(ctx, tm.teamName, task, cfg.dependencies, cfg.dependentTaskIDs)
    if !result.Ok {
        return nil, fmt.Errorf("带依赖创建任务失败: %s", result.Reason)
    }
    // 从 RefreshedTasks 中取最终状态
    created := findRefreshedTask(result.RefreshedTasks, taskID)
    if created == nil { created, _ = tm.db.Task().GetTask(ctx, taskID) }
    tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{...})
    return created, nil
}
```

#### 3c. AddAsTopPriority 调用路径修复

**修改前**（两步非原子）：
```go
ok, _ := tm.db.Task().CreateTask(ctx, task)
pendingTasks, _ := tm.db.Task().GetTeamTasks(ctx, tm.teamName, fsm.TaskStatusPending)
edges := ...
tm.db.Task().MutateDependencyGraph(ctx, tm.teamName, nil, edges)
```

**修改后**（对齐 Python：走 `add_task_with_bidirectional_dependencies` 一步原子）：
```go
func (tm *TeamTaskManager) AddAsTopPriority(ctx context.Context, title, content string, opts ...TaskAddOption) (*database.TeamTaskBase, error) {
    cfg := &taskAddConfig{}
    for _, opt := range opts { opt(cfg) }
    taskID := cfg.taskID
    if taskID == "" { taskID = generateTaskID(tm.teamName, "priority") }

    pendingTasks, _ := tm.db.Task().GetTeamTasks(ctx, tm.teamName, fsm.TaskStatusPending)
    var dependentTaskIDs []string
    for _, t := range pendingTasks {
        if t.TaskID != taskID { dependentTaskIDs = append(dependentTaskIDs, t.TaskID) }
    }
    task := &database.TeamTaskBase{
        TaskID: taskID, TeamName: tm.teamName, Title: title, Content: content, Status: fsm.TaskStatusPending,
    }
    result := tm.db.Task().AddTaskWithBidirectionalDependencies(ctx, tm.teamName, task, nil, dependentTaskIDs)
    if !result.Ok {
        return nil, fmt.Errorf("创建最高优先级任务失败: %s", result.Reason)
    }
    created := findRefreshedTask(result.RefreshedTasks, taskID)
    if created == nil { created = task }
    tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{...})
    return created, nil
}
```

### 2.4 差异 4：CancelAllTasks Unblocked 返回类型

**问题**：Go 的 `CancelAllTasksResult.Unblocked` 是 `[]string`（ID 列表），Python 返回任务对象列表。

**修复**：

| 文件 | 修改 |
|------|------|
| `database/models.go` L173 | `Unblocked []string` → `Unblocked []*TeamTaskBase` |
| `database/memory_impl.go` | `unblockedSet` 从 `map[string]bool` 改为 `map[string]*TeamTaskBase`；排除自身被取消时用 task 对象判断 |
| `database/sql_task_dao.go` | SQL CancelAllTasks 同步修改 |
| `tools/task_manager.go` | `publishUnblockedEvents` 改为接收 `[]*TeamTaskBase`，从中取 `.TaskID` |

```go
// models.go 修改后
type CancelAllTasksResult struct {
    Cancelled []*TeamTaskBase
    Unblocked []*TeamTaskBase
}

// task_manager.go publishUnblockedEvents 修改后
func (tm *TeamTaskManager) publishUnblockedEvents(ctx context.Context, unblockedTasks []*database.TeamTaskBase) {
    for _, t := range unblockedTasks {
        tm.publishTaskEvent(ctx, schema.TaskUnblockedEvent{
            BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
            TaskID:           t.TaskID,
        })
    }
}
```

### 2.5 差异 5：AddBatch 容错 + TaskCreateSpec 扩展

**问题**：
1. Go 的 `AddBatch` 遇错即中断，Python 跳过无效/失败继续
2. Go 的 `TaskCreateSpec` 只有 Title/Content，Python 的 dict 还支持 task_id 和 dependencies

**修复**：

```go
// TaskCreateSpec 扩展（对齐 Python add_batch 的 dict 字段）
type TaskCreateSpec struct {
    TaskID       string   // 可选，空则自动生成
    Title        string
    Content      string
    Dependencies []string // 可选，新任务依赖的上游任务 ID
}

// AddBatch 容错（对齐 Python: 跳过无效/失败不中断）
func (tm *TeamTaskManager) AddBatch(ctx context.Context, specs []TaskCreateSpec) ([]*database.TeamTaskBase, error) {
    var tasks []*database.TeamTaskBase
    for _, spec := range specs {
        if spec.Title == "" || spec.Content == "" {
            logger.Warn(logComponent).Str("spec", fmt.Sprintf("%+v", spec)).Msg("批量创建跳过无效规格")
            continue
        }
        task, err := tm.Add(ctx, spec.Title, spec.Content,
            WithTaskID(spec.TaskID),
            WithDependencies(spec.Dependencies),
        )
        if err != nil {
            logger.Warn(logComponent).Err(err).Msg("批量创建跳过失败任务")
            continue
        }
        tasks = append(tasks, task)
    }
    return tasks, nil
}
```

## 3. 辅助函数

### findRefreshedTask

```go
// findRefreshedTask 从 refreshedTasks 列表中按 taskID 查找任务。
// 对齐 Python: for refreshed in mutation.refreshed_tasks: if refreshed.task_id == task_id
func findRefreshedTask(refreshed []*database.TeamTaskBase, taskID string) *database.TeamTaskBase {
    for _, t := range refreshed {
        if t.TaskID == taskID { return t }
    }
    return nil
}
```

### generateTaskID

将当前散落在各方法中的 taskID 生成逻辑提取为辅助函数：
```go
func generateTaskID(teamName string, suffixes ...string) string {
    base := fmt.Sprintf("task_%s_%d_%d", teamName, time.Now().UnixMilli(), time.Now().UnixNano()%1000)
    if len(suffixes) > 0 { base += "_" + strings.Join(suffixes, "_") }
    return base
}
```

## 4. 修改文件完整清单

| 文件 | 修改内容 |
|------|---------|
| `database/models.go` | `RefreshedTasks []string` → `[]*TeamTaskBase`；`Unblocked []string` → `[]*TeamTaskBase` |
| `database/database.go` | `AddTaskWithBidirectionalDependencies` 签名：`dependsOnIDs` → `dependencies` + 新增 `dependentTaskIDs` |
| `database/memory_impl.go` | loadEndpointsAndValidate 拒绝范围 + AddTaskWithBidirectionalDependencies 双向边 + refreshStatus 返回类型 + CancelAllTasks Unblocked 类型 |
| `database/sql_task_dao.go` | AddTaskWithBidirectionalDependencies 签名同步 + CancelAllTasks Unblocked 类型 |
| `tools/task_manager.go` | Add 原子路径 + AddWithPriority 重构 + AddAsTopPriority 调用路径 + AddBatch 容错 + TaskCreateSpec 扩展 + 命名对齐 + 辅助函数 |
| `tools/task_manager_test.go` | 差异 2/3/5 测试用例 |
| `database/memory_impl_test.go` | 差异 1 测试用例（CLAIMED/PLAN_APPROVED 源任务被拒绝） |
| `database/doc.go` | 更新文件目录（如有新文件） |
| `tools/doc.go` | 更新文件目录 |

## 5. 测试目标

- loadEndpointsAndValidate：CLAIMED/PLAN_APPROVED 源任务添加依赖被拒绝
- Add 有依赖：MutateDependencyGraph 原子路径 + 最终状态正确（PENDING→BLOCKED 翻转）
- AddWithPriority：dependencies + dependentTaskIDs 双向依赖 + 初始 BLOCKED 状态
- AddAsTopPriority：通过 AddTaskWithBidirectionalDependencies 一步原子
- AddBatch：跳过无效规格 + 跳过创建失败 + TaskCreateSpec 扩展字段
- CancelAllTasks：Unblocked 返回任务对象列表
- 覆盖率目标：≥ 85%
