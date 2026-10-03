package agent

import (
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateMemberHandle 创建 TeamMember 句柄，当 backend 缺失时返回 nil。
// Python: create_member_handle(member_name, blueprint, infra, agent_card)
//
// 纯构造函数：仅需已绑定的 team_backend（setup_infra 为所有角色提供了它），
// 从不触碰数据库。因此在 configure() 期间每个 Agent 调用一次——
// _setup_agent 中——Leader/Teammate/HumanAgent 一视同仁。
//
// Leader 自己的 DB 行在 BuildTeamTool 运行后才物化，
// 所以新构建的 Leader 持有的句柄对应的行还不存在。
// TeamMember 容忍缺失的行（status 读返回 None，写静默返回 False），
// 因此无需推迟构造直到行存在。
func CreateMemberHandle(
	memberName string,
	blueprint *TeamAgentBlueprint,
	infra *TeamInfra,
	agentCard *agentschema.AgentCard,
) *TeamMember {
	if infra.TeamBackend == nil {
		return nil
	}
	return &TeamMember{
		MemberName:  memberName,
		TeamName:    infra.TeamBackend.TeamName(),
		DisplayName: memberName,
		AgentCard:   agentCard,
		DB:          infra.TeamBackend.DB(),
		// Messager 通过 AgentConfigurator.Messager() 单独注入，
		// TeamBackend 未暴露 messager.Messager 接口
		Desc: blueprint.Ctx.Persona,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
