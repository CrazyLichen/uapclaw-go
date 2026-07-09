// Package runtime 提供 AgentServer 运行时管理组件。
//
// 包含 UapClaw（Agent 门面）、SessionManager（LIFO 会话任务队列）、
// AgentManager（Agent 实例管理器）等运行时组件，
// 负责 Agent 实例的并发执行控制、任务调度和请求路由。
//
// 文件目录：
//
//	runtime/
//	├── doc.go                # 包文档
//	├── uapclaw.go          # UapClaw Agent 门面（层级 0+1 已实现，层级 2-4 ⤵️）
//	├── build_user_prompt.go  # BuildUserPrompt 用户 prompt 包装
//	├── build_inputs.go       # BuildInputs adapter 输入构建
//	├── session_history.go    # 会话历史持久化（history.json 读写）
//	├── session_manager.go    # SessionManager（LIFO 会话队列）
//	└── agent_manager.go      # AgentManager Agent 实例管理器（stub，10.3.12）
//
// 对应 Python 代码：jiuwenswarm/server/runtime/
package runtime
