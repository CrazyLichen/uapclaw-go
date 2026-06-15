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
// 提供 GetGlobal/UpdateGlobal/UpdateTrace/Dump 等方法，
// 由 AgentStateCollection 和 WorkflowStateCollection 实现。
// 消费方通过此接口多态调用，无需类型断言。
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

// ──────────────────────────── 向后兼容别名 ────────────────────────────

// 以下别名保持向后兼容，后续版本移除
type ReadableState = ReadableStateLike
type RecoverableState = RecoverableStateLike
type State = StateLike
type CommitState = CommitStateLike

// ──────────────────────────── 枚举 ────────────────────────────

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
