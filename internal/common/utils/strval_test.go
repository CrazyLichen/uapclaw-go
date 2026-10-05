package utils

import (
	"encoding/json"
	"testing"
)

// TestIntVal 测试 IntVal 将任意值转为 int
func TestIntVal(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int
	}{
		{"nil返回0", nil, 0},
		{"int直接返回", 42, 42},
		{"int8转换", int8(8), 8},
		{"int16转换", int16(16), 16},
		{"int32转换", int32(32), 32},
		{"int64转换", int64(64), 64},
		{"uint转换", uint(10), 10},
		{"float64转换", float64(3.7), 3},
		{"float32转换", float32(2.5), 2},
		{"string数字", "123", 123},
		{"string非数字", "abc", 0},
		{"string空串", "", 0},
		{"json.Number", json.Number("99"), 99},
		{"json.Number非数字", json.Number("xyz"), 0},
		{"不支持的类型", []int{1, 2}, 0},
		{"负数", -7, -7},
		{"float64负数", float64(-3.7), -3},
		{"string负数", "-42", -42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IntVal(tt.input)
			if result != tt.expected {
				t.Errorf("IntVal(%v) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

// TestIntValDefault 测试 IntValDefault 带默认值的 int 提取
func TestIntValDefault(t *testing.T) {
	tests := []struct {
		name       string
		input      any
		defaultVal int
		expected   int
	}{
		{"nil用默认值", nil, 10, 10},
		{"有效int", 42, 10, 42},
		{"string数字", "5", 10, 5},
		{"string非数字用默认值", "abc", 10, 10},
		{"零值int用默认值", 0, 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IntValDefault(tt.input, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("IntValDefault(%v, %d) = %d, want %d", tt.input, tt.defaultVal, result, tt.expected)
			}
		})
	}
}

// TestStrVal 测试 StrVal 将任意值转为字符串
func TestStrVal(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"nil返回空串", nil, ""},
		{"string直接返回", "hello", "hello"},
		{"[]byte转字符串", []byte("world"), "world"},
		{"int用Sprintf", 42, "42"},
		{"float用Sprintf", 3.14, "3.14"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StrVal(tt.input)
			if result != tt.expected {
				t.Errorf("StrVal(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
