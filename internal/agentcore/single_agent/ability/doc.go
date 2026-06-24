// Package ability 提供 Agent 能力注册与调度中心（AbilityManager）及相关类型定义。
//
// AbilityManager 管理四类 Ability（Tool / Workflow / Agent / McpServer）
// 的完整生命周期：注册管理、LLM 工具描述生成、并行执行、JSON 参数修复、路由分发。
// 从 single_agent 包拆分为独立子包，便于职责隔离和后续扩展。
//
// 文件目录：
//
//	ability/
//	├── doc.go               # 包文档
//	├── ability_manager.go   # AbilityManager 核心结构 + 注册/查询/执行
//	├── ability_types.go     # Ability 联合类型 + AddAbilityResult + AbilityExecutionError
//	└── json_repair.go       # RepairToolArgumentsJSON + ParseToolArguments
//
// 对应 Python 代码：openjiuwen/core/single_agent/ability_manager.py
package ability
