package migration

import "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// SQLRegistry SQL 表迁移注册表。
	// 对齐 Python: sql_registry = OperationRegistry()
	SQLRegistry = operation.NewOperationRegistry()
	// VectorRegistry 向量集合迁移注册表。
	// 对齐 Python: vector_registry = OperationRegistry()
	VectorRegistry = operation.NewOperationRegistry()
	// KVRegistry KV 存储迁移注册表。
	// 对齐 Python: kv_registry = OperationRegistry()
	KVRegistry = operation.NewOperationRegistry()
	// MessageRegistry 消息存储迁移注册表。
	// 对齐 Python: message_registry = OperationRegistry()
	MessageRegistry = operation.NewOperationRegistry()
	// IndexRegistry 记忆索引迁移注册表。
	// 对齐 Python: index_registry = OperationRegistry()
	IndexRegistry = operation.NewOperationRegistry()
)
