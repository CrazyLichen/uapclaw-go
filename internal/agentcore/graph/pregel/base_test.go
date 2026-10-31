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

// TestInterrupt_字段 三字段读写往返。
// Python 声明仅有 value（pregel/base.py:10-14），Resumable/NS 为有意偏差 R1：
// interaction.py:61-63 实际以关键字传入了这两个 Python 不接受的参数。
func TestInterrupt_字段(t *testing.T) {
	it := Interrupt{
		Value:     "output_schema_stub",
		Resumable: true,
		NS:        "nodeA",
	}
	if it.Value != "output_schema_stub" {
		t.Errorf("Value = %v，期望 \"output_schema_stub\"", it.Value)
	}
	if !it.Resumable {
		t.Error("Resumable 应为 true")
	}
	if it.NS != "nodeA" {
		t.Errorf("NS = %q，期望 \"nodeA\"", it.NS)
	}
}

// TestInterrupt_零值 零值 Interrupt 各字段为 nil/false/空串，便于直接构造占位。
func TestInterrupt_零值(t *testing.T) {
	var it Interrupt
	if it.Value != nil {
		t.Errorf("Value = %v，期望 nil", it.Value)
	}
	if it.Resumable {
		t.Error("Resumable 零值应为 false")
	}
	if it.NS != "" {
		t.Errorf("NS = %q，期望空串", it.NS)
	}
}

// ──────────────────────────── GraphInterrupt ────────────────────────────

// TestGraphInterrupt_Error 错误文案对齐 Python str(value)。
// Python: pregel/base.py:20 super().__init__(str(value))
func TestGraphInterrupt_Error(t *testing.T) {
	cases := []struct {
		name       string
		interrupts []Interrupt
		want       string
	}{
		{"零元素", nil, "[]"},
		{"单元素", []Interrupt{{Value: "test_value"}}, "[{test_value false }]"},
		{"双元素", []Interrupt{{Value: "a"}, {Value: 1, Resumable: true, NS: "n1"}}, "[{a false } {1 true n1}]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gi := &GraphInterrupt{Interrupts: c.interrupts}
			if got := gi.Error(); got != c.want {
				t.Errorf("Error() = %q，期望 %q", got, c.want)
			}
		})
	}
}

// TestPanicGraphInterrupt panic 值恒为 *GraphInterrupt，中断值原样落地。
// 用例迁移自 session/interaction/base_test.go（8.7 本体迁入）。
func TestPanicGraphInterrupt(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 1 {
			t.Fatalf("Interrupts 长度应为 1，实际=%d", len(gi.Interrupts))
		}
		if gi.Interrupts[0].Value != "test_value" {
			t.Errorf("Interrupts[0].Value = %v，期望 \"test_value\"", gi.Interrupts[0].Value)
		}
	}()
	PanicGraphInterrupt(Interrupt{Value: "test_value"})
}

// TestPanicGraphInterrupt_多中断 变参展开为切片，顺序与实参一致。
func TestPanicGraphInterrupt_多中断(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 2 {
			t.Fatalf("Interrupts 长度应为 2，实际=%d", len(gi.Interrupts))
		}
		if gi.Interrupts[0].Value != "first" || gi.Interrupts[1].Value != "second" {
			t.Errorf("中断值顺序错乱：%v", gi.Interrupts)
		}
	}()
	PanicGraphInterrupt(Interrupt{Value: "first"}, Interrupt{Value: "second"})
}

// TestPanicGraphInterrupt_零参数 零参调用不崩，Error() 输出空切片。
// Python 同样允许 GraphInterrupt()（无参），越界防护由消费方 8.14 自行决定。
func TestPanicGraphInterrupt_零参数(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 0 {
			t.Errorf("Interrupts 长度应为 0，实际=%d", len(gi.Interrupts))
		}
		if got := gi.Error(); got != "[]" {
			t.Errorf("Error() = %q，期望 \"[]\"", got)
		}
	}()
	PanicGraphInterrupt()
}

// TestGraphInterrupt_Resumable与NS不丢 守住 8.6 §R1 的书面承诺。
func TestGraphInterrupt_Resumable与NS不丢(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if !gi.Interrupts[0].Resumable {
			t.Error("Resumable 应为 true（8.6 §R1：迁 GraphInterrupt 时不得删）")
		}
		if gi.Interrupts[0].NS != "node_x" {
			t.Errorf("NS = %q，期望 \"node_x\"", gi.Interrupts[0].NS)
		}
	}()
	PanicGraphInterrupt(Interrupt{Value: "v", Resumable: true, NS: "node_x"})
}
