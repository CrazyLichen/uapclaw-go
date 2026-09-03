// Package operation 提供存储层迁移操作的类型定义和注册表。
//
// 本包定义了迁移操作的基础类型（OperationMetadata/BaseOperation）、
// 操作多态接口（Operation）、操作注册表（OperationRegistry）
// 和 13 种具体操作类型（SQL/Vector/KV/Message/Index）。
//
// 文件目录：
//
//	operation/
//	├── doc.go                  # 包文档
//	├── base_operation.go       # OperationMetadata + BaseOperation + Operation 接口
//	├── operation_registry.go   # OperationRegistry 注册表
//	└── operations.go           # 13 个具体 Operation 类型
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/migration/operation/
//
// 核心类型/接口索引：
//
//	OperationMetadata — 操作元数据（schema_version + description）
//	BaseOperation     — 操作基类（纯 DTO，嵌入后满足 Operation 接口）
//	Operation         — 操作多态接口（SchemaVersion + Description）
//	OperationRegistry — 按实体键管理链式升级操作的注册表
package operation
