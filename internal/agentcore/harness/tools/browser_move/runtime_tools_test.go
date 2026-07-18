package browser_move

import (
	"context"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestBuildBrowserRuntimeTools(t *testing.T) {
	runtime := newTestRuntime()
	tools := BuildBrowserRuntimeTools(runtime)
	if len(tools) != 7 {
		t.Fatalf("期望 7 个工具, 得到 %d", len(tools))
	}

	expectedNames := map[string]bool{
		"browser_cancel_run":          false,
		"browser_clear_cancel":        false,
		"browser_custom_action":       false,
		"browser_list_custom_actions": false,
		"browser_probe_interactives":  false,
		"browser_probe_cards":         false,
		"browser_runtime_health":      false,
	}
	for _, tool := range tools {
		name := tool.Card().Name
		if _, ok := expectedNames[name]; !ok {
			t.Errorf("意外的工具名: %s", name)
		}
		expectedNames[name] = true
	}
	for name, found := range expectedNames {
		if !found {
			t.Errorf("缺少工具: %s", name)
		}
	}
}

func TestBrowserCancelTool_Card(t *testing.T) {
	runtime := newTestRuntime()
	tool := NewBrowserCancelTool(runtime)
	if tool.Card().Name != "browser_cancel_run" {
		t.Errorf("期望名称 browser_cancel_run, 得到 %s", tool.Card().Name)
	}
}

func TestBrowserCancelTool_Invoke(t *testing.T) {
	runtime := newTestRuntime()
	tool := NewBrowserCancelTool(runtime)
	ctx := context.Background()

	result, err := tool.Invoke(ctx, map[string]any{
		"session_id": "test-session",
		"request_id": "test-request",
	})
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
	if result["session_id"] != "test-session" {
		t.Errorf("期望 session_id=test-session, 得到 %v", result["session_id"])
	}
}

func TestBrowserCancelTool_Invoke_无RequestID(t *testing.T) {
	runtime := newTestRuntime()
	tool := NewBrowserCancelTool(runtime)
	ctx := context.Background()

	result, err := tool.Invoke(ctx, map[string]any{
		"session_id": "test-session",
	})
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
}

func TestBrowserClearCancelTool_Invoke(t *testing.T) {
	runtime := newTestRuntime()
	tool := NewBrowserClearCancelTool(runtime)
	ctx := context.Background()

	result, err := tool.Invoke(ctx, map[string]any{
		"session_id": "test-session",
	})
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
}

func TestBrowserCustomActionTool_Invoke(t *testing.T) {
	runtime := newTestRuntime()
	tool := NewBrowserCustomActionTool(runtime)
	ctx := context.Background()

	result, err := tool.Invoke(ctx, map[string]any{
		"action":     "click",
		"session_id": "test-session",
		"params":     map[string]any{"selector": "#btn"},
	})
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
	// controller 未初始化时返回 false
	if result["ok"] != false {
		t.Errorf("期望 ok=false (controller 未初始化), 得到 %v", result["ok"])
	}
}

func TestBrowserCustomActionTool_Invoke_无Params(t *testing.T) {
	runtime := newTestRuntime()
	tool := NewBrowserCustomActionTool(runtime)
	ctx := context.Background()

	result, err := tool.Invoke(ctx, map[string]any{
		"action": "scroll",
	})
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
	if result["ok"] != false {
		t.Errorf("期望 ok=false (controller 未初始化), 得到 %v", result["ok"])
	}
}

func TestBrowserListActionsTool_Invoke(t *testing.T) {
	runtime := newTestRuntime()
	tool := NewBrowserListActionsTool(runtime)
	ctx := context.Background()

	result, err := tool.Invoke(ctx, map[string]any{})
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
}

func TestBrowserProbeInteractivesTool_Invoke_默认参数(t *testing.T) {
	runtime := newTestRuntime()
	tool := NewBrowserProbeInteractivesTool(runtime)
	ctx := context.Background()

	result, err := tool.Invoke(ctx, map[string]any{})
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
	// code executor 未就绪
	if result["ok"] != false {
		t.Errorf("期望 ok=false (code executor 未就绪), 得到 %v", result["ok"])
	}
}

func TestBrowserProbeInteractivesTool_Invoke_自定义参数(t *testing.T) {
	runtime := newTestRuntime()
	// 设置 mock code executor
	runtime.SetCodeExecutor(func(_ context.Context, _ string) (any, error) {
		return `{"ok": true, "elements": [{"type": "button"}]}`, nil
	})
	tool := NewBrowserProbeInteractivesTool(runtime)
	ctx := context.Background()

	result, err := tool.Invoke(ctx, map[string]any{
		"max_items":     30,
		"viewport_only": false,
		"query":         "submit",
	})
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
}

func TestBrowserProbeCardsTool_Invoke_默认参数(t *testing.T) {
	runtime := newTestRuntime()
	tool := NewBrowserProbeCardsTool(runtime)
	ctx := context.Background()

	result, err := tool.Invoke(ctx, map[string]any{})
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
	if result["ok"] != false {
		t.Errorf("期望 ok=false (code executor 未就绪), 得到 %v", result["ok"])
	}
}

func TestBrowserProbeCardsTool_Invoke_自定义参数(t *testing.T) {
	runtime := newTestRuntime()
	runtime.SetCodeExecutor(func(_ context.Context, _ string) (any, error) {
		return `{"ok": true, "cards": [{"title": "Item 1"}]}`, nil
	})
	tool := NewBrowserProbeCardsTool(runtime)
	ctx := context.Background()

	result, err := tool.Invoke(ctx, map[string]any{
		"max_cards":       10,
		"viewport_only":   true,
		"include_buttons": false,
		"query":           "laptop",
	})
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
}

func TestBrowserRuntimeHealthTool_Invoke(t *testing.T) {
	runtime := newTestRuntime()
	tool := NewBrowserRuntimeHealthTool(runtime)
	ctx := context.Background()

	result, err := tool.Invoke(ctx, map[string]any{})
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
	if result["provider"] != "openai" {
		t.Errorf("期望 provider=openai, 得到 %v", result["provider"])
	}
}

func TestBrowserCancelTool_Stream(t *testing.T) {
	runtime := newTestRuntime()
	tool := NewBrowserCancelTool(runtime)
	_, err := tool.Stream(context.Background(), map[string]any{})
	if err == nil {
		t.Error("期望 StreamNotSupported 错误")
	}
}

// ── 辅助函数测试 ──

func TestParseInt(t *testing.T) {
	tests := []struct {
		input    any
		expected int
		hasError bool
	}{
		{42, 42, false},
		{int64(42), 42, false},
		{float64(42.0), 42, false},
		{"not a number", 0, true},
	}
	for _, tt := range tests {
		result, err := parseInt(tt.input)
		if tt.hasError {
			if err == nil {
				t.Errorf("parseInt(%v) 期望错误", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseInt(%v) 期望成功, 得到 %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("parseInt(%v) = %d, 期望 %d", tt.input, result, tt.expected)
			}
		}
	}
}

func TestParseBool(t *testing.T) {
	tests := []struct {
		input       any
		defaultVal  bool
		expected    bool
	}{
		{true, true, true},
		{false, true, false},
		{"true", true, true},
		{"false", true, false},
		{"1", true, true},
		{"0", true, false},
		{"yes", true, true},
		{"no", true, false},
		{"unknown", true, true}, // 返回 defaultVal
		{42, true, true},        // 非 bool/string 返回 defaultVal
	}
	for _, tt := range tests {
		result := parseBool(tt.input, tt.defaultVal)
		if result != tt.expected {
			t.Errorf("parseBool(%v, %v) = %v, 期望 %v", tt.input, tt.defaultVal, result, tt.expected)
		}
	}
}

func TestClampInt(t *testing.T) {
	if clampInt(5, 1, 10) != 5 {
		t.Error("范围内应不变")
	}
	if clampInt(0, 1, 10) != 1 {
		t.Error("低于最小值应截断")
	}
	if clampInt(15, 1, 10) != 10 {
		t.Error("超过最大值应截断")
	}
}
