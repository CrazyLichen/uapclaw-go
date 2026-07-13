package agent

import (
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamAgentBlueprint TeamAgent 不可变装配蓝图。
// 四象限分解的第一象限：构造时确定、生命周期内不可变。
// 对齐 Python: TeamAgentBlueprint (openjiuwen/agent_teams/agent/blueprint.py)
type TeamAgentBlueprint struct {
	// Card Agent 身份卡片
	Card *agentschema.AgentCard `json:"card,omitempty"`
	// Spec 团队 Agent 规格
	Spec atschema.TeamAgentSpec `json:"spec"`
	// Ctx 运行时上下文
	Ctx atschema.TeamRuntimeContext `json:"ctx"`
	// RolePolicy 角色策略
	RolePolicy string `json:"role_policy,omitempty"`
	// Language 语言偏好
	Language string `json:"language,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Role 返回团队角色。
// 对齐 Python: TeamAgentBlueprint.role property
func (b *TeamAgentBlueprint) Role() atschema.TeamRole {
	return b.Ctx.Role
}

// MemberName 返回成员名，nil 表示未设置。
// 对齐 Python: TeamAgentBlueprint.member_name property → Optional[str]
func (b *TeamAgentBlueprint) MemberName() *string {
	if b.Ctx.MemberName == "" {
		return nil
	}
	return &b.Ctx.MemberName
}

// Lifecycle 返回生命周期模式。
// 对齐 Python: TeamAgentBlueprint.lifecycle property
func (b *TeamAgentBlueprint) Lifecycle() atschema.TeamLifecycle {
	return b.Spec.Lifecycle
}

// TeamSpec 返回团队规格。
// 对齐 Python: TeamAgentBlueprint.team_spec property → Optional[TeamSpec]
func (b *TeamAgentBlueprint) TeamSpec() *atschema.TeamSpec {
	return b.Ctx.TeamSpec
}
