package session

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// NoControlTargetError 会话没有可接收控制的活跃执行。
//
// Python: runtime/session/model.py NoControlTargetError
type NoControlTargetError struct {
	// Message 错误说明
	Message string
}

// AnswerInFlightError 同一交互的应答正在投递中。
//
// Python: runtime/session/model.py AnswerInFlightError(NoControlTargetError)
type AnswerInFlightError struct {
	// NoControlTargetError 嵌入父错误，保持 errors.As 语义与上游继承一致
	*NoControlTargetError
}

// SessionExecutionEndedError 控制输入到达时会话执行已结束。
//
// Python: runtime/session/model.py SessionExecutionEndedError
type SessionExecutionEndedError struct {
	// Message 错误说明
	Message string
}

// SessionCloseTimeoutError 关闭会话时仍有执行未停止。
//
// Python: runtime/session/model.py SessionCloseTimeoutError
type SessionCloseTimeoutError struct {
	// SessionID 会话标识
	SessionID string
	// ExecutionIDs 超时未停的执行标识
	ExecutionIDs []string
}

// terminalSignal 单次关闭信号，等价 asyncio.Event 的幂等 set()。
type terminalSignal struct {
	// ch 关闭即表示信号已发出
	ch chan struct{}
	// once 保证只关闭一次，重复 close 不 panic
	once sync.Once
}

// SessionExecutionHandle 一次执行的运行时句柄。
//
// 与上游差异（spec §8 D-2 / D-3）：
//   - Python 用单个 asyncio.Task 同时承载"可取消"与"可等待"；Go 拆为
//     Cancel（发出取消）与 Done（执行方退出）两个成员。
//   - Python 用 asyncio.Event 表达业务终态；Go 用 Terminal（terminalSignal，
//     内部 sync.Once 保幂等）。
//
// 两个信号语义不同（spec §2.1）：
//   - Terminal：业务结果已定（registry.MarkTerminal 触发，唯一 close 点）
//   - Done：执行方收尾完成、协程即将退出（执行方 defer 触发）
//
// "清场"等 Done，"收结果"等 Terminal。
//
// Python: runtime/session/model.py SessionExecutionHandle
type SessionExecutionHandle struct {
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
	// RetainOwnerTask 终态后是否保留 owner 执行方等待其收敛
	RetainOwnerTask bool

	// Cancel 等价 asyncio.Task.cancel()；置 nil 表示已无取消必要
	Cancel context.CancelFunc
	// Done 等价 "await task"；由执行方 defer 最后一行通过 MarkDone 关闭
	Done chan struct{}
	// Terminal 等价 handle.terminal_event；仅 registry.MarkTerminal 关闭
	Terminal terminalSignal
	// Agent 该执行使用的 agent 实例（仅执行期持有）
	Agent any

	// doneOnce 保证 Done 只关闭一次
	doneOnce sync.Once
}

// ──────────────────────────── 全局变量 ────────────────────────────

// processStart 进程启动时刻，作为单调时钟基准。
var processStart = time.Now()

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *NoControlTargetError) Error() string { return e.Message }

// Error 实现 error 接口，透传嵌入的父错误说明。
func (e *AnswerInFlightError) Error() string {
	if e.NoControlTargetError == nil {
		return ""
	}
	return e.NoControlTargetError.Message
}

// Unwrap 暴露嵌入的父错误，使 errors.As/Is 能穿透到 NoControlTargetError。
//
// Go 的 struct 嵌入不等于 Python 继承：errors.As 靠 Unwrap 链而非字段提升。
func (e *AnswerInFlightError) Unwrap() error {
	if e.NoControlTargetError == nil {
		return nil
	}
	return e.NoControlTargetError
}

// Error 实现 error 接口。
func (e *SessionExecutionEndedError) Error() string { return e.Message }

// Error 实现 error 接口，文案与上游 f-string 逐字一致。
func (e *SessionCloseTimeoutError) Error() string {
	return fmt.Sprintf("session close timed out: session_id=%s executions=%v",
		e.SessionID, e.ExecutionIDs)
}

// Set 发出信号（幂等）。
//
// Python: asyncio.Event.set()
func (s *terminalSignal) Set() { s.once.Do(func() { close(s.ch) }) }

// Wait 返回等待用通道。
func (s *terminalSignal) Wait() <-chan struct{} { return s.ch }

// IsTerminalClosed 信号是否已发出。
func (s *terminalSignal) IsTerminalClosed() bool {
	select {
	case <-s.ch:
		return true
	default:
		return false
	}
}

// NewSessionExecutionHandle 构造一个处于 QUEUED 的执行句柄。
//
// Python: RuntimeSessionCoordinator._new_execution 内构造
func NewSessionExecutionHandle(
	executionID, sessionID, requestID string,
	generation int,
	workKind SessionWorkKind,
	channelID string,
) *SessionExecutionHandle {
	return &SessionExecutionHandle{
		ExecutionID:       executionID,
		SessionID:         sessionID,
		RequestID:         requestID,
		Generation:        generation,
		WorkKind:          workKind,
		ChannelID:         channelID,
		WaitingControlIDs: map[string]struct{}{},
		State:             SessionExecutionStateQueued,
		CreatedAt:         monotonicSeconds(),
		Done:              make(chan struct{}),
		Terminal:          newTerminalSignal(),
	}
}

// MarkDone 由执行方在 defer 的最后一行调用，等价"协程即将退出"。
//
// 与 Terminal 的分工见 SessionExecutionHandle 注释：Done 表示收尾完成，
// Terminal 表示业务终态。
func (h *SessionExecutionHandle) MarkDone() { h.doneOnce.Do(func() { close(h.Done) }) }

// Snapshot 生成只读快照。
//
// ⚠️ 并发契约：本方法读取 handle 的可变字段（State/StartedAt/FinishedAt/
// WaitingControlIDs 等），**必须在 registry 锁内调用**。锁外调用会与
// MarkRunning / MarkTerminal 竞争（-race 实测：StartedAt 指针写 vs 读）。
// 对外暴露的快照入口一律走 SessionExecutionRegistry.SnapshotOf /
// SnapshotValue / SnapshotsForSession，它们自带锁。
//
// Python: SessionExecutionHandle.snapshot()
func (h *SessionExecutionHandle) Snapshot() SessionExecutionSnapshot {
	var team *TeamExecutionDetails
	if h.Team != nil {
		copied := *h.Team
		team = &copied
	}
	waiting := make(map[string]struct{}, len(h.WaitingControlIDs))
	for k := range h.WaitingControlIDs {
		waiting[k] = struct{}{}
	}
	return SessionExecutionSnapshot{
		ExecutionID:           h.ExecutionID,
		SessionID:             h.SessionID,
		RequestID:             h.RequestID,
		Generation:            h.Generation,
		WorkKind:              h.WorkKind,
		ChannelID:             h.ChannelID,
		Team:                  team,
		ParentExecutionID:     h.ParentExecutionID,
		RootWorkKind:          h.RootWorkKind,
		RootRequestID:         h.RootRequestID,
		WaitingControlIDs:     waiting,
		State:                 h.State,
		CreatedAt:             h.CreatedAt,
		StartedAt:             h.StartedAt,
		FinishedAt:            h.FinishedAt,
		Error:                 h.Error,
		CancellationRequested: h.CancellationRequested,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTerminalSignal 创建未触发信号。
func newTerminalSignal() terminalSignal {
	return terminalSignal{ch: make(chan struct{})}
}

// monotonicSeconds 单调时钟秒值，等价 Python time.monotonic()。
//
// Go 的 time.Since(time.Time{}) 不单调，用进程启动时刻作基准。
func monotonicSeconds() float64 {
	return float64(time.Since(processStart).Nanoseconds()) / float64(time.Second)
}

// ──────────────────────────── 枚举 ────────────────────────────

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
