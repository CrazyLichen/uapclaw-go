package worktree

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeSessionState 每-Agent 的可变 worktree 会话状态容器。
// Python: WorktreeSessionState (session.py)
//
// 通过 context.Value 传播 *WorktreeSessionState 指针：
//   - 同一 Agent 内的 goroutine 共享同一引用，SetCurrentSession 后立即可见
//   - 子 Agent 调用 InitWorktreeSessionState 创建新实例 + WithWorktreeSessionState 派生新 ctx，父不受影响
//
// 并发安全：所有字段读写通过 sync.RWMutex 保护。
// Python 不需要锁因为 asyncio 是单线程协程。
type WorktreeSessionState struct {
	mu                  sync.RWMutex
	session             *WorktreeSession
	defaultWorktreeName string
}

// worktreeSessionStateKeyType WorktreeSessionState 的 context key 类型。
type worktreeSessionStateKeyType struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// InitWorktreeSessionState 创建新的 WorktreeSessionState 实例。
// Python: init_session_state()
//
// 在 Agent 初始化时调用，确保同一 Agent 内的 goroutine 共享同一可变 holder。
func InitWorktreeSessionState() *WorktreeSessionState {
	return &WorktreeSessionState{}
}

// WithWorktreeSessionState 将 WorktreeSessionState 注入 context。
// Python: _state.set(s) — 但 Go 通过 context.Value 传播指针
func WithWorktreeSessionState(ctx context.Context, state *WorktreeSessionState) context.Context {
	return context.WithValue(ctx, worktreeSessionStateKeyType{}, state)
}

// WorktreeSessionStateFromCtx 从 context 中获取 WorktreeSessionState。
// 返回 nil 表示当前 context 未绑定 WorktreeSessionState。
func WorktreeSessionStateFromCtx(ctx context.Context) *WorktreeSessionState {
	if s, ok := ctx.Value(worktreeSessionStateKeyType{}).(*WorktreeSessionState); ok {
		return s
	}
	return nil
}

// GetCurrentSession 从 context 中获取当前 worktree 会话。
// Python: get_current_session() -> Optional[WorktreeSession]
func GetCurrentSession(ctx context.Context) *WorktreeSession {
	if s := WorktreeSessionStateFromCtx(ctx); s != nil {
		return s.GetCurrentSession()
	}
	return nil
}

// SetCurrentSession 设置或清空当前 worktree 会话。
// Python: set_current_session(session)
func SetCurrentSession(ctx context.Context, session *WorktreeSession) {
	if s := WorktreeSessionStateFromCtx(ctx); s != nil {
		s.SetCurrentSession(session)
	} else {
		logger.Warn(logComponent).
			Str("event_type", "set_current_session_no_state").
			Msg("ctx 中缺少 WorktreeSessionState，SetCurrentSession 无效")
	}
}

// GetDefaultWorktreeName 从 context 中获取 session 级默认 worktree 名称。
// Python: get_default_worktree_name()
func GetDefaultWorktreeName(ctx context.Context) string {
	if s := WorktreeSessionStateFromCtx(ctx); s != nil {
		return s.GetDefaultWorktreeName()
	}
	return ""
}

// SetDefaultWorktreeName 设置 session 级默认 worktree 名称。
// Python: set_default_worktree_name(name)
func SetDefaultWorktreeName(ctx context.Context, name string) {
	if s := WorktreeSessionStateFromCtx(ctx); s != nil {
		s.SetDefaultWorktreeName(name)
	}
}

// RequireCurrentSession 获取当前会话，不存在返回 error。
// Python: require_current_session()
func RequireCurrentSession(ctx context.Context) (*WorktreeSession, error) {
	session := GetCurrentSession(ctx)
	if session == nil {
		return nil, fmt.Errorf("不在 worktree 会话中")
	}
	return session, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCurrentSession 获取当前 worktree 会话。
func (s *WorktreeSessionState) GetCurrentSession() *WorktreeSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.session
}

// SetCurrentSession 设置或清空当前 worktree 会话。
func (s *WorktreeSessionState) SetCurrentSession(session *WorktreeSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = session
}

// GetDefaultWorktreeName 获取 session 级默认 worktree 名称。
func (s *WorktreeSessionState) GetDefaultWorktreeName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.defaultWorktreeName
}

// SetDefaultWorktreeName 设置 session 级默认 worktree 名称。
func (s *WorktreeSessionState) SetDefaultWorktreeName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultWorktreeName = name
}
