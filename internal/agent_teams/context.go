package agent_teams

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionState 每-TeamAgent 的可变 session 状态容器。
// 已迁移到 sessionctx 包，此处保留类型别名以兼容现有调用方。
type SessionState = sessionctx.SessionState

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// InitSessionState 创建新的 SessionState 实例。
// 委托到 sessionctx.InitSessionState()。
func InitSessionState() *SessionState {
	return sessionctx.InitSessionState()
}

// WithSessionState 将 SessionState 注入 context。
// 委托到 sessionctx.WithSessionState()。
func WithSessionState(ctx context.Context, state *SessionState) context.Context {
	return sessionctx.WithSessionState(ctx, state)
}

// SessionStateFromCtx 从 context 中获取 SessionState。
// 委托到 sessionctx.SessionStateFromCtx()。
func SessionStateFromCtx(ctx context.Context) *SessionState {
	return sessionctx.SessionStateFromCtx(ctx)
}

// GetSessionID 从 context 中获取当前 session_id。
// 委托到 sessionctx.GetSessionID()。
func GetSessionID(ctx context.Context) string {
	return sessionctx.GetSessionID(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
