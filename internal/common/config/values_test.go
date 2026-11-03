package config

import (
	"encoding/json"
	"testing"
)

// TestFloatValue_形态覆盖 锁定数值读取的兼容面：
// YAML 整数字面量解出 int、JSON 恒 float64、${ENV:-default} 替换后是字符串。
func TestFloatValue_形态覆盖(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want float64
	}{
		{"YAML整数", 1800, 1800},
		{"JSON浮点", 1800.5, 1800.5},
		{"int32", int32(30), 30},
		{"int64", int64(3600), 3600},
		{"uint", uint(7), 7},
		{"float32", float32(0.5), 0.5},
		{"json.Number", json.Number("2048"), 2048},
		{"env替换后的数字字符串", "1800", 1800},
		{"带空白的数字字符串", " 900 ", 900},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := FloatValue(c.in)
			if !ok {
				t.Fatalf("FloatValue() 未识别 %T", c.in)
			}
			if got != c.want {
				t.Errorf("FloatValue() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestFloatValue_非数值 验证非数值形态返回 false，由调用方取默认值。
func TestFloatValue_非数值(t *testing.T) {
	for _, in := range []any{nil, "abc", true, []any{1}, map[string]any{}} {
		if _, ok := FloatValue(in); ok {
			t.Errorf("FloatValue(%T) 应返回 false", in)
		}
	}
}

// TestFloat_取键 验证按 key 读取、缺键与 nil map 都返回 false。
func TestFloat_取键(t *testing.T) {
	if got, ok := Float(map[string]any{"timeout": 1800}, "timeout"); !ok || got != 1800 {
		t.Errorf("Float() = (%v,%v), want (1800,true)", got, ok)
	}
	if _, ok := Float(map[string]any{}, "timeout"); ok {
		t.Error("缺键应返回 false")
	}
	if _, ok := Float(nil, "timeout"); ok {
		t.Error("nil map 应返回 false")
	}
}

// TestIntValue_形态覆盖 验证整数字段同样多形态兼容（小数截断）。
func TestIntValue_形态覆盖(t *testing.T) {
	cases := []any{3, 3.0, int32(3), int64(3), json.Number("3"), "3"}
	for _, in := range cases {
		got, ok := IntValue(in)
		if !ok || got != 3 {
			t.Errorf("IntValue(%T) = (%v,%v), want (3,true)", in, got, ok)
		}
	}
	if got, ok := IntValue(3.9); !ok || got != 3 {
		t.Errorf("IntValue(3.9) 应截断为 3, 实际 (%v,%v)", got, ok)
	}
}

// TestInt_取键 验证按 key 读取整数、nil map 返回 false。
func TestInt_取键(t *testing.T) {
	if got, ok := Int(map[string]any{"max_retries": "3"}, "max_retries"); !ok || got != 3 {
		t.Errorf("Int() = (%v,%v), want (3,true)", got, ok)
	}
	if _, ok := Int(nil, "max_retries"); ok {
		t.Error("nil map 应返回 false")
	}
}
