package agent_teams

import (
	"context"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionState 每-TeamAgent 的可变 session 状态容器。
// 对齐 Python: _session_id_context (contextvars.ContextVar)
//
// 通过 context.Value 传播 *SessionState 指针：
//   - 同一 TeamAgent 内的 goroutine 共享同一 SessionState 引用，SetSessionID 后立即可见
//   - 子 Teammate 调用 InitSessionState 创建新实例 + WithSessionState 派生新 ctx，父不受影响
//
// 并发安全：所有字段读写通过 sync.RWMutex 保护。
// Python 不需要锁因为 asyncio 是单线程协程。
type SessionState struct {
	mu        sync.RWMutex
	sessionID string
}

// sessionStateKeyType SessionState 的 context key 类型。
type sessionStateKeyType struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// InitSessionState 创建新的 SessionState 实例。
// 对齐 Python: _session_id_context = ContextVar("session_id", default=None)
func InitSessionState() *SessionState {
	return &SessionState{}
}

// WithSessionState 将 SessionState 注入 context。
// 对齐 Python: set_session_id(session_id) — 但 Go 通过 context.Value 传播指针
func WithSessionState(ctx context.Context, state *SessionState) context.Context {
	return context.WithValue(ctx, sessionStateKeyType{}, state)
}

// SessionStateFromCtx 从 context 中获取 SessionState。
// 返回 nil 表示当前 context 未绑定 SessionState。
func SessionStateFromCtx(ctx context.Context) *SessionState {
	if s, ok := ctx.Value(sessionStateKeyType{}).(*SessionState); ok {
		return s
	}
	return nil
}

// GetSessionID 从 context 中获取当前 session_id。
// 对齐 Python: get_session_id() -> Optional[str]
// 读取优先级：SessionState.sessionID → ""（空字符串）
func GetSessionID(ctx context.Context) string {
	if s := SessionStateFromCtx(ctx); s != nil {
		return s.GetSessionID()
	}
	return ""
}

// GetSessionID 获取当前 session_id。
func (s *SessionState) GetSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

// SetSessionID 设置当前 session_id。
// 对齐 Python: set_session_id(session_id) -> Token
// Go 不需要 Token，直接原地修改，同一指针的 goroutine 立即可见。
func (s *SessionState) SetSessionID(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionID = sessionID
}
