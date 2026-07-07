// Package runtime 提供 AgentServer 运行时管理组件。
//
// 包含 SessionManager（LIFO 会话任务队列）和 JiuWenClaw（Agent 门面）等运行时组件，
// 负责 Agent 实例的并发执行控制、任务调度和请求路由。
//
// 文件目录：
//
//	runtime/
//	├── doc.go              # 包文档
//	├── session_manager.go  # SessionManager（LIFO 会话队列）
//	├── session_manager_test.go # SessionManager 单元测试
//	├── jiowenclaw.go       # JiuWenClaw 门面（10.3.2）
//	└── agent_manager.go    # AgentManager（10.3.12）
//
// 对应 Python 代码：jiuwenswarm/server/runtime/
package runtime
