package model_clients

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessagesParam 消息参数，支持多种输入格式（输入侧）。
//
// 对应 Python: Union[str, List[BaseMessage], List[dict]]
// 与 schema.MessageContent（输出侧，content 字段纯文本/多模态）对称，
// MessagesParam 处理输入侧，即整个 messages 列表的格式：
//
//	IsText     → 纯文本，自动包装为一条 UserMessage
//	IsMessages → 消息列表，转换为 OpenAI dict 格式
//	              通过 BaseMessage 接口统一承载，消费端按需做具体类型断言
//	IsDicts    → 已是 OpenAI dict 格式，直接透传（零转换开销）
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/base_model_client.py
// (_convert_messages_to_dict 的输入参数 messages)
type MessagesParam struct {
	// text 纯文本（对应 Python str 输入，自动包装为一条 UserMessage）
	text string
	// messages 消息列表（对应 Python List[BaseMessage] 输入）
	// 通过 BaseMessage 接口承载，消费端通过类型断言提取特有字段（tool_calls, tool_call_id）。
	messages []llmschema.BaseMessage
	// dicts 已是 OpenAI dict 格式的列表（对应 Python List[dict] 输入，直接透传）
	dicts []map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTextMessagesParam 创建纯文本消息参数。
// 内部会自动包装为一条 UserMessage（role="user"）。
//
// 对应 Python: messages="你好"
func NewTextMessagesParam(text string) MessagesParam {
	return MessagesParam{text: text}
}

// NewMessagesParam 创建消息列表参数。
//
// 接受 BaseMessage 接口类型的消息，包括 *UserMessage, *SystemMessage,
// *AssistantMessage, *ToolMessage 等。传入的 nil 值会被自动过滤。
//
// 对应 Python: messages=[UserMessage("你好"), AssistantMessage("hi")]
func NewMessagesParam(messages ...llmschema.BaseMessage) MessagesParam {
	// 过滤 nil 值：variadic 调用 NewMessagesParam(nil) 会产生含 nil 的切片
	filtered := make([]llmschema.BaseMessage, 0, len(messages))
	for _, m := range messages {
		if m != nil {
			filtered = append(filtered, m)
		}
	}
	return MessagesParam{messages: filtered}
}

// NewDictsMessagesParam 创建 dict 列表消息参数。
// 列表中的每个 dict 应为 OpenAI API 格式：{"role": "...", "content": "..."}。
//
// 对应 Python: messages=[{"role": "user", "content": "你好"}]
func NewDictsMessagesParam(dicts []map[string]any) MessagesParam {
	return MessagesParam{dicts: dicts}
}

// IsEmpty 消息参数是否为空（三种模式均为零值）。
func (p MessagesParam) IsEmpty() bool {
	return p.text == "" && len(p.messages) == 0 && len(p.dicts) == 0
}

// IsText 是否为纯文本模式。
func (p MessagesParam) IsText() bool {
	return p.text != ""
}

// IsMessages 是否为消息列表模式。
func (p MessagesParam) IsMessages() bool {
	return len(p.messages) > 0
}

// IsDicts 是否为 dict 列表模式。
func (p MessagesParam) IsDicts() bool {
	return len(p.dicts) > 0
}

// Text 返回纯文本内容（IsText 为 true 时有效）。
func (p MessagesParam) Text() string {
	return p.text
}

// Messages 返回消息列表（IsMessages 为 true 时有效）。
//
// 列表元素通过 BaseMessage 接口返回，消费端按需做具体类型断言
// （如 msg.(*AssistantMessage) 访问 ToolCalls）。
func (p MessagesParam) Messages() []llmschema.BaseMessage {
	return p.messages
}

// Dicts 返回 dict 列表（IsDicts 为 true 时有效）。
func (p MessagesParam) Dicts() []map[string]any {
	return p.dicts
}

// ──────────────────────────── 非导出函数 ────────────────────────────
