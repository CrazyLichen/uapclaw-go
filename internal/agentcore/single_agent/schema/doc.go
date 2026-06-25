// Package schema 提供 Agent 领域的公共类型定义，包括 AgentCard、AgentResult、中断类型等。
//
// 本包是 Agent 核心领域（领域六）的基础类型包，供 ability_manager、session、
// interrupt、A2A 扩展等上层包引用。
//
// 依赖约束：本包只依赖 common/schema/（BaseCard/ToolInfo/AbilityKind）、
// controller/schema/（TaskStatus）、foundation/tool/、foundation/llm/schema 和 stdlib，
// 不反向引用 single_agent/interfaces、single_agent/config 或 session/，以避免循环依赖。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── agent_card.go    # AgentCard 结构体 + 构造函数 + Ability 接口实现
//	├── agent_result.go  # Part/Artifact/AgentResult 结果模型 + RawBytes 自定义 JSON marshal
//	├── exception.go     # ToolInterruptException（实现 error 接口）
//	├── response.go      # InterruptRequest + ToolCallInterruptRequest
//	└── state.go         # 常量 + 中断状态类型（BaseInterruptionState/ToolInterruptionState 等）
//
// 对应 Python 代码：openjiuwen/core/single_agent/schema/ + openjiuwen/core/single_agent/interrupt/
//
// 核心类型/接口索引：
//
//	AgentCard               — Agent 配置卡片
//	AgentResult             — Agent 执行结果
//	InterruptRequest        — 工具中断请求
//	ToolCallInterruptRequest — 工具调用级中断请求
//	ToolInterruptException  — 工具中断异常（实现 error 接口）
//	BaseInterruptionState   — 中断状态基类
//	ToolInterruptEntry      — 工具中断条目
//	ToolInterruptionState   — 工具中断状态（HITL）
//	InterruptionState       — 工作流中断状态
package schema
