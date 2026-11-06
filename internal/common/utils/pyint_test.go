package utils

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPyInt_对齐PythonInt 校验 int() 的全类型语义
func TestPyInt_对齐PythonInt(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  int
	}{
		{name: "int", input: 7, want: 7},
		{name: "int8", input: int8(7), want: 7},
		{name: "int64", input: int64(7), want: 7},
		{name: "uint32", input: uint32(7), want: 7},
		{name: "float64整数", input: float64(7), want: 7},
		{name: "float64截断", input: 3.9, want: 3},
		{name: "float32截断", input: float32(3.9), want: 3},
		{name: "负浮点向零截断", input: -3.9, want: -3},
		{name: "字符串", input: "7", want: 7},
		{name: "字符串带空白", input: " 7 ", want: 7},
		{name: "字符串负数", input: "-7", want: -7},
		// Python: int(True) == 1, int(False) == 0（bool 是 int 子类）
		{name: "true", input: true, want: 1},
		{name: "false", input: false, want: 0},
		{name: "json整数", input: json.Number("42"), want: 42},
		// Python: json.loads 把 3.0 解析为 float → int(3.0) == 3
		{name: "json小数", input: json.Number("3.0"), want: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PyInt(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestPyInt_拒绝非整数输入 校验错误分支
func TestPyInt_拒绝非整数输入(t *testing.T) {
	cases := []struct {
		name  string
		input any
	}{
		// Python: int("3.0") 抛 ValueError
		{name: "小数字符串", input: "3.0"},
		{name: "空字符串", input: ""},
		{name: "非数字字符串", input: "abc"},
		{name: "nil", input: nil},
		{name: "map", input: map[string]any{}},
		// 超界整数：Go 装不下，不退化成 float
		{name: "超界json整数", input: json.Number("12345678901234567890")},
		{name: "超界uint64", input: uint64(maxIntValue) + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PyInt(tc.input)
			assert.Error(t, err)
		})
	}
}
