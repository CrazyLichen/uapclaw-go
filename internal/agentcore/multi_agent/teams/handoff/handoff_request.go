package handoff

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HandoffHistoryEntry 交接历史记录
type HandoffHistoryEntry struct {
	// AgentID Agent 标识
	AgentID string `json:"agent"`
	// Output Agent 输出结果
	Output map[string]any `json:"output"`
}

// HandoffRequest 交接驱动消息
type HandoffRequest struct {
	// InputMessage 输入消息，类型与 BaseTeam.Invoke/BaseAgent.Invoke 一致。
	// 调用方必须将纯字符串包装为 {"query": msg} 传入，
	// Python 的 isinstance(msg, dict) 安全网在 Go 中由类型系统保证。
	InputMessage map[string]any
	// History 交接历史
	History []HandoffHistoryEntry
	// Session 团队会话（始终是 team-level session）
	Session *session.AgentTeamSession
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// SessionID 返回会话标识，无 session 时返回空字符串
func (r *HandoffRequest) SessionID() string {
	if r.Session != nil {
		return r.Session.GetSessionID()
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────
