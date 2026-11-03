package context

import (
	"context"
	"encoding/json"
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDecodeMessageValue_具体类型直传 验证同进程直传的消息原样返回。
func TestDecodeMessageValue_具体类型直传(t *testing.T) {
	origin := llm_schema.NewUserMessage("原样返回")
	decoded := decodeMessageValue(origin)
	if decoded != origin {
		t.Fatalf("具体类型应原样返回，实际 %T", decoded)
	}
}

// TestDecodeMessageValue_JSON形态 验证 JSON 反序列化后的字典按 role 还原为具体类型，
// 且 content / tool_calls / tool_call_id 等字段不丢。
func TestDecodeMessageValue_JSON形态(t *testing.T) {
	cases := []struct {
		name     string
		fields   map[string]any
		wantType string
	}{
		{
			name:     "user",
			fields:   map[string]any{"role": "user", "content": "用户问题"},
			wantType: "user",
		},
		{
			name: "assistant带工具调用",
			fields: map[string]any{
				"role":    "assistant",
				"content": "调用工具",
				"tool_calls": []any{
					map[string]any{"id": "call-1", "type": "function", "name": "read_file", "arguments": "{}"},
				},
			},
			wantType: "assistant",
		},
		{
			name:     "tool",
			fields:   map[string]any{"role": "tool", "content": "工具结果", "tool_call_id": "call-1"},
			wantType: "tool",
		},
		{
			name:     "system",
			fields:   map[string]any{"role": "system", "content": "系统提示"},
			wantType: "system",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoded := decodeMessageValue(tc.fields)
			if decoded == nil {
				t.Fatalf("role=%s 应还原出消息", tc.fields["role"])
			}
			if got := decoded.GetRole().String(); got != tc.wantType {
				t.Errorf("role = %q, 期望 %q", got, tc.wantType)
			}
		})
	}

	// 工具链字段逐个校验
	assistant, ok := decodeMessageValue(cases[1].fields).(*llm_schema.AssistantMessage)
	if !ok {
		t.Fatal("assistant 应还原为 *AssistantMessage")
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call-1" {
		t.Errorf("tool_calls 未还原: %+v", assistant.ToolCalls)
	}
	toolMsg, ok := decodeMessageValue(cases[2].fields).(*llm_schema.ToolMessage)
	if !ok {
		t.Fatal("tool 应还原为 *ToolMessage")
	}
	if toolMsg.ToolCallID != "call-1" {
		t.Errorf("tool_call_id = %q, 期望 call-1", toolMsg.ToolCallID)
	}
}

// TestDecodeMessageValue_无法识别 验证未知 role 与非字典输入返回 nil。
func TestDecodeMessageValue_无法识别(t *testing.T) {
	if decoded := decodeMessageValue(map[string]any{"role": "unknown", "content": "x"}); decoded != nil {
		t.Errorf("未知 role 应返回 nil，实际 %T", decoded)
	}
	if decoded := decodeMessageValue("raw-string"); decoded != nil {
		t.Errorf("非字典输入应返回 nil，实际 %T", decoded)
	}
	if decoded := decodeMessageValue(nil); decoded != nil {
		t.Errorf("nil 输入应返回 nil，实际 %T", decoded)
	}
}

// TestDecodeMessageValue_字段非法 验证各 role 反序列化失败时返回 nil 而非 panic。
func TestDecodeMessageValue_字段非法(t *testing.T) {
	for _, role := range []string{"user", "assistant", "tool", "system"} {
		t.Run(role, func(t *testing.T) {
			// content 为数字，MessageContent 既不是字符串也不是分片数组
			if decoded := decodeMessageValue(map[string]any{"role": role, "content": 123}); decoded != nil {
				t.Errorf("role=%s content 非法时应返回 nil，实际 %T", role, decoded)
			}
		})
	}
}

// TestMessagesFromState_两种形态 验证消息列表在具体类型与 JSON 形态下都能还原。
func TestMessagesFromState_两种形态(t *testing.T) {
	typed := []llm_schema.BaseMessage{llm_schema.NewUserMessage("a"), llm_schema.NewAssistantMessage("b")}
	if got := messagesFromState(typed); len(got) != 2 {
		t.Errorf("具体类型形态还原 %d 条, 期望 2 条", len(got))
	}

	jsonShaped := []any{
		map[string]any{"role": "user", "content": "a"},
		map[string]any{"role": "assistant", "content": "b"},
		map[string]any{"role": "unknown", "content": "丢弃"},
	}
	if got := messagesFromState(jsonShaped); len(got) != 2 {
		t.Errorf("JSON 形态还原 %d 条, 期望 2 条（未知 role 丢弃）", len(got))
	}

	if got := messagesFromState("not-a-list"); got != nil {
		t.Errorf("非列表应返回 nil，实际 %v", got)
	}
	// 识别到的空列表应返回非 nil，供调用方区分"空状态"与"无法识别"
	if got := messagesFromState([]any{}); got == nil {
		t.Error("空列表应返回非 nil 空切片")
	}
}

// TestOffloadMessagesFromState_两种形态 验证卸载消息缓冲区还原。
func TestOffloadMessagesFromState_两种形态(t *testing.T) {
	typed := map[string][]llm_schema.BaseMessage{
		"h1": {llm_schema.NewUserMessage("卸载")},
	}
	if got := offloadMessagesFromState(typed); len(got) != 1 || len(got["h1"]) != 1 {
		t.Errorf("具体类型形态还原失败: %+v", got)
	}

	jsonShaped := map[string]any{
		"h1": []any{map[string]any{"role": "user", "content": "卸载"}},
	}
	got := offloadMessagesFromState(jsonShaped)
	if len(got["h1"]) != 1 {
		t.Errorf("JSON 形态还原失败: %+v", got)
	}

	if got := offloadMessagesFromState(42); got != nil {
		t.Errorf("非字典应返回 nil，实际 %v", got)
	}
}

// TestCompressionHistoryFromState_两种形态 验证压缩历史还原。
func TestCompressionHistoryFromState_两种形态(t *testing.T) {
	typed := []map[string]any{{"operation_id": "op1"}}
	if got := compressionHistoryFromState(typed); len(got) != 1 || got[0]["operation_id"] != "op1" {
		t.Errorf("具体类型形态还原失败: %+v", got)
	}

	jsonShaped := []any{map[string]any{"operation_id": "op1"}, "非字典记录丢弃"}
	if got := compressionHistoryFromState(jsonShaped); len(got) != 1 {
		t.Errorf("JSON 形态还原 %d 条, 期望 1 条", len(got))
	}

	if got := compressionHistoryFromState("not-a-list"); got != nil {
		t.Errorf("非列表应返回 nil，实际 %v", got)
	}
}

// TestSessionModelContext_LoadState_JSON落盘往返 覆盖进程重启场景：
// 上下文状态经 JSON 序列化（检查点落盘）后再 LoadState，消息与工具链必须完整还原。
func TestSessionModelContext_LoadState_JSON落盘往返(t *testing.T) {
	mc := newTestSessionModelContext()
	assistant := llm_schema.NewAssistantMessage("调用工具",
		llm_schema.WithToolCalls([]*llm_schema.ToolCall{llm_schema.NewToolCall("call-1", "read_file", "{}")}))
	_, err := mc.AddMessages(context.Background(), []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("重启前的问题"),
		assistant,
		llm_schema.NewToolMessage("call-1", "重启前的工具结果"),
	})
	if err != nil {
		t.Fatalf("AddMessages 失败: %v", err)
	}

	// 模拟 SaveContexts → session 全局状态 → checkpointer JSON 落盘
	raw, err := json.Marshal(map[string]any{mc.ContextID(): mc.SaveState()})
	if err != nil {
		t.Fatalf("序列化上下文状态失败: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("反序列化上下文状态失败: %v", err)
	}

	restored := newTestSessionModelContext()
	restored.LoadState(decoded)

	if restored.Len() != 3 {
		t.Fatalf("重启后恢复 %d 条消息, 期望 3 条", restored.Len())
	}
	messages, err := restored.GetMessages(0, true)
	if err != nil {
		t.Fatalf("GetMessages 失败: %v", err)
	}
	toolMsg, ok := messages[2].(*llm_schema.ToolMessage)
	if !ok {
		t.Fatalf("第 3 条应为 *ToolMessage，实际 %T", messages[2])
	}
	if toolMsg.ToolCallID != "call-1" {
		t.Errorf("tool_call_id = %q, 期望 call-1", toolMsg.ToolCallID)
	}
	assistantMsg, ok := messages[1].(*llm_schema.AssistantMessage)
	if !ok {
		t.Fatalf("第 2 条应为 *AssistantMessage，实际 %T", messages[1])
	}
	if len(assistantMsg.ToolCalls) != 1 || assistantMsg.ToolCalls[0].ID != "call-1" {
		t.Errorf("tool_calls 未还原: %+v", assistantMsg.ToolCalls)
	}
}

// TestSessionModelContext_LoadState_异常状态 验证无法识别的 messages 形态与非法消息
// 不会污染已有上下文（前者告警后跳过，后者整次恢复中止）。
func TestSessionModelContext_LoadState_异常状态(t *testing.T) {
	t.Run("messages 形态无法识别", func(t *testing.T) {
		mc := newTestSessionModelContext()
		mc.LoadState(map[string]any{
			mc.ContextID(): map[string]any{"messages": 42},
		})
		if mc.Len() != 0 {
			t.Errorf("无法识别的形态不应恢复出消息, Len()=%d", mc.Len())
		}
	})

	t.Run("消息含 nil 元素中止恢复", func(t *testing.T) {
		mc := newTestSessionModelContext()
		mc.LoadState(map[string]any{
			mc.ContextID(): map[string]any{
				"messages": []llm_schema.BaseMessage{nil},
			},
		})
		if mc.Len() != 0 {
			t.Errorf("校验失败应中止恢复, Len()=%d", mc.Len())
		}
	})
}
