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
		TeamBackend:         c.TeamBackend(),
		WorkspaceManager:    c.WorkspaceManager(),
		ModelAllocator:      c.ModelAllocator(),
		Messager:            c.Messager(),
		PermissionsOverride: ctx.PermissionsOverride,
		WorktreeManager:     c.WorktreeManager(),
		TaskEventTracker:    c.TaskEventTracker(),
		// 成员创建回调：TeamBackend.Startup 以参数接收（上游由 backend 自带），
		// 不经句柄传就会让 autoStartMembers 直接返回、成员永不启动（F-02）。
		OnTeammateCreated: c.onTeammateCreated,
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
	// 团队名优先取配置器持有的（旧路径 c.TeamName()），TeamSpec 兜底——
	// ctx.TeamSpec 可能为 nil（leader 首轮建队前），只读它会得到空团队名，
	// 让 TeamToolRail 的工具名/主题全部错位。
	teamName := c.TeamName()
	leaderMemberName := ""
	if ctx.TeamSpec != nil {
		if ctx.TeamSpec.TeamName != "" {
			teamName = ctx.TeamSpec.TeamName
		}
		leaderMemberName = ctx.TeamSpec.LeaderMemberName
	}
	return rails.TeamRailSpecInput{
		Role:         string(ctx.Role),
		MemberName:   ctx.MemberName,
		Language:     resolvedLanguage,
		TeammateMode: string(spec.TeammateMode),
		Lifecycle:    string(spec.Lifecycle),
		// 三个 mode 必须走与旧手工挂载路径相同的解析函数：
		// 留空会让 provider 落到自己的默认值，spawn 工具行为随之改变。
		TeamMode:         resolveTeamMode(spec),
		DispatchMode:     resolveDispatchMode(spec),
		BootstrapMode:    resolveBootstrapMode(spec),
		TeamName:         teamName,
		LeaderMemberName: leaderMemberName,
		// ⤵️ ctx.Prompt / DisplayName / ForkSource 及 team_outputs_dir 在 Go 侧
		// TeamRuntimeContext 无对应字段（上游 :687-689/:737-752 有），本批留零值。
		BasePrompt:   agentSpec.SystemPrompt,
		ExcludeTools: predefinedExcludeTools(spec),
		// 上游 agent_configurator.py:739 写的是 spawn_mode == "inprocess"，
		// 但 team_tool_rail.py:193-208 已明确废弃 qualify_team_tool_ids：
		// 现在走 ability_manager.add_ability，由框架把 id 限定成
		// `{name}_{owner_id}`，"superseding the old qualify_ids suffixing"，
		// 并注明旧的 bespoke 路径（qualify + add_tool + ability_manager.add）
		// 产出的 team.<tool>.<session>.<member> id 与 teardown_tools 不匹配、
		// 会跨 native 重建泄漏。
		//
		// Go 侧 TeamToolRail.Init 仍是那条 bespoke 路径（无 AddAbility），
		// 打开后缀会让工具按限定 id 注册而按原名调用 → 结果为空。
		// 故此处关闭，等 F-03 把注册改为 add_ability 后即可去掉本字段。
		QualifyIDs:            false,
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
