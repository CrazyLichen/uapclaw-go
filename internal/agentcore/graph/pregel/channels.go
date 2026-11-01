package pregel

import (
	"fmt"
	"sort"
	"strings"
)

// 依赖说明：fmt 用于 Flush 的错误文案，sort 用于合成路由键与就绪集/快照排序，
// strings 用于 Join。

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
// 调用方纪律：需要"buffer + 额外消息"时**必须**先复制
// （`append(append([]Message(nil), m.Buffer()...), extras...)`），不得直接 append
// 到返回值上——Python 的 `pending_buffer.extend(...)` 实际改写了管理器内部状态，
// 那是别名副作用，Go 侧不许复刻。
// Python: engine.py:180（self.manager.buffer）
func (m *ChannelManager) Buffer() []Message { return m.buffer }

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
//  2. 该节点的通道数与快照长度不等（图结构变更）；
//  3. 单条通道快照为空（两字段皆空）。
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
