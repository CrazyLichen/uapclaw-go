package agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamAgentState TeamAgent 可变运行时状态。
// 四象限分解的第二象限：运行时可变值，跨 Manager 共享。
// Python: TeamAgentState (openjiuwen/agent_teams/agent/state.py)
//
// 注意：session_id 不在此处保存。
// session_id 的唯一真实来源是 agent_teams contextvar
// （通过 agent_teams.GetSessionID 获取）。
// 在 state 上缓存字符串会重新引入"双真实来源"问题。
type TeamAgentState struct {
	// TeamSession 团队会话
	// M-29: 对齐 Python state.team_session 的窄存储语义——
	// Python `session if isinstance(session, AgentTeamSession) else None` 只存
	// AgentTeamSession 实例（非 AgentTeamSession 输入一律 None）。
	// BindSession 签名保持 any（对齐 Python 接受 str/AgentTeamSession 联合类型），
	// 收窄在 BindSession 内完成。
	TeamSession *session.AgentTeamSession
	// TeamMember 当前成员句柄
	TeamMember *TeamMember
	// PendingUserQuery 待处理的用户查询
	PendingUserQuery string
	// EventListeners 已注册的事件监听器
	// Python: list[Callable[[EventMessage], Awaitable[None]]]
	// Go 等价：messager.MessagerHandler = func(ctx, *EventMessage) error
	// 用 EventListenerHandle 包装以支持移除操作
	EventListeners []*messager.EventListenerHandle
	// TeamCleaned clean_team 成功路径的一次性锁存标志
	TeamCleaned bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamAgentState 创建默认的 TeamAgentState。
func NewTeamAgentState() *TeamAgentState {
	return &TeamAgentState{
		EventListeners: make([]*messager.EventListenerHandle, 0),
	}
}
