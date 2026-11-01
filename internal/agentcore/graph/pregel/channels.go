package pregel

// 本文件此刻不需要任何 import；后续分步加入：
//   - Task 2 引入 sort / strings（合成路由键需要）
//   - Task 3 引入 fmt（Flush 的错误文案需要）
// Go 对未使用 import 报编译错误，故按需加入。

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
