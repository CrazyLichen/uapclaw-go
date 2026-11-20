package schema

import (
	"fmt"
	hschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tools/database"
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
	// ExternalCLI 预定义外部 CLI 成员的启动配置；nil 表示普通成员。
	// Python 侧 ExternalCliMemberSpec 是 predefined_members union 的独立子类
	// （team.py:536）；Go 以可选字段承载（偏差 D-15.16-b12：pydantic union →
	// 可选字段，member_name 复用 TeamMemberSpec.MemberName）。
	ExternalCLI *ExternalCliAgentSpec `json:"external_cli,omitempty"`
}

// TeamSpec 团队定义与目标。
type TeamSpec struct {
	// TeamName 团队名
	TeamName string `json:"team_name"`
	// DisplayName 显示名
	DisplayName string `json:"display_name"`
	// ConversationMode 会话形态："team" 或 "group_chat"。
	// Python: conversation_mode (team.py:552)
	ConversationMode string `json:"conversation_mode,omitempty"`
	// LeaderMemberName Leader 成员名
	LeaderMemberName string `json:"leader_member_name"`
	// Language 团队语言（可选）
	Language string `json:"language,omitempty"`
	// DispatchMode 任务如何到达成员（"autonomous"/"scheduled"）。
	// 随 ctx 分发到外部 CLI 成员：member-scope 连接据此解析工具集，
	// 与 system prompt 在 spawn 时的渲染模式保持一致。
	// Python: dispatch_mode (team.py:556)
	DispatchMode string `json:"dispatch_mode,omitempty"`
	// EnableMemberMessaging scheduled 成员是否可直接给同伴发消息（F_126）。
	// Python: enable_member_messaging (team.py:563)
	EnableMemberMessaging bool `json:"enable_member_messaging,omitempty"`
	// MaxSubtaskDepth scheduled 成员的子任务拆分深度上限（F_128）。
	// Python: max_subtask_depth (team.py:569)
	MaxSubtaskDepth int `json:"max_subtask_depth,omitempty"`
	// TeammateMode 队友执行任务模式（"build_mode"/"plan_mode"）。
	// Python: teammate_mode (team.py:575)
	TeammateMode MemberMode `json:"teammate_mode,omitempty"`
	// Metadata 元数据（可选）
	Metadata map[string]any `json:"metadata,omitempty"`
	// ModelPool LLM 端点池
	ModelPool []models.ModelPoolEntry `json:"model_pool,omitempty"`
	// ModelPoolStrategy 模型池分配策略
	ModelPoolStrategy string `json:"model_pool_strategy,omitempty"`
	// ExternalMessagerConfig 外部 CLI 成员使用的消息传输配置（可选）。
	// descriptor_from_context 优先取它，其次取 ctx.MessagerConfig，最后回落默认。
	// Python: external_messager_config (team.py:621)
	ExternalMessagerConfig *MessagerTransportConfig `json:"external_messager_config,omitempty"`
	// Workspace 共享工作空间配置（从 TeamAgentSpec 镜像到运行时摘要）。
	// Python: workspace (team.py:623，dict[str,Any]；Go 用具体类型承接）
	Workspace *team_workspace.TeamWorkspaceConfig `json:"workspace,omitempty"`
	// EvolutionEnabled 自演进覆盖开关（从 TeamAgentSpec 镜像）。
	// Python: evolution_enabled (team.py:625)
	EvolutionEnabled *bool `json:"evolution_enabled,omitempty"`
	// MemberWorkspacePrefix 成员工作区前缀开关（从 TeamAgentSpec 镜像）。
	// Python: member_workspace_prefix (team.py:628)
	MemberWorkspacePrefix *bool `json:"member_workspace_prefix,omitempty"`
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
	// AgentTemplateSpec 专家孵化成员的能力快照（DB options.agent_template
	// → BuildContextFromDB 恢复 → resolve_agent_spec 的 apply_template_snapshot
	// 挂载）。native 成员非空；外部 CLI 成员为 nil（其启动配置在
	// TemplateExternalCLI）。
	// Python: TeamRuntimeContext.agent_template_spec (spawn_manager.py:568)
	AgentTemplateSpec *hschema.AgentTemplateSpec `json:"agent_template_spec,omitempty"`
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
	PermissionsOverride map[string]string `json:"permissions_override,omitempty"`
	// CLIAgent 外部 CLI 成员的后端标识（可选，空表示本地 DeepAgent）。
	// 取值："claude"/"codex"（SDK 后端）、内置 adapter 名（"openclaw"/
	// "hermes"/"gemini"）、或第三方 harness provider 名（如 "dsh"）。
	// Python: TeamRuntimeContext.cli_agent (team.py:697)
	CLIAgent string `json:"cli_agent,omitempty"`
	// TeamSpec 团队规格（可选）
	TeamSpec *TeamSpec `json:"team_spec,omitempty"`
	// MessagerConfig 消息传输配置（可选）
	MessagerConfig *MessagerTransportConfig `json:"messager_config,omitempty"`
	// DBConfig 数据库配置
	DBConfig database.DBConfigProvider `json:"db_config,omitempty"`
	// MemberModel 成员模型配置（可选）
	// Python: member_model (team.py:676)
	MemberModel *models.TeamModelConfig `json:"member_model,omitempty"`
	// FallbackMemberModel 预留给原生外部 CLI 认证回退的成员模型配置（可选）。
	// Python: fallback_member_model (team.py:678)
	FallbackMemberModel *models.TeamModelConfig `json:"fallback_member_model,omitempty"`
	// BuiltinModel 外部 CLI 成员用自身登录运行的内置模型与 effort（可选）。
	// 来自 TeamMember.options.builtin_model；不带 endpoint，优先级高于
	// 池分配与静态 model_config。
	// Python: builtin_model (team.py:680)
	BuiltinModel *ExternalCliModelConfig `json:"builtin_model,omitempty"`
	// TemplateExternalCli 从专家模板生成的外部 CLI 成员的启动配置（可选）。
	// 从 TeamMember.options.agent_template.external_cli 回读；外部 CLI spawn
	// 路径优先于团队对同一 CLI kind 的声明配置。
	// Python: template_external_cli (team.py:750)
	TemplateExternalCli *ExternalCliAgentSpec `json:"template_external_cli,omitempty"`
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
