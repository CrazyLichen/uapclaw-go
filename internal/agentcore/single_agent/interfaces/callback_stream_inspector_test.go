package interfaces

import (
	"context"
	"errors"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// inspectorA / inspectorB 代码指针不同的两个检查器，用于验证读写与去重前提。
func inspectorA(_ context.Context, _ *AgentCallbackContext, _ *llmschema.AssistantMessageChunk) error {
	return nil
}

func inspectorB(_ context.Context, _ *AgentCallbackContext, _ *llmschema.AssistantMessageChunk) error {
	return errors.New("b")
}

// TestStreamChunkInspectors_读写往返 验证检查器列表的存取与副本语义。
func TestStreamChunkInspectors_读写往返(t *testing.T) {
	cbc := NewAgentCallbackContext(nil, nil, nil)
	if got := cbc.StreamChunkInspectors(); len(got) != 0 {
		t.Fatalf("初始应为空，实际 %d 条", len(got))
	}

	cbc.SetStreamChunkInspectors([]StreamChunkInspectorFunc{inspectorA, inspectorB})
	got := cbc.StreamChunkInspectors()
	if len(got) != 2 {
		t.Fatalf("期望 2 条，实际 %d", len(got))
	}

	// 返回副本：调用方修改切片不得影响内部状态
	got[0] = nil
	if cbc.StreamChunkInspectors()[0] == nil {
		t.Fatal("StreamChunkInspectors 必须返回副本")
	}

	cbc.SetStreamChunkInspectors(nil)
	if len(cbc.StreamChunkInspectors()) != 0 {
		t.Fatal("Set(nil) 应清空")
	}
}

// TestModelVisibleOutputEmitted_默认false 验证可见输出标志的默认值与读写。
func TestModelVisibleOutputEmitted_默认false(t *testing.T) {
	cbc := NewAgentCallbackContext(nil, nil, nil)
	if cbc.ModelVisibleOutputEmitted() {
		t.Fatal("默认应为 false")
	}
	cbc.SetModelVisibleOutputEmitted(true)
	if !cbc.ModelVisibleOutputEmitted() {
		t.Fatal("设置后应为 true")
	}
	cbc.SetModelVisibleOutputEmitted(false)
	if cbc.ModelVisibleOutputEmitted() {
		t.Fatal("复位后应为 false")
	}
}

// TestStreamChunkInspectors_类型不符返回空 防"检查器无声消失"：类型不符时返回空并告警。
func TestStreamChunkInspectors_类型不符返回空(t *testing.T) {
	cbc := NewAgentCallbackContext(nil, nil, nil)
	cbc.SetExtra(ExtraKeyStreamChunkInspectors, "not-a-list")
	if got := cbc.StreamChunkInspectors(); len(got) != 0 {
		t.Fatalf("类型不符应返回空，实际 %d 条", len(got))
	}
}

// TestModelVisibleOutputEmitted_类型不符按false 非 bool 值一律视为未输出。
func TestModelVisibleOutputEmitted_类型不符按false(t *testing.T) {
	cbc := NewAgentCallbackContext(nil, nil, nil)
	cbc.SetExtra(ExtraKeyModelVisibleOutputEmitted, "true")
	if cbc.ModelVisibleOutputEmitted() {
		t.Fatal("非 bool 值应视为 false")
	}
}
