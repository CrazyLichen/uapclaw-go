# 9.65a-2 TaskDao + TaskManager 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 TeamBackend 子系统中的任务管理数据层（TaskDao）和业务层（TeamTaskManager），对齐 Python 的 task_dao.py + task_manager.py。

**Architecture:** 拆分 A+B 两阶段。A 阶段先实现 TaskDao 纯数据层（接口+InMemory实现+依赖图5步管线+原子终止传播），B 阶段实现 TeamTaskManager 业务层（全部方法+PLAN_MODE审批+事件发布any占位）。管线采用 mutationContext struct + failReason flag 闭包模式替代 Python _MutationFailure 异常。终止传播一次 Lock 内完成。

**Tech Stack:** Go 1.x, sync.Mutex, encoding/json, os/filepath（plan文件IO）

---

## 文件结构

| 文件 | 责责 | 变更类型 |
|------|------|---------|
| `internal/agent_teams/tools/database/models.go` | TeamTaskBase + TeamTaskDependencyBase + NewTaskSpec + EdgeSpec + GraphMutationResult 数据模型 | 修改 |
| `internal/agent_teams/tools/database/database.go` | TaskDao 空接口 → 具体 18 方法签名 | 修改 |
| `internal/agent_teams/tools/database/task_dao.go` | 占位 → InMemoryTaskDao 方法实现（管线+终止传播） | 修改 |
| `internal/agent_teams/tools/database/memory_impl.go` | 新增 tasks/deps map + TaskDao 实现 | 修改 |
| `internal/agent_teams/tools/database/memory_impl_test.go` | 新增 TaskDao 全部方法测试 | 修改 |
| `internal/agent_teams/tools/database/fsm.go` | 新增 IsValidTaskTransition wrapper | 修改 |
| `internal/agent_teams/tools/database/doc.go` | 更新文件目录 | 修改 |
| `internal/agent_teams/tools/task_manager.go` | any 返回值 → 具体 TeamTaskManager struct + 实现 | 修改 |
| `internal/agent_teams/tools/task_manager_test.go` | TeamTaskManager 测试 | 新建 |
| `internal/agent_teams/tools/doc.go` | 更新文件目录 | 修改 |
| `internal/agent_teams/agent/infra.go` | TaskManager any → tools.TeamTaskManager | 修改 |

---

## Task 1: 数据模型 + FSM wrapper

**Files:**
- Modify: `internal/agent_teams/tools/database/models.go:1-72`
- Modify: `internal/agent_teams/tools/database/fsm.go:1-19`

- [ ] **Step 1: 在 models.go 中添加 Task 数据模型**

在 `models.go` 文件末尾（`TeamDynamicTablePrefixes` 之后）添加以下数据模型。保持声明顺序规范：结构体→全局变量。

```go
// TeamTaskBase 任务行模型。
// 对齐 Python: TeamTaskBase (openjiuwen/agent_teams/tools/database/task_dao.py)
// 动态表 team_task_<session_suffix> 的行模型。
type TeamTaskBase struct {
	// TaskID 任务唯一标识
	TaskID string `json:"task_id"`
	// TeamName 团队名称
	TeamName string `json:"team_name"`
	// Title 任务标题
	Title string `json:"title"`
	// Content 任务内容
	Content string `json:"content"`
	// Status 任务状态（TaskStatus 枚举值）
	Status string `json:"status"`
	// Assignee 认领人/分配人
	Assignee string `json:"assignee,omitempty"`
	// UpdatedAt 更新时间（毫秒时间戳）
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

// TeamTaskDependencyBase 依赖边模型。
// 对齐 Python: TeamTaskDependencyBase (openjiuwen/agent_teams/tools/database/task_dao.py)
// 动态表 team_task_dependency_<session_suffix> 的行模型。
type TeamTaskDependencyBase struct {
	// TaskID 下游任务ID（被阻塞的任务）
	TaskID string `json:"task_id"`
	// DependsOnID 上游任务ID（阻塞源）
	DependsOnID string `json:"depends_on_task_id"`
	// TeamName 团队名称
	TeamName string `json:"team_name"`
	// Resolved 依赖是否已解决（上游完成/取消时标记为 true）
	Resolved bool `json:"resolved"`
}
```

同时在 `models.go` 的全局变量区域（现有 `TeamDynamicTablePrefixes` 之后）添加：

```go
// ──────────────────────────── 辅助类型 ────────────────────────────

// NewTaskSpec 图变更管线中待插入的新任务规范。
// 对齐 Python: NewTaskSpec (openjiuwen/agent_teams/tools/database/task_dao.py)
type NewTaskSpec struct {
	// TaskID 任务唯一标识
	TaskID string
	// Title 任务标题
	Title string
	// Content 任务内容
	Content string
	// InitialStatus 初始状态
	InitialStatus string
}

// EdgeSpec 依赖边规范（管线输入）。
// 方向语义：TaskID 依赖 DependsOnID（TaskID 被 DependsOnID 阻塞）。
// 对齐 Python 的 (task_id, depends_on_task_id) 边方向。
type EdgeSpec struct {
	// TaskID 下游任务ID（被阻塞的任务）
	TaskID string
	// DependsOnID 上游任务ID（阻塞源）
	DependsOnID string
}

// GraphMutationResult 图变更操作结果。
// 对齐 Python: GraphMutationResult (openjiuwen/agent_teams/tools/database/task_dao.py)
type GraphMutationResult struct {
	// Ok 操作是否成功
	Ok bool
	// Reason 失败原因（Ok=false 时）
	Reason string
	// RefreshedTasks 状态刷新产出的任务ID列表
	RefreshedTasks []string
}
```

- [ ] **Step 2: 在 fsm.go 中添加 IsValidTaskTransition wrapper**

在 `fsm.go` 文件末尾添加：

```go
// IsValidTaskTransition 检查 TaskStatus 状态转换是否合法。
// 对齐 Python: is_valid_transition(current, new, TASK_TRANSITIONS)
// 委托 fsm 包实现，本包仅提供 string 版 wrapper。
func IsValidTaskTransition(current, target string) bool {
	return fsm.IsValidTaskTransition(current, target)
}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/tools/database/models.go internal/agent_teams/tools/database/fsm.go
git commit -m "feat(9.65a-2): 添加 Task 数据模型和 FSM wrapper"
```

---

## Task 2: TaskDao 接口定义

**Files:**
- Modify: `internal/agent_teams/tools/database/database.go:84-88`

- [ ] **Step 1: 替换 TaskDao 空接口为具体 18 方法签名**

在 `database.go` 中，将 L84-88 的 `TaskDao interface{}` 和 `MessageDao interface{}` 替换为：

```go
// TaskDao 任务 DAO 接口。
// 对齐 Python: TaskDao (openjiuwen/agent_teams/tools/database/task_dao.py)
type TaskDao interface {
	// CreateTask 创建单条任务。返回 true 表示成功，false 表示 task_id 冲突（对齐 Python IntegrityError → False）。
	CreateTask(ctx context.Context, task *TeamTaskBase) (bool, error)
	// GetTask 按 ID 查询任务。返回 nil 表示不存在（对齐 Python Optional[TeamTaskBase]）。
	GetTask(ctx context.Context, taskID string) (*TeamTaskBase, error)
	// GetTeamTasks 查询团队全部任务。status 为空字符串表示不过滤。
	GetTeamTasks(ctx context.Context, teamName, status string) ([]*TeamTaskBase, error)
	// GetTasksByAssignee 查询成员的任务。status 为空字符串表示不过滤。
	GetTasksByAssignee(ctx context.Context, teamName, assignee, status string) ([]*TeamTaskBase, error)
	// ClaimTask 认领任务：设置 assignee + PENDING→CLAIMED FSM 校验。
	// 对齐 Python: claim_task() → bool
	ClaimTask(ctx context.Context, taskID, assignee string) (bool, error)
	// ResetTask 重置任务：CLAIMED→PENDING，清除 assignee。
	// 对齐 Python: reset_task() → bool
	ResetTask(ctx context.Context, taskID string) (bool, error)
	// ApprovePlanTask 计划审批：CLAIMED→PLAN_APPROVED FSM 校验。
	// 对齐 Python: approve_plan_task() → bool
	ApprovePlanTask(ctx context.Context, taskID string) (bool, error)
	// UpdateTaskStatus 更新任务状态。完成时自动解除下游依赖并刷新 BLOCKED→PENDING。
	// 返回刷新的 task ID 列表。
	UpdateTaskStatus(ctx context.Context, taskID, newStatus string) ([]string, error)
	// UpdateTask 更新标题/内容。CLAIMED/PLAN_APPROVED 状态下禁止编辑。
	// 对齐 Python: update_task() → bool
	UpdateTask(ctx context.Context, taskID, title, content string) (bool, error)
	// MutateDependencyGraph 原子图变更：5 步管线。
	// 对齐 Python: mutate_dependency_graph()
	MutateDependencyGraph(ctx context.Context, teamName string, newTasks []NewTaskSpec, addEdges []EdgeSpec) GraphMutationResult
	// AddTaskWithBidirectionalDependencies 带双向依赖创建任务。委托 MutateDependencyGraph。
	// 对齐 Python: add_task_with_bidirectional_dependencies()
	AddTaskWithBidirectionalDependencies(ctx context.Context, teamName string, task *TeamTaskBase, dependsOnIDs []string) GraphMutationResult
	// GetTaskDependencies 查询任务依赖。
	GetTaskDependencies(ctx context.Context, taskID string) ([]*TeamTaskDependencyBase, error)
	// GetUnresolvedDependenciesCount 未解决依赖计数。
	GetUnresolvedDependenciesCount(ctx context.Context, taskID string) (int, error)
	// GetTasksDependingOn 查询下游依赖任务（即被 taskID 阻塞的任务）。
	GetTasksDependingOn(ctx context.Context, taskID string) ([]*TeamTaskBase, error)
	// DeleteTask 删除任务。
	DeleteTask(ctx context.Context, taskID string) error
	// CancelTask 取消任务（原子终止传播），返回 unblocked task IDs。
	CancelTask(ctx context.Context, taskID string) ([]string, error)
	// CancelAllTasks 批量取消（原子终止传播），支持 skipAssignees 过滤。
	CancelAllTasks(ctx context.Context, teamName string, skipAssignees []string) ([]*TeamTaskBase, error)
	// CompleteTask 完成任务（原子终止传播），返回 unblocked task IDs。
	CompleteTask(ctx context.Context, taskID string) ([]string, error)
	// VerifyAndFixTaskConsistency 一致性修复：扫描 BLOCKED 任务并刷新状态。
	VerifyAndFixTaskConsistency(ctx context.Context, teamName string) ([]string, error)
}

// MessageDao 消息 DAO 接口。⤵️ 9.65a-3 回填具体方法签名
type MessageDao interface{}
```

- [ ] **Step 2: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/...`

注意：此步骤编译会失败，因为 InMemoryTeamDatabase 还没有实现 TaskDao 的新方法。这是预期的。先继续 Task 3 实现后再验证编译。

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/tools/database/database.go
git commit -m "feat(9.65a-2): 定义 TaskDao 18 方法接口签名"
```

---

## Task 3: InMemoryTeamDatabase 新增字段 + 基础 TaskDao 方法

**Files:**
- Modify: `internal/agent_teams/tools/database/memory_impl.go:16-35`

- [ ] **Step 1: 在 InMemoryTeamDatabase 结构体中添加 tasks 和 deps 字段**

修改 `memory_impl.go` 中 InMemoryTeamDatabase 结构体（L16-25），添加 tasks 和 deps map：

```go
type InMemoryTeamDatabase struct {
	// teams 团队数据，key=teamName
	teams map[string]*Team
	// members 成员数据，key=memberName+"\x00"+teamName（复合主键编码）
	members map[string]*TeamMember
	// tasks 任务数据，key=taskID
	tasks map[string]*TeamTaskBase
	// deps 依赖边数据，key=taskID+"\x00"+dependsOnID（复合主键编码）
	deps map[string]*TeamTaskDependencyBase
	// initialized 是否已初始化
	initialized bool
	// mu 保护并发访问
	mu sync.Mutex
}
```

同时更新 `NewInMemoryTeamDatabase`（L30-35）初始化新 map：

```go
func NewInMemoryTeamDatabase() *InMemoryTeamDatabase {
	return &InMemoryTeamDatabase{
		teams:   make(map[string]*Team),
		members: make(map[string]*TeamMember),
		tasks:   make(map[string]*TeamTaskBase),
		deps:    make(map[string]*TeamTaskDependencyBase),
	}
}
```

更新 `CleanupAllRuntimeState`（L61-67）清空新 map：

```go
func (db *InMemoryTeamDatabase) CleanupAllRuntimeState(_ context.Context) ([]string, []string, error) {
	db.mu.Lock()
	db.teams = make(map[string]*Team)
	db.members = make(map[string]*TeamMember)
	db.tasks = make(map[string]*TeamTaskBase)
	db.deps = make(map[string]*TeamTaskDependencyBase)
	db.mu.Unlock()
	return nil, nil, nil
}
```

更新 `ForceDeleteTeamSession`（L76-88）级联删除任务和依赖：

```go
func (db *InMemoryTeamDatabase) ForceDeleteTeamSession(_ context.Context, teamName string) bool {
	db.mu.Lock()
	_, exists := db.teams[teamName]
	delete(db.teams, teamName)
	// 删除该 team 下所有成员
	for key, member := range db.members {
		if member.TeamName == teamName {
			delete(db.members, key)
		}
	}
	// 删除该 team 下所有任务
	for id, task := range db.tasks {
		if task.TeamName == teamName {
			delete(db.tasks, id)
		}
	}
	// 删除该 team 下所有依赖边
	for key, dep := range db.deps {
		if dep.TeamName == teamName {
			delete(db.deps, key)
		}
	}
	db.mu.Unlock()
	return exists
}
```

更新 `Close`（L91-98）清空新 map：

```go
func (db *InMemoryTeamDatabase) Close() error {
	db.mu.Lock()
	db.teams = nil
	db.members = nil
	db.tasks = nil
	db.deps = nil
	db.initialized = false
	db.mu.Unlock()
	return nil
}
```

在 `memberKey` 函数下方添加 `depKey` 函数：

```go
// depKey 构造依赖边复合主键 key。
func depKey(taskID, dependsOnID string) string {
	return taskID + "\x00" + dependsOnID
}
```

- [ ] **Step 2: 添加基础 TaskDao CRUD 方法实现**

在 `memory_impl.go` 末尾（MemberDao 实现区域之后、编译期断言之前）添加以下基础 CRUD 方法：

```go
// ──────────────────────────── TaskDao 接口实现 ────────────────────────────

// CreateTask 创建单条任务。对齐 Python: TaskDao.create_task()
// 成功返回 true，task_id 冲突返回 false（对齐 Python IntegrityError → False）
func (db *InMemoryTeamDatabase) CreateTask(_ context.Context, task *TeamTaskBase) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, exists := db.tasks[task.TaskID]; exists {
		return false, nil // 对齐 Python IntegrityError → False
	}
	task.UpdatedAt = GetCurrentTime()
	db.tasks[task.TaskID] = task
	return true, nil
}

// GetTask 按 ID 查询任务。对齐 Python: TaskDao.get_task()
func (db *InMemoryTeamDatabase) GetTask(_ context.Context, taskID string) (*TeamTaskBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, exists := db.tasks[taskID]
	if !exists {
		return nil, nil // 对齐 Python Optional[TeamTaskBase] → None
	}
	return task, nil
}

// GetTeamTasks 查询团队全部任务。对齐 Python: TaskDao.get_team_tasks()
// status 为空字符串表示不过滤。
func (db *InMemoryTeamDatabase) GetTeamTasks(_ context.Context, teamName, status string) ([]*TeamTaskBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []*TeamTaskBase
	for _, task := range db.tasks {
		if task.TeamName != teamName {
			continue
		}
		if status != "" && task.Status != status {
			continue
		}
		result = append(result, task)
	}
	return result, nil
}

// GetTasksByAssignee 查询成员的任务。对齐 Python: TaskDao.get_tasks_by_assignee()
// status 为空字符串表示不过滤。
func (db *InMemoryTeamDatabase) GetTasksByAssignee(_ context.Context, teamName, assignee, status string) ([]*TeamTaskBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []*TeamTaskBase
	for _, task := range db.tasks {
		if task.TeamName != teamName || task.Assignee != assignee {
			continue
		}
		if status != "" && task.Status != status {
			continue
		}
		result = append(result, task)
	}
	return result, nil
}

// DeleteTask 删除任务（级联删除依赖边）。对齐 Python: TaskDao.delete_task()
func (db *InMemoryTeamDatabase) DeleteTask(_ context.Context, taskID string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.tasks, taskID)
	// 级联删除涉及此任务的依赖边（作为上游或下游）
	for key, dep := range db.deps {
		if dep.TaskID == taskID || dep.DependsOnID == taskID {
			delete(db.deps, key)
		}
	}
	return nil
}

// GetTaskDependencies 查询任务依赖。对齐 Python: TaskDao.get_task_dependencies()
func (db *InMemoryTeamDatabase) GetTaskDependencies(_ context.Context, taskID string) ([]*TeamTaskDependencyBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []*TeamTaskDependencyBase
	for _, dep := range db.deps {
		if dep.TaskID == taskID {
			result = append(result, dep)
		}
	}
	return result, nil
}

// GetUnresolvedDependenciesCount 未解决依赖计数。对齐 Python: TaskDao.get_unresolved_dependencies_count()
func (db *InMemoryTeamDatabase) GetUnresolvedDependenciesCount(_ context.Context, taskID string) (int, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	count := 0
	for _, dep := range db.deps {
		if dep.TaskID == taskID && !dep.Resolved {
			count++
		}
	}
	return count, nil
}

// GetTasksDependingOn 查询下游依赖任务。对齐 Python: TaskDao.get_tasks_depending_on()
// 返回 taskID 阻塞的任务列表（即 deps 中 depends_on_task_id == taskID 的下游任务）
func (db *InMemoryTeamDatabase) GetTasksDependingOn(_ context.Context, taskID string) ([]*TeamTaskBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []*TeamTaskBase
	for _, dep := range db.deps {
		if dep.DependsOnID == taskID {
			task, exists := db.tasks[dep.TaskID]
			if exists {
				result = append(result, task)
			}
		}
	}
	return result, nil
}
```

- [ ] **Step 3: 添加 TaskDao FSM 状态转换方法实现**

在基础 CRUD 方法之后添加 FSM 校验方法：

```go
// ClaimTask 认领任务：设置 assignee + PENDING→CLAIMED FSM 校验。
// 对齐 Python: TaskDao.claim_task()
func (db *InMemoryTeamDatabase) ClaimTask(_ context.Context, taskID, assignee string) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, exists := db.tasks[taskID]
	if !exists {
		return false, nil
	}
	if !IsValidTaskTransition(task.Status, TaskStatusClaimed) {
		return false, nil // 对齐 Python: invalid transition → False
	}
	task.Status = TaskStatusClaimed
	task.Assignee = assignee
	task.UpdatedAt = GetCurrentTime()
	return true, nil
}

// ResetTask 重置任务：CLAIMED→PENDING，清除 assignee。
// 对齐 Python: TaskDao.reset_task()
func (db *InMemoryTeamDatabase) ResetTask(_ context.Context, taskID string) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, exists := db.tasks[taskID]
	if !exists {
		return false, nil
	}
	if !IsValidTaskTransition(task.Status, TaskStatusPending) {
		return false, nil
	}
	task.Status = TaskStatusPending
	task.Assignee = ""
	task.UpdatedAt = GetCurrentTime()
	return true, nil
}

// ApprovePlanTask 计划审批：CLAIMED→PLAN_APPROVED FSM 校验。
// 对齐 Python: TaskDao.approve_plan_task()
func (db *InMemoryTeamDatabase) ApprovePlanTask(_ context.Context, taskID string) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, exists := db.tasks[taskID]
	if !exists {
		return false, nil
	}
	if !IsValidTaskTransition(task.Status, TaskStatusPlanApproved) {
		return false, nil
	}
	task.Status = TaskStatusPlanApproved
	task.UpdatedAt = GetCurrentTime()
	return true, nil
}

// UpdateTask 更新标题/内容。CLAIMED/PLAN_APPROVED 状态下禁止编辑。
// 对齐 Python: TaskDao.update_task()
func (db *InMemoryTeamDatabase) UpdateTask(_ context.Context, taskID, title, content string) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, exists := db.tasks[taskID]
	if !exists {
		return false, nil
	}
	// 对齐 Python: CLAIMED/PLAN_APPROVED 状态下禁止编辑标题内容
	if task.Status == TaskStatusClaimed || task.Status == TaskStatusPlanApproved {
		return false, nil
	}
	task.Title = title
	task.Content = content
	task.UpdatedAt = GetCurrentTime()
	return true, nil
}
```

注意：`TaskStatusClaimed`、`TaskStatusPending`、`TaskStatusPlanApproved` 等常量来自 `fsm` 包，但 TaskDao 方法中需要用 string 常量。通过 `fsm.go` 中已有的 wrapper 或者直接引用 `fsm` 包常量。需要确认 database 包 import fsm 包（已确认 `fsm.go` 已 import `fsm` 包）。

但 database 包中已定义 string 常量映射：在 fsm.go 中 wrapper 直接调用 fsm 包函数。TaskDao 方法中应使用 `fsm.TaskStatusPending` 等常量（因为它们是 string 类型）。需要添加 import。

- [ ] **Step 4: 更新编译期断言**

更新 `memory_impl.go` 底部的编译期断言，添加 TaskDao：

```go
var (
	_ TeamDatabase = (*InMemoryTeamDatabase)(nil)
	_ TeamDao      = (*InMemoryTeamDatabase)(nil)
	_ MemberDao    = (*InMemoryTeamDatabase)(nil)
	_ TaskDao      = (*InMemoryTeamDatabase)(nil) // InMemoryTeamDatabase 必须满足 TaskDao 接口
)
```

- [ ] **Step 5: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/tools/database/...`

注意：此时编译会失败，因为 UpdateTaskStatus/MutateDependencyGraph/AddTaskWithBidirectionalDependencies/CancelTask/CancelAllTasks/CompleteTask/VerifyAndFixTaskConsistency 还没实现。先继续后续 Task 实现后再验证。

- [ ] **Step 6: Commit**

```bash
git add internal/agent_teams/tools/database/memory_impl.go
git commit -m "feat(9.65a-2): InMemoryTeamDatabase 新增 tasks/deps map 和基础 TaskDao CRUD+FSM 方法"
```

---

## Task 4: 依赖图变更管线（mutateDependencyGraph + addTaskWithBidirectionalDependencies）

**Files:**
- Modify: `internal/agent_teams/tools/database/memory_impl.go`

- [ ] **Step 1: 定义 mutationContext 结构体和管线方法**

在 `memory_impl.go` 的非导出函数区域添加 mutationContext 和 5 步管线方法：

```go
// ──────────────────────────── 依赖图管线 ────────────────────────────

// mutationContext 依赖图变更管线共享上下文。
// 替代 Python _MutationFailure 异常信号，使用 failReason flag 模式。
type mutationContext struct {
	db       *InMemoryTeamDatabase
	teamName string
	newTasks []NewTaskSpec
	addEdges []EdgeSpec

	// 步骤间共享数据（闭包操作）
	stagedTasks   map[string]*TeamTaskBase   // 步骤1产出：已插入的新任务
	endpointTasks map[string]*TeamTaskBase   // 步骤2产出：边端点对应的任务
	newEdgeRows   []TeamTaskDependencyBase   // 步骤4产出：待插入的依赖边行
	refreshedIDs  []string                   // 步骤5产出：状态刷新的 task IDs

	// 失败标记（替代 Python _MutationFailure）
	failReason string
}

// stageNewTasks 步骤1：插入新任务行，检测 task_id 重复。
// 对齐 Python: _stage_new_tasks()
func (mc *mutationContext) stageNewTasks() {
	mc.stagedTasks = make(map[string]*TeamTaskBase)
	for _, spec := range mc.newTasks {
		if _, exists := mc.db.tasks[spec.TaskID]; exists {
			mc.failReason = "task_id already exists: " + spec.TaskID
			return
		}
		// 检查已 staged 的新任务中是否也有重复
		if _, exists := mc.stagedTasks[spec.TaskID]; exists {
			mc.failReason = "duplicate new task_id in spec: " + spec.TaskID
			return
		}
		status := spec.InitialStatus
		if status == "" {
			status = fsm.TaskStatusPending // 默认 PENDING
		}
		task := &TeamTaskBase{
			TaskID:   spec.TaskID,
			TeamName: mc.teamName,
			Title:    spec.Title,
			Content:  spec.Content,
			Status:   status,
			UpdatedAt: GetCurrentTime(),
		}
		mc.db.tasks[spec.TaskID] = task
		mc.stagedTasks[spec.TaskID] = task
	}
}

// loadEndpointsAndValidate 步骤2：加载边端点，拒绝缺失/终态/已执行源。
// 对齐 Python: _load_endpoints_and_validate()
func (mc *mutationContext) loadEndpointsAndValidate() {
	mc.endpointTasks = make(map[string]*TeamTaskBase)
	for _, edge := range mc.addEdges {
		// 加载下游任务（被阻塞的）
		downstream, downExists := mc.db.tasks[edge.TaskID]
		if !downExists {
			mc.failReason = "edge endpoint not found: " + edge.TaskID
			mc.rollbackStagedTasks()
			return
		}
		mc.endpointTasks[edge.TaskID] = downstream

		// 加载上游任务（阻塞源）
		upstream, upExists := mc.db.tasks[edge.DependsOnID]
		if !upExists {
			mc.failReason = "edge endpoint not found: " + edge.DependsOnID
			mc.rollbackStagedTasks()
			return
		}
		mc.endpointTasks[edge.DependsOnID] = upstream

		// 拒绝终态目标：上游是 COMPLETED/CANCELLED 时不允许添加依赖
		if upstream.Status == fsm.TaskStatusCompleted || upstream.Status == fsm.TaskStatusCancelled {
			mc.failReason = "cannot add dependency on terminal task: " + edge.DependsOnID
			mc.rollbackStagedTasks()
			return
		}
	}
}

// checkCycleAndComputeNewEdges 步骤3：构建后变更邻接表，检测环路。
// 对齐 Python: _check_cycle_and_compute_new_edges()
func (mc *mutationContext) checkCycleAndComputeNewEdges() {
	// 构建后变更邻接表：downstream → [upstream1, upstream2, ...]
	adj := make(map[string][]string)
	// 加载已有边
	for _, dep := range mc.db.deps {
		if dep.TeamName == mc.teamName {
			adj[dep.TaskID] = append(adj[dep.TaskID], dep.DependsOnID)
		}
	}
	// 加载新边
	for _, edge := range mc.addEdges {
		adj[edge.TaskID] = append(adj[edge.TaskID], edge.DependsOnID)
	}

	// 检测环路：DFS
	visited := make(map[string]int) // 0=未访问, 1=正在访问, 2=已完成
	for node := range adj {
		if mc.hasCycleDFS(node, adj, visited) {
			mc.failReason = "dependency cycle detected involving: " + node
			mc.rollbackStagedTasks()
			return
		}
	}

	// 计算新边行
	mc.newEdgeRows = make([]TeamTaskDependencyBase, 0, len(mc.addEdges))
	for _, edge := range mc.addEdges {
		upstream := mc.endpointTasks[edge.DependsOnID]
		// 终态依赖初始 resolved=True（对齐 Python）
		resolved := upstream.Status == fsm.TaskStatusCompleted || upstream.Status == fsm.TaskStatusCancelled
		mc.newEdgeRows = append(mc.newEdgeRows, TeamTaskDependencyBase{
			TaskID:      edge.TaskID,
			DependsOnID: edge.DependsOnID,
			TeamName:    mc.teamName,
			Resolved:    resolved,
		})
	}
}

// hasCycleDFS DFS 检测环路。
func (mc *mutationContext) hasCycleDFS(node string, adj map[string][]string, visited map[string]int) bool {
	if visited[node] == 1 {
		return true // 环路
	}
	if visited[node] == 2 {
		return false // 已完成
	}
	visited[node] = 1 // 正在访问
	for _, neighbor := range adj[node] {
		if mc.hasCycleDFS(neighbor, adj, visited) {
			return true
		}
	}
	visited[node] = 2 // 已完成
	return false
}

// applyNewEdges 步骤4：插入依赖边行。
func (mc *mutationContext) applyNewEdges() {
	for _, edgeRow := range mc.newEdgeRows {
		mc.db.deps[depKey(edgeRow.TaskID, edgeRow.DependsOnID)] = &edgeRow
	}
}

// refreshStatus 步骤5：刷新 PENDING↔BLOCKED 状态。
// 对齐 Python: _refresh_status_in_session()
func (mc *mutationContext) refreshStatus() {
	mc.refreshedIDs = mc.db.refreshTaskStatuses(mc.teamName)
}

// rollbackStagedTasks 回滚步骤1插入的新任务。
func (mc *mutationContext) rollbackStagedTasks() {
	for id := range mc.stagedTasks {
		delete(mc.db.tasks, id)
	}
}
```

需要 import `fsm` 包。在 memory_impl.go 的 import 区域添加 `"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"`。

- [ ] **Step 2: 添加 MutateDependencyGraph 主方法和 AddTaskWithBidirectionalDependencies**

```go
// MutateDependencyGraph 原子图变更：5 步管线。
// 对齐 Python: mutate_dependency_graph()
func (db *InMemoryTeamDatabase) MutateDependencyGraph(_ context.Context, teamName string, newTasks []NewTaskSpec, addEdges []EdgeSpec) GraphMutationResult {
	db.mu.Lock()
	defer db.mu.Unlock()

	mc := &mutationContext{db: db, teamName: teamName, newTasks: newTasks, addEdges: addEdges}

	// 步骤1
	mc.stageNewTasks()
	if mc.failReason != "" {
		return GraphMutationResult{Ok: false, Reason: mc.failReason}
	}

	// 步骤2
	mc.loadEndpointsAndValidate()
	if mc.failReason != "" {
		return GraphMutationResult{Ok: false, Reason: mc.failReason}
	}

	// 步骤3
	mc.checkCycleAndComputeNewEdges()
	if mc.failReason != "" {
		return GraphMutationResult{Ok: false, Reason: mc.failReason}
	}

	// 步骤4
	mc.applyNewEdges()

	// 步骤5
	mc.refreshStatus()

	return GraphMutationResult{Ok: true, RefreshedTasks: mc.refreshedIDs}
}

// AddTaskWithBidirectionalDependencies 带双向依赖创建任务。
// 对齐 Python: add_task_with_bidirectional_dependencies()
func (db *InMemoryTeamDatabase) AddTaskWithBidirectionalDependencies(_ context.Context, teamName string, task *TeamTaskBase, dependsOnIDs []string) GraphMutationResult {
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

	// 构建 EdgeSpec：task 依赖 dependsOnIDs 中的每个上游任务
	edges := make([]EdgeSpec, 0, len(dependsOnIDs))
	for _, upstreamID := range dependsOnIDs {
		edges = append(edges, EdgeSpec{TaskID: task.TaskID, DependsOnID: upstreamID})
	}

	return db.MutateDependencyGraph(context.Background(), teamName, []NewTaskSpec{newTaskSpec}, edges)
}
```

- [ ] **Step 3: 添加 refreshTaskStatuses 内部方法**

```go
// refreshTaskStatuses 刷新团队内所有任务的 PENDING↔BLOCKED 状态。
// 对齐 Python: _refresh_status_in_session()
// PENDING + 有未解决依赖 → BLOCKED
// BLOCKED + 无未解决依赖 → PENDING
func (db *InMemoryTeamDatabase) refreshTaskStatuses(teamName string) []string {
	var refreshed []string
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
			refreshed = append(refreshed, task.TaskID)
		}
	}
	return refreshed
}
```

- [ ] **Step 4: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/tools/database/...`

注意：此时 UpdateTaskStatus/CancelTask/CancelAllTasks/CompleteTask/VerifyAndFixTaskConsistency 还没实现。

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/tools/database/memory_impl.go
git commit -m "feat(9.65a-2): 实现依赖图5步管线 mutationContext 闭包模式"
```

---

## Task 5: 原子终止传播（UpdateTaskStatus + CancelTask/CompleteTask/CancelAllTasks + VerifyAndFixTaskConsistency）

**Files:**
- Modify: `internal/agent_teams/tools/database/memory_impl.go`

- [ ] **Step 1: 添加 terminateTaskInSession 内部方法**

```go
// terminateTaskInSession 原子终止传播：终止任务 + 标记下游 resolved + 刷新状态。
// 对齐 Python: _terminate_task_in_session()
// 一次 Lock 内完成所有操作（由调用方持锁，此方法不加锁）。
func (db *InMemoryTeamDatabase) terminateTaskInSession(taskID, terminalStatus string) ([]string, error) {
	task, exists := db.tasks[taskID]
	if !exists {
		return nil, nil
	}
	if !IsValidTaskTransition(task.Status, terminalStatus) {
		return nil, nil
	}

	// 1. 将任务设为终态
	task.Status = terminalStatus
	task.UpdatedAt = GetCurrentTime()

	// 2. 批量标记下游依赖为 resolved=True
	for key, dep := range db.deps {
		if dep.DependsOnID == taskID && !dep.Resolved {
			dep.Resolved = true
			db.deps[key] = dep
		}
	}

	// 3. 对所有下游任务执行状态刷新（可能解除阻塞）
	// 收集下游任务 ID
	var downstreamIDs []string
	for _, dep := range db.deps {
		if dep.DependsOnID == taskID {
			downstreamIDs = append(downstreamIDs, dep.TaskID)
		}
	}

	// 刷新每个下游任务的状态
	var refreshed []string
	for _, downID := range downstreamIDs {
		downTask, downExists := db.tasks[downID]
		if !downExists {
			continue
		}
		// 重新计算下游任务的未解决依赖数
		if downTask.Status == fsm.TaskStatusBlocked {
			unresolved := 0
			for _, dep := range db.deps {
				if dep.TaskID == downID && !dep.Resolved {
					unresolved++
				}
			}
			if unresolved == 0 {
				downTask.Status = fsm.TaskStatusPending
				downTask.UpdatedAt = GetCurrentTime()
				refreshed = append(refreshed, downID)
			}
		}
	}

	return refreshed, nil
}
```

- [ ] **Step 2: 添加 UpdateTaskStatus / CancelTask / CompleteTask / CancelAllTasks / VerifyAndFixTaskConsistency**

```go
// UpdateTaskStatus 更新任务状态。完成时自动解除下游依赖并刷新 BLOCKED→PENDING。
// 对齐 Python: TaskDao.update_task_status()
func (db *InMemoryTeamDatabase) UpdateTaskStatus(_ context.Context, taskID, newStatus string) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	task, exists := db.tasks[taskID]
	if !exists {
		return nil, nil
	}
	if !IsValidTaskTransition(task.Status, newStatus) {
		return nil, nil
	}

	// 如果是终态（COMPLETED/CANCELLED），执行终止传播
	if newStatus == fsm.TaskStatusCompleted || newStatus == fsm.TaskStatusCancelled {
		return db.terminateTaskInSession(taskID, newStatus)
	}

	// 非终态转换：直接更新状态
	task.Status = newStatus
	task.UpdatedAt = GetCurrentTime()
	return nil, nil
}

// CancelTask 取消任务（原子终止传播）。对齐 Python: TaskDao.cancel_task()
func (db *InMemoryTeamDatabase) CancelTask(_ context.Context, taskID string) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.terminateTaskInSession(taskID, fsm.TaskStatusCancelled)
}

// CompleteTask 完成任务（原子终止传播）。对齐 Python: TaskDao.complete_task()
func (db *InMemoryTeamDatabase) CompleteTask(_ context.Context, taskID string) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.terminateTaskInSession(taskID, fsm.TaskStatusCompleted)
}

// CancelAllTasks 批量取消（原子终止传播）。对齐 Python: TaskDao.cancel_all_tasks()
// skipAssignees 列表中的 assignee 对应的任务不会被取消。
func (db *InMemoryTeamDatabase) CancelAllTasks(_ context.Context, teamName string, skipAssignees []string) ([]*TeamTaskBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	skipSet := make(map[string]bool, len(skipAssignees))
	for _, a := range skipAssignees {
		skipSet[a] = true
	}

	var cancelled []*TeamTaskBase
	for _, task := range db.tasks {
		if task.TeamName != teamName {
			continue
		}
		// 终态跳过
		if task.Status == fsm.TaskStatusCompleted || task.Status == fsm.TaskStatusCancelled {
			continue
		}
		// skipAssignees 跳过
		if skipSet[task.Assignee] {
			continue
		}
		db.terminateTaskInSession(task.TaskID, fsm.TaskStatusCancelled)
		cancelled = append(cancelled, task)
	}
	return cancelled, nil
}

// VerifyAndFixTaskConsistency 一致性修复：扫描 BLOCKED 任务并刷新状态。
// 对齐 Python: TaskDao.verify_and_fix_task_consistency()
func (db *InMemoryTeamDatabase) VerifyAndFixTaskConsistency(_ context.Context, teamName string) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.refreshTaskStatuses(teamName), nil
}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/tools/database/...`
Expected: 编译成功

- [ ] **Step 4: 运行已有测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test ./internal/agent_teams/tools/database/...`
Expected: 所有已有测试通过

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/tools/database/memory_impl.go
git commit -m "feat(9.65a-2): 实现原子终止传播和全部 TaskDao 方法"
```

---

## Task 6: TaskDao 测试

**Files:**
- Modify: `internal/agent_teams/tools/database/memory_impl_test.go`

- [ ] **Step 1: 添加 CreateTask/GetTask/GetTeamTasks/GetTasksByAssignee/DeleteTask 测试**

在 `memory_impl_test.go` 末尾添加 TaskDao 测试区域：

```go
// ──────────────────────────── TaskDao 测试 ────────────────────────────

func TestCreateTask_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	task := &TeamTaskBase{TaskID: "task1", TeamName: "alpha", Title: "任务1", Content: "内容1", Status: "pending"}
	ok, err := db.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("CreateTask 返回错误: %v", err)
	}
	if !ok {
		t.Error("CreateTask 应返回 true")
	}

	got, _ := db.GetTask(ctx, "task1")
	if got == nil {
		t.Fatal("GetTask 应返回任务数据")
	}
	if got.TaskID != "task1" {
		t.Errorf("TaskID: got %q, want %q", got.TaskID, "task1")
	}
	if got.UpdatedAt == 0 {
		t.Error("UpdatedAt 应非零")
	}
}

func TestCreateTask_已存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	task := &TeamTaskBase{TaskID: "task1", TeamName: "alpha", Title: "任务1", Status: "pending"}
	db.CreateTask(ctx, task)
	ok, _ := db.CreateTask(ctx, &TeamTaskBase{TaskID: "task1", TeamName: "alpha", Title: "其他", Status: "pending"})
	if ok {
		t.Error("重复创建任务应返回 false")
	}
}

func TestGetTask_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	got, err := db.GetTask(ctx, "nonexist")
	if err != nil {
		t.Fatalf("GetTask 返回错误: %v", err)
	}
	if got != nil {
		t.Error("不存在任务应返回 nil")
	}
}

func TestGetTeamTasks_按状态过滤(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "pending", Title: "P1"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: "claimed", Title: "C1"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t3", TeamName: "alpha", Status: "pending", Title: "P2"})

	pending, _ := db.GetTeamTasks(ctx, "alpha", "pending")
	if len(pending) != 2 {
		t.Errorf("pending 任务数量: got %d, want 2", len(pending))
	}

	all, _ := db.GetTeamTasks(ctx, "alpha", "")
	if len(all) != 3 {
		t.Errorf("全部任务数量: got %d, want 3", len(all))
	}
}

func TestGetTasksByAssignee(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Assignee: "agent1", Status: "claimed", Title: "A1"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Assignee: "agent2", Status: "claimed", Title: "A2"})

	result, _ := db.GetTasksByAssignee(ctx, "alpha", "agent1", "")
	if len(result) != 1 {
		t.Errorf("agent1 的任务数量: got %d, want 1", len(result))
	}
}

func TestDeleteTask_级联删依赖(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "pending", Title: "上游"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: "pending", Title: "下游"})

	// 通过管线添加依赖
	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}})

	db.DeleteTask(ctx, "t1")
	// 上游被删除后，依赖边也应被级联删除
	deps, _ := db.GetTaskDependencies(ctx, "t2")
	if len(deps) != 0 {
		t.Errorf("上游删除后，下游依赖应为0: got %d", len(deps))
	}
}
```

- [ ] **Step 2: 添加 FSM 状态转换测试**

```go
func TestClaimTask_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "pending", Title: "任务"})

	ok, _ := db.ClaimTask(ctx, "t1", "agent1")
	if !ok {
		t.Error("ClaimTask PENDING→CLAIMED 应返回 true")
	}
	task, _ := db.GetTask(ctx, "t1")
	if task.Assignee != "agent1" {
		t.Errorf("Assignee: got %q, want %q", task.Assignee, "agent1")
	}
}

func TestClaimTask_非法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "completed", Title: "已完成"})

	ok, _ := db.ClaimTask(ctx, "t1", "agent1")
	if ok {
		t.Error("ClaimTask COMPLETED→CLAIMED 应返回 false")
	}
}

func TestResetTask_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "claimed", Assignee: "agent1", Title: "任务"})

	ok, _ := db.ResetTask(ctx, "t1")
	if !ok {
		t.Error("ResetTask CLAIMED→PENDING 应返回 true")
	}
	task, _ := db.GetTask(ctx, "t1")
	if task.Assignee != "" {
		t.Errorf("ResetTask 后 Assignee 应为空: got %q", task.Assignee)
	}
}

func TestApprovePlanTask_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "claimed", Title: "任务"})

	ok, _ := db.ApprovePlanTask(ctx, "t1")
	if !ok {
		t.Error("ApprovePlanTask CLAIMED→PLAN_APPROVED 应返回 true")
	}
	task, _ := db.GetTask(ctx, "t1")
	if task.Status != "plan_approved" {
		t.Errorf("Status: got %q, want %q", task.Status, "plan_approved")
	}
}
```

- [ ] **Step 3: 添加依赖图管线测试**

```go
func TestMutateDependencyGraph_简单成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "pending", Title: "上游"})
	newTasks := []NewTaskSpec{{TaskID: "t2", Title: "下游", InitialStatus: "pending"}}
	edges := []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}}

	result := db.MutateDependencyGraph(ctx, "alpha", newTasks, edges)
	if !result.Ok {
		t.Errorf("管线应成功: reason=%s", result.Reason)
	}

	task2, _ := db.GetTask(ctx, "t2")
	if task2 == nil {
		t.Fatal("t2 应已创建")
	}
	// t2 依赖 t1（未解决）→ t2 应变为 blocked
	if task2.Status != "blocked" {
		t.Errorf("t2 有未解决依赖应变为 blocked: got %q", task2.Status)
	}
}

func TestMutateDependencyGraph_环路检测(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "pending", Title: "A"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: "pending", Title: "B"})

	// t1→t2 + t2→t1 = 环路
	edges := []EdgeSpec{
		{TaskID: "t1", DependsOnID: "t2"},
		{TaskID: "t2", DependsOnID: "t1"},
	}
	result := db.MutateDependencyGraph(ctx, "alpha", nil, edges)
	if result.Ok {
		t.Error("环路应导致管线失败")
	}
}

func TestMutateDependencyGraph_taskID冲突(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "pending", Title: "已有"})

	newTasks := []NewTaskSpec{{TaskID: "t1", Title: "冲突"}}
	result := db.MutateDependencyGraph(ctx, "alpha", newTasks, nil)
	if result.Ok {
		t.Error("task_id 冲突应导致管线失败")
	}
}
```

- [ ] **Step 4: 添加终止传播测试**

```go
func TestCompleteTask_终止传播(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "upstream", TeamName: "alpha", Status: "pending", Title: "上游"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "downstream", TeamName: "alpha", Status: "pending", Title: "下游"})

	// downstream 依赖 upstream
	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "downstream", DependsOnID: "upstream"}})

	// downstream 应变为 blocked
	down, _ := db.GetTask(ctx, "downstream")
	if down.Status != "blocked" {
		t.Fatalf("下游应被阻塞: got %q", down.Status)
	}

	// 完成上游 → 下游应解除阻塞
	refreshed, _ := db.CompleteTask(ctx, "upstream")
	if len(refreshed) == 0 {
		t.Error("完成上游应刷新下游任务")
	}

	down, _ = db.GetTask(ctx, "downstream")
	if down.Status != "pending" {
		t.Errorf("上游完成后下游应解除阻塞: got %q", down.Status)
	}
}

func TestCancelTask_终止传播(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "upstream", TeamName: "alpha", Status: "pending", Title: "上游"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "downstream", TeamName: "alpha", Status: "pending", Title: "下游"})

	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "downstream", DependsOnID: "upstream"}})

	refreshed, _ := db.CancelTask(ctx, "upstream")
	if len(refreshed) == 0 {
		t.Error("取消上游应刷新下游任务")
	}

	down, _ := db.GetTask(ctx, "downstream")
	if down.Status != "pending" {
		t.Errorf("取消上游后下游应解除阻塞: got %q", down.Status)
	}
}

func TestCancelAllTasks(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "pending", Title: "任务1"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: "claimed", Assignee: "agent1", Title: "任务2"})

	cancelled, _ := db.CancelAllTasks(ctx, "alpha", nil)
	if len(cancelled) != 2 {
		t.Errorf("应取消2个任务: got %d", len(cancelled))
	}
}

func TestCancelAllTasks_skipAssignees(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "pending", Title: "任务1"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: "claimed", Assignee: "agent1", Title: "任务2"})

	cancelled, _ := db.CancelAllTasks(ctx, "alpha", []string{"agent1"})
	if len(cancelled) != 1 {
		t.Errorf("skipAssignees 后应只取消1个任务: got %d", len(cancelled))
	}
}

func TestVerifyAndFixTaskConsistency(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: "pending", Title: "A"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: "pending", Title: "B"})

	// 手动添加依赖（绕过管线），t2 依赖 t1 但状态仍是 pending（不一致）
	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}})

	refreshed, _ := db.VerifyAndFixTaskConsistency(ctx, "alpha")
	// t2 有未解决依赖应变为 blocked
	if len(refreshed) == 0 {
		t.Error("一致性修复应刷新任务状态")
	}
}
```

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test -v ./internal/agent_teams/tools/database/...`
Expected: 全部测试通过

- [ ] **Step 6: Commit**

```bash
git add internal/agent_teams/tools/database/memory_impl_test.go
git commit -m "feat(9.65a-2): 添加 TaskDao 全部方法测试（CRUD+FSM+管线+终止传播）"
```

---

## Task 7: doc.go 更新 + task_dao.go 清理 + A 阶段完成验证

**Files:**
- Modify: `internal/agent_teams/tools/database/doc.go`
- Modify: `internal/agent_teams/tools/database/task_dao.go`
- Modify: `internal/agent_teams/tools/database/database_test.go`

- [ ] **Step 1: 更新 database/doc.go 文件目录**

更新 doc.go 中的文件目录，反映新增的内容。在描述中更新 TaskDao 相关信息，将 `⤵️ 9.65a-2` 标记改为 `✅ 9.65a-2`。

- [ ] **Step 2: 更新 task_dao.go**

将 task_dao.go 从占位文件改为注释说明文件（实际 TaskDao 方法在 memory_impl.go 中实现）：

```go
package database

// TaskDao 任务 DAO 接口。
// 完整接口定义在 database.go 中，InMemory 实现在 memory_impl.go 中。
// 管线辅助类型（NewTaskSpec/EdgeSpec/GraphMutationResult/mutationContext）定义在 models.go 和 memory_impl.go 中。
```

- [ ] **Step 3: 运行完整编译 + 测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/... && go test -tags test -cover ./internal/agent_teams/tools/database/...`
Expected: 编译成功，覆盖率 ≥ 85%

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/tools/database/doc.go internal/agent_teams/tools/database/task_dao.go
git commit -m "feat(9.65a-2): A 阶段完成——TaskDao 接口+InMemory 实现+管线+测试"
```

---

## Task 8: TeamTaskManager 结构体和基础方法

**Files:**
- Modify: `internal/agent_teams/tools/task_manager.go`
- New: `internal/agent_teams/tools/task_manager_test.go`

- [ ] **Step 1: 替换 TaskManager 接口为具体结构体+方法**

将 `task_manager.go` 中的空接口替换为具体结构体和方法实现。先写辅助数据结构，然后写核心方法：

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskCreateSpec 批量创建任务的输入规范。
type TaskCreateSpec struct {
	Title   string
	Content string
}

// TaskDetail 任务详细视图（含阻塞关系）。
type TaskDetail struct {
	Task      *database.TeamTaskBase
	BlockedBy []*database.TeamTaskBase
	Blocks    []*database.TeamTaskBase
}

// TaskSummary 任务摘要视图（含阻塞信息）。
type TaskSummary struct {
	TaskID    string
	Title     string
	Status    string
	Assignee  string
	BlockedBy []string
}

// PlanRecord 计划记录（index.json 中的一条）。
// 对齐 Python: PlanRecord (openjiuwen/agent_teams/tools/task_manager.py)
type PlanRecord struct {
	PlanID     string `json:"plan_id"`
	TaskID     string `json:"task_id"`
	MemberName string `json:"member_name"`
	Status     string `json:"status"`
	Decision   string `json:"decision"`
	Feedback   string `json:"feedback,omitempty"`
	CreatedAt  int64  `json:"created_at"`
}

// PlanIndex 计划索引（index.json 结构）。
type PlanIndex struct {
	Tasks     map[string]*TaskPlanIndex `json:"tasks"`
	TaskPlans map[string]*PlanRecord    `json:"task_plans"`
}

// TaskPlanIndex 每任务的计划索引。
type TaskPlanIndex struct {
	PlanIDs      []string `json:"plan_ids"`
	LatestPlanID string   `json:"latest_plan_id"`
	Status       string   `json:"status"`
}

// TeamTaskManager 团队任务管理器。
// 对齐 Python: TeamTaskManager (openjiuwen/agent_teams/tools/task_manager.py)
type TeamTaskManager struct {
	// db 团队数据库实例（内含 TaskDao）
	db database.TeamDatabase
	// teamName 团队标识
	teamName string
	// memberName 当前成员标识
	memberName string
	// messager 事件发布器。⤵️ 9.65 回填：需实现以下方法
	//   Publish(topic schema.TeamTopic, event any) error
	messager any
	// plansDir 计划文件存储目录
	plansDir string
	// teamPlanID 团队级计划标识
	teamPlanID string
	// leaderMemberName Leader 成员名（用于通知计划审批）
	leaderMemberName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamTaskManager 创建任务管理器。
func NewTeamTaskManager(db database.TeamDatabase, teamName, memberName string, messager any, plansDir, teamPlanID, leaderMemberName string) *TeamTaskManager {
	return &TeamTaskManager{
		db:               db,
		teamName:         teamName,
		memberName:       memberName,
		messager:         messager,
		plansDir:         plansDir,
		teamPlanID:       teamPlanID,
		leaderMemberName: leaderMemberName,
	}
}

// Add 创建单条任务。对齐 Python: TeamTaskManager.add()
func (tm *TeamTaskManager) Add(ctx context.Context, title, content string) (*database.TeamTaskBase, error) {
	taskID := fmt.Sprintf("task_%s_%d", tm.teamName, time.Now().UnixMilli())
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
	// ⤵️ 9.65 回填：tm.messager.Publish(schema.TeamTopicTask, schema.TaskCreatedEvent{...})
	return task, nil
}

// AddBatch 批量创建任务。对齐 Python: TeamTaskManager.add_batch()
func (tm *TeamTaskManager) AddBatch(ctx context.Context, specs []TaskCreateSpec) ([]*database.TeamTaskBase, error) {
	var tasks []*database.TeamTaskBase
	for _, spec := range specs {
		task, err := tm.Add(ctx, spec.Title, spec.Content)
		if err != nil {
			return tasks, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// Get 按 ID 查任务。对齐 Python: TeamTaskManager.get()
func (tm *TeamTaskManager) Get(ctx context.Context, taskID string) (*database.TeamTaskBase, error) {
	return tm.db.Task().GetTask(ctx, taskID)
}

// ListTasks 列出团队任务。对齐 Python: TeamTaskManager.list_tasks()
func (tm *TeamTaskManager) ListTasks(ctx context.Context, status string) ([]*database.TeamTaskBase, error) {
	return tm.db.Task().GetTeamTasks(ctx, tm.teamName, status)
}

// GetClaimableTasks 获取可认领任务（PENDING + 无未解决依赖）。
// 对齐 Python: TeamTaskManager.get_claimable_tasks()
func (tm *TeamTaskManager) GetClaimableTasks(ctx context.Context) ([]*database.TeamTaskBase, error) {
	pendingTasks, err := tm.db.Task().GetTeamTasks(ctx, tm.teamName, fsm.TaskStatusPending)
	if err != nil {
		return nil, err
	}
	var result []*database.TeamTaskBase
	for _, task := range pendingTasks {
		count, _ := tm.db.Task().GetUnresolvedDependenciesCount(ctx, task.TaskID)
		if count == 0 {
			result = append(result, task)
		}
	}
	return result, nil
}

// GetTasksByAssignee 按成员查任务。对齐 Python: TeamTaskManager.get_tasks_by_assignee()
func (tm *TeamTaskManager) GetTasksByAssignee(ctx context.Context, memberName, status string) ([]*database.TeamTaskBase, error) {
	return tm.db.Task().GetTasksByAssignee(ctx, tm.teamName, memberName, status)
}

// Claim 成员自认领。对齐 Python: TeamTaskManager.claim()
func (tm *TeamTaskManager) Claim(ctx context.Context, taskID string) error {
	ok, err := tm.db.Task().ClaimTask(ctx, taskID, tm.memberName)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("认领任务失败: 任务不存在或状态不允许认领 %s", taskID)
	}
	// ⤵️ 9.65 回填：tm.messager.Publish(schema.TeamTopicTask, schema.TaskClaimedEvent{...})
	return nil
}

// Assign Leader 分配。对齐 Python: TeamTaskManager.assign()
func (tm *TeamTaskManager) Assign(ctx context.Context, taskID, assignee string) error {
	ok, err := tm.db.Task().ClaimTask(ctx, taskID, assignee)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("分配任务失败: 任务不存在或状态不允许分配 %s", taskID)
	}
	// ⤵️ 9.65 回填：tm.messager.Publish(schema.TeamTopicTask, schema.TaskClaimedEvent{...})
	return nil
}

// Complete 完成任务。对齐 Python: TeamTaskManager.complete()
func (tm *TeamTaskManager) Complete(ctx context.Context, taskID string) ([]string, error) {
	refreshed, err := tm.db.Task().CompleteTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if refreshed == nil {
		return nil, fmt.Errorf("完成任务失败: 任务不存在或状态不允许完成 %s", taskID)
	}
	// ⤵️ 9.65 回填：tm.messager.Publish(schema.TeamTopicTask, schema.TaskCompletedEvent{...})
	// ⤵️ 9.65 回填：逐条发布 TaskUnblockedEvent
	// ⤵️ 9.65 回填：检查 TaskListDrainedEvent
	return refreshed, nil
}

// Cancel 取消单条任务。对齐 Python: TeamTaskManager.cancel()
func (tm *TeamTaskManager) Cancel(ctx context.Context, taskID string) ([]string, error) {
	refreshed, err := tm.db.Task().CancelTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if refreshed == nil {
		return nil, fmt.Errorf("取消任务失败: 任务不存在或状态不允许取消 %s", taskID)
	}
	// ⤵️ 9.65 回填：tm.messager.Publish(schema.TeamTopicTask, schema.TaskCancelledEvent{...})
	// ⤵️ 9.65 回填：逐条发布 TaskUnblockedEvent
	// ⤵️ 9.65 回填：检查 TaskListDrainedEvent
	return refreshed, nil
}

// CancelAllTasks 批量取消。对齐 Python: TeamTaskManager.cancel_all_tasks()
func (tm *TeamTaskManager) CancelAllTasks(ctx context.Context, skipAssignees []string) ([]*database.TeamTaskBase, error) {
	cancelled, err := tm.db.Task().CancelAllTasks(ctx, tm.teamName, skipAssignees)
	if err != nil {
		return nil, err
	}
	// ⤵️ 9.65 回填：逐条发布 TaskCancelledEvent
	// ⤵️ 9.65 回填：检查 TaskListDrainedEvent
	return cancelled, nil
}

// Reset 重置任务（CLAIMED→PENDING）。对齐 Python: TeamTaskManager.reset()
func (tm *TeamTaskManager) Reset(ctx context.Context, taskID string) error {
	ok, err := tm.db.Task().ResetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("重置任务失败: 任务不存在或状态不允许重置 %s", taskID)
	}
	return nil
}

// UpdateTask 更新标题/内容。对齐 Python: TeamTaskManager.update_task()
func (tm *TeamTaskManager) UpdateTask(ctx context.Context, taskID, title, content string) error {
	ok, err := tm.db.Task().UpdateTask(ctx, taskID, title, content)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("更新任务失败: 任务不存在或状态不允许编辑 %s", taskID)
	}
	// ⤵️ 9.65 回填：tm.messager.Publish(schema.TeamTopicTask, schema.TaskUpdatedEvent{...})
	return nil
}

// AddWithPriority 带双向依赖创建任务。对齐 Python: TeamTaskManager.add_with_priority()
func (tm *TeamTaskManager) AddWithPriority(ctx context.Context, taskID, title, content string, dependsOnIDs []string, newTasksSpec []database.NewTaskSpec) (database.GraphMutationResult, error) {
	task := &database.TeamTaskBase{
		TaskID:   taskID,
		TeamName: tm.teamName,
		Title:    title,
		Content:  content,
		Status:   fsm.TaskStatusPending,
	}
	result := tm.db.Task().AddTaskWithBidirectionalDependencies(ctx, tm.teamName, task, dependsOnIDs)
	if !result.Ok {
		return result, fmt.Errorf("带依赖创建任务失败: %s", result.Reason)
	}
	// ⤵️ 9.65 回填：tm.messager.Publish(schema.TeamTopicTask, schema.TaskCreatedEvent{...})
	return result, nil
}

// AddAsTopPriority 最高优先级插入。对齐 Python: TeamTaskManager.add_as_top_priority()
func (tm *TeamTaskManager) AddAsTopPriority(ctx context.Context, title, content string) (*database.TeamTaskBase, error) {
	taskID := fmt.Sprintf("task_%s_%d_priority", tm.teamName, time.Now().UnixMilli())
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
		return nil, fmt.Errorf("创建最高优先级任务失败: task_id 冲突 %s", taskID)
	}

	// 阻塞所有 PENDING 任务：每个 PENDING 任务依赖新任务
	pendingTasks, _ := tm.db.Task().GetTeamTasks(ctx, tm.teamName, fsm.TaskStatusPending)
	var edges []database.EdgeSpec
	for _, existing := range pendingTasks {
		if existing.TaskID == taskID {
			continue // 跳过自己
		}
		edges = append(edges, database.EdgeSpec{TaskID: existing.TaskID, DependsOnID: taskID})
	}

	if len(edges) > 0 {
		result := tm.db.Task().MutateDependencyGraph(ctx, tm.teamName, nil, edges)
		if !result.Ok {
			return task, fmt.Errorf("添加阻塞依赖失败: %s", result.Reason)
		}
	}

	// ⤵️ 9.65 回填：tm.messager.Publish(schema.TeamTopicTask, schema.TaskCreatedEvent{...})
	return task, nil
}

// AddDependencies 向已有任务添加依赖。对齐 Python: TeamTaskManager.add_dependencies()
func (tm *TeamTaskManager) AddDependencies(ctx context.Context, taskID string, dependsOnIDs []string) (database.GraphMutationResult, error) {
	var edges []database.EdgeSpec
	for _, upstreamID := range dependsOnIDs {
		edges = append(edges, database.EdgeSpec{TaskID: taskID, DependsOnID: upstreamID})
	}
	result := tm.db.Task().MutateDependencyGraph(ctx, tm.teamName, nil, edges)
	if !result.Ok {
		return result, fmt.Errorf("添加依赖失败: %s", result.Reason)
	}
	return result, nil
}

// GetTaskDetail 详细视图（含 blocked_by + blocks）。
// 对齐 Python: TeamTaskManager.get_task_detail()
func (tm *TeamTaskManager) GetTaskDetail(ctx context.Context, taskID string) (*TaskDetail, error) {
	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, nil
	}

	// blocked_by：task 的上游依赖
	deps, _ := tm.db.Task().GetTaskDependencies(ctx, taskID)
	var blockedBy []*database.TeamTaskBase
	for _, dep := range deps {
		upstream, _ := tm.db.Task().GetTask(ctx, dep.DependsOnID)
		if upstream != nil {
			blockedBy = append(blockedBy, upstream)
		}
	}

	// blocks：task 的下游依赖
	downstream, _ := tm.db.Task().GetTasksDependingOn(ctx, taskID)
	return &TaskDetail{
		Task:      task,
		BlockedBy: blockedBy,
		Blocks:    downstream,
	}, nil
}

// ListTasksWithDeps 摘要视图。对齐 Python: TeamTaskManager.list_tasks_with_deps()
func (tm *TeamTaskManager) ListTasksWithDeps(ctx context.Context) ([]*TaskSummary, error) {
	tasks, err := tm.db.Task().GetTeamTasks(ctx, tm.teamName, "")
	if err != nil {
		return nil, err
	}
	var result []*TaskSummary
	for _, task := range tasks {
		deps, _ := tm.db.Task().GetTaskDependencies(ctx, task.TaskID)
		var blockedBy []string
		for _, dep := range deps {
			blockedBy = append(blockedBy, dep.DependsOnID)
		}
		result = append(result, &TaskSummary{
			TaskID:    task.TaskID,
			Title:     task.Title,
			Status:    task.Status,
			Assignee:  task.Assignee,
			BlockedBy: blockedBy,
		})
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// checkTaskListDrained 检查所有任务是否均处于终态。
// 对齐 Python: TeamTaskManager._check_task_list_drained()
func (tm *TeamTaskManager) checkTaskListDrained(ctx context.Context) bool {
	tasks, err := tm.db.Task().GetTeamTasks(ctx, tm.teamName, "")
	if err != nil || len(tasks) == 0 {
		return false
	}
	for _, t := range tasks {
		if t.Status != fsm.TaskStatusCompleted && t.Status != fsm.TaskStatusCancelled {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: 编写 TaskManager 基础方法测试**

在 `internal/agent_teams/tools/task_manager_test.go` 中：

```go
package tools

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

func setupTestTaskManager() (*TeamTaskManager, *database.InMemoryTeamDatabase) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	tm := NewTeamTaskManager(db, "alpha", "agent1", nil, "", "", "leader1")
	db.Initialize(ctx)
	return tm, db
}

func TestTaskManager_Add(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, err := tm.Add(ctx, "任务标题", "任务内容")
	if err != nil {
		t.Fatalf("Add 返回错误: %v", err)
	}
	if task.Status != fsm.TaskStatusPending {
		t.Errorf("新任务状态应为 pending: got %q", task.Status)
	}
}

func TestTaskManager_Claim(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "内容")
	err := tm.Claim(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("Claim 返回错误: %v", err)
	}
	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusClaimed {
		t.Errorf("认领后状态应为 claimed: got %q", got.Status)
	}
	if got.Assignee != "agent1" {
		t.Errorf("认领人应为 agent1: got %q", got.Assignee)
	}
}

func TestTaskManager_Complete(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "内容")
	tm.Claim(ctx, task.TaskID)
	refreshed, err := tm.Complete(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("Complete 返回错误: %v", err)
	}
	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusCompleted {
		t.Errorf("完成后状态应为 completed: got %q", got.Status)
	}
}

func TestTaskManager_Cancel(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "内容")
	_, err := tm.Cancel(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("Cancel 返回错误: %v", err)
	}
	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusCancelled {
		t.Errorf("取消后状态应为 cancelled: got %q", got.Status)
	}
}

func TestTaskManager_CancelAllTasks(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	tm.Add(ctx, "任务1", "")
	tm.Add(ctx, "任务2", "")

	cancelled, err := tm.CancelAllTasks(ctx, nil)
	if err != nil {
		t.Fatalf("CancelAllTasks 返回错误: %v", err)
	}
	if len(cancelled) != 2 {
		t.Errorf("应取消2个任务: got %d", len(cancelled))
	}
}

func TestTaskManager_GetClaimableTasks(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	tm.Add(ctx, "任务1", "")
	tm.Add(ctx, "任务2", "")
	// 认领一个
	tasks, _ := tm.ListTasks(ctx, fsm.TaskStatusPending)
	tm.Claim(ctx, tasks[0].TaskID)

	claimable, _ := tm.GetClaimableTasks(ctx)
	if len(claimable) != 1 {
		t.Errorf("可认领任务应为1: got %d", len(claimable))
	}
}

func TestTaskManager_ListTasksWithDeps(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	tm.Add(ctx, "上游任务", "")
	tasks, _ := tm.ListTasks(ctx, "")
	tm.AddWithPriority(ctx, "下游任务", "依赖上游", []string{tasks[0].TaskID}, nil)

	summaries, err := tm.ListTasksWithDeps(ctx)
	if err != nil {
		t.Fatalf("ListTasksWithDeps 返回错误: %v", err)
	}
	if len(summaries) != 2 {
		t.Errorf("摘要数量应为2: got %d", len(summaries))
	}
}

func TestTaskManager_AddAsTopPriority(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	tm.Add(ctx, "已有任务", "")

	priorityTask, err := tm.AddAsTopPriority(ctx, "最高优先级", "")
	if err != nil {
		t.Fatalf("AddAsTopPriority 返回错误: %v", err)
	}
	if priorityTask.Status != fsm.TaskStatusPending {
		t.Errorf("最高优先级任务状态应为 pending: got %q", priorityTask.Status)
	}

	// 已有任务应被阻塞
	allTasks, _ := tm.ListTasks(ctx, fsm.TaskStatusBlocked)
	if len(allTasks) == 0 {
		t.Error("已有任务应被最高优先级任务阻塞")
	}
}

func TestTaskManager_Reset(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	tm.Claim(ctx, task.TaskID)

	err := tm.Reset(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("Reset 返回错误: %v", err)
	}
	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusPending {
		t.Errorf("重置后状态应为 pending: got %q", got.Status)
	}
}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/tools/...`

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test -v ./internal/agent_teams/tools/...`

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/tools/task_manager.go internal/agent_teams/tools/task_manager_test.go
git commit -m "feat(9.65a-2): TeamTaskManager 结构体和基础方法实现"
```

---

## Task 9: PLAN_MODE（SubmitPlan + ApprovePlan）

**Files:**
- Modify: `internal/agent_teams/tools/task_manager.go`

- [ ] **Step 1: 实现 SubmitPlan 方法**

在 `task_manager.go` 导出函数区域末尾添加：

```go
// SubmitPlan PLAN_MODE 提交计划。对齐 Python: TeamTaskManager.submit_plan()
// ⤵️ 9.65 回填 messager 通知逻辑
func (tm *TeamTaskManager) SubmitPlan(ctx context.Context, taskID, planFilePath, toolCallID string) (*PlanRecord, error) {
	// 1. 校验成员是否为 PLAN_MODE
	member, _ := tm.db.Member().GetMember(ctx, tm.memberName, tm.teamName)
	if member == nil || member.Mode != "plan_mode" {
		return nil, fmt.Errorf("成员 %s 不是 PLAN_MODE", tm.memberName)
	}

	// 2. 校验任务状态
	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("任务不存在: %s", taskID)
	}
	if task.Status != fsm.TaskStatusPending && task.Status != fsm.TaskStatusClaimed {
		return nil, fmt.Errorf("任务状态不允许提交计划: %s (当前: %s)", taskID, task.Status)
	}
	if task.Status == fsm.TaskStatusClaimed && task.Assignee != tm.memberName {
		return nil, fmt.Errorf("任务 %s 已被 %s 认领，当前成员无法提交计划", taskID, task.Assignee)
	}

	// 3. 如果 PENDING → 先 claim
	if task.Status == fsm.TaskStatusPending {
		ok, _ := tm.db.Task().ClaimTask(ctx, taskID, tm.memberName)
		if !ok {
			return nil, fmt.Errorf("提交计划前认领任务失败: %s", taskID)
		}
	}

	// 4. 生成 plan_id
	planID := fmt.Sprintf("plan_%s_%d", taskID, time.Now().UnixMilli())

	// 5. 拷贝 plan 文件到 plansDir
	planDir := filepath.Join(tm.plansDir, tm.teamPlanID, "tasks", taskID, "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建计划目录失败: %v", err)
	}
	destPath := filepath.Join(planDir, planID+".md")
	if planFilePath != "" {
		content, err := os.ReadFile(planFilePath)
		if err != nil {
			return nil, fmt.Errorf("读取计划文件失败: %v", err)
		}
		if err := os.WriteFile(destPath, content, 0o644); err != nil {
			return nil, fmt.Errorf("写入计划文件失败: %v", err)
		}
	}

	// 6. 写入 index.json
	record := &PlanRecord{
		PlanID:     planID,
		TaskID:     taskID,
		MemberName: tm.memberName,
		Status:     fsm.TaskStatusClaimed,
		Decision:   "pending",
		CreatedAt:  time.Now().UnixMilli(),
	}
	if err := tm.writePlanIndex(record); err != nil {
		return nil, fmt.Errorf("写入计划索引失败: %v", err)
	}

	// 7. 发布事件（⤵️ 9.65 回填）
	// tm.messager.Publish(schema.TeamTopicTask, schema.TaskPlanRequestEvent{
	//   TeamName: tm.teamName, TaskID: taskID, PlanID: planID,
	//   MemberPlanMD: destPath, ToolCallID: toolCallID,
	// })

	return record, nil
}

// ApprovePlan PLAN_MODE 审批/拒绝计划。对齐 Python: TeamTaskManager.approve_plan()
// ⤵️ 9.65 回填 messager 通知逻辑
func (tm *TeamTaskManager) ApprovePlan(ctx context.Context, planID string, approved bool, feedback string) error {
	// 1. 加载 index.json
	index, err := tm.loadPlanIndex()
	if err != nil {
		return fmt.Errorf("加载计划索引失败: %v", err)
	}

	// 2. 校验 planID 存在
	planRecord, planExists := index.TaskPlans[planID]
	if !planExists {
		return fmt.Errorf("计划不存在: %s", planID)
	}

	// 3. 校验 task 状态
	task, _ := tm.db.Task().GetTask(ctx, planRecord.TaskID)
	if task == nil {
		return fmt.Errorf("任务不存在: %s", planRecord.TaskID)
	}
	if task.Status != fsm.TaskStatusClaimed {
		return fmt.Errorf("任务状态应为 CLAIMED: 当前 %s", task.Status)
	}

	// 4. 校验 planID 是 latest_plan_id
	taskPlanIdx, idxExists := index.Tasks[planRecord.TaskID]
	if idxExists && taskPlanIdx.LatestPlanID != planID {
		return fmt.Errorf("不能审批过期计划: latest=%s, current=%s", taskPlanIdx.LatestPlanID, planID)
	}

	// 5. 校验 decision 为 pending
	if planRecord.Decision != "pending" {
		return fmt.Errorf("计划已审批: decision=%s", planRecord.Decision)
	}

	// 6. 校验 plan 文件物理存在
	planDir := filepath.Join(tm.plansDir, tm.teamPlanID, "tasks", planRecord.TaskID, "plans")
	planFile := filepath.Join(planDir, planID+".md")
	if _, err := os.Stat(planFile); os.IsNotExist(err) {
		return fmt.Errorf("计划文件不存在: %s", planFile)
	}

	if approved {
		// 审批通过：CLAIMED→PLAN_APPROVED
		ok, _ := tm.db.Task().ApprovePlanTask(ctx, planRecord.TaskID)
		if !ok {
			return fmt.Errorf("审批 FSM 转换失败: %s", planRecord.TaskID)
		}
		planRecord.Decision = "approve"
		planRecord.Status = fsm.TaskStatusPlanApproved
	} else {
		// 审批拒绝：任务保持 CLAIMED
		planRecord.Decision = "reject"
		planRecord.Feedback = feedback
		planRecord.Status = fsm.TaskStatusClaimed
	}

	// 更新 index.json
	if err := tm.updatePlanIndex(planID, planRecord); err != nil {
		return fmt.Errorf("更新计划索引失败: %v", err)
	}

	// 发布事件（⤵️ 9.65 回填）
	// tm.messager.Publish(schema.TeamTopicTask, schema.TaskPlanResponseEvent{
	//   TeamName: tm.teamName, TaskID: planRecord.TaskID,
	//   Approved: approved, PlanID: planID, Feedback: feedback,
	// })

	return nil
}
```

- [ ] **Step 2: 添加 plan 文件 IO 辅助方法**

在非导出函数区域添加：

```go
// writePlanIndex 写入或创建 index.json。
func (tm *TeamTaskManager) writePlanIndex(record *PlanRecord) error {
 indexPath := filepath.Join(tm.plansDir, tm.teamPlanID, "index.json")
 index, err := tm.loadPlanIndex()
 if err != nil {
	 index = &PlanIndex{
		 Tasks:     make(map[string]*TaskPlanIndex),
		 TaskPlans: make(map[string]*PlanRecord),
	 }
 }

 // 更新 task_plans
 index.TaskPlans[record.PlanID] = record

 // 更新 tasks
 taskIdx, exists := index.Tasks[record.TaskID]
 if !exists {
	 taskIdx = &TaskPlanIndex{PlanIDs: []string{}, Status: record.Status}
	 index.Tasks[record.TaskID] = taskIdx
 }
 taskIdx.PlanIDs = append(taskIdx.PlanIDs, record.PlanID)
 taskIdx.LatestPlanID = record.PlanID
 taskIdx.Status = record.Status

 data, err := json.MarshalIndent(index, "", "  ")
 if err != nil {
	 return err
	}
	return os.WriteFile(indexPath, data, 0o644)
}

// updatePlanIndex 更新已有 plan 记录。
func (tm *TeamTaskManager) updatePlanIndex(planID string, record *PlanRecord) error {
	indexPath := filepath.Join(tm.plansDir, tm.teamPlanID, "index.json")
	index, err := tm.loadPlanIndex()
	if err != nil {
		return err
	}

	// 更新 task_plans
	index.TaskPlans[planID] = record

	// 更新 tasks 的 status
	taskIdx, exists := index.Tasks[record.TaskID]
	if exists {
		taskIdx.Status = record.Status
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath, data, 0o644)
}

// loadPlanIndex 加载 index.json。
func (tm *TeamTaskManager) loadPlanIndex() (*PlanIndex, error) {
	indexPath := filepath.Join(tm.plansDir, tm.teamPlanID, "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	var index PlanIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}
	return &index, nil
}
```

- [ ] **Step 3: 添加 PLAN_MODE 测试**

```go
func TestTaskManager_SubmitPlan(t *testing.T) {
	tm, db := setupTestTaskManager()
	ctx := context.Background()

	// 设置成员为 plan_mode
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "plan_mode", "", "")

	// 创建临时 plansDir
	plansDir := t.TempDir()
	tm.plansDir = plansDir
	tm.teamPlanID = "plan_session_1"

	// 创建临时 plan 文件
	planFile := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(planFile, []byte("# 执行计划\n完成数据分析"), 0o644)

	// 创建任务
	task, _ := tm.Add(ctx, "数据分析", "完成数据分析任务")

	// 提交计划
	record, err := tm.SubmitPlan(ctx, task.TaskID, planFile, "call_123")
	if err != nil {
		t.Fatalf("SubmitPlan 返回错误: %v", err)
	}
	if record.PlanID == "" {
		t.Error("PlanID 应非空")
	}
	if record.Decision != "pending" {
		t.Errorf("Decision 应为 pending: got %q", record.Decision)
	}

	// 任务应变为 claimed
	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusClaimed {
		t.Errorf("提交计划后任务应为 claimed: got %q", got.Status)
	}
}

func TestTaskManager_ApprovePlan_通过(t *testing.T) {
	tm, db := setupTestTaskManager()
	ctx := context.Background()

	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "plan_mode", "", "")

	plansDir := t.TempDir()
	tm.plansDir = plansDir
	tm.teamPlanID = "plan_session_1"

	planFile := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(planFile, []byte("# 执行计划"), 0o644)

	task, _ := tm.Add(ctx, "数据分析", "")
	record, _ := tm.SubmitPlan(ctx, task.TaskID, planFile, "call_123")

	// 审批通过
	err := tm.ApprovePlan(ctx, record.PlanID, true, "")
	if err != nil {
		t.Fatalf("ApprovePlan 返回错误: %v", err)
	}

	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusPlanApproved {
		t.Errorf("审批通过后任务应为 plan_approved: got %q", got.Status)
	}
}

func TestTaskManager_ApprovePlan_拒绝(t *testing.T) {
	tm, db := setupTestTaskManager()
	ctx := context.Background()

	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "plan_mode", "", "")

	plansDir := t.TempDir()
	tm.plansDir = plansDir
	tm.teamPlanID = "plan_session_1"

	planFile := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(planFile, []byte("# 执行计划"), 0o644)

	task, _ := tm.Add(ctx, "数据分析", "")
	record, _ := tm.SubmitPlan(ctx, task.TaskID, planFile, "call_123")

	// 审批拒绝
	err := tm.ApprovePlan(ctx, record.PlanID, false, "需要更多细节")
	if err != nil {
		t.Fatalf("ApprovePlan 返回错误: %v", err)
	}

	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusClaimed {
		t.Errorf("审批拒绝后任务应保持 claimed: got %q", got.Status)
	}
}
```

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go test -tags test -v ./internal/agent_teams/tools/...`

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/tools/task_manager.go internal/agent_teams/tools/task_manager_test.go
git commit -m "feat(9.65a-2): PLAN_MODE SubmitPlan+ApprovePlan 完整实现"
```

---

## Task 10: 回填 infra.go 类型化 + doc.go 更新 + B 阶段完成验证

**Files:**
- Modify: `internal/agent_teams/agent/infra.go:22`
- Modify: `internal/agent_teams/tools/doc.go`

- [ ] **Step 1: 类型化 infra.go 中 TaskManager 字段**

将 `infra.go` L22 的 `TaskManager any` 替换为 `TaskManager tools.TeamTaskManager`。需要在 infra.go 中添加 import `"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"`。

```go
// TaskManager 任务管理器（概念上从 TeamBackend 派生，显式保留以便测试注入）
TaskManager tools.TeamTaskManager
```

- [ ] **Step 2: 更新 tools/doc.go 文件目录**

更新 doc.go 中 task_manager.go 的描述，将 `⤵️ 9.65a-2` 标记更新。

- [ ] **Step 3: 运行完整编译 + 测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/agent_teams/... && go test -tags test -cover ./internal/agent_teams/tools/database/... ./internal/agent_teams/tools/...`
Expected: 编译成功，覆盖率 ≥ 85%

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/agent/infra.go internal/agent_teams/tools/doc.go
git commit -m "feat(9.65a-2): B 阶段完成——TaskManager 类型化+doc 更新"
```

---

## Task 11: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:602`

- [ ] **Step 1: 更新 9.65a-2 行状态为 ✅**

将 `9.65a-2 | ☐` 改为 `9.65a-2 | ✅`，描述更新为实际实现内容。

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 9.65a-2 TaskDao+TaskManager 实现完成"
```
