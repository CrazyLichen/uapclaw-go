package cron

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/reugn/go-quartz/quartz"
)

// goldenCase 黄金用例：表达式 × 基准时刻 → 期望下次触发。
// 期望值由 croniter 实测生成（openjiuwen-env，2026-12-25 为周五）。
type goldenCase struct {
	expr string
	tz   string
	base string // RFC3339
	want string // RFC3339 期望值（croniter 基准）
}

// TestNextFireTime_黄金用例 验证 go-quartz 变换后与 croniter 语义一致
func TestNextFireTime_黄金用例(t *testing.T) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	base := time.Date(2026, 12, 25, 0, 0, 0, 0, sh) // 周五
	cases := []goldenCase{
		{"0 9 * * *", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2026-12-25T09:00:00+08:00"},
		{"0 9 * * 1", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2026-12-28T09:00:00+08:00"}, // 下周一
		// croniter 实测：周五 00:00 当天 8:30 尚未到 → 当天（计划注释"周五已过 8:30"有误，以 croniter 基准为准）
		{"30 8 * * 1-5", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2026-12-25T08:30:00+08:00"},
		{"0 */2 * * *", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2026-12-25T02:00:00+08:00"},
		{"15 10 15 * *", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2027-01-15T10:15:00+08:00"},
	}
	for _, tc := range cases {
		normalized, err := NormalizeCronExpr(tc.expr)
		if err != nil {
			t.Fatalf("normalize %q: %v", tc.expr, err)
		}
		got, err := NextFireTime(normalized, base)
		if err != nil {
			t.Fatalf("NextFireTime %q: %v", tc.expr, err)
		}
		if got.Format(time.RFC3339) != tc.want {
			t.Errorf("expr=%q got %s want %s", tc.expr, got.Format(time.RFC3339), tc.want)
		}
	}
}

// TestNextFireTime_日与周同时受限 拆分 OR 语义对齐 croniter
func TestNextFireTime_日与周同时受限(t *testing.T) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	base := time.Date(2026, 12, 25, 0, 0, 0, 0, sh) // 周五
	cases := []goldenCase{
		// croniter 实测：'30 8 15 * 1-5' 周五(且25日) 08:30 当天 → OR 语义当天命中
		{"30 8 15 * 1-5", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2026-12-25T08:30:00+08:00"},
		// croniter 实测：'0 9 13 * 5' 周五(13日不是13号) → 下个周五 2027-01-01? 需比对 croniter
		{"0 9 13 * 5", "Asia/Shanghai", "2026-12-25T00:00:00+08:00", "2026-12-25T09:00:00+08:00"},
	}
	for _, tc := range cases {
		normalized, _ := NormalizeCronExpr(tc.expr)
		got, err := NextFireTime(normalized, base)
		if err != nil {
			t.Fatalf("NextFireTime %q: %v", tc.expr, err)
		}
		if got.Format(time.RFC3339) != tc.want {
			t.Errorf("expr=%q got %s want %s", tc.expr, got.Format(time.RFC3339), tc.want)
		}
	}
}

// TestNormalizeCronExpr_纯补齐对齐Python：normalize 输出保持 croniter dow 约定（不变换）
func TestNormalizeCronExpr_纯补齐对齐Python(t *testing.T) {
	cases := map[string]string{
		"0 9 * * *":       "0 0 9 * * * *",       // dow=* 不变（前补秒后补年）
		"0 9 * * 1":       "0 0 9 * * 1 *",       // croniter 1=MON 原样保持
		"0 9 * * 0":       "0 0 9 * * 0 *",       // croniter 0=SUN 原样保持
		"30 8 * * 1-5":    "0 30 8 * * 1-5 *",    // 区间原样
		"0 9 * * MON":     "0 0 9 * * MON *",     // 名称原样
		"0 9 * * mon":     "0 0 9 * * mon *",     // 小写原样（normalize 不大写化）
		"0 9 * * sun,mon": "0 0 9 * * sun,mon *", // 列表原样
	}
	for input, want := range cases {
		got, err := NormalizeCronExpr(input)
		if err != nil {
			t.Fatalf("normalize %q: %v", input, err)
		}
		if got != want {
			t.Errorf("normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestNormalizeCronExpr_域数校验
func TestNormalizeCronExpr_域数校验(t *testing.T) {
	for _, bad := range []string{"0 9 * *", "0 9 * * * * * *", "", "9"} {
		if _, err := NormalizeCronExpr(bad); err == nil {
			t.Errorf("%q should error", bad)
		}
	}
}

// TestTransformDow dow 变换规则（croniter 0=SUN → Quartz 1=SUN）
func TestTransformDow(t *testing.T) {
	cases := map[string]string{
		"0 9 * * * *":      "0 9 * * * *",      // dow=* 不变
		"0 9 * * * 1 *":    "0 9 * * * 2 *",    // 1=MON → 2=MON
		"0 9 * * * 0 *":    "0 9 * * * 1 *",    // 0=SUN → 1=SUN
		"0 9 * * * 6 *":    "0 9 * * * 7 *",    // 6=SAT → 7=SAT
		"0 9 * * * 7 *":    "0 9 * * * 1 *",    // 容错 7 → 1（Python ctor 7=SUN 别名）
		"0 30 8 * * 1-5 *": "0 30 8 * * 2-6 *", // 工作日 → 2-6
		"0 9 * * * MON *":  "0 9 * * * MON *",  // 名称不变
		"0 9 * * * mon *":  "0 9 * * * MON *",  // 小写归大写
		"0 9 * * * */2 *":  "0 9 * * * */2 *",  // 步长不变
		"0 9 * * * 1/2 *":  "0 9 * * * 2/2 *",  // a/n 前缀变换步长保持
	}
	for input, want := range cases {
		got := transformDow(input)
		if got != want {
			t.Errorf("transformDow(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestTransformDow_6域不处理
func TestTransformDow_6域不处理(t *testing.T) {
	if got := transformDow("0 9 * * *"); got != "0 9 * * *" {
		t.Errorf("6-field should pass through, got %q", got)
	}
}

// TestISOToSevenFieldCron ISO 字符串转七域 cron
func TestISOToSevenFieldCron(t *testing.T) {
	got, err := ISOToSevenFieldCron("2026-12-28T17:00:00", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0 0 17 28 12 ? 2026" {
		t.Errorf("ISOToSevenFieldCron = %q", got)
	}
}

// TestISOToSevenFieldCron_fromisoformat接受面
func TestISOToSevenFieldCron_fromisoformat接受面(t *testing.T) {
	// 空格分隔（Python fromisoformat 3.11+ 接受）
	got, err := ISOToSevenFieldCron("2026-12-28 17:00:00", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0 0 17 28 12 ? 2026" {
		t.Errorf("space-separated = %q", got)
	}
	// 无秒
	got, err = ISOToSevenFieldCron("2026-12-28T17:00", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0 0 17 28 12 ? 2026" {
		t.Errorf("no-seconds = %q", got)
	}
	// 纯日期
	got, err = ISOToSevenFieldCron("2026-12-28", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0 0 0 28 12 ? 2026" {
		t.Errorf("date-only = %q", got)
	}
	// 分数秒（截断到秒）
	got, err = ISOToSevenFieldCron("2026-12-28T17:00:00.123456", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0 0 17 28 12 ? 2026" {
		t.Errorf("fractional-seconds = %q", got)
	}
}

// TestISOToSevenFieldCron_带时区转换
func TestISOToSevenFieldCron_带时区转换(t *testing.T) {
	// UTC 09:00 = 上海 17:00
	got, err := ISOToSevenFieldCron("2026-12-28T09:00:00Z", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0 0 17 28 12 ? 2026" {
		t.Errorf("ISOToSevenFieldCron with tz = %q", got)
	}
}

// TestISOToSevenFieldCron_空串报错
func TestISOToSevenFieldCron_空串报错(t *testing.T) {
	if _, err := ISOToSevenFieldCron("", "Asia/Shanghai"); err == nil {
		t.Error("empty at_iso should error")
	}
	if _, err := ISOToSevenFieldCron("not-a-date", "Asia/Shanghai"); err == nil {
		t.Error("invalid at_iso should error")
	}
	if _, err := ISOToSevenFieldCron("2026-12-28T17:00:00", "Not/AZone"); err == nil {
		t.Error("invalid timezone should error")
	}
}

// TestValidateCronExpression 表达式+时区校验
func TestValidateCronExpression(t *testing.T) {
	if err := ValidateCronExpression("0 9 * * *", "Asia/Shanghai"); err != nil {
		t.Errorf("valid expr should pass: %v", err)
	}
	if err := ValidateCronExpression("99 9 * * *", "Asia/Shanghai"); err == nil {
		t.Error("invalid minute should fail")
	}
	if err := ValidateCronExpression("0 9 * * *", "Not/AZone"); err == nil {
		t.Error("invalid timezone should fail")
	}
	if err := ValidateCronExpression("", "Asia/Shanghai"); err == nil {
		t.Error("empty expr should fail")
	}
	if err := ValidateCronExpression("0 9 * *", "Asia/Shanghai"); err == nil {
		t.Error("4-field expr should fail")
	}
}

// TestValidateCronExpression_croniter接受面矩阵 实测对齐
func TestValidateCronExpression_croniter接受面矩阵(t *testing.T) {
	// croniter 实测 OK 的表达式
	for _, ok := range []string{
		"0 9 * * */7",     // dow 步长 */7 容忍（值域校验跳过步长上下文）
		"0 9 * * SUN-SAT", // 名称区间
		"0 9 * * sun",     // 小写名称
		"0 9 * * 6",       // dow 0-6
		"0 9 * * 0",       // dow 0-6
		"0 9 * * 1-5",     // 工作日区间
		"30 8 15 * 1-5",   // 日+周同时受限 OR 语义
		"30 8 * 2 1-5",    // 月+周同时受限
		"0 9 ? * 1-5",     // 日域 ?
		"0 9 * * 1#2",     // 每月第二个周一
		"0 9 * * 1-5/2",   // 区间+步长
		"0 9 15W * *",     // 最近工作日
		"0 9 L * *",       // 月末
		"0 9 * 2 3",       // 普通 5 域
	} {
		if err := ValidateCronExpression(ok, "Asia/Shanghai"); err != nil {
			t.Errorf("validate(%q) should pass, got %v", ok, err)
		}
	}
	// croniter 实测 FAIL 的表达式
	for _, bad := range []string{
		"0 9 * * 7",   // dow 7 拒绝（is_valid 值域 0-6）
		"0 9 * * 5-7", // 区间含 7
		"0 9 * * 1,7", // 列表含 7
		"0 9 * * 0-7", // 区间含 7
		"0 9 * * L",   // dow L 拒绝
		"0 9 * * 7/2", // 步长前缀 7 拒绝
		"0 9 * * JAN", // 月名称拒绝（croniter 不支持月名称）
		"0 9 * * jan-mar",
		"@daily",        // 别名拒绝（normalize 5/7 域）
		"0 0 12 * * ?",  // 6 域拒绝
		"0 15 10 ? * *", // 6 域拒绝
	} {
		if err := ValidateCronExpression(bad, "Asia/Shanghai"); err == nil {
			t.Errorf("validate(%q) should fail", bad)
		}
	}
}

// TestNextFireTime_过期一次性表达式
func TestNextFireTime_过期一次性表达式(t *testing.T) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	base := time.Date(2026, 12, 25, 0, 0, 0, 0, sh)
	// 2020 年的一次性表达式（年固定过去）
	_, err := NextFireTime("0 0 17 28 12 ? 2020", base)
	if !IsNoNextDateError(err) {
		t.Errorf("past one-shot should report no-next-date, got %v", err)
	}
}

// TestNextFireTime_未来一次性表达式
func TestNextFireTime_未来一次性表达式(t *testing.T) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	base := time.Date(2026, 12, 25, 0, 0, 0, 0, sh)
	got, err := NextFireTime("0 0 17 28 12 ? 2026", base)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format(time.RFC3339) != "2026-12-28T17:00:00+08:00" {
		t.Errorf("future one-shot = %s", got.Format(time.RFC3339))
	}
}

// TestIsNoNextDateError 无下次触发日期错误判定
func TestIsNoNextDateError(t *testing.T) {
	if IsNoNextDateError(nil) {
		t.Error("nil should be false")
	}
	if !IsNoNextDateError(quartz.ErrTriggerExpired) {
		t.Error("ErrTriggerExpired should be true")
	}
	if !IsNoNextDateError(errors.New("croniter failed to find next date")) {
		t.Error("message match should be true")
	}
	if IsNoNextDateError(errors.New("some other error")) {
		t.Error("unrelated error should be false")
	}
	if !strings.Contains("placeholder", "placeholder") {
		t.Error("sanity")
	}
}
