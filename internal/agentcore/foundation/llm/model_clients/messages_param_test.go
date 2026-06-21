package model_clients

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestNewTextMessagesParam 测试纯文本消息参数构造。
func TestNewTextMessagesParam(t *testing.T) {
	p := NewTextMessagesParam("你好")
	if !p.IsText() {
		t.Error("IsText() 应为 true")
	}
	if p.IsMessages() {
		t.Error("IsMessages() 应为 false")
	}
	if p.IsDicts() {
		t.Error("IsDicts() 应为 false")
	}
	if p.Text() != "你好" {
		t.Errorf("Text() = %q, 期望 %q", p.Text(), "你好")
	}
	if p.IsEmpty() {
		t.Error("IsEmpty() 应为 false")
	}
}

// TestNewMessagesParam 测试消息列表参数构造。
func TestNewMessagesParam(t *testing.T) {
	p := NewMessagesParam(
		llmschema.NewUserMessage("你好"),
		llmschema.NewAssistantMessage("hi"),
	)
	if !p.IsMessages() {
		t.Error("IsMessages() 应为 true")
	}
	if p.IsText() {
		t.Error("IsText() 应为 false")
	}
	if p.IsDicts() {
		t.Error("IsDicts() 应为 false")
	}
	if len(p.Messages()) != 2 {
		t.Errorf("len(Messages()) = %d, 期望 2", len(p.Messages()))
	}
	if p.IsEmpty() {
		t.Error("IsEmpty() 应为 false")
	}
}

// TestNewMessagesParam_AssistantMessage 保留具体类型信息。
func TestNewMessagesParam_AssistantMessage(t *testing.T) {
	assistantMsg := llmschema.NewAssistantMessage("test", llmschema.WithToolCalls([]*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "get_weather", `{"city":"Beijing"}`),
	}))
	p := NewMessagesParam(assistantMsg)

	msgs := p.Messages()
	if len(msgs) != 1 {
		t.Fatalf("len(Messages()) = %d, 期望 1", len(msgs))
	}

	// 类型断言：应能获取到 *AssistantMessage
	am, ok := msgs[0].(*llmschema.AssistantMessage)
	if !ok {
		t.Fatal("消息应为 *AssistantMessage 类型")
	}
	if len(am.ToolCalls) != 1 {
		t.Errorf("ToolCalls 数量 = %d, 期望 1", len(am.ToolCalls))
	}
}

// TestNewMessagesParam_ToolMessage 保留具体类型信息。
func TestNewMessagesParam_ToolMessage(t *testing.T) {
	toolMsg := llmschema.NewToolMessage("call_1", `{"temp": 25}`)
	p := NewMessagesParam(toolMsg)

	msgs := p.Messages()
	if len(msgs) != 1 {
		t.Fatalf("len(Messages()) = %d, 期望 1", len(msgs))
	}

	// 类型断言：应能获取到 *ToolMessage
	tm, ok := msgs[0].(*llmschema.ToolMessage)
	if !ok {
		t.Fatal("消息应为 *ToolMessage 类型")
	}
	if tm.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q, 期望 %q", tm.ToolCallID, "call_1")
	}
}

// TestNewDictsMessagesParam 测试 dict 列表消息参数构造。
func TestNewDictsMessagesParam(t *testing.T) {
	dicts := []map[string]any{
		{"role": "user", "content": "你好"},
	}
	p := NewDictsMessagesParam(dicts)
	if !p.IsDicts() {
		t.Error("IsDicts() 应为 true")
	}
	if p.IsText() {
		t.Error("IsText() 应为 false")
	}
	if p.IsMessages() {
		t.Error("IsMessages() 应为 false")
	}
	if len(p.Dicts()) != 1 {
		t.Errorf("len(Dicts()) = %d, 期望 1", len(p.Dicts()))
	}
	if p.IsEmpty() {
		t.Error("IsEmpty() 应为 false")
	}
}

// TestMessagesParam_IsEmpty 测试空消息参数判断。
func TestMessagesParam_IsEmpty(t *testing.T) {
	p := MessagesParam{}
	if !p.IsEmpty() {
		t.Error("零值 MessagesParam 应为空")
	}
}

// TestMessagesParam_NilSlices 测试 nil 切片场景。
func TestMessagesParam_NilSlices(t *testing.T) {
	p := NewMessagesParam(nil)
	if p.IsMessages() {
		t.Error("nil 切片 IsMessages() 应为 false")
	}
	if !p.IsEmpty() {
		t.Error("nil 切片 IsEmpty() 应为 true")
	}

	p2 := NewDictsMessagesParam(nil)
	if p2.IsDicts() {
		t.Error("nil 切片 IsDicts() 应为 false")
	}
	if !p2.IsEmpty() {
		t.Error("nil 切片 IsEmpty() 应为 true")
	}
}

// TestMessagesParam_EmptyText 测试空字符串文本场景。
func TestMessagesParam_EmptyText(t *testing.T) {
	p := NewTextMessagesParam("")
	if p.IsText() {
		t.Error("空字符串 IsText() 应为 false")
	}
	if !p.IsEmpty() {
		t.Error("空字符串 IsEmpty() 应为 true")
	}
}

// TestToBaseMessage 已移除：convertOneMessage 参数改为 llmschema.BaseMessage，
// 不再需要 toBaseMessage 辅助函数，且 NewMessagesParam 只接受 BaseMessage 类型。
