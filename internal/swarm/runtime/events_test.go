package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestControl 验证控制事件构造（对齐 Python RuntimeEvent.control）。
func TestControl(t *testing.T) {
	sid := "sess-1"
	req := &schema.AgentRequest{RequestID: "req-1", ChannelID: "web", SessionID: &sid}

	ev := Control(req, map[string]any{"event_type": PlanModeExitedEvent, "mode": "agent.work.normal"})

	require.Equal(t, "req-1", ev.RequestID)
	require.Equal(t, "web", ev.ChannelID)
	require.Equal(t, "sess-1", ev.SessionID)
	require.Equal(t, PlanModeExitedEvent, ev.EventType())
	require.True(t, ev.OK)
	require.False(t, ev.IsComplete)
}

// TestControl_nil请求 验证 req 为 nil 时不 panic（错误路径）。
func TestControl_nil请求(t *testing.T) {
	ev := Control(nil, map[string]any{"event_type": "x"})
	require.Equal(t, "", ev.RequestID)
	require.Equal(t, "x", ev.EventType())
}

// TestFromAgentResponse 验证非流式响应包装。
func TestFromAgentResponse(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-1", ChannelID: "web", OK: true,
		Payload: map[string]any{"event_type": "chat.final", "content": "hi"},
	}

	ev := FromAgentResponse(resp)
	require.Equal(t, "chat.final", ev.EventType())
	require.True(t, ev.IsComplete)
	require.True(t, ev.OK)
	require.Equal(t, "hi", ev.Payload["content"])

	nilEv := FromAgentResponse(nil)
	require.True(t, nilEv.IsComplete)
	require.Equal(t, "", nilEv.EventType())
}

// TestFromAgentResponseChunk 验证流式片段包装与还原。
func TestFromAgentResponseChunk(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID: "req-1", ChannelID: "web", IsComplete: true,
		Payload: map[string]any{"event_type": "chat.final"},
	}

	ev := FromAgentResponseChunk(chunk)
	require.Equal(t, "chat.final", ev.EventType())
	require.True(t, ev.IsComplete)

	back := ev.ToChunk()
	require.Equal(t, chunk.RequestID, back.RequestID)
	require.Equal(t, chunk.ChannelID, back.ChannelID)
	require.Equal(t, chunk.IsComplete, back.IsComplete)
	require.Equal(t, chunk.Payload, back.Payload)

	require.True(t, FromAgentResponseChunk(nil).IsComplete)
}

// TestRuntimeEvent_ToDict 验证字典序列化字段。
func TestRuntimeEvent_ToDict(t *testing.T) {
	ev := &RuntimeEvent{
		RequestID: "req-1", ChannelID: "web", SessionID: "s1",
		Payload: map[string]any{"event_type": "plan_mode_exited"}, OK: true,
	}

	d := ev.ToDict()
	require.Equal(t, "req-1", d["request_id"])
	require.Equal(t, "web", d["channel_id"])
	require.Equal(t, "s1", d["session_id"])
	require.Equal(t, true, d["ok"])
	require.Equal(t, false, d["is_complete"])
	require.Equal(t, "plan_mode_exited", d["payload"].(map[string]any)["event_type"])
}

// TestRuntimeEvent_EventType_空负载 验证空负载与 nil 接收者返回空串。
func TestRuntimeEvent_EventType_空负载(t *testing.T) {
	require.Equal(t, "", (&RuntimeEvent{}).EventType())
	var nilEv *RuntimeEvent
	require.Equal(t, "", nilEv.EventType())
}

// TestTerminalErrorEventTypes_成员齐全 集合与上游 events.py:127-133 一致。
func TestTerminalErrorEventTypes_成员齐全(t *testing.T) {
	want := []string{"chat.error", "runtime.error", "execution.error", "team.error", "error"}
	require.Len(t, TerminalErrorEventTypes, len(want))
	for _, name := range want {
		_, ok := TerminalErrorEventTypes[name]
		require.True(t, ok, "%q 应在集合内", name)
	}
}

// TestIsTerminalErrorEvent_按类型判定 ok=True 的错误事件仍判为失败。
// Python: events.py:117-121 —— from_agent_message 的 ok 默认 True，
// 消费方必须同时看 event_type。
func TestIsTerminalErrorEvent_按类型判定(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		want      bool
	}{
		{"chat.error", "chat.error", true},
		{"runtime.error", "runtime.error", true},
		{"execution.error", "execution.error", true},
		{"team.error", "team.error", true},
		{"error", "error", true},
		{"chat.final 非错误", "chat.final", false},
		{"plan_mode_exited 非错误", "plan_mode_exited", false},
		{"空类型", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := &RuntimeEvent{Payload: map[string]any{"event_type": tc.eventType}, OK: true}
			require.Equal(t, tc.want, IsTerminalErrorEvent(ev))
		})
	}
}

// TestIsTerminalErrorEvent_nil 与空负载不 panic。
func TestIsTerminalErrorEvent_nil(t *testing.T) {
	require.False(t, IsTerminalErrorEvent(nil))
	require.False(t, IsTerminalErrorEvent(&RuntimeEvent{}))
}

// TestFromAgentResponseChunk_RuntimeCompletion透传 验证 runtime_completion
// 经 chunk → RuntimeEvent 透传（默认空串）。Python: events.py:23/:75。
func TestFromAgentResponseChunk_RuntimeCompletion透传(t *testing.T) {
	// 默认无标记
	ev := FromAgentResponseChunk(&schema.AgentResponseChunk{
		Payload: map[string]any{"event_type": "chat.delta"}, IsComplete: false,
	})
	require.Empty(t, ev.RuntimeCompletion)

	// chunk 携带标记（Python external_harness_helpers.py:_response_chunk 同款）
	ev2 := FromAgentResponseChunk(&schema.AgentResponseChunk{
		Payload:           map[string]any{"event_type": "chat.ask_user_question"},
		IsComplete:        true,
		RuntimeCompletion: "suspended",
	})
	require.Equal(t, "suspended", ev2.RuntimeCompletion)

	// 非流式响应不携带该字段（Python AgentResponse 无 runtime_completion）
	ev3 := FromAgentResponse(&schema.AgentResponse{OK: true})
	require.Empty(t, ev3.RuntimeCompletion)
}
