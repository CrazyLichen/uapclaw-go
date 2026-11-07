package interfaces

import (
	"testing"
)

// TestAgentCallbackEvent_事件值对齐Python 验证事件值与 Python AgentCallbackEvent 完全对齐
func TestAgentCallbackEvent_事件值对齐Python(t *testing.T) {
	// Python: openjiuwen/core/single_agent/rail/base.py AgentCallbackEvent
	tests := []struct {
		got  AgentCallbackEvent
		want string
	}{
		{CallbackBeforeInvoke, "before_invoke"},
		{CallbackAfterInvoke, "after_invoke"},
		{CallbackBeforeTaskIteration, "before_task_iteration"},
		{CallbackAfterTaskIteration, "after_task_iteration"},
		{CallbackBeforeModelCall, "before_model_call"},
		{CallbackAfterModelCall, "after_model_call"},
		{CallbackOnModelException, "on_model_exception"},
		{CallbackBeforeToolCall, "before_tool_call"},
		{CallbackAfterToolCall, "after_tool_call"},
		{CallbackOnToolException, "on_tool_exception"},
		{CallbackAfterReactIteration, "after_react_iteration"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("事件值 = %q, want %q", tt.got, tt.want)
		}
	}
}

// TestAgentCallbackEvent_String 验证 String() 方法返回事件值
func TestAgentCallbackEvent_String(t *testing.T) {
	if got := CallbackBeforeModelCall.String(); got != "before_model_call" {
		t.Errorf("CallbackBeforeModelCall.String() = %q, want %q", got, "before_model_call")
	}
}

// TestAgentCallbackEvent_GoString 验证 GoString() 方法返回带类型名前缀
func TestAgentCallbackEvent_GoString(t *testing.T) {
	if got := CallbackBeforeModelCall.GoString(); got != `interfaces.AgentCallbackEvent("before_model_call")` {
		t.Errorf("CallbackBeforeModelCall.GoString() = %q, want %q", got, `interfaces.AgentCallbackEvent("before_model_call")`)
	}
}

// TestAllCallbackEvents 验证 AllCallbackEvents 返回全部 13 个事件（含 on_user_message / before_steering_drain）
func TestAllCallbackEvents(t *testing.T) {
	events := AllCallbackEvents()
	if len(events) != 13 {
		t.Fatalf("AllCallbackEvents() 返回 %d 个事件，want 13", len(events))
	}

	// 验证无重复
	seen := make(map[AgentCallbackEvent]bool)
	for _, e := range events {
		if seen[e] {
			t.Errorf("重复事件: %q", e)
		}
		seen[e] = true
	}

	// 验证包含关键事件
	if !seen[CallbackBeforeInvoke] {
		t.Error("缺少 CallbackBeforeInvoke")
	}
	if !seen[CallbackOnToolException] {
		t.Error("缺少 CallbackOnToolException")
	}
}

// TestAgentCallbackEvent_事件值即方法名 验证事件值就是 Python AgentRail 对应方法名
func TestAgentCallbackEvent_事件值即方法名(t *testing.T) {
	// 事件值直接对应 Python AgentRail 的方法名，无需 EVENT_METHOD_MAP
	methodNames := map[AgentCallbackEvent]string{
		CallbackBeforeInvoke:        "before_invoke",
		CallbackAfterInvoke:         "after_invoke",
		CallbackBeforeTaskIteration: "before_task_iteration",
		CallbackAfterTaskIteration:  "after_task_iteration",
		CallbackBeforeModelCall:     "before_model_call",
		CallbackAfterModelCall:      "after_model_call",
		CallbackOnModelException:    "on_model_exception",
		CallbackBeforeToolCall:      "before_tool_call",
		CallbackAfterToolCall:       "after_tool_call",
		CallbackOnToolException:     "on_tool_exception",
		CallbackAfterReactIteration: "after_react_iteration",
	}
	for event, methodName := range methodNames {
		if string(event) != methodName {
			t.Errorf("事件 %q 的值不等于方法名 %q", event, methodName)
		}
	}
}

// TestAllCallbackEvents_含BeforeSteeringDrain 验证新事件已进全集。
// spec C15：漏加会让 Clear() 清不掉该事件的回调，留下悬挂钩子。
func TestAllCallbackEvents_含BeforeSteeringDrain(t *testing.T) {
	events := AllCallbackEvents()
	seen := make(map[AgentCallbackEvent]bool, len(events))
	for _, e := range events {
		seen[e] = true
	}
	if !seen[CallbackBeforeSteeringDrain] {
		t.Fatal("AllCallbackEvents() 缺 CallbackBeforeSteeringDrain")
	}
	if !seen[CallbackOnUserMessage] {
		t.Fatal("AllCallbackEvents() 缺 CallbackOnUserMessage")
	}
}

// TestAllBaseCallbackEvents_含BeforeSteeringDrain 验证新事件进 base 集合。
func TestAllBaseCallbackEvents_含BeforeSteeringDrain(t *testing.T) {
	events := AllBaseCallbackEvents()
	if len(events) != 10 {
		t.Fatalf("AllBaseCallbackEvents() 返回 %d 个事件，want 10", len(events))
	}
	var found bool
	for _, e := range events {
		if e == CallbackBeforeSteeringDrain {
			found = true
		}
	}
	if !found {
		t.Fatal("AllBaseCallbackEvents() 缺 CallbackBeforeSteeringDrain")
	}
}

// TestSteeringDrainInputs_EventKind 验证事件输入的种类标识与 Limit 可写。
func TestSteeringDrainInputs_EventKind(t *testing.T) {
	in := &SteeringDrainInputs{Pending: 3}
	if in.EventKind() != "steering_drain_inputs" {
		t.Fatalf("EventKind()=%q", in.EventKind())
	}
	limit := 2
	in.Limit = &limit
	if in.Limit == nil || *in.Limit != 2 {
		t.Fatal("Limit 必须可写可读")
	}
}
