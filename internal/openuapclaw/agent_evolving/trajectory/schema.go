package trajectory

// schema.go 轨迹自有的 OTLP 信封与 RL 属性名常量。
// 对齐 Python: agent_evolving/trajectory/schema.py（全文，2026-12-04 快照）。
// 观测语义键（GenAI/tool/team 上下文）归属 extensions.observability —— Go 侧
// 复用 internal/openuapclaw/agent_teams/observability/semconv.go 的既有常量，不在本包重复
// 定义（与 Python 不同：Python 为切断 agent_teams 运行时依赖图而镜像字符串；
// Go 侧 observability 包不反向依赖 evolving，直接导入无循环，故不镜像）。
// 本包只拥有标识轨迹的字段与 evolution/RL 投影新增的字段。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MemberID 成员归属属性键。
	// Python: schema.py MEMBER_ID = "agentteam.member.name"（镜像 observability）。
	MemberID = "agentteam.member.name"
	// SessionID 会话归属属性键（标准 GenAI 属性，非项目镜像）。
	// Python: schema.py SESSION_ID = "gen_ai.conversation.id"。
	SessionID = "gen_ai.conversation.id"
	// TeamID 团队归属属性键。
	// Python: schema.py TEAM_ID = "agentteam.team.id"。
	TeamID = "agentteam.team.id"

	// TrajectoryProjectionVersion 投影信封版本（资源属性值）。
	// Python: TRAJECTORY_PROJECTION_VERSION = "1"。
	// 与 span 级 openjiuwen.trajectory.schema_version（span 契约版本）是两个键。
	TrajectoryProjectionVersion = "1"
	// TrajectoryScopeName 投影 scope 名。
	// Python: TRAJECTORY_SCOPE_NAME = "openjiuwen.agent_evolving.trajectory"。
	TrajectoryScopeName = "openjiuwen.agent_evolving.trajectory"

	// TrajectoryID 轨迹标识属性键（资源属性）。
	// Python: TRAJECTORY_ID = "openjiuwen.trajectory_id"。
	TrajectoryID = "openjiuwen.trajectory_id"
	// TrajectoryProjectionVersionAttr 投影版本属性键。
	// Python: TRAJECTORY_PROJECTION_VERSION_ATTR = "openjiuwen.trajectory.projection_version"。
	TrajectoryProjectionVersionAttr = "openjiuwen.trajectory.projection_version"
	// TrajectorySource 轨迹来源属性键。
	// Python: TRAJECTORY_SOURCE = "openjiuwen.trajectory.source"。
	TrajectorySource = "openjiuwen.trajectory.source"
	// CaseID 离线用例标识属性键。
	// Python: CASE_ID = "case_id"。
	CaseID = "case_id"

	// RL enrichment 字段为轨迹自有，不由 extensions.observability 产出。
	// 名称对齐 S_004。

	// RLPromptTokenIDs RL 提示词 token ID 属性键。
	// Python: RL_PROMPT_TOKEN_IDS = "evolution.rl.prompt_token_ids"。
	RLPromptTokenIDs = "evolution.rl.prompt_token_ids"
	// RLCompletionTokenIDs RL 补全 token ID 属性键。
	// Python: RL_COMPLETION_TOKEN_IDS = "evolution.rl.completion_token_ids"。
	RLCompletionTokenIDs = "evolution.rl.completion_token_ids"
	// RLLogprobs RL token 对数概率属性键。
	// Python: RL_LOGPROBS = "evolution.rl.logprobs"。
	RLLogprobs = "evolution.rl.logprobs"
	// RLReward RL 标量奖励属性键。
	// Python: RL_REWARD = "evolution.rl.reward"。
	RLReward = "evolution.rl.reward"
	// RLFinalReward RL 终局奖励属性键。
	// Python: RL_FINAL_REWARD = "evolution.rl.final_reward"。
	RLFinalReward = "evolution.rl.final_reward"
	// RLRewardSource RL 奖励来源属性键。
	// Python: RL_REWARD_SOURCE = "evolution.rl.reward_source"。
	RLRewardSource = "evolution.rl.reward_source"
	// RLRolloutID RL rollout 标识属性键。
	// Python: RL_ROLLOUT_ID = "evolution.rl.rollout_id"。
	RLRolloutID = "evolution.rl.rollout_id"
	// RLAttemptSeq RL 尝试序号属性键。
	// Python: RL_ATTEMPT_SEQ = "evolution.rl.attempt_seq"。
	RLAttemptSeq = "evolution.rl.attempt_seq"
	// RLTokenSource RL token 来源属性键。
	// Python: RL_TOKEN_SOURCE = "evolution.rl.token_source"。
	RLTokenSource = "evolution.rl.token_source"
)
