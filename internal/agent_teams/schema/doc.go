// Package schema 提供团队相关的数据结构定义。
//
// 包含团队角色、状态枚举、规格定义、运行时上下文、事件类型等。
// blueprint.go 是核心文件，定义 TeamAgentSpec（构造 TeamAgent 的完整 JSON 可序列化规格）、
// DeepAgentSpec（单角色规格）、LeaderSpec、TransportSpec/StorageSpec 及其注册表、
// 以及全部校验方法（互斥、保留名、HITT 一致性、Leader 模型解析）。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── team.go          # TeamRole/TeamSpec/TeamRuntimeContext 等团队级类型
//	├── blueprint.go     # TeamAgentSpec/DeepAgentSpec/LeaderSpec 等规格定义与校验
//	├── status.go        # 成员/任务状态枚举与状态转换表
//	└── events.go        # 事件类型与事件消息 Schema
//
// 对应 Python 代码：openjiuwen/agent_teams/schema/
package schema
