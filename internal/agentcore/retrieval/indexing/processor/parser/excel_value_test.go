package parser

import (
	"math"
	"testing"
	"time"
)

// TestPyFloatStr 测试 Python str(float) 记法还原。
//
// 实测锚点（spec §3.4）：1e15→"1000000000000000.0"、0.1+0.2 全精度、
// 1e-05 两位指数、1e+16 出 e 记法。
func TestPyFloatStr(t *testing.T) {
	// 运行时求值（避免 Go 常量折叠把 0.1+0.2 算成精确 0.3）
	a, b := 0.1, 0.2
	cases := []struct {
		in   float64
		want string
	}{
		{3.5, "3.5"},
		{a + b, "0.30000000000000004"},
		{1e15, "1000000000000000.0"},
		{1e16, "1e+16"},
		{1e-4, "0.0001"},
		{1e-5, "1e-05"},
		{2.0, "2.0"},
		{-3.5, "-3.5"},
		{1e15 + 0.5, "1000000000000000.5"},
		{9999999999999998.0, "9999999999999998.0"},
		{0, "0.0"},
		{math.Copysign(0, -1), "-0.0"}, // Go -0.0 常量折叠成正零，用 Copysign 构造真负零
	}
	for _, c := range cases {
		if got := pyFloatStr(c.in); got != c.want {
			t.Errorf("pyFloatStr(%v) = %q, want %q", c.in, got, c.want)
		}
	}
	// NaN/Inf（Python 输出 nan/inf/-inf）
	if got := pyFloatStr(math.NaN()); got != "nan" {
		t.Errorf("pyFloatStr(NaN) = %q, want nan", got)
	}
	if got := pyFloatStr(math.Inf(1)); got != "inf" {
		t.Errorf("pyFloatStr(+Inf) = %q, want inf", got)
	}
	if got := pyFloatStr(math.Inf(-1)); got != "-inf" {
		t.Errorf("pyFloatStr(-Inf) = %q, want -inf", got)
	}
}

// TestCellStr 测试 cellStr 逐变体模拟（Python str(value).strip()）。
func TestCellStr(t *testing.T) {
	cases := []struct {
		in   cellValue
		want string
	}{
		{nil, ""},
		{noneCell, ""},
		{cellString("  x  "), "x"},
		{cellInt(30), "30"},
		{cellFloat(3.5), "3.5"},
		{cellFloat(2.0), "2.0"},
		{cellBool(true), "True"},
		{cellBool(false), "False"},
		{cellString(""), ""},
		{cellString("  "), ""},
	}
	for _, c := range cases {
		if got := cellStr(c.in); got != c.want {
			t.Errorf("cellStr(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestCellStr_日期 测试 cellTime 的 Python str(datetime) 格式。
func TestCellStr_日期(t *testing.T) {
	// 微秒 0：无小数部分（对齐 str(datetime) 省略微秒）
	d := cellTime(time.Date(2025, 1, 1, 12, 30, 5, 0, time.UTC))
	if got := cellStr(d); got != "2025-01-01 12:30:05" {
		t.Errorf("cellStr(日期) = %q, want %q", got, "2025-01-01 12:30:05")
	}
	// 微秒非 0：补 .%06d（对齐 str(datetime) 的 6 位微秒）
	d2 := cellTime(time.Date(2025, 1, 1, 12, 30, 5, 123456000, time.UTC))
	if got := cellStr(d2); got != "2025-01-01 12:30:05.123456" {
		t.Errorf("cellStr(带微秒日期) = %q, want %q", got, "2025-01-01 12:30:05.123456")
	}
}

// TestDateTokensInCode 测试 numFmt code 的日期 token 扫描（对齐 openpyxl is_date_format）。
func TestDateTokensInCode(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{"yyyy-mm-dd", true},                   // 日期 token
		{"hh:mm:ss", true},                     // 时间 token
		{"m/d/yy hh:mm", true},                 // 混合
		{"0.00", false},                        // 纯数字
		{"#,##0", false},                       // 千分位
		{"General", false},                     // 常规
		{"yyyy\"年\"m\"月\"", true},              // 引号内中文不含 token，token 在引号外
		{"\"Day\" 0", false},                   // 引号内 Day 但在引号里
		{"_(* #,##0_);_(* \\(#,##0\\)", false}, // 会计格式（_ 转义）
		{"0.00;[red]0.00", false},              // 分段格式只看第一段
		{"yyyy-mm-dd;0.00", true},              // 第一段含 token
		{"0.00;yyyy-mm-dd", false},             // token 只在第二段（第一段无）
		{"\\d\\m", false},                      // 反斜杠转义
	}
	for _, c := range cases {
		if got := dateTokensInCode(c.code); got != c.want {
			t.Errorf("dateTokensInCode(%q) = %v, want %v", c.code, got, c.want)
		}
	}
}
