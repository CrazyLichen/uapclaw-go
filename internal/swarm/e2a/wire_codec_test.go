package e2a

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── IsE2AResponseWireDict ────────────────────────────

// TestIsE2AResponseWireDict_E2A格式 验证合法 E2A 响应格式
func TestIsE2AResponseWireDict_E2A格式(t *testing.T) {
	data := map[string]any{
		"protocol_version": "1.0",
		"response_kind":    "e2a.complete",
		"request_id":       "r1",
	}
	if !IsE2AResponseWireDict(data) {
		t.Error("合法 E2A 格式应返回 true")
	}
}

// TestIsE2AResponseWireDict_事件帧 验证 type=event 返回 false
func TestIsE2AResponseWireDict_事件帧(t *testing.T) {
	data := map[string]any{
		"type":             "event",
		"protocol_version": "1.0",
		"response_kind":    "e2a.complete",
	}
	if IsE2AResponseWireDict(data) {
		t.Error("type=event 应返回 false")
	}
}

// TestIsE2AResponseWireDict_空ResponseKind 验证空 response_kind 返回 false
func TestIsE2AResponseWireDict_空ResponseKind(t *testing.T) {
	data := map[string]any{
		"protocol_version": "1.0",
		"response_kind":    "",
	}
	if IsE2AResponseWireDict(data) {
		t.Error("空 response_kind 应返回 false")
	}
}

// TestIsE2AResponseWireDict_非E2A格式 验证非 E2A 格式返回 false
func TestIsE2AResponseWireDict_非E2A格式(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
	}{
		{"nil", nil},
		{"缺少protocol_version", map[string]any{"response_kind": "e2a.complete"}},
		{"错误版本", map[string]any{"protocol_version": "2.0", "response_kind": "e2a.complete"}},
		{"缺少response_kind", map[string]any{"protocol_version": "1.0"}},
		{"response_kind为nil", map[string]any{"protocol_version": "1.0", "response_kind": nil}},
		{"response_kind非string", map[string]any{"protocol_version": "1.0", "response_kind": 123}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsE2AResponseWireDict(tc.data) {
				t.Errorf("%s 应返回 false", tc.name)
			}
		})
	}
}

// ──────────────────────────── EncodeAgentResponseForWire ────────────────────────────

// TestEncodeAgentResponseForWire_正常编码 验证正常出站编码
func TestEncodeAgentResponseForWire_正常编码(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-1",
		ChannelID: "ch-1",
		OK:        true,
		Payload:   map[string]any{"result": "ok"},
	}
	wire := EncodeAgentResponseForWire(resp, "resp-1", 0)

	if wire["protocol_version"] != E2AProtocolVersion {
		t.Errorf("protocol_version 期望 %s，实际 %v", E2AProtocolVersion, wire["protocol_version"])
	}
	if wire["response_kind"] != E2AResponseKindE2AComplete {
		t.Errorf("response_kind 期望 %s，实际 %v", E2AResponseKindE2AComplete, wire["response_kind"])
	}
	if wire["request_id"] != "req-1" {
		t.Errorf("request_id 期望 req-1，实际 %v", wire["request_id"])
	}
}

// TestEncodeAgentResponseForWire_失败响应 验证 OK=false 的编码
func TestEncodeAgentResponseForWire_失败响应(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-2",
		ChannelID: "ch-2",
		OK:        false,
		Payload:   map[string]any{"error": "something went wrong"},
	}
	wire := EncodeAgentResponseForWire(resp, "resp-2", 0)

	if wire["response_kind"] != E2AResponseKindE2AError {
		t.Errorf("response_kind 期望 %s，实际 %v", E2AResponseKindE2AError, wire["response_kind"])
	}
	if wire["status"] != E2AResponseStatusFailed {
		t.Errorf("status 期望 %s，实际 %v", E2AResponseStatusFailed, wire["status"])
	}
}

// ──────────────────────────── EncodeAgentChunkForWire ────────────────────────────

// TestEncodeAgentChunkForWire_正常编码 验证正常出站 chunk 编码
func TestEncodeAgentChunkForWire_正常编码(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "ch-1",
		Payload:    map[string]any{"event_type": "chat.delta", "content": "hello"},
		IsComplete: false,
	}
	wire := EncodeAgentChunkForWire(chunk, "resp-1", 1, true)

	if wire["protocol_version"] != E2AProtocolVersion {
		t.Errorf("protocol_version 期望 %s，实际 %v", E2AProtocolVersion, wire["protocol_version"])
	}
	if wire["request_id"] != "req-1" {
		t.Errorf("request_id 期望 req-1，实际 %v", wire["request_id"])
	}
}

// TestEncodeAgentChunkForWire_终止帧 验证 is_complete=true 的终止帧编码
func TestEncodeAgentChunkForWire_终止帧(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "ch-1",
		Payload:    map[string]any{"is_complete": true},
		IsComplete: true,
	}
	wire := EncodeAgentChunkForWire(chunk, "resp-1", 10, true)

	if wire["response_kind"] != E2AResponseKindE2AComplete {
		t.Errorf("终止帧 response_kind 期望 %s，实际 %v", E2AResponseKindE2AComplete, wire["response_kind"])
	}
}

// ──────────────────────────── ParseAgentServerWireUnary ────────────────────────────

// TestParseAgentServerWireUnary_E2A格式解析 验证 E2A 格式入站解码
func TestParseAgentServerWireUnary_E2A格式解析(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-1",
		ChannelID: "ch-1",
		OK:        true,
		Payload:   map[string]any{"answer": 42},
	}
	wire := EncodeAgentResponseForWire(resp, "resp-1", 0)

	out, err := ParseAgentServerWireUnary(wire)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.RequestID != "req-1" {
		t.Errorf("request_id 期望 req-1，实际 %s", out.RequestID)
	}
	if out.ChannelID != "ch-1" {
		t.Errorf("channel_id 期望 ch-1，实际 %s", out.ChannelID)
	}
	if out.OK != true {
		t.Error("ok 期望 true")
	}
}

// TestParseAgentServerWireUnary_LegacyFallback 验证含 legacy 键的入站走兜底
func TestParseAgentServerWireUnary_LegacyFallback(t *testing.T) {
	legacyData := map[string]any{
		"request_id": "req-legacy",
		"channel_id": "ch-legacy",
		"ok":         false,
		"payload":    map[string]any{"error": "legacy error"},
	}
	wire := map[string]any{
		"protocol_version": "1.0",
		"response_kind":    "e2a.complete",
		"request_id":       "req-legacy",
		"response_id":      "resp-legacy",
		"status":           "succeeded",
		"is_final":         true,
		"sequence":         0,
		"metadata": map[string]any{
			E2AWireLegacyAgentResponseKey: legacyData,
		},
		"body": map[string]any{
			"result": map[string]any{},
		},
		"provenance": map[string]any{
			"source_protocol": "e2a",
		},
		"identity_origin": "agent",
	}

	out, err := ParseAgentServerWireUnary(wire)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.RequestID != "req-legacy" {
		t.Errorf("request_id 期望 req-legacy，实际 %s", out.RequestID)
	}
	if out.OK != false {
		t.Error("ok 期望 false")
	}
}

// TestParseAgentServerWireUnary_非E2A格式 验证非 E2A 格式返回 error
func TestParseAgentServerWireUnary_非E2A格式(t *testing.T) {
	data := map[string]any{
		"request_id": "req-1",
		"channel_id": "ch-1",
		"ok":         true,
	}
	_, err := ParseAgentServerWireUnary(data)
	if err == nil {
		t.Error("非 E2A 格式应返回 error")
	}
}

// ──────────────────────────── ParseAgentServerWireChunk ────────────────────────────

// TestParseAgentServerWireChunk_E2A格式解析 验证 E2A 格式入站 chunk 解码
func TestParseAgentServerWireChunk_E2A格式解析(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "ch-1",
		Payload:    map[string]any{"event_type": "chat.delta", "content": "hi"},
		IsComplete: false,
	}
	wire := EncodeAgentChunkForWire(chunk, "resp-1", 1, true)

	out, err := ParseAgentServerWireChunk(wire)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.RequestID != "req-1" {
		t.Errorf("request_id 期望 req-1，实际 %s", out.RequestID)
	}
	if out.IsComplete != false {
		t.Error("is_complete 期望 false")
	}
}

// TestParseAgentServerWireChunk_LegacyFallback 验证含 legacy 键的入站 chunk 走兜底
func TestParseAgentServerWireChunk_LegacyFallback(t *testing.T) {
	legacyData := map[string]any{
		"request_id":  "req-legacy",
		"channel_id":  "ch-legacy",
		"payload":     map[string]any{"content": "chunk"},
		"is_complete": false,
	}
	wire := map[string]any{
		"protocol_version": "1.0",
		"response_kind":    "e2a.chunk",
		"request_id":       "req-legacy",
		"response_id":      "resp-legacy",
		"status":           "in_progress",
		"is_final":         false,
		"sequence":         1,
		"metadata": map[string]any{
			E2AWireLegacyAgentChunkKey: legacyData,
		},
		"body": map[string]any{
			"delta_kind": "text",
			"delta":      "chunk",
		},
		"provenance": map[string]any{
			"source_protocol": "e2a",
		},
		"identity_origin": "agent",
	}

	out, err := ParseAgentServerWireChunk(wire)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.RequestID != "req-legacy" {
		t.Errorf("request_id 期望 req-legacy，实际 %s", out.RequestID)
	}
}

// TestParseAgentServerWireChunk_非E2A格式 验证非 E2A 格式返回 error
func TestParseAgentServerWireChunk_非E2A格式(t *testing.T) {
	data := map[string]any{
		"request_id":  "req-1",
		"channel_id":  "ch-1",
		"is_complete": true,
	}
	_, err := ParseAgentServerWireChunk(data)
	if err == nil {
		t.Error("非 E2A 格式应返回 error")
	}
}

// ──────────────────────────── EncodeJSONParseErrorWire ────────────────────────────

// TestEncodeJSONParseErrorWire_错误帧结构 验证 JSON 解析错误帧结构
func TestEncodeJSONParseErrorWire_错误帧结构(t *testing.T) {
	wire := EncodeJSONParseErrorWire("req-1", "ch-1", "invalid json")

	if wire["protocol_version"] != E2AProtocolVersion {
		t.Errorf("protocol_version 期望 %s，实际 %v", E2AProtocolVersion, wire["protocol_version"])
	}
	if wire["response_kind"] != E2AResponseKindE2AError {
		t.Errorf("response_kind 期望 %s，实际 %v", E2AResponseKindE2AError, wire["response_kind"])
	}
	if wire["status"] != E2AResponseStatusFailed {
		t.Errorf("status 期望 %s，实际 %v", E2AResponseStatusFailed, wire["status"])
	}
	body, ok := wire["body"].(map[string]any)
	if !ok {
		t.Fatal("body 应为 map[string]any")
	}
	if body["code"] != "E2A.INVALID_JSON" {
		t.Errorf("body.code 期望 E2A.INVALID_JSON，实际 %v", body["code"])
	}
	if body["message"] != "invalid json" {
		t.Errorf("body.message 期望 invalid json，实际 %v", body["message"])
	}
}

// TestEncodeJSONParseErrorWire_带ResponseID 验证指定 responseID
func TestEncodeJSONParseErrorWire_带ResponseID(t *testing.T) {
	wire := EncodeJSONParseErrorWire("req-1", "ch-1", "bad", "custom-resp-id")
	if wire["response_id"] != "custom-resp-id" {
		t.Errorf("response_id 期望 custom-resp-id，实际 %v", wire["response_id"])
	}
}

// TestEncodeJSONParseErrorWire_默认ResponseID 验证不指定 responseID 时用 requestID
func TestEncodeJSONParseErrorWire_默认ResponseID(t *testing.T) {
	wire := EncodeJSONParseErrorWire("req-1", "ch-1", "bad")
	if wire["response_id"] != "req-1" {
		t.Errorf("response_id 期望 req-1，实际 %v", wire["response_id"])
	}
}

// ──────────────────────────── 往返测试 ────────────────────────────

// TestRoundTrip_Unary 验证 encode → parse 往返一致性（unary）
func TestRoundTrip_Unary(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "rt-req-1",
		ChannelID: "rt-ch-1",
		OK:        true,
		Payload:   map[string]any{"data": "value"},
		Metadata:  map[string]any{"trace_id": "abc"},
	}
	wire := EncodeAgentResponseForWire(resp, "rt-resp-1", 0)
	out, err := ParseAgentServerWireUnary(wire)
	if err != nil {
		t.Fatalf("往返解析失败: %v", err)
	}
	if out.RequestID != resp.RequestID {
		t.Errorf("request_id 期望 %s，实际 %s", resp.RequestID, out.RequestID)
	}
	if out.ChannelID != resp.ChannelID {
		t.Errorf("channel_id 期望 %s，实际 %s", resp.ChannelID, out.ChannelID)
	}
	if out.OK != resp.OK {
		t.Errorf("ok 期望 %v，实际 %v", resp.OK, out.OK)
	}
}

// TestRoundTrip_Unary_失败 验证 encode → parse 往返一致性（unary 失败响应）
func TestRoundTrip_Unary_失败(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "rt-req-2",
		ChannelID: "rt-ch-2",
		OK:        false,
		Payload:   map[string]any{"error": "fail"},
	}
	wire := EncodeAgentResponseForWire(resp, "rt-resp-2", 0)
	out, err := ParseAgentServerWireUnary(wire)
	if err != nil {
		t.Fatalf("往返解析失败: %v", err)
	}
	if out.OK != false {
		t.Error("ok 期望 false")
	}
}

// TestRoundTrip_Chunk 验证 encode → parse 往返一致性（chunk 中间帧）
func TestRoundTrip_Chunk(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "rt-req-3",
		ChannelID:  "rt-ch-3",
		Payload:    map[string]any{"event_type": "chat.delta", "content": "world"},
		IsComplete: false,
	}
	wire := EncodeAgentChunkForWire(chunk, "rt-resp-3", 1, true)
	out, err := ParseAgentServerWireChunk(wire)
	if err != nil {
		t.Fatalf("往返解析失败: %v", err)
	}
	if out.RequestID != chunk.RequestID {
		t.Errorf("request_id 期望 %s，实际 %s", chunk.RequestID, out.RequestID)
	}
	if out.ChannelID != chunk.ChannelID {
		t.Errorf("channel_id 期望 %s，实际 %s", chunk.ChannelID, out.ChannelID)
	}
}

// TestRoundTrip_Chunk_终止帧 验证 encode → parse 往返一致性（chunk 终止帧）
func TestRoundTrip_Chunk_终止帧(t *testing.T) {
	chunk := schema.NewTerminalChunk("rt-req-4", "rt-ch-4")
	wire := EncodeAgentChunkForWire(chunk, "rt-resp-4", 10, true)
	out, err := ParseAgentServerWireChunk(wire)
	if err != nil {
		t.Fatalf("往返解析失败: %v", err)
	}
	if out.IsComplete != true {
		t.Error("is_complete 期望 true")
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// TestRawDictToAgentResponse_基本 验证从原始 dict 构造 AgentResponse
func TestRawDictToAgentResponse_基本(t *testing.T) {
	data := map[string]any{
		"request_id": "r1",
		"channel_id": "c1",
		"ok":         true,
		"payload":    map[string]any{"key": "val"},
	}
	resp := rawDictToAgentResponse(data)
	if resp.RequestID != "r1" {
		t.Errorf("request_id 期望 r1，实际 %s", resp.RequestID)
	}
	if resp.OK != true {
		t.Error("ok 期望 true")
	}
}

// TestRawDictToAgentChunk_基本 验证从原始 dict 构造 AgentResponseChunk
func TestRawDictToAgentChunk_基本(t *testing.T) {
	data := map[string]any{
		"request_id":  "r1",
		"channel_id":  "c1",
		"payload":     map[string]any{"content": "hi"},
		"is_complete": true,
	}
	chunk := rawDictToAgentChunk(data)
	if chunk.RequestID != "r1" {
		t.Errorf("request_id 期望 r1，实际 %s", chunk.RequestID)
	}
	if chunk.IsComplete != true {
		t.Error("is_complete 期望 true")
	}
}

// TestAgentResponseToMap_基本 验证 AgentResponse → map
func TestAgentResponseToMap_基本(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "r1",
		ChannelID: "c1",
		OK:        false,
		Payload:   map[string]any{"error": "fail"},
	}
	m := agentResponseToMap(resp)
	if m["request_id"] != "r1" {
		t.Errorf("request_id 期望 r1，实际 %v", m["request_id"])
	}
	if m["ok"] != false {
		t.Error("ok 期望 false")
	}
}

// TestAgentChunkToMap_基本 验证 AgentResponseChunk → map
func TestAgentChunkToMap_基本(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "c1",
		Payload:    map[string]any{"content": "hi"},
		IsComplete: true,
	}
	m := agentChunkToMap(chunk)
	if m["request_id"] != "r1" {
		t.Errorf("request_id 期望 r1，实际 %v", m["request_id"])
	}
	if m["is_complete"] != true {
		t.Error("is_complete 期望 true")
	}
}
