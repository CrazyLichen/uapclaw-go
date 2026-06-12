package model

import "gorm.io/gorm"

// ──────────────────────────── 结构体 ────────────────────────────

// UserMessage 用户消息表模型。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/db_model.py (UserMessage)
type UserMessage struct {
	// MessageID 消息唯一标识（SHA-256 hash 前16位 + 时间戳毫秒）
	MessageID string `gorm:"primaryKey;size:64"`
	// UserID 用户 ID
	UserID string `gorm:"size:64;not null"`
	// ScopeID 作用域 ID
	ScopeID string `gorm:"size:64;not null"`
	// Content 消息内容（AES 加密后存储）
	Content string `gorm:"size:4096;not null"`
	// SessionID 会话 ID
	SessionID string `gorm:"size:64"`
	// Role 消息角色
	Role string `gorm:"size:32"`
	// Timestamp 时间戳（ISO 字符串，对齐 Python）
	Timestamp string `gorm:"size:32"`
}

// TableName 指定表名。
func (UserMessage) TableName() string { return "user_message" }

// ScopeUserMapping 作用域用户映射表模型。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/db_model.py (ScopeUserMapping)
type ScopeUserMapping struct {
	// UserID 用户 ID
	UserID string `gorm:"primaryKey;size:64;not null"`
	// ScopeID 作用域 ID
	ScopeID string `gorm:"primaryKey;size:64;not null"`
}

// TableName 指定表名。
func (ScopeUserMapping) TableName() string { return "scope_user_mapping" }

// MemoryMeta 记忆元数据表模型，用于 schema 版本管理。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/db_model.py (MemoryMeta)
type MemoryMeta struct {
	// TblName 元数据对应的表名
	TblName string `gorm:"primaryKey;size:64;not null;column:table_name"`
	// SchemaVersion schema 版本号
	SchemaVersion string `gorm:"size:64;not null"`
}

// TableName 指定表名。
// 使用 memory_meta 作为表名，避免与字段 TblName 冲突。
func (MemoryMeta) TableName() string { return "memory_meta" }

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateTables 创建所有记忆表。
// 使用 GORM AutoMigrate 自动建表，对齐 Python 的 create_tables()。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/db_model.py (create_tables)
func CreateTables(db *gorm.DB) error {
	return db.AutoMigrate(
		&UserMessage{},
		&ScopeUserMapping{},
		&MemoryMeta{},
	)
}
