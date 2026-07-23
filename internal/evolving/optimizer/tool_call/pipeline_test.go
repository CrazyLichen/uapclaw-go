package tool_call

import (
	"context"
	"testing"
)

// TestCustomizedPipeline_无效阶段 测试无效 stage 参数
func TestCustomizedPipeline_无效阶段(t *testing.T) {
	callFn := func(tool map[string]any, toolInput map[string]any) (string, int) {
		return `{"response": "ok"}`, 0
	}
	_, err := CustomizedPipeline(
		context.Background(), "invalid", map[string]any{}, map[string]any{}, callFn, nil,
	)
	if err == nil {
		t.Error("expected error for invalid stage")
	}
	if err.Error() != "无效阶段: invalid" {
		t.Errorf("error = %v, want '无效阶段: invalid'", err)
	}
}

// TestCustomizedPipeline_无Callable 测试缺少 tool_callable
func TestCustomizedPipeline_无Callable(t *testing.T) {
	_, err := CustomizedPipeline(
		context.Background(), "example", map[string]any{}, map[string]any{}, nil, nil,
	)
	if err == nil {
		t.Error("expected error for missing tool_callable")
	}
}

// TestCustomizedPipeline_FnCallPath 测试 fn_call_path 未实现
func TestCustomizedPipeline_FnCallPath(t *testing.T) {
	config := map[string]any{"fn_call_path": "/some/path"}
	_, err := CustomizedPipeline(
		context.Background(), "example", config, map[string]any{}, nil, nil,
	)
	if err == nil {
		t.Error("expected error for fn_call_path")
	}
}
