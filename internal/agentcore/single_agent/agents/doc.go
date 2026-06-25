// Package agents 提供 Agent 的具体实现。
//
// 本包包含各种 Agent 模式的实现，如 ReActAgent（Reasoning + Acting）等。
// 每个 Agent 实现 single_agent/interfaces 包中定义的 Agent 接口，
// 由 base.go 中的 WarpBaseAgent 提供公共委托实现。
//
// 文件目录：
//
//	agents/
//	├── react_agent.go   # ReActAgent — ReAct 循环 Agent（Think → Act → Observe）
//	├── react_hitl.go    # ReActAgent HITL 中断/恢复集成方法
//	└── react_stream.go  # ReActAgent 流式执行（_inner_stream 模式）
//
// 对应 Python 代码：openjiuwen/core/agent/agents/
package agents
