package gaussdb

import (
	"gorm.io/driver/postgres"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GaussMigrator GaussDB 迁移器，基于 PostgreSQL 迁移器扩展。
//
// 对应 Python: GaussDialectAsyncpg._domain_query / _enum_query / _patch_gaussdb_reflection_sql
//
// Python 的 GaussDB 方言需要覆写以下 SQLAlchemy 特有的内省行为：
//   - _patch_gaussdb_reflection_sql: 拦截含 pg_type.typcollation 的子查询并替换为 NULL
//   - _domain_query / _enum_query: 返回 SELECT 1 WHERE FALSE（跳过 domain/enum 内省）
//
// Go 侧使用 GORM，GORM postgres Migrator 的 ColumnTypes() 方法：
//   - 仅 JOIN pg_type ON typname 并使用 typlen，不查询 typcollation
//   - 不做 domain/enum 类型内省
//   - 因此对 GaussDB 天然兼容，无需覆写
//
// 本 Migrator 预留覆写点，以便未来 GORM 版本变更时快速适配。
// 如果未来 GORM 版本的 postgres Migrator 引入了 GaussDB 不兼容的 SQL
// （如查询 pg_type.typcollation 或 pg_enum），应在此处覆写
// ColumnTypes() 方法进行改写。
type GaussMigrator struct {
	postgres.Migrator
}
