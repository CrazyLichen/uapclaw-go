package processor

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestProcessorType_默认返回空字符串 验证基类 ProcessorType 默认值
func TestProcessorType_默认返回空字符串(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	if pt := p.ProcessorType(); pt != "" {
		t.Errorf("ProcessorType() = %q, want 空字符串", pt)
	}
}

// TestOnAddMessages_默认透传 验证默认透传消息
func TestOnAddMessages_默认透传(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
	}
	event, result, err := p.OnAddMessages(context.Background(), nil, msgs)
	if err != nil {
		t.Fatalf("OnAddMessages 返回错误: %v", err)
	}
	if event != nil {
		t.Error("默认 OnAddMessages 应返回 nil ContextEvent")
	}
	if len(result) != len(msgs) {
		t.Errorf("结果消息数 = %d, want %d", len(result), len(msgs))
	}
}

// TestOnAddMessages_空消息列表 验证空消息列表透传
func TestOnAddMessages_空消息列表(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	event, result, err := p.OnAddMessages(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("OnAddMessages 返回错误: %v", err)
	}
	if event != nil {
		t.Error("默认 OnAddMessages 应返回 nil ContextEvent")
	}
	if result != nil {
		t.Errorf("空消息列表应透传 nil，实际 %v", result)
	}
}

// TestOnGetContextWindow_默认透传 验证默认透传上下文窗口
func TestOnGetContextWindow_默认透传(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	cw := context_engine.ContextWindow{
		SystemMessages:  []llm_schema.BaseMessage{llm_schema.NewSystemMessage("sys")},
		ContextMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")},
	}
	event, result, err := p.OnGetContextWindow(context.Background(), nil, cw)
	if err != nil {
		t.Fatalf("OnGetContextWindow 返回错误: %v", err)
	}
	if event != nil {
		t.Error("默认 OnGetContextWindow 应返回 nil ContextEvent")
	}
	if len(result.SystemMessages) != 1 || len(result.ContextMessages) != 1 {
		t.Error("透传后消息数量不一致")
	}
}

// TestTriggerAddMessages_默认不触发 验证默认不触发
func TestTriggerAddMessages_默认不触发(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	triggered, err := p.TriggerAddMessages(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 返回错误: %v", err)
	}
	if triggered {
		t.Error("默认 TriggerAddMessages 应返回 false")
	}
}

// TestTriggerGetContextWindow_默认不触发 验证默认不触发
func TestTriggerGetContextWindow_默认不触发(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	triggered, err := p.TriggerGetContextWindow(context.Background(), nil, context_engine.ContextWindow{})
	if err != nil {
		t.Fatalf("TriggerGetContextWindow 返回错误: %v", err)
	}
	if triggered {
		t.Error("默认 TriggerGetContextWindow 应返回 false")
	}
}
