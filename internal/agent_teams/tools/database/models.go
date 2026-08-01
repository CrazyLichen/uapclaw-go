package database

// ──────────────────────────── 结构体 ────────────────────────────

// Team 团队信息模型。
// 对齐 Python: Team (openjiuwen/agent_teams/tools/models.py)
// 静态表 team_info 的行模型。
type Team struct {
	// TeamName 团队名称（主键）
	TeamName string `json:"team_name"`
	// DisplayName 显示名称
	DisplayName string `json:"display_name"`
	// LeaderMemberName Leader 成员名
	LeaderMemberName string `json:"leader_member_name"`
	// Desc 团队描述
	Desc string `json:"desc,omitempty"`
	// Prompt 团队提示词
	Prompt string `json:"prompt,omitempty"`
	// Created 创建时间（毫秒时间戳）
	Created int64 `json:"created"`
	// UpdatedAt 更新时间（毫秒时间戳，仅 roster 变更时 bump）
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

// TeamMember 团队成员模型。
// 对齐 Python: TeamMember (openjiuwen/agent_teams/tools/models.py)
// 静态表 team_member 的行模型，复合主键 (member_name, team_name)。
type TeamMember struct {
	// MemberName 成员名称（主键）
	MemberName string `json:"member_name"`
	// TeamName 团队名称（主键，外键 team_info.team_name）
	TeamName string `json:"team_name"`
	// DisplayName 显示名称
	DisplayName string `json:"display_name"`
	// Desc 成员描述
	Desc string `json:"desc,omitempty"`
	// AgentCard Agent 卡片 JSON 字符串（存储 AgentCard.model_dump_json()）
	AgentCard string `json:"agent_card"`
	// Status 成员状态（MemberStatus 枚举值）
	Status string `json:"status"`
	// ExecutionStatus 执行状态（ExecutionStatus 枚举值）
	ExecutionStatus string `json:"execution_status,omitempty"`
	// Mode 成员模式（MemberMode 枚举值：build_mode / plan_mode）
	Mode string `json:"mode"`
	// Role 成员角色（TeamRole 枚举值：leader / teammate / human_agent）
	Role string `json:"role"`
	// Prompt 成员专属提示词
	Prompt string `json:"prompt,omitempty"`
	// ModelRefJSON 模型引用 JSON（{"model_id": str, "model_name": str}）
	ModelRefJSON string `json:"model_ref_json,omitempty"`
	// UpdatedAt 更新时间（毫秒时间戳，仅 roster 变更时 bump，status 变更不 bump）
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// TeamDynamicTablePrefixes 动态表名前缀（用于识别和清理）。
	// 对齐 Python: TEAM_DYNAMIC_TABLE_PREFIXES
	TeamDynamicTablePrefixes = [...]string{
		"team_task_dependency_",
		"team_task_",
		"team_message_",
		"message_read_status_",
	}
	// TeamStaticTablesToClear 需要清空的静态表名。
	// 对齐 Python: TEAM_STATIC_TABLES_TO_CLEAR
	TeamStaticTablesToClear = [...]string{
		"team_info",
		"team_member",
	}
)
