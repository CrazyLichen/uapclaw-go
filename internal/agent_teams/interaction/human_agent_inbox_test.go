package interaction

import (
	"testing"
)

func TestHumanAgentNotEnabledError(t *testing.T) {
	err := &HumanAgentNotEnabledError{}
	if err.Error() == "" {
		t.Error("Error() 不应为空")
	}
	err2 := &HumanAgentNotEnabledError{Message: "custom msg"}
	if err2.Error() != "custom msg" {
		t.Errorf("Error() = %v, want custom msg", err2.Error())
	}
}

func TestUnknownHumanAgentError(t *testing.T) {
	err := &UnknownHumanAgentError{Sender: "ghost", Registered: []string{"human_agent"}}
	if err.Error() == "" {
		t.Error("Error() 不应为空")
	}
	// 应包含发送者名
	if err.Error() != "'ghost' is not a registered human-agent member; registered members: [human_agent]" {
		t.Errorf("Error() = %v", err.Error())
	}
}

func TestNewHumanAgentInbox(t *testing.T) {
	h := NewHumanAgentInbox(nil, nil, nil, nil)
	if h == nil {
		t.Error("NewHumanAgentInbox 应返回非 nil")
	}
}

func TestHumanAgentInbox_Send_驱动avatar(t *testing.T) {
	var lookedUp string
	lookup := func(sender string) any {
		lookedUp = sender
		return "mock-agent" // 非 nil 表示有活跃运行时
	}
	h := NewHumanAgentInbox(nil, nil, lookup, nil)
	result, err := h.Send("hello", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
	if lookedUp != "human_agent" {
		t.Errorf("lookedUp = %v, want human_agent", lookedUp)
	}
	// 对齐 Python: deliver_to_leader 通道不产生 bus message → MessageID 为 nil
	if result.MessageID != nil {
		t.Errorf("MessageID = %v, want nil (drive avatar)", result.MessageID)
	}
}

func TestHumanAgentInbox_Send_广播(t *testing.T) {
	h := NewHumanAgentInbox(nil, nil, nil, nil)
	target := "all"
	result, err := h.Send("hello all", &target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestHumanAgentInbox_Send_无lookup时驱动失败(t *testing.T) {
	h := NewHumanAgentInbox(nil, nil, nil, nil)
	result, err := h.Send("hello", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("无 agentLookup 时驱动 avatar 应失败")
	}
	if result.Reason == nil || *result.Reason != "agent_unavailable" {
		t.Errorf("Reason = %v, want agent_unavailable", result.Reason)
	}
}

func TestHumanAgentInbox_Send_lookup返回nil(t *testing.T) {
	lookup := func(sender string) any { return nil }
	h := NewHumanAgentInbox(nil, nil, lookup, nil)
	result, err := h.Send("hello", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("agentLookup 返回 nil 时应失败")
	}
	if result.Reason == nil || *result.Reason != "agent_unavailable" {
		t.Errorf("Reason = %v, want agent_unavailable", result.Reason)
	}
}

func TestHumanAgentInbox_Send_未知发送者(t *testing.T) {
	h := NewHumanAgentInbox(nil, nil, nil, nil)
	sender := "ghost"
	_, err := h.Send("hello", nil, &sender)
	if err == nil {
		t.Error("未知发送者应返回错误")
	}
	if _, ok := err.(*UnknownHumanAgentError); !ok {
		t.Errorf("err 类型 = %T, want *UnknownHumanAgentError", err)
	}
}

func TestHumanAgentInbox_Send_指定发送者(t *testing.T) {
	lookup := func(sender string) any { return "mock-agent" }
	h := NewHumanAgentInbox(nil, nil, lookup, nil)
	sender := "human_agent"
	result, err := h.Send("hello", nil, &sender)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestHumanAgentInbox_Send_点对点(t *testing.T) {
	h := NewHumanAgentInbox(nil, nil, nil, nil)
	target := "alice"
	result, err := h.Send("hello", &target, nil)
	if err != nil {
		t.Fatal(err)
	}
	// stub 模式下 memberExists 返回 true，DeliverDirect 返回成功
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestHumanAgentInbox_GetOnInbound(t *testing.T) {
	cb := func(event HumanAgentInboundEvent) error { return nil }
	h := NewHumanAgentInbox(nil, nil, nil, cb)
	if h.GetOnInbound() == nil {
		t.Error("GetOnInbound 不应为 nil")
	}
}

func TestHumanAgentInbox_GetOnInbound_nil(t *testing.T) {
	h := NewHumanAgentInbox(nil, nil, nil, nil)
	if h.GetOnInbound() != nil {
		t.Error("GetOnInbound 应为 nil")
	}
}
