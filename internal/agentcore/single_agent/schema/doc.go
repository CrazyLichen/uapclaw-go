// Package schema 提供 Agent 领域的公共类型定义，包括 AgentCard、AgentResult、ReActAgentConfig 等。
//
// 本包是 Agent 核心领域（领域六）的基础类型包，供 ability_manager、session、
// A2A 扩展等上层包引用。
//
// 依赖约束：本包只依赖 common/schema/（BaseCard/ToolInfo/AbilityKind）、
// controller/schema/（TaskStatus）、context_engine/、foundation/llm/schema/ 和 stdlib，
// 不反向引用 single_agent/ 或 session/，以避免循环依赖。
//
// 文件目录：
//
//	schema/
//	├── doc.go                  # 包文档
//	├── agent_card.go           # AgentCard 结构体 + 构造函数 + Ability 接口实现
//	├── agent_result.go         # Part/Artifact/AgentResult 结果模型 + RawBytes 自定义 JSON marshal
//	└── react_agent_config.go   # ReActAgentConfig 结构体 + Option + AgentConfig 接口实现 + Validate
//
// 对应 Python 代码：openjiuwen/core/single_agent/schema/ + openjiuwen/core/single_agent/agents/react_agent.py (ReActAgentConfig)
//
// 核心类型/接口索引：
//
//	AgentCard          — Agent 配置卡片
//	AgentResult        — Agent 执行结果
//	ReActAgentConfig   — ReAct Agent 配置（实现 interfaces.AgentConfig 接口）
package schema
