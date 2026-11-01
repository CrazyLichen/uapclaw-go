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

// TestChannelManager_触发通道消费后复位 消费后节点出集，下一超步不重激活（防无限循环）。
// Python: test_channel.py:11-59
func TestChannelManager_触发通道消费后复位(t *testing.T) {
	chStart := NewTriggerChannel("start")
	chA := NewTriggerChannel("a")
	m := NewChannelManager([]Channel{chStart, chA})

	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("构造后就绪集 = %v，期望空", got)
	}

	m.BufferMessage(NewTriggerMessage(Start, "start"))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if got := m.GetReadyNodes(); len(got) != 1 || got[0] != "start" {
		t.Fatalf("就绪集 = %v，期望 [start]", got)
	}
	if !chStart.IsReady() {
		t.Fatal("start 通道应就绪")
	}

	m.Consume("start")
	if chStart.IsReady() {
		t.Fatal("消费后通道必须清空（否则同节点无限重激活）")
	}
	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("消费后就绪集 = %v，期望空", got)
	}

	m.BufferMessage(NewTriggerMessage("start", "a"))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if got := m.GetReadyNodes(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("就绪集 = %v，期望只有 [a]", got)
	}

	m.Consume("a")
	if err := m.Flush(); err != nil {
		t.Fatalf("空 buffer Flush 返回错误：%v", err)
	}
	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("就绪集 = %v，期望空", got)
	}
}

// TestChannelManager_汇聚通道全生命周期 等待→部分到达→就绪→消费→再等待。
// Python: test_channel.py:61-127
func TestChannelManager_汇聚通道全生命周期(t *testing.T) {
	bar := NewBarrierChannel("collect", [][]string{{"A"}, {"B"}})
	m := NewChannelManager([]Channel{bar})

	m.BufferMessage(NewBarrierMessage("A", bar.Key()))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if bar.IsReady() {
		t.Fatal("只到 A 不应就绪")
	}
	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("就绪集 = %v，期望空", got)
	}

	m.BufferMessage(NewBarrierMessage("B", bar.Key()))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if got := m.GetReadyNodes(); len(got) != 1 || got[0] != "collect" {
		t.Fatalf("就绪集 = %v，期望 [collect]", got)
	}

	m.Consume("collect")
	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("消费后就绪集 = %v，期望空", got)
	}
	if got := len(bar.Snapshot().Received); got != 0 {
		t.Fatalf("消费后 received = %d，期望 0", got)
	}

	m.BufferMessage(NewBarrierMessage("A", bar.Key()))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if bar.IsReady() {
		t.Fatal("新一轮只到 A 不应就绪")
	}
}

// TestChannelManager_CNF汇聚全链路 Manager 与 CNF 汇聚通道联动，单分支到达即激活目标。
// Python: test_barrier_cnf.py:127-137
func TestChannelManager_CNF汇聚全链路(t *testing.T) {
	bar := NewBarrierChannel("end", [][]string{{"LLM_1", "LLM_2"}})
	m := NewChannelManager([]Channel{bar})
	m.BufferMessage(NewBarrierMessage("LLM_1", bar.Key()))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if !bar.IsReady() {
		t.Fatal("OR 组只到 LLM_1 应就绪")
	}
	if got := m.GetReadyNodes(); len(got) != 1 || got[0] != "end" {
		t.Fatalf("就绪集 = %v，期望 [end]", got)
	}
}

// TestChannelManager_Flush目标无通道 返回 error 且 buffer 不被清空（偏差 D-3）。
// Python: channels.py:43-45 的 ValueError + :52 的 clear 位置
func TestChannelManager_Flush目标无通道(t *testing.T) {
	ch := NewTriggerChannel("a")
	m := NewChannelManager([]Channel{ch})
	m.BufferMessage(NewTriggerMessage("s", "ghost"))

	err := m.Flush()
	if err == nil {
		t.Fatal("目标通道不存在时应返回 error")
	}
	if got := len(m.Buffer()); got != 1 {
		t.Fatalf("buffer 长度 = %d，期望 1（出错时不清 buffer）", got)
	}
}

// TestChannelManager_空通道列表 nil 通道列表可用（等价 Python 空 list）。
func TestChannelManager_空通道列表(t *testing.T) {
	m := NewChannelManager(nil)
	if !m.IsEmpty() {
		t.Fatal("新建管理器 buffer 应为空")
	}
	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("就绪集 = %v，期望空", got)
	}
	if err := m.Flush(); err != nil {
		t.Fatalf("空 buffer Flush 应返回 nil，实际 %v", err)
	}
}

// TestChannelManager_构造期恢复就绪 构造时已有数据的通道使节点立即进就绪集。
// Python: channels.py:29-31（# recover 分支）
func TestChannelManager_构造期恢复就绪(t *testing.T) {
	ch := NewTriggerChannel("a")
	ch.Accept(NewTriggerMessage("s", "a"))

	m := NewChannelManager([]Channel{ch})
	if got := m.GetReadyNodes(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("就绪集 = %v，期望 [a]（构造期应扫出已就绪通道）", got)
	}
}

// TestChannelManager_GetReadyNodes升序 多节点就绪时按字典序返回（偏差 D-4）。
// Python: channels.py:60-61 返回 list(set)，顺序无契约
func TestChannelManager_GetReadyNodes升序(t *testing.T) {
	chs := make([]Channel, 0, 3)
	for _, name := range []string{"c", "a", "b"} {
		ch := NewTriggerChannel(name)
		ch.Accept(NewTriggerMessage("s", name))
		chs = append(chs, ch)
	}
	m := NewChannelManager(chs)
	got := m.GetReadyNodes()
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("就绪集 = %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("就绪集 = %v，期望升序 %v", got, want)
		}
	}
}

// TestChannelManager_Consume未知节点 未知节点无操作、不 panic。
// Python: channels.py:64-65
func TestChannelManager_Consume未知节点(t *testing.T) {
	m := NewChannelManager([]Channel{NewTriggerChannel("a")})
	m.Consume("ghost")
	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("就绪集 = %v，期望空", got)
	}
}

// TestChannelManager_重复键后者覆盖 同 Key 的两个通道：写入只进后者，读侧两个都保留（偏差 D-7）。
// Python: channels.py:26-28（两个索引独立写）
func TestChannelManager_重复键后者覆盖(t *testing.T) {
	first := NewTriggerChannel("a")
	second := NewTriggerChannel("a")
	m := NewChannelManager([]Channel{first, second})

	m.BufferMessage(NewTriggerMessage("s", "a"))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if first.IsReady() {
		t.Fatal("重复键时写入应只进后者")
	}
	if !second.IsReady() {
		t.Fatal("重复键时后者应收到消息")
	}
	if got := len(m.nodeToChannels["a"]); got != 2 {
		t.Fatalf("读侧通道数 = %d，期望 2（两个都保留）", got)
	}
}

// TestChannelManager_重复键汇聚僵尸 两个同 Key 的 barrier：消息只进后者，前者永远收不满（偏差 D-7）。
// Python: channels.py:26-28（key 相同才会互相覆盖，故两个通道的 expected_groups 必须一致）
func TestChannelManager_重复键汇聚僵尸(t *testing.T) {
	first := NewBarrierChannel("collect", [][]string{{"A"}, {"B"}})
	second := NewBarrierChannel("collect", [][]string{{"A"}, {"B"}})
	if first.Key() != second.Key() {
		t.Fatalf("两个通道 Key 必须相同才能触发覆盖：%q vs %q", first.Key(), second.Key())
	}
	m := NewChannelManager([]Channel{first, second})

	m.BufferMessage(NewBarrierMessage("A", first.Key()))
	m.BufferMessage(NewBarrierMessage("B", first.Key()))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if first.IsReady() {
		t.Fatal("前者不应收到消息（写入索引被后者覆盖）")
	}
	if !second.IsReady() {
		t.Fatal("后者应收到 A、B 并就绪")
	}
	if got := m.GetReadyNodes(); len(got) != 1 || got[0] != "collect" {
		t.Fatalf("就绪集 = %v，期望 [collect]", got)
	}
}

// TestChannelManager_Buffer返回副本 取到的是快照副本：改它不污染内部、内部增长不回灌、
// Flush 后旧副本仍是完好的快照（偏差 D-9 实施后修订）。
func TestChannelManager_Buffer返回副本(t *testing.T) {
	m := NewChannelManager([]Channel{NewTriggerChannel("b"), NewTriggerChannel("d")})
	m.BufferMessage(NewTriggerMessage("a", "b"))

	buf := m.Buffer()
	if len(buf) != 1 || buf[0].Sender != "a" || buf[0].Kind() != KindTrigger {
		t.Fatalf("副本内容 = %+v，期望 1 条 a→b 且 kind=trigger", buf)
	}

	// 改副本不污染内部
	buf[0] = NewTriggerMessage("x", "y")
	if got := m.Buffer()[0].Sender; got != "a" {
		t.Fatalf("改副本污染了内部 buffer，实际 %q", got)
	}

	// 内部增长不回灌旧副本
	m.BufferMessage(NewTriggerMessage("c", "d"))
	if len(buf) != 1 {
		t.Fatalf("旧副本长度 = %d，期望 1（副本不随内部增长）", len(buf))
	}

	// 往副本 append 不影响内部长度
	buf = append(buf, NewTriggerMessage("e", "d"))
	if got := len(m.Buffer()); got != 2 {
		t.Fatalf("对副本 append 影响了内部，内部长度 = %d，期望 2", got)
	}

	// Flush 清空内部后，旧副本仍是完好快照（不再是「len 还在、元素全零」）
	old := m.Buffer()
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if len(old) != 2 || old[0].Sender != "a" || old[0].Kind() != KindTrigger {
		t.Fatalf("Flush 后旧副本被污染：%+v", old)
	}
	if !m.IsEmpty() {
		t.Fatal("Flush 后内部 buffer 应为空")
	}
}

// TestChannelManager_Flush只重算状态改变的节点 重复汇聚信号不把已消费节点重新激活。
// Python: channels.py:47-58（changed 为 false 时不进 updated_nodes）
func TestChannelManager_Flush只重算状态改变的节点(t *testing.T) {
	bar := NewBarrierChannel("collect", [][]string{{"A"}, {"B"}})
	m := NewChannelManager([]Channel{bar})

	m.BufferMessage(NewBarrierMessage("A", bar.Key()))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	m.Consume("collect")

	m.BufferMessage(NewBarrierMessage("A", bar.Key()))
	m.BufferMessage(NewBarrierMessage("B", bar.Key()))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if got := m.GetReadyNodes(); len(got) != 1 {
		t.Fatalf("就绪集 = %v，期望 [collect]", got)
	}
	m.Consume("collect")

	// 直接改通道内部状态，制造"已收满但 Accept 返 false"的场景
	bar.Restore(ChannelSnapshot{Received: []string{"A", "B"}})
	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("Restore 通道不应影响管理器就绪集，实际 %v", got)
	}

	m.BufferMessage(NewBarrierMessage("A", bar.Key()))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("重复信号（Accept 返 false）不应把节点加回就绪集，实际 %v", got)
	}
}

// TestChannelManager_Snapshot跳过END END 虚拟节点的通道不进快照。
// Python: channels.py:79-80
func TestChannelManager_Snapshot跳过END(t *testing.T) {
	ch := NewTriggerChannel(End)
	ch.Accept(NewTriggerMessage("s", End))
	m := NewChannelManager([]Channel{ch})

	if _, ok := m.Snapshot()[End]; ok {
		t.Fatalf("快照不应包含 END 节点，实际 %v", m.Snapshot())
	}
}

// TestChannelManager_Snapshot空节点不存 全空节点整条不出现。
// Python: channels.py:82-84（has_state 判定）
func TestChannelManager_Snapshot空节点不存(t *testing.T) {
	trig := NewTriggerChannel("idle")
	m := NewChannelManager([]Channel{trig})
	if got := m.Snapshot(); len(got) != 0 {
		t.Fatalf("快照 = %v，期望空", got)
	}
}

// TestChannelManager_Snapshot空管理器 无通道时返回空 map（非 nil）。
func TestChannelManager_Snapshot空管理器(t *testing.T) {
	m := NewChannelManager(nil)
	snap := m.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot 应返回非 nil map")
	}
	if len(snap) != 0 {
		t.Fatalf("快照 = %v，期望空", snap)
	}
}

// TestChannelManager_Snapshot保留空占位 有任一通道非空时，同节点其余通道的空快照必须按位保留。
// Python: channels.py:81-84（整串存储，restore 靠下标 zip）
func TestChannelManager_Snapshot保留空占位(t *testing.T) {
	trig := NewTriggerChannel("collect")
	bar := NewBarrierChannel("collect", [][]string{{"A"}})
	m := NewChannelManager([]Channel{trig, bar})
	bar.Accept(NewBarrierMessage("A", bar.Key()))

	snap := m.Snapshot()
	states, ok := snap["collect"]
	if !ok {
		t.Fatalf("快照缺少 collect 节点，实际 %v", snap)
	}
	if len(states) != 2 {
		t.Fatalf("通道快照数 = %d，期望 2（空占位必须保留）", len(states))
	}
	if len(states[0].Messages) != 0 {
		t.Errorf("下标 0（触发通道）快照 = %+v，期望空", states[0])
	}
	if len(states[1].Received) != 1 || states[1].Received[0] != "A" {
		t.Errorf("下标 1（汇聚通道）快照 = %+v，期望 Received=[A]", states[1])
	}
}

// TestChannelManager_快照恢复往返 快照→新管理器→恢复后节点重新就绪。
func TestChannelManager_快照恢复往返(t *testing.T) {
	bar := NewBarrierChannel("collect", [][]string{{"A"}, {"B"}})
	m1 := NewChannelManager([]Channel{bar})
	m1.BufferMessage(NewBarrierMessage("A", bar.Key()))
	m1.BufferMessage(NewBarrierMessage("B", bar.Key()))
	if err := m1.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	snap := m1.Snapshot()

	bar2 := NewBarrierChannel("collect", [][]string{{"A"}, {"B"}})
	m2 := NewChannelManager([]Channel{bar2})
	m2.Restore(snap)

	if got := m2.GetReadyNodes(); len(got) != 1 || got[0] != "collect" {
		t.Fatalf("恢复后就绪集 = %v，期望 [collect]", got)
	}
}

// TestChannelManager_Restore长度不匹配静默 通道数变了就整节点跳过，不 panic 不报错（偏差 D-5）。
// Python: channels.py:93-94
func TestChannelManager_Restore长度不匹配静默(t *testing.T) {
	trig := NewTriggerChannel("collect")
	bar := NewBarrierChannel("collect", [][]string{{"A"}})
	m := NewChannelManager([]Channel{trig, bar})

	m.Restore(map[string][]ChannelSnapshot{
		"collect": {{Received: []string{"A"}}},
	})

	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("长度不匹配时应静默跳过，就绪集实际 %v", got)
	}
	if bar.IsReady() {
		t.Fatal("长度不匹配时不应调用通道的 Restore")
	}
}

// TestChannelManager_Restore未知节点静默 快照里的陌生节点被忽略，其余正常恢复（偏差 D-5）。
// Python: channels.py:90
func TestChannelManager_Restore未知节点静默(t *testing.T) {
	bar := NewBarrierChannel("collect", [][]string{{"A"}})
	m := NewChannelManager([]Channel{bar})

	m.Restore(map[string][]ChannelSnapshot{
		"ghost":   {{Received: []string{"Z"}}},
		"collect": {{Received: []string{"A"}}},
	})

	if got := m.GetReadyNodes(); len(got) != 1 || got[0] != "collect" {
		t.Fatalf("就绪集 = %v，期望 [collect]（未知节点被忽略）", got)
	}
}

// TestChannelManager_Restore空快照跳过 空快照不调用通道的 Restore、不加就绪集（偏差 D-5）。
// Python: channels.py:96-98
func TestChannelManager_Restore空快照跳过(t *testing.T) {
	bar := NewBarrierChannel("collect", [][]string{{"A"}})
	m := NewChannelManager([]Channel{bar})

	m.Restore(map[string][]ChannelSnapshot{"collect": {{}}})

	if bar.IsReady() {
		t.Fatal("空快照不应使通道就绪")
	}
	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("就绪集 = %v，期望空", got)
	}
}

// TestBarrierChannel_检查点恢复不死锁 旧检查点 + CNF 分组自动修复死锁。
// Python: test_barrier_cnf.py:139-153
func TestBarrierChannel_检查点恢复不死锁(t *testing.T) {
	bar := NewBarrierChannel("end", [][]string{{"LLM_3"}, {"LLM_1", "LLM_2"}})
	m := NewChannelManager([]Channel{bar})

	m.Restore(map[string][]ChannelSnapshot{
		"end": {{Received: []string{"LLM_1", "LLM_3"}}},
	})

	if !bar.IsReady() {
		t.Fatal("LLM_2 永不到来时，OR 组仍应被 LLM_1 满足")
	}
	if got := m.GetReadyNodes(); len(got) != 1 || got[0] != "end" {
		t.Fatalf("就绪集 = %v，期望 [end]", got)
	}
}
