package pregel

import "testing"

// TestNewTriggerMessage 激活消息的字段与种类落地。
// Python: TriggerMessage(sender, target)（pregel/base.py:36-38）
func TestNewTriggerMessage(t *testing.T) {
	msg := NewTriggerMessage("nodeA", "nodeB")
	if msg.Sender != "nodeA" {
		t.Errorf("Sender = %q，期望 \"nodeA\"", msg.Sender)
	}
	if msg.Target != "nodeB" {
		t.Errorf("Target = %q，期望 \"nodeB\"", msg.Target)
	}
	if msg.Payload != nil {
		t.Errorf("Payload = %v，期望 nil（Python 调用点均不传 payload）", msg.Payload)
	}
	if msg.Kind() != KindTrigger {
		t.Errorf("Kind() = %q，期望 %q", msg.Kind(), KindTrigger)
	}
}

// TestNewBarrierMessage 汇聚消息的字段与种类落地。
// Target 取 BarrierChannel._make_router_key 的合成键格式（channels.py:155-164）。
// Python: BarrierMessage(sender, target)（pregel/base.py:41-43）
func TestNewBarrierMessage(t *testing.T) {
	msg := NewBarrierMessage("nodeA", "barrier:nodeA&nodeB->nodeC")
	if msg.Sender != "nodeA" {
		t.Errorf("Sender = %q，期望 \"nodeA\"", msg.Sender)
	}
	if msg.Target != "barrier:nodeA&nodeB->nodeC" {
		t.Errorf("Target = %q，期望 \"barrier:nodeA&nodeB->nodeC\"", msg.Target)
	}
	if msg.Payload != nil {
		t.Errorf("Payload = %v，期望 nil", msg.Payload)
	}
	if msg.Kind() != KindBarrier {
		t.Errorf("Kind() = %q，期望 %q", msg.Kind(), KindBarrier)
	}
}

// TestMessageKind_字符串值 判别式底层值逐值断言，防字符串漂移。
func TestMessageKind_字符串值(t *testing.T) {
	tests := []struct {
		name string
		kind MessageKind
		want string
	}{
		{"KindTrigger", KindTrigger, "trigger"},
		{"KindBarrier", KindBarrier, "barrier"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.kind) != tt.want {
				t.Errorf("%s = %q，期望 %q", tt.name, string(tt.kind), tt.want)
			}
		})
	}
}

// TestMessage_Kind 表驱动三态：trigger / barrier / 零值空串。
func TestMessage_Kind(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want MessageKind
	}{
		{"激活消息", NewTriggerMessage("a", "b"), KindTrigger},
		{"汇聚消息", NewBarrierMessage("a", "b"), KindBarrier},
		{"零值消息", Message{}, MessageKind("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.Kind(); got != tt.want {
				t.Errorf("Kind() = %q，期望 %q", got, tt.want)
			}
		})
	}
}

// TestMessage_零值不被任何通道接受
// 零值 Message 的 kind 为空串，既不等于 KindTrigger 也不等于 KindBarrier，
// 等价于 Python 基类 Message 实例不被任一 isinstance 命中（base.py 内无直接构造点）。
func TestMessage_零值不被任何通道接受(t *testing.T) {
	var msg Message
	if msg.Kind() != "" {
		t.Errorf("零值 Kind() = %q，期望空串", msg.Kind())
	}
	// 8.4 通道的过滤条件形式：Kind() == KindTrigger / Kind() == KindBarrier
	if msg.Kind() == KindTrigger {
		t.Error("零值不得被判为激活消息")
	}
	if msg.Kind() == KindBarrier {
		t.Error("零值不得被判为汇聚消息")
	}
}

// TestMessage_值拷贝独立 值类型语义：拷贝后修改副本不影响原件，
// 这是 GraphState.pending_buffer 快照可直接用 slice 拷贝的前提。
func TestMessage_值拷贝独立(t *testing.T) {
	origin := NewTriggerMessage("nodeA", "nodeB")
	copied := origin
	copied.Target = "nodeC"
	copied.Payload = "mutated"
	if origin.Target != "nodeB" {
		t.Errorf("原件 Target 被副本修改：%q", origin.Target)
	}
	if origin.Payload != nil {
		t.Errorf("原件 Payload 被副本修改：%v", origin.Payload)
	}
}
