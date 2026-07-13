package agent_teams

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// sessionIDKey session_id 的 context key。
// 对齐 Python: _session_id_context (contextvars.ContextVar)
type sessionIDKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetSessionID 设置当前 session_id 到 context 中。
// 对齐 Python: set_session_id(session_id) -> Token
func SetSessionID(parent context.Context, sessionID string) context.Context {
	return context.WithValue(parent, sessionIDKey{}, sessionID)
}

// GetSessionID 从 context 中获取当前 session_id。
// 对齐 Python: get_session_id() -> Optional[str]
func GetSessionID(ctx context.Context) string {
	if v, ok := ctx.Value(sessionIDKey{}).(string); ok {
		return v
	}
	return ""
}
