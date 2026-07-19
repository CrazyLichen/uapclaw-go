package tool_call

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// testExampleConfig 创建测试用配置。
// 对齐 Python: _config()
func testExampleConfig() map[string]any {
	return map[string]any{
		"gen_model_id":       "gpt-gen",
		"eval_model_id":      "gpt-eval",
		"llm_api_key":        "k",
		"verbose":            false,
		"num_init_loop":      2,
		"num_refine_steps":   2,
		"num_feedback_steps": 1,
		"score_eval_weight":  0.5,
	}
}

// testExampleTool 创建测试用工具。
// 对齐 Python: _tool()
func testExampleTool() map[string]any {
	return map[string]any{
		"name":        "search",
		"description": `The description of this function is: "desc"`,
	}
}

// TestGetOriginalDescription_ToolBench格式 测试 ToolBench 格式描述提取。
// 对齐 Python: test_get_original_description - toolbench 格式
func TestGetOriginalDescription_ToolBench格式(t *testing.T) {
	m := &APICallToExampleMethod{}
	result := m.GetOriginalDescription(testExampleTool())
	if result != "desc" {
		t.Errorf("期望 'desc', 实际 '%s'", result)
	}
}

// TestGetOriginalDescription_普通描述 测试普通描述直接返回。
// 对齐 Python: test_get_original_description - plain 格式
func TestGetOriginalDescription_普通描述(t *testing.T) {
	m := &APICallToExampleMethod{}
	tool := map[string]any{"name": "x", "description": "plain"}
	result := m.GetOriginalDescription(tool)
	if result != "plain" {
		t.Errorf("期望 'plain', 实际 '%s'", result)
	}
}

// TestGetOriginalDescription_空描述 测试空描述。
func TestGetOriginalDescription_空描述(t *testing.T) {
	m := &APICallToExampleMethod{}
	tool := map[string]any{"description": ""}
	result := m.GetOriginalDescription(tool)
	if result != "" {
		t.Errorf("期望空字符串, 实际 '%s'", result)
	}
}

// TestGetOriginalDescription_无描述字段 测试无 description 字段。
func TestGetOriginalDescription_无描述字段(t *testing.T) {
	m := &APICallToExampleMethod{}
	tool := map[string]any{"name": "test_api"}
	result := m.GetOriginalDescription(tool)
	if result != "" {
		t.Errorf("期望空字符串, 实际 '%s'", result)
	}
}

// TestNewAPICallToExampleMethod 测试构造函数。
func TestNewAPICallToExampleMethod(t *testing.T) {
	config := testExampleConfig()
	callFn := func(tool map[string]any, toolInput map[string]any) (string, int) {
		return `{"response": "ok"}`, 0
	}
	evalFn := NewSimpleEval(callFn, config, 0.4, 0.6, nil)

	m := NewAPICallToExampleMethod(config, nil, callFn, evalFn, nil, nil)

	if m == nil {
		t.Fatal("期望非 nil APICallToExampleMethod")
	}
	if m.runToolWithAPICall == nil {
		t.Error("期望 runToolWithAPICall 非空")
	}
	if m.evalFn == nil {
		t.Error("期望 evalFn 非空")
	}
	if m.apiKeys != nil {
		t.Error("期望 apiKeys 为 nil")
	}
	if m.nonOptParams == nil {
		t.Error("期望 nonOptParams 非空（应为空切片）")
	}
	if len(m.nonOptParams) != 0 {
		t.Errorf("期望 nonOptParams 长度 0, 实际 %d", len(m.nonOptParams))
	}
}

// TestNewAPICallToExampleMethod_带APIKeys 测试带 API Keys 的构造。
func TestNewAPICallToExampleMethod_带APIKeys(t *testing.T) {
	config := map[string]any{}
	apiKeys := []string{"key1", "key2"}
	nonOptParams := []string{"param1", "param2"}

	m := NewAPICallToExampleMethod(config, nil, nil, nil, apiKeys, nonOptParams)

	if m.apiKeys == nil {
		t.Error("期望 apiKeys 非空")
	}
	if len(m.nonOptParams) != 2 {
		t.Errorf("期望 nonOptParams 长度 2, 实际 %d", len(m.nonOptParams))
	}
}

// TestAPICallToExampleMethod_接口实现 测试 BeamSearchMethod 接口实现。
func TestAPICallToExampleMethod_接口实现(t *testing.T) {
	var _ BeamSearchMethod = (*APICallToExampleMethod)(nil)
}

// TestAPICallToExampleMethod_GetExamples 测试 GetExamples 返回 nil。
func TestAPICallToExampleMethod_GetExamples(t *testing.T) {
	m := &APICallToExampleMethod{}
	result := m.GetExamples(context.Background(), map[string]any{})
	if result != nil {
		t.Errorf("期望 nil, 实际 %v", result)
	}
}

// TestGenerateAPICallFromDescription_Mock 测试 API 调用生成。
// 对齐 Python: test_generate_api_call_from_description
func TestGenerateAPICallFromDescription_Mock(t *testing.T) {
	config := testExampleConfig()
	method := NewAPICallToExampleMethod(config, (*llm.Model)(nil), nil, nil, nil, nil)

	origImpl := invokeWithVerifyImpl
	defer func() { invokeWithVerifyImpl = origImpl }()

	invokeWithVerifyImpl = func(
		ctx context.Context,
		model *llm.Model,
		modelName string,
		prompt string,
		policy llm_resilience.LLMInvokePolicy,
		verifyFn VerifyFunc,
	) (any, error) {
		if modelName != "gpt-gen" {
			t.Errorf("期望 model name 'gpt-gen', 实际 '%s'", modelName)
		}
		if !strings.Contains(prompt, "search") {
			t.Error("Prompt 应包含函数名 'search'")
		}
		return verifyFn(`{"name":"search","arguments":{"q":"x"}}`)
	}

	result, err := method.GenerateAPICallFromDescription(
		context.Background(),
		testExampleTool(),
		nil, 1, nil,
	)
	if err != nil {
		t.Fatalf("GenerateAPICallFromDescription 失败: %v", err)
	}
	if result["name"] != "search" {
		t.Errorf("期望 name='search', 实际 %v", result["name"])
	}
	args, ok := result["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("期望 arguments 为 map[string]any, 实际 %T", result["arguments"])
	}
	if args["q"] != "x" {
		t.Errorf("期望 q='x', 实际 %v", args["q"])
	}
}

// TestGenerateAPICallFromDescription_有前驱输出 测试带前驱输出的 API 调用生成。
func TestGenerateAPICallFromDescription_有前驱输出(t *testing.T) {
	config := testExampleConfig()
	method := NewAPICallToExampleMethod(config, (*llm.Model)(nil), nil, nil, nil, nil)

	origImpl := invokeWithVerifyImpl
	defer func() { invokeWithVerifyImpl = origImpl }()

	invokeWithVerifyImpl = func(
		ctx context.Context,
		model *llm.Model,
		modelName string,
		prompt string,
		policy llm_resilience.LLMInvokePolicy,
		verifyFn VerifyFunc,
	) (any, error) {
		return verifyFn(`{"name":"search","arguments":{"q":"test"}}`)
	}

	prevOutput := []any{
		map[string]any{
			"fn_call":      map[string]any{"name": "search", "arguments": map[string]any{"q": "old"}},
			"tool_results": map[string]any{"ok": 1},
			"status_code":  0,
		},
	}

	result, err := method.GenerateAPICallFromDescription(
		context.Background(),
		testExampleTool(),
		nil, 1, prevOutput,
	)
	if err != nil {
		t.Fatalf("GenerateAPICallFromDescription 失败: %v", err)
	}
	if result["name"] != "search" {
		t.Errorf("期望 name='search', 实际 %v", result["name"])
	}
}

// TestGenerateAPICallFromDescription_函数名不匹配 测试验证错误。
// 对齐 Python: test_generate_api_call_from_description_validation_error
func TestGenerateAPICallFromDescription_函数名不匹配(t *testing.T) {
	config := testExampleConfig()
	method := NewAPICallToExampleMethod(config, (*llm.Model)(nil), nil, nil, nil, nil)

	origImpl := invokeWithVerifyImpl
	defer func() { invokeWithVerifyImpl = origImpl }()

	invokeWithVerifyImpl = func(
		ctx context.Context,
		model *llm.Model,
		modelName string,
		prompt string,
		policy llm_resilience.LLMInvokePolicy,
		verifyFn VerifyFunc,
	) (any, error) {
		// verifyFn 失败后 InvokeWithVerify 返回 error dict
		return verifyFn(`{"name":"other","arguments":{}}`)
	}

	_, err := method.GenerateAPICallFromDescription(
		context.Background(),
		testExampleTool(),
		nil, 1, nil,
	)
	// 函数名不匹配时，verifyFn 失败导致 InvokeWithVerify 返回错误
	if err == nil {
		t.Error("期望函数名不匹配时返回错误")
	}
}

// TestCritiqueAPICall_Mock 测试批判 API 调用。
func TestCritiqueAPICall_Mock(t *testing.T) {
	config := testExampleConfig()
	method := NewAPICallToExampleMethod(config, (*llm.Model)(nil), nil, nil, nil, nil)

	origImpl := invokeWithVerifyImpl
	defer func() { invokeWithVerifyImpl = origImpl }()

	invokeWithVerifyImpl = func(
		ctx context.Context,
		model *llm.Model,
		modelName string,
		prompt string,
		policy llm_resilience.LLMInvokePolicy,
		verifyFn VerifyFunc,
	) (any, error) {
		return verifyFn(`{"analysis":"ok","err_code":0}`)
	}

	fnCall := map[string]any{"name": "search", "arguments": map[string]any{"q": "x"}}
	result, err := method.CritiqueAPICall(context.Background(), testExampleTool(), fnCall, "result")
	if err != nil {
		t.Fatalf("CritiqueAPICall 失败: %v", err)
	}
	errCode, ok := result["err_code"].(int)
	if !ok {
		t.Fatalf("期望 err_code 为 int, 实际 %T", result["err_code"])
	}
	if errCode != 0 {
		t.Errorf("期望 err_code=0, 实际 %d", errCode)
	}
}

// TestCritiqueAPICall_错误响应 测试 API 调用批判返回错误码。
func TestCritiqueAPICall_错误响应(t *testing.T) {
	config := testExampleConfig()
	method := NewAPICallToExampleMethod(config, (*llm.Model)(nil), nil, nil, nil, nil)

	origImpl := invokeWithVerifyImpl
	defer func() { invokeWithVerifyImpl = origImpl }()

	invokeWithVerifyImpl = func(
		ctx context.Context,
		model *llm.Model,
		modelName string,
		prompt string,
		policy llm_resilience.LLMInvokePolicy,
		verifyFn VerifyFunc,
	) (any, error) {
		return verifyFn(`{"analysis":"bad params","err_code":-1}`)
	}

	fnCall := map[string]any{"name": "search", "arguments": map[string]any{"q": "x"}}
	result, err := method.CritiqueAPICall(context.Background(), testExampleTool(), fnCall, "error response")
	if err != nil {
		t.Fatalf("CritiqueAPICall 失败: %v", err)
	}
	errCode, ok := result["err_code"].(int)
	if !ok {
		t.Fatalf("期望 err_code 为 int, 实际 %T", result["err_code"])
	}
	if errCode != -1 {
		t.Errorf("期望 err_code=-1, 实际 %d", errCode)
	}
}

// TestCritiqueAPICall_长响应截断 测试长响应（>2048 字符）截断。
func TestCritiqueAPICall_长响应截断(t *testing.T) {
	config := testExampleConfig()
	method := NewAPICallToExampleMethod(config, (*llm.Model)(nil), nil, nil, nil, nil)

	origImpl := invokeWithVerifyImpl
	defer func() { invokeWithVerifyImpl = origImpl }()

	invokeWithVerifyImpl = func(
		ctx context.Context,
		model *llm.Model,
		modelName string,
		prompt string,
		policy llm_resilience.LLMInvokePolicy,
		verifyFn VerifyFunc,
	) (any, error) {
		return verifyFn(`{"analysis":"ok","err_code":0}`)
	}

	fnCall := map[string]any{"name": "search", "arguments": map[string]any{"q": "x"}}
	longResponse := strings.Repeat("x", 3000)
	result, err := method.CritiqueAPICall(context.Background(), testExampleTool(), fnCall, longResponse)
	if err != nil {
		t.Fatalf("CritiqueAPICall 失败: %v", err)
	}
	errCode, ok := result["err_code"].(int)
	if !ok || errCode != 0 {
		t.Errorf("期望 err_code=0, 实际 %v", result["err_code"])
	}
}

// TestGenerateInstructionFromAPICall_Mock 测试指令生成。
// 对齐 Python: test_critique_and_instruction_and_batch_methods
func TestGenerateInstructionFromAPICall_Mock(t *testing.T) {
	config := testExampleConfig()
	method := NewAPICallToExampleMethod(config, (*llm.Model)(nil), nil, nil, nil, nil)

	origImpl := invokeWithVerifyImpl
	defer func() { invokeWithVerifyImpl = origImpl }()

	invokeWithVerifyImpl = func(
		ctx context.Context,
		model *llm.Model,
		modelName string,
		prompt string,
		policy llm_resilience.LLMInvokePolicy,
		verifyFn VerifyFunc,
	) (any, error) {
		return verifyFn(`{"instruction":"I need weather in Beijing"}`)
	}

	fnCall := map[string]any{"name": "search", "arguments": map[string]any{"q": "x"}}
	result, err := method.GenerateInstructionFromAPICall(
		context.Background(),
		testExampleTool(),
		fnCall,
		"resp",
		nil,
	)
	if err != nil {
		t.Fatalf("GenerateInstructionFromAPICall 失败: %v", err)
	}
	if !strings.Contains(result, "Beijing") {
		t.Errorf("期望指令包含 'Beijing', 实际 '%s'", result)
	}
}

// TestGenerateInstructionFromAPICall_有前驱输出 测试带前驱输出的指令生成。
func TestGenerateInstructionFromAPICall_有前驱输出(t *testing.T) {
	config := testExampleConfig()
	method := NewAPICallToExampleMethod(config, (*llm.Model)(nil), nil, nil, nil, nil)

	origImpl := invokeWithVerifyImpl
	defer func() { invokeWithVerifyImpl = origImpl }()

	invokeWithVerifyImpl = func(
		ctx context.Context,
		model *llm.Model,
		modelName string,
		prompt string,
		policy llm_resilience.LLMInvokePolicy,
		verifyFn VerifyFunc,
	) (any, error) {
		return verifyFn(`{"instruction":"I want to search for weather"}`)
	}

	fnCall := map[string]any{"name": "search", "arguments": map[string]any{"q": "x"}}
	prevOutput := map[string]any{
		"instructions":      []string{"a"},
		"scores":           []float64{1},
		"batch_reflection": "b",
	}
	result, err := method.GenerateInstructionFromAPICall(
		context.Background(),
		testExampleTool(),
		fnCall,
		"resp",
		prevOutput,
	)
	if err != nil {
		t.Fatalf("GenerateInstructionFromAPICall 失败: %v", err)
	}
	if result == "" {
		t.Error("期望非空指令")
	}
}

// TestCritiqueInstruction_Mock 测试指令批判。
// 对齐 Python: test_critique_and_instruction_and_batch_methods
func TestCritiqueInstruction_Mock(t *testing.T) {
	config := testExampleConfig()
	method := NewAPICallToExampleMethod(config, (*llm.Model)(nil), nil, nil, nil, nil)

	origImpl := invokeWithVerifyImpl
	defer func() { invokeWithVerifyImpl = origImpl }()

	invokeWithVerifyImpl = func(
		ctx context.Context,
		model *llm.Model,
		modelName string,
		prompt string,
		policy llm_resilience.LLMInvokePolicy,
		verifyFn VerifyFunc,
	) (any, error) {
		return verifyFn(`{"analysis":"good","score":3}`)
	}

	fnCall := map[string]any{"name": "search", "arguments": map[string]any{"q": "x"}}
	result, err := method.CritiqueInstruction(
		context.Background(),
		testExampleTool(),
		"inst",
		fnCall,
		"resp",
		"ans",
	)
	if err != nil {
		t.Fatalf("CritiqueInstruction 失败: %v", err)
	}
	score, ok := result["score"].(int)
	if !ok {
		// 也可能是 float64
		if scoreFloat, okFloat := result["score"].(float64); okFloat {
			if scoreFloat != 3 {
				t.Errorf("期望 score=3, 实际 %v", scoreFloat)
			}
			return
		}
		t.Fatalf("期望 score 为 int 或 float64, 实际 %T", result["score"])
	}
	if score != 3 {
		t.Errorf("期望 score=3, 实际 %d", score)
	}
}

// TestBatchReflectionWithScores_Mock 测试批量反思。
// 对齐 Python: test_critique_and_instruction_and_batch_methods
func TestBatchReflectionWithScores_Mock(t *testing.T) {
	config := testExampleConfig()
	method := NewAPICallToExampleMethod(config, (*llm.Model)(nil), nil, nil, nil, nil)

	origImpl := invokeWithVerifyImpl
	defer func() { invokeWithVerifyImpl = origImpl }()

	invokeWithVerifyImpl = func(
		ctx context.Context,
		model *llm.Model,
		modelName string,
		prompt string,
		policy llm_resilience.LLMInvokePolicy,
		verifyFn VerifyFunc,
	) (any, error) {
		return verifyFn("reflection")
	}

	fnCall := map[string]any{"name": "search", "arguments": map[string]any{"q": "x"}}
	result, err := method.BatchReflectionWithScores(
		context.Background(),
		testExampleTool(),
		fnCall,
		[]string{"i1"},
		[]float64{2},
		[]string{"a1"},
	)
	if err != nil {
		t.Fatalf("BatchReflectionWithScores 失败: %v", err)
	}
	if result != "reflection" {
		t.Errorf("期望 'reflection', 实际 '%s'", result)
	}
}

// TestDeepCopyMap 测试深拷贝 map。
func TestDeepCopyMap(t *testing.T) {
	original := map[string]any{
		"name":   "test",
		"params": map[string]any{"key": "value"},
		"list":   []any{1, 2, 3},
	}

	copied := deepCopyMap(original)

	// 修改拷贝不影响原始
	copied["name"] = "modified"
	if original["name"] != "test" {
		t.Error("深拷贝应不影响原始 map")
	}

	innerMap, _ := copied["params"].(map[string]any)
	innerMap["key"] = "modified"
	origInner, _ := original["params"].(map[string]any)
	if origInner["key"] != "value" {
		t.Error("深拷贝应不影响嵌套 map")
	}
}

// TestDeepCopyMap_Nil 测试 nil map 深拷贝。
func TestDeepCopyMap_Nil(t *testing.T) {
	if deepCopyMap(nil) != nil {
		t.Error("deepCopyMap(nil) 应返回 nil")
	}

	result := deepCopyMap(map[string]any{})
	if len(result) != 0 {
		t.Error("空 map 深拷贝应返回空 map")
	}
}

// TestDeepCopySlice 测试深拷贝 slice。
func TestDeepCopySlice(t *testing.T) {
	original := []any{1, "two", map[string]any{"key": "val"}}
	copied := deepCopySlice(original)

	if len(copied) != len(original) {
		t.Errorf("期望长度 %d, 实际 %d", len(original), len(copied))
	}

	// 修改嵌套 map 不影响原始
	if m, ok := copied[2].(map[string]any); ok {
		m["key"] = "changed"
	}
	if origM, ok := original[2].(map[string]any); ok {
		if origM["key"] != "val" {
			t.Error("深拷贝应不影响嵌套 map")
		}
	}
}

// TestLastN 测试 lastN 辅助函数。
func TestLastN(t *testing.T) {
	slice := []string{"a", "b", "c", "d"}

	result := lastN(slice, 2)
	if len(result) != 2 || result[0] != "c" || result[1] != "d" {
		t.Errorf("期望 ['c', 'd'], 实际 %v", result)
	}

	// n >= len(slice)
	result = lastN(slice, 10)
	if len(result) != 4 {
		t.Errorf("期望完整 slice, 实际 %v", result)
	}
}

// TestLastNFloat 测试 lastNFloat 辅助函数。
func TestLastNFloat(t *testing.T) {
	slice := []float64{1.0, 2.0, 3.0}

	result := lastNFloat(slice, 2)
	if len(result) != 2 || result[0] != 2.0 || result[1] != 3.0 {
		t.Errorf("期望 [2.0, 3.0], 实际 %v", result)
	}
}

// TestToIntSafe 测试 toIntSafe 辅助函数。
func TestToIntSafe(t *testing.T) {
	if toIntSafe(42) != 42 {
		t.Errorf("期望 42, 实际 %d", toIntSafe(42))
	}
	if toIntSafe(float64(3)) != 3 {
		t.Errorf("期望 3, 实际 %d", toIntSafe(float64(3)))
	}
	if toIntSafe(nil) != 0 {
		t.Errorf("期望 0, 实际 %d", toIntSafe(nil))
	}
	if toIntSafe("abc") != 0 {
		t.Errorf("期望 0, 实际 %d", toIntSafe("abc"))
	}
	if toIntSafe(-1) != -1 {
		t.Errorf("期望 -1, 实际 %d", toIntSafe(-1))
	}
}

// TestJsonStr 测试 jsonStr 辅助函数。
func TestJsonStr(t *testing.T) {
	// 测试 map
	result := jsonStr(map[string]any{"key": "value"})
	if result != `{"key":"value"}` {
		t.Errorf("期望 '{\"key\":\"value\"}', 实际 '%s'", result)
	}

	// 测试字符串
	result = jsonStr("hello")
	if result != `"hello"` {
		t.Errorf("期望 '\"hello\"', 实际 '%s'", result)
	}

	// 测试整数
	result = jsonStr(42)
	if result != `42` {
		t.Errorf("期望 '42', 实际 '%s'", result)
	}
}

// TestStep_通过MockRits 测试 Step 方法完整流程。
// 使用 mock invokeWithVerifyImpl 替换 LLM 调用。
func TestStep_通过MockRits(t *testing.T) {
	config := testExampleConfig()

	// 对齐 Python: api_call_fn=lambda tool, fn_call: ('{"response":"ok"}', 0)
	callAPIFn := func(tool map[string]any, toolInput map[string]any) (string, int) {
		return `{"response": "ok"}`, 0
	}

	evalFn := NewSimpleEval(callAPIFn, config, 0.4, 0.6, (*llm.Model)(nil))
	method := NewAPICallToExampleMethod(config, (*llm.Model)(nil), callAPIFn, evalFn, nil, nil)

	// Mock invokeWithVerifyImpl — 所有 LLM 调用返回预定义响应
	origImpl := invokeWithVerifyImpl
	defer func() { invokeWithVerifyImpl = origImpl }()

	callCount := 0
	invokeWithVerifyImpl = func(
		ctx context.Context,
		model *llm.Model,
		modelName string,
		prompt string,
		policy llm_resilience.LLMInvokePolicy,
		verifyFn VerifyFunc,
	) (any, error) {
		callCount++
		// 交替返回不同的响应
		switch {
		case strings.Contains(prompt, "example API call"):
			// GenerateAPICallFromDescription
			return verifyFn(`{"name":"search","arguments":{"q":"x"}}`)
		case strings.Contains(prompt, "analyze the response"):
			// CritiqueAPICall
			return verifyFn(`{"analysis":"","err_code":0}`)
		case strings.Contains(prompt, "generate a user instruction"):
			// GenerateInstructionFromAPICall
			return verifyFn(`{"instruction":"I need to search for something"}`)
		case strings.Contains(prompt, "give a `score`"):
			// CritiqueInstruction
			return verifyFn(`{"analysis":"good","score":3}`)
		case strings.Contains(prompt, "identify and contrast"):
			// BatchReflectionWithScores
			return verifyFn("reflection text")
		default:
			// ProduceAnswerFromAPICall 的 verifyFn
			return verifyFn(`{"answer":"the answer"}`)
		}
	}

	outputs, insts, score, err := method.Step(context.Background(), testExampleTool(), nil, nil, 0)
	if err != nil {
		t.Fatalf("Step 失败: %v", err)
	}

	outputsMap, ok := outputs.(map[string]any)
	if !ok {
		t.Fatalf("期望 outputs 为 map[string]any, 实际 %T", outputs)
	}

	instsSlice, ok := insts.([]string)
	if !ok {
		t.Fatalf("期望 insts 为 []string, 实际 %T", insts)
	}

	if len(instsSlice) == 0 {
		t.Error("期望至少一条指令")
	}

	statusCode, _ := outputsMap["status_code"].(int)
	if statusCode != 0 {
		t.Errorf("期望 status_code=0, 实际 %d", statusCode)
	}

	_ = score // 分数值取决于 LLM 响应

	// 验证 LLM 至少被调用了几次
	if callCount < 3 {
		t.Errorf("期望至少 3 次 LLM 调用, 实际 %d", callCount)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
