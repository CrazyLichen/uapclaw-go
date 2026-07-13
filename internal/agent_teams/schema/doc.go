// Package schema 提供团队级 Schema 定义。
//
// 包含团队角色、状态枚举、规格定义、运行时上下文等。
// 对齐 Python: openjiuwen/agent_teams/schema/
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── status.go        # 成员/任务状态枚举与状态转换表
//	├── team.go          # TeamRole/TeamSpec/TeamRuntimeContext 等
//	├── blueprint.go     # TeamAgentSpec/LeaderSpec 等规格定义
//	└── events.go        # 事件类型与事件消息 Schema
//
// 对应 Python 代码：openjiuwen/agent_teams/schema/
package schema
