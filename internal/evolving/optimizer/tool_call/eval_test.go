package tool_call

import (
	"context"
	"math"
	"testing"
)

// newTestSimpleEval 创建测试用 SimpleEval 实例
func newTestSimpleEval() *SimpleEval {
	return NewSimpleEval(nil, map[string]any{}, 0.5, 0.5, nil)
}

// TestEvaluateFunctionCallAccuracy_名称匹配 测试函数名匹配权重
func TestEvaluateFunctionCallAccuracy_名称匹配(t *testing.T) {
	e := newTestSimpleEval()
	generated := map[string]any{"name": "get_weather", "arguments": map[string]any{}}
	expected := map[string]any{"name": "get_weather", "arguments": map[string]any{}}
	score := e.evaluateFunctionCallAccuracy(generated, expected)
	if score != 1.0 {
		t.Errorf("score = %f, want 1.0 for matching name and empty args", score)
	}
}

// TestEvaluateFunctionCallAccuracy_名称不匹配 测试函数名不匹配
func TestEvaluateFunctionCallAccuracy_名称不匹配(t *testing.T) {
	e := newTestSimpleEval()
	generated := map[string]any{"name": "wrong_func", "arguments": map[string]any{}}
	expected := map[string]any{"name": "get_weather", "arguments": map[string]any{}}
	score := e.evaluateFunctionCallAccuracy(generated, expected)
	// 名称不匹配 = 0/0.3，参数为空 = 0.7/0.7 → 总分 0.7/1.0 = 0.7
	if math.Abs(score-0.7) > 1e-6 {
		t.Errorf("score = %f, want 0.7", score)
	}
}

// TestEvaluateFunctionCallAccuracy_参数部分匹配 测试参数部分匹配
func TestEvaluateFunctionCallAccuracy_参数部分匹配(t *testing.T) {
	e := newTestSimpleEval()
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
	score := e.evaluateFunctionCallAccuracy(generated, expected)
	// 名称匹配 0.3，1/2 参数匹配 = 0.7/2 = 0.35
	expectedScore := 0.3 + 0.35
	if math.Abs(score-expectedScore) > 1e-6 {
		t.Errorf("score = %f, want %f", score, expectedScore)
	}
}

// TestCompareParameterValues_相等 测试直接相等
func TestCompareParameterValues_相等(t *testing.T) {
	e := newTestSimpleEval()
	if !e.compareParameterValues("hello", "hello") {
		t.Error("expected true for equal strings")
	}
}

// TestCompareParameterValues_数值容忍 测试数值类型容忍
func TestCompareParameterValues_数值容忍(t *testing.T) {
	e := newTestSimpleEval()
	if !e.compareParameterValues(1, 1.0) {
		t.Error("expected true for int(1) == float64(1.0)")
	}
}

// TestCompareParameterValues_字符串忽略大小写 测试大小写忽略
func TestCompareParameterValues_字符串忽略大小写(t *testing.T) {
	e := newTestSimpleEval()
	if !e.compareParameterValues("Hello", "hello") {
		t.Error("expected true for case-insensitive match")
	}
}

// TestCompareParameterValues_不相等 测试不匹配
func TestCompareParameterValues_不相等(t *testing.T) {
	e := newTestSimpleEval()
	if e.compareParameterValues("abc", "xyz") {
		t.Error("expected false for different strings")
	}
}

// TestSimpleOutputComparison_包含 测试输出包含期望
func TestSimpleOutputComparison_包含(t *testing.T) {
	e := newTestSimpleEval()
	score := e.simpleOutputComparison("The weather is sunny today", "sunny")
	if score != 1.0 {
		t.Errorf("score = %f, want 1.0", score)
	}
}

// TestSimpleOutputComparison_反向包含 测试期望包含输出
func TestSimpleOutputComparison_反向包含(t *testing.T) {
	e := newTestSimpleEval()
	score := e.simpleOutputComparison("sunny", "The weather is sunny today")
	if score != 0.8 {
		t.Errorf("score = %f, want 0.8", score)
	}
}

// TestSimpleOutputComparison_不匹配 测试不匹配
func TestSimpleOutputComparison_不匹配(t *testing.T) {
	e := newTestSimpleEval()
	score := e.simpleOutputComparison("rainy", "sunny")
	if score != 0.3 {
		t.Errorf("score = %f, want 0.3", score)
	}
}

// TestSimpleOutputComparison_空结果 测试空结果
func TestSimpleOutputComparison_空结果(t *testing.T) {
	e := newTestSimpleEval()
	score := e.simpleOutputComparison(nil, "sunny")
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

// TestEvaluateSingleExample_ApiWrapperNil 测试 apiWrapper 为 nil 时返回 error
// 对齐 Python: _evaluate_single_example 中 api_wrapper 为 None 时 raise ValueError
//
// 注意：由于 generateFunctionCall 在 apiWrapper 检查之前执行且需要 model，
// 当 model 也为 nil 时会先返回 "model 为 nil" 错误。
// 当 model 正常但 apiWrapper 为 nil 时会返回 "缺少必需输入: api_wrapper" 错误。
// 两种情况都是 error 返回，对齐 Python 的 raise 行为。
func TestEvaluateSingleExample_ApiWrapperNil(t *testing.T) {
	e := NewSimpleEval(nil, map[string]any{}, 0.4, 0.6, nil)
	tool := map[string]any{"name": "test_tool", "type": "function"}
	example := ExampleTuple{
		Instruction: "test instruction",
		FnCall:      map[string]any{"name": "test_tool", "arguments": map[string]any{}},
		FnOutput:    "test output",
		Answer:      "test answer",
	}
	_, err := e.evaluateSingleExample(context.Background(), tool, "test desc", example, 0)
	if err == nil {
		t.Error("expected error when model and apiWrapper are nil")
	}
	// model 为 nil 时先返回 "model 为 nil" 错误（在 generateFunctionCall 阶段）
	// 这也是 Python 中 _generate_function_call 失败会 raise 的对齐行为
}

// TestEvaluateSingleExample_ReturnsError 测试 evaluateSingleExample 在失败时返回 error
// 对齐 Python: _evaluate_single_example 在失败时 raise ValueError
func TestEvaluateSingleExample_ReturnsError(t *testing.T) {
	// model 为 nil → generateFunctionCall 失败 → 返回 error
	e := NewSimpleEval(nil, map[string]any{}, 0.4, 0.6, nil)
	tool := map[string]any{"name": "test_tool", "type": "function"}
	example := ExampleTuple{
		Instruction: "test instruction",
		FnCall:      map[string]any{"name": "test_tool", "arguments": map[string]any{}},
		FnOutput:    "test output",
		Answer:      "test answer",
	}
	_, err := e.evaluateSingleExample(context.Background(), tool, "test desc", example, 0)
	if err == nil {
		t.Error("expected error when evaluateSingleExample fails")
	}
}

// ──────────────────────────── 非导出函数 测试 ────────────────────────────

// TestGetFnName 从 fn_call 中获取函数名
func TestGetFnName(t *testing.T) {
	t.Run("正常获取", func(t *testing.T) {
		result := getFnName(map[string]any{"name": "grep"})
		if result != "grep" {
			t.Errorf("getFnName = %q, 期望 %q", result, "grep")
		}
	})
	t.Run("nil返回unknown", func(t *testing.T) {
		if getFnName(nil) != "unknown" {
			t.Error("getFnName(nil) 应返回 unknown")
		}
	})
	t.Run("name非字符串返回unknown", func(t *testing.T) {
		if getFnName(map[string]any{"name": 42}) != "unknown" {
			t.Error("getFnName(非字符串 name) 应返回 unknown")
		}
	})
}

// TestGetToolName 从 tool 字典中获取工具名
func TestGetToolName(t *testing.T) {
	t.Run("正常获取", func(t *testing.T) {
		result := getToolName(map[string]any{"name": "read_file"})
		if result != "read_file" {
			t.Errorf("getToolName = %q, 期望 %q", result, "read_file")
		}
	})
	t.Run("无name返回unknown", func(t *testing.T) {
		if getToolName(map[string]any{}) != "unknown" {
			t.Error("getToolName(无 name) 应返回 unknown")
		}
	})
}

// TestGetToolDescription 从 tool 字典中获取描述
func TestGetToolDescription(t *testing.T) {
	t.Run("正常获取", func(t *testing.T) {
		result := getToolDescription(map[string]any{"description": "搜索文件"})
		if result != "搜索文件" {
			t.Errorf("getToolDescription = %q, 期望 %q", result, "搜索文件")
		}
	})
	t.Run("无description返回空字符串", func(t *testing.T) {
		if getToolDescription(map[string]any{}) != "" {
			t.Error("getToolDescription(无 description) 应返回空字符串")
		}
	})
}

// TestGetToolParameters 从 tool 字典中获取参数
func TestGetToolParameters(t *testing.T) {
	t.Run("正常获取", func(t *testing.T) {
		params := map[string]any{"type": "object"}
		result := getToolParameters(map[string]any{"parameters": params})
		if result["type"] != "object" {
			t.Errorf("getToolParameters 返回值不正确")
		}
	})
	t.Run("无parameters返回空map", func(t *testing.T) {
		result := getToolParameters(map[string]any{})
		if len(result) != 0 {
			t.Error("getToolParameters(无 parameters) 应返回空 map")
		}
	})
}

// TestIsNumeric 判断是否为数值类型
func TestIsNumeric(t *testing.T) {
	if !isNumeric(42) || !isNumeric(3.14) || !isNumeric(int64(1)) {
		t.Error("数值类型应返回 true")
	}
	if isNumeric("string") || isNumeric(nil) {
		t.Error("非数值类型应返回 false")
	}
}

// TestToFloat 将数值转为 float64 指针
func TestToFloat(t *testing.T) {
	t.Run("int转换", func(t *testing.T) {
		f := toFloat(42)
		if f == nil || *f != 42.0 {
			t.Error("toFloat(int) 失败")
		}
	})
	t.Run("float64直接返回", func(t *testing.T) {
		f := toFloat(3.14)
		if f == nil || *f != 3.14 {
			t.Error("toFloat(float64) 失败")
		}
	})
	t.Run("非数值返回nil", func(t *testing.T) {
		if toFloat("string") != nil {
			t.Error("toFloat(string) 应返回 nil")
		}
	})
}
