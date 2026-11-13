// Package storage CLI 会话的结构化存储（JSON 文件）。
//
// 每个 CLI 会话持久化为 ~/.openjiuwen/sessions/ 下的一个 JSON 文件：
// StoredSession（会话标识、模型、创建时间、有序消息）。SessionStore
// 提供 new_session / add_message / list_sessions 三个入口；list_sessions
// 对损坏文件逐个跳过，保证 REPL 的 /sessions 命令不被单个坏文件炸穿。
//
// 文件目录：
//
//	storage/
//	├── doc.go               # 包文档
//	├── session_store.go    # StoredMessage / StoredSession / SessionStore
//	└── session_store_test.go
//
// 对应 Python 代码：openjiuwen/harness/cli/storage/session_store.py（140 行）
package storage
