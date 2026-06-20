package schema

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 常量 ────────────────────────────

// FinishReasonNull 流式场景中表示尚未收到终止信号的标记值。
//
// Python 端使用字符串 "null"（不是 None）作为默认值，流式合并时通过
// finish_reason != "null" 判断是否收到终止信号。Go 端保持相同语义。
const FinishReasonNull = "null"

// ──────────────────────────── 结构体 ────────────────────────────

// AssistantMessage 助手消息，LLM 响应的核心载体。
//
// 设计要点：
//   - ToolCalls 内部使用扁平格式（ToolCall 结构体）
//   - 反序列化时自动将 OpenAI 嵌套格式转为扁平格式（UnmarshalJSON）
//   - 序列化时使用内部扁平格式（MarshalJSON）
//   - 如需 OpenAI API 格式，请调用 ToOpenAIDict()
//   - finish_reason 默认值 "null"（字符串），表示流式场景尚未终止
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/message.py (AssistantMessage)
type AssistantMessage struct {
	DefaultMessage
	// ToolCalls 工具调用列表（扁平格式）
	ToolCalls []*ToolCall `json:"tool_calls,omitempty"`
	// UsageMetadata 用量元数据
	UsageMetadata *UsageMetadata `json:"usage_metadata,omitempty"`
	// FinishReason 完成原因，默认 "null"（流式场景哨兵值）
	FinishReason string `json:"finish_reason"`
	// ParserContent 解析器内容（输出解析器填充）
	ParserContent any `json:"parser_content,omitempty"`
	// ReasoningContent 推理内容（思维链，如 DeepSeek-R1）
	ReasoningContent string `json:"reasoning_content,omitempty"`
	// PromptTokenIDs 输入 token ID 列表（vLLM 等推理引擎返回，用于 RL 轨迹收集）
	PromptTokenIDs []int `json:"prompt_token_ids,omitempty"`
	// CompletionTokenIDs 输出 token ID 列表（vLLM 等推理引擎返回，用于 RL 轨迹收集）
	CompletionTokenIDs []int `json:"completion_token_ids,omitempty"`
	// Logprobs 对数概率信息（vLLM 等推理引擎返回）
	Logprobs any `json:"logprobs,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// AssistantMessageOption AssistantMessage 构造选项函数。
type AssistantMessageOption func(*AssistantMessage)

// WithToolCalls 设置工具调用列表。
func WithToolCalls(calls []*ToolCall) AssistantMessageOption {
	return func(m *AssistantMessage) { m.ToolCalls = calls }
}

// WithUsageMetadata 设置用量元数据。
func WithAssistantUsageMetadata(meta *UsageMetadata) AssistantMessageOption {
	return func(m *AssistantMessage) { m.UsageMetadata = meta }
}

// WithFinishReason 设置完成原因。
func WithFinishReason(reason string) AssistantMessageOption {
	return func(m *AssistantMessage) { m.FinishReason = reason }
}

// WithReasoningContent 设置推理内容。
func WithReasoningContent(content string) AssistantMessageOption {
	return func(m *AssistantMessage) { m.ReasoningContent = content }
}

// WithParserContent 设置解析器内容。
func WithParserContent(content any) AssistantMessageOption {
	return func(m *AssistantMessage) { m.ParserContent = content }
}

// WithPromptTokenIDs 设置输入 token ID 列表。
func WithPromptTokenIDs(ids []int) AssistantMessageOption {
	return func(m *AssistantMessage) { m.PromptTokenIDs = ids }
}

// WithCompletionTokenIDs 设置输出 token ID 列表。
func WithCompletionTokenIDs(ids []int) AssistantMessageOption {
	return func(m *AssistantMessage) { m.CompletionTokenIDs = ids }
}

// WithLogprobs 设置对数概率信息。
func WithLogprobs(logprobs any) AssistantMessageOption {
	return func(m *AssistantMessage) { m.Logprobs = logprobs }
}

// NewAssistantMessage 创建助手消息，finish_reason 默认 "null"。
//
// 对应 Python: AssistantMessage(content=..., tool_calls=..., ...)
func NewAssistantMessage(content string, opts ...AssistantMessageOption) *AssistantMessage {
	msg := &AssistantMessage{
		DefaultMessage: DefaultMessage{
			Role:    RoleTypeAssistant,
			Content: NewTextContent(content),
		},
		FinishReason: FinishReasonNull,
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}

// IsFinished 判断是否已收到终止信号。
//
// finish_reason != "null" 表示已收到终止信号。
// 对应 Python: finish_reason != "null"
func (m *AssistantMessage) IsFinished() bool {
	return m.FinishReason != FinishReasonNull
}

// ToOpenAIDict 转换为 OpenAI API 格式的 dict，供 LLM 请求使用。
//
// 核心转换逻辑：
//   - ToolCalls 从扁平格式转为 OpenAI 嵌套格式
//   - 仅输出非空字段
//
// 对应 Python: AssistantMessage.model_dump()
func (m *AssistantMessage) ToOpenAIDict() map[string]any {
	result := map[string]any{
		"role":    "assistant",
		"content": m.Content,
	}
	if m.Name != "" {
		result["name"] = m.Name
	}
	if len(m.Metadata) > 0 {
		result["metadata"] = m.Metadata
	}
	if len(m.ToolCalls) > 0 {
		calls := make([]map[string]any, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			calls = append(calls, tc.ToOpenAIFormat())
		}
		result["tool_calls"] = calls
	}
	if m.UsageMetadata != nil {
		result["usage_metadata"] = m.UsageMetadata
	}
	if m.FinishReason != "" {
		result["finish_reason"] = m.FinishReason
	}
	if m.ParserContent != nil {
		result["parser_content"] = m.ParserContent
	}
	if m.ReasoningContent != "" {
		result["reasoning_content"] = m.ReasoningContent
	}
	if len(m.PromptTokenIDs) > 0 {
		result["prompt_token_ids"] = m.PromptTokenIDs
	}
	if len(m.CompletionTokenIDs) > 0 {
		result["completion_token_ids"] = m.CompletionTokenIDs
	}
	if m.Logprobs != nil {
		result["logprobs"] = m.Logprobs
	}
	return result
}

// MarshalJSON 实现 json.Marshaler 接口，输出内部扁平格式。
//
// 内部格式中 tool_calls 的每个元素为扁平 ToolCall（name/arguments 直属）。
// 如需 OpenAI API 嵌套格式，请使用 ToOpenAIDict() + json.Marshal()。
func (m *AssistantMessage) MarshalJSON() ([]byte, error) {
	// 使用别名避免无限递归
	type Alias AssistantMessage
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	})
}

// UnmarshalJSON 实现 json.Unmarshaler 接口，自动将 OpenAI 嵌套 tool_calls 转为扁平格式。
//
// 对应 Python: AssistantMessage.convert_openai_tool_calls_format() (model_validator)
func (m *AssistantMessage) UnmarshalJSON(data []byte) error {
	// 使用临时结构体解析，避免无限递归
	var raw struct {
		Role               RoleType          `json:"role"`
		Content            json.RawMessage   `json:"content"`
		Name               string            `json:"name"`
		Metadata           map[string]any    `json:"metadata"`
		ToolCalls          []json.RawMessage `json:"tool_calls"`
		UsageMetadata      *UsageMetadata    `json:"usage_metadata"`
		FinishReason       string            `json:"finish_reason"`
		ParserContent      any               `json:"parser_content"`
		ReasoningContent   string            `json:"reasoning_content"`
		PromptTokenIDs     []int             `json:"prompt_token_ids"`
		CompletionTokenIDs []int             `json:"completion_token_ids"`
		Logprobs           any               `json:"logprobs"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("AssistantMessage 反序列化失败: %w", err)
	}

	// 解析 Content（支持 string 或 array）
	var content MessageContent
	if err := json.Unmarshal(raw.Content, &content); err != nil {
		return fmt.Errorf("AssistantMessage Content 反序列化失败: %w", err)
	}

	// 解析 ToolCalls（每个元素可能是 OpenAI 嵌套格式，ToolCall.UnmarshalJSON 自动转换）
	var toolCalls []*ToolCall
	for _, rawTC := range raw.ToolCalls {
		var tc ToolCall
		if err := json.Unmarshal(rawTC, &tc); err != nil {
			return fmt.Errorf("AssistantMessage ToolCalls 反序列化失败: %w", err)
		}
		toolCalls = append(toolCalls, &tc)
	}

	// 组装 AssistantMessage
	m.Role = raw.Role
	m.Content = content
	m.Name = raw.Name
	m.Metadata = raw.Metadata
	m.ToolCalls = toolCalls
	m.UsageMetadata = raw.UsageMetadata
	m.FinishReason = raw.FinishReason
	m.ParserContent = raw.ParserContent
	m.ReasoningContent = raw.ReasoningContent
	m.PromptTokenIDs = raw.PromptTokenIDs
	m.CompletionTokenIDs = raw.CompletionTokenIDs
	m.Logprobs = raw.Logprobs

	// 确保 role 默认值为 assistant
	if m.Role != RoleTypeAssistant {
		m.Role = RoleTypeAssistant
	}

	// 确保 finish_reason 默认值为 "null"
	if m.FinishReason == "" {
		m.FinishReason = FinishReasonNull
	}

	return nil
}
