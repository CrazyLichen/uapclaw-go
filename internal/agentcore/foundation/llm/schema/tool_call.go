package schema

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCall 工具调用信息，扁平化结构。
//
// 内部格式（扁平）：{"id", "type", "name", "arguments", "index"}
// OpenAI API 格式（嵌套）：{"id", "type", "function": {"name", "arguments"}, "index"}
//
// 反序列化时自动将 OpenAI 嵌套格式转为扁平格式（UnmarshalJSON）。
// 序列化时输出内部扁平格式（MarshalJSON），如需 OpenAI 格式请调用 ToOpenAIFormat()。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/tool_call.py (ToolCall)
type ToolCall struct {
	// ID 工具调用 ID
	ID string `json:"id,omitempty"`
	// Type 工具调用类型，默认 "function"
	Type string `json:"type"`
	// Name 工具名称
	Name string `json:"name"`
	// Arguments 工具参数（JSON 字符串）
	Arguments string `json:"arguments"`
	// Index 工具调用索引，用于区分同一消息中的多个工具调用
	Index int `json:"index,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolCallOption ToolCall 构造选项函数。
type ToolCallOption func(*ToolCall)

// WithToolCallIndex 设置工具调用索引。
func WithToolCallIndex(index int) ToolCallOption {
	return func(tc *ToolCall) { tc.Index = index }
}

// WithToolCallType 设置工具调用类型。
func WithToolCallType(typ string) ToolCallOption {
	return func(tc *ToolCall) { tc.Type = typ }
}

// NewToolCall 创建 ToolCall 实例，Type 默认为 "function"。
//
// 对应 Python: ToolCall(id=..., type="function", name=..., arguments=..., index=None)
func NewToolCall(id, name, arguments string, opts ...ToolCallOption) *ToolCall {
	tc := &ToolCall{
		ID:        id,
		Type:      "function",
		Name:      name,
		Arguments: arguments,
	}
	for _, opt := range opts {
		opt(tc)
	}
	return tc
}

// ToOpenAIFormat 转换为 OpenAI API 嵌套格式 map。
//
// 输出格式：{"id": "...", "type": "function", "function": {"name": "...", "arguments": "..."}}
//
// 对应 Python: AssistantMessage.model_dump() 中 tool_calls 的序列化逻辑
func (tc *ToolCall) ToOpenAIFormat() map[string]any {
	result := map[string]any{
		"type": tc.Type,
		"function": map[string]any{
			"name":      tc.Name,
			"arguments": tc.Arguments,
		},
	}
	if tc.ID != "" {
		result["id"] = tc.ID
	}
	if tc.Index > 0 {
		result["index"] = tc.Index
	}
	return result
}

// MarshalJSON 实现 json.Marshaler 接口，输出内部扁平格式。
//
// 内部格式：{"id": "...", "type": "function", "name": "...", "arguments": "..."}
// 如需 OpenAI API 嵌套格式，请使用 ToOpenAIFormat() + json.Marshal()。
func (tc *ToolCall) MarshalJSON() ([]byte, error) {
	// 使用别名避免无限递归
	type Alias ToolCall
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(tc),
	})
}

// UnmarshalJSON 实现 json.Unmarshaler 接口，自动处理 OpenAI 嵌套格式到扁平格式的转换。
//
// 如果输入数据包含 "function" 键（OpenAI 格式），自动提取 function.name 和 function.arguments
// 填入 Name 和 Arguments 字段。
//
// 对应 Python: AssistantMessage.convert_openai_tool_calls_format() (model_validator)
func (tc *ToolCall) UnmarshalJSON(data []byte) error {
	// 先解析为通用 map，检测是否为 OpenAI 嵌套格式
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("ToolCall 反序列化失败: %w", err)
	}

	// 检测并转换 OpenAI 嵌套格式
	if fn, ok := raw["function"]; ok {
		if fnMap, ok := fn.(map[string]any); ok {
			// OpenAI 嵌套格式：提取 function.name 和 function.arguments
			if name, ok := fnMap["name"]; ok {
				raw["name"] = name
			}
			if args, ok := fnMap["arguments"]; ok {
				raw["arguments"] = args
			}
			// 移除嵌套的 function 键
			delete(raw, "function")
		}
	}

	// 确保 type 有默认值
	if _, ok := raw["type"]; !ok {
		raw["type"] = "function"
	}

	// 重新序列化后用标准方式解析
	normalized, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("ToolCall 格式转换失败: %w", err)
	}

	// 使用别名避免无限递归
	type Alias ToolCall
	var alias Alias
	if err := json.Unmarshal(normalized, &alias); err != nil {
		return fmt.Errorf("ToolCall 反序列化失败: %w", err)
	}
	*tc = ToolCall(alias)
	return nil
}
