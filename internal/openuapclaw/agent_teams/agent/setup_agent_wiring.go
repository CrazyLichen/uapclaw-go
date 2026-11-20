package agent

import (
	experts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/experts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails"
	reliability "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability"
	reliabilitycore "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/core"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
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

// buildReliability 构造成员的可靠性组件与轨参数（门卫通过时）。
//
// 门卫对齐上游 agent_configurator.py:790-804：spec.reliability.enabled +
// member_name 非空 + 角色值 ∈ monitor_roles +（leader 或有 messager）。
// 有状态核心在此构建一次（extras 的单写者），每个 native 周期把它包进一条
// 新 rail——detector 窗口 / leader sink 跨运行周期存活。
//
// Python: agent_configurator.py:790-821 reliability 段。
func buildReliability(
	spec atschema.TeamAgentSpec,
	ctx atschema.TeamRuntimeContext,
	c *AgentConfigurator,
) (*reliability.ReliabilityComponents, bool) {
	cfg := spec.Reliability
	if cfg == nil || !cfg.Enabled || ctx.MemberName == "" {
		return nil, false
	}
	isLeader := ctx.Role == atschema.TeamRoleLeader
	roleIn := false
	for _, r := range cfg.MonitorRoles {
		if r == string(ctx.Role) {
			roleIn = true
			break
		}
	}
	if !roleIn || (!isLeader && c.Messager() == nil) {
		return nil, false
	}
	components := reliability.BuildReliabilityComponents(reliability.BuildComponentsDeps{
		Config:     cfg,
		MemberName: ctx.MemberName,
		Messager:   c.Messager(),
		TeamName:   c.TeamName(),
		SenderId:   ctx.MemberName,
		IsLeader:   isLeader,
	})
	return components, isLeader
}

// buildTeamHandles 收集 17 个团队活句柄。
//
// 对齐上游 agent_configurator.py:910-928 的 inject_team_handles(...) 实参。
// swarmflow / expert 域在 Go 侧未迁（各字段保持零值），
// 见 rails.TeamHandles 的 ⤵️ 标注。reliability 域已随 25.1g 落地。
func buildTeamHandles(
	c *AgentConfigurator,
	ctx atschema.TeamRuntimeContext,
	agentSpec atschema.DeepAgentSpec,
	reliabilityComponents *reliability.ReliabilityComponents,
	teamSpec *atschema.TeamAgentSpec,
) rails.TeamHandles {
	// Python: agent_configurator.py:892-899——host 注入的专家 catalog 支撑
	// leader 的专家工具；成员侧不走 catalog 门控，恒 nil。
	var expertCatalog experts.ExpertCatalog
	var expertBinding *rails.ExpertBinding
	if ctx.Role == atschema.TeamRoleLeader {
		if catalog := experts.GetProcessExpertCatalog(); catalog != nil {
			expertCatalog = catalog
			expertBinding = deriveExpertBinding(teamSpec)
		}
	}
	return rails.TeamHandles{
		TeamBackend:           c.TeamBackend(),
		WorkspaceManager:      c.WorkspaceManager(),
		ModelAllocator:        c.ModelAllocator(),
		Messager:              c.Messager(),
		PermissionsOverride:   ctx.PermissionsOverride,
		WorktreeManager:       c.WorktreeManager(),
		TaskEventTracker:      c.TaskEventTracker(),
		ReliabilityComponents: reliabilityComponents,
		ExpertCatalog:         expertCatalog,
		ExpertBinding:         expertBinding,
	}
}

// deriveExpertBinding leader 的 spawn_expert 所需的团队 spec 数据。
// Python: _derive_expert_binding (agent_configurator.py:196-207)
func deriveExpertBinding(spec *atschema.TeamAgentSpec) *rails.ExpertBinding {
	if spec == nil {
		return nil
	}
	base, ok := spec.Agents["teammate"]
	if !ok {
		base, ok = spec.Agents["leader"]
	}
	personaWorkspace := ""
	personaLanguage := ""
	if ok {
		if base.Workspace != nil {
			personaWorkspace = base.Workspace.RootPath
		}
		personaLanguage = base.Language
	}
	return &rails.ExpertBinding{
		BoundGroup:       experts.BoundAgentGroupOf(spec),
		PersonaWorkspace: personaWorkspace,
		PersonaLanguage:  personaLanguage,
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
	reliabilityComps *reliability.ReliabilityComponents,
	reliabilityIsLeader bool,
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
		// F-03 已修：注册改走 ability_manager.add_ability，QualifyIDs 字段已删除。
		ApprovalTools:         approvalTools,
		SwarmflowEnabled:      spec.SwarmflowEnabled,
		HasWorkspaceManager:   c.WorkspaceManager() != nil,
		IsTeamPlanLeader:      isTeamPlanLeader,
		IsCoordinatedTeammate: isCoordinatedTeammate,
		HasTeamBackend:        c.TeamBackend() != nil,
		HasMessager:           c.Messager() != nil,
		// 门卫通过时透传可靠性配置（RailSpec.params 的 reliability_cfg）
		Reliability:         reliabilityCompsInput(spec, reliabilityComps),
		ReliabilityIsLeader: reliabilityIsLeader,
	}
}

// reliabilityCompsInput 门卫通过时返回 spec.Reliability（nil = 不出 reliability 轨）。
func reliabilityCompsInput(spec atschema.TeamAgentSpec, comps *reliability.ReliabilityComponents) *reliabilitycore.ReliabilityConfig {
	if comps == nil {
		return nil
	}
	return spec.Reliability
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
