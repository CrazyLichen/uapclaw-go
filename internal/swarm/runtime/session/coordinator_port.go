package session

import (
	"context"
	"errors"
	"time"
)

// ──────────────────────────── 接口 ────────────────────────────

// CoordinatorPort 清理与执行账本需要的协调器能力。
// Python: service.py 里 self._session_coordinator 的调用面（清理、登记、执行、取消、关闭）
//
// 窄接口而非具体类型：只声明 Runtime 实际使用的方法，避免把千行协调器的
// 完整方法面拖进 service.go 依赖面（与 goalActivityChecker 同款约定）。
//
// 定义在 runtime/session 而非 runtime：它描述的是协调器自身的能力边界，
// 与 RuntimeSessionCoordinator 同包；server 侧要用它时只需 import 本包，
// 不必为了一个接口 import 整个 runtime。
type CoordinatorPort interface {
	// CloseSession 关闭某会话的运行中执行
	CloseSession(sessionID string, generation *int, waitTimeout *time.Duration) (CloseSessionResult, error)
	// SnapshotSession 取会话快照（不存在返回 nil）
	SnapshotSession(sessionID string) *RuntimeSessionSnapshot
	// RegisterSession 登记/刷新会话代次
	RegisterSession(sessionID, channelID string, policy SessionPersistencePolicy) (RuntimeSessionSnapshot, error)
	// RunUnary 执行一次非流式任务并等待结果
	RunUnary(ctx context.Context, sessionID, requestID string, kind SessionWorkKind, task func(context.Context) (any, error), options UnaryOptions) (any, error)
	// CancelExecution 取消某会话的执行
	CancelExecution(sessionID string, options CancelExecutionOptions) (CancelExecutionResult, error)
	// SnapshotSessions 取全部会话快照
	SnapshotSessions() []RuntimeSessionSnapshot
	// QuiesceSession 置会话为 QUIESCING 并返回生命周期上下文
	QuiesceSession(sessionID string, generation *int) (*SessionLifecycleContext, error)
	// Close 关闭协调器，返回超时会话
	Close(waitTimeout *time.Duration, timedOut *[]string) error
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrCoordinatorUnavailable 协调器未注入。
//
// 上游 _session_coordinator 恒存在（service.py:412 构造期自建）；
// go 侧由注入提供，未注入时相关承接方法返回此错误，避免静默丢弃登记/取消语义。
var ErrCoordinatorUnavailable = errors.New("session coordinator is not available")
