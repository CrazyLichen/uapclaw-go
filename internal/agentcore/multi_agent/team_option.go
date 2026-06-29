package multi_agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamOptions 团队调用选项。
//
// 对应 Python: BaseTeam 各方法的可选参数（session、session_id、timeout、stream_modes）
type TeamOptions struct {
	// Session 团队会话（可选）
	//
	// 对应 Python: invoke(message, session) 的 session 参数
	// ⤵️ 8.30 TeamSession 实现后替换为具体类型
	Session any
	// SessionID 会话标识（可选）
	//
	// 对应 Python: send/publish 的 session_id 参数
	SessionID string
	// Timeout 响应超时秒数（可选）
	//
	// 对应 Python: send 的 timeout 参数
	Timeout float64
	// StreamModes 流式输出模式（可选）
	//
	// 对应 Python: stream 的 stream_modes 参数
	StreamModes []stream.StreamMode
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TeamOption 团队调用选项函数。
type TeamOption func(*TeamOptions)

// WithTeamSession 设置团队会话。
//
// ⤵️ 8.30 TeamSession 实现后参数类型从 any 替换为具体类型。
func WithTeamSession(sess any) TeamOption {
	return func(o *TeamOptions) { o.Session = sess }
}

// WithTeamSessionID 设置会话标识。
func WithTeamSessionID(sessionID string) TeamOption {
	return func(o *TeamOptions) { o.SessionID = sessionID }
}

// WithTeamTimeout 设置响应超时。
func WithTeamTimeout(timeout float64) TeamOption {
	return func(o *TeamOptions) { o.Timeout = timeout }
}

// WithTeamStreamModes 设置流式输出模式。
func WithTeamStreamModes(modes []stream.StreamMode) TeamOption {
	return func(o *TeamOptions) { o.StreamModes = modes }
}

// NewTeamOptions 从选项列表构建 TeamOptions。
func NewTeamOptions(opts ...TeamOption) *TeamOptions {
	o := &TeamOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// ──────────────────────────── 非导出函数 ────────────────────────────
