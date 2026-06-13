package gaussdb

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"gorm.io/gorm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GaussDbStore GaussDB 数据库存储，实现 db.BaseDbStore 接口。
//
// 对应 Python: openjiuwen/extensions/store/db/gauss_db_store.py
//
// 本实现通过 GaussDialector 创建 *gorm.DB 实例，
// 并提供 GaussDB 特有的方言适配（LOCKING 子句简化、
// 字符串序列化、UUID/ENUM 类型映射等）。
type GaussDbStore struct {
	// db GORM 数据库实例（通过 GaussDialector 创建）
	db *gorm.DB
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGaussDbStore 从 DSN 创建 GaussDbStore。
// 内部使用 GaussDialector 连接 GaussDB，对等 Python 的 GaussDbStore(async_conn)。
// dsn 为 GaussDB 连接串（如 "host=localhost port=5432 dbname=mydb"），
// opts 为可选的 GORM 配置项。
func NewGaussDbStore(dsn string, opts ...gorm.Option) (*GaussDbStore, error) {
	dialector := GaussOpen(dsn)
	db, err := gorm.Open(dialector, opts...)
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).Str("dsn", dsn).Msg("GaussDB 连接创建失败")
		return nil, fmt.Errorf("GaussDB 连接创建失败: %w", err)
	}
	logger.Info(gaussLogComponent).Str("dialect", "gaussdb").Msg("GaussDB 数据库存储创建成功")
	return &GaussDbStore{db: db}, nil
}

// NewGaussDbStoreWithDB 从已有的 *gorm.DB 创建 GaussDbStore。
// 调用方需确保该 DB 使用了 GaussDialector。
func NewGaussDbStoreWithDB(db *gorm.DB) *GaussDbStore {
	return &GaussDbStore{db: db}
}

// GetDB 实现 db.BaseDbStore 接口，返回持有的 *gorm.DB 实例。
// 对标 Python: GaussDbStore.get_async_engine() -> AsyncEngine
func (s *GaussDbStore) GetDB(_ context.Context) *gorm.DB {
	return s.db
}

// Close 关闭数据库连接池。
// Python 中由 AsyncEngine 管理生命周期，Go 中需要显式关闭。
func (s *GaussDbStore) Close() error {
	if s.db == nil {
		return fmt.Errorf("GaussDbStore 未初始化，无法关闭")
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).Msg("获取底层数据库连接失败")
		return fmt.Errorf("获取底层数据库连接失败: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		logger.Error(gaussLogComponent).Err(err).Msg("关闭 GaussDB 连接失败")
		return fmt.Errorf("关闭 GaussDB 连接失败: %w", err)
	}
	logger.Info(gaussLogComponent).Msg("GaussDB 数据库连接已关闭")
	return nil
}
