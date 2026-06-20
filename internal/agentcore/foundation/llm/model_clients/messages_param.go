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
//	              支持的具体类型：*UserMessage, *SystemMessage, *AssistantMessage, *ToolMessage
//	IsDicts    → 已是 OpenAI dict 格式，直接透传（零转换开销）
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/base_model_client.py
// (_convert_messages_to_dict 的输入参数 messages)
type MessagesParam struct {
	// text 纯文本（对应 Python str 输入，自动包装为一条 UserMessage）
	text string
	// messages 消息列表（对应 Python List[BaseMessage] 输入）
	// 保留具体类型（*UserMessage, *AssistantMessage, *ToolMessage 等），
	// 以便 ConvertMessagesToDict 通过类型断言提取特有字段（tool_calls, tool_call_id）。
	messages []any
	// dicts 已是 OpenAI dict 格式的列表（对应 Python List[dict] 输入，直接透传）
	dicts []map[string]any
}

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
// 接受的具体类型：*UserMessage, *SystemMessage, *AssistantMessage, *ToolMessage。
// 保留具体类型信息，以便后续 ConvertMessagesToDict 正确提取特有字段。
// 传入的 nil 值会被自动过滤。
//
// 对应 Python: messages=[UserMessage("你好"), AssistantMessage("hi")]
func NewMessagesParam(messages ...any) MessagesParam {
	// 过滤 nil 值：variadic 调用 NewMessagesParam(nil) 会产生 []any{nil}
	filtered := make([]any, 0, len(messages))
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
// 列表元素的具体类型可能是 *UserMessage, *SystemMessage, *AssistantMessage, *ToolMessage。
// 使用方应通过类型断言访问具体类型。
func (p MessagesParam) Messages() []any {
	return p.messages
}

// Dicts 返回 dict 列表（IsDicts 为 true 时有效）。
func (p MessagesParam) Dicts() []map[string]any {
	return p.dicts
}

// toBaseMessage 从任意消息类型提取 BaseMessage 接口。
// 所有消息类型都实现了 BaseMessage 接口，直接做类型断言即可。
func toBaseMessage(msg any) (llmschema.BaseMessage, bool) {
	m, ok := msg.(llmschema.BaseMessage)
	return m, ok
}
