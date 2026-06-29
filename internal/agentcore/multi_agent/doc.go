// Package multi_agent 提供多 Agent 团队的核心抽象与运行时基础设施。
//
// 定义 BaseTeam 接口作为多 Agent 团队体系的根基契约，
// 具体团队实现（HandoffTeam、HierarchicalTeam）均实现此接口。
// 团队内部的 Agent 通信通过 TeamRuntime（8.30）的 P2P 和 Pub-Sub 消息机制完成。
//
// 文件目录：
//
//	multi_agent/
//	├── doc.go              # 包文档
//	├── config.go           # TeamConfig 团队运行时配置 + 链式配置方法
//	├── team.go             # BaseTeam 接口 + TeamCard 类型别名 + AgentTeamProvider/TeamAgentProvider
//	├── team_option.go      # TeamOptions 结构体 + TeamOption 函数类型 + WithXxx
//	└── schema/
//	    ├── doc.go           # schema 子包文档
//	    └── team_card.go    # TeamCard 团队身份卡片 + 构造函数 + TeamCardOption + String
//
// 对应 Python 代码：openjiuwen/core/multi_agent/
package multi_agent
