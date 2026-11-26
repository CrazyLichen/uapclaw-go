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
	evolutionRegistrarOnce.Do(func() { wireEvolutionRailRegistrar() })
}

// wireEvolutionRailRegistrar 注入 evolution rail 的 TeamManager 注册回调
// Python: 各 Swarm 子类 init 的注册段。kind 分派：
//   - team_evolution → RegisterTeamLiveRail + RegisterTeamSkillRail +
//     bind_review_feedback_skill_create_rail + consume_watcher + RegisterTeamRailContext
//   - team_create → RegisterTeamLiveRail + RegisterTeamSkillCreateRail +
//     反向 bind + RegisterTeamRailContext
//   - member_evolution → RegisterTeamLiveRail + RegisterTeamMemberSkillEvolutionRail +
//     RegisterTeamMemberRailContext
func wireEvolutionRailRegistrar() {
	providers.SetEvolutionRailRegistrar(func(kind providers.EvolutionRailKind, snapshot *swarmsnapshot.Snapshot, agent agentinterfaces.BaseAgent, rail agentinterfaces.AgentRail) {
		teamManager := GetTeamManager(snapshot.Channel)
		if teamManager == nil || snapshot == nil {
			return
		}
		deepAgent := deepOfAgent(agent)
		workspace := NewTeamWorkspaceInfo(snapshot.TeamWsRoot, snapshot.SkillsDir, snapshot.TeamID, snapshot.Config, snapshot.Language)
		workspace.MountContext = NewTeamRailMountContext(deepAgent, snapshot.Role, snapshot.Channel, snapshot.Language, workspace)
		switch kind {
		case providers.EvolutionRailKindTeamEvolution:
			swarmRail, ok := rail.(*providers.SwarmTeamSkillEvolutionRail)
			if !ok {
				return
			}
			teamManager.RegisterTeamLiveRail(snapshot.SessionID, deepAgent, swarmRail)
			teamManager.RegisterTeamSkillRail(snapshot.SessionID, swarmRail.TeamSkillEvolutionRail)
			// Python: bind_review_feedback_skill_create_rail(get_team_skill_create_rail)（:259-261）
			if createRail := teamManager.GetTeamSkillCreateRail(snapshot.SessionID); createRail != nil {
				swarmRail.TeamSkillEvolutionRail.BindReviewFeedbackSkillCreateRail(createRail)
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
		case providers.EvolutionRailKindTeamCreate:
			swarmRail, ok := rail.(*providers.SwarmTeamSkillCreateRail)
			if !ok {
				return
			}
			teamManager.RegisterTeamLiveRail(snapshot.SessionID, deepAgent, swarmRail)
			teamManager.RegisterTeamSkillCreateRail(snapshot.SessionID, swarmRail.TeamSkillCreateRail)
			// Python: team_rail.bind_review_feedback_skill_create_rail(self)（:337-344）
			if teamRail := teamManager.GetTeamSkillRail(snapshot.SessionID); teamRail != nil {
				teamRail.BindReviewFeedbackSkillCreateRail(swarmRail.TeamSkillCreateRail)
			}
			teamManager.RegisterTeamRailContext(snapshot.SessionID, workspace.MountContext)
			logger.Info(evolutionWiringLogComponent).
				Str("session_id", snapshot.SessionID).
				Msg("[swarm.team_skill_create] registered live rail")
		case providers.EvolutionRailKindMemberEvolution:
			swarmRail, ok := rail.(*providers.SwarmMemberSkillEvolutionRail)
			if !ok {
				return
			}
			teamManager.RegisterTeamLiveRail(snapshot.SessionID, deepAgent, swarmRail)
			teamManager.RegisterTeamMemberSkillEvolutionRail(snapshot.SessionID, swarmRail.SkillEvolutionRail)
			// Python: register_team_member_rail_context（:420-445）
			teamManager.RegisterTeamMemberRailContext(snapshot.SessionID, workspace.MountContext)
			logger.Info(evolutionWiringLogComponent).
				Str("session_id", snapshot.SessionID).
				Msg("[swarm.member_skill_evolution] registered member evolution rail")
		default:
			logger.Warn(evolutionWiringLogComponent).Str("kind", string(kind)).Msg("unknown evolution rail kind, skipping")
		}
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
