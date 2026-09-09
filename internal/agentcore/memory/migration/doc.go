// Package migration 提供记忆存储的迁移注册表和迁移管理器。
//
// 本包定义全局迁移注册表（SQL / Vector / KV / Message / Index），
// 各 Operation 通过注册表声明迁移步骤，由 Migrator 按序执行。
//
// 文件目录：
//
//	migration/
//	├── doc.go                  # 包文档
//	├── migration_plan.go       # 全局迁移注册表（SQLRegistry 等）
//	├── migrator/               # 迁移执行器
//	│   ├── doc.go              # 子包文档
//	│   └── memory_meta_manager.go  # 记忆元数据迁移管理器
//	└── operation/              # 迁移操作定义
//	    ├── doc.go              # 子包文档
//	    ├── base_operation.go   # 操作基类
//	    ├── operation_registry.go  # 操作注册表
//	    └── operations.go       # 具体迁移操作
//
// 对应 Python 代码：openjiuwen/core/memory/migration/
package migration
