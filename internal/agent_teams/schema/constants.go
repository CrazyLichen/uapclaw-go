package schema

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// HumanAgentMemberName 保留的 Human-in-the-Team 成员名。
// Python: HUMAN_AGENT_MEMBER_NAME
// 从 agent_teams 根包搬入，打断 schema→agent_teams 循环依赖。
const HumanAgentMemberName string = "human_agent"

// UserPseudoMemberName 伪成员名，代表外部调用者（非团队成员）。
// Python: USER_PSEUDO_MEMBER_NAME
// 从 agent_teams 根包搬入，打断 schema→agent_teams 循环依赖。
const UserPseudoMemberName string = "user"

// DefaultLeaderMemberName 默认 Leader 成员名。
// Python: DEFAULT_LEADER_MEMBER_NAME
// 从 agent_teams 根包搬入，打断 schema→agent_teams 循环依赖。
const DefaultLeaderMemberName string = "team_leader"

// InlineTaskMemberEnabled 任务级内联成员硬开关（F_118），本版本关闭。
//
// 关闭时 dispatch 模板的 {{inline_member}} 槽位恒为空，leader 不会被告知
// 一个它无法使用的字段（inline 声明 / lifetime / 休眠那整段散文一起消失）。
//
// 必须在调用时读，不要下传成局部常量——上游注释明确这条（constants.py L41-42）。
// 放在 schema 包是因为 prompts 包要读它、而 prompts 已依赖 schema；
// agent_teams 根包通过同名常量转发，保持既有引用面不变。
//
// Python: INLINE_TASK_MEMBER_ENABLED: bool = False (agent_teams/constants.py L36)
const InlineTaskMemberEnabled = false

// ──────────────────────────── 全局变量 ────────────────────────────

// ReservedMemberNames 保留的成员名集合，用户声明成员不得使用。
// Python: RESERVED_MEMBER_NAMES
// 从 agent_teams 根包搬入，打断 schema→agent_teams 循环依赖。
// human_agent 仅在 enable_hitt=True 时由运行时注入，手动声明保留名会被拒绝。
var ReservedMemberNames = map[string]bool{
	HumanAgentMemberName:    true,
	UserPseudoMemberName:    true,
	DefaultLeaderMemberName: true,
}
