package context_utils

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestExtractToolName(t *testing.T) {
	t.Run("Name字段有值", func(t *testing.T) {
		tc := &llm_schema.ToolCall{Name: "grep"}
		got := ExtractToolName(tc)
		if got != "grep" {
			t.Errorf("ExtractToolName() = %q, want %q", got, "grep")
		}
	})

	t.Run("Name字段为空", func(t *testing.T) {
		tc := &llm_schema.ToolCall{Name: ""}
		got := ExtractToolName(tc)
		if got != "" {
			t.Errorf("ExtractToolName() = %q, want empty string", got)
		}
	})

	t.Run("nil ToolCall", func(t *testing.T) {
		got := ExtractToolName(nil)
		if got != "" {
			t.Errorf("ExtractToolName(nil) = %q, want empty string", got)
		}
	})
}

func TestResolveToolCallFromMessage(t *testing.T) {
	t.Run("非ToolMessage返回nil", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("hello")
		got := ResolveToolCallFromMessage(msg, nil)
		if got != nil {
			t.Errorf("ResolveToolCallFromMessage() = %v, want nil", got)
		}
	})

	t.Run("ToolMessage无ToolCallID返回nil", func(t *testing.T) {
		msg := llm_schema.NewToolMessage("", "result")
		got := ResolveToolCallFromMessage(msg, nil)
		if got != nil {
			t.Errorf("ResolveToolCallFromMessage() = %v, want nil", got)
		}
	})

	t.Run("匹配到ToolCall", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_1", "result")
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "glob"},
			},
		}
		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		got := ResolveToolCallFromMessage(toolMsg, messages)
		if got == nil {
			t.Fatal("ResolveToolCallFromMessage() = nil, want non-nil")
		}
		if got.ID != "call_1" {
			t.Errorf("got.ID = %q, want %q", got.ID, "call_1")
		}
	})

	t.Run("多条AssistantMessage从后匹配", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_2", "result")
		assistant1 := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
			},
		}
		assistant2 := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_2", Name: "glob"},
			},
		}
		messages := []llm_schema.BaseMessage{assistant1, assistant2, toolMsg}
		got := ResolveToolCallFromMessage(toolMsg, messages)
		if got == nil {
			t.Fatal("ResolveToolCallFromMessage() = nil, want non-nil")
		}
		if got.Name != "glob" {
			t.Errorf("got.Name = %q, want %q", got.Name, "glob")
		}
	})

	t.Run("未匹配返回nil", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_999", "result")
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
			},
		}
		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		got := ResolveToolCallFromMessage(toolMsg, messages)
		if got != nil {
			t.Errorf("ResolveToolCallFromMessage() = %v, want nil", got)
		}
	})
}

func TestResolveToolNameFromMessage(t *testing.T) {
	t.Run("回溯找到工具名", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_1", "result")
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
			},
		}
		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		got := ResolveToolNameFromMessage(toolMsg, messages)
		if got != "grep" {
			t.Errorf("ResolveToolNameFromMessage() = %q, want %q", got, "grep")
		}
	})

	t.Run("未找到返回空字符串", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_999", "result")
		got := ResolveToolNameFromMessage(toolMsg, []llm_schema.BaseMessage{})
		if got != "" {
			t.Errorf("ResolveToolNameFromMessage() = %q, want empty string", got)
		}
	})

	t.Run("非ToolMessage返回空字符串", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("hello")
		got := ResolveToolNameFromMessage(msg, nil)
		if got != "" {
			t.Errorf("ResolveToolNameFromMessage() = %q, want empty string", got)
		}
	})
}
