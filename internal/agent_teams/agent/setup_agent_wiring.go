package agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// deriveMemberBuildContext 派生每成员的构建上下文。
//
// 上游 agent_configurator.py:826-852：
//   - 有平台传入的 build_context 时 derive（member_name / role / language /
//     member_card_id / project_dir），并**重绑 extras**（成员之间不共享句柄）
//   - 否则 synthesize 一个最小 BuildContext
//
// ⤵️ Go 侧 TeamAgentSpec 目前没有 build_context 字段（上游 :626/:835 有），
// 故本批走 else 分支自建。字段补齐后把 spec 的 context 传进第一个参数即可。
func deriveMemberBuildContext(
	platformCtx atschema.BuildContextInterface,
	ctx atschema.TeamRuntimeContext,
	resolvedLanguage string,
	memberCardID string,
) atschema.BuildContextInterface {
	if platformCtx != nil {
		derived := platformCtx.Derive(atschema.BuildContextOverrides{
			MemberName:   strPtr(ctx.MemberName),
			Role:         strPtr(string(ctx.Role)),
			Language:     strPtr(resolvedLanguage),
			MemberCardID: strPtr(memberCardID),
		})
		// 重绑 extras：derive 是浅拷贝，不重绑会让成员共享同一句柄字典。
		// Python: member_build_context.extras = dict(member_build_context.extras)
		extras := derived.GetExtras()
		rebound := make(map[string]any, len(extras))
		for k, v := range extras {
			rebound[k] = v
		}
		if concrete, ok := derived.(*atschema.BuildContext); ok {
			concrete.Extras = rebound
			return concrete
		}
		return derived
	}
	return &atschema.BuildContext{
		Language:     resolvedLanguage,
		MemberName:   ctx.MemberName,
		Role:         string(ctx.Role),
		MemberCardID: memberCardID,
	}
}

// buildTeamHandles 收集 17 个团队活句柄。
//
// 对齐上游 agent_configurator.py:910-928 的 inject_team_handles(...) 实参。
// swarmflow / reliability / expert 三个域在 Go 侧未迁（各字段保持零值），
// 见 rails.TeamHandles 的 ⤵️ 标注。
func buildTeamHandles(
	c *AgentConfigurator,
	ctx atschema.TeamRuntimeContext,
	agentSpec atschema.DeepAgentSpec,
) rails.TeamHandles {
	return rails.TeamHandles{
		TeamBackend:      c.TeamBackend(),
		WorkspaceManager: c.WorkspaceManager(),
		ModelAllocator:   c.ModelAllocator(),
		Messager:         c.Messager(),
		// ⤵️ ctx.PermissionsOverride 在 Go 侧 TeamRuntimeContext 无此字段
		// （上游 agent_configurator.py:915 有），本批不臆造，留零值。
		WorktreeManager:  c.WorktreeManager(),
		TaskEventTracker: c.TaskEventTracker(),
	}
}

// buildTeamRailSpecInput 把配置器状态摊成 TeamRailSpecs 的输入。
//
// 对齐上游 agent_configurator.py:723-800 的 team_rail_specs 构造输入。
func buildTeamRailSpecInput(
	spec atschema.TeamAgentSpec,
	ctx atschema.TeamRuntimeContext,
	agentSpec atschema.DeepAgentSpec,
	resolvedLanguage string,
	isTeamPlanLeader bool,
	approvalTools []string,
	isCoordinatedTeammate bool,
	c *AgentConfigurator,
) rails.TeamRailSpecInput {
	teamName := ""
	leaderMemberName := ""
	if ctx.TeamSpec != nil {
		teamName = ctx.TeamSpec.TeamName
		leaderMemberName = ctx.TeamSpec.LeaderMemberName
	}
	return rails.TeamRailSpecInput{
		Role:             string(ctx.Role),
		MemberName:       ctx.MemberName,
		Language:         resolvedLanguage,
		TeammateMode:     string(spec.TeammateMode),
		Lifecycle:        string(spec.Lifecycle),
		TeamMode:         spec.TeamMode,
		TeamName:         teamName,
		LeaderMemberName: leaderMemberName,
		// ⤵️ ctx.Prompt / DisplayName / ForkSource 及 team_outputs_dir 在 Go 侧
		// TeamRuntimeContext 无对应字段（上游 :687-689/:737-752 有），本批留零值。
		BasePrompt:            agentSpec.SystemPrompt,
		ExcludeTools:          predefinedExcludeTools(spec),
		QualifyIDs:            spec.SpawnMode == "inprocess",
		ApprovalTools:         approvalTools,
		SwarmflowEnabled:      spec.SwarmflowEnabled,
		HasWorkspaceManager:   c.WorkspaceManager() != nil,
		IsTeamPlanLeader:      isTeamPlanLeader,
		IsCoordinatedTeammate: isCoordinatedTeammate,
		HasTeamBackend:        c.TeamBackend() != nil,
		HasMessager:           c.Messager() != nil,
	}
}

// predefinedExcludeTools 预定义团队要剔除动态 spawn 工具。
//
// 上游 agent_configurator.py:722-727：predefined 模式下把
// spawn_teammate / spawn_human_agent / spawn_bridge_agent 从 leader 工具集剔除。
func predefinedExcludeTools(spec atschema.TeamAgentSpec) []string {
	if spec.TeamMode != "predefined" {
		return nil
	}
	return []string{"spawn_teammate", "spawn_human_agent", "spawn_bridge_agent"}
}

// strPtr 返回字符串指针（BuildContextOverrides 用）。
func strPtr(s string) *string { return &s }

// cardIDOf 取卡片 id（nil 安全）。
func cardIDOf(card *agentschema.AgentCard) string {
	if card == nil {
		return ""
	}
	return card.GetID()
}
