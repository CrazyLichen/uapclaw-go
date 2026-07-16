package schema

// ──────────────────────────── 结构体 ────────────────────────────

// TaskOpResult 任务变更操作结果，保留失败原因。
// 对齐 Python: TaskOpResult (openjiuwen/agent_teams/schema/task.py)
// TaskManager 写路径方法返回此类型而非裸 bool，以便工具包装器将真实原因反馈给 LLM。
type TaskOpResult struct {
	// OK 操作是否成功
	OK bool
	// Reason 失败原因
	Reason string
}

// TaskCreateResult 任务创建操作结果。
// 对齐 Python: TaskCreateResult (openjiuwen/agent_teams/schema/task.py)
// 成功时携带创建的 task，失败时携带可读的 reason。
// Task 字段类型为 any 以避免跨包循环导入（models → schema）。
type TaskCreateResult struct {
	// Task 创建的任务对象，nil 表示创建失败
	Task any
	// Reason 失败原因
	Reason string
}

// TaskSummary 任务摘要信息，用于 list/claimable 操作的返回。
// 对齐 Python: TaskSummary (openjiuwen/agent_teams/schema/task.py)
type TaskSummary struct {
	// TaskID 任务唯一标识
	TaskID string `json:"task_id"`
	// Title 任务标题
	Title string `json:"title"`
	// Status 任务状态
	Status string `json:"status"`
	// Assignee 任务认领者，nil 表示未被认领
	Assignee *string `json:"assignee,omitempty"`
	// BlockedBy 阻塞当前任务的依赖任务 ID 列表
	BlockedBy []string `json:"blocked_by"`
}

// TaskDetail 任务完整详情，用于 get 操作的返回。
// 对齐 Python: TaskDetail (openjiuwen/agent_teams/schema/task.py)
// UpdatedAt 是最近一次状态变更的毫秒级墙钟时间戳，其语义取决于 Status：
// status=claimed 时为认领时间，status=completed 时为完成时间，以此类推。
type TaskDetail struct {
	// TaskID 任务唯一标识
	TaskID string `json:"task_id"`
	// Title 任务标题
	Title string `json:"title"`
	// Content 任务内容描述
	Content string `json:"content"`
	// Status 任务状态
	Status string `json:"status"`
	// Assignee 任务认领者，nil 表示未被认领
	Assignee *string `json:"assignee,omitempty"`
	// BlockedBy 阻塞当前任务的依赖任务 ID 列表
	BlockedBy []string `json:"blocked_by"`
	// Blocks 当前任务阻塞的下游任务 ID 列表
	Blocks []string `json:"blocks"`
	// UpdatedAt 最近一次状态变更的毫秒时间戳
	UpdatedAt *int64 `json:"updated_at,omitempty"`
}

// TaskListResult 任务列表查询结果。
// 对齐 Python: TaskListResult (openjiuwen/agent_teams/schema/task.py)
type TaskListResult struct {
	// Tasks 任务摘要列表
	Tasks []TaskSummary `json:"tasks"`
	// Count 任务数量
	Count int `json:"count"`
}

// NewTaskSpec 通过 mutate_dependency_graph 创建的任务规格。
// 对齐 Python: NewTaskSpec (openjiuwen/agent_teams/schema/task.py)
// 边通过 add_edges 参数单独传入，以便单次原子变更可同时插入节点和连线。
// InitialStatus 是调用方提供的种子状态，变更后的刷新过程可能会根据结果边集在
// PENDING 和 BLOCKED 之间翻转。
type NewTaskSpec struct {
	// TaskID 任务唯一标识
	TaskID string `json:"task_id"`
	// Title 任务标题
	Title string `json:"title"`
	// Content 任务内容描述
	Content string `json:"content"`
	// InitialStatus 初始状态种子
	InitialStatus string `json:"initial_status"`
}

// GraphMutationResult 依赖图变更操作结果。
// 对齐 Python: GraphMutationResult (openjiuwen/agent_teams/schema/task.py)
// 失败时 Reason 携带可读原因（环路、缺失端点、终态目标等）。
// RefreshedTasks 包含变更后刷新过程中状态被翻转的任务。
type GraphMutationResult struct {
	// OK 操作是否成功
	OK bool
	// Reason 失败原因
	Reason string
	// RefreshedTasks 变更后刷新过程中状态被翻转的任务列表
	RefreshedTasks []any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Success 创建一个成功的 TaskOpResult。
// 对齐 Python: TaskOpResult.success()
func (TaskOpResult) Success() TaskOpResult {
	return TaskOpResult{OK: true}
}

// Fail 创建一个失败的 TaskOpResult。
// 对齐 Python: TaskOpResult.fail(reason)
func (TaskOpResult) Fail(reason string) TaskOpResult {
	return TaskOpResult{OK: false, Reason: reason}
}

// OK 判断 TaskCreateResult 是否创建成功（Task 不为 nil）。
// 对齐 Python: TaskCreateResult.ok
func (r TaskCreateResult) OK() bool {
	return r.Task != nil
}

// CreateSuccess 创建一个成功的 TaskCreateResult。
// 对齐 Python: TaskCreateResult.success(task)
func (TaskCreateResult) CreateSuccess(task any) TaskCreateResult {
	return TaskCreateResult{Task: task}
}

// CreateFail 创建一个失败的 TaskCreateResult。
// 对齐 Python: TaskCreateResult.fail(reason)
func (TaskCreateResult) CreateFail(reason string) TaskCreateResult {
	return TaskCreateResult{Reason: reason}
}

// GraphSuccess 创建一个成功的 GraphMutationResult。
// 对齐 Python: GraphMutationResult.success(refreshed_tasks)
func (GraphMutationResult) GraphSuccess(refreshedTasks ...any) GraphMutationResult {
	return GraphMutationResult{OK: true, RefreshedTasks: refreshedTasks}
}

// GraphFail 创建一个失败的 GraphMutationResult。
// 对齐 Python: GraphMutationResult.fail(reason)
func (GraphMutationResult) GraphFail(reason string) GraphMutationResult {
	return GraphMutationResult{OK: false, Reason: reason}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
