package native

import (
	"encoding/json"
	"testing"

	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// TestSavedInput_CaptureRestore 对齐 checkpoint.py:27-37。
func TestSavedInput_CaptureRestore(t *testing.T) {
	turn := &harness_providers.PendingTurn{
		Content: hp.HarnessInput{
			Content:  "hello",
			Metadata: hp.JsonObject{"k": "v"},
		},
		MessageID:    "msg-1",
		TurnID:       "turn-1",
		AcceptedMode: hp.DeliveryModeFollowUp,
	}
	s := CaptureSavedInput(turn)
	if s.MessageID != "msg-1" || s.TurnID != "turn-1" || s.Mode != hp.DeliveryModeFollowUp {
		t.Fatalf("capture 字段错: %+v", s)
	}
	if s.Content != "hello" {
		t.Fatalf("content = %v", s.Content)
	}
	restored := s.Restore()
	if restored.MessageID != "msg-1" || restored.TurnID != "turn-1" ||
		restored.AcceptedMode != hp.DeliveryModeFollowUp {
		t.Fatalf("restore 字段错: %+v", restored)
	}
	if restored.Content.Content != "hello" {
		t.Fatalf("restore content = %v", restored.Content.Content)
	}
}

// TestSavedInput_JSON往返 对齐 pydantic model_dump(mode="json") + model_validate。
func TestSavedInput_JSON往返(t *testing.T) {
	s := SavedInput{
		MessageID: "m", TurnID: "t", Content: "q",
		Metadata: map[string]any{"a": float64(1)}, Mode: hp.DeliveryModeAuto,
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back SavedInput
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.MessageID != "m" || back.TurnID != "t" || back.Mode != hp.DeliveryModeAuto {
		t.Fatalf("JSON 往返失真: %+v", back)
	}
}

// TestNativeCheckpoint_九字段 对齐 checkpoint.py:40-52。
func TestNativeCheckpoint_九字段(t *testing.T) {
	pq := "original"
	cp := NativeCheckpoint{
		SessionID:   "host:agent",
		CardID:      "card-1",
		Cwd:         "/tmp/w",
		Contexts:    map[string]map[string]any{"c1": {"messages": []any{}}},
		DeepAgent:   map[string]any{"stop_condition_state": map[string]any{"iteration": float64(1)}},
		PausedInput: &SavedInput{MessageID: "m", TurnID: "t", Mode: hp.DeliveryModeAuto},
		PausedQuery: &pq,
		Queued:      []SavedInput{{MessageID: "m2", TurnID: "t2", Mode: hp.DeliveryModeFollowUp}},
		OutputState: map[string]any{"final_output": "x"},
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"session_id", "card_id", "cwd", "contexts", "deepagent",
		"paused_input", "paused_query", "queued", "output_state",
	} {
		if _, ok := m[key]; !ok {
			t.Fatalf("缺 JSON 键 %s", key)
		}
	}
}

// TestDecodeContexts_逐context调用 对齐 checkpoint.py:54-55。
func TestDecodeContexts_逐context调用(t *testing.T) {
	cp := NativeCheckpoint{
		Contexts: map[string]map[string]any{
			"c1": {"messages": []any{map[string]any{"role": "user", "content": "a"}}},
			"c2": {"messages": []any{map[string]any{"role": "tool", "content": "b", "tool_call_id": "tc"}}},
		},
	}
	got := cp.DecodeContexts()
	if len(got) != 2 {
		t.Fatalf("应逐 context 解码，得 %d 个", len(got))
	}
	for _, key := range []string{"c1", "c2"} {
		if _, ok := got[key]["messages"]; !ok {
			t.Fatalf("%s 缺 messages 键", key)
		}
	}
}

// TestEncodeContexts_空集语义 对齐 checkpoint.py:58-59 的 contexts or {}。
func TestEncodeContexts_空集语义(t *testing.T) {
	if got := EncodeContexts(nil); len(got) != 0 {
		t.Fatalf("nil 应产出空 map，得 %#v", got)
	}
	if got := EncodeContexts(map[string]map[string]any{"c": {"messages": []any{}}}); len(got) != 1 {
		t.Fatalf("单 context 应保留，得 %#v", got)
	}
}

// TestNativeCheckpoint_未知键拒绝 对齐 pydantic ConfigDict(extra="forbid")。
func TestNativeCheckpoint_未知键拒绝(t *testing.T) {
	raw := []byte(`{"session_id":"s","card_id":"c","contexts":{},"deepagent":{},"unknown":1}`)
	if err := ValidateNativeCheckpointJSON(raw); err == nil {
		t.Fatal("未知键应被拒绝")
	}
}
