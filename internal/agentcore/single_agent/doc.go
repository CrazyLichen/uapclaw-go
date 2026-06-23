// Package single_agent 提供 Agent 核心能力管理，包括 AbilityManager 注册与调度。
//
// AbilityManager 是 Agent 的能力注册与调度中心，管理四类 Ability
// （Tool / Workflow / Agent / McpServer）的完整生命周期：
// 注册管理、LLM 工具描述生成、并行执行、JSON 参数修复、路由分发。
//
// Workflow/Agent 接口定义从本包抽出至 interfaces 子包，
// 供 tracer 等外部包引用，避免 tracer → single_agent → context_engine 循环依赖。
//
// 文件目录：
//
//	single_agent/
//	├── doc.go                 # 包文档
//	├── base.go                # WarpBaseAgent — BaseAgent 默认实现，Invoke/Stream 回调包装骨架
//	├── ability/
//	│   ├── doc.go             # 子包文档
//	│   ├── ability_manager.go # AbilityManager 核心结构 + 注册/查询/执行
//	│   ├── ability_types.go   # Ability 联合类型 + AddAbilityResult + AbilityExecutionError + ToolRail 预留
//	│   └── json_repair.go     # RepairToolArgumentsJSON + ParseToolArguments
//	├── config/
//	│   ├── doc.go             # 子包文档
//	│   └── agent_config.go    # ReActAgentConfig 结构体 + Option + AgentConfig 接口实现 + Validate
//	├── interfaces/
//	│   ├── doc.go             # 子包文档
//	│   └── interface.go       # Workflow/Agent 接口 + WorkflowOption/AgentOption 类型
//	├── resource/
//	│   ├── doc.go             # 子包文档
//	│   └── resource_manager.go # ResourceManager 接口 + NoopResourceManager + ResourceOptions
//	└── schema/
//	    ├── doc.go             # 子包文档
//	    ├── agent_card.go      # AgentCard 结构体 + 构造函数 + Ability 接口实现
//	    └── agent_result.go    # Part/Artifact/AgentResult 结果模型
//
// 对应 Python 代码：openjiuwen/core/single_agent/ability_manager.py
package single_agent
