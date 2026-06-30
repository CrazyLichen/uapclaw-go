package multi_agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamOptions 团队调用选项，类型别名指向 schema 包。
//
// 对应 Python: BaseTeam 各方法的可选参数（session、session_id、timeout、stream_modes）
type TeamOptions = schema.TeamOptions

// ──────────────────────────── 枚举 ────────────────────────────

// TeamOption 团队调用选项函数，类型别名指向 schema 包。
type TeamOption = schema.TeamOption

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTeamSession 设置团队会话。
func WithTeamSession(sess *session.AgentTeamSession) TeamOption {
	return schema.WithTeamSession(sess)
}

// WithTeamSessionID 设置会话标识。
func WithTeamSessionID(sessionID string) TeamOption {
	return schema.WithTeamSessionID(sessionID)
}

// WithTeamTimeout 设置响应超时。
func WithTeamTimeout(timeout float64) TeamOption {
	return schema.WithTeamTimeout(timeout)
}

// WithTeamStreamModes 设置流式输出模式。
func WithTeamStreamModes(modes []stream.StreamMode) TeamOption {
	return schema.WithTeamStreamModes(modes)
}

// NewTeamOptions 从选项列表构建 TeamOptions。
func NewTeamOptions(opts ...TeamOption) *TeamOptions {
	return schema.NewTeamOptions(opts...)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
