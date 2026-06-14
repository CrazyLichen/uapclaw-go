// Package internal 提供会话的内部实现，不对外暴露。
//
// 本包包含 BaseSession 接口的具体实现（AgentSession 等），
// 由公开层 Session 组合使用，不应被外部包直接引用。
//
// 文件目录：
//
//	internal/
//	├── doc.go              # 包文档
//	└── agent_session.go    # AgentSession — BaseSession 的 Agent 会话实现
//
// 对应 Python 代码：openjiuwen/core/session/internal/agent.py
package internal
