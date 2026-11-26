package team

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/swarmsnapshot"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/providers"
)

// evolution_rails_wiring.go —— evolution rail 的 TeamManager 注册缝（team 包内实现）。
//
// Python: SwarmTeamSkillEvolutionRail/SwarmTeamSkillCreateRail/
// SwarmMemberSkillEvolutionRail 的 init(agent) 自注册段（evolution_rails.py:246-287/
// 324-358/404-455）。偏差 D-15.24c-3 的注入侧：providers↔team 成环，
// providers 只持有 registrar 回调；本文件在 team 包实现该回调
//（team → providers 无环；team 包 init 幂等注入）。

// logComponent 日志组件（swarm 父包装配系）。
const evolutionWiringLogComponent = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// evolutionRegistrarOnce 幂等注入标记。
var evolutionRegistrarOnce sync.Once

// EnsureEvolutionRailRegistrarWired 幂等注入注册回调（包 init 调用；
// team → providers 无环，providers 侧持有回调本体）。
func EnsureEvolutionRailRegistrarWired() {
	evolutionRegistrarOnce.Do(func() {
		// leader 团队技能演进 rail：登记 live/skill rail + 反向 bind 创建 rail
		// 的认卡/取件方法值 + consume watcher 标记 + 挂载上下文。
		// Python: SwarmTeamSkillEvolutionRail.init（evolution_rails.py:246-287）。
		providers.SetTeamEvolutionRailRegistrar(func(snapshot *swarmsnapshot.Snapshot, agent agentinterfaces.BaseAgent, swarmRail *providers.SwarmTeamSkillEvolutionRail) {
			teamManager := GetTeamManager(snapshot.Channel)
			if teamManager == nil {
				return
			}
			deepAgent := deepOfAgent(agent)
			workspace := NewTeamWorkspaceInfo(snapshot.TeamWsRoot, snapshot.SkillsDir, snapshot.TeamID, snapshot.Config, snapshot.Language)
			workspace.MountContext = NewTeamRailMountContext(deepAgent, snapshot.Role, snapshot.Channel, snapshot.Language, workspace)
			teamManager.RegisterTeamLiveRail(snapshot.SessionID, deepAgent, swarmRail)
			teamManager.RegisterTeamSkillRail(snapshot.SessionID, swarmRail.TeamSkillEvolutionRail)
			// Python: bind_review_feedback_skill_create_rail(get_team_skill_create_rail)（:259-261）
			if createRail := teamManager.GetTeamSkillCreateRail(snapshot.SessionID); createRail != nil {
				swarmRail.TeamSkillEvolutionRail.BindReviewFeedbackSkillCreateRail(
					createRail.OwnsExternalProposal, createRail.ResolveExternalProposal)
			}
			// Python: consume_team_evolution_watcher_deferred（:262-271）
			//（G2 裁决：consume 面随本批补；watcher 本体挂 15.24d，true 时空转日志）
			if teamManager.ConsumeTeamEvolutionWatcherDeferred(snapshot.SessionID) {
				logger.Info(evolutionWiringLogComponent).
					Str("session_id", snapshot.SessionID).
					Msg("[swarm.team_skill_evolution] deferred watcher consume（watcher 本体 ⤴️15.24d）")
			}
			teamManager.RegisterTeamRailContext(snapshot.SessionID, workspace.MountContext)
			logger.Info(evolutionWiringLogComponent).
				Str("session_id", snapshot.SessionID).
				Str("skills_dir", snapshot.SkillsDir).
				Msg("[swarm.team_skill_evolution] registered live rail")
		})
		// leader 团队技能创建 rail：登记 live/create rail + 反向 bind 自身 +
		// 挂载上下文。Python: SwarmTeamSkillCreateRail.init（:324-358）。
		providers.SetTeamCreateRailRegistrar(func(snapshot *swarmsnapshot.Snapshot, agent agentinterfaces.BaseAgent, swarmRail *providers.SwarmTeamSkillCreateRail) {
			teamManager := GetTeamManager(snapshot.Channel)
			if teamManager == nil {
				return
			}
			deepAgent := deepOfAgent(agent)
			workspace := NewTeamWorkspaceInfo(snapshot.TeamWsRoot, snapshot.SkillsDir, snapshot.TeamID, snapshot.Config, snapshot.Language)
			workspace.MountContext = NewTeamRailMountContext(deepAgent, snapshot.Role, snapshot.Channel, snapshot.Language, workspace)
			teamManager.RegisterTeamLiveRail(snapshot.SessionID, deepAgent, swarmRail)
			teamManager.RegisterTeamSkillCreateRail(snapshot.SessionID, swarmRail.TeamSkillCreateRail)
			// Python: team_rail.bind_review_feedback_skill_create_rail(self)（:337-344）
			if teamRail := teamManager.GetTeamSkillRail(snapshot.SessionID); teamRail != nil {
				teamRail.BindReviewFeedbackSkillCreateRail(
					swarmRail.TeamSkillCreateRail.OwnsExternalProposal,
					swarmRail.TeamSkillCreateRail.ResolveExternalProposal)
			}
			teamManager.RegisterTeamRailContext(snapshot.SessionID, workspace.MountContext)
			logger.Info(evolutionWiringLogComponent).
				Str("session_id", snapshot.SessionID).
				Msg("[swarm.team_skill_create] registered live rail")
		})
		// teammate 成员技能演进 rail：登记 live/member rail + member 挂载上下文。
		// Python: SwarmMemberSkillEvolutionRail.init（:404-455）。
		providers.SetMemberEvolutionRailRegistrar(func(snapshot *swarmsnapshot.Snapshot, agent agentinterfaces.BaseAgent, swarmRail *providers.SwarmMemberSkillEvolutionRail) {
			teamManager := GetTeamManager(snapshot.Channel)
			if teamManager == nil {
				return
			}
			deepAgent := deepOfAgent(agent)
			workspace := NewTeamWorkspaceInfo(snapshot.TeamWsRoot, snapshot.SkillsDir, snapshot.TeamID, snapshot.Config, snapshot.Language)
			workspace.MountContext = NewTeamRailMountContext(deepAgent, snapshot.Role, snapshot.Channel, snapshot.Language, workspace)
			teamManager.RegisterTeamLiveRail(snapshot.SessionID, deepAgent, swarmRail)
			teamManager.RegisterTeamMemberSkillEvolutionRail(snapshot.SessionID, swarmRail.SkillEvolutionRail)
			// Python: register_team_member_rail_context（:420-445）
			teamManager.RegisterTeamMemberRailContext(snapshot.SessionID, workspace.MountContext)
			logger.Info(evolutionWiringLogComponent).
				Str("session_id", snapshot.SessionID).
				Msg("[swarm.member_skill_evolution] registered member evolution rail")
		})
	})
}

// 编译期验证：Go 侧各 Swarm rail 满足 AgentRail（Init/Uninit 覆写经嵌入分发）。
var (
	_ agentinterfaces.AgentRail = (*providers.SwarmTeamSkillEvolutionRail)(nil)
	_ agentinterfaces.AgentRail = (*providers.SwarmTeamSkillCreateRail)(nil)
	_ agentinterfaces.AgentRail = (*providers.SwarmMemberSkillEvolutionRail)(nil)
)

// init 包加载时注入 evolution rail 注册回调（D-15.24c-3）。
func init() { EnsureEvolutionRailRegistrarWired() }

// 编译期验证：Go 侧各 Swarm rail 满足 AgentRail（Init/Uninit 覆写经嵌入分发）。
var (
	_ agentinterfaces.AgentRail = (*providers.SwarmTeamSkillEvolutionRail)(nil)
	_ agentinterfaces.AgentRail = (*providers.SwarmTeamSkillCreateRail)(nil)
	_ agentinterfaces.AgentRail = (*providers.SwarmMemberSkillEvolutionRail)(nil)
)

// init 包加载时注入 evolution rail 注册回调（D-15.24c-3）。
func init() { EnsureEvolutionRailRegistrarWired() }
