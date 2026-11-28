package kbadmin

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Store kbadmin 域 SQLite 存储（AutoMigrate 自建五表，agent_teams 业务表惯例）
type Store struct {
	// db GORM 句柄
	db *gorm.DB
}

// ──────────────────────────── 导出函数 ────────────────────────────

// AllModels 返回五表模型切片（AutoMigrate 与集成测试共用）
func AllModels() []any {
	return []any{&KBInfo{}, &KBDoc{}, &KBChunkMeta{}, &KBChunkRevision{}, &KBTask{}}
}

// OpenStore 打开（或创建）kbadmin SQLite 存储并自动迁移五表
func OpenStore(dbPath string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("打开 kbadmin 存储失败: %w", err)
	}
	if err := db.AutoMigrate(AllModels()...); err != nil {
		return nil, fmt.Errorf("kbadmin 五表 AutoMigrate 失败: %w", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭底层连接
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
