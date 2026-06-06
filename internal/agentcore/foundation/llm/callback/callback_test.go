package callback

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockCallback 记录被调用的事件，用于测试回调触发
type mockCallback struct {
	called int32
	last   *LLMCallEventData
}

// ──────────────────────────── 导出函数 ────────────────────────────

func (m *mockCallback) call(_ context.Context, data *LLMCallEventData) {
	atomic.AddInt32(&m.called, 1)
	m.last = data
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestLLMCallEventTypeValues 验证事件类型字符串值与 Python 一致
func TestLLMCallEventTypeValues(t *testing.T) {
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

// TestCallbackFramework_On_Off 测试回调注册和注销
func TestCallbackFramework_On_Off(t *testing.T) {
	fw := NewCallbackFramework()
	mock := &mockCallback{}

	// 注册回调
	fw.On(LLMCallStarted, mock.call)

	// 触发事件
	fw.Trigger(context.Background(), &LLMCallEventData{
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

	// 注销回调
	fw.Off(LLMCallStarted, mock.call)

	// 再次触发，回调不应被调用
	fw.Trigger(context.Background(), &LLMCallEventData{
		Event:     LLMCallStarted,
		ModelName: "gpt-3.5",
	})

	if atomic.LoadInt32(&mock.called) != 1 {
		t.Errorf("注销后期望回调不被调用，实际调用 %d 次", mock.called)
	}
}

// TestCallbackFramework_MultipleCallbacks 测试同一事件注册多个回调
func TestCallbackFramework_MultipleCallbacks(t *testing.T) {
	fw := NewCallbackFramework()
	var callOrder []string

	fw.On(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) {
		callOrder = append(callOrder, "first")
	})
	fw.On(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) {
		callOrder = append(callOrder, "second")
	})

	fw.Trigger(context.Background(), &LLMCallEventData{Event: LLMCallStarted})

	if len(callOrder) != 2 {
		t.Fatalf("期望 2 个回调被调用，实际 %d 个", len(callOrder))
	}
	if callOrder[0] != "first" || callOrder[1] != "second" {
		t.Errorf("期望调用顺序 [first, second]，实际 %v", callOrder)
	}
}

// TestCallbackFramework_TriggerContextTODO 测试 context.TODO() 正常触发回调
func TestCallbackFramework_TriggerContextTODO(t *testing.T) {
	fw := NewCallbackFramework()
	mock := &mockCallback{}
	fw.On(LLMCallStarted, mock.call)

	fw.Trigger(context.TODO(), &LLMCallEventData{Event: LLMCallStarted})

	if atomic.LoadInt32(&mock.called) != 1 {
		t.Errorf("context.TODO() 时期望回调被调用 1 次，实际调用 %d 次", mock.called)
	}
}

// TestCallbackFramework_TriggerNilData 测试 nil data 不触发回调
func TestCallbackFramework_TriggerNilData(t *testing.T) {
	fw := NewCallbackFramework()
	mock := &mockCallback{}
	fw.On(LLMCallStarted, mock.call)

	fw.Trigger(context.Background(), nil)

	if atomic.LoadInt32(&mock.called) != 0 {
		t.Errorf("nil data 时期望回调不被调用，实际调用 %d 次", mock.called)
	}
}

// TestCallbackFramework_UnregisteredEvent 测试触发未注册事件的事件
func TestCallbackFramework_UnregisteredEvent(t *testing.T) {
	fw := NewCallbackFramework()
	mock := &mockCallback{}

	// 注册 LLMCallStarted 但触发 LLMCallError
	fw.On(LLMCallStarted, mock.call)
	fw.Trigger(context.Background(), &LLMCallEventData{Event: LLMCallError})

	if atomic.LoadInt32(&mock.called) != 0 {
		t.Errorf("未注册事件不应触发回调，实际调用 %d 次", mock.called)
	}
}

// TestGetCallbackFramework 测试全局单例
func TestGetCallbackFramework(t *testing.T) {
	fw1 := GetCallbackFramework()
	fw2 := GetCallbackFramework()
	if fw1 != fw2 {
		t.Error("GetCallbackFramework 应返回同一个全局实例")
	}
}

// TestNewCallbackFramework_LoggingCallback 测试默认日志回调已注册
func TestNewCallbackFramework_LoggingCallback(t *testing.T) {
	fw := NewCallbackFramework()

	// 验证所有事件类型都有日志回调
	events := []LLMCallEventType{
		LLMCallStarted, LLMCallError, LLMResponseReceived,
		LLMInvokeInput, LLMInvokeOutput,
		LLMStreamInput, LLMStreamOutput,
		LLMInput, LLMOutput,
	}

	for _, event := range events {
		fw.mu.RLock()
		callbacks, ok := fw.callbacks[event]
		fw.mu.RUnlock()

		if !ok || len(callbacks) == 0 {
			t.Errorf("事件 %s 应该有默认日志回调", event)
		}
	}
}

// TestCallbackFramework_OffNonExistent 测试注销不存在的回调
func TestCallbackFramework_OffNonExistent(t *testing.T) {
	fw := NewCallbackFramework()

	// 注销未注册的回调不应 panic
	fw.Off(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) {})
}

// TestLLMCallEventData 测试事件数据结构
func TestLLMCallEventData(t *testing.T) {
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

// TestLoggingCallback 测试日志回调不 panic
func TestLoggingCallback(t *testing.T) {
	temp := 0.5
	data := &LLMCallEventData{
		Event:         LLMCallStarted,
		ModelName:     "test-model",
		ModelProvider: "Test",
		Temperature:   &temp,
		IsStream:      false,
	}

	// 各事件类型的日志回调都不应 panic
	events := []LLMCallEventType{
		LLMCallStarted, LLMCallError, LLMResponseReceived,
		LLMInvokeInput, LLMInvokeOutput,
		LLMStreamInput, LLMStreamOutput, LLMInput, LLMOutput,
	}

	for _, event := range events {
		data.Event = event
		LoggingCallback(context.Background(), data)
	}
}

// TestCallbackFramework_OffByPointer 测试通过指针注销回调
func TestCallbackFramework_OffByPointer(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fn := func(_ context.Context, _ *LLMCallEventData) {
		atomic.AddInt32(&called, 1)
	}

	fw.On(LLMCallStarted, fn)
	fw.Trigger(context.Background(), &LLMCallEventData{Event: LLMCallStarted})

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("期望调用 1 次，实际 %d 次", called)
	}

	// 使用相同函数指针注销
	fw.Off(LLMCallStarted, fn)
	fw.Trigger(context.Background(), &LLMCallEventData{Event: LLMCallStarted})

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("注销后期望不再调用，实际调用 %d 次", called)
	}
}

// TestCallbackFramework_TriggerErrorEvent 测试错误事件回调
func TestCallbackFramework_TriggerErrorEvent(t *testing.T) {
	fw := NewCallbackFramework()
	var receivedErr error

	fw.On(LLMCallError, func(_ context.Context, data *LLMCallEventData) {
		receivedErr = data.Error
	})

	testErr := fmt.Errorf("test error")
	fw.Trigger(context.Background(), &LLMCallEventData{
		Event: LLMCallError,
		Error: testErr,
	})

	if receivedErr == nil || receivedErr.Error() != "test error" {
		t.Errorf("期望收到 test error，实际 %v", receivedErr)
	}
}

// TestLoggingCallback_AllFields 测试日志回调覆盖所有字段分支
func TestLoggingCallback_AllFields(t *testing.T) {
	temp := 0.8
	topP := 0.95
	maxTokens := 4096
	usage := llmschema.NewUsageMetadata()
	usage.InputTokens = 100
	usage.OutputTokens = 200
	usage.TotalTokens = 300

	tests := []struct {
		name  string
		event LLMCallEventType
		data  *LLMCallEventData
	}{
		{
			name:  "LLMCallStarted-完整字段",
			event: LLMCallStarted,
			data: &LLMCallEventData{
				Event:         LLMCallStarted,
				ModelName:     "gpt-4",
				ModelProvider: "OpenAI",
				Temperature:   &temp,
				TopP:          &topP,
				MaxTokens:     &maxTokens,
				Messages:      []string{"hello"},
				Tools:         []string{"tool1"},
				IsStream:      false,
				Extra:         map[string]any{"session_id": "abc"},
			},
		},
		{
			name:  "LLMCallError-完整字段",
			event: LLMCallError,
			data: &LLMCallEventData{
				Event:         LLMCallError,
				ModelName:     "gpt-4",
				ModelProvider: "OpenAI",
				Error:         fmt.Errorf("timeout"),
				Messages:      []string{"hello"},
				Tools:         []string{"tool1"},
				IsStream:      true,
				Extra:         map[string]any{"retry": 3},
			},
		},
		{
			name:  "LLMInvokeOutput-带Usage",
			event: LLMInvokeOutput,
			data: &LLMCallEventData{
				Event:         LLMInvokeOutput,
				ModelName:     "gpt-4",
				ModelProvider: "OpenAI",
				Usage:         usage,
				Response:      llmschema.NewAssistantMessage("hi"),
				IsStream:      false,
				Extra:         map[string]any{"key": "val"},
			},
		},
		{
			name:  "LLMResponseReceived-无可选字段",
			event: LLMResponseReceived,
			data: &LLMCallEventData{
				Event:         LLMResponseReceived,
				ModelName:     "gpt-4",
				ModelProvider: "OpenAI",
				IsStream:      false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 不应 panic
			LoggingCallback(context.Background(), tt.data)
		})
	}
}

// TestCallbackFramework_Off_UnregisteredEvent 测试注销未注册事件的回调
func TestCallbackFramework_Off_UnregisteredEvent(t *testing.T) {
	fw := NewCallbackFramework()

	// 创建干净框架，无 LoggingCallback
	fw.callbacks = make(map[LLMCallEventType][]CallbackFunc)

	// 注销不存在事件的回调不应 panic
	fn := func(_ context.Context, _ *LLMCallEventData) {}
	fw.Off(LLMCallStarted, fn)
}

// TestCallbackFramework_Off_NoMatch 测试注销不匹配的回调函数
func TestCallbackFramework_Off_NoMatch(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fn1 := func(_ context.Context, _ *LLMCallEventData) {
		atomic.AddInt32(&called, 1)
	}
	fn2 := func(_ context.Context, _ *LLMCallEventData) {}

	fw.On(LLMCallStarted, fn1)

	// 用 fn2 注销，不应影响 fn1
	fw.Off(LLMCallStarted, fn2)

	fw.Trigger(context.Background(), &LLMCallEventData{Event: LLMCallStarted})
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("fn1 应仍被调用，实际调用 %d 次", called)
	}
}
