package callback

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockLLMCallback 记录被调用的事件，用于测试回调触发
type mockLLMCallback struct {
	called int32
	last   *LLMCallEventData
}

func (m *mockLLMCallback) call(_ context.Context, data *LLMCallEventData) any {
	atomic.AddInt32(&m.called, 1)
	m.last = data
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestLLMCallEventType_值(t *testing.T) {
	tests := []struct {
		event    LLMCallEventType
		expected string
	}{
		{LLMCallStarted, "_framework:llm_call_started"},
		{LLMCallError, "_framework:llm_call_error"},
		{LLMResponseReceived, "_framework:llm_response_received"},
		{LLMInvokeInput, "_framework:llm_invoke_input"},
		{LLMInvokeOutput, "_framework:llm_invoke_output"},
		{LLMStreamInput, "_framework:llm_stream_input"},
		{LLMStreamOutput, "_framework:llm_stream_output"},
		{LLMInput, "_framework:llm_input"},
		{LLMOutput, "_framework:llm_output"},
	}
	for _, tt := range tests {
		if string(tt.event) != tt.expected {
			t.Errorf("事件类型 %s 期望 %s，实际 %s", tt.event, tt.expected, string(tt.event))
		}
	}
}

func TestCallbackFramework_注册和注销(t *testing.T) {
	fw := NewCallbackFramework()
	mock := &mockLLMCallback{}

	fw.OnLLM(LLMCallStarted, mock.call)
	fw.TriggerLLM(context.Background(), &LLMCallEventData{
		Event:         LLMCallStarted,
		ModelName:     "gpt-4",
		ModelProvider: "OpenAI",
	})

	if atomic.LoadInt32(&mock.called) != 1 {
		t.Errorf("期望回调被调用 1 次，实际 %d 次", mock.called)
	}
	if mock.last.ModelName != "gpt-4" {
		t.Errorf("期望 ModelName=gpt-4，实际 %s", mock.last.ModelName)
	}

	fw.OffLLM(LLMCallStarted, mock.call)
	fw.TriggerLLM(context.Background(), &LLMCallEventData{
		Event:     LLMCallStarted,
		ModelName: "gpt-3.5",
	})

	if atomic.LoadInt32(&mock.called) != 1 {
		t.Errorf("注销后期望回调不被调用，实际调用 %d 次", mock.called)
	}
}

func TestCallbackFramework_多个回调(t *testing.T) {
	fw := NewCallbackFramework()
	var callOrder []string

	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		callOrder = append(callOrder, "first")
		return nil
	})
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		callOrder = append(callOrder, "second")
		return nil
	})

	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})

	if len(callOrder) != 2 || callOrder[0] != "first" || callOrder[1] != "second" {
		t.Errorf("期望调用顺序 [first, second]，实际 %v", callOrder)
	}
}

func TestCallbackFramework_触发Nil数据(t *testing.T) {
	fw := NewCallbackFramework()
	mock := &mockLLMCallback{}
	fw.OnLLM(LLMCallStarted, mock.call)

	fw.TriggerLLM(context.Background(), nil)

	if atomic.LoadInt32(&mock.called) != 0 {
		t.Errorf("nil data 时期望回调不被调用，实际调用 %d 次", mock.called)
	}
}

func TestGetCallbackFramework_全局实例(t *testing.T) {
	fw1 := GetCallbackFramework()
	fw2 := GetCallbackFramework()
	if fw1 != fw2 {
		t.Error("GetCallbackFramework 应返回同一个全局实例")
	}
}

func TestNewCallbackFramework_日志回调(t *testing.T) {
	fw := NewCallbackFramework()

	events := []LLMCallEventType{
		LLMCallStarted, LLMCallError, LLMResponseReceived,
		LLMInvokeInput, LLMInvokeOutput,
		LLMStreamInput, LLMStreamOutput,
		LLMInput, LLMOutput,
	}

	for _, event := range events {
		callbacks := fw.GetCallbacksForTest(event)
		if len(callbacks) == 0 {
			t.Errorf("事件 %s 应该有默认日志回调", event)
		}
	}
}

func TestLLMCallEventData_字段(t *testing.T) {
	temp := 0.7
	topP := 0.9
	maxTokens := 100
	usage := llmschema.NewUsageMetadata()
	usage.InputTokens = 10
	usage.OutputTokens = 20

	data := &LLMCallEventData{
		Event:         LLMInvokeOutput,
		ModelName:     "gpt-4",
		ModelProvider: "OpenAI",
		Temperature:   &temp,
		TopP:          &topP,
		MaxTokens:     &maxTokens,
		IsStream:      false,
		Usage:         usage,
		Extra:         map[string]any{"key": "value"},
	}

	if data.Event != LLMInvokeOutput {
		t.Errorf("期望 Event=LLMInvokeOutput，实际 %s", data.Event)
	}
	if data.ModelName != "gpt-4" {
		t.Errorf("期望 ModelName=gpt-4，实际 %s", data.ModelName)
	}
	if *data.Temperature != 0.7 {
		t.Errorf("期望 Temperature=0.7，实际 %f", *data.Temperature)
	}
	if data.Usage.InputTokens != 10 {
		t.Errorf("期望 InputTokens=10，实际 %d", data.Usage.InputTokens)
	}
	if data.Extra["key"] != "value" {
		t.Errorf("期望 Extra[key]=value，实际 %v", data.Extra["key"])
	}
}

func TestLoggingLLMCallback_日志回调(t *testing.T) {
	temp := 0.5
	data := &LLMCallEventData{
		Event:         LLMCallStarted,
		ModelName:     "test-model",
		ModelProvider: "Test",
		Temperature:   &temp,
		IsStream:      false,
	}

	events := []LLMCallEventType{
		LLMCallStarted, LLMCallError, LLMResponseReceived,
		LLMInvokeInput, LLMInvokeOutput,
		LLMStreamInput, LLMStreamOutput, LLMInput, LLMOutput,
	}

	for _, event := range events {
		data.Event = event
		LoggingLLMCallback(context.Background(), data)
	}
}

func TestCallbackFramework_按指针注销(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fn := func(_ context.Context, _ *LLMCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	}

	fw.OnLLM(LLMCallStarted, fn)
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("期望调用 1 次，实际 %d 次", called)
	}

	fw.OffLLM(LLMCallStarted, fn)
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("注销后期望不再调用，实际调用 %d 次", called)
	}
}

func TestCallbackFramework_触发错误事件(t *testing.T) {
	fw := NewCallbackFramework()
	var receivedErr error

	fw.OnLLM(LLMCallError, func(_ context.Context, data *LLMCallEventData) any {
		receivedErr = data.Error
		return nil
	})

	testErr := fmt.Errorf("test error")
	fw.TriggerLLM(context.Background(), &LLMCallEventData{
		Event: LLMCallError,
		Error: testErr,
	})

	if receivedErr == nil || receivedErr.Error() != "test error" {
		t.Errorf("期望收到 test error，实际 %v", receivedErr)
	}
}

func TestCallbackFramework_触发Tool带结果(t *testing.T) {
	fw := NewCallbackFramework()

	fw.OnTool(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) any {
		return "result1"
	})
	fw.OnTool(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) any {
		return 42
	})

	data := NewToolCallEventData(ToolCallStarted, nil)
	results := fw.TriggerTool(context.Background(), data)

	if len(results) != 2 {
		t.Fatalf("期望 2 个返回值，实际 %d", len(results))
	}
	if results[0] != "result1" {
		t.Errorf("results[0] = %v, want result1", results[0])
	}
	if results[1] != 42 {
		t.Errorf("results[1] = %v, want 42", results[1])
	}
}

func TestCallbackFramework_触发LLM带结果(t *testing.T) {
	fw := NewCallbackFramework()

	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		return "llm_result"
	})

	results := fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})
	// 第一个是默认注册的 LoggingLLMCallback 返回 nil，第二个是自定义回调
	if len(results) < 2 {
		t.Fatalf("期望至少 2 个返回值，实际 %d", len(results))
	}
	lastResult := results[len(results)-1]
	if lastResult != "llm_result" {
		t.Errorf("最后一个返回值 = %v, want llm_result", lastResult)
	}
}

func TestCallbackFramework_触发Tool空上下文(t *testing.T) {
	fw := NewCallbackFramework()
	results := fw.TriggerTool(nil, NewToolCallEventData(ToolCallStarted, nil)) //nolint:staticcheck // SA1012: 专门测试 nil context 的防御逻辑
	if results != nil {
		t.Errorf("nil context 应返回 nil，实际 %v", results)
	}
}

// TestCallbackFramework_OnSession和TriggerSession 测试注册+触发 Session 回调
func TestCallbackFramework_OnSession和TriggerSession(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	var receivedData *SessionCallEventData

	fn := func(ctx context.Context, data *SessionCallEventData) any {
		called = true
		receivedData = data
		return "result"
	}

	fw.OnSession(AgentSessionCreated, fn)
	results := fw.TriggerSession(context.Background(), &SessionCallEventData{
		Event:     AgentSessionCreated,
		SessionID: "test-session",
	})

	if !called {
		t.Error("回调未被调用")
	}
	if receivedData.SessionID != "test-session" {
		t.Errorf("SessionID 期望 test-session，实际 %s", receivedData.SessionID)
	}
	if len(results) != 1 || results[0] != "result" {
		t.Errorf("结果期望 [result]，实际 %v", results)
	}
}

// TestCallbackFramework_OffSession 测试注销 Session 回调
func TestCallbackFramework_OffSession(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool

	fn := func(ctx context.Context, data *SessionCallEventData) any {
		called = true
		return nil
	}

	fw.OnSession(AgentSessionCreated, fn)
	fw.OffSession(AgentSessionCreated, fn)
	fw.TriggerSession(context.Background(), &SessionCallEventData{
		Event: AgentSessionCreated,
	})

	if called {
		t.Error("注销后回调不应被调用")
	}
}

// TestCallbackFramework_TriggerSession_无回调时返回空 测试无回调时返回空切片
func TestCallbackFramework_TriggerSession_无回调时返回空(t *testing.T) {
	fw := NewCallbackFramework()
	results := fw.TriggerSession(context.Background(), &SessionCallEventData{
		Event: AgentSessionCreated,
	})
	if len(results) != 0 {
		t.Errorf("无回调时期望空切片，实际 %v", results)
	}
}

// TestCallbackFramework_TriggerSession_Nil上下文 测试 nil context 或 nil data
func TestCallbackFramework_TriggerSession_Nil上下文(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	fw.OnSession(AgentSessionCreated, func(ctx context.Context, data *SessionCallEventData) any {
		called = true
		return nil
	})

	// 测试 nil context 场景，使用 //nolint 抑制 SA1012 警告
	results := fw.TriggerSession(nil, &SessionCallEventData{Event: AgentSessionCreated}) //nolint:staticcheck // 测试 nil context 行为
	if results != nil {
		t.Errorf("nil context 期望 nil，实际 %v", results)
	}

	// nil data
	results = fw.TriggerSession(context.Background(), nil)
	if results != nil {
		t.Errorf("nil data 期望 nil，实际 %v", results)
	}

	if called {
		t.Error("nil 参数时回调不应被调用")
	}
}

// TestCallbackFramework_Session事件与LLMTool隔离 测试 Session 回调不影响 LLM/Tool
func TestCallbackFramework_Session事件与LLMTool隔离(t *testing.T) {
	fw := NewCallbackFramework()
	var llmCalled, toolCalled bool

	fw.OnSession(AgentSessionCreated, func(ctx context.Context, data *SessionCallEventData) any {
		return nil
	})
	fw.OnLLM(LLMCallStarted, func(ctx context.Context, data *LLMCallEventData) any {
		llmCalled = true
		return nil
	})
	fw.OnTool(ToolCallStarted, func(ctx context.Context, data *ToolCallEventData) any {
		toolCalled = true
		return nil
	})

	// 触发 Session 事件，不应触发 LLM/Tool 回调
	fw.TriggerSession(context.Background(), &SessionCallEventData{Event: AgentSessionCreated})
	if llmCalled {
		t.Error("Session 事件不应触发 LLM 回调")
	}
	if toolCalled {
		t.Error("Session 事件不应触发 Tool 回调")
	}
}
