package multi_agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTeamSession 设置团队会话。
func WithTeamSession(sess *session.AgentTeamSession) schema.TeamOption {
	return schema.WithTeamSession(sess)
}

// WithTeamSessionID 设置会话标识。
func WithTeamSessionID(sessionID string) schema.TeamOption {
	return schema.WithTeamSessionID(sessionID)
}

// WithTeamTimeout 设置响应超时。
func WithTeamTimeout(timeout float64) schema.TeamOption {
	return schema.WithTeamTimeout(timeout)
}

// WithTeamStreamModes 设置流式输出模式。
func WithTeamStreamModes(modes []stream.StreamMode) schema.TeamOption {
	return schema.WithTeamStreamModes(modes)
}

// NewTeamOptions 从选项列表构建 TeamOptions。
func NewTeamOptions(opts ...schema.TeamOption) *schema.TeamOptions {
	return schema.NewTeamOptions(opts...)
}
