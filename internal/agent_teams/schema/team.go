package schema

// ──────────────────────────── 结构体 ────────────────────────────

// MemberOpResult 团队成员操作结果，保留失败原因。
// 对齐 Python: MemberOpResult (openjiuwen/agent_teams/schema/team.py)
type MemberOpResult struct {
	// OK 操作是否成功
	OK bool
	// Reason 失败原因（成功时为空）
	Reason string
}

// TeamCompletionSnapshot 团队完成时的快照计数。
// 对齐 Python: TeamCompletionSnapshot
type TeamCompletionSnapshot struct {
	// MemberCount 成员数
	MemberCount int
	// TaskCount 任务数
	TaskCount int
}

// TeamMemberSpec 预定义成员的声明式输入。
// 对齐 Python: TeamMemberSpec
type TeamMemberSpec struct {
	// MemberName 成员名
	MemberName string
	// DisplayName 显示名
	DisplayName string
	// RoleType 角色类型
	RoleType TeamRole
	// Persona 人设描述
	Persona string
	// PromptHint 启动提示（可选）
	PromptHint string
	// ModelName 模型池分配名称（可选）
	ModelName string
}

// TeamSpec 团队定义与目标。
// 对齐 Python: TeamSpec
type TeamSpec struct {
	// TeamName 团队名
	TeamName string
	// DisplayName 显示名
	DisplayName string
	// LeaderMemberName Leader 成员名
	LeaderMemberName string
	// Language 语言偏好
	Language string
	// Metadata 元数据
	Metadata map[string]any
	// ModelPool LLM 端点池（⤵️ 回填: 9.64 — ModelPoolEntry 类型）
	ModelPool any
	// ModelPoolStrategy 池分配策略：round_robin / by_model_name / router
	ModelPoolStrategy string
}

// TeamRuntimeContext 单个团队成员的轻量运行时上下文。
// 对齐 Python: TeamRuntimeContext
type TeamRuntimeContext struct {
	// Role 团队角色
	Role TeamRole
	// MemberName 成员名
	MemberName string
	// Persona 人设
	Persona string
	// TeamSpec 团队规格
	TeamSpec *TeamSpec
	// MessagerConfig 消息传输配置（⤵️ 回填: 9.65 — MessagerTransportConfig 类型）
	MessagerConfig any
	// DBConfig 数据库配置（⤵️ 回填: 9.65 — DatabaseConfig 类型）
	DBConfig any
	// MemberModel 分配给此成员的模型配置
	MemberModel *TeamModelConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// TeamLifecycle 团队生命周期模式。
// 对齐 Python: TeamLifecycle
type TeamLifecycle string

const (
	// TeamLifecycleTemporary 临时团队（一轮后解散）
	TeamLifecycleTemporary TeamLifecycle = "temporary"
	// TeamLifecyclePersistent 持久团队（跨轮次保持）
	TeamLifecyclePersistent TeamLifecycle = "persistent"
)

// TeamRole 团队角色枚举。
// 对齐 Python: TeamRole
// HUMAN_AGENT 是代表人类协作者的一等成员，
// 在模型的思维模型中与 Leader 和 Teammate 地位平等，
// 但运行时足迹不同：不拥有 DeepAgent 进程，仅有 send_message 工具，
// 在团队清理前保持 READY 状态。
type TeamRole string

const (
	// TeamRoleLeader 团队领导
	TeamRoleLeader TeamRole = "leader"
	// TeamRoleTeammate 团队成员
	TeamRoleTeammate TeamRole = "teammate"
	// TeamRoleHumanAgent 人类代理成员
	TeamRoleHumanAgent TeamRole = "human_agent"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberOpResultSuccess 创建成功结果。
// 对齐 Python: MemberOpResult.success()
func NewMemberOpResultSuccess() MemberOpResult {
	return MemberOpResult{OK: true}
}

// NewMemberOpResultFail 创建失败结果。
// 对齐 Python: MemberOpResult.fail(reason)
func NewMemberOpResultFail(reason string) MemberOpResult {
	return MemberOpResult{OK: false, Reason: reason}
}
