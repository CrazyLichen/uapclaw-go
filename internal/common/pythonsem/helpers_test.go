package pythonsem

import "testing"

// TestIsBool_IsInt_PyInt 验证 bool 先于 int 的 Python 类型判定语义
func TestIsBool_IsInt_PyInt(t *testing.T) {
	if !IsBool(true) || IsBool(1) {
		t.Fatal("IsBool 语义错误")
	}
	if !IsInt(1) || !IsInt(int64(2)) || IsInt(true) || IsInt(1.0) {
		t.Fatal("IsInt 语义错误（Python int 不含 bool/float）")
	}
	if PyInt(3) != 3 || PyInt(int64(4)) != 4 || PyInt(1.5) != 0 {
		t.Fatal("PyInt 语义错误")
	}
}

// TestPyIntConvert 验证 Python int(x) 宽转换：bool→0/1、float 截断、字符串解析、失败 false
func TestPyIntConvert(t *testing.T) {
	cases := []struct {
		value any
		want  int64
		ok    bool
	}{
		{true, 1, true}, {false, 0, true},
		{7, 7, true}, {int64(8), 8, true},
		{200.0, 200, true}, {200.9, 200, true}, // int(float) 截断
		{"200", 200, true}, {" 200 ", 200, true}, {"abc", 0, false},
		{nil, 0, false}, {map[string]any{}, 0, false},
	}
	for _, c := range cases {
		got, ok := PyIntConvert(c.value)
		if ok != c.ok || (ok && got != c.want) {
			t.Fatalf("PyIntConvert(%v) = (%d,%v), want (%d,%v)", c.value, got, ok, c.want, c.ok)
		}
	}
}

// TestIsNumber 验证 _is_number：排除 bool、含 int/int64/有限 float
func TestIsNumber(t *testing.T) {
	cases := []struct {
		value any
		want  bool
	}{
		{1, true}, {int64(1), true}, {1.5, true},
		{true, false}, {"1", false}, {nil, false},
	}
	for _, c := range cases {
		if got := IsNumber(c.value); got != c.want {
			t.Fatalf("IsNumber(%v)=%v want %v", c.value, got, c.want)
		}
	}
}

// TestPyEqual 验证跨类型数值相等（1 == 1.0）与布尔不参与数值比较
func TestPyEqual(t *testing.T) {
	if !PyEqual(1, 1.0) || !PyEqual(int64(2), 2.0) {
		t.Fatal("PyEqual 数值跨类型相等失败")
	}
	if PyEqual(true, 1) {
		t.Fatal("PyEqual 布尔不得参与数值比较（Python True==1 为 True，但本包语义按调用方守卫排除；此处验证 isNumber(false)=false 分支）")
	}
	if !PyEqual("a", "a") || PyEqual("a", "b") {
		t.Fatal("PyEqual 字符串比较失败")
	}
}

// TestPyRepr 验证 repr 展示格式（None/True/False/单引号/float/.0/列表/字典）
func TestPyRepr(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{nil, "None"},
		{true, "True"},
		{false, "False"},
		{"abc", "'abc'"},
		{2.0, "2.0"},
		{0.1, "0.1"},
		{1e-12, "1e-12"},
		{3, "3"},
		{[]any{"a", 1}, "['a', 1]"},
		{[]string{"a", "b"}, "['a', 'b']"},
		{map[string]any{"b": 1, "a": true}, "{'a': True, 'b': 1}"},
	}
	for _, c := range cases {
		if got := PyRepr(c.value); got != c.want {
			t.Fatalf("PyRepr(%v)=%q want %q", c.value, got, c.want)
		}
	}
}

// TestPyFloatRepr 验证整值 .0 后缀与最短 round-trip（%f 截断实现会把 1e-12 写成 0.0，此处回归锁定）
func TestPyFloatRepr(t *testing.T) {
	cases := []struct {
		f    float64
		want string
	}{
		{2.0, "2.0"}, {30.0, "30.0"}, {12.5, "12.5"}, {0.3333333333333333, "0.3333333333333333"},
		{1e-12, "1e-12"}, {0.1, "0.1"},
	}
	for _, c := range cases {
		if got := PyFloatRepr(c.f); got != c.want {
			t.Fatalf("PyFloatRepr(%v)=%q want %q", c.f, got, c.want)
		}
	}
}

// TestPyDeepEqual 验证数值跨类型/嵌套 map/切片的递归相等
func TestPyDeepEqual(t *testing.T) {
	if !PyDeepEqual(map[string]any{"a": 1}, map[string]any{"a": 1.0}) {
		t.Fatal("嵌套数值跨类型相等失败")
	}
	if !PyDeepEqual([]any{1, "x"}, []any{1.0, "x"}) {
		t.Fatal("切片递归相等失败")
	}
	if PyDeepEqual(map[string]any{"a": 1}, map[string]any{"a": 2}) {
		t.Fatal("不等值误判相等")
	}
	if !PyDeepEqual(nil, nil) || PyDeepEqual(nil, 0) {
		t.Fatal("nil 语义失败")
	}
}

// TestMapValue 验证 dict.get 语义
func TestMapValue(t *testing.T) {
	m := map[string]any{"a": 1}
	if MapValue(m, "a") != 1 || MapValue(m, "missing") != nil {
		t.Fatal("MapValue 语义错误")
	}
}

// TestPyStr_str语义 str() 与 repr() 分野：str 原样、bool/None/数值、容器回退 repr
func TestPyStr_str语义(t *testing.T) {
	if PyStr("s") != "s" {
		t.Fatal("str 应原样不加引号")
	}
	if PyStr(true) != "True" || PyStr(nil) != "None" || PyStr(8080) != "8080" {
		t.Fatal("标量 str 漂移")
	}
	if PyStr(2.0) != "2.0" {
		t.Fatal("float str 应走 PyFloatRepr")
	}
	if PyStr([]any{"a"}) != "['a']" {
		t.Fatal("容器 str 应回退 repr")
	}
}

// TestValidateAndConvertType 复刻 Python questioner_comp.py validate_and_convert_type
// :309-377 的四类转换语义（bool≠int、整值 float 才算 int、str 解析、未知类型回落 str）
func TestValidateAndConvertType(t *testing.T) {
	cases := []struct {
		val  any
		typ  string
		want any
		ok   bool
	}{
		{nil, "string", nil, false},    // Python :322-323
		{"abc", "string", "abc", true}, // str(value)
		{123, "string", "123", true},
		{true, "integer", nil, false}, // bool≠int :332-334
		{int64(5), "integer", int64(5), true},
		{5.0, "integer", int64(5), true}, // 整值 float :337-340
		{5.5, "integer", nil, false},
		{" 7 ", "integer", int64(7), true}, // str 解析 :342-345
		{"x", "integer", nil, false},
		{true, "number", nil, false},    // :350-351
		{int64(3), "number", 3.0, true}, // :352-353
		{2.5, "number", 2.5, true},
		{"3.5", "number", 3.5, true},
		{true, "boolean", true, true},     // :361-362
		{" True ", "boolean", true, true}, // strip+lower :363-368
		{"yes", "boolean", nil, false},
		{int64(9), "unknown", "9", true}, // 未知类型回落 str :372-374
	}
	for _, c := range cases {
		got, ok := ValidateAndConvertType(c.val, c.typ)
		if ok != c.ok || (ok && !PyDeepEqual(got, c.want)) {
			t.Errorf("ValidateAndConvertType(%v,%q) = (%v,%v), want (%v,%v)", c.val, c.typ, got, ok, c.want, c.ok)
		}
	}
}
