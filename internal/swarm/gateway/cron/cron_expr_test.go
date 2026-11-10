package cron

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 转发层验证 ────────────────────────────
//
// 实体实现与完整用例已迁至 internal/swarm/runtime/cron/cron_expr_test.go。
// 本文件只验证兼容转发层与实体行为一致，避免转发写错。

// TestNormalizeCronExpr_转发一致 5 域补秒与年、7 域原样。
func TestNormalizeCronExpr_转发一致(t *testing.T) {
	got, err := NormalizeCronExpr("*/5 * * * *")
	require.NoError(t, err)
	require.Equal(t, "0 */5 * * * * *", got, "5 域补秒与年后为 7 域")

	got, err = NormalizeCronExpr("0 */5 * * * * *")
	require.NoError(t, err)
	require.Equal(t, "0 */5 * * * * *", got)

	_, err = NormalizeCronExpr("bad")
	require.Error(t, err)
}

// TestValidateCronExpression_转发一致 合法通过、非法报错。
func TestValidateCronExpression_转发一致(t *testing.T) {
	require.NoError(t, ValidateCronExpression("0 0 9 * * * *", "Asia/Shanghai"))
	require.Error(t, ValidateCronExpression("", "Asia/Shanghai"))
}

// TestNextFireTime_转发一致 能算出下次触发。
func TestNextFireTime_转发一致(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	next, err := NextFireTime("0 0 9 * * * *", base)
	require.NoError(t, err)
	require.True(t, next.After(base))
}

// TestISOToSevenFieldCron_转发一致 ISO 转 7 域。
func TestISOToSevenFieldCron_转发一致(t *testing.T) {
	got, err := ISOToSevenFieldCron("2026-03-15T09:30:00+08:00", "Asia/Shanghai")
	require.NoError(t, err)
	require.Contains(t, got, "30")
	require.Contains(t, got, "2026")
}

// TestIsNoNextDateError_转发一致 普通错误不为真。
func TestIsNoNextDateError_转发一致(t *testing.T) {
	require.False(t, IsNoNextDateError(nil))
	require.False(t, IsNoNextDateError(errors.New("boom")))
}
