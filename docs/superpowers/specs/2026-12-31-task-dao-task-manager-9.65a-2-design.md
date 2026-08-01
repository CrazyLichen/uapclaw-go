# 9.65a-2 TaskDao + TaskManager 设计文档

## 1. 概述

本章节实现 TeamBackend 子系统中的任务管理数据层（TaskDao）和业务层（TeamTaskManager），对齐 Python 的 `task_dao.py` + `task_manager.py`。

**拆分为 A+B 两阶段**：
- **A 阶段**：TaskDao 纯数据层 — 接口定义 + InMemory 实现 + 依赖图变更管线 + 原子终止传播
- **B 阶段**：TeamTaskManager 业务层 — 全部方法实现 + PLAN_MODE 审批 + 事件发布占位

## 2. 设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 实现范围 | 拆分 A+B 两阶段 | A 不依赖 Messager 可独立完成；B 业务层需事件发布能力 |
| 方法范围 | 对齐 Python 全部方法 | 原则：完整对齐，不挑拣 |
| 事件发布依赖 | B 阶段用 `any` 占位 + 注释标注函数签名 | 不定义中间接口，等 9.65 回填 |
| PLAN_MODE | 完整实现，Messager 用 `any` 占位 | plan 文件存储 + index.json + FSM 转换完整实现；跨成员消息通知用注释标注 |
| 图变更管线模式 | 闭包模式：mutationContext struct + failReason flag | Go 风格自然，步骤间数据共享通过 struct，替代 Python _MutationFailure 异常 |
| 原子终止传播 | 一次 Lock 内完成所有操作 | 对齐 Python 事务语义，与 InMemoryTeamDatabase 的 Mutex 使用模式一致 |
| 章节顺序 | 不调整 | A 不依赖任何未实现章节；B 的 Messager 占位等 9.65 回填 |

## 3. A 阶段：TaskDao 纯数据层

### 3.1 文件变更清单

| 文件 | 变更内容 |
|------|---------|
| `database/models.go` | 新增 `TeamTaskBase` + `TeamTaskDependencyBase` + `NewTaskSpec` + `GraphMutationResult` + `EdgeSpec` 数据模型 |
| `database/task_dao.go` | 空接口 → 具体 TaskDao 方法签名（18 个方法） |
| `database/database.go` | `TaskDao interface{}` → 引用新定义的 TaskDao 接口 |
| `database/memory_impl.go` | InMemoryTeamDatabase 新增 `tasks` + `deps` map，实现 TaskDao 全部方法 |
| `database/memory_impl_test.go` | 新增 TaskDao 测试用例 |
| `database/doc.go` | 更新文件目录 |
| `database/fsm.go` | 确认已有的 `fsm.IsValidTaskTransition` 覆盖所有 TaskDao 需要的转换 |

### 3.2 数据模型

```go
// TeamTaskBase 任务行模型（对齐 Python TeamTaskBase）
type TeamTaskBase struct {
    TaskID    string `json:"task_id"`
    TeamName  string `json:"team_name"`
    Title     string `json:"title"`
    Content   string `json:"content"`
    Status    string `json:"status"`
    Assignee  string `json:"assignee,omitempty"`
    UpdatedAt int64  `json:"updated_at,omitempty"`
}

// TeamTaskDependencyBase 依赖边模型（对齐 Python TeamTaskDependencyBase）
type TeamTaskDependencyBase struct {
    TaskID        string `json:"task_id"`
    DependsOnID   string `json:"depends_on_task_id"`
    TeamName      string `json:"team_name"`
    Resolved      bool   `json:"resolved"`
}

// NewTaskSpec 图变更管线中待插入的新任务规范（对齐 Python NewTaskSpec）
type NewTaskSpec struct {
    TaskID        string
    Title         string
    Content       string
    InitialStatus string
}

// EdgeSpec 依赖边规范（管线输入）
// 方向语义：TaskID 依赖 DependsOnTaskID（TaskID 被 DependsOnTaskID 阻塞）
// 对齐 Python 的 (task_id, depends_on_task_id) 边方向
type EdgeSpec struct {
    TaskID        string  // 下游任务（被阻塞的任务）
    DependsOnID   string  // 上游任务（阻塞源，即 depends_on）
}

// GraphMutationResult 图变更操作结果（对齐 Python GraphMutationResult）
type GraphMutationResult struct {
    Ok             bool
    Reason         string
    RefreshedTasks []string
}
```

### 3.3 TaskDao 接口方法（18 个，对齐 Python task_dao.py）

```go
type TaskDao interface {
    // 创建单条任务，检测 task_id 冲突
    CreateTask(ctx context.Context, task *TeamTaskBase) (bool, error)
    // 按 ID 查询任务
    GetTask(ctx context.Context, taskID string) (*TeamTaskBase, error)
    // 查询团队全部任务（可按 status 过滤，空字符串=全部）
    GetTeamTasks(ctx context.Context, teamName, status string) ([]*TeamTaskBase, error)
    // 查询成员的任务（可按 status 过滤）
    GetTasksByAssignee(ctx context.Context, teamName, assignee, status string) ([]*TeamTaskBase, error)
    // 认领任务：设置 assignee + PENDING→CLAIMED FSM 校验（对齐 Python claim_task）
    ClaimTask(ctx context.Context, taskID, assignee string) (bool, error)
    // 重置任务：CLAIMED→PENDING，清除 assignee
    ResetTask(ctx context.Context, taskID string) (bool, error)
    // 计划审批：CLAIMED→PLAN_APPROVED FSM 校验
    ApprovePlanTask(ctx context.Context, taskID string) (bool, error)
    // 更新任务状态，完成时自动解除下游依赖并刷新 BLOCKED→PENDING
    // 返回刷新的 task ID 列表
    UpdateTaskStatus(ctx context.Context, taskID, newStatus string) ([]string, error)
    // 更新标题/内容，CLAIMED/PLAN_APPROVED 状态下禁止编辑
    UpdateTask(ctx context.Context, taskID, title, content string) (bool, error)
    // 原子图变更：5 步管线（对齐 Python mutate_dependency_graph）
    MutateDependencyGraph(ctx context.Context, teamName string, newTasks []NewTaskSpec, addEdges []EdgeSpec) GraphMutationResult
    // 带双向依赖创建任务，委托 mutate_dependency_graph
    AddTaskWithBidirectionalDependencies(ctx context.Context, teamName string, task *TeamTaskBase, dependsOnIDs []string) GraphMutationResult
    // 查询任务依赖
    GetTaskDependencies(ctx context.Context, taskID string) ([]*TeamTaskDependencyBase, error)
    // 未解决依赖计数
    GetUnresolvedDependenciesCount(ctx context.Context, taskID string) (int, error)
    // 查询下游依赖任务
    GetTasksDependingOn(ctx context.Context, taskID string) ([]*TeamTaskBase, error)
    // 删除任务
    DeleteTask(ctx context.Context, taskID string) error
    // 取消任务（原子终止传播），返回 unblocked task IDs
    CancelTask(ctx context.Context, taskID string) ([]string, error)
    // 批量取消（原子终止传播），支持 skipAssignees 过滤
    CancelAllTasks(ctx context.Context, teamName string, skipAssignees []string) ([]*TeamTaskBase, error)
    // 完成任务（原子终止传播），返回 unblocked task IDs
    CompleteTask(ctx context.Context, taskID string) ([]string, error)
    // 一致性修复：扫描 BLOCKED 任务并刷新状态
    VerifyAndFixTaskConsistency(ctx context.Context, teamName string) ([]string, error)
}
```

### 3.4 mutationContext 闭包管线

替代 Python 的 `_MutationFailure` 异常信号，使用 struct + failReason flag 模式：

```go
// mutationContext 依赖图变更管线共享上下文
type mutationContext struct {
    db             *InMemoryTeamDatabase
    teamName       string
    newTasks       []NewTaskSpec
    addEdges       []EdgeSpec

    // 步骤间共享数据（闭包操作）
    stagedTasks    map[string]*TeamTaskBase   // 步骤1产出：已插入的新任务
    endpointTasks  map[string]*TeamTaskBase   // 步骤2产出：边端点对应的任务
    newEdgeRows    []TeamTaskDependencyBase   // 步骤4产出：待插入的依赖边行
    refreshedTasks []string                   // 步骤5产出：状态刷新的 task IDs

    // 失败标记（替代 Python _MutationFailure）
    failReason     string
}
```

**5 步管线执行顺序**（对齐 Python `mutate_dependency_graph`）：

1. `stageNewTasks` — 插入新任务行，检测 task_id 重复 → 设 `failReason` 返回
2. `loadEndpointsAndValidate` — 加载边端点，拒绝缺失/终态/已执行源 → 设 `failReason`
3. `checkCycleAndComputeNewEdges` — 构建后变更邻接表，检测环路 → 设 `failReason`
4. `applyNewEdges` — 插入依赖边行，终态依赖初始 resolved=True
5. `refreshStatus` — 刷新 PENDING↔BLOCKED 状态

**主函数结构**：

```go
func (db *InMemoryTeamDatabase) MutateDependencyGraph(...) GraphMutationResult {
    mc := &mutationContext{db: db, teamName: teamName, newTasks: newTasks, addEdges: addEdges}
    mc.stageNewTasks()
    if mc.failReason != "" {
        return GraphMutationResult{Ok: false, Reason: mc.failReason}
    }
    mc.loadEndpointsAndValidate()
    if mc.failReason != "" {
        // 回滚：删除步骤1插入的新任务
        mc.rollbackStagedTasks()
        return GraphMutationResult{Ok: false, Reason: mc.failReason}
    }
    // ... 步骤3、4、5同理，失败时回滚
}
```

**回滚设计**：InMemory 模式下回滚为直接从 map 删除已插入的数据（步骤1的 stagedTasks、步骤4的 newEdgeRows），无需 SQL ROLLBACK。

### 3.5 InMemoryTeamDatabase 新增字段

```go
type InMemoryTeamDatabase struct {
    teams      map[string]*Team                // 已有
    members    map[string]*TeamMember          // 已有
    tasks      map[string]*TeamTaskBase        // 新增：key=taskID
    deps       map[string]*TeamTaskDependencyBase // 新增：key=taskID+"\x00"+dependsOnID
    initialized bool
    mu         sync.Mutex
}
```

**复合主键编码**：沿用 `\x00` 分隔符拼接模式（与 memberKey 一致）。

### 3.6 原子终止传播

`CompleteTask` / `CancelTask` / `CancelAllTasks` 在一次 Lock 内完成所有操作：

```go
func (db *InMemoryTeamDatabase) CompleteTask(ctx, taskID) ([]string, error) {
    db.mu.Lock()
    defer db.mu.Unlock()

    // 1. 将任务设为 COMPLETED
    // 2. 批量标记下游依赖为 resolved=True
    // 3. 对所有下游任务执行 _refreshStatusInSession（PENDING↔BLOCKED 翻转）
    // 4. 收集刷新的 task IDs 返回
}
```

### 3.7 状态刷新逻辑

对齐 Python `_refresh_status_in_session`：

- PENDING + 有未解决依赖 → BLOCKED
- BLOCKED + 无未解决依赖 → PENDING
- 其他状态不变

### 3.8 TaskStatus FSM（已有，确认覆盖）

`fsm/transitions.go` 已定义：

```
TaskStatusPending → TaskStatusClaimed/TaskStatusBlocked/TaskStatusCancelled
TaskStatusClaimed → TaskStatusPlanApproved/TaskStatusCompleted/TaskStatusCancelled/TaskStatusBlocked/TaskStatusPending
TaskStatusPlanApproved → TaskStatusCompleted/TaskStatusPending/TaskStatusCancelled
TaskStatusBlocked → TaskStatusPending/TaskStatusCancelled
TaskStatusCompleted → [] (终态)
TaskStatusCancelled → [] (终态)
```

覆盖 TaskDao 所需全部转换 ✅

## 4. B 阶段：TeamTaskManager 业务层

### 4.1 文件变更清单

| 文件 | 变更内容 |
|------|---------|
| `tools/task_manager.go` | `any` 返回值 → 具体类型 + 全部方法实现 |
| `tools/task_manager_test.go` | 新增 TeamTaskManager 测试用例 |
| `tools/doc.go` | 更新文件目录 |
| `agent/infra.go` | `TaskManager any` → `tools.TeamTaskManager` 类型化 |
| `agent/agent_configurator.go` | 注入注释 → 实际调用 |
| `memory/manager.go` | 确认 `taskManager tools.TeamTaskManager` 已声明，验证使用 |
| `memory/manager_params.go` | 确认构造参数注入 |

### 4.2 TeamTaskManager 结构体

```go
type TeamTaskManager struct {
    db               TeamDatabase     // 数据库实例（内含 TaskDao）
    teamName         string           // 团队标识
    memberName       string           // 当前成员标识
    messager         any              // ⤵️ 9.65 回填：需实现 Publish(topic, event) error
    plansDir         string           // 计划文件存储目录
    teamPlanID       string           // 团队级计划标识
    leaderMemberName string           // Leader 成员名（用于通知计划审批）
}
```

**messager 占位注释**：

```go
// messager 需实现以下方法（⤵️ 9.65 回填）：
//   Publish(topic string, event interface{}) error
//   用于发布事件到 TeamTopic.TASK topic
```

### 4.3 TeamTaskManager 方法（对齐 Python task_manager.py）

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// Add 创建单条任务
func (tm *TeamTaskManager) Add(ctx context.Context, title, content string) (*TeamTaskBase, error)
// AddBatch 批量创建任务
func (tm *TeamTaskManager) AddBatch(ctx context.Context, specs []TaskCreateSpec) ([]*TeamTaskBase, error)
// AddWithPriority 带双向依赖创建任务
func (tm *TeamTaskManager) AddWithPriority(ctx context.Context, taskID, title, content string, dependsOnIDs []string, newTasksSpec []NewTaskSpec) (GraphMutationResult, error)
// AddAsTopPriority 最高优先级插入（阻塞所有 PENDING 任务）
func (tm *TeamTaskManager) AddAsTopPriority(ctx context.Context, title, content string) (*TeamTaskBase, error)
// Claim 成员自认领
func (tm *TeamTaskManager) Claim(ctx context.Context, taskID string) error
// Assign Leader 分配
func (tm *TeamTaskManager) Assign(ctx context.Context, taskID, assignee string) error
// Complete 完成任务（返回 unblocked task IDs）
func (tm *TeamTaskManager) Complete(ctx context.Context, taskID string) ([]string, error)
// Cancel 取消单条任务（返回 unblocked task IDs）
func (tm *TeamTaskManager) Cancel(ctx context.Context, taskID string) ([]string, error)
// CancelAllTasks 批量取消（支持 skipAssignees 过滤）
func (tm *TeamTaskManager) CancelAllTasks(ctx context.Context, skipAssignees []string) ([]*TeamTaskBase, error)
// Reset 重置任务（CLAIMED→PENDING）
func (tm *TeamTaskManager) Reset(ctx context.Context, taskID string) error
// UpdateTask 更新标题/内容
func (tm *TeamTaskManager) UpdateTask(ctx context.Context, taskID, title, content string) error
// ListTasks 列出团队任务（可按 status 过滤，空字符串=全部）
func (tm *TeamTaskManager) ListTasks(ctx context.Context, status string) ([]*TeamTaskBase, error)
// Get 按 ID 查任务
func (tm *TeamTaskManager) Get(ctx context.Context, taskID string) (*TeamTaskBase, error)
// GetTaskDetail 详细视图（含 blocked_by + blocks）
func (tm *TeamTaskManager) GetTaskDetail(ctx context.Context, taskID string) (*TaskDetail, error)
// ListTasksWithDeps 摘要视图（含 blocked_by）
func (tm *TeamTaskManager) ListTasksWithDeps(ctx context.Context) ([]*TaskSummary, error)
// GetClaimableTasks 可认领任务列表
func (tm *TeamTaskManager) GetClaimableTasks(ctx context.Context) ([]*TeamTaskBase, error)
// GetTasksByAssignee 按成员查任务（可按 status 过滤，空字符串=全部）
func (tm *TeamTaskManager) GetTasksByAssignee(ctx context.Context, memberName, status string) ([]*TeamTaskBase, error)
// AddDependencies 向已有任务添加依赖
func (tm *TeamTaskManager) AddDependencies(ctx context.Context, taskID string, dependsOnIDs []string) (GraphMutationResult, error)
// SubmitPlan PLAN_MODE 提交计划（⤵️ 9.65 回填 messager 通知）
func (tm *TeamTaskManager) SubmitPlan(ctx context.Context, taskID, planFilePath, toolCallID string) (*PlanRecord, error)
// ApprovePlan PLAN_MODE 审批/拒绝计划（⤵️ 9.65 回填 messager 通知）
func (tm *TeamTaskManager) ApprovePlan(ctx context.Context, planID string, approved bool, feedback string) error
```

补充辅助类型 `TaskCreateSpec`（对齐 Python add_batch 的输入）：

```go
// TaskCreateSpec 批量创建任务的输入规范
type TaskCreateSpec struct {
    Title   string
    Content string
}
```

### 4.4 PLAN_MODE 完整实现

#### SubmitPlan 流程（对齐 Python `submit_plan`）

1. 校验成员是否为 PLAN_MODE（通过 `db.Member().GetMember` 查 mode 字段）
2. 校验任务状态为 PENDING 或 CLAIMED 且 assignee 是当前成员
3. 如果 PENDING → 先 `ClaimTask` 使其变为 CLAIMED
4. 生成 plan_id（UUID）
5. 将 plan 文件拷贝到 `plansDir/teamPlanID/tasks/taskID/plans/planID.md`
6. 写入 index.json 记录 PlanRecord（taskID、planID、memberName、status=claimed、decision=pending 等）
7. 发布 TaskPlanRequestEvent — **messager `any` 占位，注释标注**：

```go
// ⤵️ 9.65 回填：需调用 messager.Publish(TeamTopic, TaskPlanRequestEvent)
// messager 需实现的方法签名：
//   Publish(topic string, event interface{}) error
//   SendPlanRequest(memberName, taskID, planID, planFilePath, toolCallID string) error
```

#### ApprovePlan 流程（对齐 Python `approve_plan`）

1. 校验 planID 在 index.json 中存在
2. 校验 task 状态为 CLAIMED 且有 assignee
3. 校验 planID 是 latestPlanID（防止审批过期计划）
4. 校验 plan 的 decision 状态为 pending（防止重复审批）
5. 校验 plan 文件物理存在
6. **审批通过**：`ApprovePlanTask`（CLAIMED→PLAN_APPROVED）+ 写 index.json（decision=approve）+ 发布 TaskPlanResponseEvent(approved=True)
7. **审批拒绝**：写 index.json（decision=reject, status=claimed）+ 发布 TaskPlanResponseEvent(approved=False, feedback) — 任务保持 CLAIMED

### 4.5 辅助数据结构

```go
// TaskDetail 任务详细视图（含阻塞关系）
type TaskDetail struct {
    Task      *TeamTaskBase
    BlockedBy []*TeamTaskBase  // 上游阻塞任务列表
    Blocks    []*TeamTaskBase  // 下游被阻塞任务列表
}

// TaskSummary 任务摘要视图（含阻塞信息）
type TaskSummary struct {
    TaskID    string
    Title     string
    Status    string
    Assignee  string
    BlockedBy []string  // 上游任务 ID 列表
}

// PlanRecord 计划记录（index.json 中的一条）
type PlanRecord struct {
    PlanID      string `json:"plan_id"`
    TaskID      string `json:"task_id"`
    MemberName  string `json:"member_name"`
    Status      string `json:"status"`       // claimed / plan_approved
    Decision    string `json:"decision"`     // pending / approve / reject
    Feedback    string `json:"feedback,omitempty"`
    CreatedAt   int64  `json:"created_at"`
}

// PlanIndex 计划索引（index.json 结构）
type PlanIndex struct {
    Tasks     map[string]*TaskPlanIndex `json:"tasks"`      // taskID → 每任务的 plan 索引
    TaskPlans map[string]*PlanRecord    `json:"task_plans"`  // planID → plan 记录详情
}

// TaskPlanIndex 每任务的计划索引
type TaskPlanIndex struct {
    PlanIDs      []string `json:"plan_ids"`
    LatestPlanID string   `json:"latest_plan_id"`
    Status       string   `json:"status"`
}
```

### 4.6 事件发布占位模式

所有需要 messager 发布事件的方法中，使用统一注释标注模式：

```go
// 事件发布占位（⤵️ 9.65 回填）：
// tm.messager.Publish(TeamTopicTask, TaskCreatedEvent{TaskID: task.TaskID, ...})
```

**需要占位的事件类型**（对齐 Python 事件名）：

| 事件 | 发布时机 | 占位位置 |
|------|---------|---------|
| TaskCreatedEvent | Add/AddBatch/AddWithPriority/AddAsTopPriority | 各 Add 方法 |
| TaskClaimedEvent | Claim/Assign | Claim/Assign 方法 |
| TaskCompletedEvent | Complete | Complete 方法 |
| TaskCancelledEvent | Cancel/CancelAllTasks | Cancel/CancelAllTasks 方法 |
| TaskUnblockedEvent | 下游解除阻塞时 | Complete/Cancel 方法 |
| TaskListDrainedEvent | 所有任务终态 | Complete/Cancel/CancelAllTasks 方法 |
| TaskUpdatedEvent | UpdateTask | UpdateTask 方法 |
| TaskPlanRequestEvent | SubmitPlan | SubmitPlan 方法 |
| TaskPlanResponseEvent | ApprovePlan | ApprovePlan 方法 |

### 4.7 TaskListDrainedEvent 刦定逻辑

对齐 Python：在 Complete/Cancel/CancelAllTasks 操作后，重新读取全部任务列表，若所有任务均处于终态（COMPLETED 或 CANCELLED），则发布此事件。

```go
// 判定逻辑（⤵️ 9.65 回填事件发布）：
func (tm *TeamTaskManager) checkTaskListDrained(ctx) bool {
    tasks, _ := tm.db.Task().GetTeamTasks(ctx, tm.teamName, "")
    for _, t := range tasks {
        if t.Status != TaskStatusCompleted && t.Status != TaskStatusCancelled {
            return false
        }
    }
    return true
}
```

## 5. 回填点汇总

| 回填点 | 当前状态 | 回填内容 | 回填章节 |
|--------|---------|---------|---------|
| `database/task_dao.go` | 空接口 | TaskDao 方法签名 + InMemory 实现 | 9.65a-2 |
| `database/database.go` L84 | 空接口 | 引用新 TaskDao 接口 | 9.65a-2 |
| `database/memory_impl.go` L107 | `Task() TaskDao { return db }` 已接线 | 实现 TaskDao 方法 | 9.65a-2 |
| `tools/task_manager.go` | 11 方法 any 返回值 | 具体实现 + FSM 校验 + 事件占位 | 9.65a-2 |
| `agent/infra.go` L22 | `TaskManager any` | 类型化为 `tools.TeamTaskManager` | 9.65a-2 |
| `agent/agent_configurator.go` L254 | 注入注释 | 实际调用 | 9.65a-2 |
| `memory/manager.go` L56 | 已声明 | 验证使用 | 9.65a-2 |
| `memory/manager_params.go` L89 | 已声明 | 验证注入 | 9.65a-2 |
| TaskManager.messager | `any` 占位 | Messager 接口实现 | 9.65 |
| TaskManager 事件发布 | 注释占位 | messager.Publish 调用 | 9.65 |

## 6. 测试目标

### A 阶段测试

- InMemoryTaskDao 全部方法的单元测试
- mutationContext 管线各步骤的独立测试 + 整体管线集成测试
- FSM 校验测试（invalid transition 返回 false）
- 原子终止传播测试（complete/cancel 后下游自动解除阻塞）
- 环路检测测试
- 一致性修复测试

### B 阶段测试

- TeamTaskManager 全部方法的单元测试
- PLAN_MODE 完整流程测试（submit → approve / reject）
- 计划文件存储 + index.json 读写测试
- TaskListDrained 判定测试
- 事件发布占位验证（注释标注完整）

### 覆盖率目标

≥ 85%（`go test -cover ./internal/agent_teams/tools/database/... ./internal/agent_teams/tools/...`）
