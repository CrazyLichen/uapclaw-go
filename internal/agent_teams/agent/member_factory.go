package agent

import (
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateMemberHandle 创建 TeamMember 句柄，当 backend 缺失时返回 nil。
// 对齐 Python: create_member_handle(member_name, blueprint, infra, agent_card)
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
	// ⤵️ 回填: 9.57 — infra.TeamBackend 为 nil 时返回 nil
	// ⤵️ 回填: 9.57 — 从 infra.TeamBackend 获取 db/messager/team_name 构造 TeamMember
	if infra.TeamBackend == nil {
		return nil
	}
	return &TeamMember{
		MemberName: memberName,
		DisplayName: memberName,
		AgentCard:  agentCard,
		Desc:       blueprint.Ctx.Persona,
	}
}
