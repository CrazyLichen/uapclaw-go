// Package core 团队可靠性数据层：统一信号流。
//
// 对应 Python：openjiuwen/agent_teams/reliability/signals.py
package core

// ──────────────────────────── 枚举 ────────────────────────────

// SignalKind 信号的生命周期点位。
//
// Python: signals.py SignalKind(str, Enum)，7 取值全量映射。
type SignalKind string

// 信号类别取值。
const (
	// SignalKindBeforeToolCall 工具执行前
	SignalKindBeforeToolCall SignalKind = "before_tool_call"
	// SignalKindAfterToolCall 工具执行成功后
	SignalKindAfterToolCall SignalKind = "after_tool_call"
	// SignalKindToolException 工具执行失败
	SignalKindToolException SignalKind = "tool_exception"
	// SignalKindModelException 模型调用失败
	SignalKindModelException SignalKind = "model_exception"
	// SignalKindAfterModelCall 模型调用完成后
	SignalKindAfterModelCall SignalKind = "after_model_call"
	// SignalKindBeforeModelCall 模型调用前
	SignalKindBeforeModelCall SignalKind = "before_model_call"
	// SignalKindMessage 团队级消息信号（来源 coordination，非成员 rail）
	SignalKindMessage SignalKind = "message"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Signal 成员执行生命周期点位上的一次观测。
//
// Python: signals.py Signal（@dataclass）。kind/member_name 之外的字段可选，
// 仅由承载它们的 kind 填充；detector 必须容忍任何字段为 nil。
type Signal struct {
	// Kind 产生该信号的生命周期点位
	Kind SignalKind
	// MemberName 信号归属的成员
	MemberName string
	// ToolName 工具标识（tool-call 类 kind）
	ToolName *string
	// ToolArgs 工具参数（BEFORE_TOOL_CALL），供重复检测使用
	ToolArgs map[string]any
	// Error 错误文本（TOOL_EXCEPTION / MODEL_EXCEPTION）
	Error *string
	// TextLen 响应文本长度（AFTER_MODEL_CALL）
	TextLen *int
	// ThinkingLen 思考/推理内容长度（AFTER_MODEL_CALL）
	ThinkingLen *int
	// MessageCount 上下文消息数（BEFORE_MODEL_CALL），压缩推断依据
	MessageCount *int
	// PeerMember 会话对端（MESSAGE），供 pingpong 追踪
	PeerMember *string
	// ToolResult 工具执行结果（AFTER_TOOL_CALL），供结果感知的循环检测
	ToolResult any
	// ModelFailure 模型组客户端附加到错误上的路由裁决（MODEL_EXCEPTION）：
	// route_case / reason / pinned_model / failures / user_action_required
	ModelFailure map[string]any
}
