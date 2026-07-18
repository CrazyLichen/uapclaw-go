package browser_move

import (
	"context"
	"fmt"
	"testing"

	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewBrowserAgentRuntime(t *testing.T) {
	runtime := NewBrowserAgentRuntime(
		"openai", "test-key", "https://api.example.com", "gpt-4",
		&mcptypes.McpServerConfig{ServerID: "pw-test", ServerName: "playwright"},
		&BrowserRunGuardrails{MaxSteps: 10, MaxFailures: 5, TimeoutS: 120, RetryOnce: true},
	)
	if runtime == nil {
		t.Fatal("期望非 nil runtime")
	}
	if runtime.Service() == nil {
		t.Fatal("期望非 nil service")
	}
	if runtime.Service().Provider != "openai" {
		t.Errorf("期望 Provider=openai, 得到 %s", runtime.Service().Provider)
	}
	if runtime.Service().ModelName != "gpt-4" {
		t.Errorf("期望 ModelName=gpt-4, 得到 %s", runtime.Service().ModelName)
	}
}

func TestNewBrowserAgentRuntime_带取消存储(t *testing.T) {
	runtime := NewBrowserAgentRuntime(
		"openai", "key", "https://api.example.com", "model",
		&mcptypes.McpServerConfig{ServerID: "pw"},
		&BrowserRunGuardrails{},
	)
	if runtime == nil {
		t.Fatal("期望非 nil runtime")
	}
}

func TestBrowserAgentRuntime_Service(t *testing.T) {
	runtime := newTestRuntime()
	if runtime.Service() == nil {
		t.Fatal("期望非 nil service")
	}
}

func TestBrowserAgentRuntime_CodeExecutor(t *testing.T) {
	runtime := newTestRuntime()
	if runtime.CodeExecutor() != nil {
		t.Error("初始 CodeExecutor 应为 nil")
	}

	executor := func(ctx context.Context, jsCode string) (any, error) {
		return nil, nil
	}
	runtime.SetCodeExecutor(executor)
	if runtime.CodeExecutor() == nil {
		t.Error("设置后 CodeExecutor 不应为 nil")
	}
}

func TestBrowserAgentRuntime_CancelRun(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	result := runtime.CancelRun(ctx, "session-1", "req-1")
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
	if result["session_id"] != "session-1" {
		t.Errorf("期望 session_id=session-1, 得到 %v", result["session_id"])
	}
	if result["request_id"] != "req-1" {
		t.Errorf("期望 request_id=req-1, 得到 %v", result["request_id"])
	}
}

func TestBrowserAgentRuntime_ClearCancel(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	result := runtime.ClearCancel(ctx, "session-1", "req-1")
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
}

func TestBrowserAgentRuntime_EnsureRuntimeReady(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	err := runtime.EnsureRuntimeReady(ctx)
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
}

func TestBrowserAgentRuntime_EnsureStarted(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	err := runtime.EnsureStarted(ctx)
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
}

func TestBrowserAgentRuntime_RunBrowserTask_服务未完全启动(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	result, err := runtime.RunBrowserTask(ctx, "test task", "", "", nil)
	// RunBrowserTask 调用 EnsureStarted → service.RunTask → runTaskOnceWithTimeout
	// 因为 browserAgent 为 nil，runTaskOnceWithTimeout 返回 error
	// 但 service.RunTask 会先 SessionNew 等，最终 runTaskOnceWithTimeout 报错
	// 两种情况都可能：返回 error 或返回包含 error 的 map
	if err != nil {
		// 预期路径：runTaskOnceWithTimeout 返回 error
		return
	}
	if result != nil && result["ok"] == false {
		// 另一种路径：返回失败结果
		return
	}
	t.Error("期望返回错误或失败结果（服务未完全启动）")
}

func TestBrowserAgentRuntime_RunCustomAction_控制器未初始化(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	result := runtime.RunCustomAction(ctx, "click", "session-1", "req-1", nil)
	if result["ok"] != false {
		t.Errorf("期望 ok=false, 得到 %v", result["ok"])
	}
	if result["error"] != "controller_not_initialized" {
		t.Errorf("期望 controller_not_initialized, 得到 %v", result["error"])
	}
}

func TestBrowserAgentRuntime_ProbeInteractives_执行器未就绪(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	result := runtime.ProbeInteractives(ctx, 50, true, "")
	if result["ok"] != false {
		t.Errorf("期望 ok=false, 得到 %v", result["ok"])
	}
	if result["error"] != "browser_code_executor_not_ready" {
		t.Errorf("期望 browser_code_executor_not_ready, 得到 %v", result["error"])
	}
}

func TestBrowserAgentRuntime_ProbeInteractives_执行器就绪_返回有效JSON(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	// 设置 mock code executor
	runtime.SetCodeExecutor(func(_ context.Context, _ string) (any, error) {
		return `{"ok": true, "elements": [{"type": "button", "text": "Submit"}]}`, nil
	})

	result := runtime.ProbeInteractives(ctx, 50, true, "")
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
	elems, ok := result["elements"].([]any)
	if !ok {
		t.Errorf("期望 elements 为 []any, 得到 %T", result["elements"])
	} else if len(elems) != 1 {
		t.Errorf("期望 1 个 element, 得到 %d", len(elems))
	}
}

func TestBrowserAgentRuntime_ProbeInteractives_执行器返回无效JSON(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	runtime.SetCodeExecutor(func(_ context.Context, _ string) (any, error) {
		return "not json at all", nil
	})

	result := runtime.ProbeInteractives(ctx, 50, true, "")
	if result["ok"] != false {
		t.Errorf("期望 ok=false, 得到 %v", result["ok"])
	}
	if result["error"] != "Could not parse browser_probe_interactives result JSON" {
		t.Errorf("期望解析失败消息, 得到 %v", result["error"])
	}
}

func TestBrowserAgentRuntime_ProbeInteractives_执行器报错(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	runtime.SetCodeExecutor(func(_ context.Context, _ string) (any, error) {
		return nil, fmt.Errorf("playwright not connected")
	})

	result := runtime.ProbeInteractives(ctx, 50, true, "")
	if result["ok"] != false {
		t.Errorf("期望 ok=false, 得到 %v", result["ok"])
	}
}

func TestBrowserAgentRuntime_ProbeCards_执行器未就绪(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	result := runtime.ProbeCards(ctx, 20, true, true, "")
	if result["ok"] != false {
		t.Errorf("期望 ok=false, 得到 %v", result["ok"])
	}
	if result["error"] != "browser_code_executor_not_ready" {
		t.Errorf("期望 browser_code_executor_not_ready, 得到 %v", result["error"])
	}
}

func TestBrowserAgentRuntime_ProbeCards_执行器就绪_返回有效JSON(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	runtime.SetCodeExecutor(func(_ context.Context, _ string) (any, error) {
		return `{"ok": true, "cards": [{"title": "Card 1"}]}`, nil
	})

	result := runtime.ProbeCards(ctx, 20, true, true, "search")
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
	cards, ok := result["cards"].([]any)
	if !ok {
		t.Errorf("期望 cards 为 []any, 得到 %T", result["cards"])
	} else if len(cards) != 1 {
		t.Errorf("期望 1 张 card, 得到 %d", len(cards))
	}
}

func TestBrowserAgentRuntime_ProbeCards_执行器返回无效JSON(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	runtime.SetCodeExecutor(func(_ context.Context, _ string) (any, error) {
		return "invalid response", nil
	})

	result := runtime.ProbeCards(ctx, 20, true, true, "")
	if result["ok"] != false {
		t.Errorf("期望 ok=false, 得到 %v", result["ok"])
	}
}

func TestBrowserAgentRuntime_ListActions(t *testing.T) {
	runtime := newTestRuntime()

	result := runtime.ListActions()
	if result["ok"] != true {
		t.Errorf("期望 ok=true, 得到 %v", result["ok"])
	}
}

func TestBrowserAgentRuntime_RuntimeHealth(t *testing.T) {
	runtime := newTestRuntime()

	health := runtime.RuntimeHealth()
	if health["ok"] != false {
		t.Errorf("初始状态 ok 应为 false, 得到 %v", health["ok"])
	}
	if health["started"] != false {
		t.Errorf("初始状态 started 应为 false, 得到 %v", health["started"])
	}
	if health["provider"] != "openai" {
		t.Errorf("期望 provider=openai, 得到 %v", health["provider"])
	}
}

func TestBrowserAgentRuntime_Shutdown(t *testing.T) {
	runtime := newTestRuntime()
	ctx := context.Background()

	err := runtime.Shutdown(ctx)
	if err != nil {
		t.Errorf("期望 nil error, 得到 %v", err)
	}
}

func TestBrowserAgentRuntime_PlaywrightClientLookupKeys(t *testing.T) {
	runtime := newTestRuntime()

	keys := runtime.PlaywrightClientLookupKeys()
	if len(keys) == 0 {
		t.Fatal("期望至少 1 个候选键")
	}

	// 应包含 playwright 常见标识
	found := false
	for _, k := range keys {
		if k == "playwright" {
			found = true
			break
		}
	}
	if !found {
		t.Error("期望包含 'playwright' 候选键")
	}

	// 应去重
	seen := make(map[string]bool)
	for _, k := range keys {
		if seen[k] {
			t.Errorf("重复键: %s", k)
		}
		seen[k] = true
	}
}

func TestUnwrapMCPTextResult_Nil(t *testing.T) {
	result := unwrapMCPTextResult(nil)
	if result != nil {
		t.Errorf("期望 nil, 得到 %v", result)
	}
}

func TestUnwrapMCPTextResult_非字典(t *testing.T) {
	result := unwrapMCPTextResult("raw string")
	if result != "raw string" {
		t.Errorf("期望原样返回, 得到 %v", result)
	}
}

func TestUnwrapMCPTextResult_Content列表(t *testing.T) {
	input := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "text", "text": "world"},
		},
	}
	result := unwrapMCPTextResult(input)
	if result != "hello\nworld" {
		t.Errorf("期望 'hello\\nworld', 得到 %v", result)
	}
}

func TestUnwrapMCPTextResult_Result字段(t *testing.T) {
	input := map[string]any{"result": "value"}
	result := unwrapMCPTextResult(input)
	if result != "value" {
		t.Errorf("期望 'value', 得到 %v", result)
	}
}

func TestUnwrapMCPTextResult_Text字段(t *testing.T) {
	input := map[string]any{"text": "hello"}
	result := unwrapMCPTextResult(input)
	if result != "hello" {
		t.Errorf("期望 'hello', 得到 %v", result)
	}
}

func TestUnwrapMCPTextResult_Data字段(t *testing.T) {
	input := map[string]any{"data": "payload"}
	result := unwrapMCPTextResult(input)
	if result != "payload" {
		t.Errorf("期望 'payload', 得到 %v", result)
	}
}

func TestUnwrapMCPTextResult_未知字典(t *testing.T) {
	input := map[string]any{"foo": "bar"}
	result := unwrapMCPTextResult(input)
	m, ok := result.(map[string]any)
	if !ok {
		t.Errorf("期望原样返回 map, 得到 %T", result)
	} else if m["foo"] != "bar" {
		t.Errorf("期望 foo=bar, 得到 %v", m["foo"])
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestRuntime 创建测试用 BrowserAgentRuntime
func newTestRuntime() *BrowserAgentRuntime {
	return NewBrowserAgentRuntime(
		"openai", "test-key", "https://api.example.com", "gpt-4",
		&mcptypes.McpServerConfig{ServerID: "pw-test", ServerName: "playwright"},
		&BrowserRunGuardrails{MaxSteps: 10, MaxFailures: 5, TimeoutS: 120, RetryOnce: true},
	)
}
