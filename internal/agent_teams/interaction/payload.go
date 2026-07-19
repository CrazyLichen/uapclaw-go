package interaction

import (
	"fmt"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InteractPayload 交互载荷接口。
// 对齐 Python: InteractPayload = Union[GodViewMessage, OperatorMessage, HumanAgentMessage]
//
// 三种载荷类型通过 Kind() 区分，分发时用类型断言获取具体字段：
//
//	switch p := payload.(type) {
//	case *GodViewMessage:    ...
//	case *OperatorMessage:   ...
//	case *HumanAgentMessage: ...
//	}
type InteractPayload interface {
	// Kind 返回载荷类型枚举
	Kind() PayloadKind
	// Body 返回消息内容
	Body() string
}

// GodViewMessage 直达 Leader DeepAgent 的消息。
// 对齐 Python: GodViewMessage(body: str) (openjiuwen/agent_teams/interaction/payload.py)
//
// 通道语义：`# body` 或裸文本 → 消息直送 Leader 的 DeepAgent，
// 等价于历史 invoke / deliver_to_leader 通道。
type GodViewMessage struct {
	// body 消息内容
	body string
}

// OperatorMessage 操作者视角消息（@成员/广播）。
// 对齐 Python: OperatorMessage(body: str, target: Optional[str])
//
// 通道语义：`@member body` → 点对点消息（target 为成员名）；
// `@all body` / `@* body` → 广播（target 为 nil）。
type OperatorMessage struct {
	// body 消息内容
	body string
	// target 目标成员名；nil 表示广播
	target *string
}

// HumanAgentMessage Human-Agent 成员消息。
// 对齐 Python: HumanAgentMessage(body: str, sender: str, target: Optional[str])
//
// 通道语义：`$name body` → 驱动 avatar（target 为 nil）；
// `$name @member body` → 点对点或广播。
type HumanAgentMessage struct {
	// body 消息内容
	body string
	// sender 发送者成员名
	sender string
	// target 目标成员名；nil 表示驱动 avatar；"*"/"all" 表示广播
	target *string
}

// DeliverResult 投递结果。
// 对齐 Python: DeliverResult(ok, message_id, reason)
//
// 成功时 OK 为 true 且 MessageID 携带消息 ID
// （deliver_to_leader 通道不产生 bus message，MessageID 为 nil）。
// 失败时 OK 为 false 且 Reason 携带短稳定 token。
type DeliverResult struct {
	// OK 是否成功
	OK bool
	// MessageID 消息 ID（可为 nil）
	MessageID *string
	// Reason 失败原因 token
	Reason *string
}

// HumanAgentInboundEvent 团队→用户通知事件。
// 对齐 Python: HumanAgentInboundEvent (openjiuwen/agent_teams/interaction/payload.py)
//
// Phase-2 HITT 不会让 human-agent 的 LLM 自动消费入站消息——
// 消息直接透传给对应的外部用户。此结构体是运行时喂给
// HumanAgentInbox 注册的 on_inbound 回调的数据。
type HumanAgentInboundEvent struct {
	// MemberName 接收消息的 human-agent 成员名
	MemberName string
	// Sender 发送者成员名（或 "user" 伪成员）
	Sender string
	// Body 消息内容
	Body string
	// Broadcast 是否为广播消息
	Broadcast bool
	// MessageID 消息总线上的 ID
	MessageID string
	// Timestamp 毫秒级时间戳
	Timestamp int64
}

// ──────────────────────────── 枚举 ────────────────────────────

// PayloadKind 载荷类型枚举。
// 对齐 Python: isinstance(payload, GodViewMessage/OperatorMessage/HumanAgentMessage)
type PayloadKind int

const (
	// PayloadKindGodView God View 载荷——直连 Leader
	PayloadKindGodView PayloadKind = iota
	// PayloadKindOperator Operator 载荷——@成员/广播
	PayloadKindOperator
	// PayloadKindHumanAgent Human-Agent 载荷
	PayloadKindHumanAgent
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGodViewMessage 创建 GodView 消息。
func NewGodViewMessage(body string) *GodViewMessage {
	return &GodViewMessage{body: body}
}

// NewOperatorMessage 创建 Operator 消息。
// target 为 nil 表示广播。
func NewOperatorMessage(body string, target *string) *OperatorMessage {
	return &OperatorMessage{body: body, target: target}
}

// NewHumanAgentMessage 创建 Human-Agent 消息。
// target 为 nil 表示驱动 avatar。
func NewHumanAgentMessage(body string, sender string, target *string) *HumanAgentMessage {
	return &HumanAgentMessage{body: body, sender: sender, target: target}
}

// NewDeliverResultSuccess 创建成功的投递结果。
// 对齐 Python: DeliverResult.success(message_id)
func NewDeliverResultSuccess(messageID *string) *DeliverResult {
	return &DeliverResult{OK: true, MessageID: messageID}
}

// NewDeliverResultFailure 创建失败的投递结果。
// 对齐 Python: DeliverResult.failure(reason)
func NewDeliverResultFailure(reason string) *DeliverResult {
	return &DeliverResult{OK: false, Reason: &reason}
}

// ──────────── InteractPayload 接口实现 ────────────

// Kind 返回载荷类型。
func (g *GodViewMessage) Kind() PayloadKind { return PayloadKindGodView }

// Body 返回消息内容。
func (g *GodViewMessage) Body() string { return g.body }

// Kind 返回载荷类型。
func (o *OperatorMessage) Kind() PayloadKind { return PayloadKindOperator }

// Body 返回消息内容。
func (o *OperatorMessage) Body() string { return o.body }

// Target 返回目标成员名（nil 表示广播）。
func (o *OperatorMessage) Target() *string { return o.target }

// Kind 返回载荷类型。
func (h *HumanAgentMessage) Kind() PayloadKind { return PayloadKindHumanAgent }

// Body 返回消息内容。
func (h *HumanAgentMessage) Body() string { return h.body }

// Sender 返回发送者成员名。
func (h *HumanAgentMessage) Sender() string { return h.sender }

// Target 返回目标成员名。
func (h *HumanAgentMessage) Target() *string { return h.target }

// ──────────── DeliverResult 方法 ────────────

// IsOK 判断投递是否成功。
// 对齐 Python: DeliverResult.__bool__
func (d *DeliverResult) IsOK() bool { return d.OK }

// String 返回可读描述。
func (d *DeliverResult) String() string {
	if d.OK {
		if d.MessageID != nil {
			return fmt.Sprintf("DeliverResult(ok=true, message_id=%s)", *d.MessageID)
		}
		return "DeliverResult(ok=true, message_id=nil)"
	}
	if d.Reason != nil {
		return fmt.Sprintf("DeliverResult(ok=false, reason=%s)", *d.Reason)
	}
	return "DeliverResult(ok=false)"
}

// ──────────── PayloadKind 方法 ────────────

// String 返回载荷类型的可读名称。
func (k PayloadKind) String() string {
	return payloadKindName(k)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// payloadKindName 载荷类型名称（用于日志）。
func payloadKindName(kind PayloadKind) string {
	switch kind {
	case PayloadKindGodView:
		return "GodView"
	case PayloadKindOperator:
		return "Operator"
	case PayloadKindHumanAgent:
		return "HumanAgent"
	default:
		return "Unknown"
	}
}

// isReservedMemberName 检查是否为运行时保留成员名。
// 对齐 Python: is_reserved_name(name) (openjiuwen/agent_teams/interaction/router.py)
func isReservedMemberName(name string) bool {
	return agentteams.ReservedMemberNames[name]
}
