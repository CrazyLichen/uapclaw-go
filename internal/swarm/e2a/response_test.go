package e2a

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 工厂默认值 ────────────────────────────

// TestNewE2AResponse_默认值 验证工厂函数默认值
func TestNewE2AResponse_默认值(t *testing.T) {
	r := NewE2AResponse()
	if r.ProtocolVersion != E2AProtocolVersion {
		t.Errorf("ProtocolVersion 期望 %q，实际 %q", E2AProtocolVersion, r.ProtocolVersion)
	}
	if r.Status != E2AResponseStatusInProgress {
		t.Errorf("Status 期望 %q，实际 %q", E2AResponseStatusInProgress, r.Status)
	}
	if r.IdentityOrigin != IdentityOriginAgent {
		t.Errorf("IdentityOrigin 期望 %q，实际 %q", IdentityOriginAgent, r.IdentityOrigin)
	}
	if r.Sequence != 0 {
		t.Errorf("Sequence 期望 0，实际 %d", r.Sequence)
	}
	if r.IsFinal != false {
		t.Error("IsFinal 期望 false")
	}
}

// ──────────────────────────── EnsureTimestamp ────────────────────────────

// TestE2AResponse_EnsureTimestamp_未设置 验证空串时填充 RFC3339
func TestE2AResponse_EnsureTimestamp_未设置(t *testing.T) {
	r := NewE2AResponse()
	r.EnsureTimestamp()
	if r.Timestamp == "" {
		t.Error("EnsureTimestamp 后 Timestamp 仍为空串")
	}
}

// TestE2AResponse_EnsureTimestamp_已设置 验证非空时不覆盖
func TestE2AResponse_EnsureTimestamp_已设置(t *testing.T) {
	r := NewE2AResponse()
	r.Timestamp = "2026-01-01T00:00:00Z"
	r.EnsureTimestamp()
	if r.Timestamp != "2026-01-01T00:00:00Z" {
		t.Errorf("已设置的 Timestamp 不应被覆盖，实际 %q", r.Timestamp)
	}
}

// ──────────────────────────── ToMap ────────────────────────────

// TestE2AResponse_ToMap_枚举值 验证 IdentityOrigin 输出字符串值
func TestE2AResponse_ToMap_枚举值(t *testing.T) {
	r := NewE2AResponse()
	m := r.ToMap()
	origin, ok := m["identity_origin"]
	if !ok {
		t.Fatal("ToMap 输出缺少 identity_origin 键")
	}
	if origin != "agent" {
		t.Errorf("identity_origin 期望 %q，实际 %q", "agent", origin)
	}
}

// TestE2AResponse_ToMap_嵌套Provenance 验证 Provenance 展开为 map
func TestE2AResponse_ToMap_嵌套Provenance(t *testing.T) {
	r := NewE2AResponse()
	m := r.ToMap()
	prov, ok := m["provenance"]
	if !ok {
		t.Fatal("ToMap 输出缺少 provenance 键")
	}
	provMap, ok := prov.(map[string]any)
	if !ok {
		t.Fatalf("provenance 期望 map[string]any，实际 %T", prov)
	}
	if provMap["source_protocol"] != E2ASourceProtocolE2A {
		t.Errorf("provenance.source_protocol 期望 %q，实际 %v", E2ASourceProtocolE2A, provMap["source_protocol"])
	}
}

// TestE2AResponse_ToMap_全部字段 验证 22 字段全部输出
func TestE2AResponse_ToMap_全部字段(t *testing.T) {
	r := NewE2AResponse()
	r.RequestID = "req-001"
	r.ResponseID = "resp-001"
	r.ResponseKind = E2AResponseKindE2AComplete
	r.Body = map[string]any{"content": "hello"}
	m := r.ToMap()
	for _, key := range []string{"protocol_version", "response_id", "request_id", "sequence", "is_final", "status", "response_kind", "timestamp", "provenance", "body", "identity_origin"} {
		if _, ok := m[key]; !ok {
			t.Errorf("ToMap 输出缺少 %q 键", key)
		}
	}
}

// ──────────────────────────── ResponseFromMap ────────────────────────────

// TestResponseFromMap_完整字段 验证所有字段解析
func TestResponseFromMap_完整字段(t *testing.T) {
	data := map[string]any{
		"protocol_version": "1.0",
		"response_id":      "resp-001",
		"request_id":       "req-001",
		"sequence":         float64(5),
		"is_final":         true,
		"status":           "succeeded",
		"response_kind":    "e2a.complete",
		"timestamp":        "2026-01-01T00:00:00Z",
		"body":             map[string]any{"content": "hello"},
		"jsonrpc_id":       42,
		"correlation_id":   "corr-001",
		"task_id":          "task-001",
		"context_id":       "ctx-001",
		"session_id":       "sess-001",
		"message_id":       "msg-001",
		"is_stream":        true,
		"identity_origin":  "user",
		"channel":          "cli",
		"user_id":          "user-001",
		"source_agent_id":  "agent-001",
		"method":           "chat.send",
		"projections":      map[string]any{"p1": "v1"},
		"channel_context":  map[string]any{"cc": "val"},
		"metadata":         map[string]any{"md": "val"},
		"a2a_metadata":     map[string]any{"a2a": "val"},
		"acp_meta":         map[string]any{"acp": "val"},
	}
	r := ResponseFromMap(data)
	if r.ResponseID != "resp-001" {
		t.Errorf("ResponseID 期望 %q，实际 %q", "resp-001", r.ResponseID)
	}
	if r.RequestID != "req-001" {
		t.Errorf("RequestID 期望 %q，实际 %q", "req-001", r.RequestID)
	}
	if r.Sequence != 5 {
		t.Errorf("Sequence 期望 5，实际 %d", r.Sequence)
	}
	if !r.IsFinal {
		t.Error("IsFinal 期望 true")
	}
	if r.Status != "succeeded" {
		t.Errorf("Status 期望 %q，实际 %q", "succeeded", r.Status)
	}
	if r.ResponseKind != "e2a.complete" {
		t.Errorf("ResponseKind 期望 %q，实际 %q", "e2a.complete", r.ResponseKind)
	}
	if r.IdentityOrigin != IdentityOriginUser {
		t.Errorf("IdentityOrigin 期望 %q，实际 %q", IdentityOriginUser, r.IdentityOrigin)
	}
}

// TestResponseFromMap_identity_origin默认AGENT 验证无 identity_origin 时默认 AGENT
func TestResponseFromMap_identity_origin默认AGENT(t *testing.T) {
	data := map[string]any{}
	r := ResponseFromMap(data)
	if r.IdentityOrigin != IdentityOriginAgent {
		t.Errorf("IdentityOrigin 期望 %q，实际 %q", IdentityOriginAgent, r.IdentityOrigin)
	}
}

// TestResponseFromMap_sequence容错_float64 验证 float64→int
func TestResponseFromMap_sequence容错_float64(t *testing.T) {
	data := map[string]any{"sequence": float64(42)}
	r := ResponseFromMap(data)
	if r.Sequence != 42 {
		t.Errorf("Sequence 期望 42，实际 %d", r.Sequence)
	}
}

// TestResponseFromMap_sequence容错_非数字 验证非数字字符串→0
func TestResponseFromMap_sequence容错_非数字(t *testing.T) {
	data := map[string]any{"sequence": "not_a_number"}
	r := ResponseFromMap(data)
	if r.Sequence != 0 {
		t.Errorf("Sequence 期望 0，实际 %d", r.Sequence)
	}
}

// TestResponseFromMap_channel兼容channel_id 验证 channel 为空时取 channel_id
func TestResponseFromMap_channel兼容channel_id(t *testing.T) {
	data := map[string]any{"channel_id": "cli"}
	r := ResponseFromMap(data)
	if r.Channel != "cli" {
		t.Errorf("Channel 期望 %q，实际 %q", "cli", r.Channel)
	}
}

// TestResponseFromMap_timestamp规范化 验证 float 纪元秒→RFC3339
func TestResponseFromMap_timestamp规范化(t *testing.T) {
	data := map[string]any{"timestamp": float64(1735689600)}
	r := ResponseFromMap(data)
	if r.Timestamp == "" {
		t.Error("Timestamp 为空串")
	}
}

// TestResponseFromMap_status默认in_progress 验证无 status 时默认 "in_progress"
func TestResponseFromMap_status默认in_progress(t *testing.T) {
	data := map[string]any{}
	r := ResponseFromMap(data)
	if r.Status != E2AResponseStatusInProgress {
		t.Errorf("Status 期望 %q，实际 %q", E2AResponseStatusInProgress, r.Status)
	}
}

// TestResponseFromMap_response_kind空串 验证无 response_kind 时默认 ""
func TestResponseFromMap_response_kind空串(t *testing.T) {
	data := map[string]any{}
	r := ResponseFromMap(data)
	if r.ResponseKind != "" {
		t.Errorf("ResponseKind 期望空串，实际 %q", r.ResponseKind)
	}
}

// ──────────────────────────── 序列化往返 ────────────────────────────

// TestE2AResponse_JSON序列化往返 验证 JSON marshal/unmarshal 往返一致性
func TestE2AResponse_JSON序列化往返(t *testing.T) {
	original := NewE2AResponse()
	original.RequestID = "req-001"
	original.ResponseID = "resp-001"
	original.Sequence = 3
	original.IsFinal = true
	original.Status = E2AResponseStatusSucceeded
	original.ResponseKind = E2AResponseKindE2AComplete
	original.Body = map[string]any{"content": "hello world"}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var decoded E2AResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID 期望 %q，实际 %q", original.RequestID, decoded.RequestID)
	}
	if decoded.Sequence != original.Sequence {
		t.Errorf("Sequence 期望 %d，实际 %d", original.Sequence, decoded.Sequence)
	}
	if decoded.IsFinal != original.IsFinal {
		t.Errorf("IsFinal 期望 %v，实际 %v", original.IsFinal, decoded.IsFinal)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status 期望 %q，实际 %q", original.Status, decoded.Status)
	}
}

// TestResponseFromMap_ToMap_往返 验证 ResponseFromMap(ToMap(resp)) ≈ resp
func TestResponseFromMap_ToMap_往返(t *testing.T) {
	original := NewE2AResponse()
	original.RequestID = "req-001"
	original.ResponseID = "resp-001"
	original.Channel = "cli"
	original.Sequence = 7
	original.IsFinal = true
	original.ResponseKind = E2AResponseKindE2AChunk

	m := original.ToMap()
	roundtrip := ResponseFromMap(m)

	if roundtrip.RequestID != original.RequestID {
		t.Errorf("RequestID 往返不一致: 期望 %q，实际 %q", original.RequestID, roundtrip.RequestID)
	}
	if roundtrip.Sequence != original.Sequence {
		t.Errorf("Sequence 往返不一致: 期望 %d，实际 %d", original.Sequence, roundtrip.Sequence)
	}
	if roundtrip.Channel != original.Channel {
		t.Errorf("Channel 往返不一致: 期望 %q，实际 %q", original.Channel, roundtrip.Channel)
	}
}
