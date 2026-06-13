package gaussdb

import (
	"gorm.io/gorm/clause"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// gaussLockingClauseBuilder GaussDB LOCKING 子句构建器。
//
// 对应 Python: GaussCompiler.for_update_clause()
//
// GaussDB 不支持 NOWAIT / SKIP LOCKED 锁选项和 OF table 语法，
// 也不支持 FOR SHARE 等锁类型，仅支持 FOR UPDATE。
// 此构建器硬编码输出 "FOR UPDATE"，对齐 Python 行为。
// 当表达式不是 clause.Locking 类型时，回退到默认的 Clause.Build()。
func gaussLockingClauseBuilder(c clause.Clause, builder clause.Builder) {
	if _, ok := c.Expression.(clause.Locking); ok {
		// 对标 Python: GaussCompiler.for_update_clause() 始终返回 " FOR UPDATE"
		// 忽略 locking.Strength / locking.Table / locking.Options
		_, _ = builder.WriteString("FOR UPDATE")
		return
	}
	// 非 Locking 表达式，回退到默认构建
	c.Build(builder)
}
