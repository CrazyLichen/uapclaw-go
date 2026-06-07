package schema

import (
	"context"
	"sync/atomic"
	"testing"

	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestToolCallEventType_值(t *testing.T) {
	tests := []struct {
		event ToolCallEventType
		want  string
	}{
		{ToolCallStarted, "_framework:tool_call_started"},
		{ToolCallFinished, "_framework:tool_call_finished"},
		{ToolCallError, "_framework:tool_call_error"},
		{ToolResultReceived, "_framework:tool_result_received"},
		{ToolParseStarted, "_framework:tool_parse_started"},
		{ToolParseFinished, "_framework:tool_parse_finished"},
		{ToolInvokeInput, "_framework:tool_invoke_input"},
		{ToolInvokeOutput, "_framework:tool_invoke_output"},
		{ToolStreamInput, "_framework:tool_stream_input"},
		{ToolStreamOutput, "_framework:tool_stream_output"},
		{ToolAuth, "_framework:tool_auth"},
	}
	for _, tt := range tests {
		if string(tt.event) != tt.want {
			t.Errorf("ToolCallEventType = %q, want %q", tt.event, tt.want)
		}
	}
}

func TestNewToolCallbackFramework(t *testing.T) {
	fw := NewToolCallbackFramework()
	if fw == nil {
		t.Fatal("NewToolCallbackFramework 返回 nil")
	}
}

func TestToolCallbackFramework_On和Trigger(t *testing.T) {
	fw := NewToolCallbackFramework()
	var called int32

	fw.On(ToolCallStarted, func(_ context.Context, data *ToolCallEventData) {
		if data.ToolName != "weather" {
			t.Errorf("ToolName = %q, want weather", data.ToolName)
		}
		atomic.AddInt32(&called, 1)
	})

	card := commonschema.NewBaseCard(commonschema.WithName("weather"))
	data := NewToolCallEventData(ToolCallStarted, card)
	fw.Trigger(context.Background(), data)

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("called = %d, want 1", called)
	}
}

func TestToolCallbackFramework_Off(t *testing.T) {
	fw := NewToolCallbackFramework()
	var called int32

	fn := func(_ context.Context, _ *ToolCallEventData) {
		atomic.AddInt32(&called, 1)
	}

	fw.On(ToolCallStarted, fn)
	fw.Off(ToolCallStarted, fn)

	data := NewToolCallEventData(ToolCallStarted, nil)
	fw.Trigger(context.Background(), data)

	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("Off 后不应触发，called = %d", called)
	}
}

func TestToolCallbackFramework_多回调按序执行(t *testing.T) {
	fw := NewToolCallbackFramework()
	var order []int

	fw.On(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) {
		order = append(order, 1)
	})
	fw.On(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) {
		order = append(order, 2)
	})

	data := NewToolCallEventData(ToolCallStarted, nil)
	fw.Trigger(context.Background(), data)

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Errorf("执行顺序 = %v, want [1 2]", order)
	}
}

func TestToolCallbackFramework_Trigger_NilContext(t *testing.T) {
	fw := NewToolCallbackFramework()
	var called int32
	fw.On(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) {
		atomic.AddInt32(&called, 1)
	})
	fw.Trigger(nil, NewToolCallEventData(ToolCallStarted, nil))
	if atomic.LoadInt32(&called) != 0 {
		t.Error("nil context 不应触发回调")
	}
}

func TestNewToolCallEventData(t *testing.T) {
	card := commonschema.NewBaseCard(commonschema.WithName("test"), commonschema.WithID("abc123"))
	data := NewToolCallEventData(ToolCallStarted, card)
	if data.Event != ToolCallStarted {
		t.Errorf("Event = %v, want ToolCallStarted", data.Event)
	}
	if data.ToolName != "test" {
		t.Errorf("ToolName = %q, want test", data.ToolName)
	}
	if data.ToolID != "abc123" {
		t.Errorf("ToolID = %q, want abc123", data.ToolID)
	}
}

func TestNewToolCallEventData_NilCard(t *testing.T) {
	data := NewToolCallEventData(ToolCallError, nil)
	if data.Event != ToolCallError {
		t.Errorf("Event = %v, want ToolCallError", data.Event)
	}
	if data.ToolName != "" {
		t.Errorf("ToolName 应为空，实际 %q", data.ToolName)
	}
}
