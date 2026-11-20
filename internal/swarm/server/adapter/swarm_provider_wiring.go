package adapter

import (
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/interfaces"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	serverhooks "github.com/uapclaw/uapclaw-go/internal/swarm/server/hooks"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/providers"
	teamrt "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
)

// swarm_provider_wiring.go —— providers 包基座构造函数注入（D-15.16f-view
// 配套缝；偏差 D-15.16f-inject：Python 无此缝——providers 直接 import 父包
// SwarmBuildContext 与基座；Go providers↔team/providers↔adapter 成环，注入
// 必须在能同时 import team 与 adapter 的本层完成）。
// 调用点：本包 init（工厂闭包在构建期才解引用，init 注入早于任何成员构建）。
//
// Deviation D-15.16f-inject.
//
// Python: register_swarm_providers 的 catalog 注册隐式携带基座绑定。

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 包初始化即注入（早于任何成员构建；工厂闭包构建期才解引用）。
func init() {
	InjectSwarmProviderBases()
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// providerBasesInjected 注入幂等守卫。
	providerBasesInjected bool
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InjectSwarmProviderBases 注入 providers 包的 team/adapter 基座构造函数。
// 幂等：仅首次生效。
func InjectSwarmProviderBases() {
	if providerBasesInjected {
		return
	}
	providers.SetContextProcessorRailBuilder(func(config map[string]any) agentinterfaces.AgentRail {
		return teamrt.BuildContextProcessorRail(config)
	})
	providers.SetWorkspaceReportPathCtor(func(rootDir, teamID, language string) agentinterfaces.AgentRail {
		return teamrt.NewTeamWorkspaceReportPathRail(rootDir, teamID, language)
	})
	providers.SetMemberSkillToolkitRailCtor(func(workspaceDir string) agentinterfaces.AgentRail {
		return teamrt.NewMemberSkillToolkitRail(workspaceDir)
	})
	providers.SetCodeAgentRailCtor(func(workspaceDir string) agentinterfaces.AgentRail {
		return NewCodeAgentRail(workspaceDir, nil)
	})
	providers.SetUserHookRailCtor(func(config hookscfg.HooksConfig) agentinterfaces.AgentRail {
		return serverhooks.NewUserHookRail(hookscfg.HooksConfig(config))
	})
	providerBasesInjected = true
}
