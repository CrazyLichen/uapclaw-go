package db

import (
	"context"

	"gorm.io/gorm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DefaultDbStore BaseDbStore 的默认实现，持有并暴露 *gorm.DB 实例。
//
// 本实现适用于所有 GORM 支持的数据库（SQLite、PostgreSQL、MySQL 等），
// 调用方负责创建和管理 *gorm.DB 的生命周期，DefaultDbStore 不提供 Close 方法。
//
// 对应 Python: openjiuwen/core/foundation/store/db/default_db_store.py
type DefaultDbStore struct {
	// db GORM 数据库实例
	db *gorm.DB
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDefaultDbStore 创建 DefaultDbStore 实例。
// 调用方应确保传入有效的 *gorm.DB 实例。
func NewDefaultDbStore(db *gorm.DB) *DefaultDbStore {
	return &DefaultDbStore{db: db}
}

// GetDB 实现 BaseDbStore 接口，返回持有的 *gorm.DB 实例。
func (s *DefaultDbStore) GetDB(_ context.Context) *gorm.DB {
	return s.db
}
