// Package session 提供会话管理的核心功能，包括历史持久化、元数据管理、任务队列和重命名。
//
// 本包对齐 Python jiuwenswarm/server/runtime/session/ 包的同包结构，
// 使 session_history 和 session_metadata 可以互引，消除跨包依赖。
//
// 文件目录：
//
//	session/
//	├── doc.go              # 包文档
//	├── session_utils.go    # 通用辅助函数（AutoTitle / SerializeValue / deepCopyMap / derefStr / currentTimestamp / MakeSessionID / NormalizeSessionID）
//	├── session_history.go  # 会话历史持久化（history.json 读写 + team 过滤 + 元数据联动）
//	├── session_manager.go  # SessionManager（LIFO 会话队列）
//	├── session_metadata.go # 会话元数据管理（metadata.json 读写 + delivery context + 异步队列）
//	├── session_rename.go   # 会话重命名三种语义
//	├── session_startup.go  # 启动清理 + 分页查询
//
// 对应 Python 代码：jiuwenswarm/server/runtime/session/
package session
