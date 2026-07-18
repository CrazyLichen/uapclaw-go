package schema

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
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
	// TeamSpec 团队规格（可选）
	TeamSpec *TeamSpec `json:"team_spec,omitempty"`
	// MessagerConfig 消息传输配置（可选）
	MessagerConfig *messager.MessagerTransportConfig `json:"messager_config,omitempty"`
	// DBConfig 数据库配置
	DBConfig database.DBConfigProvider `json:"db_config,omitempty"`
	// MemberModel 成员模型配置（可选）
	MemberModel *TeamModelConfig `json:"member_model,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// TeamLifecycle 团队生命周期模式。
type TeamLifecycle string

const (
	// TeamLifecycleTemporary 临时团队
	TeamLifecycleTemporary TeamLifecycle = "temporary"
	// TeamLifecyclePersistent 持久团队
	TeamLifecyclePersistent TeamLifecycle = "persistent"
)

// TeamRole 团队角色枚举。
type TeamRole string

const (
	// TeamRoleLeader Leader 角色
	TeamRoleLeader TeamRole = "leader"
	// TeamRoleTeammate Teammate 角色
	TeamRoleTeammate TeamRole = "teammate"
	// TeamRoleHumanAgent 人类代理角色
	TeamRoleHumanAgent TeamRole = "human_agent"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberOpResultSuccess 创建成功结果。
func NewMemberOpResultSuccess() MemberOpResult { return MemberOpResult{OK: true} }

// NewMemberOpResultFail 创建失败结果。
func NewMemberOpResultFail(reason string) MemberOpResult {
	return MemberOpResult{OK: false, Reason: reason}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
