package vcs

import (
	"encoding/json"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// roleUser 用户消息角色字符串（Python: codec.py:21 _ROLE_TO_CLASS）
	roleUser = "user"
	// roleAssistant 助手消息角色字符串（Python: codec.py:22）
	roleAssistant = "assistant"
	// roleSystem 系统消息角色字符串（Python: codec.py:23）
	roleSystem = "system"
	// roleTool 工具消息角色字符串（Python: codec.py:24）
	roleTool = "tool"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EncodeMessage 把一条消息序列化为 JSON-native 字典。
// Python: codec.py:28-30 encode_message
func EncodeMessage(message llmschema.BaseMessage) map[string]any {
	if message == nil {
		return map[string]any{"role": roleUser, "content": ""}
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return map[string]any{"role": message.GetRole().String(), "content": ""}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{"role": message.GetRole().String(), "content": ""}
	}
	return out
}

// DecodeMessage 按 role 把字典反序列化回具体消息子类。
// Python: codec.py:33-36 decode_message
//
// 未知 role 时 Python 回退到 BaseMessage；Go 的 BaseMessage 是接口不可实例化，
// 回退到 UserMessage（25.1f spec D-2 已登记此平台差异）。
func DecodeMessage(data map[string]any) llmschema.BaseMessage {
	raw, err := json.Marshal(data)
	if err != nil {
		return llmschema.NewUserMessage("")
	}
	role, _ := data["role"].(string)
	switch role {
	case roleAssistant:
		var m llmschema.AssistantMessage
		if json.Unmarshal(raw, &m) == nil {
			return &m
		}
	case roleSystem:
		var m llmschema.SystemMessage
		if json.Unmarshal(raw, &m) == nil {
			return &m
		}
	case roleTool:
		var m llmschema.ToolMessage
		if json.Unmarshal(raw, &m) == nil {
			return &m
		}
	case roleUser:
		var m llmschema.UserMessage
		if json.Unmarshal(raw, &m) == nil {
			return &m
		}
	}
	return llmschema.NewUserMessage("")
}

// EncodeContextState 把 save_state 结果编码为 JSON-native 形态。
// Python: codec.py:39-51 encode_context_state
//
// 步骤：① 逐条编码 messages；② 逐 handle 编码 offload_messages；
// ③ 返回两键结构。空集由 `state.get(...) or {}` 语义产出空容器。
func EncodeContextState(state map[string]any) map[string]any {
	messages := make([]map[string]any, 0)
	switch raw := state["messages"].(type) {
	case []llmschema.BaseMessage:
		for _, m := range raw {
			messages = append(messages, EncodeMessage(m))
		}
	case []map[string]any:
		messages = append(messages, raw...)
	case []any:
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				messages = append(messages, m)
			}
		}
	}
	offload := make(map[string][]map[string]any)
	switch raw := state["offload_messages"].(type) {
	case map[string][]llmschema.BaseMessage:
		for handle, msgs := range raw {
			offload[handle] = encodeMessageList(msgs)
		}
	case map[string][]map[string]any:
		for handle, msgs := range raw {
			offload[handle] = msgs
		}
	case map[string]any:
		for handle, item := range raw {
			offload[handle] = encodeJSONMessageList(item)
		}
	}
	return map[string]any{"messages": messages, "offload_messages": offload}
}

// DecodeContextState 把 JSON-native 上下文状态还原为消息对象。
// Python: codec.py:54-68 decode_context_state
//
// 步骤：① 逐条解码 messages；② 逐 handle 解码 offload_messages；
// ③ 返回两键结构，与 EncodeContextState 互为逆操作。
func DecodeContextState(state map[string]any) map[string]any {
	messages := make([]llmschema.BaseMessage, 0)
	switch raw := state["messages"].(type) {
	case []any:
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				messages = append(messages, DecodeMessage(m))
			}
		}
	case []map[string]any:
		for _, m := range raw {
			messages = append(messages, DecodeMessage(m))
		}
	}
	offload := make(map[string][]llmschema.BaseMessage)
	switch raw := state["offload_messages"].(type) {
	case map[string]any:
		for handle, item := range raw {
			offload[handle] = decodeJSONMessageList(item)
		}
	case map[string][]map[string]any:
		for handle, items := range raw {
			msgs := make([]llmschema.BaseMessage, 0, len(items))
			for _, m := range items {
				msgs = append(msgs, DecodeMessage(m))
			}
			offload[handle] = msgs
		}
	case map[string][]llmschema.BaseMessage:
		for handle, msgs := range raw {
			offload[handle] = msgs
		}
	}
	return map[string]any{"messages": messages, "offload_messages": offload}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// encodeMessageList 编码一组消息对象。
func encodeMessageList(msgs []llmschema.BaseMessage) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, EncodeMessage(m))
	}
	return out
}

// encodeJSONMessageList 编码已为 JSON-native 形态的一组消息（透传）。
func encodeJSONMessageList(item any) []map[string]any {
	out := make([]map[string]any, 0)
	switch list := item.(type) {
	case []any:
		for _, v := range list {
			if m, ok := v.(map[string]any); ok {
				out = append(out, m)
			}
		}
	case []map[string]any:
		out = append(out, list...)
	}
	return out
}

// decodeJSONMessageList 解码 JSON-native 形态的一组消息。
func decodeJSONMessageList(item any) []llmschema.BaseMessage {
	out := make([]llmschema.BaseMessage, 0)
	switch list := item.(type) {
	case []any:
		for _, v := range list {
			if m, ok := v.(map[string]any); ok {
				out = append(out, DecodeMessage(m))
			}
		}
	case []map[string]any:
		for _, m := range list {
			out = append(out, DecodeMessage(m))
		}
	}
	return out
}
