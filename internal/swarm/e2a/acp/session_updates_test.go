package acp

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestBuildAcpSessionUpdate_DeltaAndReasoning 分流：chat.delta → agent_message_chunk；
// chat.reasoning → agent_thought_chunk。Python: build_acp_session_update L107-124。
func TestBuildAcpSessionUpdate_DeltaAndReasoning(t *testing.T) {
	state := &AcpSessionUpdateState{}
	msg := &schema.Message{EventType: schema.EventTypeChatDelta}
	update := BuildAcpSessionUpdate(msg, map[string]any{"content": "hello"}, state)
	if update == nil {
		t.Fatalf("chat.delta should produce an update")
	}
	if update["sessionUpdate"] != "agent_message_chunk" {
		t.Errorf("sessionUpdate = %v, want agent_message_chunk", update["sessionUpdate"])
	}
	if content, _ := update["content"].(map[string]any); content["text"] != "hello" {
		t.Errorf("content.text = %v, want hello", content["text"])
	}
	if state.AssistantText != "hello" {
		t.Errorf("AssistantText = %q, want hello", state.AssistantText)
	}
	if state.AssistantMessageID == "" {
		t.Error("AssistantMessageID should be ensured")
	}
	id1 := state.AssistantMessageID

	// 第二条 delta 复用同一 messageId 且累积文本
	update2 := BuildAcpSessionUpdate(msg, map[string]any{"content": " world"}, state)
	if update2["messageId"] != id1 {
		t.Errorf("second delta should reuse messageId, got %v", update2["messageId"])
	}
	if state.AssistantText != "hello world" {
		t.Errorf("AssistantText = %q, want hello world", state.AssistantText)
	}

	// 推理分流（chat.reasoning 不带 source_chunk_type 也必须走 thought 分支——
	// Python event_type == CHAT_REASONING 恒真分支，见 is_reasoning_event L107-117）
	rmsg := &schema.Message{EventType: schema.EventTypeChatReasoning}
	rupdate := BuildAcpSessionUpdate(rmsg, map[string]any{
		"content": "thinking", "source_chunk_type": "llm_reasoning",
	}, state)
	if rupdate == nil || rupdate["sessionUpdate"] != "agent_thought_chunk" {
		t.Fatalf("reasoning event should produce agent_thought_chunk, got %v", rupdate)
	}
	if state.ThoughtText != "thinking" {
		t.Errorf("ThoughtText = %q, want thinking", state.ThoughtText)
	}
	if state.ThoughtMessageID == "" {
		t.Error("ThoughtMessageID should be ensured")
	}
}

// TestBuildAcpSessionUpdate_ReasoningWithoutSourceChunk chat.reasoning 仅凭事件类型
// 即走 thought 分支（EventType 是 defined type，须经 string() 转换后断言）。
func TestBuildAcpSessionUpdate_ReasoningWithoutSourceChunk(t *testing.T) {
	state := &AcpSessionUpdateState{}
	msg := &schema.Message{EventType: schema.EventTypeChatReasoning}
	update := BuildAcpSessionUpdate(msg, map[string]any{"content": "pure event type"}, state)
	if update == nil || update["sessionUpdate"] != "agent_thought_chunk" {
		t.Fatalf("chat.reasoning should route to thought by event type alone, got %v", update)
	}
	if state.AssistantText != "" {
		t.Errorf("AssistantText should stay empty, got %q", state.AssistantText)
	}
}

// TestBuildAcpSessionUpdate_EmptyTextNil 空 content → nil（Python str(payload.get("content") or "")）。
func TestBuildAcpSessionUpdate_EmptyTextNil(t *testing.T) {
	state := &AcpSessionUpdateState{}
	msg := &schema.Message{EventType: schema.EventTypeChatDelta}
	if u := BuildAcpSessionUpdate(msg, map[string]any{"content": ""}, state); u != nil {
		t.Error("empty content should return nil")
	}
	if u := BuildAcpSessionUpdate(msg, nil, state); u != nil {
		t.Error("nil payload should return nil")
	}
}

// TestBuildAcpSessionUpdate_ToolCall 分流到 BuildAcpToolCallUpdate 并维护 cache。
func TestBuildAcpSessionUpdate_ToolCall(t *testing.T) {
	state := &AcpSessionUpdateState{}
	msg := &schema.Message{EventType: schema.EventTypeChatToolCall}
	update := BuildAcpSessionUpdate(msg, map[string]any{
		"tool_call": map[string]any{
			"id":   "tc-1",
			"name": "read_file",
			"arguments": map[string]any{
				"path": "a.txt",
			},
		},
	}, state)
	if update == nil || update["sessionUpdate"] != "tool_call" {
		t.Fatalf("chat.tool_call should produce tool_call update, got %v", update)
	}
	if state.ToolCallCache == nil || state.ToolCallCache["tc-1"] == nil {
		t.Error("ToolCallCache should be ensured and populated for tc-1")
	}
}

// TestBuildAcpSessionUpdate_ToolResult 分流到 BuildAcpToolResultUpdate。
func TestBuildAcpSessionUpdate_ToolResult(t *testing.T) {
	state := &AcpSessionUpdateState{}
	msg := &schema.Message{EventType: schema.EventTypeChatToolResult}
	update := BuildAcpSessionUpdate(msg, map[string]any{
		"tool_call_id": "tc-1",
		"result":       "done",
	}, state)
	if update == nil || update["sessionUpdate"] != "tool_call_update" {
		t.Fatalf("chat.tool_result should produce tool_call_update, got %v", update)
	}
}

// TestBuildAcpSessionUpdate_SubtaskUpdate plan 原样拷贝 payload。
func TestBuildAcpSessionUpdate_SubtaskUpdate(t *testing.T) {
	state := &AcpSessionUpdateState{}
	msg := &schema.Message{EventType: schema.EventTypeChatSubtaskUpdate}
	update := BuildAcpSessionUpdate(msg, map[string]any{"tasks": []any{"a", "b"}}, state)
	if update == nil || update["sessionUpdate"] != "plan" {
		t.Fatalf("chat.subtask_update should produce plan update, got %v", update)
	}
	plan, _ := update["plan"].(map[string]any)
	if plan == nil || plan["tasks"] == nil {
		t.Error("plan should be a shallow copy of payload")
	}
}

// TestBuildAcpSessionUpdate_TodoUpdated 分流到 BuildAcpTodoUpdate。
func TestBuildAcpSessionUpdate_TodoUpdated(t *testing.T) {
	state := &AcpSessionUpdateState{}
	msg := &schema.Message{EventType: schema.EventTypeTodoUpdated}
	update := BuildAcpSessionUpdate(msg, map[string]any{
		"todos": []any{map[string]any{"content": "a", "status": "pending"}},
	}, state)
	if update == nil || update["sessionUpdate"] != "todo_update" {
		t.Fatalf("todo.updated should produce todo_update, got %v", update)
	}
}

// TestBuildAcpSessionUpdate_ProcessingStatus 三态：true→processing、false→idle、缺省→processing。
func TestBuildAcpSessionUpdate_ProcessingStatus(t *testing.T) {
	state := &AcpSessionUpdateState{}
	msg := &schema.Message{EventType: schema.EventTypeChatProcessingStatus}
	u1 := BuildAcpSessionUpdate(msg, map[string]any{"is_processing": true}, state)
	if u1 == nil || u1["status"] != "processing" {
		t.Errorf("is_processing=true → processing, got %v", u1)
	}
	u2 := BuildAcpSessionUpdate(msg, map[string]any{"is_processing": false}, state)
	if u2 == nil || u2["status"] != "idle" {
		t.Errorf("is_processing=false → idle, got %v", u2)
	}
	u3 := BuildAcpSessionUpdate(msg, map[string]any{}, state)
	if u3 == nil || u3["status"] != "processing" {
		t.Errorf("missing is_processing → processing (Python default True), got %v", u3)
	}
}

// TestBuildAcpSessionUpdate_OtherEventNil 其余事件返回 nil。
func TestBuildAcpSessionUpdate_OtherEventNil(t *testing.T) {
	state := &AcpSessionUpdateState{}
	for _, et := range []schema.EventType{
		schema.EventTypeChatFinal, schema.EventTypeChatError, schema.EventTypeHello,
	} {
		msg := &schema.Message{EventType: et}
		if u := BuildAcpSessionUpdate(msg, map[string]any{"content": "x"}, state); u != nil {
			t.Errorf("event %s should return nil, got %v", et, u)
		}
	}
}

// TestBuildAcpFinalTextUpdate_IncrementalThreeStates 增量三态：
// 全新直发 → 前缀扩展 delta → 回退重置换 messageId。Python: _build_incremental_text_update L59-98。
func TestBuildAcpFinalTextUpdate_IncrementalThreeStates(t *testing.T) {
	// 态一：state 为空 → 直发全文 + ensure messageId
	s := &AcpSessionUpdateState{}
	u1 := BuildAcpFinalTextUpdate(map[string]any{"content": "hello"}, s)
	if u1 == nil || u1["sessionUpdate"] != "agent_message_chunk" {
		t.Fatalf("fresh text should emit agent_message_chunk, got %v", u1)
	}
	if s.AssistantText != "hello" || s.AssistantMessageID == "" {
		t.Errorf("after fresh: text=%q id=%q", s.AssistantText, s.AssistantMessageID)
	}
	id1 := s.AssistantMessageID

	// 态二：前缀扩展 → 只发 delta，messageId 不变
	u2 := BuildAcpFinalTextUpdate(map[string]any{"content": "hello world"}, s)
	if u2 == nil {
		t.Fatal("prefix extension should emit delta")
	}
	if u2["messageId"] != id1 {
		t.Errorf("prefix extension should keep messageId, got %v", u2["messageId"])
	}
	c2, _ := u2["content"].(map[string]any)
	if c2["text"] != " world" {
		t.Errorf("delta text = %v, want ' world'", c2["text"])
	}
	if s.AssistantText != "hello world" {
		t.Errorf("AssistantText = %q, want hello world", s.AssistantText)
	}

	// 态三：回退（非前缀扩展）→ 全文重发 + reset 换新 messageId
	u3 := BuildAcpFinalTextUpdate(map[string]any{"content": "short"}, s)
	if u3 == nil {
		t.Fatal("rollback should emit full text with new messageId")
	}
	if u3["messageId"] == id1 {
		t.Error("rollback should reset messageId to a new uuid")
	}
	c3, _ := u3["content"].(map[string]any)
	if c3["text"] != "short" {
		t.Errorf("rollback text = %v, want short", c3["text"])
	}
	if s.AssistantText != "short" {
		t.Errorf("AssistantText = %q, want short after rollback", s.AssistantText)
	}
}

// TestBuildAcpFinalTextUpdate_SameTextNil 文本与既有完全相同 → nil。
func TestBuildAcpFinalTextUpdate_SameTextNil(t *testing.T) {
	s := &AcpSessionUpdateState{}
	BuildAcpFinalTextUpdate(map[string]any{"content": "abc"}, s)
	if u := BuildAcpFinalTextUpdate(map[string]any{"content": "abc"}, s); u != nil {
		t.Error("identical text should return nil (Python text == existing_text → None)")
	}
}

// TestBuildAcpFinalTextUpdate_EmptyTextNil 空 content → nil。
func TestBuildAcpFinalTextUpdate_EmptyTextNil(t *testing.T) {
	if u := BuildAcpFinalTextUpdate(map[string]any{"content": ""}, &AcpSessionUpdateState{}); u != nil {
		t.Error("empty content should return nil")
	}
}

// TestBuildAcpFinalTextUpdate_ReasoningBranch chat.final + is_reasoning_event → thought 分流。
// Python L158-166：is_reasoning_event("chat.final", payload) 用 source_chunk_type/event_type 判定。
func TestBuildAcpFinalTextUpdate_ReasoningBranch(t *testing.T) {
	s := &AcpSessionUpdateState{}
	u := BuildAcpFinalTextUpdate(map[string]any{
		"content": "chain of thought", "source_chunk_type": "llm_reasoning",
	}, s)
	if u == nil || u["sessionUpdate"] != "agent_thought_chunk" {
		t.Fatalf("reasoning final should emit agent_thought_chunk, got %v", u)
	}
	if s.ThoughtText != "chain of thought" {
		t.Errorf("ThoughtText = %q, want 'chain of thought'", s.ThoughtText)
	}
	if s.ThoughtMessageID == "" {
		t.Error("ThoughtMessageID should be ensured")
	}
	if s.AssistantText != "" {
		t.Errorf("AssistantText should stay empty, got %q", s.AssistantText)
	}
}

// TestBuildAcpUsageUpdate usage dict 非空校验 + 一层浅拷贝。
func TestBuildAcpUsageUpdate(t *testing.T) {
	if u := BuildAcpUsageUpdate(map[string]any{}); u != nil {
		t.Error("missing usage key → nil")
	}
	if u := BuildAcpUsageUpdate(map[string]any{"usage": nil}); u != nil {
		t.Error("usage nil → nil")
	}
	if u := BuildAcpUsageUpdate(map[string]any{"usage": map[string]any{}}); u != nil {
		t.Error("empty usage dict → nil")
	}
	payload := map[string]any{"usage": map[string]any{"input_tokens": 1.0}}
	u := BuildAcpUsageUpdate(payload)
	if u == nil || u["sessionUpdate"] != "usage_update" {
		t.Fatalf("usage present → usage_update, got %v", u)
	}
	usage, _ := u["usage"].(map[string]any)
	if usage["input_tokens"] != 1.0 {
		t.Errorf("usage copy lost input_tokens, got %v", usage["input_tokens"])
	}
	usage["input_tokens"] = 999.0
	if payload["usage"].(map[string]any)["input_tokens"] != 1.0 {
		t.Error("returned usage should be a copy, mutation must not leak into payload")
	}
}
