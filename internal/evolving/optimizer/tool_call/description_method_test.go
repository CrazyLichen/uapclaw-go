package tool_call

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestToolDescriptionMethod_GetOriginalDescription 测试 GetOriginalDescription 各种输入格式
func TestToolDescriptionMethod_GetOriginalDescription(t *testing.T) {
	m := &ToolDescriptionMethod{}

	tests := []struct {
		name     string
		tool     map[string]any
		expected string
	}{
		{
			name: "普通描述",
			tool: map[string]any{
				"description": "This is a simple tool description",
			},
			expected: "This is a simple tool description",
		},
		{
			name: "包含前缀的描述",
			tool: map[string]any{
				"description": "Some prefix. The description of this function is: \"actual description here\"",
			},
			expected: "actual description here",
		},
		{
			name: "空字符串描述",
			tool: map[string]any{
				"description": "",
			},
			expected: "",
		},
		{
			name: "无 description 字段",
			tool: map[string]any{
				"name": "test_tool",
			},
			expected: "",
		},
		{
			name: "description 为非字符串类型",
			tool: map[string]any{
				"description": 42,
			},
			expected: "42",
		},
		{
			name: "仅前缀无内容",
			tool: map[string]any{
				"description": "The description of this function is: \"\"",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.GetOriginalDescription(tt.tool)
			if result != tt.expected {
				t.Errorf("GetOriginalDescription() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestToolDescriptionMethod_LoadExamples 测试 LoadExamples 的文件 IO
func TestToolDescriptionMethod_LoadExamples(t *testing.T) {
	tmpDir := t.TempDir()
	functionName := "test_function"

	// 构造测试数据
	allOutputs := []any{
		[]any{
			map[string]any{
				"fn_call":      map[string]any{"name": "test_function", "arguments": map[string]any{"param1": "value1"}},
				"tool_results": "tool result output",
				"instructions": []any{"test instruction"},
				"answers":      []any{"test answer"},
				"scores":       []any{4.0},
			},
		},
	}

	data, err := json.Marshal(allOutputs)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	examplesPath := filepath.Join(tmpDir, functionName+".json")
	if err := os.WriteFile(examplesPath, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	m := &ToolDescriptionMethod{
		BaseMethod: BaseMethod{config: map[string]any{}},
	}

	examples, err := m.LoadExamples(tmpDir, functionName, 3)
	if err != nil {
		t.Fatalf("LoadExamples failed: %v", err)
	}

	if len(examples) != 1 {
		t.Fatalf("expected 1 example, got %d", len(examples))
	}

	if examples[0].Instruction != "test instruction" {
		t.Errorf("expected instruction 'test instruction', got %q", examples[0].Instruction)
	}
	if examples[0].Answer != "test answer" {
		t.Errorf("expected answer 'test answer', got %q", examples[0].Answer)
	}
	if examples[0].FnOutput != "tool result output" {
		t.Errorf("expected fn_output 'tool result output', got %q", examples[0].FnOutput)
	}
}

// TestToolDescriptionMethod_LoadExamples_文件不存在 测试文件不存在时返回错误
func TestToolDescriptionMethod_LoadExamples_文件不存在(t *testing.T) {
	m := &ToolDescriptionMethod{
		BaseMethod: BaseMethod{config: map[string]any{}},
	}

	_, err := m.LoadExamples("/nonexistent/path", "nonexistent_function", 3)
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

// TestToolDescriptionMethod_LoadExamples_分数过滤 测试分数过滤逻辑
func TestToolDescriptionMethod_LoadExamples_分数过滤(t *testing.T) {
	tmpDir := t.TempDir()
	functionName := "score_filter_function"

	// 构造测试数据：高分、低分和无分数的条目
	allOutputs := []any{
		[]any{
			map[string]any{
				"fn_call":      map[string]any{"name": "test_function"},
				"tool_results": "high score result",
				"instructions": []any{"high score instruction"},
				"answers":      []any{"high score answer"},
				"scores":       []any{5.0}, // score >= 3，应被选中
			},
			map[string]any{
				"fn_call":      map[string]any{"name": "test_function"},
				"tool_results": "low score result",
				"instructions": []any{"low score instruction"},
				"answers":      []any{"low score answer"},
				"scores":       []any{1.0}, // score < 3，不应被选中
			},
		},
	}

	data, err := json.Marshal(allOutputs)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	examplesPath := filepath.Join(tmpDir, functionName+".json")
	if err := os.WriteFile(examplesPath, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	m := &ToolDescriptionMethod{
		BaseMethod: BaseMethod{config: map[string]any{}},
	}

	examples, err := m.LoadExamples(tmpDir, functionName, 3)
	if err != nil {
		t.Fatalf("LoadExamples failed: %v", err)
	}

	// 因为 node_history[::-1] 先看最后一个（score=1.0，不符合），再看第一个（score=5.0，符合）
	// 所以应该选中一个
	if len(examples) != 1 {
		t.Fatalf("expected 1 example (score >= 3), got %d", len(examples))
	}

	if examples[0].Instruction != "high score instruction" {
		t.Errorf("expected 'high score instruction', got %q", examples[0].Instruction)
	}
}

// TestToolDescriptionMethod_GetNegativeExamples 测试 GetNegativeExamples 从文件加载负例
func TestToolDescriptionMethod_GetNegativeExamples(t *testing.T) {
	tmpDir := t.TempDir()

	// 构造负例数据
	negData := []any{
		[]any{
			map[string]any{
				"instructions": []any{"negative instruction"},
				"fn_call":      map[string]any{"name": "test_function", "arguments": map[string]any{}},
				"tool_results": "negative result",
				"answers":      []any{"negative answer"},
				"scores":       []any{2.0}, // 1.0 <= score < 3.0
			},
		},
	}

	data, err := json.Marshal(negData)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	negPath := filepath.Join(tmpDir, "neg_examples.json")
	if err := os.WriteFile(negPath, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	m := &ToolDescriptionMethod{
		BaseMethod: BaseMethod{
			config: map[string]any{
				"neg_ex_input_path":     negPath,
				"num_examples_for_desc": 3,
			},
		},
	}

	examples := m.GetNegativeExamples("test_function")
	if len(examples) != 1 {
		t.Fatalf("expected 1 negative example, got %d", len(examples))
	}

	if examples[0].Instruction != "negative instruction" {
		t.Errorf("expected 'negative instruction', got %q", examples[0].Instruction)
	}
}

// TestToolDescriptionMethod_GetNegativeExamples_回退到示例目录 测试负例文件不存在时回退到示例目录
func TestToolDescriptionMethod_GetNegativeExamples_回退到示例目录(t *testing.T) {
	tmpDir := t.TempDir()
	functionName := "fallback_function"

	// 在 examples_dir 下创建示例文件
	exampleData := []any{
		[]any{
			map[string]any{
				"instructions": []any{"fallback instruction"},
				"fn_call":      map[string]any{"name": "test_function", "arguments": map[string]any{}},
				"tool_results": "fallback result",
				"answers":      []any{"fallback answer"},
				"scores":       []any{1.5},
			},
		},
	}

	data, err := json.Marshal(exampleData)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	examplesPath := filepath.Join(tmpDir, functionName+".json")
	if err := os.WriteFile(examplesPath, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	m := &ToolDescriptionMethod{
		BaseMethod: BaseMethod{
			config: map[string]any{
				"neg_ex_input_path":     "/nonexistent/neg_examples.json",
				"examples_dir":          tmpDir,
				"num_examples_for_desc": 3,
			},
		},
	}

	examples := m.GetNegativeExamples(functionName)
	if len(examples) != 1 {
		t.Fatalf("expected 1 fallback example, got %d", len(examples))
	}

	if examples[0].Instruction != "fallback instruction" {
		t.Errorf("expected 'fallback instruction', got %q", examples[0].Instruction)
	}
}

// TestToolDescriptionMethod_Step_签名验证 测试 Step 方法签名
func TestToolDescriptionMethod_Step_签名验证(t *testing.T) {
	// 验证 ToolDescriptionMethod 实现了 BeamSearchMethod 接口
	var _ BeamSearchMethod = (*ToolDescriptionMethod)(nil)
}

// TestToolDescriptionMethod_Step_it0 测试 Step 方法 it==0 时返回原始描述
func TestToolDescriptionMethod_Step_it0(t *testing.T) {
	// 创建一个 mock evalFn
	config := map[string]any{}
	callFn := func(tool map[string]any, toolInput map[string]any) (string, int) {
		return `{"response": "ok"}`, 0
	}
	evalFn := NewSimpleEval(callFn, config, 0.4, 0.6, nil)

	m := &ToolDescriptionMethod{
		BaseMethod: BaseMethod{config: config},
		evalFn:     evalFn,
	}

	tool := map[string]any{
		"name":        "test_function",
		"description": "A test function description",
	}

	output, data, _, err := m.Step(context.Background(), tool, nil, nil, 0)
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	outputMap, ok := output.(map[string]any)
	if !ok {
		t.Fatalf("expected output to be map[string]any, got %T", output)
	}

	if outputMap["description"] != "A test function description" {
		t.Errorf("expected description 'A test function description', got %v", outputMap["description"])
	}

	if outputMap["iteration"] != 0 {
		t.Errorf("expected iteration 0, got %v", outputMap["iteration"])
	}

	descStr, ok := data.(string)
	if !ok {
		t.Fatalf("expected data to be string, got %T", data)
	}
	if descStr != "A test function description" {
		t.Errorf("expected data 'A test function description', got %q", descStr)
	}
}

// TestDescToInt 测试 descToInt 辅助函数
func TestDescToInt(t *testing.T) {
	tests := []struct {
		input    any
		expected int
	}{
		{42, 42},
		{float64(3.7), 3},
		{int64(100), 100},
		{nil, 0},
		{"not a number", 0},
	}

	for _, tt := range tests {
		result := descToInt(tt.input)
		if result != tt.expected {
			t.Errorf("descToInt(%v) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

// TestDescToFloat64 测试 descToFloat64 辅助函数
func TestDescToFloat64(t *testing.T) {
	tests := []struct {
		input    any
		expected float64
	}{
		{42.0, 42.0},
		{42, 42.0},
		{int64(100), 100.0},
		{nil, 0.0},
		{"not a number", 0.0},
	}

	for _, tt := range tests {
		result := descToFloat64(tt.input)
		if result != tt.expected {
			t.Errorf("descToFloat64(%v) = %f, want %f", tt.input, result, tt.expected)
		}
	}
}

// TestDescToString 测试 descToString 辅助函数
func TestDescToString(t *testing.T) {
	tests := []struct {
		input    any
		expected string
	}{
		{"hello", "hello"},
		{42, "42"},
		{nil, ""},
		{true, "true"},
	}

	for _, tt := range tests {
		result := toString(tt.input)
		if result != tt.expected {
			t.Errorf("toString(%v) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestDescToJSON 测试 descToJSON 辅助函数
func TestDescToJSON(t *testing.T) {
	result := toJSON(map[string]any{"key": "value"})
	expected := `{"key":"value"}`
	if result != expected {
		t.Errorf("toJSON() = %q, want %q", result, expected)
	}
}

// TestDescToExampleTuples 测试 descToExampleTuples 辅助函数
func TestDescToExampleTuples(t *testing.T) {
	tuples := []ExampleTuple{
		{Instruction: "test", FnCall: map[string]any{"name": "fn"}, FnOutput: "out", Answer: "ans"},
	}

	// 测试 []ExampleTuple 输入
	result := descToExampleTuples(tuples)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Instruction != "test" {
		t.Errorf("expected 'test', got %q", result[0].Instruction)
	}

	// 测试 nil 输入
	result = descToExampleTuples(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}

	// 测试 []any 输入
	sliceInput := []any{
		[]any{"inst", map[string]any{"name": "fn"}, "output", "answer"},
	}
	result = descToExampleTuples(sliceInput)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Instruction != "inst" {
		t.Errorf("expected 'inst', got %q", result[0].Instruction)
	}
}

// TestDescTruncateString 测试 descTruncateString 辅助函数
func TestDescTruncateString(t *testing.T) {
	result := descTruncateString("hello world", 5)
	if result != "hello" {
		t.Errorf("descTruncateString('hello world', 5) = %q, want 'hello'", result)
	}

	result = descTruncateString("hi", 5)
	if result != "hi" {
		t.Errorf("descTruncateString('hi', 5) = %q, want 'hi'", result)
	}
}

// TestDescReverseSlice 测试 descReverseSlice 辅助函数
func TestDescReverseSlice(t *testing.T) {
	input := []map[string]any{
		{"a": 1},
		{"b": 2},
		{"c": 3},
	}
	result := descReverseSlice(input)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	if result[0]["c"] != 3 || result[2]["a"] != 1 {
		t.Errorf("reverse not correct: %v", result)
	}
}

// TestDescResultsToMap 测试 descResultsToMap 辅助函数
func TestDescResultsToMap(t *testing.T) {
	// 测试 nil 输入
	result := descResultsToMap(nil)
	if len(result) != 0 {
		t.Errorf("expected empty map for nil input, got %v", result)
	}

	// 测试正常输入
	evalResult := &EvalResult{
		ScoreAvg: 85.5,
		ScoreStd: 5.2,
		Results:  []EvalItemResult{},
	}
	result = descResultsToMap(evalResult)
	if result["score_avg"] != 85.5 {
		t.Errorf("expected score_avg 85.5, got %v", result["score_avg"])
	}
	if result["score_std"] != 5.2 {
		t.Errorf("expected score_std 5.2, got %v", result["score_std"])
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
