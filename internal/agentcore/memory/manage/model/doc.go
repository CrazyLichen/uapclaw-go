// Package model 提供记忆系统的数据模型和数据库操作。
//
// 本包定义了消息存储相关的数据库模型（UserMessage）、
// 通用 SQL CRUD 层（SqlDbStore）、消息存储实现（SqlMessageStore）
// 和消息管理器（MessageManager）。
// Schema 版本管理已迁移到 migrator 包，加解密编解码已迁移到 codec 包。
//
// 文件目录：
//
//	model/
//	├── doc.go                 # 包文档
//	├── db_model.go            # 数据库模型（UserMessage、ScopeUserMapping、MemoryMeta）
//	├── sql_db_store.go        # SqlDbStore 通用 SQL CRUD 层
//	├── sql_message_store.go   # SqlMessageStore 消息存储实现
//	└── message_manager.go     # MessageManager 消息管理器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/mem_model/
//
// 关联包：
//
//	memory/codec/              — AesStorageCodec 存储编解码器
//	memory/migration/migrator/ — MemoryMetaManager schema 版本管理器
//
// 核心类型/接口索引：
//
//	UserMessage      — 用户消息表 GORM 模型
//	ScopeUserMapping — 作用域用户映射表 GORM 模型
//	MemoryMeta       — 记忆元数据表 GORM 模型
//	SqlDbStore       — 通用 SQL CRUD 层，封装 GORM 通用操作
//	SqlMessageStore  — BaseMessageStore 的 SQL 实现
//	MessageManager   — 消息管理器，BaseMessageStore 的上层封装
package model
