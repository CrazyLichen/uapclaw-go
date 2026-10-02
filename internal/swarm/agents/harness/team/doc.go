// Package team 提供应用层 TeamManager，管理每个 channel 下的 TeamAgent 运行时。
//
// 对齐 Python: jiuwenswarm/agents/harness/team/team_manager.py
//
// TeamManager 是应用层封装，负责：
//   - channel_id 索引和全局实例管理
//   - session 活跃状态管理（active/pending）
//   - TeamAgent 创建/销毁/生命周期
//   - Rail/技能注册与热更新
//   - 流任务和监控管理
//   - 分布式 hooks 附加
//
// 核心层 TeamRuntimeManager（agent_teams/runtime）负责 Pool/interact/activate，
// TeamManager 通过间接调用来完成实际操作。
//
// Go 与 Python 的差异：
//   - 全局索引：Python 用 _team_managers dict，Go 用 sync.RWMutex 保护
//   - 流任务：Python 用 asyncio.Task，Go 用 context.CancelFunc
//   - Runner 桥接：Python 有 TeamManager→Runner→TeamRuntimeManager 三层，
//     Go 省略 Runner 层，TeamManager 直接调 TeamRuntimeManager
//   - session_id 传参：Python 用 contextvars 隐式传播，Go 显式传参
//
// 文件目录：
//
//	team/
//	├── doc.go                        # 包文档
//	├── team_manager.go               # TeamManager 结构体 + 全局索引 + 访问器
//	├── team_manager_lifecycle.go     # 生命周期方法
//	├── team_manager_interact.go      # interact 路由
//	├── team_manager_spec.go          # Spec 构建
//	├── team_manager_skill.go         # 技能管理
//	├── team_manager_monitor.go       # 监控/流
//	└── team_manager_distributed.go   # 分布式 hooks
//
// 对应 Python 代码：jiwenswarm/agents/harness/team/team_manager.py
package team
