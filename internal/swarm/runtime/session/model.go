package session

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
