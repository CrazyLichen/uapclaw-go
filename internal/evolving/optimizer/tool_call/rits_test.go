package tool_call

import (
	"context"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// TestInvokeWithVerify_无VerifyFunc 测试不带验证函数时吞异常行为
func TestInvokeWithVerify_无VerifyFunc(t *testing.T) {
	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:         1,
		TotalBudgetSecs:    1,
		AttemptTimeoutSecs: 1,
	}
	// 传入 nil model 会报错，验证吞异常行为
	result, err := InvokeWithVerify(context.Background(), nil, "test", "prompt", policy, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 对齐 Python get_rits_response: 吞异常返回 error 字典
	errMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if _, hasErr := errMap["error"]; !hasErr {
		t.Error("expected error key in result map")
	}
}

// TestInvokeWithVerify_VerifyFunc失败 测试验证函数失败时的行为
func TestInvokeWithVerify_VerifyFunc失败(t *testing.T) {
	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:         1,
		TotalBudgetSecs:    1,
		AttemptTimeoutSecs: 1,
	}
	verifyFn := func(text string) (any, error) {
		return nil, fmt.Errorf("parse failed")
	}
	result, err := InvokeWithVerify(context.Background(), nil, "test", "prompt", policy, verifyFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	errMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if _, hasErr := errMap["error"]; !hasErr {
		t.Error("expected error key in result map")
	}
}

// TestInvokeText_无Model 测试简单调用（无 model）
func TestInvokeText_无Model(t *testing.T) {
	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:         1,
		TotalBudgetSecs:    1,
		AttemptTimeoutSecs: 1,
	}
	_, err := InvokeText(context.Background(), nil, "test", "prompt", policy)
	if err == nil {
		t.Error("expected error for nil model")
	}
}
