package browser_move

import (
	"testing"

	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewBrowserRuntimeRail(t *testing.T) {
	runtime := newTestRuntime()
	rail := NewBrowserRuntimeRail(runtime)
	if rail == nil {
		t.Fatal("期望非 nil rail")
	}
	if rail.Runtime() != runtime {
		t.Error("Runtime() 应返回传入的 runtime")
	}
}

func TestNewBrowserRuntimeRail_Priority(t *testing.T) {
	runtime := newTestRuntime()
	rail := NewBrowserRuntimeRail(runtime)
	// BaseRail 默认优先级为 50
	if rail.Priority() != 50 {
		t.Errorf("期望优先级 50, 得到 %d", rail.Priority())
	}
}

func TestExtractProgressPayload_包含标签(t *testing.T) {
	text := `Here is the result <browser_progress>{"status":"completed","completed_steps":["step1"]}</browser_progress> end`
	clean, payload := ExtractProgressPayload(text)
	if payload == nil {
		t.Fatal("期望非 nil payload")
	}
	if payload["status"] != "completed" {
		t.Errorf("期望 status=completed, 得到 %v", payload["status"])
	}
	if clean == text {
		t.Error("清理后文本应与原文不同")
	}
}

func TestExtractProgressPayload_不包含标签(t *testing.T) {
	text := "No progress tag here"
	clean, payload := ExtractProgressPayload(text)
	if payload != nil {
		t.Error("期望 nil payload")
	}
	if clean != text {
		t.Error("无标签时文本应不变")
	}
}

func TestExtractProgressPayload_空字符串(t *testing.T) {
	clean, payload := ExtractProgressPayload("")
	if payload != nil {
		t.Error("期望 nil payload")
	}
	if clean != "" {
		t.Error("期望空字符串")
	}
}

func TestExtractProgressPayload_无效JSON(t *testing.T) {
	text := `<browser_progress>{invalid json}</browser_progress>`
	_, payload := ExtractProgressPayload(text)
	if payload != nil {
		t.Error("期望 nil payload（无效 JSON）")
	}
}

func TestExtractProgressPayload_多行JSON(t *testing.T) {
	text := `Result <browser_progress>{"status":"partial","completed_steps":["a","b"],"remaining_steps":["c"]}</browser_progress>`
	clean, payload := ExtractProgressPayload(text)
	if payload == nil {
		t.Fatal("期望非 nil payload")
	}
	if payload["status"] != "partial" {
		t.Errorf("期望 status=partial, 得到 %v", payload["status"])
	}
	steps, ok := payload["completed_steps"].([]any)
	if !ok || len(steps) != 2 {
		t.Errorf("期望 completed_steps 有 2 个元素, 得到 %v", payload["completed_steps"])
	}
	// 清理后不应包含标签
	if clean == text {
		t.Error("清理后文本应与原文不同")
	}
}

func TestBuildProgressResult_完成(t *testing.T) {
	payload := map[string]any{
		"status": "completed",
	}
	result := BuildProgressResult(payload, "done text")
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
	if result["status"] != "completed" {
		t.Errorf("期望 status=completed, 得到 %v", result["status"])
	}
	if result["error"] != "" {
		t.Errorf("完成时 error 应为空, 得到 %v", result["error"])
	}
}

func TestBuildProgressResult_部分完成(t *testing.T) {
	payload := map[string]any{
		"status": "partial",
	}
	result := BuildProgressResult(payload, "partial text")
	if result["ok"] != false {
		t.Errorf("期望 ok=false, 得到 %v", result["ok"])
	}
	if result["error"] != "browser_task_incomplete" {
		t.Errorf("期望 browser_task_incomplete, 得到 %v", result["error"])
	}
}

func TestBuildProgressResult_空状态(t *testing.T) {
	payload := map[string]any{}
	result := BuildProgressResult(payload, "")
	if result["status"] != "partial" {
		t.Errorf("空状态默认为 partial, 得到 %v", result["status"])
	}
}

func TestBuildProgressResult_失败状态(t *testing.T) {
	payload := map[string]any{
		"status": "failed",
	}
	result := BuildProgressResult(payload, "")
	if result["ok"] != false {
		t.Errorf("期望 ok=false, 得到 %v", result["ok"])
	}
	if result["error"] != "browser_task_incomplete" {
		t.Errorf("失败状态 error 应为 browser_task_incomplete, 得到 %v", result["error"])
	}
}

func TestIsBrowserProgressTool_浏览器工具(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"browser_navigate", true},
		{"browser_click", true},
		{"browser_type", true},
		{"browser_screenshot", true},
		{"browser_cancel_run", false},
		{"browser_clear_cancel", false},
		{"browser_list_custom_actions", false},
		{"browser_runtime_health", false},
		{"playwright_navigate", false},
		{"", false},
		{"mcp.browser_navigate", true},
	}
	for _, tt := range tests {
		result := IsBrowserProgressTool(tt.name)
		if result != tt.expected {
			t.Errorf("IsBrowserProgressTool(%q) = %v, 期望 %v", tt.name, result, tt.expected)
		}
	}
}

func TestNormalizeToolResult_Nil(t *testing.T) {
	result := normalizeToolResult(nil)
	if result != nil {
		t.Errorf("期望 nil, 得到 %v", result)
	}
}

func TestNormalizeToolResult_非字典(t *testing.T) {
	result := normalizeToolResult("raw string")
	if result != "raw string" {
		t.Errorf("期望原样返回, 得到 %v", result)
	}
}

func TestNormalizeToolResult_DataSuccess(t *testing.T) {
	input := map[string]any{
		"data":    "payload",
		"success": true,
	}
	result := normalizeToolResult(input)
	if result != "payload" {
		t.Errorf("期望 payload, 得到 %v", result)
	}
}

func TestNormalizeToolResult_DataNilWithError(t *testing.T) {
	input := map[string]any{
		"data":    nil,
		"success": false,
		"error":   "something went wrong",
	}
	result := normalizeToolResult(input)
	m, ok := result.(map[string]any)
	if !ok {
		t.Errorf("期望 map, 得到 %T", result)
	} else if m["ok"] != false || m["error"] != "something went wrong" {
		t.Errorf("期望 {ok:false, error:...}, 得到 %v", m)
	}
}

func TestNormalizeToolResult_普通字典(t *testing.T) {
	input := map[string]any{"foo": "bar"}
	result := normalizeToolResult(input)
	m, ok := result.(map[string]any)
	if !ok {
		t.Errorf("期望原样返回 map, 得到 %T", result)
	} else if m["foo"] != "bar" {
		t.Errorf("期望 foo=bar, 得到 %v", m["foo"])
	}
}

func TestIsMaxIterationResultFromMap_最大迭代(t *testing.T) {
	result := map[string]any{
		"output":      "Max iterations reached without completion",
		"result_type": "error",
	}
	if !isMaxIterationResultFromMap(result) {
		t.Error("期望识别为最大迭代次数结果")
	}
}

func TestIsMaxIterationResultFromMap_非错误类型(t *testing.T) {
	result := map[string]any{
		"output":      "Max iterations reached without completion",
		"result_type": "answer",
	}
	if isMaxIterationResultFromMap(result) {
		t.Error("result_type=answer 不应识别为最大迭代次数结果")
	}
}

func TestIsMaxIterationResultFromMap_其他错误(t *testing.T) {
	result := map[string]any{
		"output":      "some other error",
		"result_type": "error",
	}
	if isMaxIterationResultFromMap(result) {
		t.Error("不包含 Max iterations 标记")
	}
}

func TestBrowserRuntimeRail_GetCallbacks(t *testing.T) {
	runtime := newTestRuntime()
	rail := NewBrowserRuntimeRail(runtime)

	callbacks := rail.GetCallbacks()
	if len(callbacks) != 4 {
		t.Errorf("期望 4 个回调, 得到 %d", len(callbacks))
	}

	// 验证包含 4 个事件
	expectedEvents := map[string]bool{
		"before_invoke":     false,
		"before_model_call": false,
		"after_tool_call":   false,
		"after_invoke":      false,
	}
	for event := range callbacks {
		expectedEvents[string(event)] = true
	}
	for event, found := range expectedEvents {
		if !found {
			t.Errorf("缺少回调事件: %s", event)
		}
	}
}

func TestBrowserRuntimeRail_ProgressFormatGuidance(t *testing.T) {
	// 验证格式指南包含中英文
	if browserProgressFormatGuidance["en"] == "" {
		t.Error("英文格式指南不应为空")
	}
	if browserProgressFormatGuidance["cn"] == "" {
		t.Error("中文格式指南不应为空")
	}
	// 验证包含关键字段名
	enGuidance := browserProgressFormatGuidance["en"]
	cnGuidance := browserProgressFormatGuidance["cn"]
	if len(enGuidance) < 50 {
		t.Error("英文格式指南过短")
	}
	if len(cnGuidance) < 50 {
		t.Error("中文格式指南过短")
	}
}

func TestBrowserRuntimeRail_RuntimeHealth集成(t *testing.T) {
	runtime := newTestRuntime()
	_ = NewBrowserRuntimeRail(runtime)

	// 验证 runtime 和 service 连通
	health := runtime.RuntimeHealth()
	if health["provider"] != "openai" {
		t.Errorf("期望 provider=openai, 得到 %v", health["provider"])
	}
}

func TestRailOrEmpty(t *testing.T) {
	if railOrEmpty("") != "(empty)" {
		t.Error("空字符串应返回 (empty)")
	}
	if railOrEmpty("hello") != "hello" {
		t.Error("非空字符串应原样返回")
	}
}

func TestBrowserProgressTagRE(t *testing.T) {
	// 验证正则匹配
	tests := []struct {
		input   string
		matches bool
		jsonStr string
	}{
		{
			`<browser_progress>{"status":"completed"}</browser_progress>`,
			true,
			`{"status":"completed"}`,
		},
		{
			`<browser_progress> {"status":"partial"} </browser_progress>`,
			true,
			`{"status":"partial"}`,
		},
		{
			`no tag here`,
			false,
			"",
		},
	}
	for _, tt := range tests {
		match := browserProgressTagRE.FindStringSubmatch(tt.input)
		if tt.matches {
			if match == nil {
				t.Errorf("期望匹配: %s", tt.input)
			} else if match[1] != tt.jsonStr {
				t.Errorf("期望 JSON=%q, 得到 %q", tt.jsonStr, match[1])
			}
		} else {
			if match != nil {
				t.Errorf("不期望匹配: %s", tt.input)
			}
		}
	}
}

func TestNewBrowserRuntimeRail_带MCPCfg(t *testing.T) {
	mcpCfg := &mcptypes.McpServerConfig{
		ServerID:   "pw-test",
		ServerName: "playwright",
	}
	runtime := NewBrowserAgentRuntime(
		"openai", "key", "https://api.example.com", "gpt-4",
		mcpCfg,
		&BrowserRunGuardrails{MaxSteps: 10},
	)
	rail := NewBrowserRuntimeRail(runtime)
	if rail.Runtime().Service().MCPCfg != mcpCfg {
		t.Error("MCPCfg 应一致")
	}
}
