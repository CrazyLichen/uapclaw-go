package utils

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPyStr_标量语义 验证 str() 语义：字符串不加引号、bool 大写、整数值浮点补 .0。
// 对齐 Python: str("a")="a"、str(True)="True"、str(1.0)="1.0"、str(None)="None"
func TestPyStr_标量语义(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"字符串不加引号", "abc", "abc"},
		{"中文原样", "工作空间未初始化", "工作空间未初始化"},
		{"true", true, "True"},
		{"false", false, "False"},
		{"int", 42, "42"},
		{"int64", int64(-7), "-7"},
		{"float 整数值补 .0", 1.0, "1.0"},
		{"float 小数", 0.5, "0.5"},
		{"float32", float32(2.5), "2.5"},
		{"uint", uint(9), "9"},
		{"nil 为 None", nil, "None"},
	}
	for _, tt := range tests {
		if got := PyStr(tt.in); got != tt.want {
			t.Errorf("%s: PyStr(%v) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

// TestPyStr_容器等同repr 验证 str(container)==repr(container)：
// map 渲染为 {'k': v}（不是 Go 的 map[k:v]），list 为 [a, b]。
func TestPyStr_容器等同repr(t *testing.T) {
	m := map[string]any{
		"success": false,
		"error":   "工作空间未初始化",
		"count":   3,
		"ok":      true,
		"items":   []any{"a", 1},
	}
	got := PyStr(m)
	want := `{'count': 3, 'error': '工作空间未初始化', 'items': ['a', 1], 'ok': True, 'success': False}`
	assert.Equal(t, want, got)
	assert.NotContains(t, got, "map[", "不得出现 Go 的 map[...] 记法")
}

// TestPyStr_空容器 验证空结果渲染为 {} 与 []（对应记忆检索无命中的形状）
func TestPyStr_空容器(t *testing.T) {
	assert.Equal(t, "{}", PyStr(map[string]any{}))
	assert.Equal(t, "[]", PyStr([]any{}))
	assert.Equal(t, `{'results': []}`, PyStr(map[string]any{"results": []any{}}))
}

// TestPyRepr_字符串引号规则 验证 repr 的引号选择与转义。
// 对齐 Python: repr("a")="'a'"、含单引号且无双引号时用双引号包裹。
func TestPyRepr_字符串引号规则(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"普通", "ab", `'ab'`},
		{"含单引号改用双引号", "a'b", `"a'b"`},
		{"含双引号保持单引号", `a"b`, `'a"b'`},
		{"换行转义", "a\nb", `'a\nb'`},
		{"回车转义", "a\rb", `'a\rb'`},
		{"制表转义", "a\tb", `'a\tb'`},
		{"反斜杠转义", `a\b`, `'a\\b'`},
	}
	for _, tt := range tests {
		if got := PyRepr(tt.in); got != tt.want {
			t.Errorf("%s: PyRepr(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

// TestPyRepr_嵌套容器 验证递归 repr 与 None 渲染
func TestPyRepr_嵌套容器(t *testing.T) {
	v := map[string]any{
		"outer": map[string]any{"inner": []any{1, "x", nil}},
	}
	assert.Equal(t, `{'outer': {'inner': [1, 'x', None]}}`, PyRepr(v))
}

// TestPyStr_未知类型回退 验证非基础类型走 %v 兜底（不 panic）
func TestPyStr_未知类型回退(t *testing.T) {
	type custom struct{ Name string }
	assert.Equal(t, "{PyStrTest}", PyStr(custom{Name: "PyStrTest"}))
}

// TestPyStr_全部整数族 覆盖各宽度整型（Python 只有一种 int，str 输出十进制）
func TestPyStr_全部整数族(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{int8(-1), "-1"},
		{int16(-2), "-2"},
		{int32(-3), "-3"},
		{uint8(1), "1"},
		{uint16(2), "2"},
		{uint32(3), "3"},
		{uint64(4), "4"},
	}
	for _, c := range cases {
		if got := PyStr(c.in); got != c.want {
			t.Errorf("PyStr(%T %v) = %q, want %q", c.in, c.in, got, c.want)
		}
	}
}

// TestPyStr_浮点记法 覆盖 Python str(float) 的 f/e 记法切换与 nan/inf 字面量。
// 对齐 Python: str(1e17)="1e+17"、str(1e-5)="1e-05"、str(float("inf"))="inf"
func TestPyStr_浮点记法(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1e17, "1e+17"},
		{1e-5, "1e-05"},
		{math.NaN(), "nan"},
		{math.Inf(1), "inf"},
		{math.Inf(-1), "-inf"},
		{0, "0.0"},
	}
	for _, c := range cases {
		if got := PyStr(c.in); got != c.want {
			t.Errorf("PyStr(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestPyRepr_同时含两种引号 验证同时含单双引号时用单引号并转义单引号
func TestPyRepr_同时含两种引号(t *testing.T) {
	assert.Equal(t, `'a"b\'c'`, PyRepr(`a"b'c`))
}

// TestPyRepr_容器内非基础类型兜底 验证容器里出现结构体等非基础类型时
// 走 %v 兜底（不 panic、不带 map[ 记法）
func TestPyRepr_容器内非基础类型兜底(t *testing.T) {
	type probe struct{ Name string }
	got := PyStr(map[string]any{"probe": probe{Name: "x"}})
	assert.Equal(t, "{'probe': {x}}", got)
}

// TestPyStr_jsonNumber 验证 json.Number 的字面量还原（对齐 Python int/float 分野）：
// 整数字面原样（含超界），小数/指数走 float 记法。
func TestPyStr_jsonNumber(t *testing.T) {
	cases := []struct {
		in   json.Number
		want string
	}{
		{"42", "42"},
		{"-7", "-7"},
		{"12345678901234567890", "12345678901234567890"}, // 超 int64，Python int 无界
		{"1.5", "1.5"},
		{"1.0", "1.0"},
		{"1.5e300", "1.5e+300"},
	}
	for _, c := range cases {
		if got := PyStr(c.in); got != c.want {
			t.Errorf("PyStr(json.Number %q) = %q, want %q", c.in, got, c.want)
		}
	}

	// 容器内的 json.Number 同样走字面量还原
	got := PyStr(map[string]any{"n": json.Number("1.0"), "big": json.Number("12345678901234567890")})
	assert.Equal(t, "{'big': 12345678901234567890, 'n': 1.0}", got)
}

// TestIsPureIntegerLiteral 验证纯整数字面量判定（超界整数还原的守卫）
func TestIsPureIntegerLiteral(t *testing.T) {
	yes := []string{"0", "42", "-7", "+7", "12345678901234567890"}
	no := []string{"", "-", "+", "1.5", "1e5", "1.", ".5", "0x10", "abc", "1 "}
	for _, s := range yes {
		if !IsPureIntegerLiteral(s) {
			t.Errorf("IsPureIntegerLiteral(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if IsPureIntegerLiteral(s) {
			t.Errorf("IsPureIntegerLiteral(%q) = true, want false", s)
		}
	}
}

// TestPyFloatStr 验证 str(float) 记法（与 retrieval 侧 excel_value / 抽取器同源）
func TestPyFloatStr(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{3.5, "3.5"},
		{2, "2.0"},
		{math.Copysign(0, -1), "-0.0"},
		{1e15, "1000000000000000.0"},
		{1e16, "1e+16"},
		{1e-5, "1e-05"},
		{math.NaN(), "nan"},
		{math.Inf(1), "inf"},
		{math.Inf(-1), "-inf"},
	}
	for _, c := range cases {
		if got := PyFloatStr(c.in); got != c.want {
			t.Errorf("PyFloatStr(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
