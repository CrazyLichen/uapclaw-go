package schema

import (
	"encoding/json"
	"testing"
)

// TestAllEventTypes 验证 AllEventTypes 返回全部 26 个枚举值
func TestAllEventTypes(t *testing.T) {
	events := AllEventTypes()
	if len(events) != 26 {
		t.Fatalf("AllEventTypes() 返回 %d 个事件，want 26", len(events))
	}

	// 验证无重复
	seen := make(map[EventType]bool)
	for _, et := range events {
		if seen[et] {
			t.Errorf("重复事件: %q", et)
		}
		seen[et] = true
	}

	// 验证包含关键事件
	keyEvents := []EventType{
		EventTypeConnectionAck,
		EventTypeHello,
		EventTypeChatDelta,
		EventTypeChatFinal,
		EventTypeChatError,
		EventTypeChatToolCall,
	}
	for _, ke := range keyEvents {
		if !seen[ke] {
			t.Errorf("缺少关键事件: %q", ke)
		}
	}
}

// TestParseEventType_合法值 验证解析合法值成功
func TestParseEventType_合法值(t *testing.T) {
	tests := []struct {
		input string
		want  EventType
	}{
		{"connection.ack", EventTypeConnectionAck},
		{"hello", EventTypeHello},
		{"chat.delta", EventTypeChatDelta},
		{"chat.final", EventTypeChatFinal},
		{"chat.tool_call", EventTypeChatToolCall},
		{"chat.error", EventTypeChatError},
		{"context.usage", EventTypeContextUsage},
		{"todo.updated", EventTypeTodoUpdated},
		{"team.member", EventTypeTeamMember},
		{"heartbeat.relay", EventTypeHeartbeatRelay},
		{"history.message", EventTypeHistoryGet},
	}
	for _, tt := range tests {
		got, err := ParseEventType(tt.input)
		if err != nil {
			t.Errorf("ParseEventType(%q) 返回错误: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseEventType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestParseEventType_非法值 验证解析非法值返回错误
func TestParseEventType_非法值(t *testing.T) {
	invalidInputs := []string{
		"",
		"unknown.event",
		"chat.",
		".delta",
		"CHAT_DELTA",
		"chat/delta",
		"foo.bar.baz",
	}
	for _, input := range invalidInputs {
		_, err := ParseEventType(input)
		if err == nil {
			t.Errorf("ParseEventType(%q) 应返回错误，但返回 nil", input)
		}
	}
}

// TestIsValidEventType 验证 IsValidEventType 对合法/非法值的判断
func TestIsValidEventType(t *testing.T) {
	// 合法值
	if !IsValidEventType("chat.delta") {
		t.Error(`IsValidEventType("chat.delta") = false, want true`)
	}
	if !IsValidEventType("connection.ack") {
		t.Error(`IsValidEventType("connection.ack") = false, want true`)
	}
	if !IsValidEventType("chat.ask_user_question") {
		t.Error(`IsValidEventType("chat.ask_user_question") = false, want true`)
	}

	// 非法值
	if IsValidEventType("") {
		t.Error(`IsValidEventType("") = true, want false`)
	}
	if IsValidEventType("nonexistent.event") {
		t.Error(`IsValidEventType("nonexistent.event") = true, want false`)
	}
}

// TestEventTypeString 验证 String() 返回原始字符串值
func TestEventTypeString(t *testing.T) {
	if got := EventTypeChatDelta.String(); got != "chat.delta" {
		t.Errorf("EventTypeChatDelta.String() = %q, want %q", got, "chat.delta")
	}
	if got := EventTypeConnectionAck.String(); got != "connection.ack" {
		t.Errorf("EventTypeConnectionAck.String() = %q, want %q", got, "connection.ack")
	}
}

// TestEventTypeGoString 验证 GoString() 格式
func TestEventTypeGoString(t *testing.T) {
	if got := EventTypeChatDelta.GoString(); got != `schema.EventType("chat.delta")` {
		t.Errorf("EventTypeChatDelta.GoString() = %q, want %q", got, `schema.EventType("chat.delta")`)
	}
}

// TestEventTypeJSON序列化往返 验证 JSON marshal/unmarshal 往返一致
func TestEventTypeJSON序列化往返(t *testing.T) {
	events := []EventType{
		EventTypeConnectionAck,
		EventTypeChatDelta,
		EventTypeChatFinal,
		EventTypeChatToolCall,
		EventTypeTeamMember,
	}
	for _, et := range events {
		data, err := json.Marshal(et)
		if err != nil {
			t.Errorf("json.Marshal(%q) 错误: %v", et, err)
			continue
		}
		var got EventType
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("json.Unmarshal(%s) 错误: %v", data, err)
			continue
		}
		if got != et {
			t.Errorf("JSON 往返: got %q, want %q", got, et)
		}
	}
}

// TestEventType常量值与Python对齐 验证全部 26 个常量字符串值与 Python EventType 完全对齐
func TestEventType常量值与Python对齐(t *testing.T) {
	// 对应 Python: jiuwenswarm/common/schema/message.py (EventType)
	tests := []struct {
		got  EventType
		want string
	}{
		{EventTypeConnectionAck, "connection.ack"},
		{EventTypeHello, "hello"},
		{EventTypeChatDelta, "chat.delta"},
		{EventTypeChatReasoning, "chat.reasoning"},
		{EventTypeChatUsageMetadata, "chat.usage_metadata"},
		{EventTypeChatUsageSummary, "chat.usage_summary"},
		{EventTypeChatFinal, "chat.final"},
		{EventTypeChatMedia, "chat.media"},
		{EventTypeChatFile, "chat.file"},
		{EventTypeChatToolCall, "chat.tool_call"},
		{EventTypeChatToolUpdate, "chat.tool_update"},
		{EventTypeChatToolResult, "chat.tool_result"},
		{EventTypeContextUsage, "context.usage"},
		{EventTypeTodoUpdated, "todo.updated"},
		{EventTypeChatProcessingStatus, "chat.processing_status"},
		{EventTypeChatError, "chat.error"},
		{EventTypeChatInterruptResult, "chat.interrupt_result"},
		{EventTypeChatEvolutionStatus, "chat.evolution_status"},
		{EventTypeChatSubtaskUpdate, "chat.subtask_update"},
		{EventTypeChatAskUserQuestion, "chat.ask_user_question"},
		{EventTypeChatSessionResult, "chat.session_result"},
		{EventTypeTeamMember, "team.member"},
		{EventTypeTeamTask, "team.task"},
		{EventTypeTeamMessage, "team.message"},
		{EventTypeHeartbeatRelay, "heartbeat.relay"},
		{EventTypeHistoryGet, "history.message"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("常量值 = %q, want %q", tt.got, tt.want)
		}
	}
	// 验证测试覆盖全部 26 个常量
	if len(tests) != 26 {
		t.Errorf("Python 对齐测试覆盖 %d 个常量，want 26", len(tests))
	}
}
