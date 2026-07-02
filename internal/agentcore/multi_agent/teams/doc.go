// Package teams 提供多 Agent 团队的具体实现模式。
//
// 本包包含基于事件驱动的团队类型，如 HandoffTeam（单活跃 Agent 交接模式）。
// 所有团队类型均实现 BaseTeam 接口，可注册到 TeamRuntime 中运行。
//
// 文件目录：
//
//	teams/
//	├── doc.go           # 包文档
//	├── utils.go         # 独立调用上下文工具函数
//	└── handoff/         # HandoffTeam 实现
//
// 对应 Python 代码：openjiuwen/core/multi_agent/teams/
package teams
