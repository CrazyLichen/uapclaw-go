package session

// ──────────────────────────── 结构体 ────────────────────────────

// SessionLifecycleContext 标识一个进程内产品会话的代次。
//
// Python: runtime/session/model.py SessionLifecycleContext
type SessionLifecycleContext struct {
	// SessionID 会话标识
	SessionID string
	// Generation 代次（每次重新注册 +1）
	Generation int
}

// ExecutionLifecycleContext 承载一次 Runtime 执行的传输无关身份。
//
// Python: runtime/session/model.py ExecutionLifecycleContext
type ExecutionLifecycleContext struct {
	// Session 所属会话与代次
	Session SessionLifecycleContext
	// ExecutionID 执行标识
	ExecutionID string
	// RequestID 发起该执行的请求标识
	RequestID string
	// WorkKind 执行种类
	WorkKind SessionWorkKind
	// ChannelID 渠道标识
	ChannelID string
}

// TeamExecutionDetails 描述一次执行附带的团队所有权。
//
// Python: runtime/session/model.py TeamExecutionDetails
type TeamExecutionDetails struct {
	// ControlID 团队控制标识
	ControlID string
	// BackgroundTrigger 后台触发来源
	BackgroundTrigger string
	// SessionMessageID 跨会话消息标识
	SessionMessageID string
	// WaiterKind 响应等待者种类
	WaiterKind TeamWaiterKind
	// ObservationOnly 纯订阅者（跨业务暂停存活）
	ObservationOnly bool
}

// TeamExecutionContext 承载团队请求所有权，不耦合协调器。
//
// Python: runtime/session/model.py TeamExecutionContext
type TeamExecutionContext struct {
	// ExecutionLifecycleContext 执行身份
	ExecutionLifecycleContext
	// Details 团队详情
	Details TeamExecutionDetails
}

// SessionExecutionSnapshot 单次执行的只读快照。
//
// Python: runtime/session/model.py SessionExecutionSnapshot
type SessionExecutionSnapshot struct {
	// ExecutionID 执行标识
	ExecutionID string
	// SessionID 会话标识
	SessionID string
	// RequestID 请求标识
	RequestID string
	// Generation 代次
	Generation int
	// WorkKind 执行种类
	WorkKind SessionWorkKind
	// ChannelID 渠道标识
	ChannelID string
	// Team 团队详情（可空）
	Team *TeamExecutionDetails
	// ParentExecutionID 父执行标识
	ParentExecutionID string
	// RootWorkKind 根执行种类
	RootWorkKind SessionWorkKind
	// RootRequestID 根请求标识
	RootRequestID string
	// WaitingControlIDs 该执行等待的交互标识
	WaitingControlIDs map[string]struct{}
	// State 状态
	State SessionExecutionState
	// CreatedAt 创建时刻（单调秒）
	CreatedAt float64
	// StartedAt 开始时刻（可空）
	StartedAt *float64
	// FinishedAt 结束时刻（可空）
	FinishedAt *float64
	// Error 错误文本
	Error string
	// CancellationRequested 是否已请求取消
	CancellationRequested bool
}

// RuntimeSessionSnapshot 会话级只读快照。
//
// Python: runtime/session/model.py RuntimeSessionSnapshot
type RuntimeSessionSnapshot struct {
	// SessionID 会话标识
	SessionID string
	// ChannelID 渠道标识
	ChannelID string
	// PersistencePolicy 持久化策略
	PersistencePolicy SessionPersistencePolicy
	// Generation 代次
	Generation int
	// State 会话状态
	State RuntimeSessionState
	// Executions 该代次下的全部执行快照
	Executions []SessionExecutionSnapshot
}

// CancelExecutionResult 一次取消操作的结果。
//
// Python: runtime/session/model.py CancelExecutionResult
type CancelExecutionResult struct {
	// Matched 命中的执行数
	Matched int
	// Cancelled 成功取消的执行数
	Cancelled int
	// TimedOut 超时未停的执行标识
	TimedOut []string
}

// CloseSessionResult 一次关闭会话操作的结果。
//
// Python: runtime/session/model.py CloseSessionResult
type CloseSessionResult struct {
	// SessionID 会话标识
	SessionID string
	// Generation 代次（nil 表示调用方未指定）
	Generation *int
	// Existed 该代次是否存在
	Existed bool
	// TimedOut 超时未停的执行标识
	TimedOut []string
}

// ──────────────────────────── 枚举 ────────────────────────────

// RuntimeSessionState 会话在 Runtime 内部的临时状态。
//
// Python: runtime/session/model.py RuntimeSessionState
type RuntimeSessionState string

const (
	// RuntimeSessionStateReady 会话已注册但无活跃执行
	RuntimeSessionStateReady RuntimeSessionState = "ready"
	// RuntimeSessionStateActive 会话有活跃执行
	RuntimeSessionStateActive RuntimeSessionState = "active"
	// RuntimeSessionStateQuiescing 会话正在排空，不再接受新工作
	RuntimeSessionStateQuiescing RuntimeSessionState = "quiescing"
	// RuntimeSessionStateClosed 会话已关闭
	RuntimeSessionStateClosed RuntimeSessionState = "closed"
)

// SessionPersistencePolicy 会话持久化策略。
//
// Python: runtime/session/model.py SessionPersistencePolicy（仅一个成员）
type SessionPersistencePolicy string

const (
	// SessionPersistencePolicyPersistent 持久化会话
	SessionPersistencePolicyPersistent SessionPersistencePolicy = "persistent"
)

// SessionWorkKind 一次执行的业务种类。
//
// Python: runtime/session/model.py SessionWorkKind
type SessionWorkKind string

const (
	// SessionWorkKindChatUnary 单次（非流式）聊天轮
	SessionWorkKindChatUnary SessionWorkKind = "chat_unary"
	// SessionWorkKindChatStream 流式聊天轮
	SessionWorkKindChatStream SessionWorkKind = "chat_stream"
	// SessionWorkKindSessionMessage 跨会话消息（当前 Go 侧无生产者，枚举占位）
	SessionWorkKindSessionMessage SessionWorkKind = "session_message"
	// SessionWorkKindGoalStream 持续目标流
	SessionWorkKindGoalStream SessionWorkKind = "goal_stream"
	// SessionWorkKindGoalControl 持续目标控制
	SessionWorkKindGoalControl SessionWorkKind = "goal_control"
	// SessionWorkKindGoalAttach 持续目标挂载
	SessionWorkKindGoalAttach SessionWorkKind = "goal_attach"
	// SessionWorkKindControlInput 交互应答输入
	SessionWorkKindControlInput SessionWorkKind = "control_input"
	// SessionWorkKindSessionInput 会话补充输入
	SessionWorkKindSessionInput SessionWorkKind = "session_input"
	// SessionWorkKindHeartbeat 心跳轮
	SessionWorkKindHeartbeat SessionWorkKind = "heartbeat"
	// SessionWorkKindTeamRequest 团队请求
	SessionWorkKindTeamRequest SessionWorkKind = "team_request"
	// SessionWorkKindTeamControl 团队控制
	SessionWorkKindTeamControl SessionWorkKind = "team_control"
	// SessionWorkKindTeamBackground 团队后台
	SessionWorkKindTeamBackground SessionWorkKind = "team_background"
)

// SessionExecutionState 单次执行的状态。
//
// Python: runtime/session/model.py SessionExecutionState
type SessionExecutionState string

const (
	// SessionExecutionStateQueued 已登记待执行
	SessionExecutionStateQueued SessionExecutionState = "queued"
	// SessionExecutionStateRunning 正在执行
	SessionExecutionStateRunning SessionExecutionState = "running"
	// SessionExecutionStateWaitingForControl 等待交互应答
	SessionExecutionStateWaitingForControl SessionExecutionState = "waiting_for_control"
	// SessionExecutionStateWaitingForInput 等待下一轮输入（团队持久 owner）
	SessionExecutionStateWaitingForInput SessionExecutionState = "waiting_for_input"
	// SessionExecutionStateSucceeded 成功结束
	SessionExecutionStateSucceeded SessionExecutionState = "succeeded"
	// SessionExecutionStateFailed 失败结束
	SessionExecutionStateFailed SessionExecutionState = "failed"
	// SessionExecutionStateCancelled 已取消
	SessionExecutionStateCancelled SessionExecutionState = "cancelled"
)

// ExecutionCancelCause 描述一次执行或会话生命周期为何结束。
//
// Python: runtime/session/model.py ExecutionCancelCause
type ExecutionCancelCause string

const (
	// ExecutionCancelCauseConsumerClosed 消费方关闭
	ExecutionCancelCauseConsumerClosed ExecutionCancelCause = "consumer_closed"
	// ExecutionCancelCauseClientDisconnected 客户端断开
	ExecutionCancelCauseClientDisconnected ExecutionCancelCause = "client_disconnected"
	// ExecutionCancelCauseUserPause 用户暂停
	ExecutionCancelCauseUserPause ExecutionCancelCause = "user_pause"
	// ExecutionCancelCauseUserCancel 用户取消
	ExecutionCancelCauseUserCancel ExecutionCancelCause = "user_cancel"
	// ExecutionCancelCauseSessionDeleted 会话被删除
	ExecutionCancelCauseSessionDeleted ExecutionCancelCause = "session_deleted"
	// ExecutionCancelCauseRuntimeClosed Runtime 关闭
	ExecutionCancelCauseRuntimeClosed ExecutionCancelCause = "runtime_closed"
)

// TeamWaiterKind 团队执行持有的响应等待者种类。
//
// Python: runtime/session/model.py TeamWaiterKind
type TeamWaiterKind string

const (
	// TeamWaiterKindNone 非团队等待者
	TeamWaiterKindNone TeamWaiterKind = "none"
	// TeamWaiterKindPersistent 持久响应所有者（跨轮存活）
	TeamWaiterKindPersistent TeamWaiterKind = "persistent"
	// TeamWaiterKindRequest 单请求等待者
	TeamWaiterKindRequest TeamWaiterKind = "request"
	// TeamWaiterKindExclusive 独占等待者
	TeamWaiterKindExclusive TeamWaiterKind = "exclusive"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Scheduled 该种类是否走调度 lane。
//
// Python: SessionWorkKind.scheduled —— CHAT_UNARY / CHAT_STREAM / SESSION_MESSAGE
func (k SessionWorkKind) Scheduled() bool {
	switch k {
	case SessionWorkKindChatUnary, SessionWorkKindChatStream, SessionWorkKindSessionMessage:
		return true
	}
	return false
}

// LatestFirst 该种类是否后到先跑。
//
// Python: SessionWorkKind.latest_first —— 仅 CHAT_UNARY / CHAT_STREAM
func (k SessionWorkKind) LatestFirst() bool {
	switch k {
	case SessionWorkKindChatUnary, SessionWorkKindChatStream:
		return true
	}
	return false
}

// Terminal 该状态是否为终态。
//
// Python: SessionExecutionState.terminal —— SUCCEEDED / FAILED / CANCELLED
func (s SessionExecutionState) Terminal() bool {
	switch s {
	case SessionExecutionStateSucceeded, SessionExecutionStateFailed, SessionExecutionStateCancelled:
		return true
	}
	return false
}
