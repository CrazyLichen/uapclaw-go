package schema

import (
	"fmt"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberOpResult 团队成员操作结果。
type MemberOpResult struct {
	// OK 操作是否成功
	OK bool `json:"ok"`
	// Reason 失败原因
	Reason string `json:"reason"`
}

// TeamCompletionSnapshot 团队完成时的快照计数。
type TeamCompletionSnapshot struct {
	// MemberCount 完成时的成员数
	MemberCount int `json:"member_count"`
	// TaskCount 完成时的任务数
	TaskCount int `json:"task_count"`
}

// TeamMemberSpec 预定义成员的声明式输入。
type TeamMemberSpec struct {
	// MemberName 成员名
	MemberName string `json:"member_name"`
	// DisplayName 显示名
	DisplayName string `json:"display_name"`
	// RoleType 角色类型
	RoleType TeamRole `json:"role_type"`
	// Persona 人设描述
	Persona string `json:"persona"`
	// Desc 成员公开描述（DB team_member.desc）
	// Python: MemberSpecBase.desc (schema/team.py:183)
	Desc string `json:"desc,omitempty"`
	// Prompt 成员私有工作约定（DB team_member.prompt）
	// Python: MemberSpecBase.prompt (schema/team.py:184)
	Prompt string `json:"prompt,omitempty"`
	// PromptHint 提示词提示（可选）
	PromptHint string `json:"prompt_hint,omitempty"`
	// ModelName 模型名称（可选）
	ModelName string `json:"model_name,omitempty"`
}

// TeamSpec 团队定义与目标。
type TeamSpec struct {
	// TeamName 团队名
	TeamName string `json:"team_name"`
	// DisplayName 显示名
	DisplayName string `json:"display_name"`
	// LeaderMemberName Leader 成员名
	LeaderMemberName string `json:"leader_member_name"`
	// Language 团队语言（可选）
	Language string `json:"language,omitempty"`
	// Metadata 元数据（可选）
	Metadata map[string]any `json:"metadata,omitempty"`
	// ModelPool LLM 端点池
	ModelPool []models.ModelPoolEntry `json:"model_pool,omitempty"`
	// ModelPoolStrategy 模型池分配策略
	ModelPoolStrategy string `json:"model_pool_strategy,omitempty"`
}

// TeamRuntimeContext 单个团队成员的轻量运行时上下文。
type TeamRuntimeContext struct {
	// Role 团队角色
	Role TeamRole `json:"role"`
	// MemberName 成员名
	MemberName string `json:"member_name"`
	// Persona 人设描述
	Persona string `json:"persona"`
	// DisplayName 成员可读标签（DB team_member.display_name）
	//
	// 队友在名册里看到它，成员自己也会被告知，以便认出哪一行是自己。
	// Python: TeamRuntimeContext.display_name (schema/team.py:648)
	DisplayName string `json:"display_name,omitempty"`
	// Desc 成员公开描述（DB team_member.desc）
	// Python: TeamRuntimeContext.desc (schema/team.py:654)
	Desc string `json:"desc,omitempty"`
	// Prompt 成员私有工作约定（DB team_member.prompt）
	// Python: TeamRuntimeContext.prompt (schema/team.py:658)
	Prompt string `json:"prompt,omitempty"`
	// TeamDesc 团队级描述（DB team_info.desc）
	// Python: TeamRuntimeContext.team_desc (schema/team.py:668)
	TeamDesc string `json:"team_desc,omitempty"`
	// TeamPrompt 团队级提示词（DB team_info.prompt）
	// Python: TeamRuntimeContext.team_prompt (schema/team.py:674)
	TeamPrompt string `json:"team_prompt,omitempty"`
	// WorktreePath 隔离 worktree 的绝对 cwd 覆盖（teammate 专用）
	// Python: TeamRuntimeContext.worktree_path (schema/team.py:687)
	WorktreePath string `json:"worktree_path,omitempty"`
	// ForkSource 本成员继承上下文的来源成员名（普通 spawn 为空）
	// Python: TeamRuntimeContext.fork_source (schema/team.py:694)
	ForkSource string `json:"fork_source,omitempty"`
	// PermissionsOverride 平台下发的权限覆盖（nil 表示不覆盖）
	// Python: TeamRuntimeContext.permissions_override
	PermissionsOverride any `json:"permissions_override,omitempty"`
	// TeamSpec 团队规格（可选）
	TeamSpec *TeamSpec `json:"team_spec,omitempty"`
	// MessagerConfig 消息传输配置（可选）
	MessagerConfig *MessagerTransportConfig `json:"messager_config,omitempty"`
	// DBConfig 数据库配置
	DBConfig database.DBConfigProvider `json:"db_config,omitempty"`
	// MemberModel 成员模型配置（可选）
	MemberModel *models.TeamModelConfig `json:"member_model,omitempty"`
}

// UnknownHumanAgentError 发送者不是已注册的 human-agent 成员时抛出。
// Python: UnknownHumanAgentError (openjiuwen/agent_teams/interaction/human_agent_inbox.py)
// 从 interaction 包提升到 schema 包，避免 tools → interaction 循环依赖。
type UnknownHumanAgentError struct {
	// Sender 尝试使用的发送者名
	Sender string
	// Registered 已注册的 human-agent 成员名列表
	Registered []string
}

// ──────────────────────────── 枚举 ────────────────────────────

// TeamLifecycle 团队生命周期模式。
type TeamLifecycle string

// TeamRole 团队角色枚举。
type TeamRole string

const (
	// TeamLifecycleTemporary 临时团队
	TeamLifecycleTemporary TeamLifecycle = "temporary"
	// TeamLifecyclePersistent 持久团队
	TeamLifecyclePersistent TeamLifecycle = "persistent"
	// TeamRoleLeader Leader 角色
	TeamRoleLeader TeamRole = "leader"
	// TeamRoleTeammate Teammate 角色
	TeamRoleTeammate TeamRole = "teammate"
	// TeamRoleHumanAgent 人类代理角色（有 SDK 头像，由外部控制者驱动）
	TeamRoleHumanAgent TeamRole = "human_agent"
	// TeamRolePassiveHuman 无头像人类成员（无 harness / 无 LLM / 不参与协调循环，
	// 只是名册身份 + 消息总线地址，消息直转给控制者）
	TeamRolePassiveHuman TeamRole = "passive_human"
	// TeamRoleBridgeAgent 桥接头像角色（本地 LLM 作调度，具体产出由远端 agent 提供）
	TeamRoleBridgeAgent TeamRole = "bridge_agent"
	// TeamRoleWorker swarmflow 执行角色（单次无状态，用完即弃，不进协调循环）
	TeamRoleWorker TeamRole = "worker"
	// TeamRoleExternalCLI 外部 CLI 成员（由第三方 CLI 子进程驱动，但完整参与协调循环）
	TeamRoleExternalCLI TeamRole = "external_cli"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsCoordinatedMember 该角色是否作为自主成员参与协调循环。
// TEAMMATE 与 EXTERNAL_CLI 都会轮询邮箱、认领任务、对停滞的已认领任务自我催促，
// 并接收团队的审批/worktree 策略；其余角色不是协调成员。
// 原本以「== TEAMMATE」代替「是否自主参与成员」的判定都应改用本方法，
// 这样下一个协调角色出现时不会静默漏出这些路径。
// Python: TeamRole.is_coordinated_member (schema/team.py L150-159)
func (r TeamRole) IsCoordinatedMember() bool {
	return r == TeamRoleTeammate || r == TeamRoleExternalCLI
}

// NewMemberOpResultSuccess 创建成功结果。
func NewMemberOpResultSuccess() MemberOpResult { return MemberOpResult{OK: true} }

// NewMemberOpResultFail 创建失败结果。
func NewMemberOpResultFail(reason string) MemberOpResult {
	return MemberOpResult{OK: false, Reason: reason}
}

// Error 实现 error 接口
func (e *UnknownHumanAgentError) Error() string {
	sorted := make([]string, len(e.Registered))
	copy(sorted, e.Registered)
	sort.Strings(sorted)
	return fmt.Sprintf("'%s' is not a registered human-agent member; registered members: %v",
		e.Sender, sorted)
}
