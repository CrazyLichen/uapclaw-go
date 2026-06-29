// Package multi_agent 提供多 Agent 团队的核心抽象与运行时基础设施。
//
// 定义 BaseTeam 接口作为多 Agent 团队体系的根基契约，
// 具体团队实现（HandoffTeam、HierarchicalTeam）均实现此接口。
// 团队内部的 Agent 通信通过 TeamRuntime（8.30）的 P2P 和 Pub-Sub 消息机制完成。
//
// 文件目录：
//
//	multi_agent/
//	├── doc.go            # 包文档
//	├── team.go           # BaseTeam 接口 + AgentTeamProvider + TeamCard/TeamConfig 占位
//	└── team_option.go    # TeamOptions 结构体 + TeamOption 函数类型 + WithXxx
//
// 对应 Python 代码：openjiuwen/core/multi_agent/
package multi_agent
