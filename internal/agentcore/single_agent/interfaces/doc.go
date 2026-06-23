// Package interfaces 提供 single_agent 领域的核心接口定义。
//
// 将 Workflow、Agent、AgentConfig 等接口从此包导出，供 tracer、single_agent 等包共同引用，
// 避免 tracer → single_agent → context_engine 的循环依赖。
// 接口与具体实现分离，tracer 只需依赖接口定义，不需要导入 single_agent 包本体。
//
// AgentConfig 接口的具体实现位于 single_agent/config 包（ReActAgentConfig 等），
// 本包仅定义最小接口方法，不导入 config 包以保持依赖方向单一。
//
// 文件目录：
//
//	interfaces/
//	├── doc.go           # 包文档
//	└── interface.go     # AgentConfig/Workflow/BaseAgent 接口及选项类型
//
// 核心类型/接口索引：
//
//	AgentConfig — Agent 配置接口（所有 Agent 配置的通用抽象）
//	Workflow    — 工作流执行接口
//	BaseAgent   — Agent 执行的核心行为契约
package interfaces
