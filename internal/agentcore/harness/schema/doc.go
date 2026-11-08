// Package schema 提供 harness 模式的类型定义，包括 AgentMode、PromptMode、
// DeepAgentConfig、TaskPlan 及交互监督器领域对象。
// 这些类型用于控制 Agent 的执行模式（普通/规划）、提示词注入模式（完整/精简/无）、
// 任务计划管理及循环事件处理，对应 Python 中 openjiuwen/harness/schema/ 目录下的枚举和配置定义。
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── agent_mode.go    # AgentMode 枚举及 JSON 序列化
//	├── build_context.go # BuildContext 构建上下文（provider 解析期载体，叶子包）
//	├── construction_input.go # 元素双源构造输入（params/context 取值助手）
//	├── agent_specs.go # 单角色规格类型（DeepAgentSpec/SubAgentSpec/WorkspaceSpec 等，上游同文件）
//	├── deep_agent_parts.go # DeepAgentParts 装配产物（config + rails + tools）
//	├── resolve_parts.go # DeepAgentSpec.ResolveParts 七步解析 + 语言解析
//	├── resolve_parts_internal.go # sys_operation 解析 + config 组装（ResolveParts 内部步骤）
//	├── team_model_config.go # TeamModelConfig（可序列化模型配置，上游同文件）
//	├── deep_agent_spec.go # RailSpec/BuiltinToolSpec 声明式规格 + Build（经 provider 注册表）
//	├── rail_registry.go # rail provider 注册表（type 名 → 工厂，RWMutex 保护）
//	├── rail_spec_support.go # rail 解析辅助（内置元素钩子 / 未知类型告警 / 最小上下文）
//	├── prompt_mode.go   # PromptMode 枚举及 JSON 序列化
//	├── config.go        # DeepAgentConfig、SubagentSpec 及辅助类型（VisionModelConfig、AudioModelConfig、SubAgentConfig、ModelSelectionEntry）
//	├── loop_event.go    # DeepLoopEventType 枚举及 DeepLoopEvent 结构体
//	├── state.go         # PlanModeState、DeepAgentState 会话状态
//	├── task.go          # TodoStatus 枚举、TodoItem、TaskPlan、ModelUsageRecord 任务计划类型
//	├── task_type.go     # DeepTaskType + SessionSpawnTaskType 任务类型常量
//	└── interaction.go   # 交互监督器领域对象（RoundWorkItem/InteractionEvent/OutputLease/OutputLeaseManager 等）
//
// 对应 Python 代码：openjiuwen/harness/schema/
package schema
