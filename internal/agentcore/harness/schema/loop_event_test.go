package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestDeepLoopEventType_String(t *testing.T) {
	tests := []struct {
		eventType DeepLoopEventType
		want      string
	}{
		{DeepLoopEventTypeFollowup, "followup"},
		{DeepLoopEventTypeSteer, "steer"},
		{DeepLoopEventTypeAbort, "abort"},
		{DeepLoopEventType(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.eventType.String(); got != tt.want {
			t.Errorf("DeepLoopEventType(%d).String() = %q，期望 %q", tt.eventType, got, tt.want)
		}
	}
}

func TestDeepLoopEventType_JSON(t *testing.T) {
	types := []DeepLoopEventType{DeepLoopEventTypeFollowup, DeepLoopEventTypeSteer, DeepLoopEventTypeAbort}
	for _, original := range types {
		data, err := json.Marshal(original)
		if err != nil {
			t.Errorf("Marshal 出错: %v", err)
			continue
		}
		var decoded DeepLoopEventType
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Errorf("Unmarshal 出错: %v", err)
			continue
		}
		if decoded != original {
			t.Errorf("JSON 往返: 原值 %d，解码后 %d", original, decoded)
		}
	}
}

func TestDeepLoopEventType_Parse(t *testing.T) {
	tests := []struct {
		input string
		want  DeepLoopEventType
		err   bool
	}{
		{"followup", DeepLoopEventTypeFollowup, false},
		{"steer", DeepLoopEventTypeSteer, false},
		{"abort", DeepLoopEventTypeAbort, false},
		{"FOLLOWUP", DeepLoopEventTypeFollowup, false},
		{"Steer", DeepLoopEventTypeSteer, false},
		{"ABORT", DeepLoopEventTypeAbort, false},
		{"invalid", DeepLoopEventTypeFollowup, true},
		{"", DeepLoopEventTypeFollowup, true},
	}
	for _, tt := range tests {
		got, err := ParseDeepLoopEventType(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("ParseDeepLoopEventType(%q) 应返回错误", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseDeepLoopEventType(%q) 出错: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseDeepLoopEventType(%q) = %d，期望 %d", tt.input, got, tt.want)
			}
		}
	}
}

func TestDefaultEventPriority(t *testing.T) {
	tests := []struct {
		eventType DeepLoopEventType
		want      int
	}{
		{DeepLoopEventTypeAbort, 0},
		{DeepLoopEventTypeSteer, 1},
		{DeepLoopEventTypeFollowup, 10},
	}
	for _, tt := range tests {
		got := DefaultEventPriority(tt.eventType)
		if got != tt.want {
			t.Errorf("DefaultEventPriority(%d) = %d，期望 %d", tt.eventType, got, tt.want)
		}
	}
}

func TestCreateLoopEvent_默认优先级(t *testing.T) {
	evt := CreateLoopEvent(1, DeepLoopEventTypeAbort, "测试中止")
	if evt.Priority != 0 {
		t.Errorf("Abort 默认优先级应为 0，实际为 %d", evt.Priority)
	}
	if evt.Seq != 1 {
		t.Errorf("Seq 应为 1，实际为 %d", evt.Seq)
	}
	if evt.EventType != DeepLoopEventTypeAbort {
		t.Errorf("EventType 应为 Abort，实际为 %d", evt.EventType)
	}
	if evt.Content != "测试中止" {
		t.Errorf("Content 应为 '测试中止'，实际为 %q", evt.Content)
	}
	if evt.EventID == "" {
		t.Error("EventID 不应为空")
	}
	if evt.CreatedAt == 0 {
		t.Error("CreatedAt 不应为 0")
	}
}

func TestCreateLoopEvent_自定义优先级(t *testing.T) {
	evt := CreateLoopEvent(2, DeepLoopEventTypeFollowup, "测试追问", WithPriority(5))
	if evt.Priority != 5 {
		t.Errorf("自定义优先级应为 5，实际为 %d", evt.Priority)
	}
}

func TestCreateLoopEvent_可选参数(t *testing.T) {
	meta := map[string]any{"key": "value"}
	evt := CreateLoopEvent(3, DeepLoopEventTypeSteer, "测试引导",
		WithTaskID("task-123"),
		WithMetadata(meta),
	)
	if evt.TaskID != "task-123" {
		t.Errorf("TaskID 应为 'task-123'，实际为 %q", evt.TaskID)
	}
	if v, ok := evt.Metadata["key"]; !ok || v != "value" {
		t.Errorf("Metadata[key] 应为 'value'，实际为 %v", evt.Metadata["key"])
	}
}

func TestDeepLoopEvent_Less(t *testing.T) {
	// 不同优先级：数值越小越优先
	abortEvt := DeepLoopEvent{Priority: 0, Seq: 2}
	followupEvt := DeepLoopEvent{Priority: 10, Seq: 1}
	if !abortEvt.Less(followupEvt) {
		t.Error("Priority=0 应优先于 Priority=10")
	}
	if followupEvt.Less(abortEvt) {
		t.Error("Priority=10 不应优先于 Priority=0")
	}

	// 同优先级：序号越小越优先
	evt1 := DeepLoopEvent{Priority: 1, Seq: 1}
	evt2 := DeepLoopEvent{Priority: 1, Seq: 2}
	if !evt1.Less(evt2) {
		t.Error("同优先级 Seq=1 应优先于 Seq=2")
	}
	if evt2.Less(evt1) {
		t.Error("同优先级 Seq=2 不应优先于 Seq=1")
	}

	// 完全相同：不应 Less
	evtSame1 := DeepLoopEvent{Priority: 1, Seq: 1}
	evtSame2 := DeepLoopEvent{Priority: 1, Seq: 1}
	if evtSame1.Less(evtSame2) {
		t.Error("完全相同的事件不应 Less")
	}
}

func TestDeepLoopEvent_JSON往返(t *testing.T) {
	evt := DeepLoopEvent{
		Priority:  1,
		Seq:       42,
		CreatedAt: 1700000000000000000,
		EventID:   "test-event-id",
		EventType: DeepLoopEventTypeSteer,
		Content:   "引导内容",
		TaskID:    "task-abc",
		Metadata:  map[string]any{"foo": "bar"},
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal 出错: %v", err)
	}
	var decoded DeepLoopEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 出错: %v", err)
	}
	if decoded.Priority != evt.Priority {
		t.Errorf("Priority: 期望 %d，实际 %d", evt.Priority, decoded.Priority)
	}
	if decoded.Seq != evt.Seq {
		t.Errorf("Seq: 期望 %d，实际 %d", evt.Seq, decoded.Seq)
	}
	if decoded.CreatedAt != evt.CreatedAt {
		t.Errorf("CreatedAt: 期望 %f，实际 %f", evt.CreatedAt, decoded.CreatedAt)
	}
	if decoded.EventID != evt.EventID {
		t.Errorf("EventID: 期望 %q，实际 %q", evt.EventID, decoded.EventID)
	}
	if decoded.EventType != evt.EventType {
		t.Errorf("EventType: 期望 %d，实际 %d", evt.EventType, decoded.EventType)
	}
	if decoded.Content != evt.Content {
		t.Errorf("Content: 期望 %q，实际 %q", evt.Content, decoded.Content)
	}
	if decoded.TaskID != evt.TaskID {
		t.Errorf("TaskID: 期望 %q，实际 %q", evt.TaskID, decoded.TaskID)
	}
}
