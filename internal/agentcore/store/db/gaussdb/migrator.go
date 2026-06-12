package gaussdb

import (
	"gorm.io/driver/postgres"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GaussMigrator GaussDB 迁移器，基于 PostgreSQL 迁移器扩展。
//
// 对应 Python: GaussDialectAsyncpg._domain_query / _enum_query / _get_server_version_info
//
// 当前 GORM postgres Migrator 的 SQL 不查询 pg_type.typcollation，
// 也不做 domain/enum 内省，因此 GaussDB 天然兼容。
// 本 Migrator 预留覆写点，以便未来 GORM 版本变更时快速适配。
//
// 如果未来 GORM 版本的 postgres Migrator 引入了 GaussDB 不兼容的 SQL
// （如查询 pg_type.typcollation 或 pg_enum），应在此处覆写
// ColumnTypes() 方法进行改写。
type GaussMigrator struct {
	postgres.Migrator
}
