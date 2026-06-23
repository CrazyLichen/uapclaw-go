// Package config 提供 Agent 配置的具体实现，包括 ReActAgentConfig 等。
//
// 本包定义 Agent 的配置数据结构，与 interfaces 包中的 AgentConfig 接口配合使用。
// 具体配置类型实现 AgentConfig 接口，供 Agent 构造和运行时消费。
//
// 包分离设计：将配置实现从 schema 包分离到 config 包，
// 打破 schema ↔ interfaces 的循环依赖（schema 不再 import interfaces），
// 同时使 ContextEngineConfig 和 ContextProcessors 可以使用具体类型而非 any。
//
// 依赖关系：
//
//	config ──► interfaces ──► schema （单向，无循环）
//
// 文件目录：
//
//	config/
//	├── doc.go              # 包文档
//	└── agent_config.go     # ReActAgentConfig 结构体 + Option + AgentConfig 接口实现 + Validate
//
// 对应 Python 代码：openjiuwen/core/single_agent/agents/react_agent.py (ReActAgentConfig)
//
// 核心类型/接口索引：
//
//	ReActAgentConfig — ReAct Agent 配置（实现 interfaces.AgentConfig 接口）
package config
