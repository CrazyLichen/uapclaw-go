// Package sessionctx 提供会话上下文状态管理。
//
// 对齐 Python: openjiuwen/agent_teams/context.py
// 提供 SessionState（对齐 Python contextvars.ContextVar）和基于 context.Context 的传播机制。
// 作为独立子包存在，避免 schema↔database 循环依赖：
//   - database 包可 import sessionctx 获取 GetSessionID
//   - schema 包也可 import sessionctx，不再需要 database 方向的反向引用
//
// 文件目录：
//
//	sessionctx/
//	├── doc.go              # 包文档
//	└── session_state.go    # SessionState/GetSessionID 会话上下文管理
//
// 对应 Python 代码：openjiuwen/agent_teams/context.py
package sessionctx
