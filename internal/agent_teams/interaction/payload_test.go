package interaction

import (
	"testing"
)

func TestNewGodViewMessage(t *testing.T) {
	msg := NewGodViewMessage("hello")
	if msg.Kind() != PayloadKindGodView {
		t.Errorf("Kind() = %v, want PayloadKindGodView", msg.Kind())
	}
	if msg.Body() != "hello" {
		t.Errorf("Body() = %v, want hello", msg.Body())
	}
}

func TestNewOperatorMessage_广播(t *testing.T) {
	msg := NewOperatorMessage("hi all", nil)
	if msg.Kind() != PayloadKindOperator {
		t.Errorf("Kind() = %v, want PayloadKindOperator", msg.Kind())
	}
	if msg.Body() != "hi all" {
		t.Errorf("Body() = %v, want hi all", msg.Body())
	}
	if msg.Target() != nil {
		t.Errorf("Target() = %v, want nil", msg.Target())
	}
}

func TestNewOperatorMessage_点对点(t *testing.T) {
	target := "alice"
	msg := NewOperatorMessage("hi", &target)
	if msg.Target() == nil || *msg.Target() != "alice" {
		t.Errorf("Target() = %v, want alice", msg.Target())
	}
}

func TestNewHumanAgentMessage(t *testing.T) {
	target := "bob"
	msg := NewHumanAgentMessage("hello", "human_agent", &target)
	if msg.Kind() != PayloadKindHumanAgent {
		t.Errorf("Kind() = %v, want PayloadKindHumanAgent", msg.Kind())
	}
	if msg.Sender() != "human_agent" {
		t.Errorf("Sender() = %v, want human_agent", msg.Sender())
	}
	if msg.Target() == nil || *msg.Target() != "bob" {
		t.Errorf("Target() = %v, want bob", msg.Target())
	}
}

func TestNewHumanAgentMessage_驱动avatar(t *testing.T) {
	msg := NewHumanAgentMessage("hello", "human_agent", nil)
	if msg.Target() != nil {
		t.Errorf("Target() = %v, want nil (drive avatar)", msg.Target())
	}
}

func TestDeliverResult_成功(t *testing.T) {
	id := "msg-123"
	r := NewDeliverResultSuccess(&id)
	if !r.IsOK() {
		t.Error("IsOK() = false, want true")
	}
	if r.MessageID == nil || *r.MessageID != "msg-123" {
		t.Errorf("MessageID = %v, want msg-123", r.MessageID)
	}
	if r.Reason != nil {
		t.Errorf("Reason = %v, want nil", r.Reason)
	}
}

func TestDeliverResult_成功无消息ID(t *testing.T) {
	r := NewDeliverResultSuccess(nil)
	if !r.IsOK() {
		t.Error("IsOK() = false, want true")
	}
	if r.MessageID != nil {
		t.Errorf("MessageID = %v, want nil", r.MessageID)
	}
}

func TestDeliverResult_失败(t *testing.T) {
	r := NewDeliverResultFailure("gate_closed")
	if r.IsOK() {
		t.Error("IsOK() = true, want false")
	}
	if r.Reason == nil || *r.Reason != "gate_closed" {
		t.Errorf("Reason = %v, want gate_closed", r.Reason)
	}
}

func TestDeliverResult_String(t *testing.T) {
	id := "msg-1"
	r1 := NewDeliverResultSuccess(&id)
	if r1.String() != "DeliverResult(ok=true, message_id=msg-1)" {
		t.Errorf("String() = %v", r1.String())
	}

	r2 := NewDeliverResultSuccess(nil)
	if r2.String() != "DeliverResult(ok=true, message_id=nil)" {
		t.Errorf("String() = %v", r2.String())
	}

	r3 := NewDeliverResultFailure("gate_closed")
	if r3.String() != "DeliverResult(ok=false, reason=gate_closed)" {
		t.Errorf("String() = %v", r3.String())
	}
}

func TestPayloadKindName(t *testing.T) {
	tests := []struct {
		kind PayloadKind
		want string
	}{
		{PayloadKindGodView, "GodView"},
		{PayloadKindOperator, "Operator"},
		{PayloadKindHumanAgent, "HumanAgent"},
		{PayloadKind(99), "Unknown"},
	}
	for _, tt := range tests {
		got := payloadKindName(tt.kind)
		if got != tt.want {
			t.Errorf("payloadKindName(%v) = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestPayloadKind_String(t *testing.T) {
	if PayloadKindGodView.String() != "GodView" {
		t.Errorf("PayloadKindGodView.String() = %v, want GodView", PayloadKindGodView.String())
	}
}

func TestIsReservedMemberName(t *testing.T) {
	if !isReservedMemberName("user") {
		t.Error("user should be reserved")
	}
	if !isReservedMemberName("team_leader") {
		t.Error("team_leader should be reserved")
	}
	if !isReservedMemberName("human_agent") {
		t.Error("human_agent should be reserved")
	}
	if isReservedMemberName("alice") {
		t.Error("alice should not be reserved")
	}
}

func TestInteractPayload接口(t *testing.T) {
	// 确保三种类型都满足 InteractPayload 接口
	var _ InteractPayload = NewGodViewMessage("test")
	var _ InteractPayload = NewOperatorMessage("test", nil)
	var _ InteractPayload = NewHumanAgentMessage("test", "sender", nil)
}

func TestHumanAgentInboundEvent(t *testing.T) {
	evt := HumanAgentInboundEvent{
		MemberName: "human_agent",
		Sender:     "team_leader",
		Body:       "hello",
		Broadcast:  false,
		MessageID:  "msg-1",
		Timestamp:  1700000000000,
	}
	if evt.MemberName != "human_agent" {
		t.Errorf("MemberName = %v, want human_agent", evt.MemberName)
	}
	if evt.Timestamp != 1700000000000 {
		t.Errorf("Timestamp = %v, want 1700000000000", evt.Timestamp)
	}
}
