// Package single_agent 提供 Agent 核心能力管理，包括 AbilityManager 注册与调度。
//
// AbilityManager 是 Agent 的能力注册与调度中心，管理四类 Ability
// （Tool / Workflow / Agent / McpServer）的完整生命周期：
// 注册管理、LLM 工具描述生成、并行执行、JSON 参数修复、路由分发。
//
// 文件目录：
//
//	single_agent/
//	├── doc.go                 # 包文档
//	├── ability_types.go       # Ability 联合类型 + AddAbilityResult + AbilityExecutionError + ToolRail 预留
//	├── json_repair.go         # RepairToolArgumentsJSON + ParseToolArguments
//	├── resource_manager.go    # ResourceManager 接口 + NoopResourceManager + 最小依赖接口
//	└── ability_manager.go     # AbilityManager 核心结构 + 注册/查询/执行
//
// 对应 Python 代码：openjiuwen/core/single_agent/ability_manager.py
package single_agent
