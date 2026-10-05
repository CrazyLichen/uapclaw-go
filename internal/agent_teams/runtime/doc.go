// Package runtime 提供团队 Agent 运行时管理。
//
// Runtime 包管理 TeamAgent 的活跃运行时池、并发门控和生命周期调度。
// 核心组件：
//   - InteractGate：Run/Interact 并发门控，保证 streaming 结束前 interact 排空
//   - TeamRuntimePool：进程内活跃 TeamAgent 运行时池
//   - TeamRuntimeManager：运行时管理器（interact 路由 + 完整生命周期方法）
//   - TeamRunner：团队执行入口（RunAgentTeam/Streaming/BaseTeam/Member 三路径 + Interact/Monitor/ListActive）
//
// 文件目录：
//
//	runtime/
//	├── doc.go           # 包文档
//	├── gate.go          # InteractGate 并发门控
//	├── pool.go          # ActiveTeam/ActiveTeamInfo/TeamRuntimePool
//	├── manager.go       # TeamRuntimeManager（interact + Activate/Finalize/GetMonitor/ListActiveTeams + 内部调度）
//	├── dispatch.go      # RunActionKind + RunAction + DecideRunAction 纯调度决策
//	├── team_runner.go   # 团队执行入口（RunAgentTeam/Streaming/BaseTeam/Member + Interact/Monitor/ListActive + 内部辅助）
//	└── release_info.go  # TeamSessionReleaseInfo + ResolveTeamSessionReleaseInfo + 辅助函数
//
// 对应 Python 代码：openjiuwen/agent_teams/runtime/
package runtime
