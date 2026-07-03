// Package hierarchical_msgbus 提供层级多 Agent 团队实现（消息总线模式）。
//
// 消息总线模式下，SupervisorAgent 通过 ReAct 循环推理，
// LLM 返回 tool_call 时通过 P2PAbilityManager 派发给子 Agent 执行。
// 支持并行子 Agent 派发（Semaphore 限流）。
//
// 文件目录：
//
//	hierarchical_msgbus/
//	├── doc.go                      # 包文档
//	├── hierarchical_config.go      # HierarchicalTeamConfig 配置定义
//	├── hierarchical_team.go        # HierarchicalTeam 实现 BaseTeam 接口
//	├── p2p_ability_manager.go      # P2PAbilityManager P2P 能力管理器
//	└── supervisor_agent.go         # SupervisorAgent 监督者 Agent
//
// 对应 Python 代码：openjiuwen/core/multi_agent/teams/hierarchical_msgbus/
package hierarchical_msgbus
