# 8.4 ChannelManager / TriggerChannel / BarrierChannel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/agentcore/graph/pregel` 包落地 Pregel 的通道层——`Channel` 接口、`ChannelSnapshot` 快照、`TriggerChannel`／`BarrierChannel` 两个实现与 `ChannelManager`，把 8.5 产出的 `Message` 落进通道并回答「下一个超步该跑哪些节点」。

**Architecture:** `Channel` 接口随本节落地（原计划归 8.3，方案 B 已拍板），与两个实现同文件同测试；快照合并为单结构体 `ChannelSnapshot`（对齐 8.6 把三层 Message 合并的先例）；`ChannelManager` 维护「写侧 key→通道」「读侧 节点→通道列表」「就绪节点集」「待 flush 消息」四份状态，全部字段非导出 + 构造函数收口，无锁（对齐 Python，唯一使用者在单 goroutine 内串行调用）。

**Tech Stack:** Go 1.26（标准库 `fmt`／`sort`／`strings`／`testing`），包 `internal/agentcore/graph/pregel`，对齐 Python `openjiuwen/core/graph/pregel/channels.py`（189 行）+ `pregel/base.py:53-84`。

**设计文档：** `docs/superpowers/specs/2026-11-01-pregel-channels-8.4-design.md`

---

## 背景：本节在全局中的位置

Pregel 是超步调度模型。`PregelLoop.run_step` 每轮从 `ChannelManager` 取就绪节点 → `NodeTask` 执行节点函数 → Router（8.5，已完成）产出 `Message` → 消息回到 `ChannelManager` → `Flush` 决定下一超步的 `active_nodes`。通道是**边在运行时的化身**，`ChannelManager` 是**超步之间唯一的调度状态机**。

```
PregelLoop.init()   → manager.restore(state.channel_values)             engine.py:45
                    → buffer_message(TriggerMessage(__start__)) + flush() engine.py:59-60
PregelLoop.run_step():
  ① ready = manager.get_ready_nodes()                                   engine.py:111
  ② manager.consume(n)                                                  engine.py:133-135
  ③ executor.submit(node) → func + routers.Dispatch                     （8.3b / 8.5）
  ④ buffer_message(msg) + flush()                                       engine.py:150-152
出错 → manager.snapshot() → GraphState.channel_values                    engine.py:180-188
```

**章节顺序**：本节原计划排在 8.3 之后，因 `Channel` 接口移入本节（方案 B），8.4 与 8.3 已互不依赖，实际实施序变为 `8.5 ✅ → 8.4 → 8.3 → 8.8 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b`。

**硬约束（实施时不得"优化"掉的 Python 语义）**：
1. `Flush` 找不到目标通道时**返回 error 且不清 buffer**（Python 的 `buffer.clear()` 在循环之后，异常路径不可达）；
2. `Flush` 只对 `Accept` 返 true 的节点重算就绪（重复 barrier 信号不重新激活节点）；
3. `Snapshot` 只要节点有**任一**通道非空就存**整串**通道快照，空占位必须保留（`Restore` 靠下标 zip）；
4. `Snapshot` 跳过 `pregel.End`；
5. `Restore` 三类**静默**忽略（节点不存在 / 长度不等 / 单快照为空），不 panic 不报错；
6. `BarrierChannel` 空组保留（永久不就绪）、重复 key 后者覆盖——均为有意复刻，注释须标明。

---

## 文件结构

| 文件 | 职责 | 动作 |
|------|------|------|
| `internal/agentcore/graph/pregel/channels.go` | `Channel` 接口 + `ChannelSnapshot` + `TriggerChannel` + `BarrierChannel` + `ChannelManager` + `makeBarrierRouterKey` | 新建 |
| `internal/agentcore/graph/pregel/channels_test.go` | 上者单元测试（17 个 Python 等价用例 + 26 个 Go 追加用例），目标 `channels.go` 覆盖率 100% | 新建 |
| `internal/agentcore/graph/pregel/doc.go` | 目录树补 `channels.go`、概述补通道职责与并发纪律、Python 路径补 `channels.py` | 修改 |
| `IMPLEMENTATION_PLAN.md` | 8.4 行 ✅、8.3 行归属说明、领域八表格后追加顺序注记 | 修改 |
| `docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md` | 首段消费方 + §9 两处追加兑现说明 | 修改 |
| `docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md` | 首段消费方 + R2 行追加兑现说明 | 修改 |
| `docs/superpowers/specs/2026-10-31-pregel-config-8.3d-design.md` | 首段「8.4（channels 跳过 END）」追加兑现说明 | 修改 |

**`channels.go` 的声明顺序**（规范 2。Task 1-4 按此顺序追加，每次插在「最后一个已存在的同类声明之后」，不做后续搬家）：

```
结构体：Channel（接口在最前）→ ChannelSnapshot → TriggerChannel → BarrierChannel → ChannelManager
全局变量：2 条编译期接口断言（TriggerChannel / BarrierChannel）
导出函数：NewTriggerChannel → TriggerChannel 的 7 个方法
          → NewBarrierChannel → BarrierChannel 的 7 个方法
          → NewChannelManager → ChannelManager 的 8 个方法
          （BufferMessage / IsEmpty / Flush / GetReadyNodes / Consume / Buffer / Snapshot / Restore）
非导出函数：makeBarrierRouterKey
```

> 说明：`makeBarrierRouterKey` 在 Task 2 就追加到文件末尾（非导出函数区），Task 3/4 的所有新导出函数都必须插在它**之前**。

---

### Task 1: `Channel` 接口 + `ChannelSnapshot` + `TriggerChannel`

**Files:**
- Create: `internal/agentcore/graph/pregel/channels.go`
- Create: `internal/agentcore/graph/pregel/channels_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/agentcore/graph/pregel/channels_test.go`：

```go
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

// TestTriggerChannel_零值消息被拒 零值 kind（空串）等价 Python 基类 Message 实例，两通道都拒收。
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
```

- [ ] **Step 2: 跑测试确认失败（编译错误）**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestTriggerChannel|TestChannel_' -v 2>&1 | head -20
```

期望：编译失败，`undefined: NewTriggerChannel` / `undefined: Channel`。

- [ ] **Step 3: 写最小实现**

创建 `internal/agentcore/graph/pregel/channels.go`：

```go
package pregel

// 本 Task 不需要任何 import（无 fmt/sort/strings 调用）；
// Task 2 引入 `sort`/`strings`，Task 3 引入 `fmt`——Go 对未使用 import 报编译错误，
// 分步加入即可。

// ──────────────────────────── 结构体 ────────────────────────────

// Channel 通道接口：节点之间的调度边在运行时的化身。
//
// 归属说明：本接口在 Python 里定义于 pregel/base.py:53-84，计划表原归 8.3；
// 2026-11-01 拍板方案 B，改由本节（channels.go）提供——接口的七个方法只有
// TriggerChannel/BarrierChannel 两个实现，Accept 返回 bool、Consume 无返回值
// 这些形状只有看完实现才能定（见偏差 D-1），放在同一文件里才不会锁错签名。
//
// Python: pregel/base.py:53-84（ABC + key/node_name 两个 property）
type Channel interface {
	// Key 写侧路由键：TriggerChannel 即节点名；BarrierChannel 为 "barrier:...->name" 合成键
	Key() string
	// NodeName 读侧节点名
	NodeName() string
	// IsReady 是否已可激活目标节点
	IsReady() bool
	// Accept 收下消息，返回内部状态是否改变
	//
	// 偏差 D-1：Python ABC 注解为 None，但两个实现均返回 bool，Go 按实现取值。
	Accept(msg Message) bool
	// Consume 消费并复位内部状态
	//
	// 偏差 D-1：Python ABC 注解为 Any 且文档称"返回节点可消费的输入"，但两个实现
	// 均无返回值（只清空内部状态），Go 按实现取值。
	Consume()
	// Snapshot 导出快照
	//
	// 偏差 D-2：Python 两个通道的快照是异构的（List[TriggerMessage] / List[str]），
	// Go 合并为单结构体 ChannelSnapshot。
	Snapshot() ChannelSnapshot
	// Restore 从快照恢复
	//
	// 偏差 D-5：Python 无返回值、无任何校验；Go 忠实复刻为无返回值。
	Restore(state ChannelSnapshot)
}

// ChannelSnapshot 通道快照，每个通道只填自己那一半。
//
// 空判定 = 两字段皆空（等价 Python 的 `state is not None and state != [] and state != {}`，
// 两个实现只会产出 list，故空即 len==0）。
//
// 顺序纪律：Messages 保序；Received 升序（偏差 D-4，Python 是 set 序）。
// Python: channels.py:122-123 / :185-186 的返回形状
type ChannelSnapshot struct {
	// Messages TriggerChannel 的「已接收未消费」触发消息
	Messages []Message
	// Received BarrierChannel 的「已到达 sender」集合（升序）
	Received []string
}

// TriggerChannel 触发通道：收到触发消息即激活目标节点（1→N 静态边的读侧）。
//
// Python: channels.py:104-126
type TriggerChannel struct {
	// name 节点名，同时用作 Key 与 NodeName
	name string
	// messages 已接收未消费的触发消息
	messages []Message
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口断言：TriggerChannel 必须满足 Channel。
var _ Channel = (*TriggerChannel)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTriggerChannel 构造触发通道，name 同时作 Key 与 NodeName。
// Python: channels.py:105-107
func NewTriggerChannel(name string) *TriggerChannel {
	return &TriggerChannel{name: name, messages: make([]Message, 0)}
}

// Key 返回写侧路由键，即节点名。
// Python: channels.py:57-59（基类 property）
func (c *TriggerChannel) Key() string { return c.name }

// NodeName 返回读侧节点名。
// Python: channels.py:61-63（基类 property）
func (c *TriggerChannel) NodeName() string { return c.name }

// IsReady 有未消费消息即就绪。
// Python: channels.py:109-110
func (c *TriggerChannel) IsReady() bool { return len(c.messages) > 0 }

// Accept 收下触发消息。
//
// 只接受 KindTrigger；零值 kind（空串）等价 Python 基类 Message 实例，被拒绝。
// Python: channels.py:112-116
func (c *TriggerChannel) Accept(msg Message) bool {
	if msg.Kind() != KindTrigger {
		return false
	}
	c.messages = append(c.messages, msg)
	return true
}

// Consume 清空已接收消息（节点结果载荷通常为空，无"可消费输入"概念）。
// Python: channels.py:118-120
func (c *TriggerChannel) Consume() {
	clear(c.messages)
	c.messages = c.messages[:0]
}

// Snapshot 导出消息列表副本（保序）。
// Python: channels.py:122-123
func (c *TriggerChannel) Snapshot() ChannelSnapshot {
	return ChannelSnapshot{Messages: append([]Message(nil), c.messages...)}
}

// Restore 用快照替换消息列表副本。
// Python: channels.py:125-126
func (c *TriggerChannel) Restore(state ChannelSnapshot) {
	c.messages = append([]Message(nil), state.Messages...)
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestTriggerChannel|TestChannel_' -v
```

期望：6 个用例全部 PASS（`TestTriggerChannel_接收触发消息` / `_拒绝汇聚消息` / `_零值消息被拒` / `_消费复位` / `_快照恢复`、`TestChannel_接口断言`）。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/graph/pregel/channels.go internal/agentcore/graph/pregel/channels_test.go && git commit -m "feat(8.4): Channel 接口 + ChannelSnapshot + TriggerChannel"
```

---

### Task 2: `BarrierChannel`（CNF 判定 + 合成路由键）

**Files:**
- Modify: `internal/agentcore/graph/pregel/channels.go`（在 `TriggerChannel` 之后插入 `BarrierChannel` 结构体；在导出函数区末尾追加其方法；在文件末尾追加 `makeBarrierRouterKey`）
- Modify: `internal/agentcore/graph/pregel/channels_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

在 `channels_test.go` 末尾追加：

```go
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
// Python: channels.py:166-172（to for group in [] 直接返回 True）
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
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestBarrierChannel|TestNewBarrierChannel' 2>&1 | head -10
```

期望：编译失败，`undefined: NewBarrierChannel`。

- [ ] **Step 3: 写实现**

先把 `channels.go` 顶部的 import 块补成（`makeBarrierRouterKey` 需要 `sort`/`strings`）：

```go
import (
	"sort"
	"strings"
)
```

再在 `channels.go` 的 `TriggerChannel` 结构体之后插入：

```go
// BarrierChannel 汇聚通道：N→1 边的读侧，按 CNF（AND-of-OR）表达式判定就绪。
//
// groups 每个元素是一个 OR 组：组内任一 sender 到达即算该组满足，所有组都满足
// 且至少收到一个信号才算就绪。
//
//	[][]string{{"a"}, {"b"}}      → a AND b（向后兼容形态）
//	[][]string{{"a", "b"}}        → a OR b
//	[][]string{{"a", "b"}, {"c"}} → (a OR b) AND c
//
// Key 不是节点名而是合成路由键 "barrier:(a|b)&c->target"：8.3a 的 builder 会把它
// 转手给 NewBarrierRouter 作为消息目标（见 8.5 spec §9）。
//
// Python: channels.py:129-189
type BarrierChannel struct {
	// name 目标节点名（NodeName 返回它）
	name string
	// groups OR 组集合（构造期按 Python Set 语义去重，偏差 D-9）
	groups []map[string]struct{}
	// received 已到达的 sender 集合（Set 语义，去重 ⇒ 同超步重复信号幂等）
	received map[string]struct{}
	// routerKey 构造期算好的合成路由键（对齐 Python 在 __init__ 里算 key）
	routerKey string
}
```

在全局变量区追加（放在已有的 TriggerChannel 断言之后）：

```go
// 编译期接口断言：BarrierChannel 必须满足 Channel。
var _ Channel = (*BarrierChannel)(nil)
```

在导出函数区末尾（`TriggerChannel.Restore` 之后）追加：

```go
// NewBarrierChannel 构造汇聚通道。
//
// 入参深复制并逐组去重（Python 的 expected_groups 是 List[Set]，重复元素天然消失；
// Go 用 [][]string 入参必须显式去重，偏差 D-9）。
// Python: channels.py:141-145
func NewBarrierChannel(nodeName string, expectedGroups [][]string) *BarrierChannel {
	groups := make([]map[string]struct{}, 0, len(expectedGroups))
	for _, group := range expectedGroups {
		set := make(map[string]struct{}, len(group))
		for _, sender := range group {
			set[sender] = struct{}{}
		}
		groups = append(groups, set)
	}
	ch := &BarrierChannel{
		name:     nodeName,
		groups:   groups,
		received: make(map[string]struct{}),
	}
	ch.routerKey = makeBarrierRouterKey(nodeName, groups)
	return ch
}

// Key 返回合成路由键（覆盖基类的"节点名"语义）。
// Python: channels.py:147-153
func (c *BarrierChannel) Key() string { return c.routerKey }

// NodeName 返回目标节点名。
// Python: channels.py:151-153
func (c *BarrierChannel) NodeName() string { return c.name }

// IsReady CNF 判定：received 非空，且每个 OR 组至少命中一个 sender。
//
// 空组（构造时传入空切片）永远无法满足 ⇒ 永久不就绪，属构图错误，有意复刻
// Python 行为而不在构造期 panic（偏差 D-6）。
// Python: channels.py:166-172
func (c *BarrierChannel) IsReady() bool {
	if len(c.received) == 0 {
		return false
	}
	for _, group := range c.groups {
		hit := false
		for sender := range group {
			if _, ok := c.received[sender]; ok {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// Accept 收下汇聚信号。
//
// 只接受 KindBarrier；同一 sender 重复到达返回 false（Set 去重），
// 因此不会把节点重新标记为 updated。
// Python: channels.py:174-179
func (c *BarrierChannel) Accept(msg Message) bool {
	if msg.Kind() != KindBarrier {
		return false
	}
	if _, ok := c.received[msg.Sender]; ok {
		return false
	}
	c.received[msg.Sender] = struct{}{}
	return true
}

// Consume 清空已到达 sender 集合。
// Python: channels.py:181-183
func (c *BarrierChannel) Consume() {
	clear(c.received)
}

// Snapshot 导出 sender 集合的升序副本（偏差 D-4：Python 是 set 序）。
// Python: channels.py:185-186
func (c *BarrierChannel) Snapshot() ChannelSnapshot {
	senders := make([]string, 0, len(c.received))
	for sender := range c.received {
		senders = append(senders, sender)
	}
	sort.Strings(senders)
	return ChannelSnapshot{Received: senders}
}

// Restore 用快照重建 sender 集合。
// Python: channels.py:188-189
func (c *BarrierChannel) Restore(state ChannelSnapshot) {
	c.received = make(map[string]struct{}, len(state.Received))
	for _, sender := range state.Received {
		c.received[sender] = struct{}{}
	}
}
```

在文件末尾（非导出函数区）追加：

```go
// makeBarrierRouterKey 生成 CNF 合成路由键："barrier:(a|b)&c->target"。
//
// 规则：单元素组直出该元素；多元素组渲染成 "(A|B)" 且组内升序；组间用 & 连接；
// 空组渲染成 "()"（该组永不满足）；groups 为空得到 "barrier:->target"。
//
// 唯一生产者：8.3a 的 builder 只把返回值转手给 NewBarrierRouter（见 8.5 spec §9）。
// Python: channels.py:155-164
func makeBarrierRouterKey(name string, groups []map[string]struct{}) string {
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		members := make([]string, 0, len(group))
		for member := range group {
			members = append(members, member)
		}
		if len(members) == 1 {
			parts = append(parts, members[0])
			continue
		}
		sort.Strings(members)
		parts = append(parts, "("+strings.Join(members, "|")+")")
	}
	return "barrier:" + strings.Join(parts, "&") + "->" + name
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestBarrierChannel|TestNewBarrierChannel' -v 2>&1 | tail -45
```

期望：19 个 `TestBarrierChannel*` / `TestNewBarrierChannel*` 用例全部 PASS。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/graph/pregel/channels.go internal/agentcore/graph/pregel/channels_test.go && git commit -m "feat(8.4): BarrierChannel CNF 判定与合成路由键"
```

---

### Task 3: `ChannelManager` 索引与超步接口

**Files:**
- Modify: `internal/agentcore/graph/pregel/channels.go`
- Modify: `internal/agentcore/graph/pregel/channels_test.go`

- [ ] **Step 1: 写失败测试**

在 `channels_test.go` 末尾追加：

```go
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

// TestChannelManager_Buffer引用语义 Buffer 返回内部切片引用（偏差 D-9 的交棒约束）。
func TestChannelManager_Buffer引用语义(t *testing.T) {
	m := NewChannelManager(nil)
	m.BufferMessage(NewTriggerMessage("a", "b"))
	buf := m.Buffer()
	if len(buf) != 1 {
		t.Fatalf("buffer 长度 = %d，期望 1", len(buf))
	}
	buf[0] = NewTriggerMessage("x", "y")
	if got := m.Buffer()[0].Sender; got != "x" {
		t.Fatalf("Buffer 应返回内部切片引用，实际见到 %q", got)
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

	// 重复信号：sender A 已消费过，但 received 已清空 ⇒ 仍算 changed，此处改用 B 已收满后重复
	m.BufferMessage(NewBarrierMessage("A", bar.Key()))
	m.BufferMessage(NewBarrierMessage("B", bar.Key()))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if got := m.GetReadyNodes(); len(got) != 1 {
		t.Fatalf("就绪集 = %v，期望 [collect]", got)
	}
	m.Consume("collect")

	// 同一条重复消息不会再激活（Accept 返 false）
	bar.Restore(ChannelSnapshot{Received: []string{"A", "B"}})
	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("Restore 不应自动改就绪集之外的状态，实际 %v", got)
	}

	m.BufferMessage(NewBarrierMessage("A", bar.Key()))
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush 返回错误：%v", err)
	}
	if got := m.GetReadyNodes(); len(got) != 0 {
		t.Fatalf("重复信号（Accept 返 false）不应把节点加回就绪集，实际 %v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestChannelManager' 2>&1 | head -10
```

期望：编译失败，`undefined: NewChannelManager`。

- [ ] **Step 3: 写实现**

先把 `channels.go` 顶部的 import 块补成（`Flush` 的错误文案需要 `fmt`）：

```go
import (
	"fmt"
	"sort"
	"strings"
)
```

再在 `channels.go` 的 `BarrierChannel` 结构体之后插入：

```go
// ChannelManager 超步调度的通道管理器：写侧按 msg.Target 路由、读侧按节点名聚合。
//
// 四份状态：
//   - keyToChannel：路由键 → 通道（写侧检索，重复键后者覆盖，偏差 D-7）
//   - nodeToChannels：节点名 → 通道列表（读侧检索，注册顺序；Snapshot/Restore 的下标对齐依赖它）
//   - readyNodes：就绪节点集合（GetReadyNodes 排序后输出，偏差 D-4）
//   - buffer：待 Flush 的消息
//
// 并发纪律（偏差 D-8）：无锁，对齐 Python。唯一使用者是 PregelLoop，它在超步循环
// 所在 goroutine 内串行调用本类型全部方法；节点任务在其他 goroutine 执行，但
// 不得触碰 ChannelManager。
//
// Python: channels.py:13-101
type ChannelManager struct {
	// keyToChannel 写侧索引：路由键 → 通道
	keyToChannel map[string]Channel
	// nodeToChannels 读侧索引：节点名 → 通道列表（注册顺序）
	nodeToChannels map[string][]Channel
	// readyNodes 就绪节点集合
	readyNodes map[string]struct{}
	// buffer 待下一超步 Flush 的消息
	buffer []Message
}
```

在导出函数区**末尾、`makeBarrierRouterKey` 之前**插入构造函数（即 `BarrierChannel.Restore` 之后）：

```go
// NewChannelManager 按通道列表建索引。
//
// 构造时即扫描一次 IsReady()，把已有数据的通道所属节点放进就绪集
// （对齐 Python 的 `# recover` 分支，channels.py:29-31）。
// Python: channels.py:14-31
func NewChannelManager(channels []Channel) *ChannelManager {
	m := &ChannelManager{
		keyToChannel:   make(map[string]Channel, len(channels)),
		nodeToChannels: make(map[string][]Channel, len(channels)),
		readyNodes:     make(map[string]struct{}),
		buffer:         make([]Message, 0),
	}
	for _, ch := range channels {
		m.keyToChannel[ch.Key()] = ch
		m.nodeToChannels[ch.NodeName()] = append(m.nodeToChannels[ch.NodeName()], ch)
		if ch.IsReady() {
			m.readyNodes[ch.NodeName()] = struct{}{}
		}
	}
	return m
}
```

在 `NewChannelManager` 之后（仍在 `makeBarrierRouterKey` 之前）追加方法：

```go
// BufferMessage 把消息暂存到 buffer，等待下一次 Flush。
// Python: channels.py:33-34
func (m *ChannelManager) BufferMessage(msg Message) {
	m.buffer = append(m.buffer, msg)
}

// IsEmpty buffer 是否为空（只看 buffer，不看就绪集）。
// Python: channels.py:36-37
func (m *ChannelManager) IsEmpty() bool { return len(m.buffer) == 0 }

// Flush 把 buffer 里的消息投递给各自目标通道，并按需更新就绪集。
//
// 语义要点（逐条对齐 channels.py:39-58）：
//   - 找不到目标通道时返回 error 且**不清空 buffer**（Python 的 clear 在循环之后，
//     异常路径不可达，偏差 D-3）；
//   - 只有 Accept 返 true 的节点才重算就绪——重复的汇聚信号不会把节点重新激活；
//   - 重算口径是「该节点任一通道就绪即入集」，不是"全部通道就绪"。
func (m *ChannelManager) Flush() error {
	updated := make(map[string]struct{}, len(m.buffer))
	for _, msg := range m.buffer {
		ch, ok := m.keyToChannel[msg.Target]
		if !ok {
			return fmt.Errorf("通道未找到，目标路由键=%q", msg.Target)
		}
		if ch.Accept(msg) {
			updated[ch.NodeName()] = struct{}{}
		}
	}
	clear(m.buffer)
	m.buffer = m.buffer[:0]

	for nodeName := range updated {
		for _, ch := range m.nodeToChannels[nodeName] {
			if ch.IsReady() {
				m.readyNodes[nodeName] = struct{}{}
				break
			}
		}
	}
	return nil
}

// GetReadyNodes 返回就绪节点名（升序）。
//
// 偏差 D-4：Python 返回 list(set)，顺序无跨进程契约；Go 的 map 迭代是 per-iteration
// 随机，因此排序是唯一能给出稳定结果的选项。顺序不携带语义——同一超步内所有就绪
// 节点都会被提交执行。
// Python: channels.py:60-61
func (m *ChannelManager) GetReadyNodes() []string {
	nodes := make([]string, 0, len(m.readyNodes))
	for nodeName := range m.readyNodes {
		nodes = append(nodes, nodeName)
	}
	sort.Strings(nodes)
	return nodes
}

// Consume 消费节点已就绪的通道，并把节点移出就绪集。
//
// 未知节点直接返回（对齐 Python channels.py:64-65 的存在性判断，不是 defaultdict）。
// Python: channels.py:63-74
func (m *ChannelManager) Consume(nodeName string) {
	channels, ok := m.nodeToChannels[nodeName]
	if !ok {
		return
	}
	for _, ch := range channels {
		if ch.IsReady() {
			ch.Consume()
		}
	}
	delete(m.readyNodes, nodeName)
}

// Buffer 返回内部 buffer 切片**引用**（对齐 Python 直接读 self.buffer 的别名语义，
// 偏差 D-9）。
//
// 调用方纪律：需要"buffer + 额外消息"时**必须**先复制（
// `append(append([]Message(nil), m.Buffer()...), extras...)`），不得直接 append
// 到返回值上——Python 的 `pending_buffer.extend(...)` 实际改写了管理器内部状态，
// 那是别名副作用，Go 侧不许复刻。
// Python: engine.py:180（self.manager.buffer）
func (m *ChannelManager) Buffer() []Message { return m.buffer }
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestChannelManager' -v 2>&1 | tail -35
```

期望：12 个 `TestChannelManager*` 用例全部 PASS（本 Task 不含 `Snapshot`/`Restore` 断言，全部可签）。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/graph/pregel/channels.go internal/agentcore/graph/pregel/channels_test.go && git commit -m "feat(8.4): ChannelManager 索引与超步接口（Buffer/Flush/Ready/Consume）"
```

---

### Task 4: `ChannelManager` 快照与恢复

**Files:**
- Modify: `internal/agentcore/graph/pregel/channels.go`
- Modify: `internal/agentcore/graph/pregel/channels_test.go`

- [ ] **Step 1: 写失败测试**

在 `channels_test.go` 末尾追加：

```go
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
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestChannelManager_Snapshot|TestChannelManager_Restore|TestChannelManager_快照|TestBarrierChannel_检查点' 2>&1 | head -10
```

期望：编译失败，`m.Snapshot undefined` / `m.Restore undefined`。

- [ ] **Step 3: 写实现**

在 `channels.go` 的 `ChannelManager.Buffer` 之后追加：

```go
// Snapshot 导出全部通道快照。
//
// 语义要点（逐条对齐 channels.py:76-86）：
//   - 跳过 pregel.End 节点；
//   - 每节点按 nodeToChannels 顺序存**全部**通道快照——只要任一条非空就整串存储，
//     空快照作为占位保留（Restore 靠下标 zip，占位不可省）；
//   - 整节点全空则不存该节点。
func (m *ChannelManager) Snapshot() map[string][]ChannelSnapshot {
	snap := make(map[string][]ChannelSnapshot, len(m.nodeToChannels))
	for nodeName, channels := range m.nodeToChannels {
		if nodeName == End {
			continue
		}
		states := make([]ChannelSnapshot, 0, len(channels))
		hasState := false
		for _, ch := range channels {
			state := ch.Snapshot()
			if len(state.Messages) > 0 || len(state.Received) > 0 {
				hasState = true
			}
			states = append(states, state)
		}
		if hasState {
			snap[nodeName] = states
		}
	}
	return snap
}

// Restore 用快照恢复通道状态。
//
// 偏差 D-5（用户 2026-11-01 拍板"都先按照 Python 实现"）：三类情况**静默忽略**，
// 不返回 error、不 panic——
//  1. 快照里的节点在当前图中不存在；
//  2. 该节点的通道数与快照长度不等（图结构变更）
//  3. 单条通道快照为空（两字段皆空）
//
// 恢复后若通道就绪，把节点加入就绪集（对应 channels.py:100-101）。
// Python: channels.py:88-101
func (m *ChannelManager) Restore(snapshot map[string][]ChannelSnapshot) {
	for nodeName, states := range snapshot {
		channels, ok := m.nodeToChannels[nodeName]
		if !ok {
			continue
		}
		if len(channels) != len(states) {
			continue
		}
		for i, ch := range channels {
			state := states[i]
			if len(state.Messages) == 0 && len(state.Received) == 0 {
				continue
			}
			ch.Restore(state)
			if ch.IsReady() {
				m.readyNodes[nodeName] = struct{}{}
			}
		}
	}
}
```

- [ ] **Step 4: 跑测试确认通过 + 覆盖率 100%**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -coverprofile=/tmp/pregel84.out && go tool cover -func=/tmp/pregel84.out | grep channels.go
```

期望：全部用例 PASS，`channels.go` 每个函数都是 `100.0%`。若有函数未达标，按 §7.2 的用例清单补断言（常见缺口：`triggerChannel.Consume` 的 `clear` 分支、`Buffer` 的直接返回）。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/graph/pregel/channels.go internal/agentcore/graph/pregel/channels_test.go && git commit -m "feat(8.4): ChannelManager 快照与恢复（跳过 END、保留空占位、三类静默）"
```

---

### Task 5: 更新 `pregel/doc.go`

**Files:**
- Modify: `internal/agentcore/graph/pregel/doc.go`

- [ ] **Step 1: 概述段补通道职责**

把这两行：

```
// 以及超步之间传递调度消息的 Message、承载中断值的 Interrupt、承载中断跳转的
// GraphInterrupt，与决定下一超步激活哪些节点并产出调度消息的 Router。
```

替换为：

```
// 以及超步之间传递调度消息的 Message、承载中断值的 Interrupt、承载中断跳转的
// GraphInterrupt、产出调度消息的 Router，与落地这些消息并决定下一超步激活哪些
// 节点的 Channel/ChannelManager（就绪判定 + CNF 汇聚 + 检查点快照）。
```

- [ ] **Step 2: 分层纪律补一条并发约定**

在「分层纪律（审查口径）」列表末尾（`recover 点必须用 case *pregel.GraphInterrupt ...` 那一条之后）追加：

```
//   - ChannelManager 无锁（对齐 Python 的 channels.py）：只允许超步循环所在
//     goroutine 串行调用；节点任务在其他 goroutine 执行，但不得触碰 ChannelManager。
```

- [ ] **Step 3: 目录树补 `channels.go`**

把：

```
//	pregel/
//	├── doc.go         # 包文档
//	├── base.go        # Message/MessageKind/构造函数/Kind 与 Interrupt 值对象、GraphInterrupt 中断跳转
//	├── router.go      # Router 接口 + Selector/SelectorFunc + Static/Barrier/Conditional 三个路由实现
//	├── constants.go   # 保留节点名、终止态键、命名空间键名与分隔符
//	└── config.go      # PregelConfig/InnerPregelConfig/CreateInnerConfig/DefaultPregelConfig
```

替换为：

```
//	pregel/
//	├── doc.go         # 包文档
//	├── base.go        # Message/MessageKind/构造函数/Kind 与 Interrupt 值对象、GraphInterrupt 中断跳转
//	├── channels.go    # Channel 接口 + ChannelSnapshot + Trigger/Barrier 通道 + ChannelManager
//	├── router.go      # Router 接口 + Selector/SelectorFunc + Static/Barrier/Conditional 三个路由实现
//	├── constants.go   # 保留节点名、终止态键、命名空间键名与分隔符
//	└── config.go      # PregelConfig/InnerPregelConfig/CreateInnerConfig/DefaultPregelConfig
```

- [ ] **Step 4: 补 Python 路径**

把：

```
// 对应 Python 代码：openjiuwen/core/graph/pregel/base.py · config.py · constants.py · router.py
```

替换为：

```
// 对应 Python 代码：openjiuwen/core/graph/pregel/base.py · channels.py · config.py · constants.py · router.py
```

- [ ] **Step 5: 验证编译与文档渲染**

```bash
cd /home/opensource/uapclaw-gateway && go vet ./internal/agentcore/graph/pregel/ && go doc ./internal/agentcore/graph/pregel 2>&1 | head -40
```

期望：`go vet` 无输出；`go doc` 能打印包注释且目录树显示 6 个文件。

- [ ] **Step 6: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/graph/pregel/doc.go && git commit -m "docs(8.4): pregel 包文档补 channels.go 与并发纪律"
```

---

### Task 6: 计划表与历史 spec 回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:503-504,542`
- Modify: `docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md`
- Modify: `docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md`
- Modify: `docs/superpowers/specs/2026-10-31-pregel-config-8.3d-design.md`

- [ ] **Step 1: 计划表 8.4 行改为已完成**

把 L504 整行：

```
| 8.4 | ☐ | ChannelManager / TriggerChannel / BarrierChannel | 就绪节点集合 + `Buffer/Flush/GetReadyNodes/Consume` + `Snapshot`/`Restore`（跳过 END）+ BarrierChannel 的 CNF(AND-of-OR) 判定与 `barrier:...->name` 路由键 | `openjiuwen/core/graph/pregel/channels.py` |
```

替换为：

```
| 8.4 | ✅ | ChannelManager / TriggerChannel / BarrierChannel | ✅ 就绪节点集合 + `Buffer/Flush/GetReadyNodes/Consume` + `Snapshot`/`Restore`（跳过 END）+ BarrierChannel 的 CNF(AND-of-OR) 判定与 `barrier:...->name` 路由键；Go 事实：`channels.go` 提供 `Channel` 接口(7 方法，⤴️ 自 8.3 移入，方案 B) + `ChannelSnapshot{Messages,Received}` 单结构体 + `TriggerChannel`/`BarrierChannel`（Key 为 `barrier:(A\|B)&C->name` 合成键、组内升序、组内去重）+ `ChannelManager`(重复键后者覆盖/Flush 出错不清 buffer/就绪集与 Received 排序/三类静默 Restore/Buffer 引用暴露)；偏差 D-1..D-11；`channels.go` 覆盖率 100%。设计：docs/superpowers/specs/2026-11-01-pregel-channels-8.4-design.md | `openjiuwen/core/graph/pregel/channels.py` + `pregel/base.py`(53-84) |
```

- [ ] **Step 2: 计划表 8.3 行追加归属说明**

把 L503 该行「产出」列末尾的：

```
| `openjiuwen/core/graph/pregel/base.py` |
```

替换为（保留原描述，只在产出列追加）：

```
；⤴️ `Channel` ABC 已移入 8.4（2026-11-01 方案 B：接口与两个实现同文件同测试），本节只留 `PregelNode` | `openjiuwen/core/graph/pregel/base.py` |
```

- [ ] **Step 3: 追加顺序注记（不动原注记行）**

在 L542 原注记行（`> **8.x 实施顺序（2026-11-01 调整）**：…`）之后插入新行：

```
> **8.x 实施顺序（2026-11-01 二次调整）**：8.5 → 8.4 → 8.3 → 8.8 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b（`Channel` 接口随 8.4 落地后 8.4 与 8.3 互不依赖，故 8.4 前移；依据见 8.4 spec §5）。原注记保留。
```

- [ ] **Step 4: 8.5 spec 三处追加**

在 `2026-11-01-pregel-router-8.5-design.md` 中：

1. 首段 `> 后续消费方：… 8.4（\`ChannelManager.flush\` 按 \`Kind()\` 过滤）` —— 行末追加 `，已兑现（2026-11-01 8.4 落地）`；
2. §9「- **8.3**：`PregelNode` 直接引用本节接口；`Channel` ABC 的 `Accept(msg Message) bool` 依赖 8.6 的 `Kind()`。」 —— 追加一行 `  - **2026-11-01 调整**：`Channel` 接口已移入 8.4（`channels.go`，方案 B），`Accept(msg Message) bool` 的最终形态见 8.4 spec §3.1。`；
3. §9「`barrier:...` 合成键由 8.4 的 `BarrierChannel` 生成，builder 只做转手。」 —— 行末追加 `已兑现：`BarrierChannel.Key()`（`channels.go`），键格式见 8.4 spec §3.4。`

- [ ] **Step 5: 8.6 spec 两处追加**

在 `2026-10-31-pregel-message-interrupt-8.6-design.md` 中：

1. 首段 `> 后续消费方：8.3（\`Channel.Accept(msg Message)\`）、8.4（\`TriggerChannel\`/\`BarrierChannel\` 的 \`Kind()\` 过滤）…` —— 行末追加 `（2026-11-01 更新：`Channel` 接口最终归属 8.4 而非 8.3；`Kind()` 的过滤消费方已在 8.4 兑现）`；
2. 偏差表 R2 行「供 8.4 通道过滤、日志排障、8.9 序列化打标签」 —— 追加 `；8.4 已落地（`TriggerChannel.Accept` / `BarrierChannel.Accept` 均按 `Kind()` 过滤）`。

- [ ] **Step 6: 8.3d spec 一处追加**

在 `2026-10-31-pregel-config-8.3d-design.md` 首段 `> 后续消费方：… 8.4（channels 跳过 END）…` 的行末追加 ` —— 2026-11-01 已兑现：`ChannelManager.Snapshot` 显式跳过 `pregel.End``。

- [ ] **Step 7: 8.9 行追加 ⤵️ 回填点（快照序列化保真）**

在 `IMPLEMENTATION_PLAN.md` 的 8.9 行「产出」列末尾追加（原描述保留）：

```
；⤵️ 8.9b：`GraphState.channel_values` 为 `map[string][]pregel.ChannelSnapshot`，序列化时 `Message.kind` 非导出（JSON 反序列化得零值），必须用 `NewTriggerMessage`/`NewBarrierMessage` 重建 kind（8.4 偏差 D-10）
```

- [ ] **Step 8: `base_test.go` 注释指向真实用例**

`internal/agentcore/graph/pregel/base_test.go:89` 的注释：

```
	// 8.4 通道的过滤条件形式：Kind() == KindTrigger / Kind() == KindBarrier
```

改为：

```
	// 8.4 通道的过滤条件形式：Kind() == KindTrigger / Kind() == KindBarrier
	// 实际用例见 channels_test.go 的 TestTriggerChannel_拒绝汇聚消息 / TestBarrierChannel_拒绝触发消息
```

- [ ] **Step 9: 校验回填完整性**

```bash
cd /home/opensource/uapclaw-gateway && grep -n "8.4" IMPLEMENTATION_PLAN.md | head -5 && grep -c "8.4" docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md docs/superpowers/specs/2026-10-31-pregel-config-8.3d-design.md
```

期望：8.4 行为 ✅；顺序注记出现两行（原行 + 新行）；三份 spec 各含 8.4 引用且计数不少于修改前。

- [ ] **Step 10: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add IMPLEMENTATION_PLAN.md docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md docs/superpowers/specs/2026-10-31-pregel-config-8.3d-design.md internal/agentcore/graph/pregel/base_test.go && git commit -m "docs(8.4): 计划表标记完成 + 8.3 归属说明 + 三份 spec 回填"
```

---

### Task 7: 全量回归与收尾

**Files:**
- 无新增文件；仅验证与可能的格式修正

- [ ] **Step 1: 格式检查（必须用 GOROOT 下的 gofmt）**

```bash
cd /home/opensource/uapclaw-gateway && $(go env GOROOT)/bin/gofmt -l -w internal/agentcore/graph/pregel/
```

期望：无输出（或输出后 `git diff` 仅有格式变化，需与上一步一起提交）。

- [ ] **Step 2: 单包静态检查**

```bash
cd /home/opensource/uapclaw-gateway && go vet ./internal/agentcore/graph/pregel/ && pgrep -f 'go (build|test)' || true
```

期望：`go vet` 无输出；`pgrep` 无并发编译进程（如有先 `pkill -f 'go test'`）。

- [ ] **Step 3: 单包 `-race` 测试与覆盖率**

```bash
cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -race ./internal/agentcore/graph/pregel/ -coverprofile=/tmp/pregel84.out && go tool cover -func=/tmp/pregel84.out | grep -E "channels.go|total"
```

期望：PASS；`channels.go` 每个函数 100%。

- [ ] **Step 4: 全量回归（必须带 CI 同款构建标签）**

```bash
cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./... 2>&1 | tail -20
```

期望：全部包 `ok` 或 `no test files`，**零 FAIL**（8.5 收尾时基线为 294 包）。若出现与本节无关的 FAIL，先 `git fetch` + `git log origin/main -- <file>` 确认是否已被并行线修掉，不要盲改。

- [ ] **Step 5: 最终提交（如有格式修正）**

```bash
cd /home/opensource/uapclaw-gateway && git status --short && git add internal/agentcore/graph/pregel/channels.go internal/agentcore/graph/pregel/channels_test.go && git commit -m "style(8.4): gofmt"
```

期望：`git status` 只剩 `?? internal/swarm/gateway/channel_manager/web/frontend/`（与本任务无关的既有未跟踪目录，**不要** `git add`）。若无格式修正则跳过本步。

---

## 审查口径（供实施后自查）

1. **`Flush` 错误路径**：故意构造错误目标后断言 `len(m.Buffer()) == 1`，确认 buffer 未被清。
2. **空占位保留**：`Snapshot` 的 `len(states)` 必须等于该节点通道数，无论其中几个为空。
3. **跳过 END**：仅有 `End` 节点有数据时，`Snapshot()` 长度为 0。
4. **静默 Restore**：三个"非法"快照都不能 panic、不能返回错误、不能改就绪集（除合法的 ready 补集）。
5. **零日志**：`grep -n "logger\." channels.go` 必须为空（Python 全文零 logger）。
6. **无锁**：`grep -n "sync\." channels.go` 必须为空。
7. **偏差注释齐全**：`grep -n "偏差 D-" channels.go` 至少命中 D-1/D-2/D-3/D-4/D-5/D-6/D-7/D-8/D-9 九处（D-10/D-11 属跨节与不可达分支，记在 spec 与 8.9b 回填点）。
8. **声明顺序**：结构体→全局变量→导出函数→非导出函数，接口在结构体区最前（规范 2）。
9. **`Channel` 接口归属**：8.3 行已补归属说明，8.5/8.6 spec 已补去向，计划表顺序注记出现两行。
10. **`Buffer()` 语义**：返回副本（D-9 实施后修订，见文件末尾「执行修订」），注释里写清"Go 切片无法复刻 Python 活视图别名"的理由，不能只写在 spec。

---

## 执行修订（实施期间，2026-11-01）

1. **`ChannelManager.Buffer()` 由「返回内部切片引用」改为「返回内容副本」**（spec D-9 同步修订）。
   原因：Go 切片赋值只复制 `{ptr,len,cap}` 头，元素数据共享而长度不共享 —— `Flush` 的 `clear` 会把旧视图清零（旧视图 len 仍在、元素全零 ⇒ 存档出幽灵 `Message`），`append` 是否与内部互相覆盖取决于 cap。Python 的 list 别名是活视图（`clear` 后 len 归零），Go 无法等价复刻。
   落地：方法体改 `append(make([]Message, 0, len(m.buffer)), m.buffer...)`；用例 `TestChannelManager_Buffer引用语义` → `TestChannelManager_Buffer返回副本`（断言改副本不污染内部、内部增长不回灌、Flush 后旧副本完好）。
2. **远端并行线导致推送被拒一次**：`pull --rebase` 整合 11 笔 team/itest 提交后重推，8 笔提交 hash 全变（`9e0a9f5a..490eb425` → `5d0d07c2..2a296a50`），内容未变；rebase 后重跑全量 `exit=0`、296 包零 FAIL。
3. **Task 7 Step 5「格式修正提交」未执行**：`gofmt -l -w` 无输出，无差异可提交。
