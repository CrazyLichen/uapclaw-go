package pregel

import (
	"sort"
	"strings"
)

// 依赖说明：合成路由键需要 sort（组内升序）与 strings（Join）；
// Task 3 会再加入 fmt（Flush 的错误文案）。Go 对未使用 import 报编译错误，故按需加入。

// ──────────────────────────── 结构体 ────────────────────────────

// Channel 通道接口：节点之间的调度边在运行时的化身。
//
// 归属说明：本接口在 Python 里定义于 pregel/base.py:53-84，计划表原归 8.3；
// 2026-11-01 拍板方案 B，改由本节（channels.go）提供——接口的七个方法只有
// TriggerChannel/BarrierChannel 两个实现，Accept 返回 bool、Consume 无返回值
// 这些形状只有看完实现才能定（偏差 D-1），放在同一文件里才不会锁错签名。
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

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口断言：TriggerChannel 必须满足 Channel。
var _ Channel = (*TriggerChannel)(nil)

// 编译期接口断言：BarrierChannel 必须满足 Channel。
var _ Channel = (*BarrierChannel)(nil)

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
// Python: channels.py:147-149
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

// ──────────────────────────── 非导出函数 ────────────────────────────

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
