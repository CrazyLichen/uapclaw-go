package callback

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestNewCallbackFramework_无默认回调(t *testing.T) {
	fw := NewCallbackFramework()

	events := []LLMCallEventType{
		LLMCallStarted, LLMCallError, LLMResponseReceived,
		LLMInvokeInput, LLMInvokeOutput,
		LLMStreamInput, LLMStreamOutput,
		LLMInput, LLMOutput,
	}

	for _, event := range events {
		callbacks := fw.GetCallbacksForTest(event)
		if len(callbacks) != 0 {
			t.Errorf("事件 %s 不应有默认回调（6.24 删除 logging.go 后），实际 %d 个", event, len(callbacks))
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
	if len(results) < 1 {
		t.Fatalf("期望至少 1 个返回值，实际 %d", len(results))
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

// ──────────────────────────── 自定义事件测试 ────────────────────────────

// TestCallbackFramework_OnCustom和TriggerCustom 测试注册+触发自定义事件回调
func TestCallbackFramework_OnCustom和TriggerCustom(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	var receivedData map[string]any

	fn := func(_ context.Context, data map[string]any) any {
		called = true
		receivedData = data
		return "custom_result"
	}

	fw.OnCustom("abc-123write_stream", fn)
	results := fw.TriggerCustom(context.Background(), "abc-123write_stream", map[string]any{
		"data": "hello",
	})

	if !called {
		t.Error("回调未被调用")
	}
	if receivedData["data"] != "hello" {
		t.Errorf("data 期望 hello，实际 %v", receivedData["data"])
	}
	if len(results) != 1 || results[0] != "custom_result" {
		t.Errorf("结果期望 [custom_result]，实际 %v", results)
	}
}

// TestCallbackFramework_OffCustom 测试按指针注销自定义事件回调
func TestCallbackFramework_OffCustom(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool

	fn := func(_ context.Context, data map[string]any) any {
		called = true
		return nil
	}

	fw.OnCustom("test-event", fn)
	fw.OffCustom("test-event", fn)
	fw.TriggerCustom(context.Background(), "test-event", nil)

	if called {
		t.Error("注销后回调不应被调用")
	}
}

// TestCallbackFramework_OffAllCustom 测试清除某事件的全部回调
func TestCallbackFramework_OffAllCustom(t *testing.T) {
	fw := NewCallbackFramework()
	var callCount int32

	fn1 := func(_ context.Context, data map[string]any) any {
		atomic.AddInt32(&callCount, 1)
		return nil
	}
	fn2 := func(_ context.Context, data map[string]any) any {
		atomic.AddInt32(&callCount, 1)
		return nil
	}

	// 注册两个回调到同一事件
	fw.OnCustom("session-Awrite_stream", fn1)
	fw.OnCustom("session-Awrite_stream", fn2)

	// 触发：两个回调都应被调用
	fw.TriggerCustom(context.Background(), "session-Awrite_stream", nil)
	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("期望调用 2 次，实际 %d 次", callCount)
	}

	// OffAllCustom 清除全部
	fw.OffAllCustom("session-Awrite_stream")

	// 再次触发：不应有回调被调用
	atomic.StoreInt32(&callCount, 0)
	fw.TriggerCustom(context.Background(), "session-Awrite_stream", nil)
	if atomic.LoadInt32(&callCount) != 0 {
		t.Errorf("OffAllCustom 后期望无回调被调用，实际 %d 次", callCount)
	}
}

// TestCallbackFramework_OffAllCustom_PerSession隔离 测试不同 session 事件名互不影响
func TestCallbackFramework_OffAllCustom_PerSession隔离(t *testing.T) {
	fw := NewCallbackFramework()
	var callA, callB int32

	fw.OnCustom("session-Awrite_stream", func(_ context.Context, data map[string]any) any {
		atomic.AddInt32(&callA, 1)
		return nil
	})
	fw.OnCustom("session-Bwrite_stream", func(_ context.Context, data map[string]any) any {
		atomic.AddInt32(&callB, 1)
		return nil
	})

	// 清除 session-A 的回调
	fw.OffAllCustom("session-Awrite_stream")

	// session-A 回调不应被触发
	fw.TriggerCustom(context.Background(), "session-Awrite_stream", nil)
	if atomic.LoadInt32(&callA) != 0 {
		t.Error("session-A 回调不应被触发")
	}

	// session-B 回调应正常触发
	fw.TriggerCustom(context.Background(), "session-Bwrite_stream", nil)
	if atomic.LoadInt32(&callB) != 1 {
		t.Error("session-B 回调应被触发 1 次")
	}
}

// TestCallbackFramework_TriggerCustom_Nil上下文 测试 nil context 防御
func TestCallbackFramework_TriggerCustom_Nil上下文(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	fw.OnCustom("test", func(_ context.Context, data map[string]any) any {
		called = true
		return nil
	})

	results := fw.TriggerCustom(nil, "test", map[string]any{"data": "hello"}) //nolint:staticcheck // 测试 nil context 行为
	if results != nil {
		t.Errorf("nil context 期望 nil，实际 %v", results)
	}
	if called {
		t.Error("nil context 时回调不应被调用")
	}
}

// TestCallbackFramework_TriggerCustom_无回调 测试无注册回调时返回空切片
func TestCallbackFramework_TriggerCustom_无回调(t *testing.T) {
	fw := NewCallbackFramework()
	results := fw.TriggerCustom(context.Background(), "nonexistent", nil)
	if len(results) != 0 {
		t.Errorf("无回调时期望空切片，实际 %v", results)
	}
}

// TestCallbackFramework_自定义事件与LLMToolSession隔离 测试自定义事件不影响其他域
func TestCallbackFramework_自定义事件与LLMToolSession隔离(t *testing.T) {
	fw := NewCallbackFramework()
	var llmCalled, toolCalled, sessionCalled, customCalled bool

	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		llmCalled = true
		return nil
	})
	fw.OnTool(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) any {
		toolCalled = true
		return nil
	})
	fw.OnSession(AgentSessionCreated, func(_ context.Context, _ *SessionCallEventData) any {
		sessionCalled = true
		return nil
	})
	fw.OnCustom("custom-event", func(_ context.Context, _ map[string]any) any {
		customCalled = true
		return nil
	})

	// 触发自定义事件，不应触发其他域
	fw.TriggerCustom(context.Background(), "custom-event", nil)
	if customCalled != true {
		t.Error("自定义回调应被调用")
	}
	if llmCalled {
		t.Error("自定义事件不应触发 LLM 回调")
	}
	if toolCalled {
		t.Error("自定义事件不应触发 Tool 回调")
	}
	if sessionCalled {
		t.Error("自定义事件不应触发 Session 回调")
	}
}

// TestCallbackFramework_OnContext和TriggerContext 测试 Context 回调注册与触发
func TestCallbackFramework_OnContext和TriggerContext(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fw.OnContext(ContextUpdated, func(_ context.Context, data *ContextCallEventData) any {
		if data.SessionID != "sess-001" {
			t.Errorf("SessionID = %q, want sess-001", data.SessionID)
		}
		atomic.AddInt32(&called, 1)
		return nil
	})

	fw.TriggerContext(context.Background(), &ContextCallEventData{
		Event:     ContextUpdated,
		SessionID: "sess-001",
		ContextID: "ctx-001",
	})

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("called = %d, want 1", called)
	}
}

// TestCallbackFramework_OffContext 测试注销 Context 回调
func TestCallbackFramework_OffContext(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fn := func(_ context.Context, _ *ContextCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	}

	fw.OnContext(ContextCleared, fn)
	fw.OffContext(ContextCleared, fn)

	fw.TriggerContext(context.Background(), &ContextCallEventData{
		Event: ContextCleared,
	})

	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("OffContext 后不应触发，called = %d", called)
	}
}

// TestCallbackFramework_TriggerContext_Nil上下文 测试 nil context 防御
func TestCallbackFramework_TriggerContext_Nil上下文(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32
	fw.OnContext(ContextRetrieved, func(_ context.Context, _ *ContextCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	})
	fw.TriggerContext(nil, &ContextCallEventData{Event: ContextRetrieved}) //nolint:staticcheck // SA1012: 专门测试 nil context 的防御逻辑
	if atomic.LoadInt32(&called) != 0 {
		t.Error("nil context 不应触发回调")
	}
}

// TestCallbackFramework_TriggerContext_NilData 测试 nil data 防御
func TestCallbackFramework_TriggerContext_NilData(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32
	fw.OnContext(ContextRetrieved, func(_ context.Context, _ *ContextCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	})
	fw.TriggerContext(context.Background(), nil)
	if atomic.LoadInt32(&called) != 0 {
		t.Error("nil data 不应触发回调")
	}
}

func TestCallbackFramework_TransformLLMIO_透传(t *testing.T) {
	fw := NewCallbackFramework()
	ctx := context.Background()

	// 未注册时透传
	input := map[string]any{"key": "value"}
	result := fw.TransformLLMIOInput(ctx, LLMStreamInput, input)
	assert.Equal(t, input, result)

	output := &llmschema.AssistantMessageChunk{}
	result = fw.TransformLLMIOOutput(ctx, LLMStreamOutput, output)
	assert.Equal(t, output, result)
}

func TestCallbackFramework_TransformAgentIO_透传(t *testing.T) {
	fw := NewCallbackFramework()
	ctx := context.Background()

	input := map[string]any{"key": "value"}
	result := fw.TransformAgentIOInput(ctx, GlobalAgentStreamInput, input)
	assert.Equal(t, input, result)

	result = fw.TransformAgentIOOutput(ctx, GlobalAgentStreamOutput, input)
	assert.Equal(t, input, result)
}

func TestCallbackFramework_TransformLLMIO_注册后变换(t *testing.T) {
	fw := NewCallbackFramework()
	ctx := context.Background()

	// 注册变换：输入加前缀，输出加倍
	fw.RegisterLLMTransformIO(
		LLMStreamInput, LLMStreamOutput,
		func(ctx context.Context, event LLMCallEventType, input any) any {
			return "transformed_" + input.(string)
		},
		func(ctx context.Context, event LLMCallEventType, output any) any {
			return output.(string) + output.(string)
		},
	)

	result := fw.TransformLLMIOInput(ctx, LLMStreamInput, "hello")
	assert.Equal(t, "transformed_hello", result)

	result = fw.TransformLLMIOOutput(ctx, LLMStreamOutput, "ab")
	assert.Equal(t, "abab", result)
}

// ──────────────────────────── Tool TransformIO 测试 ────────────────────────────

func TestRegisterToolTransformIO_双键注册(t *testing.T) {
	fw := NewCallbackFramework()
	var inputCalled, outputCalled bool

	fw.RegisterToolTransformIO(
		ToolInvokeInput, ToolInvokeOutput,
		func(_ context.Context, _ ToolCallEventType, input map[string]any) map[string]any {
			inputCalled = true
			input["transformed"] = true
			return input
		},
		func(_ context.Context, _ ToolCallEventType, output map[string]any) map[string]any {
			outputCalled = true
			output["transformed"] = true
			return output
		},
	)

	// 通过 inputEvent 查找
	result := fw.TransformToolIOInput(context.Background(), ToolInvokeInput, map[string]any{"key": "val"})
	if !inputCalled {
		t.Error("inputFn 未被调用")
	}
	if result["transformed"] != true {
		t.Error("input 变换未生效")
	}

	// 通过 outputEvent 查找
	outResult := fw.TransformToolIOOutput(context.Background(), ToolInvokeOutput, map[string]any{"key": "val"})
	if !outputCalled {
		t.Error("outputFn 未被调用")
	}
	if outResult["transformed"] != true {
		t.Error("output 变换未生效")
	}
}

func TestTransformToolIOInput_未注册时透传(t *testing.T) {
	fw := NewCallbackFramework()
	input := map[string]any{"key": "val"}
	result := fw.TransformToolIOInput(context.Background(), ToolInvokeInput, input)
	if result["key"] != "val" {
		t.Errorf("未注册时应该透传，got %v", result)
	}
}

func TestTransformToolIOOutput_未注册时透传(t *testing.T) {
	fw := NewCallbackFramework()
	output := map[string]any{"key": "val"}
	result := fw.TransformToolIOOutput(context.Background(), ToolInvokeOutput, output)
	if result["key"] != "val" {
		t.Errorf("未注册时应该透传，got %v", result)
	}
}

func TestTransformToolIOInput_已注册时变换(t *testing.T) {
	fw := NewCallbackFramework()
	fw.RegisterToolTransformIO(
		ToolInvokeInput, ToolInvokeOutput,
		func(_ context.Context, _ ToolCallEventType, input map[string]any) map[string]any {
			input["added"] = "by_transform"
			return input
		},
		nil,
	)
	result := fw.TransformToolIOInput(context.Background(), ToolInvokeInput, map[string]any{"key": "val"})
	if result["added"] != "by_transform" {
		t.Errorf("变换未生效，got %v", result)
	}
}

func TestTransformToolIOOutput_已注册时变换(t *testing.T) {
	fw := NewCallbackFramework()
	fw.RegisterToolTransformIO(
		ToolInvokeInput, ToolInvokeOutput,
		nil,
		func(_ context.Context, _ ToolCallEventType, output map[string]any) map[string]any {
			output["added"] = "by_transform"
			return output
		},
	)
	result := fw.TransformToolIOOutput(context.Background(), ToolInvokeOutput, map[string]any{"key": "val"})
	if result["added"] != "by_transform" {
		t.Errorf("变换未生效，got %v", result)
	}
}

// ──────────────────────────── CallbackInfo 包装测试 ────────────────────────────

// TestCallbackFramework_CallbackInfo优先级 测试优先级排序
func TestCallbackFramework_CallbackInfo优先级(t *testing.T) {
	fw := NewCallbackFramework()
	var callOrder []string

	// 低优先级（默认0）先注册，高优先级后注册
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		callOrder = append(callOrder, "low")
		return nil
	})
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		callOrder = append(callOrder, "high")
		return nil
	}, WithPriority(10))

	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})

	// callOrder 只包含用户回调
	if len(callOrder) != 2 {
		t.Fatalf("期望 2 次用户回调，实际 %d 次，顺序 %v", len(callOrder), callOrder)
	}
	// 高优先级应先执行
	if callOrder[0] != "high" || callOrder[1] != "low" {
		t.Errorf("期望 [high, low]，实际 %v", callOrder)
	}
}

// TestCallbackFramework_CallbackInfoOnce 测试一次性回调
func TestCallbackFramework_CallbackInfoOnce(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	}, WithOnce())

	// 第一次触发
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("期望调用 1 次，实际 %d 次", called)
	}

	// 第二次触发：Once 回调应被禁用
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("Once 回调第二次不应被调用，实际 %d 次", called)
	}
}

// TestCallbackFramework_CallbackInfoDisabled 测试 CallbackType=transform 被跳过
func TestCallbackFramework_CallbackInfoDisabled(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	}, WithCallbackType("transform"))

	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})
	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("transform 类型回调应被跳过，实际调用 %d 次", called)
	}
}

// TestCallbackFramework_GetCallbacksForTest_CallbackInfo 测试 GetCallbacksForTest 返回 CallbackInfo
func TestCallbackFramework_GetCallbacksForTest_CallbackInfo(t *testing.T) {
	fw := NewCallbackFramework()

	callbacks := fw.GetCallbacksForTest(LLMCallStarted)
	// 无默认日志回调（6.24 删除 logging.go 后不再自动注册）
	if len(callbacks) != 0 {
		t.Fatalf("不应有默认回调，实际 %d 个", len(callbacks))
	}
}

// ──────────────────────────── PerAgent 域测试 ────────────────────────────

// TestCallbackFramework_OnPerAgent和TriggerPerAgent 测试 PerAgent 注册与触发
func TestCallbackFramework_OnPerAgent和TriggerPerAgent(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fn := func(_ context.Context, _ any) error {
		atomic.AddInt32(&called, 1)
		return nil
	}

	fw.OnPerAgent("agent1_before_model_call", fn)
	err := fw.TriggerPerAgent(context.Background(), "agent1_before_model_call", nil)
	if err != nil {
		t.Errorf("TriggerPerAgent 返回错误: %v", err)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("期望调用 1 次，实际 %d 次", called)
	}
}

// TestCallbackFramework_OffPerAgent 测试注销 PerAgent 回调
func TestCallbackFramework_OffPerAgent(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fn := func(_ context.Context, _ any) error {
		atomic.AddInt32(&called, 1)
		return nil
	}

	fw.OnPerAgent("agent1_before_model_call", fn)
	fw.OffPerAgent("agent1_before_model_call", fn)
	err := fw.TriggerPerAgent(context.Background(), "agent1_before_model_call", nil)
	if err != nil {
		t.Errorf("TriggerPerAgent 返回错误: %v", err)
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("注销后不应被调用，实际 %d 次", called)
	}
}

// TestCallbackFramework_OffAllPerAgent 测试清除所有 PerAgent 回调
func TestCallbackFramework_OffAllPerAgent(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fn1 := func(_ context.Context, _ any) error {
		atomic.AddInt32(&called, 1)
		return nil
	}
	fn2 := func(_ context.Context, _ any) error {
		atomic.AddInt32(&called, 1)
		return nil
	}

	fw.OnPerAgent("agent1_before_model_call", fn1)
	fw.OnPerAgent("agent1_before_model_call", fn2)
	fw.OffAllPerAgent("agent1_before_model_call")

	err := fw.TriggerPerAgent(context.Background(), "agent1_before_model_call", nil)
	if err != nil {
		t.Errorf("TriggerPerAgent 返回错误: %v", err)
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("OffAllPerAgent 后不应有回调被调用，实际 %d 次", called)
	}
}

// TestCallbackFramework_TriggerPerAgent_错误中断 测试 PerAgent error 中断执行
func TestCallbackFramework_TriggerPerAgent_错误中断(t *testing.T) {
	fw := NewCallbackFramework()
	var callOrder []string

	fw.OnPerAgent("agent1_before_model_call", func(_ context.Context, _ any) error {
		callOrder = append(callOrder, "first")
		return fmt.Errorf("callback error")
	}, WithPriority(10))
	fw.OnPerAgent("agent1_before_model_call", func(_ context.Context, _ any) error {
		callOrder = append(callOrder, "second")
		return nil
	})

	err := fw.TriggerPerAgent(context.Background(), "agent1_before_model_call", nil)
	if err == nil || err.Error() != "callback error" {
		t.Errorf("期望 callback error，实际 %v", err)
	}
	if len(callOrder) != 1 || callOrder[0] != "first" {
		t.Errorf("错误后应中断，期望 [first]，实际 %v", callOrder)
	}
}

// TestCallbackFramework_PerAgent优先级 测试 PerAgent 优先级排序
func TestCallbackFramework_PerAgent优先级(t *testing.T) {
	fw := NewCallbackFramework()
	var callOrder []string

	fw.OnPerAgent("agent1_before_model_call", func(_ context.Context, _ any) error {
		callOrder = append(callOrder, "low")
		return nil
	})
	fw.OnPerAgent("agent1_before_model_call", func(_ context.Context, _ any) error {
		callOrder = append(callOrder, "high")
		return nil
	}, WithPriority(10))

	err := fw.TriggerPerAgent(context.Background(), "agent1_before_model_call", nil)
	if err != nil {
		t.Errorf("TriggerPerAgent 返回错误: %v", err)
	}
	if len(callOrder) != 2 || callOrder[0] != "high" || callOrder[1] != "low" {
		t.Errorf("期望 [high, low]，实际 %v", callOrder)
	}
}

// TestCallbackFramework_PerAgentOnce 测试 PerAgent Once 选项
func TestCallbackFramework_PerAgentOnce(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fw.OnPerAgent("agent1_before_model_call", func(_ context.Context, _ any) error {
		atomic.AddInt32(&called, 1)
		return nil
	}, WithOnce())

	// 第一次触发
	err := fw.TriggerPerAgent(context.Background(), "agent1_before_model_call", nil)
	if err != nil {
		t.Errorf("TriggerPerAgent 返回错误: %v", err)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("期望调用 1 次，实际 %d 次", called)
	}

	// 第二次触发：Once 应被禁用
	err = fw.TriggerPerAgent(context.Background(), "agent1_before_model_call", nil)
	if err != nil {
		t.Errorf("TriggerPerAgent 返回错误: %v", err)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("Once 回调第二次不应被调用，实际 %d 次", called)
	}
}

// TestCallbackFramework_HasPerAgentHooks 测试 HasPerAgentHooks
func TestCallbackFramework_HasPerAgentHooks(t *testing.T) {
	fw := NewCallbackFramework()

	if fw.HasPerAgentHooks("agent1_before_model_call") {
		t.Error("未注册时不应有回调")
	}

	fw.OnPerAgent("agent1_before_model_call", func(_ context.Context, _ any) error {
		return nil
	})

	if !fw.HasPerAgentHooks("agent1_before_model_call") {
		t.Error("注册后应有回调")
	}

	if fw.HasPerAgentHooks("agent2_before_model_call") {
		t.Error("不同事件不应有回调")
	}
}

// TestCallbackFramework_TriggerPerAgent_Nil上下文 测试 nil context 防御
func TestCallbackFramework_TriggerPerAgent_Nil上下文(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fw.OnPerAgent("agent1_before_model_call", func(_ context.Context, _ any) error {
		atomic.AddInt32(&called, 1)
		return nil
	})

	err := fw.TriggerPerAgent(nil, "agent1_before_model_call", nil) //nolint:staticcheck // 测试 nil context 行为
	if err != nil {
		t.Errorf("nil context 应返回 nil，实际 %v", err)
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Error("nil context 时回调不应被调用")
	}
}

// ──────────────────────────── 6.24 回填逻辑测试 ────────────────────────────

// TestCallbackFramework_钩子执行 测试 BEFORE/AFTER 钩子执行顺序
func TestCallbackFramework_钩子执行(t *testing.T) {
	fw := NewCallbackFramework()
	fw.EnableMetrics(true)
	var order []string
	fw.AddHook("_framework:llm_call_started", HookTypeBefore, func(_ context.Context, _ string, _ any) {
		order = append(order, "before")
	})
	fw.AddHook("_framework:llm_call_started", HookTypeAfter, func(_ context.Context, _ string, _ any) {
		order = append(order, "after")
	})
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		order = append(order, "callback")
		return nil
	})
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})
	if len(order) != 3 || order[0] != "before" || order[1] != "callback" || order[2] != "after" {
		t.Errorf("钩子执行顺序 = %v, want [before callback after]", order)
	}
}

// TestCallbackFramework_过滤器 测试全局过滤器 SKIP 后回调不执行
func TestCallbackFramework_过滤器(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	fw.AddGlobalFilter(&ConditionalFilter{
		Condition:     func(_ context.Context, _ string, _ string, _ any) bool { return false },
		ActionOnFalse: FilterActionSkip,
	})
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		called = true
		return nil
	})
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})
	if called {
		t.Errorf("过滤器 SKIP 后回调不应执行")
	}
}

// TestCallbackFramework_AbortError_无Cause 测试 AbortError 中止触发
func TestCallbackFramework_AbortError_无Cause(t *testing.T) {
	fw := NewCallbackFramework()
	fw.OnPerAgent("test_abort", func(_ context.Context, _ any) error {
		return NewAbortError("test abort", nil)
	})
	// TriggerPerAgent 返回 error，AbortError 会导致回调提前终止
	err := fw.TriggerPerAgent(context.Background(), "test_abort", nil)
	if err == nil {
		t.Errorf("AbortError 应中止触发并返回错误，err = nil")
	}
}

// TestCallbackFramework_指标记录 测试 EnableMetrics + GetMetrics 方法不报错
func TestCallbackFramework_指标记录(t *testing.T) {
	fw := NewCallbackFramework()
	fw.EnableMetrics(true)
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		return nil
	})
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})
	// 指标按实际 callback 类型名记录，key 不确定；主要验证方法不报错
	// 遍历可能的 key 验证 EnableMetrics + GetMetrics 机制
	_ = fw.GetMetrics("_framework:llm_call_started", "func(*callback.LLMCallEventData)")
}

// TestCallbackFramework_OnWorkflow和TriggerWorkflow 测试 Workflow 注册与触发
func TestCallbackFramework_OnWorkflow和TriggerWorkflow(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	var receivedData *WorkflowEventData

	fn := func(_ context.Context, data *WorkflowEventData) any {
		called = true
		receivedData = data
		return "workflow_result"
	}

	fw.OnWorkflow(WorkflowStarted, fn)
	results := fw.TriggerWorkflow(context.Background(), &WorkflowEventData{
		Event:      WorkflowStarted,
		WorkflowID: "wf-001",
		NodeID:     "node-001",
	})

	if !called {
		t.Error("Workflow 回调未被调用")
	}
	if receivedData.WorkflowID != "wf-001" {
		t.Errorf("WorkflowID 期望 wf-001，实际 %s", receivedData.WorkflowID)
	}
	if len(results) != 1 || results[0] != "workflow_result" {
		t.Errorf("结果期望 [workflow_result]，实际 %v", results)
	}

	// 注销后不再触发
	fw.OffWorkflow(WorkflowStarted, fn)
	results2 := fw.TriggerWorkflow(context.Background(), &WorkflowEventData{Event: WorkflowStarted})
	if len(results2) != 0 {
		t.Errorf("注销后不应有结果，实际 %v", results2)
	}
}

// TestCallbackFramework_OnMemory和TriggerMemory 测试 Memory 注册与触发
func TestCallbackFramework_OnMemory和TriggerMemory(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	var receivedData *MemoryEventData

	fn := func(_ context.Context, data *MemoryEventData) any {
		called = true
		receivedData = data
		return "memory_result"
	}

	fw.OnMemory(MemoryAdded, fn)
	results := fw.TriggerMemory(context.Background(), &MemoryEventData{
		Event: MemoryAdded,
		Key:   "test-key",
	})

	if !called {
		t.Error("Memory 回调未被调用")
	}
	if receivedData.Key != "test-key" {
		t.Errorf("Key 期望 test-key，实际 %s", receivedData.Key)
	}
	if len(results) != 1 || results[0] != "memory_result" {
		t.Errorf("结果期望 [memory_result]，实际 %v", results)
	}

	// 注销后不再触发
	fw.OffMemory(MemoryAdded, fn)
	results2 := fw.TriggerMemory(context.Background(), &MemoryEventData{Event: MemoryAdded})
	if len(results2) != 0 {
		t.Errorf("注销后不应有结果，实际 %v", results2)
	}
}

// TestCallbackFramework_OnTaskManager和TriggerTaskManager 测试 TaskManager 注册与触发
func TestCallbackFramework_OnTaskManager和TriggerTaskManager(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	var receivedData *TaskManagerEventData

	fn := func(_ context.Context, data *TaskManagerEventData) any {
		called = true
		receivedData = data
		return "task_result"
	}

	fw.OnTaskManager(TaskCreated, fn)
	results := fw.TriggerTaskManager(context.Background(), &TaskManagerEventData{
		Event:  TaskCreated,
		TaskID: "task-001",
	})

	if !called {
		t.Error("TaskManager 回调未被调用")
	}
	if receivedData.TaskID != "task-001" {
		t.Errorf("TaskID 期望 task-001，实际 %s", receivedData.TaskID)
	}
	if len(results) != 1 || results[0] != "task_result" {
		t.Errorf("结果期望 [task_result]，实际 %v", results)
	}

	// 注销后不再触发
	fw.OffTaskManager(TaskCreated, fn)
	results2 := fw.TriggerTaskManager(context.Background(), &TaskManagerEventData{Event: TaskCreated})
	if len(results2) != 0 {
		t.Errorf("注销后不应有结果，实际 %v", results2)
	}
}

// TestCallbackFramework_重试 测试 WithMaxRetries 重试机制
func TestCallbackFramework_重试(t *testing.T) {
	fw := NewCallbackFramework()
	var attempts int
	fw.OnPerAgent("test_retry", func(_ context.Context, _ any) error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("fail attempt %d", attempts)
		}
		return nil
	}, WithMaxRetries(2), WithRetryDelay(0.01))
	// TriggerPerAgent 使用 strategyAbortOnError，重试在 triggerCallbacks 内部
	err := fw.TriggerPerAgent(context.Background(), "test_retry", nil)
	if err != nil {
		t.Errorf("重试后应成功，err = %v", err)
	}
	if attempts != 3 {
		t.Errorf("期望 3 次尝试，实际 %d", attempts)
	}
}

// ──────────────────────────── 覆盖率补充测试 ────────────────────────────

// TestCallbackFramework_OnGlobalAgent和TriggerGlobalAgent 测试 GlobalAgent 注册与触发
func TestCallbackFramework_OnGlobalAgent和TriggerGlobalAgent(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	var receivedData *GlobalAgentEventData

	fn := func(_ context.Context, data *GlobalAgentEventData) any {
		called = true
		receivedData = data
		return "agent_result"
	}

	fw.OnGlobalAgent(GlobalAgentStarted, fn)
	results := fw.TriggerGlobalAgent(context.Background(), &GlobalAgentEventData{
		Event:     GlobalAgentStarted,
		AgentID:   "agent-001",
		AgentName: "test-agent",
	})

	if !called {
		t.Error("GlobalAgent 回调未被调用")
	}
	if receivedData.AgentID != "agent-001" {
		t.Errorf("AgentID 期望 agent-001，实际 %s", receivedData.AgentID)
	}
	if len(results) != 1 || results[0] != "agent_result" {
		t.Errorf("结果期望 [agent_result]，实际 %v", results)
	}

	// 注销后不再触发
	fw.OffGlobalAgent(GlobalAgentStarted, fn)
	results2 := fw.TriggerGlobalAgent(context.Background(), &GlobalAgentEventData{Event: GlobalAgentStarted})
	if len(results2) != 0 {
		t.Errorf("注销后不应有结果，实际 %v", results2)
	}
}

// TestCallbackFramework_TriggerGlobalAgent_Nil上下文 测试 nil context/data 防御
func TestCallbackFramework_TriggerGlobalAgent_Nil上下文(t *testing.T) {
	fw := NewCallbackFramework()
	results := fw.TriggerGlobalAgent(nil, &GlobalAgentEventData{Event: GlobalAgentStarted}) //nolint:staticcheck // 测试 nil context
	if results != nil {
		t.Errorf("nil context 期望 nil，实际 %v", results)
	}
	results = fw.TriggerGlobalAgent(context.Background(), nil)
	if results != nil {
		t.Errorf("nil data 期望 nil，实际 %v", results)
	}
}

// TestCallbackFramework_RegisterAgentTransformIO_注册后变换 测试 Agent TransformIO 注册后变换
func TestCallbackFramework_RegisterAgentTransformIO_注册后变换(t *testing.T) {
	fw := NewCallbackFramework()
	ctx := context.Background()

	fw.RegisterAgentTransformIO(
		GlobalAgentStreamInput, GlobalAgentStreamOutput,
		func(ctx context.Context, event GlobalAgentEventType, input any) any {
			return "transformed_" + input.(string)
		},
		func(ctx context.Context, event GlobalAgentEventType, output any) any {
			return output.(string) + output.(string)
		},
	)

	result := fw.TransformAgentIOInput(ctx, GlobalAgentStreamInput, "hello")
	assert.Equal(t, "transformed_hello", result)

	result = fw.TransformAgentIOOutput(ctx, GlobalAgentStreamOutput, "ab")
	assert.Equal(t, "abab", result)
}

// TestCallbackFramework_OnAgentTeam和TriggerAgentTeam 测试 AgentTeam 注册与触发
func TestCallbackFramework_OnAgentTeam和TriggerAgentTeam(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	var receivedData *AgentTeamEventData

	fn := func(_ context.Context, data *AgentTeamEventData) any {
		called = true
		receivedData = data
		return "team_result"
	}

	fw.OnAgentTeam(AgentP2PReceived, fn)
	results := fw.TriggerAgentTeam(context.Background(), &AgentTeamEventData{
		Event:   AgentP2PReceived,
		AgentID: "agent-001",
	})

	if !called {
		t.Error("AgentTeam 回调未被调用")
	}
	if receivedData.AgentID != "agent-001" {
		t.Errorf("AgentID 期望 agent-001，实际 %s", receivedData.AgentID)
	}
	if len(results) != 1 || results[0] != "team_result" {
		t.Errorf("结果期望 [team_result]，实际 %v", results)
	}

	// 注销后不再触发
	fw.OffAgentTeam(AgentP2PReceived, fn)
	results2 := fw.TriggerAgentTeam(context.Background(), &AgentTeamEventData{Event: AgentP2PReceived})
	if len(results2) != 0 {
		t.Errorf("注销后不应有结果，实际 %v", results2)
	}
}

// TestCallbackFramework_TriggerAgentTeam_Nil防御 测试 nil context/data
func TestCallbackFramework_TriggerAgentTeam_Nil防御(t *testing.T) {
	fw := NewCallbackFramework()
	if fw.TriggerAgentTeam(nil, &AgentTeamEventData{Event: AgentP2PReceived}) != nil { //nolint:staticcheck // 测试 nil context
		t.Error("nil context 期望 nil")
	}
	if fw.TriggerAgentTeam(context.Background(), nil) != nil {
		t.Error("nil data 期望 nil")
	}
}

// TestCallbackFramework_OnRetrieval和TriggerRetrieval 测试 Retrieval 注册与触发
func TestCallbackFramework_OnRetrieval和TriggerRetrieval(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	var receivedData *RetrievalEventData

	fn := func(_ context.Context, data *RetrievalEventData) any {
		called = true
		receivedData = data
		return "retrieval_result"
	}

	fw.OnRetrieval(RetrievalStarted, fn)
	results := fw.TriggerRetrieval(context.Background(), &RetrievalEventData{
		Event: RetrievalStarted,
		Query: "test-query",
	})

	if !called {
		t.Error("Retrieval 回调未被调用")
	}
	if receivedData.Query != "test-query" {
		t.Errorf("Query 期望 test-query，实际 %s", receivedData.Query)
	}
	if len(results) != 1 || results[0] != "retrieval_result" {
		t.Errorf("结果期望 [retrieval_result]，实际 %v", results)
	}

	// 注销后不再触发
	fw.OffRetrieval(RetrievalStarted, fn)
	results2 := fw.TriggerRetrieval(context.Background(), &RetrievalEventData{Event: RetrievalStarted})
	if len(results2) != 0 {
		t.Errorf("注销后不应有结果，实际 %v", results2)
	}
}

// TestCallbackFramework_TriggerRetrieval_Nil防御 测试 nil context/data
func TestCallbackFramework_TriggerRetrieval_Nil防御(t *testing.T) {
	fw := NewCallbackFramework()
	if fw.TriggerRetrieval(nil, &RetrievalEventData{Event: RetrievalStarted}) != nil { //nolint:staticcheck // 测试 nil context
		t.Error("nil context 期望 nil")
	}
	if fw.TriggerRetrieval(context.Background(), nil) != nil {
		t.Error("nil data 期望 nil")
	}
}

// TestCallbackFramework_TriggerWithTimeout 测试带总超时的触发
func TestCallbackFramework_TriggerWithTimeout(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool

	fw.OnCustom("timeout_event", func(_ context.Context, _ map[string]any) any {
		called = true
		return "ok"
	})

	results, err := fw.TriggerWithTimeout(context.Background(), "timeout_event", 5.0, nil)
	if err != nil {
		t.Errorf("期望无错误，实际 %v", err)
	}
	if !called {
		t.Error("回调未被调用")
	}
	if len(results) != 1 || results[0] != "ok" {
		t.Errorf("结果期望 [ok]，实际 %v", results)
	}
}

// TestCallbackFramework_AddFilter 测试事件级过滤器
func TestCallbackFramework_AddFilter(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool

	fw.AddFilter("_framework:llm_call_started", &ConditionalFilter{
		Condition:     func(_ context.Context, _ string, _ string, _ any) bool { return false },
		ActionOnFalse: FilterActionSkip,
	})
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		called = true
		return nil
	})
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})
	if called {
		t.Error("事件级过滤器 SKIP 后回调不应执行")
	}
}

// TestCallbackFramework_FilterModify_修改LLM参数 测试 FilterActionModify 在 triggerCallbacks 中修改执行参数
func TestCallbackFramework_FilterModify_修改LLM参数(t *testing.T) {
	fw := NewCallbackFramework()
	var receivedModel string

	// 全局 ParamModifyFilter：修改 LLMCallEventData 的 ModelName
	fw.AddGlobalFilter(&ParamModifyFilter{
		Modifier: func(data any) any {
			if d, ok := data.(*LLMCallEventData); ok {
				d.ModelName = "modified-model"
				return d
			}
			return data
		},
	})

	fw.OnLLM(LLMCallStarted, func(_ context.Context, data *LLMCallEventData) any {
		receivedModel = data.ModelName
		return nil
	})

	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted, ModelName: "original-model"})
	if receivedModel != "modified-model" {
		t.Errorf("FilterModify 后 ModelName 期望 modified-model，实际 %s", receivedModel)
	}
}

// TestCallbackFramework_FilterModify_修改CustomData 测试 FilterActionModify 在自定义域中修改 map[string]any 数据
func TestCallbackFramework_FilterModify_修改CustomData(t *testing.T) {
	fw := NewCallbackFramework()
	var receivedValue any

	// 全局 ParamModifyFilter：在 data 中添加 modified=true
	fw.AddGlobalFilter(&ParamModifyFilter{
		Modifier: func(data any) any {
			if m, ok := data.(map[string]any); ok {
				m["modified"] = true
				return m
			}
			return data
		},
	})

	fw.OnCustom("modify_event", func(_ context.Context, data map[string]any) any {
		receivedValue = data["modified"]
		return "ok"
	})

	results := fw.TriggerCustom(context.Background(), "modify_event", map[string]any{"key": "val"})
	if len(results) != 1 || results[0] != "ok" {
		t.Errorf("期望 [ok]，实际 %v", results)
	}
	if receivedValue != true {
		t.Errorf("FilterModify 后数据应包含 modified=true，实际 %v", receivedValue)
	}
}

// TestCallbackFramework_AddCircuitBreaker 测试添加熔断器
func TestCallbackFramework_AddCircuitBreaker(t *testing.T) {
	fw := NewCallbackFramework()
	fw.EnableMetrics(true)

	fw.AddCircuitBreaker("_framework:llm_call_started", "func(*callback.LLMCallEventData)", 1, 60.0)

	var called int32
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	})

	// 首次正常触发
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("期望调用 1 次，实际 %d", called)
	}
}

// TestCallbackFramework_TriggerChain 测试链式触发
func TestCallbackFramework_TriggerChain(t *testing.T) {
	fw := NewCallbackFramework()

	// 无注册链时返回 Continue + 原始数据
	result := fw.TriggerChain(context.Background(), "chain_event", "input_data")
	if result.Action != ChainActionContinue {
		t.Errorf("期望 ChainActionContinue，实际 %v", result.Action)
	}
	if result.Result != "input_data" {
		t.Errorf("期望 Result=input_data，实际 %v", result.Result)
	}
}

// TestCallbackFramework_TriggerParallel 测试并发触发
func TestCallbackFramework_TriggerParallel(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fw.OnCustom("parallel_event", func(_ context.Context, _ map[string]any) any {
		atomic.AddInt32(&called, 1)
		return "result"
	})

	results := fw.TriggerParallel(context.Background(), "parallel_event", map[string]any{"key": "val"})
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("期望调用 1 次，实际 %d", called)
	}
	if len(results) != 1 || results[0] != "result" {
		t.Errorf("结果期望 [result]，实际 %v", results)
	}
}

// TestCallbackFramework_TriggerParallel_多回调并发 测试多个回调并发执行
func TestCallbackFramework_TriggerParallel_多回调并发(t *testing.T) {
	fw := NewCallbackFramework()
	fw.EnableMetrics(true)
	var callCount int32

	// 注册 3 个回调
	for i := 0; i < 3; i++ {
		fw.OnCustom("parallel_multi", func(_ context.Context, _ map[string]any) any {
			atomic.AddInt32(&callCount, 1)
			return "ok"
		})
	}

	results := fw.TriggerParallel(context.Background(), "parallel_multi", nil)
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("期望 3 个回调都执行，实际 %d", callCount)
	}
	if len(results) != 3 {
		t.Errorf("期望 3 个结果，实际 %d", len(results))
	}
}

// TestCallbackFramework_TriggerParallel_过滤器SKIP 测试并发触发中过滤器 SKIP
func TestCallbackFramework_TriggerParallel_过滤器SKIP(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	// 全局过滤器 SKIP
	fw.AddGlobalFilter(&ConditionalFilter{
		Condition:     func(_ context.Context, _ string, _ string, _ any) bool { return false },
		ActionOnFalse: FilterActionSkip,
	})

	fw.OnCustom("parallel_filtered", func(_ context.Context, _ map[string]any) any {
		atomic.AddInt32(&called, 1)
		return "result"
	})

	results := fw.TriggerParallel(context.Background(), "parallel_filtered", nil)
	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("过滤器 SKIP 后回调不应执行，实际 %d", called)
	}
	if len(results) != 0 {
		t.Errorf("期望空结果，实际 %v", results)
	}
}

// TestCallbackFramework_TriggerParallel_无回调 测试并发触发无回调事件
func TestCallbackFramework_TriggerParallel_无回调(t *testing.T) {
	fw := NewCallbackFramework()
	results := fw.TriggerParallel(context.Background(), "no_callbacks", nil)
	if results != nil {
		t.Errorf("无回调期望 nil，实际 %v", results)
	}
}

// TestCallbackFramework_TriggerUntil 测试触发直到条件满足
func TestCallbackFramework_TriggerUntil(t *testing.T) {
	fw := NewCallbackFramework()

	fw.OnCustom("until_event", func(_ context.Context, _ map[string]any) any {
		return "match"
	})

	result := fw.TriggerUntil(context.Background(), "until_event", func(r any) bool {
		return r == "match"
	}, nil)

	if result != "match" {
		t.Errorf("期望 match，实际 %v", result)
	}

	// 无匹配时返回 nil
	result2 := fw.TriggerUntil(context.Background(), "until_event", func(r any) bool {
		return false
	}, nil)
	if result2 != nil {
		t.Errorf("无匹配期望 nil，实际 %v", result2)
	}
}

// TestCallbackFramework_TriggerUntil_过滤器管线 测试 TriggerUntil 过滤器管线
func TestCallbackFramework_TriggerUntil_过滤器管线(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	// 全局过滤器 SKIP
	fw.AddGlobalFilter(&ConditionalFilter{
		Condition:     func(_ context.Context, _ string, _ string, _ any) bool { return false },
		ActionOnFalse: FilterActionSkip,
	})

	fw.OnCustom("until_filtered", func(_ context.Context, _ map[string]any) any {
		atomic.AddInt32(&called, 1)
		return "match"
	})

	result := fw.TriggerUntil(context.Background(), "until_filtered", func(r any) bool {
		return r == "match"
	}, nil)

	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("过滤器 SKIP 后回调不应执行，实际 %d", called)
	}
	if result != nil {
		t.Errorf("过滤后期望 nil，实际 %v", result)
	}
}

// TestCallbackFramework_TriggerUntil_过滤器STOP 测试 TriggerUntil 过滤器 STOP 终止循环
func TestCallbackFramework_TriggerUntil_过滤器STOP(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	// 全局过滤器 STOP
	fw.AddGlobalFilter(&ConditionalFilter{
		Condition:     func(_ context.Context, _ string, _ string, _ any) bool { return false },
		ActionOnFalse: FilterActionStop,
	})

	fw.OnCustom("until_stop", func(_ context.Context, _ map[string]any) any {
		atomic.AddInt32(&called, 1)
		return "match"
	})

	result := fw.TriggerUntil(context.Background(), "until_stop", func(r any) bool {
		return r == "match"
	}, nil)

	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("过滤器 STOP 后回调不应执行，实际 %d", called)
	}
	if result != nil {
		t.Errorf("STOP 后期望 nil，实际 %v", result)
	}
}

// TestCallbackFramework_TriggerUntil_Once 测试 TriggerUntil 的 once 处理
func TestCallbackFramework_TriggerUntil_Once(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fw.OnCustom("until_once", func(_ context.Context, _ map[string]any) any {
		atomic.AddInt32(&called, 1)
		return 5
	}, WithOnce())

	// 第一次：条件不满足，once 回调被禁用
	result := fw.TriggerUntil(context.Background(), "until_once", func(r any) bool {
		return r == 10
	}, nil)
	if result != nil {
		t.Errorf("条件不满足期望 nil，实际 %v", result)
	}

	// 第二次：once 回调已禁用，不再执行
	result2 := fw.TriggerUntil(context.Background(), "until_once", func(r any) bool {
		return r == 5
	}, nil)
	if result2 != nil {
		t.Errorf("Once 回调禁用后期望 nil，实际 %v", result2)
	}
}

// TestCallbackFramework_TriggerUntil_优先级遍历 测试 TriggerUntil 按优先级遍历
func TestCallbackFramework_TriggerUntil_优先级遍历(t *testing.T) {
	fw := NewCallbackFramework()
	var callOrder []string

	fw.OnCustom("until_priority", func(_ context.Context, _ map[string]any) any {
		callOrder = append(callOrder, "low")
		return 5
	})
	fw.OnCustom("until_priority", func(_ context.Context, _ map[string]any) any {
		callOrder = append(callOrder, "high")
		return 10
	}, WithPriority(10))

	// 高优先级先执行，返回 10，满足条件
	result := fw.TriggerUntil(context.Background(), "until_priority", func(r any) bool {
		return r == 10
	}, nil)

	if result != 10 {
		t.Errorf("期望 10，实际 %v", result)
	}
	// 高优先级先执行后返回，低优先级不应执行
	if len(callOrder) != 1 || callOrder[0] != "high" {
		t.Errorf("期望 [high]，实际 %v", callOrder)
	}
}

// TestCallbackFramework_TriggerUntil_过滤器MODIFY 测试 TriggerUntil MODIFY 修改参数
func TestCallbackFramework_TriggerUntil_过滤器MODIFY(t *testing.T) {
	fw := NewCallbackFramework()
	var receivedValue any

	// 添加参数修改过滤器
	fw.AddGlobalFilter(&ParamModifyFilter{
		Modifier: func(data any) any {
			if m, ok := data.(map[string]any); ok {
				m["modified"] = true
				return m
			}
			return data
		},
	})

	fw.OnCustom("until_modify", func(_ context.Context, data map[string]any) any {
		receivedValue = data["modified"]
		return "ok"
	})

	result := fw.TriggerUntil(context.Background(), "until_modify", func(r any) bool {
		return r == "ok"
	}, map[string]any{"key": "val"})

	if result != "ok" {
		t.Errorf("期望 ok，实际 %v", result)
	}
	if receivedValue != true {
		t.Errorf("MODIFY 后数据应包含 modified=true，实际 %v", receivedValue)
	}
}

// TestCallbackFramework_TriggerUntil_无回调 测试 TriggerUntil 无回调事件
func TestCallbackFramework_TriggerUntil_无回调(t *testing.T) {
	fw := NewCallbackFramework()
	result := fw.TriggerUntil(context.Background(), "no_callbacks", func(r any) bool { return true }, nil)
	if result != nil {
		t.Errorf("无回调期望 nil，实际 %v", result)
	}
}

// TestCallbackFramework_ResetMetrics 测试重置指标
func TestCallbackFramework_ResetMetrics(t *testing.T) {
	fw := NewCallbackFramework()
	fw.EnableMetrics(true)
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any { return nil })
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})

	fw.ResetMetrics()
	// 重置后 GetMetrics 应返回 nil
	m := fw.GetMetrics("_framework:llm_call_started", "func(*callback.LLMCallEventData)")
	if m != nil {
		t.Errorf("ResetMetrics 后期望 nil，实际 %v", m)
	}
}

// TestCallbackFramework_GetSlowCallbacks 测试查询慢回调
func TestCallbackFramework_GetSlowCallbacks(t *testing.T) {
	fw := NewCallbackFramework()
	fw.EnableMetrics(true)
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any {
		return nil
	})
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})

	// 用极大阈值，应无慢回调
	slow := fw.GetSlowCallbacks(999.0)
	if len(slow) != 0 {
		t.Errorf("极大阈值期望无慢回调，实际 %d 个", len(slow))
	}

	// 用极小阈值，可能有慢回调（取决于执行耗时）
	slow2 := fw.GetSlowCallbacks(0.0)
	// 不验证数量，仅验证方法不报错
	_ = slow2
}

// TestCallbackFramework_EnableEventHistory 测试开关事件历史
func TestCallbackFramework_EnableEventHistory(t *testing.T) {
	fw := NewCallbackFramework()
	fw.EnableEventHistory(true)
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any { return nil })
	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})

	// 验证不报错即可
	fw.EnableEventHistory(false)
}

// TestCallbackFramework_GetStatistics 测试框架统计信息
func TestCallbackFramework_GetStatistics(t *testing.T) {
	fw := NewCallbackFramework()
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) any { return nil })
	fw.OnTool(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) any { return nil })

	stats := fw.GetStatistics()
	if stats["llm_callbacks"].(int) != 1 {
		t.Errorf("llm_callbacks 期望 1，实际 %v", stats["llm_callbacks"])
	}
	if stats["tool_callbacks"].(int) != 1 {
		t.Errorf("tool_callbacks 期望 1，实际 %v", stats["tool_callbacks"])
	}
	if stats["session_callbacks"].(int) != 0 {
		t.Errorf("session_callbacks 期望 0，实际 %v", stats["session_callbacks"])
	}
}

// TestSplitCircuitBreakerKey 测试 splitCircuitBreakerKey 拆分熔断器键
func TestSplitCircuitBreakerKey(t *testing.T) {
	// 正常拆分
	result := splitCircuitBreakerKey("event:callback")
	assert.Equal(t, [2]string{"event", "callback"}, result)

	// 多个冒号，取最后一个
	result = splitCircuitBreakerKey("ns:event:callback")
	assert.Equal(t, [2]string{"ns:event", "callback"}, result)

	// 无冒号
	result = splitCircuitBreakerKey("nokey")
	assert.Equal(t, [2]string{"nokey", ""}, result)

	// 空字符串
	result = splitCircuitBreakerKey("")
	assert.Equal(t, [2]string{"", ""}, result)
}
