package tool_call

import (
	"math"
	"testing"
)

// TestEvaluateFunctionCallAccuracy_名称匹配 测试函数名匹配权重
func TestEvaluateFunctionCallAccuracy_名称匹配(t *testing.T) {
	generated := map[string]any{"name": "get_weather", "arguments": map[string]any{}}
	expected := map[string]any{"name": "get_weather", "arguments": map[string]any{}}
	score := EvaluateFunctionCallAccuracy(generated, expected)
	if score != 1.0 {
		t.Errorf("score = %f, want 1.0 for matching name and empty args", score)
	}
}

// TestEvaluateFunctionCallAccuracy_名称不匹配 测试函数名不匹配
func TestEvaluateFunctionCallAccuracy_名称不匹配(t *testing.T) {
	generated := map[string]any{"name": "wrong_func", "arguments": map[string]any{}}
	expected := map[string]any{"name": "get_weather", "arguments": map[string]any{}}
	score := EvaluateFunctionCallAccuracy(generated, expected)
	// 名称不匹配 = 0/0.3，参数为空 = 0.7/0.7 → 总分 0.7/1.0 = 0.7
	if math.Abs(score-0.7) > 1e-6 {
		t.Errorf("score = %f, want 0.7", score)
	}
}

// TestEvaluateFunctionCallAccuracy_参数部分匹配 测试参数部分匹配
func TestEvaluateFunctionCallAccuracy_参数部分匹配(t *testing.T) {
	generated := map[string]any{
		"name": "get_weather",
		"arguments": map[string]any{
			"city": "Beijing",
			"unit": "celsius",
		},
	}
	expected := map[string]any{
		"name": "get_weather",
		"arguments": map[string]any{
			"city": "Beijing",
			"unit": "fahrenheit",
		},
	}
	score := EvaluateFunctionCallAccuracy(generated, expected)
	// 名称匹配 0.3，1/2 参数匹配 = 0.7/2 = 0.35
	expectedScore := 0.3 + 0.35
	if math.Abs(score-expectedScore) > 1e-6 {
		t.Errorf("score = %f, want %f", score, expectedScore)
	}
}

// TestCompareParameterValues_相等 测试直接相等
func TestCompareParameterValues_相等(t *testing.T) {
	if !CompareParameterValues("hello", "hello") {
		t.Error("expected true for equal strings")
	}
}

// TestCompareParameterValues_数值容忍 测试数值类型容忍
func TestCompareParameterValues_数值容忍(t *testing.T) {
	if !CompareParameterValues(1, 1.0) {
		t.Error("expected true for int(1) == float64(1.0)")
	}
}

// TestCompareParameterValues_字符串忽略大小写 测试大小写忽略
func TestCompareParameterValues_字符串忽略大小写(t *testing.T) {
	if !CompareParameterValues("Hello", "hello") {
		t.Error("expected true for case-insensitive match")
	}
}

// TestCompareParameterValues_不相等 测试不匹配
func TestCompareParameterValues_不相等(t *testing.T) {
	if CompareParameterValues("abc", "xyz") {
		t.Error("expected false for different strings")
	}
}

// TestSimpleOutputComparison_包含 测试输出包含期望
func TestSimpleOutputComparison_包含(t *testing.T) {
	score := SimpleOutputComparison("The weather is sunny today", "sunny")
	if score != 1.0 {
		t.Errorf("score = %f, want 1.0", score)
	}
}

// TestSimpleOutputComparison_反向包含 测试期望包含输出
func TestSimpleOutputComparison_反向包含(t *testing.T) {
	score := SimpleOutputComparison("sunny", "The weather is sunny today")
	if score != 0.8 {
		t.Errorf("score = %f, want 0.8", score)
	}
}

// TestSimpleOutputComparison_不匹配 测试不匹配
func TestSimpleOutputComparison_不匹配(t *testing.T) {
	score := SimpleOutputComparison("rainy", "sunny")
	if score != 0.3 {
		t.Errorf("score = %f, want 0.3", score)
	}
}

// TestSimpleOutputComparison_空结果 测试空结果
func TestSimpleOutputComparison_空结果(t *testing.T) {
	score := SimpleOutputComparison(nil, "sunny")
	if score != 0.0 {
		t.Errorf("score = %f, want 0.0", score)
	}
}

// TestMean 测试均值计算
func TestMean(t *testing.T) {
	values := []float64{1.0, 2.0, 3.0}
	if got := mean(values); got != 2.0 {
		t.Errorf("mean = %f, want 2.0", got)
	}
	if got := mean([]float64{}); got != 0 {
		t.Errorf("mean(empty) = %f, want 0", got)
	}
}

// TestStd 测试标准差计算
func TestStd(t *testing.T) {
	values := []float64{2.0, 2.0, 2.0}
	if got := std(values); got != 0 {
		t.Errorf("std = %f, want 0", got)
	}
	if got := std([]float64{}); got != 0 {
		t.Errorf("std(empty) = %f, want 0", got)
	}
	if got := std([]float64{1.0}); got != 0 {
		t.Errorf("std(single) = %f, want 0", got)
	}
}

// TestNewSimpleEval_权重校验 测试权重校验
func TestNewSimpleEval_权重校验(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid weights")
		}
	}()
	NewSimpleEval(nil, map[string]any{}, 0.5, 0.6, nil)
}

// TestGetArgsMap 测试参数提取
func TestGetArgsMap(t *testing.T) {
	fnCall := map[string]any{
		"name":      "test",
		"arguments": map[string]any{"key": "value"},
	}
	args := getArgsMap(fnCall)
	if args["key"] != "value" {
		t.Errorf("expected value, got %v", args["key"])
	}
}

// TestGetArgsMap_字符串参数 测试字符串类型的 arguments
func TestGetArgsMap_字符串参数(t *testing.T) {
	fnCall := map[string]any{
		"name":      "test",
		"arguments": `{"key": "value"}`,
	}
	args := getArgsMap(fnCall)
	if args["key"] != "value" {
		t.Errorf("expected value, got %v", args["key"])
	}
}

// TestGetArgsMap_无参数 测试无 arguments 字段
func TestGetArgsMap_无参数(t *testing.T) {
	fnCall := map[string]any{"name": "test"}
	args := getArgsMap(fnCall)
	if len(args) != 0 {
		t.Errorf("expected empty map, got %v", args)
	}
}
