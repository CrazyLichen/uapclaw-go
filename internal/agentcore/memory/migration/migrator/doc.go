// Package migrator 提供存储层迁移管理工具。
//
// 本包定义了数据库 schema 版本管理器，
// 用于跟踪和更新各存储表的 schema 版本。
// MemoryMetaManager 通过 SqlDbStore 操作 memory_meta 表，
// 实现版本记录的增删查功能。
//
// 文件目录：
//
//	migrator/
//	├── doc.go                     # 包文档
//	└── memory_meta_manager.go     # MemoryMetaManager schema 版本管理器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/migration/migrator/
//
// 核心类型/接口索引：
//
//	SqlDbQuerier      — SqlDbStore 的最小接口，用于解耦 migrator 和 model 包
//	MemoryMetaManager — schema 版本管理器，基于 SqlDbQuerier 操作 memory_meta 表
package migrator
