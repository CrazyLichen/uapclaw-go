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

// TestBarrierChannel_全AND 单元素组全部到达才就绪（向后兼容形态）。
// Python: test_barrier_cnf.py:11-23
func TestBarrierChannel_全AND(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"A"}, {"B"}, {"C"}})
	if ch.IsReady() {
		t.Fatal("未收到任何信号不应就绪")
	}
	ch.Accept(NewBarrierMessage("A", ch.Key()))
	if ch.IsReady() {
		t.Fatal("只到 A 不应就绪")
	}
	ch.Accept(NewBarrierMessage("B", ch.Key()))
	if ch.IsReady() {
		t.Fatal("只到 A、B 不应就绪")
	}
	ch.Accept(NewBarrierMessage("C", ch.Key()))
	if !ch.IsReady() {
		t.Fatal("A、B、C 全到应就绪")
	}
}

// TestBarrierChannel_单OR组任一满足 OR 组内任一 sender 到达即算该组满足。
// Python: test_barrier_cnf.py:25-32
func TestBarrierChannel_单OR组任一满足(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"LLM_1", "LLM_2"}})
	ch.Accept(NewBarrierMessage("LLM_1", ch.Key()))
	if !ch.IsReady() {
		t.Fatal("OR 组只到 LLM_1 即应就绪")
	}
}

// TestBarrierChannel_CNF混合 (LLM_1 OR LLM_2) AND LLM_3。
// Python: test_barrier_cnf.py:34-45
func TestBarrierChannel_CNF混合(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"LLM_1", "LLM_2"}, {"LLM_3"}})
	ch.Accept(NewBarrierMessage("LLM_1", ch.Key()))
	if ch.IsReady() {
		t.Fatal("缺 LLM_3 不应就绪")
	}
	ch.Accept(NewBarrierMessage("LLM_3", ch.Key()))
	if !ch.IsReady() {
		t.Fatal("LLM_1+LLM_3 应就绪")
	}
}

// TestBarrierChannel_CNF替代路径 走 LLM_2 而非 LLM_1 同样满足第一个 OR 组。
// Python: test_barrier_cnf.py:47-56
func TestBarrierChannel_CNF替代路径(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"LLM_1", "LLM_2"}, {"LLM_3"}})
	ch.Accept(NewBarrierMessage("LLM_2", ch.Key()))
	if ch.IsReady() {
		t.Fatal("缺 LLM_3 不应就绪")
	}
	ch.Accept(NewBarrierMessage("LLM_3", ch.Key()))
	if !ch.IsReady() {
		t.Fatal("LLM_2+LLM_3 应就绪")
	}
}

// TestBarrierChannel_多独立分支 (A OR B) AND (C OR D)。
// Python: test_barrier_cnf.py:58-66
func TestBarrierChannel_多独立分支(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"A", "B"}, {"C", "D"}})
	ch.Accept(NewBarrierMessage("A", ch.Key()))
	if ch.IsReady() {
		t.Fatal("缺第二组不应就绪")
	}
	ch.Accept(NewBarrierMessage("D", ch.Key()))
	if !ch.IsReady() {
		t.Fatal("A+D 应就绪")
	}
}

// TestBarrierChannel_未收到不就绪 即使 groups 非空，received 为空也不就绪。
// Python: test_barrier_cnf.py:68-71
func TestBarrierChannel_未收到不就绪(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"A", "B"}})
	if ch.IsReady() {
		t.Fatal("received 为空不应就绪")
	}
}

// TestBarrierChannel_OR组全到仍就绪 OR 组内全到不影响判定。
// Python: test_barrier_cnf.py:73-82
func TestBarrierChannel_OR组全到仍就绪(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"A", "B"}, {"C"}})
	ch.Accept(NewBarrierMessage("A", ch.Key()))
	ch.Accept(NewBarrierMessage("B", ch.Key()))
	if ch.IsReady() {
		t.Fatal("缺 C 不应就绪")
	}
	ch.Accept(NewBarrierMessage("C", ch.Key()))
	if !ch.IsReady() {
		t.Fatal("A、B、C 全对应就绪")
	}
}

// TestBarrierChannel_同超步重复信号幂等 同一 sender 重复到达只记一次，且 Accept 返 false。
// Python: test_channel.py:129-143（test_barrier_duplicate_signals）
func TestBarrierChannel_同超步重复信号幂等(t *testing.T) {
	ch := NewBarrierChannel("collect", [][]string{{"A"}, {"B"}})
	if !ch.Accept(NewBarrierMessage("A", ch.Key())) {
		t.Fatal("首次到达应返回 true")
	}
	if ch.Accept(NewBarrierMessage("A", ch.Key())) {
		t.Fatal("重复到达应返回 false")
	}
	if got := len(ch.Snapshot().Received); got != 1 {
		t.Errorf("received 数量 = %d，期望 1（Set 去重）", got)
	}
	if ch.IsReady() {
		t.Fatal("缺 B 不应就绪")
	}
}

// TestBarrierChannel_消费复位 消费后 received 清空且不就绪。
// Python: test_barrier_cnf.py:84-93
func TestBarrierChannel_消费复位(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"A", "B"}})
	ch.Accept(NewBarrierMessage("A", ch.Key()))
	if !ch.IsReady() {
		t.Fatal("A 应满足唯一 OR 组")
	}
	ch.Consume()
	if ch.IsReady() {
		t.Fatal("消费后不应就绪")
	}
	if got := len(ch.Snapshot().Received); got != 0 {
		t.Errorf("消费后 received 数量 = %d，期望 0", got)
	}
}

// TestBarrierChannel_快照恢复CNF 快照内容为 sender 集合，恢复后重新就绪。
// Python: test_barrier_cnf.py:95-110
func TestBarrierChannel_快照恢复CNF(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"A", "B"}, {"C"}})
	ch.Accept(NewBarrierMessage("A", ch.Key()))
	ch.Accept(NewBarrierMessage("C", ch.Key()))
	if !ch.IsReady() {
		t.Fatal("A+C 应就绪")
	}

	snap := ch.Snapshot()
	if len(snap.Received) != 2 || snap.Received[0] != "A" || snap.Received[1] != "C" {
		t.Fatalf("快照 Received = %v，期望升序 [A C]", snap.Received)
	}

	ch.Consume()
	if ch.IsReady() {
		t.Fatal("消费后不应就绪")
	}
	ch.Restore(snap)
	if !ch.IsReady() {
		t.Fatal("恢复后应就绪")
	}
}

// TestBarrierChannel_路由键单元素组 单元素组直出名字，组间 & 连接。
// Python: test_barrier_cnf.py:112-115
func TestBarrierChannel_路由键单元素组(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"A"}, {"B"}, {"C"}})
	if ch.Key() != "barrier:A&B&C->end" {
		t.Errorf("Key = %q，期望 barrier:A&B&C->end", ch.Key())
	}
	if ch.NodeName() != "end" {
		t.Errorf("NodeName = %q，期望 end", ch.NodeName())
	}
}

// TestBarrierChannel_路由键OR组 多元素组渲染成 (A|B)，组内升序。
// Python: test_barrier_cnf.py:117-120
func TestBarrierChannel_路由键OR组(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"A", "B"}, {"C"}})
	if ch.Key() != "barrier:(A|B)&C->end" {
		t.Errorf("Key = %q，期望 barrier:(A|B)&C->end", ch.Key())
	}
}

// TestBarrierChannel_路由键多OR组 多个 OR 组各自加括号。
// Python: test_barrier_cnf.py:122-125
func TestBarrierChannel_路由键多OR组(t *testing.T) {
	ch := NewBarrierChannel("end", [][]string{{"B", "A"}, {"D", "C"}})
	if ch.Key() != "barrier:(A|B)&(C|D)->end" {
		t.Errorf("Key = %q，期望 barrier:(A|B)&(C|D)->end", ch.Key())
	}
}

// TestBarrierChannel_零组 无任何 OR 组时，收到任一 sender 即就绪（空循环为真）。
// Python: channels.py:166-172（for group in [] 直接返回 True）
func TestBarrierChannel_零组(t *testing.T) {
	ch := NewBarrierChannel("x", nil)
	if ch.Key() != "barrier:->x" {
		t.Errorf("Key = %q，期望 barrier:->x", ch.Key())
	}
	if ch.IsReady() {
		t.Fatal("received 为空不应就绪")
	}
	ch.Accept(NewBarrierMessage("s", ch.Key()))
	if !ch.IsReady() {
		t.Fatal("无 OR 组时收到任一 sender 即应就绪")
	}
}

// TestBarrierChannel_空组永不就绪 空组渲染成 ()，且永远无法满足（构图错误 ⇒ 死锁）。
// Python: channels.py:162 + :169-171；用户 2026-11-01 拍板保留 Python 行为（偏差 D-6）
func TestBarrierChannel_空组永不就绪(t *testing.T) {
	ch := NewBarrierChannel("x", [][]string{{}})
	if ch.Key() != "barrier:()->x" {
		t.Errorf("Key = %q，期望 barrier:()->x", ch.Key())
	}
	ch.Accept(NewBarrierMessage("s", ch.Key()))
	if ch.IsReady() {
		t.Fatal("空组永不满足，不应就绪")
	}
}

// TestBarrierChannel_零值消息被拒 零值 kind（空串）等价 Python 基类 Message 实例，汇聚通道拒收。
// Python: base.py:29-43
func TestBarrierChannel_零值消息被拒(t *testing.T) {
	ch := NewBarrierChannel("x", [][]string{{"a"}})
	var zero Message
	if ch.Accept(zero) {
		t.Fatal("汇聚通道不应接受零值 Message")
	}
	if ch.IsReady() {
		t.Fatal("被拒消息不应使通道就绪")
	}
}

// TestBarrierChannel_拒绝触发消息 汇聚通道只接受 KindBarrier。
// Python: channels.py:174-179
func TestBarrierChannel_拒绝触发消息(t *testing.T) {
	ch := NewBarrierChannel("x", [][]string{{"a"}})
	if ch.Accept(NewTriggerMessage("a", ch.Key())) {
		t.Fatal("不应接受 TriggerMessage")
	}
	if ch.IsReady() {
		t.Fatal("被拒消息不应使通道就绪")
	}
}

// TestNewBarrierChannel_深复制入参 构造后调用方改入参不影响实例（偏差 D-9）。
func TestNewBarrierChannel_深复制入参(t *testing.T) {
	groups := [][]string{{"a", "b"}}
	ch := NewBarrierChannel("x", groups)
	groups[0][0] = "z"
	if ch.Key() != "barrier:(a|b)->x" {
		t.Errorf("Key = %q，期望 barrier:(a|b)->x（入参被复制）", ch.Key())
	}
}

// TestNewBarrierChannel_组内去重 Python 用 Set，重复元素天然消失（偏差 D-9）。
func TestNewBarrierChannel_组内去重(t *testing.T) {
	ch := NewBarrierChannel("x", [][]string{{"a", "a"}})
	if ch.Key() != "barrier:a->x" {
		t.Errorf("Key = %q，期望 barrier:a->x（组内去重后只剩一个元素）", ch.Key())
	}
	ch.Accept(NewBarrierMessage("a", ch.Key()))
	if !ch.IsReady() {
		t.Fatal("去重后单元素组，收到 a 即应就绪")
	}
}
