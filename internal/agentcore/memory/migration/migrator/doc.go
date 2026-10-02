// Package migrator 提供存储层迁移执行器。
//
// 本包定义了 5 种存储后端的迁移执行器（Migrator），
// 每个执行器负责检测待迁移操作、执行迁移逻辑、处理失败回滚。
// 所有执行器实现 Migrator 接口（IndexVersionMigrator 除外）。
//
// 文件目录：
//
//	migrator/
//	├── doc.go                      # 包文档
//	├── memory_meta_manager.go      # MemoryMetaManager schema 版本管理器
//	├── kv_migrator.go              # KVMigrator KV 存储迁移执行器
//	├── sql_migrator.go             # SQLMigrator SQL 数据库迁移执行器（GORM Migrator）
//	├── vector_migrator.go          # VectorMigrator 向量存储迁移执行器
//	├── message_migrator.go         # MessageMigrator 消息存储迁移执行器
//	└── index_version_migrator.go   # IndexVersionMigrator 索引版本迁移执行器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/migration/migrator/
//
// 核心类型/接口索引：
//
//	SqlDbQuerier         — SqlDbStore 的最小接口，用于解耦 migrator 和 model 包
//	MemoryMetaManager    — schema 版本管理器，基于 SqlDbQuerier 操作 memory_meta 表
//	Migrator             — 迁移执行器接口（TryMigrate）
//	KVMigrator           — KV 存储迁移执行器，支持备份/恢复
//	SQLMigrator          — SQL 数据库迁移执行器，基于 GORM Migrator
//	VectorMigrator       — 向量存储迁移执行器，遍历集合并调用 UpdateSchema
//	MessageMigrator      — 消息存储迁移执行器，支持备份/恢复
//	IndexVersionMigrator — 索引版本迁移执行器，独立签名（无 entityKey）
package migrator
