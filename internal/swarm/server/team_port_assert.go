package server

import (
	teamharness "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// 编译期断言：TeamManager 实现生命周期适配器所需的团队运行时操作面。
// TeamSessionLifecycleAdapter（swruntime）经 TeamLifecycleManager 窄接口驱动
// TeamManager 的六账本真实现（team_manager_round_state.go，25.2b Task 12）与
// pause/stop 代次感知入口（team_manager_lifecycle.go）。
// 断言置于 server 包：swruntime→server/runtime→harness/team 存在依赖链，
// harness/team 侧反向断言会成环。
var _ swruntime.TeamLifecycleManager = (*teamharness.TeamManager)(nil)
