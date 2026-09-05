package mem_model

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration"
)

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

// ScopeUserMapping 作用域用户映射表模型。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/db_model.py (ScopeUserMapping)
type ScopeUserMapping struct {
	// UserID 用户 ID
	UserID string `gorm:"primaryKey;size:64;not null"`
	// ScopeID 作用域 ID
	ScopeID string `gorm:"primaryKey;size:64;not null"`
}

// MemoryMeta 记忆元数据表模型，用于 schema 版本管理。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/db_model.py (MemoryMeta)
type MemoryMeta struct {
	// TblName 元数据对应的表名
	TblName string `gorm:"primaryKey;size:64;not null;column:table_name"`
	// SchemaVersion schema 版本号
	SchemaVersion string `gorm:"size:64;not null"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TableName 指定 UserMessage 表名。
func (UserMessage) TableName() string { return "user_message" }

// TableName 指定 ScopeUserMapping 表名。
func (ScopeUserMapping) TableName() string { return "scope_user_mapping" }

// TableName 指定 MemoryMeta 表名。
// 使用 memory_meta 作为表名，避免与字段 TblName 冲突。
func (MemoryMeta) TableName() string { return "memory_meta" }

// CreateTables 创建所有记忆表。
// 对齐 Python: openjiuwen/core/memory/manage/mem_model/db_model.py (create_tables)
//
// 步骤：
//  1. 检测 user_message 表是否有旧版 group_id 列，有则 DROP 重建
//  2. 使用 GORM AutoMigrate 自动建表
//  3. 为新创建的表写入初始 schema_version（从 sql_registry 获取当前版本）
func CreateTables(db *gorm.DB) error {
	// 步骤1：旧表迁移检测——检测 user_message 表是否有旧版 group_id 列
	// 对齐 Python: if "group_id" in column_names → DROP TABLE
	if db.Migrator().HasTable(&UserMessage{}) && db.Migrator().HasColumn(&UserMessage{}, "group_id") {
		_ = db.Migrator().DropTable(&UserMessage{})
	}

	// 步骤2：建表
	if err := db.AutoMigrate(
		&UserMessage{},
		&ScopeUserMapping{},
		&MemoryMeta{},
	); err != nil {
		return err
	}

	// 步骤3：为新表写入初始 schema_version
	// 对齐 Python: current_version = sql_registry.get_current_version(entity_key)
	// 仅 current_version > 0 时写入，等于 0 时不写
	tableEntityKeys := map[string]string{
		"user_message":       "user_message",
		"scope_user_mapping": "scope_user_mapping",
	}
	for tbl, entityKey := range tableEntityKeys {
		var count int64
		db.Model(&MemoryMeta{}).Where("table_name = ?", tbl).Count(&count)
		if count == 0 {
			currentVersion := migration.SQLRegistry.GetCurrentVersion(entityKey)
			// 对齐 Python: create_tables 总是为新表写入 schema_version="0"
			// 如果注册表有版本，使用注册表版本；否则默认写入 "0"
			versionToWrite := 0
			if currentVersion > 0 {
				versionToWrite = currentVersion
			}
			db.Create(&MemoryMeta{TblName: tbl, SchemaVersion: fmt.Sprintf("%d", versionToWrite)})
		}
	}

	return nil
}
