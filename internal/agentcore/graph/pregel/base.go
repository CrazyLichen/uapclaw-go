package pregel

// ──────────────────────────── 结构体 ────────────────────────────

// Message 超步之间传递的调度信号。
//
// Python 用三层继承表达（Message → TriggerMessage / BarrierMessage），三者均
// 零行为纯数据、仅靠 isinstance 判别，故 Go 侧合并为单结构体 + 判别式，
// 由 NewTriggerMessage / NewBarrierMessage 收口构造。
// Python: pregel/base.py:29-43
type Message struct {
	// Sender 发送方节点名（Barrier 通道据此判断 OR 组是否已满足）
	Sender string
	// Target 目标通道的路由键（普通通道即节点名；Barrier 通道为 "barrier:a&b->c" 合成键）
	Target string
	// Payload 载荷。Python 声明了该形参（默认 None）但全部调用点均不传，实际恒为 nil；
	// 保留字段以对齐数据形状，当前无消费者
	Payload any
	// kind 消息种类，只能经 NewTriggerMessage / NewBarrierMessage 设置，
	// 包外无法伪造消息种类（零值 kind 会被任何通道拒绝接收）
	kind MessageKind
}

// Interrupt 图中断信号，值对象：随检查点持久化后由 Pregel.Run 返回给上层
// （顶层返回 {"__interrupt__": value}）。
//
// 与 GraphInterrupt（控制流跳转手段，8.7 迁入本包）职责不同：本类型只承载中断值。
//
// 分层纪律：Value 的具体类型由 session 层决定（当前唯一来源是
// session/interaction.InteractionOutputSchema），本包只做装箱透传、从不拆箱。
// 原因：Python 把该字段声明为 Any（pregel/base.py:11），正是为了让 graph 层不依赖
// session 层；Go 侧若收窄为具体类型，pregel 就必须 import session/interaction，
// 而 interaction 已 import 本包，构成编译期循环。类型断言只允许发生在 session 层。
// Python: pregel/base.py:10-14
type Interrupt struct {
	// Value 中断携带的值，由调用方（session 层）构造，本包不读取其内容。
	// 消费链路只做存在性判断，见 doc.go 的分层纪律说明。
	Value any
	// Resumable 是否可恢复中断。Python 类型声明的 __init__ 不接受该形参，
	// 但 interaction.py:62 以关键字传入（运行时 TypeError），保留以承载作者意图
	Resumable bool
	// NS 命名空间（节点 ID），同上，interaction.py:63 以关键字传入
	NS string
}

// ──────────────────────────── 枚举 ────────────────────────────

// MessageKind 消息种类判别式。
//
// 零值（空串）表示未设置种类：此时任何通道都拒绝接收，等价于 Python 基类
// Message 实例不被任一 isinstance 命中。Python 无此概念（靠 isinstance 判别）。
type MessageKind string

const (
	// KindTrigger 1→N 激活消息
	KindTrigger MessageKind = "trigger"
	// KindBarrier N→1 汇聚消息
	KindBarrier MessageKind = "barrier"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTriggerMessage 构造激活消息（1→N）。
// Python: TriggerMessage(sender, target)
func NewTriggerMessage(sender, target string) Message {
	return Message{Sender: sender, Target: target, kind: KindTrigger}
}

// NewBarrierMessage 构造汇聚消息（N→1）。
// Python: BarrierMessage(sender, target)
func NewBarrierMessage(sender, target string) Message {
	return Message{Sender: sender, Target: target, kind: KindBarrier}
}

// Kind 返回消息种类。
func (m Message) Kind() MessageKind {
	return m.kind
}
