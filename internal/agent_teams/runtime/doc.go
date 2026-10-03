// Package runtime 提供团队 Agent 运行时管理。
//
// Runtime 包管理 TeamAgent 的活跃运行时池、并发门控和生命周期调度。
// 核心组件：
//   - InteractGate：Run/Interact 并发门控，保证 streaming 结束前 interact 排空
//   - TeamRuntimePool：进程内活跃 TeamAgent 运行时池
//   - TeamRuntimeManager：运行时管理器（interact 路由 + 完整生命周期方法）
//
// 文件目录：
//
//	runtime/
//	├── doc.go           # 包文档
//	├── gate.go          # InteractGate 并发门控
//	├── pool.go          # ActiveTeam/ActiveTeamInfo/TeamRuntimePool
//	├── manager.go       # TeamRuntimeManager（interact + Pause/StopTeam/Finalize/DeleteTeam/ReleaseSession/IsTeamSession）
//	└── release_info.go  # TeamSessionReleaseInfo + ResolveTeamSessionReleaseInfo + 辅助函数
//
// 对应 Python 代码：openjiuwen/agent_teams/runtime/
package runtime
