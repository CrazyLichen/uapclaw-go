// Package hierarchical_tools 提供层级多 Agent 团队实现（工具委托模式）。
//
// 工具委托模式下，子 Agent 注册到父 Agent 的 ability_manager 中，
// LLM 将子 Agent 视为可调用的工具（tool_call），
// 子 Agent 的执行由 AbilityManager.executeAgent() → Runner.RunAgent() 完成。
// 支持多级树状层级（父→子→孙），任意 Agent 都可作为父节点。
//
// 文件目录：
//
//	hierarchical_tools/
//	├── doc.go                      # 包文档
//	├── hierarchical_config.go      # HierarchicalToolsTeamConfig 配置定义
//	└── hierarchical_team.go        # HierarchicalToolsTeam 实现 BaseTeam 接口
//
// 对应 Python 代码：openjiuwen/core/multi_agent/teams/hierarchical_tools/
package hierarchical_tools
