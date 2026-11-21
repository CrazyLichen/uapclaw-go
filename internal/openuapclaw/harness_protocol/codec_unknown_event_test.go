package harness_protocol

import (
	"encoding/json"
	"testing"
)

// TestEventJsonCodec_UnknownEvent往返 对齐
// test_event_json_codec_round_trips_and_preserves_unknown_events
// （tests/unit_tests/harness_protocol/test_protocol.py:743-765）：
// ① 已知事件（OutputEvent structured/final）线格式往返等价，信封关联字段
//
//	原样保留且不带旧命名字段；
//
// ② event_type 换成未来事件 + 新 payload → 解码为 UnknownEvent 原样保留；
// ③ UnknownEvent 再编码 → event_type 保留 future_event。
func TestEventJsonCodec_UnknownEvent往返(t *testing.T) {
	// 步骤 1：构造已知事件（Python :744-761）
	content, err := FreezeJsonObject(map[string]any{"answer": []any{1.0, 2.0}})
	if err != nil {
		t.Fatal(err)
	}
	output, err := NewOutputEvent("answer-1", OutputKindStructured, content,
		OutputOperationFinal, OutputChannelAnswer, 0, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewHarnessEvent(HarnessEventOptions{
		Sequence:          7,
		Timestamp:         7.5,
		Event:             output,
		HostSessionID:     "team-session-1",
		AgentID:           "team-a_member-a",
		ProviderSessionID: "provider-session-1",
		TurnID:            "turn-1",
		CorrelationID:     "trace-1",
		CausationIDs:      []string{"message-1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 步骤 2：编码 → JSON 可序列化 + 信封字段保留（Python :763-768）
	wire := HarnessEventToDict(event)
	if _, err := json.Marshal(wire); err != nil {
		t.Fatal(err)
	}
	if wire["host_session_id"] != "team-session-1" {
		t.Fatalf("host_session_id 漂移: %v", wire["host_session_id"])
	}
	if wire["agent_id"] != "team-a_member-a" {
		t.Fatalf("agent_id 漂移: %v", wire["agent_id"])
	}
	if wire["provider_session_id"] != "provider-session-1" {
		t.Fatalf("provider_session_id 漂移: %v", wire["provider_session_id"])
	}
	for _, legacy := range []string{"team_session_id", "member_agent_id", "session_id"} {
		if _, present := wire[legacy]; present {
			t.Fatalf("旧命名信封字段 %s 不应出现", legacy)
		}
	}

	// 步骤 3：解码往返等价（Python :769）
	decoded, err := HarnessEventFromDict(wire)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Sequence != event.Sequence || decoded.Timestamp != event.Timestamp {
		t.Fatalf("信封序号/时间戳往返漂移: %d/%v", decoded.Sequence, decoded.Timestamp)
	}
	roundOutput, ok := decoded.Event.(OutputEvent)
	if !ok {
		t.Fatalf("往返后应为 OutputEvent，实为 %T", decoded.Event)
	}
	if roundOutput.OutputID != output.OutputID || roundOutput.Kind != output.Kind ||
		roundOutput.Operation != output.Operation {
		t.Fatal("OutputEvent 主体往返漂移")
	}

	// 步骤 4：未知事件类型 + 新 payload → UnknownEvent 原样保留（Python :771-775）
	wire["event_type"] = "future_event"
	wire["payload"] = map[string]any{"new_field": []any{1.0, 2.0, 3.0}}
	unknown, err := HarnessEventFromDict(wire)
	if err != nil {
		t.Fatal(err)
	}
	unknownPayload, ok := unknown.Event.(UnknownEvent)
	if !ok {
		t.Fatalf("应解码为 UnknownEvent，实为 %T", unknown.Event)
	}
	if unknownPayload.EventType != "future_event" {
		t.Fatalf("EventType 保留失败: %v", unknownPayload.EventType)
	}
	payloadMap, ok := JsonValueToBuiltin(unknownPayload.Payload).(map[string]any)
	if !ok {
		t.Fatalf("Payload 应为对象，实为 %T", unknownPayload.Payload)
	}
	newField, ok := payloadMap["new_field"].([]any)
	if !ok || len(newField) != 3 {
		t.Fatalf("未知字段保留失败: %v", payloadMap["new_field"])
	}

	// 步骤 5：UnknownEvent 再编码 → event_type 保留（Python :777）
	rewire := HarnessEventToDict(unknown)
	if rewire["event_type"] != "future_event" {
		t.Fatalf("UnknownEvent 再编码 event_type 丢失: %v", rewire["event_type"])
	}
}
