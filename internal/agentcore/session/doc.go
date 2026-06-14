// Package session 提供会话管理的抽象接口、代理实现和 Agent 公开会话。
//
// 本包定义 BaseSession 接口，作为所有会话类型的统一抽象。ProxySession 实现代理模式。
// Session 是 Agent 场景下的公开会话，组合内部层 AgentSession，提供 PreRun/PostRun
// 生命周期、状态读写、流写入等用户面向 API。
//
// 本包依赖 state 子包提供的状态接口（State/CommitState 等），Config/Tracer/
// StreamWriterManager/Checkpointer/ActorManager 等依赖类型暂用 any 占位，
// 待后续步骤（5.8/5.10/5.11/5.12）回填具体类型。
//
// 文件目录：
//
//	session/
//	├── doc.go              # 包文档
//	├── session.go          # BaseSession 接口 + ProxySession 实现
//	├── agent.go            # Session 公开会话（Agent 场景）
//	├── state/              # 状态接口与内存实现（5.1 已完成）
//	└── internal/           # 内部会话实现
//	    └── agent_session.go  # AgentSession（BaseSession 实现）
//
// 对应 Python 代码：openjiuwen/core/session/agent.py + openjiuwen/core/session/session.py
//
// 核心类型/接口索引：
//
//	BaseSession    — 会话基类接口，所有会话类型的核心抽象
//	ProxySession   — 代理会话，将调用委托给内部 stub
//	Session        — Agent 公开会话，用户面向 API
package session
