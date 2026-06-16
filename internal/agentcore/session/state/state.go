package state

// ──────────────────────────── 结构体 ────────────────────────────

// ReadableStateLike 只读状态访问接口
// 对应 Python: ReadableStateLike
type ReadableStateLike interface {
	// Get 根据 key 获取状态值
	Get(key StateKey) any
	// GetByPrefix 根据 key 和嵌套前缀获取状态值
	GetByPrefix(key StateKey, nestedPrefix string) any
}

// RecoverableStateLike 可恢复状态接口，支持快照保存和恢复
// 对应 Python: RecoverableStateLike
type RecoverableStateLike interface {
	// GetState 获取完整状态快照
	GetState() map[string]any
	// SetState 从快照恢复状态
	SetState(state map[string]any)
}

// StateLike 可读写状态接口，组合只读和可恢复能力
// 对应 Python: StateLike(ReadableStateLike, RecoverableStateLike)
type StateLike interface {
	ReadableStateLike
	RecoverableStateLike
	// Update 更新状态数据
	Update(data map[string]any) error
	// GetByTransformer 通过转换函数获取状态值
	GetByTransformer(transformer Transformer) any
}

// CommitStateLike 事务性状态接口，支持按节点 ID 的提交/回滚
// 对应 Python: CommitStateLike(StateLike)
type CommitStateLike interface {
	StateLike
	// UpdateByID 按节点 ID 暂存更新
	// nodeID 为空字符串时返回 error
	UpdateByID(nodeID string, data map[string]any) error
	// Commit 提交指定节点（或全部）的暂存更新
	// 不传 nodeID 则提交所有节点
	Commit(nodeID ...string)
	// Rollback 回滚指定节点的暂存更新
	Rollback(nodeID string)
	// GetUpdates 获取所有暂存更新
	GetUpdates() map[string][]map[string]any
	// SetUpdates 设置暂存更新
	SetUpdates(updates map[string][]map[string]any)
}

// SessionState 会话状态接口，面向会话调用方的统一抽象
// 对应 Python: State(RecoverableStateLike)
//
// 提供基础的状态读写能力：GetGlobal/UpdateGlobal/UpdateTrace/Update/Get/Dump。
// Workflow 专属方法（CommitCmp/CreateNodeState/Rollback 等）定义在独立的 WorkflowState
// 接口中，消费方通过类型断言 `if ws, ok := st.(WorkflowState); ok` 判断是否支持。
// 类型断言失败时对齐 Python AttributeError：Log Error + Panic。
type SessionState interface {
	RecoverableStateLike
	// GetGlobal 从全局状态获取值
	GetGlobal(key StateKey) any
	// UpdateGlobal 更新全局状态
	UpdateGlobal(data map[string]any)
	// UpdateTrace 更新追踪状态
	UpdateTrace(span any)
	// Update 更新状态数据
	Update(data map[string]any) error
	// Get 根据 key 获取状态值
	Get(key StateKey) any
	// Dump 导出完整状态快照
	Dump() map[string]any
}

// WorkflowState 工作流状态接口，定义 Workflow 专属的提交/回滚/IO 操作。
// 对应 Python: CommitState（继承自 StateCollection 继承自 State）。
//
// Python 中通过继承链直接调用，Go 中通过类型断言获取：
//
//	if ws, ok := session.State().(state.WorkflowState); ok {
//	    ws.CommitCmp()
//	} else {
//	    // 对齐 Python AttributeError — Log Error + Panic
//	}
//
// 只有 WorkflowCommitState 实现此接口；
// AgentStateCollection/InMemoryStateLike/InMemoryCommitState/WorkflowStateCollection 不实现。
type WorkflowState interface {
	// CommitCmp 提交当前节点的 comp_state 和 io_state 暂存更新
	// 对齐 Python StateCollection.commit_cmp()
	CommitCmp()
	// CreateNodeState 创建节点专属状态视图
	// 对齐 Python CommitState.create_node_state()
	CreateNodeState(executableID, parentID string) SessionState
	// GetWorkflowState 从工作流状态获取值
	// 对齐 Python CommitState.get_workflow_state()
	GetWorkflowState(key StateKey) any
	// UpdateAndCommitWorkflowState 立即更新并提交工作流状态
	// 对齐 Python CommitState.update_and_commit_workflow_state()
	UpdateAndCommitWorkflowState(data map[string]any)
	// Commit 提交所有子状态的全部暂存（无参数，对齐 Python CommitState.commit()）
	Commit()
	// Rollback 回滚当前节点的暂存更新（无参数，对齐 Python CommitState.rollback()）
	Rollback()
	// GetInputs 从 io_state 查询输入
	// 对齐 Python CommitState.get_inputs()
	GetInputs(schema StateKey) any
	// GetInputsByTransformer 通过转换函数获取输入
	// 对齐 Python CommitState.get_inputs_by_transformer()
	GetInputsByTransformer(transformer Transformer) any
	// SetOutputs 向 io_state 写入当前节点的输出
	// 对齐 Python CommitState.set_outputs()
	SetOutputs(data map[string]any)
	// GetOutputs 从 io_state 查询指定节点的输出
	// 对齐 Python CommitState.get_outputs()
	GetOutputs(nodeID ...string) any
}

// ──────────────────────────── 枚举 ────────────────────────────

// 以下别名保持向后兼容，后续版本移除

// ReadableState 只读状态访问接口别名（向后兼容）
type ReadableState = ReadableStateLike

// RecoverableState 可恢复状态接口别名（向后兼容）
type RecoverableState = RecoverableStateLike

// State 可读写状态接口别名（向后兼容）
type State = StateLike

// CommitState 事务性状态接口别名（向后兼容）
type CommitState = CommitStateLike

// Transformer 状态转换函数，接受只读状态视图返回任意值
type Transformer func(readable ReadableStateLike) any

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultNodeID 默认节点 ID
	DefaultNodeID = "default"
	// DefaultWorkflowID 默认工作流 ID
	DefaultWorkflowID = "workflow"
	// IOStateKey IO 状态键
	IOStateKey = "io_state"
	// GlobalStateKey 全局状态键
	GlobalStateKey = "global_state"
	// CompStateKey 组件状态键
	CompStateKey = "comp_state"
	// WorkflowStateKey 工作流状态键
	WorkflowStateKey = "workflow_state"
	// AgentStateKey Agent 状态键
	AgentStateKey = "agent_state"
	// IOStateUpdatesKey IO 状态更新键
	IOStateUpdatesKey = "io_state_updates"
	// GlobalStateUpdatesKey 全局状态更新键
	GlobalStateUpdatesKey = "global_state_updates"
	// CompStateUpdatesKey 组件状态更新键
	CompStateUpdatesKey = "comp_state_updates"
	// WorkflowStateUpdatesKey 工作流状态更新键
	WorkflowStateUpdatesKey = "workflow_state_updates"
)
