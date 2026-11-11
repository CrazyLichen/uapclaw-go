package protocol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPublicEventPayload_非稳定事件原样返回
// Python: contracts.py:79-80 if event_type not in STABLE_EVENT_TYPES: return payload
func TestPublicEventPayload_非稳定事件原样返回(t *testing.T) {
	in := map[string]any{"a": 1}
	out := PublicEventPayload("runtime.event", in)
	require.Equal(t, in, out)
}

// TestPublicEventPayload_chatDelta补text字段
// Python: contracts.py:82-83 result["text"] = _text(result)
func TestPublicEventPayload_chatDelta补text字段(t *testing.T) {
	out := PublicEventPayload("chat.delta", map[string]any{"delta": "hi"})
	require.Equal(t, "hi", out.(map[string]any)["text"])
}

// TestPublicEventPayload_text取值顺序 验证 delta > content > text > message > answer。
// Python: contracts.py:35-40 _text 依次取
func TestPublicEventPayload_text取值顺序(t *testing.T) {
	out := PublicEventPayload("chat.final", map[string]any{
		"content": "c", "text": "t",
	})
	require.Equal(t, "c", out.(map[string]any)["text"], "content 优先于 text")
}

// TestPublicEventPayload_interactionKind映射 验证三种 source。
// Python: contracts.py:84-95
func TestPublicEventPayload_interactionKind映射(t *testing.T) {
	cases := map[string]string{
		"permission_interrupt": "permission",
		"confirm_interrupt":    "confirmation",
		"other":                "question",
	}
	for source, want := range cases {
		out := PublicEventPayload("interaction.requested", map[string]any{
			"interaction": map[string]any{"source": source},
		})
		require.Equal(t, want, out.(map[string]any)["kind"], "source=%s", source)
	}
}

// TestPublicEventPayload_questions规范化
// Python: contracts.py:43-74 _questions
func TestPublicEventPayload_questions规范化(t *testing.T) {
	out := PublicEventPayload("interaction.requested", map[string]any{
		"interaction": map[string]any{
			"source": "ask_user",
			"questions": []any{
				map[string]any{
					"question": "pick",
					"options":  []any{"a", "b"},
				},
			},
		},
	})
	qs := out.(map[string]any)["questions"].([]any)
	require.Len(t, qs, 1)
	q := qs[0].(map[string]any)
	require.Equal(t, "q0", q["question_id"])
	require.Equal(t, "pick", q["question"])
	require.True(t, q["allow_custom_input"].(bool), "非 permission_interrupt 默认允许自定义输入")
}

// TestPublicEventPayload_permission问题不允许自定义输入
// Python: contracts.py:66-70 allow_custom_input 默认 source != "permission_interrupt"
func TestPublicEventPayload_permission问题不允许自定义输入(t *testing.T) {
	out := PublicEventPayload("interaction.requested", map[string]any{
		"interaction": map[string]any{
			"source": "permission_interrupt",
			"questions": []any{
				map[string]any{"question": "allow?"},
			},
		},
	})
	qs := out.(map[string]any)["questions"].([]any)
	require.False(t, qs[0].(map[string]any)["allow_custom_input"].(bool))
}

// TestPublicEventPayload_toolCall补tool对象
// Python: contracts.py:96-126
func TestPublicEventPayload_toolCall补tool对象(t *testing.T) {
	out := PublicEventPayload("chat.tool_call", map[string]any{
		"tool_call": map[string]any{
			"tool_call_id": "c1",
			"name":         "read_file",
			"arguments":    `{"path":"a.txt"}`,
		},
	})
	tool := out.(map[string]any)["tool"].(map[string]any)
	require.Equal(t, "c1", tool["call_id"])
	require.Equal(t, "read_file", tool["name"])
	require.Equal(t, "started", tool["status"])
	args := tool["arguments"].(map[string]any)
	require.Equal(t, "a.txt", args["path"], "字符串 arguments 应被 JSON 解析")
}

// TestPublicEventPayload_toolResult状态映射
// Python: contracts.py:118-122 success is False → failed，否则 completed
func TestPublicEventPayload_toolResult状态映射(t *testing.T) {
	failed := PublicEventPayload("chat.tool_result", map[string]any{
		"tool_call": map[string]any{"tool_call_id": "c1", "name": "rm"},
		"success":   false,
	})
	require.Equal(t, "failed", failed.(map[string]any)["tool"].(map[string]any)["status"])

	ok := PublicEventPayload("chat.tool_result", map[string]any{
		"tool_call": map[string]any{"tool_call_id": "c1", "name": "rm"},
		"success":   true,
	})
	require.Equal(t, "completed", ok.(map[string]any)["tool"].(map[string]any)["status"])
}

// TestPublicEventPayload_toolArguments非法JSON置nil
// Python: contracts.py:100-110 解析失败 → arguments = None
func TestPublicEventPayload_toolArguments非法JSON置nil(t *testing.T) {
	out := PublicEventPayload("chat.tool_call", map[string]any{
		"tool_call": map[string]any{"name": "x", "arguments": "{not json"},
	})
	require.Nil(t, out.(map[string]any)["tool"].(map[string]any)["arguments"])
}

// TestStableEventTypes_六项
// Python: contracts.py:14-21 STABLE_EVENT_TYPES
func TestStableEventTypes_六项(t *testing.T) {
	require.Len(t, StableEventTypes, 6)
	require.Equal(t, 1, ProtocolRevision)
}
