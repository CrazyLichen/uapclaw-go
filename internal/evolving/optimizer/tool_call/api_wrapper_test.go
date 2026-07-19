package tool_call

import (
	"encoding/json"
	"fmt"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSimpleAPIWrapperFromCallable 测试创建 API 包装器
func TestNewSimpleAPIWrapperFromCallable(t *testing.T) {
	callable := func(tool map[string]any, toolInput map[string]any) (string, int) {
		return "", 0
	}
	wrapper := NewSimpleAPIWrapperFromCallable(callable, "test_fn")
	if wrapper == nil {
		t.Fatal("expected non-nil wrapper")
	}
	if wrapper.fnCallName != "test_fn" {
		t.Errorf("expected fnCallName 'test_fn', got %q", wrapper.fnCallName)
	}
}

// TestSimpleAPIWrapperFromCallable_Call_成功 测试成功调用场景
func TestSimpleAPIWrapperFromCallable_Call_成功(t *testing.T) {
	// 对齐 Python: fn(params) → output = fn(params); return json.dumps({'response': output}), 0
	callable := func(tool map[string]any, toolInput map[string]any) (string, int) {
		output := fmt.Sprintf("result for %v", toolInput["query"])
		result, _ := json.Marshal(map[string]any{"response": output})
		return string(result), 0
	}
	wrapper := NewSimpleAPIWrapperFromCallable(callable, "my_fn")

	tool := map[string]any{"name": "test_tool"}
	toolInput := map[string]any{"query": "hello"}

	resp, code := wrapper.Call(tool, toolInput)
	if code != 0 {
		t.Errorf("expected status code 0, got %d", code)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if parsed["response"] != "result for hello" {
		t.Errorf("expected response 'result for hello', got %v", parsed["response"])
	}
}

// TestSimpleAPIWrapperFromCallable_Call_失败 测试调用失败场景
func TestSimpleAPIWrapperFromCallable_Call_失败(t *testing.T) {
	// 对齐 Python: except Exception as e: return json.dumps({"error": ..., "response": ""}), 12
	callable := func(tool map[string]any, toolInput map[string]any) (string, int) {
		result, _ := json.Marshal(map[string]string{
			"error":    "request invalid, error: something went wrong",
			"response": "",
		})
		return string(result), 12
	}
	wrapper := NewSimpleAPIWrapperFromCallable(callable, "my_fn")

	tool := map[string]any{"name": "test_tool"}
	toolInput := map[string]any{"query": "fail"}

	resp, code := wrapper.Call(tool, toolInput)
	if code != 12 {
		t.Errorf("expected status code 12, got %d", code)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if _, hasErr := parsed["error"]; !hasErr {
		t.Error("expected error key in response")
	}
	if parsed["response"] != "" {
		t.Errorf("expected empty response, got %v", parsed["response"])
	}
}

// TestSimpleAPIWrapperFromCallable_Call_无Callable 测试无注册函数时的错误返回
func TestSimpleAPIWrapperFromCallable_Call_无Callable(t *testing.T) {
	// 对齐 Python: fn = self.functions.get(self.fn_call_name) → None → error
	wrapper := NewSimpleAPIWrapperFromCallable(nil, "missing_fn")

	tool := map[string]any{"name": "test_tool"}
	toolInput := map[string]any{}

	resp, code := wrapper.Call(tool, toolInput)
	if code != 12 {
		t.Errorf("expected status code 12, got %d", code)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if _, hasErr := parsed["error"]; !hasErr {
		t.Error("expected error key in response")
	}
	if parsed["response"] != "" {
		t.Errorf("expected empty response, got %v", parsed["response"])
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
