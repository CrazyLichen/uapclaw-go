// Package db 提供 SQL 数据库的抽象接口定义。
//
// 本包定义了数据库连接的依赖注入接口 BaseDbStore，
// 让上层组件（SqlDbStore、SqlMessageStore 等）通过接口获取数据库连接，
// 而非直接依赖具体引擎。BaseDbStore 本身不提供任何数据存储能力，
// 唯一的职责是暴露 *gorm.DB 实例。
//
// 文件目录：
//
//	db/
//	├── doc.go                    # 包文档
//	├── base.go                   # BaseDbStore 接口定义
//	├── base_message_store.go     # BaseMessageStore 接口及辅助类型
//	├── default.go                # DefaultDbStore 默认实现
//	└── gaussdb/                  # GaussDB 数据库扩展
//	    ├── doc.go                # 包文档
//	    ├── dialector.go          # GaussDB 方言定义
//	    ├── store.go              # GaussDbStore 存储实现
//	    ├── clause.go             # GaussDB LOCKING 子句构建器
//	    ├── serializer.go         # GaussDB 字符串序列化器
//	    └── migrator.go           # GaussDB 迁移器
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/base_db_store.py
//
// 核心类型/接口索引：
//
//	BaseDbStore        — SQL 数据库连接抽象接口，通过 GetDB 返回 *gorm.DB 实例
//	DefaultDbStore     — BaseDbStore 的默认实现，直接持有并返回 *gorm.DB
//	GaussDbStore       — BaseDbStore 的 GaussDB 实现，提供 GaussDB 特有方言适配（在 gaussdb 子包中）
//	BaseMessageStore   — 消息持久化接口，定义消息的 CRUD、计数和 schema 版本管理（消息类型为 schema.BaseMessage 接口）
//	MessageMetadata    — 消息元数据（message_id, user_id, scope_id, session_id, timestamp, message_type）
//	MessageAdd         — 添加消息的入参
//	MessageFilter      — 消息查询过滤条件
//	MessageAndMeta     — 消息+元数据组合
package db
