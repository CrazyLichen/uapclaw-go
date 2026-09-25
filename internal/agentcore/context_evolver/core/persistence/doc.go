// Package persistence 提供上下文记忆演化系统的记忆持久化助手。
//
// MemoryPersistenceHelper 支持 JSON 文件和 Milvus 双后端。
// "auto" 模式惰性探测 Milvus 可达性后回退 JSON。
// P7 已实现 MilvusConnectorImpl（固定 5 字段 schema + namespace 分区）。
//
// 文件目录：
//
//	persistence/
//	├── doc.go                   # 包文档
//	├── persistence_helper.go    # MemoryPersistenceHelper + MilvusConnector 接口 + auto 模式
//	└── milvus_connector.go      # MilvusConnectorImpl Milvus 向量数据库连接器实现（5 字段 schema + namespace 分区 + 惰性客户端）
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/persistence.py + db_connector/milvus_connector.py
package persistence
