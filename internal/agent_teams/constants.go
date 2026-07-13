package agent_teams

// ──────────────────────────── 常量 ────────────────────────────

// HumanAgentMemberName 保留的 Human-in-the-Team 成员名。
// 对齐 Python: HUMAN_AGENT_MEMBER_NAME
const HumanAgentMemberName string = "human_agent"

// UserPseudoMemberName 伪成员名，代表外部调用者（非团队成员）。
// 对齐 Python: USER_PSEUDO_MEMBER_NAME
const UserPseudoMemberName string = "user"

// DefaultLeaderMemberName 默认 Leader 成员名。
// 对齐 Python: DEFAULT_LEADER_MEMBER_NAME
const DefaultLeaderMemberName string = "team_leader"

// ──────────────────────────── 全局变量 ────────────────────────────

// ReservedMemberNames 保留的成员名集合，用户声明成员不得使用。
// 对齐 Python: RESERVED_MEMBER_NAMES
//
// human_agent 仅在 enable_hitt=True 时由运行时注入，手动声明保留名会被拒绝。
var ReservedMemberNames = map[string]bool{
	HumanAgentMemberName:    true,
	UserPseudoMemberName:    true,
	DefaultLeaderMemberName: true,
}
