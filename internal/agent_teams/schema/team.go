package schema

// ──────────────────────────── 结构体 ────────────────────────────

// MemberOpResult 团队成员操作结果。
type MemberOpResult struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

// TeamCompletionSnapshot 团队完成时的快照计数。
type TeamCompletionSnapshot struct {
	MemberCount int `json:"member_count"`
	TaskCount   int `json:"task_count"`
}

// TeamMemberSpec 预定义成员的声明式输入。
type TeamMemberSpec struct {
	MemberName  string  `json:"member_name"`
	DisplayName string  `json:"display_name"`
	RoleType    TeamRole `json:"role_type"`
	Persona     string  `json:"persona"`
	PromptHint  string  `json:"prompt_hint,omitempty"`
	ModelName   string  `json:"model_name,omitempty"`
}

// TeamSpec 团队定义与目标。
type TeamSpec struct {
	TeamName          string         `json:"team_name"`
	DisplayName       string         `json:"display_name"`
	LeaderMemberName  string         `json:"leader_member_name"`
	Language          string         `json:"language,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	// ModelPool LLM 端点池（运行时为 []models.ModelPoolEntry）
	ModelPool         any            `json:"model_pool,omitempty"`
	ModelPoolStrategy string         `json:"model_pool_strategy,omitempty"`
}

// TeamRuntimeContext 单个团队成员的轻量运行时上下文。
type TeamRuntimeContext struct {
	Role          TeamRole        `json:"role"`
	MemberName    string          `json:"member_name"`
	Persona       string          `json:"persona"`
	TeamSpec      *TeamSpec       `json:"team_spec,omitempty"`
	MessagerConfig any            `json:"messager_config,omitempty"`
	DBConfig      any             `json:"db_config,omitempty"`
	MemberModel   *TeamModelConfig `json:"member_model,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// TeamLifecycle 团队生命周期模式。
type TeamLifecycle string

const (
	TeamLifecycleTemporary  TeamLifecycle = "temporary"
	TeamLifecyclePersistent TeamLifecycle = "persistent"
)

// TeamRole 团队角色枚举。
type TeamRole string

const (
	TeamRoleLeader     TeamRole = "leader"
	TeamRoleTeammate   TeamRole = "teammate"
	TeamRoleHumanAgent TeamRole = "human_agent"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberOpResultSuccess 创建成功结果。
func NewMemberOpResultSuccess() MemberOpResult { return MemberOpResult{OK: true} }

// NewMemberOpResultFail 创建失败结果。
func NewMemberOpResultFail(reason string) MemberOpResult { return MemberOpResult{OK: false, Reason: reason} }
