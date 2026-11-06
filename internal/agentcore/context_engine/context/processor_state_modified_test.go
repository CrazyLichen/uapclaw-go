package context

import (
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// msgWithID 构造带 context_message_id 的用户消息。
func msgWithID(id, text string) llm_schema.BaseMessage {
	msg := llm_schema.NewUserMessage(text)
	msg.SetMetadata(map[string]any{ContextMessageIDKey: id})
	return msg
}

// toolMsgWithID 构造带 id 的工具结果消息。
func toolMsgWithID(id, callID, text string) llm_schema.BaseMessage {
	msg := llm_schema.NewToolMessage(callID, text)
	msg.SetMetadata(map[string]any{ContextMessageIDKey: id})
	return msg
}

// offloadedToolWithID 构造已卸载的工具消息（带句柄）。
func offloadedToolWithID(id, callID, handle, offloadType string) llm_schema.BaseMessage {
	msg := schema.NewOffloadToolMessage(callID, "summary", handle, offloadType)
	msg.SetMetadata(map[string]any{ContextMessageIDKey: id})
	return msg
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildModifiedMessages_五类情况 覆盖改写/新卸载/删除/新增/无 id。
func TestBuildModifiedMessages_五类情况(t *testing.T) {
	recorder := newTestRecorder(nil, 10)

	before := []llm_schema.BaseMessage{
		msgWithID("a", "原文"),
		toolMsgWithID("b", "call-1", "原始工具结果"),
		msgWithID("c", "将被删除"),
		llm_schema.NewUserMessage("无 id 不参与"),
	}
	after := []llm_schema.BaseMessage{
		msgWithID("a", "改写后"),
		offloadedToolWithID("b", "call-1", "handle-1", "filesystem"),
		msgWithID("d", "新增"),
		llm_schema.NewUserMessage("无 id 不参与"),
	}

	got := recorder.buildModifiedMessages(before, after)
	if len(got) != 2 {
		t.Fatalf("应只记改写与新卸载两条，got %d: %+v", len(got), got)
	}
	if got[0].MessageID != "a" || got[0].OffloadHandle != "" || got[0].Role == "" {
		t.Fatalf("改写项不应带句柄且有角色: %+v", got[0])
	}
	if got[1].MessageID != "b" || got[1].OffloadHandle != "handle-1" ||
		got[1].OffloadType != "filesystem" || got[1].ToolCallID != "call-1" {
		t.Fatalf("新卸载项应带句柄/类型/调用 id: %+v", got[1])
	}
}

// TestBuildModifiedMessages_内容未变不记 验证同 id 同内容不产生条目。
func TestBuildModifiedMessages_内容未变不记(t *testing.T) {
	recorder := newTestRecorder(nil, 10)
	before := []llm_schema.BaseMessage{msgWithID("a", "内容")}
	after := []llm_schema.BaseMessage{msgWithID("a", "内容")}
	if got := recorder.buildModifiedMessages(before, after); len(got) != 0 {
		t.Fatalf("内容未变不应记录，got %+v", got)
	}
}

// TestBuildState_带modified_messages 验证状态对象与流载荷都带清单。
func TestBuildState_带modified_messages(t *testing.T) {
	recorder := newTestRecorder(nil, 10)
	now := time.Now()
	state := recorder.BuildState(ProcessorStateInput{
		OperationID:    "op-mod",
		Status:         schema.CompressionCompleted,
		Phase:          schema.CompressionPhase("add_messages"),
		BeforeMessages: []llm_schema.BaseMessage{msgWithID("a", "原文")},
		AfterMessages:  []llm_schema.BaseMessage{msgWithID("a", "改写后")},
		StartedAt:      now,
		EndedAt:        now,
		ContextMax:     100,
	})
	if len(state.ModifiedMessages) != 1 || state.ModifiedMessages[0].MessageID != "a" {
		t.Fatalf("状态应带 modified_messages，got %+v", state.ModifiedMessages)
	}

	payload := recorder.stateToMap(state)
	raw, ok := payload["modified_messages"].([]map[string]any)
	if !ok || len(raw) != 1 {
		t.Fatalf("流载荷应带 modified_messages 数组，got %v", payload["modified_messages"])
	}
	if raw[0]["message_id"] != "a" {
		t.Fatalf("键名应为 message_id，got %v", raw[0])
	}
}

// TestStateToMap_无改写时为空数组 验证前端拿到的是 [] 而不是 null。
func TestStateToMap_无改写时为空数组(t *testing.T) {
	recorder := newTestRecorder(nil, 10)
	now := time.Now()
	state := recorder.BuildState(ProcessorStateInput{
		OperationID:    "op-none",
		Status:         schema.CompressionCompleted,
		BeforeMessages: []llm_schema.BaseMessage{msgWithID("a", "内容")},
		AfterMessages:  []llm_schema.BaseMessage{msgWithID("a", "内容")},
		StartedAt:      now,
		EndedAt:        now,
	})
	raw, ok := recorder.stateToMap(state)["modified_messages"].([]map[string]any)
	if !ok || len(raw) != 0 {
		t.Fatalf("无改写时应为空数组，got %v", recorder.stateToMap(state)["modified_messages"])
	}
}
