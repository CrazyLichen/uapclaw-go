package utils

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPyText_展示文本 验证工具结果展示口径：nil→空串、字符串原样、
// 其余走紧凑 JSON（前端与日志里不再出现 map[...]）。
func TestPyText_展示文本(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil 为空串", nil, ""},
		{"字符串不加引号", "file written", "file written"},
		{"中文原样", "工作空间未初始化", "工作空间未初始化"},
		{"空 map", map[string]any{}, "{}"},
		{"map 转 JSON", map[string]any{"success": false, "error": "boom"}, `{"error":"boom","success":false}`},
		{"bool 转 JSON", true, "true"},
		{"数字转 JSON", 42, "42"},
		{"浮点转 JSON", 1.5, "1.5"},
		{"切片转 JSON", []any{"a", 1}, `["a",1]`},
		{"结构体转 JSON", struct {
			Name string `json:"name"`
		}{Name: "x"}, `{"name":"x"}`},
	}
	for _, tt := range tests {
		if got := PyText(tt.in); got != tt.want {
			t.Errorf("%s: PyText(%v) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

// TestPyText_不可序列化回退 验证 json.Marshal 失败时退回 fmt.Sprint（不 panic）
func TestPyText_不可序列化回退(t *testing.T) {
	ch := make(chan int)
	if got := PyText(ch); got == "" {
		t.Error("不可序列化值也应有兜底文本（fmt.Sprint）")
	}
}

// TestPyText_嵌套结果 验证嵌套 map 输出为可解析 JSON（前端可直接 JSON.parse）
func TestPyText_嵌套结果(t *testing.T) {
	v := map[string]any{
		"results": []any{
			map[string]any{"path": "MEMORY.md", "start_line": 1},
		},
		"disabled": false,
	}
	got := PyText(v)

	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("PyText 输出应是合法 JSON: %v（%s）", err, got)
	}
	assert.Equal(t, false, decoded["disabled"])
}

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

// TestPyStr_容器走兜底 验证 PyStr 只承诺标量：容器按 fmt.Sprint 输出
// （展示场景请用 PyText，输出 JSON）
func TestPyStr_容器走兜底(t *testing.T) {
	got := PyStr(map[string]any{"a": 1})
	assert.Contains(t, got, "a:1", "容器走 fmt.Sprint 兜底")
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
