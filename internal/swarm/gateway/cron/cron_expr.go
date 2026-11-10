package cron

import (
	"time"

	runtimecron "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/cron"
)

// ──────────────────────────── 导出函数 ────────────────────────────
//
// 本文件是兼容转发层：cron 表达式实现已归属 Runtime 层
// （上游 jiuwenswarm/gateway/cron/cron_expr.py 已退化为 7 行
//  `sys.modules[__name__] = _implementation` 的别名壳）。
// 实体在 internal/swarm/runtime/cron/cron_expr.go，此处只做转调。

// NormalizeCronExpr 将表达式归一化为 7 域 Quartz 格式。
// 转发至 runtimecron.NormalizeCronExpr。
func NormalizeCronExpr(raw string) (string, error) {
	return runtimecron.NormalizeCronExpr(raw)
}

// ISOToSevenFieldCron 将 ISO8601 时间转为 7 域 cron 表达式。
// 转发至 runtimecron.ISOToSevenFieldCron。
func ISOToSevenFieldCron(atISO string, timezone string) (string, error) {
	return runtimecron.ISOToSevenFieldCron(atISO, timezone)
}

// ValidateCronExpression 校验 cron 表达式（5 域或 7 域）。
// 转发至 runtimecron.ValidateCronExpression。
func ValidateCronExpression(expr string, timezone string) error {
	return runtimecron.ValidateCronExpression(expr, timezone)
}

// NextFireTime 计算下一次触发时刻。
// 转发至 runtimecron.NextFireTime。
func NextFireTime(expr string, base time.Time) (time.Time, error) {
	return runtimecron.NextFireTime(expr, base)
}

// IsNoNextDateError 判断是否为"算不出下一次触发"。
// 转发至 runtimecron.IsNoNextDateError。
func IsNoNextDateError(err error) bool {
	return runtimecron.IsNoNextDateError(err)
}
