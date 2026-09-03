# 9.65a-2 差异修复实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 9.65a-2（TaskDao + TaskManager）Go 实现与 Python 原版之间的 5 项差异，完成命名对齐。

**Architecture:** 分 5 个 Task 对应 5 项差异修复。每项涉及模型/接口/实现/测试的连锁修改。核心原则：所有图变更走 MutateDependencyGraph 原子路径。

**Tech Stack:** Go 1.x, sync.Mutex, encoding/json, os/filepath

**Spec:** `docs/superpowers/specs/2027-06-15-task-dao-manager-9.65a-2-alignment-fixes-design.md`

---

## 文件结构

| 文件 | 责责 | 变更类型 |
|------|------|---------|
| `internal/agent_teams/tools/database/models.go` | GraphMutationResult.RefreshedTasks + CancelAllTasksResult.Unblocked 类型 | 修改 |
| `internal/agent_teams/tools/database/database.go` | TaskDao 接口：AddTaskWithBidirectionalDependencies 签名 | 修改 |
| `internal/agent_teams/tools/database/memory_impl.go` | loadEndpointsAndValidate + AddTaskWithBidirectionalDependencies + refreshTaskStatuses + CancelAllTasks | 修改 |
| `internal/agent_teams/tools/database/sql_task_dao.go` | AddTaskWithBidirectionalDependencies + CancelAllTasks 同步修改 | 修改 |
| `internal/agent_teams/tools/task_manager.go` | Add + AddWithPriority + AddAsTopPriority + AddBatch + TaskCreateSpec + 辅助函数 | 修改 |
| `internal/agent_teams/tools/database/memory_impl_test.go` | 差异 1 + 差异 4 测试用例 | 修改 |
| `internal/agent_teams/tools/task_manager_test.go` | 差异 2 + 差异 3 + 差异 5 测试用例 | 修改 |

---

## Task 1: 差异 1 — loadEndpointsAndValidate 拒绝范围修复

**Files:**
- Modify: `internal/agent_teams/tools/database/memory_impl.go:962-992`
- Modify: `internal/agent_teams/tools/database/memory_impl_test.go`

- [ ] **Step 1: 修改 loadEndpointsAndValidate**

将检查对象从上游（DependsOnID）改为源任务（TaskID），拒绝范围从 COMPLETED/CANCELLED 扩展为 COMPLETED/CANCELLED/CLAIMED/PLAN_APPROVED。

在 `internal/agent_teams/tools/database/memory_impl.go` 中，替换 `loadEndpointsAndValidate` 方法（L962-992）：

```go
// loadEndpointsAndValidate 步骤2：加载边端点，拒绝缺失/终态/已执行源。
// 对齐 Python: _load_endpoints_and_validate()
// 拒绝规则对齐 Python TASK_DEPENDENCY_REJECT_STATUSES：
// 源任务（TaskID，被阻塞的下游）处于 COMPLETED/CANCELLED/CLAIMED/PLAN_APPROVED 时拒绝。
func (mc *mutationContext) loadEndpointsAndValidate() {
	mc.endpointTasks = make(map[string]*TeamTaskBase)
	for _, edge := range mc.addEdges {
		// 加载下游任务（被阻塞的，即源任务 edge.TaskID）
		downstream, downExists := mc.db.tasks[edge.TaskID]
		if !downExists {
			mc.failReason = "edge endpoint not found: " + edge.TaskID
			mc.rollbackStagedTasks()
			return
		}
		mc.endpointTasks[edge.TaskID] = downstream

		// 加载上游任务（阻塞源 edge.DependsOnID）
		upstream, upExists := mc.db.tasks[edge.DependsOnID]
		if !upExists {
			mc.failReason = "edge endpoint not found: " + edge.DependsOnID
			mc.rollbackStagedTasks()
			return
		}
		mc.endpointTasks[edge.DependsOnID] = upstream

		// 对齐 Python TASK_DEPENDENCY_REJECT_STATUSES：
		// 源任务（edge.TaskID）处于终态或已执行状态时拒绝添加依赖
		srcStatus := downstream.Status
		if srcStatus == fsm.TaskStatusCompleted || srcStatus == fsm.TaskStatusCancelled ||
			srcStatus == fsm.TaskStatusClaimed || srcStatus == fsm.TaskStatusPlanApproved {
			mc.failReason = "cannot add dependency to " + edge.TaskID + " in terminal or executing status: " + srcStatus
			mc.rollbackStagedTasks()
			return
		}
	}
}
```

- [ ] **Step 2: 添加测试用例**

在 `internal/agent_teams/tools/database/memory_impl_test.go` 的 `TestMutateDependencyGraph_终态目标` 测试之后添加：

```go
func TestMutateDependencyGraph_CLAIMED源拒绝(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "m1", "alpha", "M1", "", "active", "teammate", "", "idle", "build_mode", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusClaimed, Assignee: "m1", Title: "已认领"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "上游"})
	// 尝试让已认领任务 t1 依赖 t2（给 CLAIMED 源任务加依赖）
	edges := []EdgeSpec{{TaskID: "t1", DependsOnID: "t2"}}
	result := db.MutateDependencyGraph(ctx, "alpha", nil, edges)
	if result.Ok {
		t.Error("CLAIMED 源任务添加依赖应被拒绝")
	}
}

func TestMutateDependencyGraph_PLAN_APPROVED源拒绝(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "m1", "alpha", "M1", "", "active", "teammate", "", "idle", "build_mode", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPlanApproved, Assignee: "m1", Title: "计划已批准"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "上游"})
	edges := []EdgeSpec{{TaskID: "t1", DependsOnID: "t2"}}
	result := db.MutateDependencyGraph(ctx, "alpha", nil, edges)
	if result.Ok {
		t.Error("PLAN_APPROVED 源任务添加依赖应被拒绝")
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test -v -run "TestMutateDependencyGraph_(CLAIMED|PLAN_APPROVED)" ./internal/agent_teams/tools/database/...`

Expected: PASS

- [ ] **Step 4: 运行完整 database 包测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test -v ./internal/agent_teams/tools/database/...`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/tools/database/memory_impl.go internal/agent_teams/tools/database/memory_impl_test.go
git commit -m "fix(9.65a-2): loadEndpointsAndValidate 拒绝范围扩展为 4 种状态对齐 Python TASK_DEPENDENCY_REJECT_STATUSES"
```

---

## Task 2: 差异 2 — GraphMutationResult.RefreshedTasks 类型 + Add 原子路径

**Files:**
- Modify: `internal/agent_teams/tools/database/models.go:158-165`
- Modify: `internal/agent_teams/tools/database/memory_impl.go` (refreshTaskStatuses 返回值 + MutateDependencyGraph 返回值)
- Modify: `internal/agent_teams/tools/database/sql_task_dao.go` (SQL MutateDependencyGraph 返回值)
- Modify: `internal/agent_teams/tools/task_manager.go` (Add 方法)

- [ ] **Step 1: 修改 models.go — RefreshedTasks 类型**

在 `internal/agent_teams/tools/database/models.go` 中替换 L158-165：

```go
// GraphMutationResult 图变更操作结果。
// 对齐 Python: GraphMutationResult (openjiuwen/agent_teams/tools/database/task_dao.py)
type GraphMutationResult struct {
	// Ok 操作是否成功
	Ok bool
	// Reason 失败原因（Ok=false 时）
	Reason string
	// RefreshedTasks 状态刷新产出的任务列表（对齐 Python: refreshed_tasks: list[TeamTaskBase]）
	RefreshedTasks []*TeamTaskBase
}
```

- [ ] **Step 2: 修改 InMemory — refreshTaskStatuses 返回 []*TeamTaskBase**

在 `internal/agent_teams/tools/database/memory_impl.go` 中修改 `refreshTaskStatuses`（L1140-1169）：

```go
// refreshTaskStatuses 刷新团队内所有任务的 PENDING↔BLOCKED 状态。
// 对齐 Python: _refresh_status_in_session()
// PENDING + 有未解决依赖 → BLOCKED
// BLOCKED + 无未解决依赖 → PENDING
func (db *InMemoryTeamDatabase) refreshTaskStatuses(teamName string) []*TeamTaskBase {
	var refreshed []*TeamTaskBase
	for _, task := range db.tasks {
		if task.TeamName != teamName {
			continue
		}
		unresolvedCount := 0
		for _, dep := range db.deps {
			if dep.TaskID == task.TaskID && dep.TeamName == teamName && !dep.Resolved {
				unresolvedCount++
			}
		}

		oldStatus := task.Status
		if task.Status == fsm.TaskStatusPending && unresolvedCount > 0 {
			task.Status = fsm.TaskStatusBlocked
		} else if task.Status == fsm.TaskStatusBlocked && unresolvedCount == 0 {
			task.Status = fsm.TaskStatusPending
		}
		if oldStatus != task.Status {
			task.UpdatedAt = GetCurrentTime()
			refreshed = append(refreshed, task)
		}
	}
	return refreshed
}
```

- [ ] **Step 3: 修改 mutationContext.refreshedIDs 字段类型 + MutateDependencyGraph 返回值**

在 `internal/agent_teams/tools/database/memory_impl.go` 中修改 `mutationContext` 结构体字段（约 L48）：

```go
// 替换
refreshedIDs  []string                 // 步骤5产出：状态刷新的 task IDs
// 为
refreshedTasks []*TeamTaskBase         // 步骤5产出：状态刷新的任务列表
```

修改 `refreshStatus` 方法：

```go
// refreshStatus 步骤5：刷新 PENDING↔BLOCKED 状态。
// 对齐 Python: _refresh_status_in_session()
func (mc *mutationContext) refreshStatus() {
	mc.refreshedTasks = mc.db.refreshTaskStatuses(mc.teamName)
}
```

修改 `MutateDependencyGraph` 返回值（L707）：

```go
// 替换
return GraphMutationResult{Ok: true, RefreshedTasks: mc.refreshedIDs}
// 为
return GraphMutationResult{Ok: true, RefreshedTasks: mc.refreshedTasks}
```

- [ ] **Step 4: 修改 SQL MutateDependencyGraph 返回值**

在 `internal/agent_teams/tools/database/sql_task_dao.go` 中，找到 `MutateDependencyGraphInTx` 返回 `GraphMutationResult` 的地方，将 `RefreshedTasks` 从 `[]string` 改为 `[]*TeamTaskBase`。需要先查出 refreshed 的任务对象列表。

搜索 `RefreshedTasks` 在 `sql_task_dao.go` 中的使用，同步修改类型。SQL 实现的 `_refresh_status_in_session_in_tx` 也需返回 `[]*TeamTaskBase`。

- [ ] **Step 5: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/...`

Expected: 编译成功（如有编译错误，修复 RefreshedTasks 类型不匹配的调用方）

- [ ] **Step 6: 修改 task_manager.go Add 方法 — 原子路径**

在 `internal/agent_teams/tools/task_manager.go` 中替换 `Add` 方法（L137-170）：

```go
// Add 创建单条任务。对齐 Python: TeamTaskManager.add()
// 支持可选参数 WithTaskID、WithDependencies，对齐 Python: add(task_id, dependencies)
func (tm *TeamTaskManager) Add(ctx context.Context, title, content string, opts ...TaskAddOption) (*database.TeamTaskBase, error) {
	cfg := &taskAddConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	taskID := cfg.taskID
	if taskID == "" {
		taskID = generateTaskID(tm.teamName)
	}

	if len(cfg.dependencies) > 0 {
		// 有依赖：走 MutateDependencyGraph 原子路径（对齐 Python: mutate_dependency_graph）
		newTaskSpec := database.NewTaskSpec{
			TaskID:        taskID,
			Title:         title,
			Content:       content,
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
		if task == nil {
			task, _ = tm.db.Task().GetTask(ctx, taskID)
		}
		if task != nil {
			tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{
				BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
				TaskID:           task.TaskID,
				Status:           task.Status,
			})
		}
		return task, nil
	}

	// 无依赖：走 CreateTask
	task := &database.TeamTaskBase{
		TaskID:   taskID,
		TeamName: tm.teamName,
		Title:    title,
		Content:  content,
		Status:   fsm.TaskStatusPending,
	}
	ok, err := tm.db.Task().CreateTask(ctx, task)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("创建任务失败: task_id 冲突 %s", taskID)
	}
	tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           task.TaskID,
		Status:           task.Status,
	})
	return task, nil
}
```

- [ ] **Step 7: 添加辅助函数 findRefreshedTask + generateTaskID**

在 `internal/agent_teams/tools/task_manager.go` 非导出函数区域添加：

```go
// findRefreshedTask 从 refreshedTasks 列表中按 taskID 查找任务。
// 对齐 Python: for refreshed in mutation.refreshed_tasks: if refreshed.task_id == task_id
func findRefreshedTask(refreshed []*database.TeamTaskBase, taskID string) *database.TeamTaskBase {
	for _, t := range refreshed {
		if t.TaskID == taskID {
			return t
		}
	}
	return nil
}

// generateTaskID 生成任务 ID，对齐 Python: uuid.uuid4() 的等价逻辑。
func generateTaskID(teamName string, suffixes ...string) string {
	base := fmt.Sprintf("task_%s_%d_%d", teamName, time.Now().UnixMilli(), time.Now().UnixNano()%1000)
	if len(suffixes) > 0 {
		base += "_" + strings.Join(suffixes, "_")
	}
	return base
}
```

替换 `Add` / `AddAsTopPriority` 中现有的内联 taskID 生成逻辑为 `generateTaskID` 调用。

- [ ] **Step 8: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/...`

Expected: 编译成功

- [ ] **Step 9: 运行 tools 包测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test -v ./internal/agent_teams/tools/...`

Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/agent_teams/tools/database/models.go internal/agent_teams/tools/database/memory_impl.go internal/agent_teams/tools/database/sql_task_dao.go internal/agent_teams/tools/task_manager.go
git commit -m "fix(9.65a-2): Add 有依赖时走 MutateDependencyGraph 原子路径 + RefreshedTasks 改为 []*TeamTaskBase"
```

---

## Task 3: 差异 3 — AddWithPriority 双向依赖 + 命名对齐 + AddAsTopPriority 修复

**Files:**
- Modify: `internal/agent_teams/tools/database/database.go:119` (TaskDao 接口)
- Modify: `internal/agent_teams/tools/database/memory_impl.go:710-731` (InMemory 实现)
- Modify: `internal/agent_teams/tools/database/sql_task_dao.go` (SQL 实现)
- Modify: `internal/agent_teams/tools/task_manager.go` (AddWithPriority + AddAsTopPriority)

- [ ] **Step 1: 修改 TaskDao 接口签名**

在 `internal/agent_teams/tools/database/database.go` 中替换 L117-119：

```go
// AddTaskWithBidirectionalDependencies 带双向依赖创建任务。委托 MutateDependencyGraph。
// 对齐 Python: add_task_with_bidirectional_dependencies(task_id, team_name, title, content, status, *, dependencies, dependent_task_ids)
AddTaskWithBidirectionalDependencies(ctx context.Context, teamName string, task *TeamTaskBase, dependencies []string, dependentTaskIDs []string) GraphMutationResult
```

- [ ] **Step 2: 修改 InMemory AddTaskWithBidirectionalDependencies 实现**

在 `internal/agent_teams/tools/database/memory_impl.go` 中替换 L710-731：

```go
// AddTaskWithBidirectionalDependencies 带双向依赖创建任务。
// 对齐 Python: add_task_with_bidirectional_dependencies()
func (db *InMemoryTeamDatabase) AddTaskWithBidirectionalDependencies(_ context.Context, teamName string, task *TeamTaskBase, dependencies []string, dependentTaskIDs []string) GraphMutationResult {
	// 构建 NewTaskSpec
	newTaskSpec := NewTaskSpec{
		TaskID:        task.TaskID,
		Title:         task.Title,
		Content:       task.Content,
		InitialStatus: task.Status,
	}
	if newTaskSpec.InitialStatus == "" {
		newTaskSpec.InitialStatus = fsm.TaskStatusPending
	}

	// 构建双向 EdgeSpec
	edges := make([]EdgeSpec, 0, len(dependencies)+len(dependentTaskIDs))
	// dependencies：新任务依赖上游 → 边方向 (new_task, upstream)
	for _, upstreamID := range dependencies {
		edges = append(edges, EdgeSpec{TaskID: task.TaskID, DependsOnID: upstreamID})
	}
	// dependentTaskIDs：下游依赖新任务 → 边方向 (existing_task, new_task)
	for _, downstreamID := range dependentTaskIDs {
		edges = append(edges, EdgeSpec{TaskID: downstreamID, DependsOnID: task.TaskID})
	}

	return db.MutateDependencyGraph(context.Background(), teamName, []NewTaskSpec{newTaskSpec}, edges)
}
```

- [ ] **Step 3: 修改 SQL AddTaskWithBidirectionalDependencies 实现**

在 `internal/agent_teams/tools/database/sql_task_dao.go` 中，找到 `AddTaskWithBidirectionalDependencies` 方法，将签名从 `(task *TeamTaskBase, dependsOnIDs []string)` 改为 `(task *TeamTaskBase, dependencies []string, dependentTaskIDs []string)`，并在构建 `addEdges` 时增加 `dependentTaskIDs` 的边构建逻辑（与 InMemory 实现相同模式）。

- [ ] **Step 4: 修改 TeamTaskManager.AddWithPriority**

在 `internal/agent_teams/tools/task_manager.go` 中：

1. 在结构体区域添加 Option 类型：

```go
// TaskAddWithPriorityOption AddWithPriority 的可选参数。
type TaskAddWithPriorityOption func(*taskAddWithPriorityConfig)

// taskAddWithPriorityConfig AddWithPriority 的可选配置
type taskAddWithPriorityConfig struct {
	taskID           string
	dependencies     []string    // 对齐 Python: dependencies
	dependentTaskIDs []string    // 对齐 Python: dependent_task_ids
}
```

2. 在导出函数区域添加 Option 构造函数：

```go
// WithPriorityTaskID 设置自定义任务 ID。对齐 Python: add_with_priority(task_id=...)
func WithPriorityTaskID(taskID string) TaskAddWithPriorityOption {
	return func(c *taskAddWithPriorityConfig) {
		c.taskID = taskID
	}
}

// WithPriorityDependencies 设置新任务依赖的上游任务。对齐 Python: add_with_priority(dependencies=...)
func WithPriorityDependencies(deps []string) TaskAddWithPriorityOption {
	return func(c *taskAddWithPriorityConfig) {
		c.dependencies = deps
	}
}

// WithPriorityDependentTaskIDs 设置依赖新任务的下游任务。对齐 Python: add_with_priority(dependent_task_ids=...)
func WithPriorityDependentTaskIDs(ids []string) TaskAddWithPriorityOption {
	return func(c *taskAddWithPriorityConfig) {
		c.dependentTaskIDs = ids
	}
}
```

3. 替换 `AddWithPriority` 方法（L455-474）：

```go
// AddWithPriority 带双向依赖创建任务。对齐 Python: TeamTaskManager.add_with_priority()
// 支持双向依赖：dependencies（新任务依赖谁）+ dependentTaskIDs（谁依赖新任务）。
// 有 dependencies 时初始状态为 BLOCKED，否则为 PENDING。
func (tm *TeamTaskManager) AddWithPriority(ctx context.Context, title, content string, opts ...TaskAddWithPriorityOption) (*database.TeamTaskBase, error) {
	cfg := &taskAddWithPriorityConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	taskID := cfg.taskID
	if taskID == "" {
		taskID = generateTaskID(tm.teamName)
	}

	// 初始状态逻辑（对齐 Python: 有 dependencies → BLOCKED，否则 PENDING）
	initialStatus := fsm.TaskStatusPending
	if len(cfg.dependencies) > 0 {
		initialStatus = fsm.TaskStatusBlocked
	}

	task := &database.TeamTaskBase{
		TaskID:   taskID,
		TeamName: tm.teamName,
		Title:    title,
		Content:  content,
		Status:   initialStatus,
	}
	result := tm.db.Task().AddTaskWithBidirectionalDependencies(ctx, tm.teamName, task, cfg.dependencies, cfg.dependentTaskIDs)
	if !result.Ok {
		return nil, fmt.Errorf("带依赖创建任务失败: %s", result.Reason)
	}

	// 从 RefreshedTasks 中取最终状态
	created := findRefreshedTask(result.RefreshedTasks, taskID)
	if created == nil {
		created, _ = tm.db.Task().GetTask(ctx, taskID)
	}
	if created != nil {
		tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{
			BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
			TaskID:           created.TaskID,
			Status:           created.Status,
		})
	}
	return created, nil
}
```

- [ ] **Step 5: 修改 AddAsTopPriority — 走 AddTaskWithBidirectionalDependencies 一步原子**

在 `internal/agent_teams/tools/task_manager.go` 中替换 `AddAsTopPriority` 方法（L477-517）：

```go
// AddAsTopPriority 最高优先级插入。对齐 Python: TeamTaskManager.add_as_top_priority()
// 让所有 PENDING 任务依赖新任务，保证新任务最先执行。
func (tm *TeamTaskManager) AddAsTopPriority(ctx context.Context, title, content string, opts ...TaskAddOption) (*database.TeamTaskBase, error) {
	cfg := &taskAddConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	taskID := cfg.taskID
	if taskID == "" {
		taskID = generateTaskID(tm.teamName, "priority")
	}

	// 查询所有 PENDING 任务作为 dependentTaskIDs
	pendingTasks, _ := tm.db.Task().GetTeamTasks(ctx, tm.teamName, fsm.TaskStatusPending)
	var dependentTaskIDs []string
	for _, t := range pendingTasks {
		if t.TaskID != taskID {
			dependentTaskIDs = append(dependentTaskIDs, t.TaskID)
		}
	}

	task := &database.TeamTaskBase{
		TaskID:   taskID,
		TeamName: tm.teamName,
		Title:    title,
		Content:  content,
		Status:   fsm.TaskStatusPending,
	}
	// 一步原子：通过 AddTaskWithBidirectionalDependencies（对齐 Python）
	result := tm.db.Task().AddTaskWithBidirectionalDependencies(ctx, tm.teamName, task, nil, dependentTaskIDs)
	if !result.Ok {
		return nil, fmt.Errorf("创建最高优先级任务失败: %s", result.Reason)
	}

	created := findRefreshedTask(result.RefreshedTasks, taskID)
	if created == nil {
		created = task
	}
	tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           created.TaskID,
		Status:           created.Status,
	})
	return created, nil
}
```

- [ ] **Step 6: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/...`

Expected: 编译成功（如有 AddWithPriority 调用方不匹配，同步修改）

- [ ] **Step 7: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test -v ./internal/agent_teams/tools/...`

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/agent_teams/tools/database/database.go internal/agent_teams/tools/database/memory_impl.go internal/agent_teams/tools/database/sql_task_dao.go internal/agent_teams/tools/task_manager.go
git commit -m "fix(9.65a-2): AddWithPriority 双向依赖 + 命名对齐 + AddAsTopPriority 原子路径"
```

---

## Task 4: 差异 4 — CancelAllTasks Unblocked 返回类型

**Files:**
- Modify: `internal/agent_teams/tools/database/models.go:167-174`
- Modify: `internal/agent_teams/tools/database/memory_impl.go` (CancelAllTasks)
- Modify: `internal/agent_teams/tools/database/sql_task_dao.go` (CancelAllTasks)
- Modify: `internal/agent_teams/tools/task_manager.go` (publishUnblockedEvents)

- [ ] **Step 1: 修改 models.go — Unblocked 类型**

在 `internal/agent_teams/tools/database/models.go` 中替换 L167-174：

```go
// CancelAllTasksResult 批量取消任务的返回结果。
// 对齐 Python: cancel_all_tasks() → {"cancelled_tasks": [...], "unblocked_tasks": [...]}
type CancelAllTasksResult struct {
	// Cancelled 被取消的任务列表
	Cancelled []*TeamTaskBase
	// Unblocked 被解除阻塞的任务列表（从 BLOCKED→PENDING 的任务，对齐 Python: unblocked_tasks）
	Unblocked []*TeamTaskBase
}
```

- [ ] **Step 2: 修改 InMemory CancelAllTasks 实现**

在 `internal/agent_teams/tools/database/memory_impl.go` 中修改 `CancelAllTasks` 方法（约 L615-665），将 `unblockedSet` 从 `map[string]bool` 改为 `map[string]*TeamTaskBase`：

```go
func (db *InMemoryTeamDatabase) CancelAllTasks(_ context.Context, teamName string, skipAssignees []string) (*CancelAllTasksResult, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	skipSet := make(map[string]bool, len(skipAssignees))
	for _, a := range skipAssignees {
		skipSet[a] = true
	}

	result := &CancelAllTasksResult{}
	tasks, _ := db.getTeamTasksLocked(teamName, "")
	unblockedByID := make(map[string]*TeamTaskBase)

	for _, task := range tasks {
		if task.Status == fsm.TaskStatusCompleted || task.Status == fsm.TaskStatusCancelled {
			continue
		}
		if skipSet[task.Assignee] {
			continue
		}
		refreshed, _ := db.terminateTaskInSession(task.TaskID, fsm.TaskStatusCancelled)
		result.Cancelled = append(result.Cancelled, task)
		// 收集被解除阻塞的任务对象
		for _, id := range refreshed {
			if unblockedTask, exists := db.tasks[id]; exists {
				if _, already := unblockedByID[id]; !already {
					unblockedByID[id] = unblockedTask
				}
			}
		}
	}

	// 对齐 Python: 排除自身被取消的任务
	cancelledSet := make(map[string]bool, len(result.Cancelled))
	for _, t := range result.Cancelled {
		cancelledSet[t.TaskID] = true
	}
	for id, t := range unblockedByID {
		if !cancelledSet[id] {
			result.Unblocked = append(result.Unblocked, t)
		}
	}

	return result, nil
}
```

- [ ] **Step 3: 修改 SQL CancelAllTasks 实现**

在 `internal/agent_teams/tools/database/sql_task_dao.go` 中修改 `CancelAllTasks`（L771-834），将 `result.Unblocked` 从 `append(result.Unblocked, id)` 改为 `append(result.Unblocked, unblockedByID[id])`（已有 `unblockedByID` map，直接用对象而非 ID）。

- [ ] **Step 4: 修改 task_manager.go publishUnblockedEvents 签名**

在 `internal/agent_teams/tools/task_manager.go` 中修改 `publishUnblockedEvents`：

```go
// publishUnblockedEvents 逐条发布 TaskUnblockedEvent。
// 对齐 Python: TeamTaskManager._publish_unblocked_events()
func (tm *TeamTaskManager) publishUnblockedEvents(ctx context.Context, unblockedTasks []*database.TeamTaskBase) {
	for _, t := range unblockedTasks {
		tm.publishTaskEvent(ctx, schema.TaskUnblockedEvent{
			BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
			TaskID:           t.TaskID,
		})
	}
}
```

同步修改 `Complete`、`Cancel` 方法中调用 `publishUnblockedEvents` 的地方——`CompleteTask` 和 `CancelTask` 返回 `[]string`（unblocked ID 列表），需要转换为 `[]*TeamTaskBase`：

```go
// Complete 方法中
refreshed, err := tm.db.Task().CompleteTask(ctx, taskID)
// refreshed 是 []string，需要转换
var unblockedTasks []*database.TeamTaskBase
for _, id := range refreshed {
	if t, _ := tm.db.Task().GetTask(ctx, id); t != nil {
		unblockedTasks = append(unblockedTasks, t)
	}
}
tm.publishUnblockedEvents(ctx, unblockedTasks)

// Cancel 方法同理
```

- [ ] **Step 5: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/...`

Expected: 编译成功

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test -v ./internal/agent_teams/tools/...`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/agent_teams/tools/database/models.go internal/agent_teams/tools/database/memory_impl.go internal/agent_teams/tools/database/sql_task_dao.go internal/agent_teams/tools/task_manager.go
git commit -m "fix(9.65a-2): CancelAllTasksResult.Unblocked 改为 []*TeamTaskBase 对齐 Python"
```

---

## Task 5: 差异 5 — AddBatch 容错 + TaskCreateSpec 扩展

**Files:**
- Modify: `internal/agent_teams/tools/task_manager.go` (TaskCreateSpec + AddBatch)

- [ ] **Step 1: 扩展 TaskCreateSpec**

在 `internal/agent_teams/tools/task_manager.go` 结构体区域替换 TaskCreateSpec：

```go
// TaskCreateSpec 批量创建任务的输入规范。
// 对齐 Python: add_batch(tasks: List[dict]) 中每个 dict 的字段。
type TaskCreateSpec struct {
	// TaskID 可选自定义任务 ID，空则自动生成。对齐 Python: task_spec.get("task_id")
	TaskID string
	// Title 任务标题
	Title string
	// Content 任务内容
	Content string
	// Dependencies 可选依赖列表。对齐 Python: task_spec.get("dependencies")
	Dependencies []string
}
```

- [ ] **Step 2: 修改 AddBatch 容错行为**

在 `internal/agent_teams/tools/task_manager.go` 中替换 `AddBatch` 方法（L173-183）：

```go
// AddBatch 批量创建任务。对齐 Python: TeamTaskManager.add_batch()
// 跳过无效规格（缺 title/content）和创建失败的任务，返回成功创建的列表。
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
			logger.Warn(logComponent).Err(err).Str("title", spec.Title).Msg("批量创建跳过失败任务")
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/...`

Expected: 编译成功

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test -v ./internal/agent_teams/tools/...`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/tools/task_manager.go
git commit -m "fix(9.65a-2): AddBatch 容错跳过无效/失败 + TaskCreateSpec 扩展 TaskID/Dependencies 字段"
```

---

## Task 6: 全量验证 + doc.go 更新

**Files:**
- Modify: `internal/agent_teams/tools/database/doc.go`
- Modify: `internal/agent_teams/tools/doc.go`

- [ ] **Step 1: 运行 database 包完整测试 + 覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test -cover -v ./internal/agent_teams/tools/database/...`

Expected: 覆盖率 ≥ 85%

- [ ] **Step 2: 运行 tools 包完整测试 + 覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test -cover -v ./internal/agent_teams/tools/...`

Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 运行全量编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./...`

Expected: 编译成功

- [ ] **Step 4: 更新 doc.go 文件目录**

在 `internal/agent_teams/tools/database/doc.go` 和 `internal/agent_teams/tools/doc.go` 中，确认文件目录条目与实际文件一致。

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/tools/database/doc.go internal/agent_teams/tools/doc.go
git commit -m "docs(9.65a-2): 更新 doc.go 文件目录"
```

- [ ] **Step 6: 更新 IMPLEMENTATION_PLAN.md 状态**

在 `IMPLEMENTATION_PLAN.md` 的 9.65a-2 行，确认状态仍为 ✅（本章节已在之前标记完成，本次是差异修复）。
