package server

import (
	teamharness "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	swsession "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	serverruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// 编译期断言：TeamManager 实现生命周期适配器所需的团队运行时操作面。
// TeamSessionLifecycleAdapter（swruntime）经 TeamLifecycleManager 窄接口驱动
// TeamManager 的六账本真实现（team_manager_round_state.go，25.2b Task 12）与
// pause/stop 代次感知入口（team_manager_lifecycle.go）。
// 断言置于 server 包：swruntime→server/runtime→harness/team 存在依赖链，
// harness/team 侧反向断言会成环。
var _ swruntime.TeamLifecycleManager = (*teamharness.TeamManager)(nil)

// 编译期断言：轮状态协调器桥接面（rounds）结构化匹配 RoundStateCoordinator。
var _ swruntime.RoundStateCoordinator = (serverruntime.RoundStateRounds)(nil)

func init() {
	// 装配团队生命周期适配器构造器（Python: service.py:_team_lifecycle 懒构造的
	// 渠道化形态——TeamManager 按渠道多实例，适配器随渠道各建一个并注册 reporter；
	// rounds 协调器由调用方（server/runtime 门面）随构造参数传入）。
	serverruntime.SetTeamLifecycleFactory(newTeamLifecycleAdapter)
}

// newTeamLifecycleAdapter 构造一个渠道的团队会话生命周期适配器。
func newTeamLifecycleAdapter(channelID string, rounds serverruntime.RoundStateRounds) swsession.SessionRuntimeLifecyclePort {
	tm := teamharness.GetTeamManager(channelID)
	if tm == nil {
		return nil
	}
	return swruntime.NewTeamSessionLifecycleAdapter(tm, rounds)
}
