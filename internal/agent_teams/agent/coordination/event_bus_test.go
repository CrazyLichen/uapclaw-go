package coordination

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
)

func TestInnerEventType_值(t *testing.T) {
	tests := []struct {
		name  string
		value InnerEventType
		want  string
	}{
		{"user_input", InnerEventTypeUserInput, "user_input"},
		{"poll_mailbox", InnerEventTypePollMailbox, "coordination_poll_mailbox"},
		{"poll_task", InnerEventTypePollTask, "coordination_poll_task"},
		{"shutdown", InnerEventTypeShutdown, "shutdown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("InnerEventType %s = %q, want %q", tt.name, tt.value, tt.want)
			}
		})
	}
}

func TestCoordinationEvent_IsInner(t *testing.T) {
	innerEvent := CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput}}
	if !innerEvent.IsInner() {
		t.Error("IsInner() 应为 true（Inner 非 nil）")
	}
	if innerEvent.IsTransport() {
		t.Error("IsTransport() 应为 false（Inner 非 nil 时）")
	}
}

func TestCoordinationEvent_IsTransport(t *testing.T) {
	transportEvent := CoordinationEvent{Transport: &events.EventMessage{EventType: "message"}}
	if !transportEvent.IsTransport() {
		t.Error("IsTransport() 应为 true（Transport 非 nil）")
	}
	if transportEvent.IsInner() {
		t.Error("IsInner() 应为 false（Transport 非 nil 时）")
	}
}

func TestCoordinationEvent_EventType(t *testing.T) {
	innerEvent := CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypePollTask}}
	if got := innerEvent.EventType(); got != "coordination_poll_task" {
		t.Errorf("EventType() = %q, want %q", got, "coordination_poll_task")
	}
	transportEvent := CoordinationEvent{Transport: &events.EventMessage{EventType: "member_shutdown"}}
	if got := transportEvent.EventType(); got != "member_shutdown" {
		t.Errorf("EventType() = %q, want %q", got, "member_shutdown")
	}
}
