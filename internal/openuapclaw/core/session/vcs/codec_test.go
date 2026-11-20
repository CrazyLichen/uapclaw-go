package vcs

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// TestEncodeMessage_四角色往返 按 role 派发到具体子类，tool_calls/tool_call_id 不丢。
// Python: codec.py:18-36（头注释：BaseMessage 无 discriminated union，
// model_validate 基类会静默丢 tool_calls / tool_call_id）
func TestEncodeMessage_四角色往返(t *testing.T) {
	cases := []struct {
		name string
		msg  llmschema.BaseMessage
	}{
		{"user", llmschema.NewUserMessage("hi")},
		{"assistant", llmschema.NewAssistantMessage("ok")},
		{"system", llmschema.NewSystemMessage("sys")},
		{"tool", llmschema.NewToolMessage("tc-1", "res")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			encoded := EncodeMessage(c.msg)
			if encoded["role"] != c.name {
				t.Fatalf("encoded role = %v, want %q", encoded["role"], c.name)
			}
			decoded := DecodeMessage(encoded)
			if decoded.GetRole().String() != c.name {
				t.Fatalf("decoded role = %q, want %q", decoded.GetRole().String(), c.name)
			}
			if a, ok := decoded.(*llmschema.AssistantMessage); ok {
				_ = a.ToolCalls
			}
			if tm, ok := decoded.(*llmschema.ToolMessage); ok {
				if tm.ToolCallID != "tc-1" {
					t.Fatalf("tool_call_id 丢失: %q", tm.ToolCallID)
				}
			}
		})
	}
}

// TestEncodeMessage_AssistantToolCalls不丢 对齐 codec.py 头注释的核心理由。
func TestEncodeMessage_AssistantToolCalls不丢(t *testing.T) {
	a := llmschema.NewAssistantMessage("ok")
	a.ToolCalls = []*llmschema.ToolCall{{ID: "tc-9"}}
	encoded := EncodeMessage(a)
	decoded := DecodeMessage(encoded)
	back, ok := decoded.(*llmschema.AssistantMessage)
	if !ok {
		t.Fatalf("应按 role=assistant 还原为 *AssistantMessage，得 %T", decoded)
	}
	if len(back.ToolCalls) != 1 || back.ToolCalls[0].ID != "tc-9" {
		t.Fatalf("tool_calls 丢失: %+v", back.ToolCalls)
	}
}

// TestContextState_两键结构 对齐 encode_context_state/decode_context_state。
func TestContextState_两键结构(t *testing.T) {
	in := map[string]any{
		"messages": []llmschema.BaseMessage{llmschema.NewUserMessage("q")},
		"offload_messages": map[string][]llmschema.BaseMessage{
			"h1": {llmschema.NewAssistantMessage("a")},
		},
	}
	encoded := EncodeContextState(in)
	msgs, ok := encoded["messages"].([]map[string]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages 编码失败: %#v", encoded["messages"])
	}
	off, ok := encoded["offload_messages"].(map[string][]map[string]any)
	if !ok || len(off["h1"]) != 1 {
		t.Fatalf("offload_messages 编码失败: %#v", encoded["offload_messages"])
	}
	decoded := DecodeContextState(encoded)
	backMsgs, ok := decoded["messages"].([]llmschema.BaseMessage)
	if !ok || len(backMsgs) != 1 {
		t.Fatalf("messages 解码失败: %#v", decoded["messages"])
	}
	backOff, ok := decoded["offload_messages"].(map[string][]llmschema.BaseMessage)
	if !ok || len(backOff["h1"]) != 1 {
		t.Fatalf("offload_messages 解码失败: %#v", decoded["offload_messages"])
	}
}

// TestEncodeContextState_空集语义 对齐 Python `state.get(...) or {}`。
func TestEncodeContextState_空集语义(t *testing.T) {
	out := EncodeContextState(nil)
	if _, ok := out["messages"]; !ok {
		t.Fatal("messages 键缺失")
	}
	off, ok := out["offload_messages"].(map[string][]map[string]any)
	if !ok || len(off) != 0 {
		t.Fatalf("nil 输入应产出空 offload map，得 %#v", out["offload_messages"])
	}
}

// TestDecodeContextState_JSON形态 覆盖来自 checkpoint 的 []any / map[string]any。
func TestDecodeContextState_JSON形态(t *testing.T) {
	in := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "q"}},
		"offload_messages": map[string]any{
			"h1": []any{map[string]any{"role": "tool", "content": "r", "tool_call_id": "tc-2"}},
		},
	}
	out := DecodeContextState(in)
	msgs, ok := out["messages"].([]llmschema.BaseMessage)
	if !ok || len(msgs) != 1 {
		t.Fatalf("JSON 形态 messages 解码失败: %#v", out["messages"])
	}
	off, ok := out["offload_messages"].(map[string][]llmschema.BaseMessage)
	if !ok || len(off["h1"]) != 1 {
		t.Fatalf("JSON 形态 offload 解码失败: %#v", out["offload_messages"])
	}
	if tm, ok := off["h1"][0].(*llmschema.ToolMessage); !ok || tm.ToolCallID != "tc-2" {
		t.Fatalf("tool 子类还原失败: %T", off["h1"][0])
	}
}
