// Package interfaces 提供 single_agent 领域的核心接口与类型定义。
//
// 将 Agent、AgentConfig、Workflow、AgentCallbackEvent、AgentRail 等接口和类型
// 从此包导出，供 tracer、single_agent、harness 等包共同引用，
// 避免 rail ↔ interfaces 循环依赖。
//
// 文件目录：
//
//	interfaces/
//	├── doc.go         # 包文档
//	├── agent.go       # BaseAgent 接口、AgentConfig、AgentOption/AgentOptions
//	├── abilitymgr.go  # AbilityManagerInterface 能力管理器接口
//	├── callback.go    # AgentCallbackEvent/Context/Manager、AgentRail/BaseRail、EventInputs 等
//	└── workflow.go    # Workflow 接口、WorkflowOption/WorkflowOptions
//
// 核心类型/接口索引：
//
//	BaseAgent                — Agent 执行的核心行为契约（含 SystemPromptBuilder()）
//	AgentConfig              — Agent 配置接口
//	AbilityManagerInterface  — 能力管理器接口
//	AgentCallbackEvent       — 10 种生命周期事件枚举
//	AgentCallbackContext     — Rail 系统核心中介对象（retry/force_finish/steering）
//	AgentCallbackManager     — PerAgent 实例级回调管理器
//	AgentRail                — Agent 生命周期 Rail 接口
//	BaseRail                 — AgentRail 的 no-op 默认实现
//	EventInputs              — 回调事件输入接口
//	Workflow                 — 工作流执行接口
package interfaces
