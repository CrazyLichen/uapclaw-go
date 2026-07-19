package interaction

import (
	"context"
	"errors"
	"testing"
)

func TestNewUserInbox(t *testing.T) {
	u := NewUserInbox(nil)
	if u == nil {
		t.Error("NewUserInbox 应返回非 nil")
	}
}

func TestUserInbox_Direct_成功(t *testing.T) {
	u := NewUserInbox(nil)
	result, err := u.Direct("alice", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
	if result.MessageID == nil {
		t.Error("MessageID 不应为 nil")
	}
}

func TestUserInbox_Broadcast_成功(t *testing.T) {
	u := NewUserInbox(nil)
	result, err := u.Broadcast("hello all")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
	if result.MessageID == nil {
		t.Error("MessageID 不应为 nil")
	}
}

func TestDeliverToLeader_成功(t *testing.T) {
	var received string
	deliverInput := func(ctx context.Context, content string) error {
		received = content
		return nil
	}
	result := DeliverToLeader(deliverInput, "hello leader")
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
	// 对齐 Python: deliver_to_leader 不产生 bus message id → MessageID 为 nil
	if result.MessageID != nil {
		t.Errorf("MessageID = %v, want nil (deliver_to_leader 不产生 bus 消息)", result.MessageID)
	}
	if received != "hello leader" {
		t.Errorf("received = %v, want hello leader", received)
	}
}

func TestDeliverToLeader_投递失败(t *testing.T) {
	deliverInput := func(ctx context.Context, content string) error {
		return errors.New("agent busy")
	}
	result := DeliverToLeader(deliverInput, "hello")
	if result.IsOK() {
		t.Error("IsOK 应为 false")
	}
	if result.Reason == nil {
		t.Error("Reason 不应为 nil")
	}
	// 对齐 Python: f"deliver_to_leader_failed:{e}"
	if *result.Reason != "deliver_to_leader_failed:agent busy" {
		t.Errorf("Reason = %v, want deliver_to_leader_failed:agent busy", *result.Reason)
	}
}

func TestDeliverToLeader_nil回调(t *testing.T) {
	result := DeliverToLeader(nil, "hello")
	if result.IsOK() {
		t.Error("IsOK 应为 false（无回调）")
	}
	// 对齐 Python: 无回调等价于 deliver_input 抛异常
	if result.Reason == nil {
		t.Error("Reason 不应为 nil")
	}
}
