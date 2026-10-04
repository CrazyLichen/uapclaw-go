package events

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// TeamTopic 团队事件路由的 topic 类别。
// Python: TeamTopic (openjiuwen/agent_teams/schema/events.py)
type TeamTopic string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TeamTopicTeam 团队级事件
	TeamTopicTeam TeamTopic = "team"
	// TeamTopicTask 任务级事件
	TeamTopicTask TeamTopic = "task"
	// TeamTopicMessage 消息级事件
	TeamTopicMessage TeamTopic = "message"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// AllTeamTopics 所有 TeamTopic 枚举值，对齐 Python: for topic in TeamTopic
var AllTeamTopics = []TeamTopic{TeamTopicTeam, TeamTopicTask, TeamTopicMessage}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildTopic 构建 topic 字符串。
// Python: TeamTopic.build(session_id, team_name)
func (t TeamTopic) Build(sessionID, teamName string) string {
	return "session:" + sessionID + ":team:" + teamName + ":" + string(t)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
