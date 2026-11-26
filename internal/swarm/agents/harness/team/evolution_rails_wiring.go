package team

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	skillrails "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/skills"
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
	providers.SetEvolutionRailRegistrar(func(kind, channel, sessionID string, agent hinterfaces.DeepAgentInterface, rail agentinterfaces.AgentRail, ws *providers.EvolutionWorkspaceSnapshot) {
		teamManager := GetTeamManager(channel)
		if teamManager == nil || ws == nil {
			return
		}
		workspace := NewTeamWorkspaceInfo(ws.RootDir, ws.SkillsDir, ws.TeamID, ws.Config, ws.Language)
		switch kind {
		case "team_evolution":
			swarmRail, ok := rail.(*providers.SwarmTeamSkillEvolutionRail)
			if !ok {
				return
			}
			teamManager.RegisterTeamLiveRail(sessionID, agent, swarmRail)
			teamManager.RegisterTeamSkillRail(sessionID, swarmRail.TeamSkillEvolutionRail)
			// Python: bind_review_feedback_skill_create_rail(get_team_skill_create_rail)（:259-261）
			if createRail := teamManager.GetTeamSkillCreateRail(sessionID); createRail != nil {
				swarmRail.TeamSkillEvolutionRail.BindReviewFeedbackSkillCreateRail(createRailReviewFeedbackAdapter{rail: createRail})
			}
			// Python: consume_team_evolution_watcher_deferred（:262-271）
			//（G2 裁决：consume 面随本批补；watcher 本体挂 15.24d，true 时空转日志）
			if teamManager.ConsumeTeamEvolutionWatcherDeferred(sessionID) {
				logger.Info(evolutionWiringLogComponent).
					Str("session_id", sessionID).
					Msg("[swarm.team_skill_evolution] deferred watcher consume（watcher 本体 ⤴️15.24d）")
			}
			workspace.MountContext = NewTeamRailMountContext(agent, "leader", channel, "cn", workspace)
			teamManager.RegisterTeamRailContext(sessionID, workspace.MountContext)
			logger.Info(evolutionWiringLogComponent).
				Str("session_id", sessionID).
				Str("skills_dir", ws.SkillsDir).
				Msg("[swarm.team_skill_evolution] registered live rail")
		case "team_create":
			swarmRail, ok := rail.(*providers.SwarmTeamSkillCreateRail)
			if !ok {
				return
			}
			teamManager.RegisterTeamLiveRail(sessionID, agent, swarmRail)
			teamManager.RegisterTeamSkillCreateRail(sessionID, swarmRail.TeamSkillCreateRail)
			// Python: team_rail.bind_review_feedback_skill_create_rail(self)（:337-344）
			if teamRail := teamManager.GetTeamSkillRail(sessionID); teamRail != nil {
				teamRail.BindReviewFeedbackSkillCreateRail(createRailReviewFeedbackAdapter{rail: swarmRail.TeamSkillCreateRail})
			}
			workspace.MountContext = NewTeamRailMountContext(agent, "leader", channel, "cn", workspace)
			teamManager.RegisterTeamRailContext(sessionID, workspace.MountContext)
			logger.Info(evolutionWiringLogComponent).
				Str("session_id", sessionID).
				Msg("[swarm.team_skill_create] registered live rail")
		case "member_evolution":
			swarmRail, ok := rail.(*providers.SwarmMemberSkillEvolutionRail)
			if !ok {
				return
			}
			teamManager.RegisterTeamLiveRail(sessionID, agent, swarmRail)
			teamManager.RegisterTeamMemberSkillEvolutionRail(sessionID, swarmRail.SkillEvolutionRail)
			// Python: register_team_member_rail_context（:420-445）
			workspace.MountContext = NewTeamRailMountContext(agent, "teammate", channel, ws.Language, workspace)
			teamManager.RegisterTeamMemberRailContext(sessionID, workspace.MountContext)
			logger.Info(evolutionWiringLogComponent).
				Str("session_id", sessionID).
				Msg("[swarm.member_skill_evolution] registered member evolution rail")
		default:
			logger.Warn(evolutionWiringLogComponent).Str("kind", kind).Msg("unknown evolution rail kind, skipping")
		}
	})
}

// 编译期验证：Go 侧各 Swarm rail 满足 AgentRail（Init/Uninit 覆写经嵌入分发）。
var (
	_ agentinterfaces.AgentRail           = (*providers.SwarmTeamSkillEvolutionRail)(nil)
	_ agentinterfaces.AgentRail           = (*providers.SwarmTeamSkillCreateRail)(nil)
	_ agentinterfaces.AgentRail           = (*providers.SwarmMemberSkillEvolutionRail)(nil)
	_ evolution.ReviewFeedbackCoordinator = createRailReviewFeedbackAdapter{}
)

// createRailReviewFeedbackAdapter 将 TeamSkillCreateRail 适配成
// ReviewFeedbackCoordinator 注入面（Python 鸭子式：team_rail 持 rail 引用后
// 仅经 owns_external_proposal/resolve_external_proposal 消费——
// 本批 TeamSkillEvolutionRail.externalProposalOwner 承载；协调器面两方法
// 对 creation rail 恒空转）。
type createRailReviewFeedbackAdapter struct {
	rail *skillrails.TeamSkillCreateRail
}

// HandleReviewFeedback creation rail 无协调器语义（恒 nil）。
func (a createRailReviewFeedbackAdapter) HandleReviewFeedback(ctx context.Context, payload map[string]any) error {
	return nil
}

// OnTeamCompleted creation rail 无团队聚合语义。
func (a createRailReviewFeedbackAdapter) OnTeamCompleted(ctx context.Context, payload map[string]any) (bool, error) {
	return false, nil
}

var ()

// init 包加载时注入 evolution rail 注册回调（D-15.24c-3）。
func init() { EnsureEvolutionRailRegistrarWired() }
