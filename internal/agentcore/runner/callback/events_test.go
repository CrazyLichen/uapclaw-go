package callback

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

func TestCallbackFramework_OnTool和TriggerTool(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fw.OnTool(ToolCallStarted, func(_ context.Context, data *ToolCallEventData) any {
		if data.ToolName != "weather" {
			t.Errorf("ToolName = %q, want weather", data.ToolName)
		}
		atomic.AddInt32(&called, 1)
		return nil
	})

	card := commonschema.NewBaseCard(commonschema.WithName("weather"))
	data := NewToolCallEventData(ToolCallStarted, card)
	fw.TriggerTool(context.Background(), data)

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("called = %d, want 1", called)
	}
}

func TestCallbackFramework_注销Tool(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fn := func(_ context.Context, _ *ToolCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	}

	fw.OnTool(ToolCallStarted, fn)
	fw.OffTool(ToolCallStarted, fn)

	data := NewToolCallEventData(ToolCallStarted, nil)
	fw.TriggerTool(context.Background(), data)

	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("OffTool 后不应触发，called = %d", called)
	}
}

func TestCallbackFramework_多Tool回调按序执行(t *testing.T) {
	fw := NewCallbackFramework()
	var order []int

	fw.OnTool(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) any {
		order = append(order, 1)
		return nil
	})
	fw.OnTool(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) any {
		order = append(order, 2)
		return nil
	})

	data := NewToolCallEventData(ToolCallStarted, nil)
	fw.TriggerTool(context.Background(), data)

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Errorf("执行顺序 = %v, want [1 2]", order)
	}
}

func TestCallbackFramework_TriggerTool_Nil上下文(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32
	fw.OnTool(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	})
	fw.TriggerTool(nil, NewToolCallEventData(ToolCallStarted, nil)) //nolint:staticcheck // SA1012: 专门测试 nil context 的防御逻辑
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

// TestToolCallEventType_String 验证 ToolCallEventType.String() 返回字符串值。
func TestToolCallEventType_String(t *testing.T) {
	if got := ToolCallStarted.String(); got != string(ToolCallStarted) {
		t.Errorf("ToolCallStarted.String() = %q, want %q", got, string(ToolCallStarted))
	}
}

// TestLLMCallEventType_String 验证 LLMCallEventType.String() 返回字符串值。
func TestLLMCallEventType_String(t *testing.T) {
	if got := LLMCallStarted.String(); got != string(LLMCallStarted) {
		t.Errorf("LLMCallStarted.String() = %q, want %q", got, string(LLMCallStarted))
	}
}

// TestToolCallEventData_String 验证 ToolCallEventData.String() 返回简洁描述。
func TestToolCallEventData_String(t *testing.T) {
	card := commonschema.NewBaseCard(commonschema.WithName("test"), commonschema.WithID("id123"))
	data := NewToolCallEventData(ToolCallStarted, card)
	got := data.String()
	if got == "" {
		t.Error("String() 不应返回空字符串")
	}
}

// TestSessionCallEventType_字符串值 测试 Session 事件类型字符串值
func TestSessionCallEventType_字符串值(t *testing.T) {
	if SessionCreated != "_framework:session_created" {
		t.Errorf("SessionCreated 期望 _framework:session_created，实际 %s", SessionCreated)
	}
	if AgentSessionCreated != "_framework:agent_session_created" {
		t.Errorf("AgentSessionCreated 期望 _framework:agent_session_created，实际 %s", AgentSessionCreated)
	}
}

// TestSessionCallEventType_String 测试 String 方法
func TestSessionCallEventType_String(t *testing.T) {
	if SessionCreated.String() != "_framework:session_created" {
		t.Errorf("String() 期望 _framework:session_created，实际 %s", SessionCreated.String())
	}
}

// TestSessionCallEventData_String 测试 String 方法
func TestSessionCallEventData_String(t *testing.T) {
	data := &SessionCallEventData{
		Event:     AgentSessionCreated,
		SessionID: "test-123",
	}
	result := data.String()
	if result == "" {
		t.Error("String() 不应返回空字符串")
	}
}
