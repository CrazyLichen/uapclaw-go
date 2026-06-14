// Package session 提供会话管理的抽象接口和代理实现。
//
// 本包定义 BaseSession 接口，作为所有会话类型（AgentSession、WorkflowSession 等）
// 的统一抽象。BaseSession 提供 Config/State/Tracer/StreamWriterManager/SessionID/
// Checkpointer/ActorManager/Close 八个核心能力。ProxySession 实现代理模式，将调用
// 委托给内部 stub，支持运行时替换底层会话。
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
//	└── state/              # 状态接口与内存实现（5.1 已完成）
//
// 对应 Python 代码：openjiuwen/core/session/session.py
//
// 核心类型/接口索引：
//
//	BaseSession    — 会话基类接口，所有会话类型的核心抽象
//	ProxySession   — 代理会话，将调用委托给内部 stub
package session
