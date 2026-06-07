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

func (m *mockLLMCallback) call(_ context.Context, data *LLMCallEventData) {
	atomic.AddInt32(&m.called, 1)
	m.last = data
}

// ──────────────────────────── 导出函数 ────────────────────────────

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

func TestCallbackFramework_On_Off(t *testing.T) {
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

func TestCallbackFramework_MultipleCallbacks(t *testing.T) {
	fw := NewCallbackFramework()
	var callOrder []string

	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) {
		callOrder = append(callOrder, "first")
	})
	fw.OnLLM(LLMCallStarted, func(_ context.Context, _ *LLMCallEventData) {
		callOrder = append(callOrder, "second")
	})

	fw.TriggerLLM(context.Background(), &LLMCallEventData{Event: LLMCallStarted})

	if len(callOrder) != 2 || callOrder[0] != "first" || callOrder[1] != "second" {
		t.Errorf("期望调用顺序 [first, second]，实际 %v", callOrder)
	}
}

func TestCallbackFramework_TriggerNilData(t *testing.T) {
	fw := NewCallbackFramework()
	mock := &mockLLMCallback{}
	fw.OnLLM(LLMCallStarted, mock.call)

	fw.TriggerLLM(context.Background(), nil)

	if atomic.LoadInt32(&mock.called) != 0 {
		t.Errorf("nil data 时期望回调不被调用，实际调用 %d 次", mock.called)
	}
}

func TestGetCallbackFramework(t *testing.T) {
	fw1 := GetCallbackFramework()
	fw2 := GetCallbackFramework()
	if fw1 != fw2 {
		t.Error("GetCallbackFramework 应返回同一个全局实例")
	}
}

func TestNewCallbackFramework_LoggingCallback(t *testing.T) {
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

func TestLoggingLLMCallback(t *testing.T) {
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

func TestCallbackFramework_OffByPointer(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fn := func(_ context.Context, _ *LLMCallEventData) {
		atomic.AddInt32(&called, 1)
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

func TestCallbackFramework_TriggerErrorEvent(t *testing.T) {
	fw := NewCallbackFramework()
	var receivedErr error

	fw.OnLLM(LLMCallError, func(_ context.Context, data *LLMCallEventData) {
		receivedErr = data.Error
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
