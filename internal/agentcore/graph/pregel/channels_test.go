package pregel

import (
	"testing"
)

// TestTriggerChannel_接收触发消息 触发通道收下 TriggerMessage 后就绪，Key 与 NodeName 同为节点名。
// Python: channels.py:104-116（test_channel.py:13-30 前半段）
func TestTriggerChannel_接收触发消息(t *testing.T) {
	ch := NewTriggerChannel("a")
	if ch.Key() != "a" || ch.NodeName() != "a" {
		t.Errorf("Key/NodeName = %q/%q，期望均为 a", ch.Key(), ch.NodeName())
	}
	if ch.IsReady() {
		t.Fatal("新建通道不应就绪")
	}
	if !ch.Accept(NewTriggerMessage(Start, "a")) {
		t.Fatal("Accept 应返回 true")
	}
	if !ch.IsReady() {
		t.Fatal("收下消息后就绪")
	}
}

// TestTriggerChannel_拒绝汇聚消息 触发通道只接受 KindTrigger。
// Python: channels.py:112-116（isinstance(msg, TriggerMessage)）
func TestTriggerChannel_拒绝汇聚消息(t *testing.T) {
	ch := NewTriggerChannel("a")
	if ch.Accept(NewBarrierMessage("s", "a")) {
		t.Fatal("不应接受 BarrierMessage")
	}
	if ch.IsReady() {
		t.Fatal("被拒消息不应使通道就绪")
	}
}

// TestTriggerChannel_零值消息被拒 零值 kind（空串）等价 Python 基类 Message 实例，触发通道拒收。
// Python: base.py:29-43（基类实例不被任一 isinstance 命中）
func TestTriggerChannel_零值消息被拒(t *testing.T) {
	ch := NewTriggerChannel("a")
	var zero Message
	if ch.Accept(zero) {
		t.Fatal("触发通道不应接受零值 Message")
	}
	if ch.IsReady() {
		t.Fatal("被拒消息不应使通道就绪")
	}
}

// TestTriggerChannel_消费复位 消费后清空且不就绪——不清空会导致同一节点被无限重激活。
// Python: channels.py:118-120 + test_channel.py:37
func TestTriggerChannel_消费复位(t *testing.T) {
	ch := NewTriggerChannel("a")
	ch.Accept(NewTriggerMessage("s1", "a"))
	ch.Accept(NewTriggerMessage("s2", "a"))
	ch.Consume()
	if ch.IsReady() {
		t.Fatal("消费后不应就绪")
	}
	if got := ch.Snapshot(); len(got.Messages) != 0 {
		t.Errorf("快照 Messages 长度 = %d，期望 0", len(got.Messages))
	}
}

// TestTriggerChannel_快照恢复 快照保序、kind 保真，恢复后原样就绪。
// Python: channels.py:122-126
func TestTriggerChannel_快照恢复(t *testing.T) {
	ch := NewTriggerChannel("a")
	ch.Accept(NewTriggerMessage("s1", "a"))
	ch.Accept(NewTriggerMessage("s2", "a"))
	snap := ch.Snapshot()
	if len(snap.Messages) != 2 || snap.Messages[0].Sender != "s1" || snap.Messages[1].Sender != "s2" {
		t.Fatalf("快照未保序：%+v", snap.Messages)
	}
	if snap.Messages[0].Kind() != KindTrigger {
		t.Errorf("快照消息 kind = %q，期望 %q", snap.Messages[0].Kind(), KindTrigger)
	}

	ch.Consume()
	ch.Restore(snap)
	if !ch.IsReady() {
		t.Fatal("恢复后应就绪")
	}
	if got := ch.Snapshot().Messages; len(got) != 2 || got[0].Sender != "s1" {
		t.Errorf("恢复内容不符：%+v", got)
	}
}

// TestChannel_接口断言 接口满足关系由 channels.go 的 var 块在编译期保证，此处防止被误删。
func TestChannel_接口断言(t *testing.T) {
	var ch Channel = NewTriggerChannel("a")
	if ch.NodeName() != "a" {
		t.Errorf("NodeName = %q，期望 a", ch.NodeName())
	}
}
