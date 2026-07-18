package agent

// ──────────────────────────── 结构体 ────────────────────────────

// TeamAgentState TeamAgent 可变运行时状态。
// 四象限分解的第二象限：运行时可变值，跨 Manager 共享。
// 对齐 Python: TeamAgentState (openjiuwen/agent_teams/agent/state.py)
//
// 注意：session_id 不在此处保存。
// session_id 的唯一真实来源是 agent_teams contextvar
// （agent_teams.GetSessionID）。
// 在 state 上缓存字符串会重新引入"双真实来源"问题。
type TeamAgentState struct {
	// TeamSession 团队会话
	// TODO: 定义 AgentTeamSession 接口后替换 any
	TeamSession any
	// TeamMember 当前成员句柄
	TeamMember *TeamMember
	// PendingUserQuery 待处理的用户查询
	PendingUserQuery string
	// EventListeners 已注册的事件监听器
	EventListeners []any
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
		EventListeners: make([]any, 0),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
