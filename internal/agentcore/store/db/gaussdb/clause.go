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
// 此构建器忽略 Locking.Options 和 Locking.Table，仅输出 "FOR <strength>"。
// 当表达式不是 clause.Locking 类型时，回退到默认的 Clause.Build()。
func gaussLockingClauseBuilder(c clause.Clause, builder clause.Builder) {
	if locking, ok := c.Expression.(clause.Locking); ok {
		// 对标 Python: GaussCompiler.for_update_clause() 始终返回 " FOR UPDATE"
		// 忽略 locking.Table 和 locking.Options
		builder.WriteString("FOR ")
		builder.WriteString(locking.Strength)
		return
	}
	// 非 Locking 表达式，回退到默认构建
	c.Build(builder)
}
