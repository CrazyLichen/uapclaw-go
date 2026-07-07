package e2a

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ──────────────────────────── 工厂默认值 ────────────────────────────

// TestNewE2AEnvelope_默认值 验证工厂函数默认值
func TestNewE2AEnvelope_默认值(t *testing.T) {
	e := NewE2AEnvelope()
	if e.ProtocolVersion != E2AProtocolVersion {
		t.Errorf("ProtocolVersion 期望 %q，实际 %q", E2AProtocolVersion, e.ProtocolVersion)
	}
	if e.Provenance.SourceProtocol != E2ASourceProtocolE2A {
		t.Errorf("Provenance.SourceProtocol 期望 %q，实际 %q", E2ASourceProtocolE2A, e.Provenance.SourceProtocol)
	}
	if e.IdentityOrigin != IdentityOriginUser {
		t.Errorf("IdentityOrigin 期望 %q，实际 %q", IdentityOriginUser, e.IdentityOrigin)
	}
	if e.IsStream != false {
		t.Error("IsStream 期望 false")
	}
}

// ──────────────────────────── EnsureTimestamp ────────────────────────────

// TestE2AEnvelope_EnsureTimestamp_未设置 验证空串时填充 RFC3339
func TestE2AEnvelope_EnsureTimestamp_未设置(t *testing.T) {
	e := NewE2AEnvelope()
	if e.Timestamp != "" {
		t.Fatalf("初始 Timestamp 应为空串，实际 %q", e.Timestamp)
	}
	e.EnsureTimestamp()
	if e.Timestamp == "" {
		t.Error("EnsureTimestamp 后 Timestamp 仍为空串")
	}
	if _, err := time.Parse(time.RFC3339, e.Timestamp); err != nil {
		t.Errorf("Timestamp 不是合法 RFC3339: %v (值: %q)", err, e.Timestamp)
	}
}

// TestE2AEnvelope_EnsureTimestamp_已设置 验证非空时不覆盖
func TestE2AEnvelope_EnsureTimestamp_已设置(t *testing.T) {
	e := NewE2AEnvelope()
	e.Timestamp = "2026-01-01T00:00:00Z"
	e.EnsureTimestamp()
	if e.Timestamp != "2026-01-01T00:00:00Z" {
		t.Errorf("已设置的 Timestamp 不应被覆盖，实际 %q", e.Timestamp)
	}
}

// ──────────────────────────── ToMap ────────────────────────────

// TestE2AEnvelope_ToMap_枚举值 验证 IdentityOrigin 输出字符串值
func TestE2AEnvelope_ToMap_枚举值(t *testing.T) {
	e := NewE2AEnvelope()
	e.IdentityOrigin = IdentityOriginAgent
	m := e.ToMap()
	origin, ok := m["identity_origin"]
	if !ok {
		t.Fatal("ToMap 输出缺少 identity_origin 键")
	}
	if origin != "agent" {
		t.Errorf("identity_origin 期望 %q，实际 %q", "agent", origin)
	}
}

// TestE2AEnvelope_ToMap_嵌套Provenance 验证 Provenance 展开为 map
func TestE2AEnvelope_ToMap_嵌套Provenance(t *testing.T) {
	e := NewE2AEnvelope()
	e.Provenance = E2AProvenance{
		SourceProtocol: E2ASourceProtocolACP,
		Converter:      "test_converter",
	}
	m := e.ToMap()
	prov, ok := m["provenance"]
	if !ok {
		t.Fatal("ToMap 输出缺少 provenance 键")
	}
	provMap, ok := prov.(map[string]any)
	if !ok {
		t.Fatalf("provenance 期望 map[string]any，实际 %T", prov)
	}
	if provMap["source_protocol"] != E2ASourceProtocolACP {
		t.Errorf("provenance.source_protocol 期望 %q，实际 %v", E2ASourceProtocolACP, provMap["source_protocol"])
	}
}

// ──────────────────────────── EnvelopeFromMap 完整 ────────────────────────────

// TestEnvelopeFromMap_完整字段 验证所有字段解析
func TestEnvelopeFromMap_完整字段(t *testing.T) {
	data := map[string]any{
		"protocol_version":      "1.0",
		"request_id":            "req-001",
		"jsonrpc_id":            42,
		"correlation_id":        "corr-001",
		"task_id":               "task-001",
		"context_id":            "ctx-001",
		"session_id":            "sess-001",
		"message_id":            "msg-001",
		"timestamp":             "2026-01-01T00:00:00Z",
		"identity_origin":       "agent",
		"channel":               "cli",
		"user_id":               "user-001",
		"chat_id":               "chat-001",
		"source_agent_id":       "agent-001",
		"method":                "chat.send",
		"params":                map[string]any{"text": "hello"},
		"ext_method":            "custom",
		"session_update_kind":   "tool_call",
		"is_stream":             true,
		"expected_output_modes": []any{"text", "json"},
		"channel_context":       map[string]any{"key": "val"},
		"a2a_metadata":          map[string]any{"a2a_key": "a2a_val"},
		"acp_meta":              map[string]any{"acp_key": "acp_val"},
		"auth": map[string]any{
			"method_id":    "oauth2",
			"bearer_token": "tok123",
		},
	}
	e := EnvelopeFromMap(data)
	if e.ProtocolVersion != "1.0" {
		t.Errorf("ProtocolVersion 期望 %q，实际 %q", "1.0", e.ProtocolVersion)
	}
	if e.RequestID != "req-001" {
		t.Errorf("RequestID 期望 %q，实际 %q", "req-001", e.RequestID)
	}
	if e.JSONRPCID != 42 {
		t.Errorf("JSONRPCID 期望 42，实际 %v", e.JSONRPCID)
	}
	if e.IdentityOrigin != IdentityOriginAgent {
		t.Errorf("IdentityOrigin 期望 %q，实际 %q", IdentityOriginAgent, e.IdentityOrigin)
	}
	if e.Channel != "cli" {
		t.Errorf("Channel 期望 %q，实际 %q", "cli", e.Channel)
	}
	if e.Method != "chat.send" {
		t.Errorf("Method 期望 %q，实际 %q", "chat.send", e.Method)
	}
	if !e.IsStream {
		t.Error("IsStream 期望 true")
	}
	if e.Auth.MethodID != "oauth2" {
		t.Errorf("Auth.MethodID 期望 %q，实际 %q", "oauth2", e.Auth.MethodID)
	}
	if e.Auth.BearerToken != "tok123" {
		t.Errorf("Auth.BearerToken 期望 %q，实际 %q", "tok123", e.Auth.BearerToken)
	}
	if len(e.ExpectedOutputModes) != 2 || e.ExpectedOutputModes[0] != "text" {
		t.Errorf("ExpectedOutputModes 期望 [text, json]，实际 %v", e.ExpectedOutputModes)
	}
}

// ──────────────────────────── Legacy 兼容 ────────────────────────────

// TestEnvelopeFromMap_legacy_binding迁移 验证 binding→provenance.details.migrated_from_binding
func TestEnvelopeFromMap_legacy_binding迁移(t *testing.T) {
	data := map[string]any{
		"binding": "acp",
	}
	e := EnvelopeFromMap(data)
	if e.Provenance.SourceProtocol != E2ASourceProtocolACP {
		t.Errorf("SourceProtocol 期望 %q，实际 %q", E2ASourceProtocolACP, e.Provenance.SourceProtocol)
	}
	if e.Provenance.Details["migrated_from_binding"] != "acp" {
		t.Errorf("migrated_from_binding 期望 %q，实际 %v", "acp", e.Provenance.Details["migrated_from_binding"])
	}
}

// TestEnvelopeFromMap_legacy_payload合并 验证顶层 payload 合并到 params
func TestEnvelopeFromMap_legacy_payload合并(t *testing.T) {
	data := map[string]any{
		"params":  map[string]any{"existing": "val"},
		"payload": map[string]any{"extra": "data"},
	}
	e := EnvelopeFromMap(data)
	if e.Params["existing"] != "val" {
		t.Error("params 中原有键被覆盖")
	}
	if e.Params["extra"] != "data" {
		t.Error("payload 键未合并到 params")
	}
}

// TestEnvelopeFromMap_legacy_req_method 验证 req_method→method
func TestEnvelopeFromMap_legacy_req_method(t *testing.T) {
	data := map[string]any{
		"req_method": "chat.send",
	}
	e := EnvelopeFromMap(data)
	if e.Method != "chat.send" {
		t.Errorf("Method 期望 %q，实际 %q", "chat.send", e.Method)
	}
}

// TestEnvelopeFromMap_legacy_channel_id 验证 channel_id→channel
func TestEnvelopeFromMap_legacy_channel_id(t *testing.T) {
	data := map[string]any{
		"channel_id": "cli",
	}
	e := EnvelopeFromMap(data)
	if e.Channel != "cli" {
		t.Errorf("Channel 期望 %q，实际 %q", "cli", e.Channel)
	}
}

// TestEnvelopeFromMap_legacy_metadata合并 验证顶层 metadata→channel_context
func TestEnvelopeFromMap_legacy_metadata合并(t *testing.T) {
	data := map[string]any{
		"channel_context": map[string]any{"existing": "val"},
		"metadata":        map[string]any{"extra": "data"},
	}
	e := EnvelopeFromMap(data)
	if e.ChannelContext["existing"] != "val" {
		t.Error("channel_context 中原有键被覆盖")
	}
	if e.ChannelContext["extra"] != "data" {
		t.Error("metadata 键未合并到 channel_context")
	}
}

// ──────────────────────────── 时间戳规范化 ────────────────────────────

// TestEnvelopeFromMap_timestamp_float转RFC3339 验证 float64 纪元秒转换
func TestEnvelopeFromMap_timestamp_float转RFC3339(t *testing.T) {
	data := map[string]any{
		"timestamp": float64(1735689600), // 2025-01-01T00:00:00Z
	}
	e := EnvelopeFromMap(data)
	if e.Timestamp == "" {
		t.Error("Timestamp 为空串")
	}
	if !strings.Contains(e.Timestamp, "2025") {
		t.Errorf("Timestamp 应包含 2025，实际 %q", e.Timestamp)
	}
}

// TestNormalizeTimestampValue_string 验证字符串原样返回
func TestNormalizeTimestampValue_string(t *testing.T) {
	result := normalizeTimestampValue("2026-01-01T00:00:00Z")
	if result != "2026-01-01T00:00:00Z" {
		t.Errorf("期望原样返回，实际 %q", result)
	}
}

// TestNormalizeTimestampValue_float 验证 float64 纪元秒转换
func TestNormalizeTimestampValue_float(t *testing.T) {
	result := normalizeTimestampValue(float64(1735689600))
	if result == "" {
		t.Error("结果为空串")
	}
}

// TestNormalizeTimestampValue_int 验证 int 纪元秒转换
func TestNormalizeTimestampValue_int(t *testing.T) {
	result := normalizeTimestampValue(1735689600)
	if result == "" {
		t.Error("结果为空串")
	}
}

// TestNormalizeTimestampValue_nil 验证 nil→空串
func TestNormalizeTimestampValue_nil(t *testing.T) {
	result := normalizeTimestampValue(nil)
	if result != "" {
		t.Errorf("nil 期望空串，实际 %q", result)
	}
}

// ──────────────────────────── migrateLegacyBinding ────────────────────────────

// TestMigrateLegacyBinding_无binding字段 验证无 binding 时不修改
func TestMigrateLegacyBinding_无binding字段(t *testing.T) {
	data := map[string]any{}
	prov := *NewE2AProvenance()
	result := migrateLegacyBinding(data, prov)
	if result.SourceProtocol != E2ASourceProtocolE2A {
		t.Errorf("SourceProtocol 期望 %q，实际 %q", E2ASourceProtocolE2A, result.SourceProtocol)
	}
}

// TestMigrateLegacyBinding_binding为ACP 验证 binding=acp→SourceProtocol=acp
func TestMigrateLegacyBinding_binding为ACP(t *testing.T) {
	data := map[string]any{"binding": "acp"}
	prov := *NewE2AProvenance()
	result := migrateLegacyBinding(data, prov)
	if result.SourceProtocol != E2ASourceProtocolACP {
		t.Errorf("SourceProtocol 期望 %q，实际 %q", E2ASourceProtocolACP, result.SourceProtocol)
	}
}

// TestMigrateLegacyBinding_binding为dict含value 验证 binding={"value":"acp"}→取 value
func TestMigrateLegacyBinding_binding为dict含value(t *testing.T) {
	data := map[string]any{"binding": map[string]any{"value": "acp"}}
	prov := *NewE2AProvenance()
	result := migrateLegacyBinding(data, prov)
	if result.SourceProtocol != E2ASourceProtocolACP {
		t.Errorf("SourceProtocol 期望 %q，实际 %q", E2ASourceProtocolACP, result.SourceProtocol)
	}
}

// ──────────────────────────── paramsWithOptionalLegacyPayload ────────────────────────────

// TestParamsWithOptionalLegacyPayload_有payload 验证 payload 键合并，不覆盖
func TestParamsWithOptionalLegacyPayload_有payload(t *testing.T) {
	data := map[string]any{
		"params":  map[string]any{"existing": "val"},
		"payload": map[string]any{"extra": "data", "existing": "should_not_override"},
	}
	result := paramsWithOptionalLegacyPayload(data)
	if result["existing"] != "val" {
		t.Error("已有键被覆盖")
	}
	if result["extra"] != "data" {
		t.Error("payload 键未合并")
	}
}

// TestParamsWithOptionalLegacyPayload_无payload 验证无 payload 时原样
func TestParamsWithOptionalLegacyPayload_无payload(t *testing.T) {
	data := map[string]any{
		"params": map[string]any{"key": "val"},
	}
	result := paramsWithOptionalLegacyPayload(data)
	if result["key"] != "val" {
		t.Error("params 内容被修改")
	}
}

// ──────────────────────────── MergeParamsToACPPrompt ────────────────────────────

// TestMergeParamsToACPPrompt_非prompt方法 验证 method≠session/prompt 时原样返回
func TestMergeParamsToACPPrompt_非prompt方法(t *testing.T) {
	env := NewE2AEnvelope()
	env.Method = "chat.send"
	env.Params = map[string]any{"text": "hello"}
	result := MergeParamsToACPPrompt(env)
	if result["text"] != "hello" {
		t.Error("非 prompt 方法应原样返回")
	}
}

// TestMergeParamsToACPPrompt_已有prompt 验证已有 prompt 时不修改
func TestMergeParamsToACPPrompt_已有prompt(t *testing.T) {
	env := NewE2AEnvelope()
	env.Method = "session/prompt"
	env.Params = map[string]any{"prompt": "existing"}
	result := MergeParamsToACPPrompt(env)
	if result["prompt"] != "existing" {
		t.Error("已有 prompt 不应被覆盖")
	}
}

// TestMergeParamsToACPPrompt_content_blocks转prompt 验证 content_blocks→prompt
func TestMergeParamsToACPPrompt_content_blocks转prompt(t *testing.T) {
	env := NewE2AEnvelope()
	env.Method = "session/prompt"
	env.Params = map[string]any{
		"content_blocks": []any{
			map[string]any{"type": "text", "text": "hello"},
		},
	}
	result := MergeParamsToACPPrompt(env)
	prompt, ok := result["prompt"]
	if !ok {
		t.Fatal("缺少 prompt 键")
	}
	if _, ok := prompt.([]map[string]any); !ok {
		t.Errorf("prompt 期望 []map[string]any，实际 %T", prompt)
	}
}

// TestMergeParamsToACPPrompt_text转prompt 验证 text→单条 ContentBlock
func TestMergeParamsToACPPrompt_text转prompt(t *testing.T) {
	env := NewE2AEnvelope()
	env.Method = "session/prompt"
	env.Params = map[string]any{"text": "hello"}
	result := MergeParamsToACPPrompt(env)
	prompt, ok := result["prompt"]
	if !ok {
		t.Fatal("缺少 prompt 键")
	}
	blocks, ok := prompt.([]map[string]any)
	if !ok {
		t.Fatalf("prompt 期望 []map[string]any，实际 %T", prompt)
	}
	if len(blocks) != 1 || blocks[0]["text"] != "hello" {
		t.Errorf("blocks 期望 [{type:text text:hello}]，实际 %v", blocks)
	}
}

// TestMergeParamsToACPPrompt_补session_id 验证 envelope.session_id→params.session_id
func TestMergeParamsToACPPrompt_补session_id(t *testing.T) {
	env := NewE2AEnvelope()
	env.Method = "session/prompt"
	env.SessionID = "sess-001"
	env.Params = map[string]any{"text": "hello"}
	result := MergeParamsToACPPrompt(env)
	if result["session_id"] != "sess-001" {
		t.Errorf("session_id 期望 %q，实际 %v", "sess-001", result["session_id"])
	}
}

// TestMergeParamsToACPPrompt_补acp_meta 验证 envelope.acp_meta→params._meta
func TestMergeParamsToACPPrompt_补acp_meta(t *testing.T) {
	env := NewE2AEnvelope()
	env.Method = "session/prompt"
	env.ACPMeta = map[string]any{"tool_use_id": "tu-001"}
	env.Params = map[string]any{"text": "hello"}
	result := MergeParamsToACPPrompt(env)
	meta, ok := result["_meta"].(map[string]any)
	if !ok {
		t.Fatal("_meta 不是 map[string]any")
	}
	if meta["tool_use_id"] != "tu-001" {
		t.Errorf("_meta.tool_use_id 期望 %q，实际 %v", "tu-001", meta["tool_use_id"])
	}
}

// ──────────────────────────── 序列化往返 ────────────────────────────

// TestE2AEnvelope_JSON序列化往返 验证 JSON marshal/unmarshal 往返一致性
func TestE2AEnvelope_JSON序列化往返(t *testing.T) {
	original := NewE2AEnvelope()
	original.RequestID = "req-001"
	original.Method = "chat.send"
	original.IsStream = true
	original.Params = map[string]any{"text": "hello"}
	original.SessionID = "sess-001"
	original.Provenance = E2AProvenance{
		SourceProtocol: E2ASourceProtocolACP,
		Converter:      "acp_adapter",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var decoded E2AEnvelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID 期望 %q，实际 %q", original.RequestID, decoded.RequestID)
	}
	if decoded.Method != original.Method {
		t.Errorf("Method 期望 %q，实际 %q", original.Method, decoded.Method)
	}
	if decoded.IsStream != original.IsStream {
		t.Errorf("IsStream 期望 %v，实际 %v", original.IsStream, decoded.IsStream)
	}
	if decoded.Provenance.SourceProtocol != original.Provenance.SourceProtocol {
		t.Errorf("Provenance.SourceProtocol 期望 %q，实际 %q", original.Provenance.SourceProtocol, decoded.Provenance.SourceProtocol)
	}
}

// TestEnvelopeFromMap_ToMap_往返 验证 EnvelopeFromMap(ToMap(env)) ≈ env
func TestEnvelopeFromMap_ToMap_往返(t *testing.T) {
	original := NewE2AEnvelope()
	original.RequestID = "req-001"
	original.Method = "chat.send"
	original.IsStream = true
	original.SessionID = "sess-001"
	original.Channel = "cli"
	original.UserID = "user-001"
	original.Params = map[string]any{"text": "hello"}

	m := original.ToMap()
	roundtrip := EnvelopeFromMap(m)

	if roundtrip.RequestID != original.RequestID {
		t.Errorf("RequestID 往返不一致: 期望 %q，实际 %q", original.RequestID, roundtrip.RequestID)
	}
	if roundtrip.Method != original.Method {
		t.Errorf("Method 往返不一致: 期望 %q，实际 %q", original.Method, roundtrip.Method)
	}
	if roundtrip.IsStream != original.IsStream {
		t.Errorf("IsStream 往返不一致: 期望 %v，实际 %v", original.IsStream, roundtrip.IsStream)
	}
	if roundtrip.Channel != original.Channel {
		t.Errorf("Channel 往返不一致: 期望 %q，实际 %q", original.Channel, roundtrip.Channel)
	}
}

// ──────────────────────────── 辅助函数覆盖补全 ────────────────────────────

// TestGetInt_各种类型 验证 getInt 辅助函数
func TestGetInt_各种类型(t *testing.T) {
	m := map[string]any{
		"float_val": float64(42),
		"int_val":   99,
		"int64_val": int64(7),
		"str_val":   "123",
		"bad_val":   "not_a_number",
		"nil_val":   nil,
	}
	if v := getInt(m, "float_val", 0); v != 42 {
		t.Errorf("float64 期望 42，实际 %d", v)
	}
	if v := getInt(m, "int_val", 0); v != 99 {
		t.Errorf("int 期望 99，实际 %d", v)
	}
	if v := getInt(m, "int64_val", 0); v != 7 {
		t.Errorf("int64 期望 7，实际 %d", v)
	}
	if v := getInt(m, "str_val", 0); v != 123 {
		t.Errorf("字符串数字 期望 123，实际 %d", v)
	}
	if v := getInt(m, "bad_val", -1); v != -1 {
		t.Errorf("非法字符串 期望默认值 -1，实际 %d", v)
	}
	if v := getInt(m, "nil_val", -1); v != -1 {
		t.Errorf("nil 期望默认值 -1，实际 %d", v)
	}
	if v := getInt(m, "missing", -1); v != -1 {
		t.Errorf("缺失键 期望默认值 -1，实际 %d", v)
	}
}

// TestGetMapString_字符串map 验证 getMapString 辅助函数
func TestGetMapString_字符串map(t *testing.T) {
	m := map[string]any{
		"headers": map[string]any{"X-Custom": "val", "X-Num": 42},
	}
	result := getMapString(m, "headers")
	if result["X-Custom"] != "val" {
		t.Errorf("X-Custom 期望 %q，实际 %q", "val", result["X-Custom"])
	}
	if _, ok := result["X-Num"]; ok {
		t.Error("非字符串值不应出现在结果中")
	}
}

// TestIsEmptySliceOrMap_各种类型 验证 isEmptySliceOrMap 辅助函数
func TestIsEmptySliceOrMap_各种类型(t *testing.T) {
	if !isEmptySliceOrMap([]any{}) {
		t.Error("空切片期望 true")
	}
	if !isEmptySliceOrMap(map[string]any{}) {
		t.Error("空 map 期望 true")
	}
	if isEmptySliceOrMap([]any{1}) {
		t.Error("非空切片期望 false")
	}
	if isEmptySliceOrMap(map[string]any{"k": 1}) {
		t.Error("非空 map 期望 false")
	}
	if isEmptySliceOrMap("string") {
		t.Error("字符串期望 false")
	}
	if isEmptySliceOrMap(42) {
		t.Error("整数期望 false")
	}
}

// TestGetStringSlice_各种类型 验证 getStringSlice 辅助函数
func TestGetStringSlice_各种类型(t *testing.T) {
	m := map[string]any{
		"modes":   []any{"text", "json"},
		"empty":   []any{},
		"nil_val": nil,
	}
	result := getStringSlice(m, "modes")
	if len(result) != 2 || result[0] != "text" {
		t.Errorf("期望 [text, json]，实际 %v", result)
	}
	if len(getStringSlice(m, "empty")) != 0 {
		t.Error("空切片期望长度 0")
	}
	if len(getStringSlice(m, "nil_val")) != 0 {
		t.Error("nil 期望长度 0")
	}
	if len(getStringSlice(m, "missing")) != 0 {
		t.Error("缺失键期望长度 0")
	}
}

// TestNormalizeTimestampValue_int64 验证 int64 纪元秒转换
func TestNormalizeTimestampValue_int64(t *testing.T) {
	result := normalizeTimestampValue(int64(1735689600))
	if result == "" {
		t.Error("结果为空串")
	}
}

// TestNormalizeTimestampValue_其他类型 验证其他类型走 fmt.Sprintf
func TestNormalizeTimestampValue_其他类型(t *testing.T) {
	result := normalizeTimestampValue(true)
	if result != "true" {
		t.Errorf("bool 期望 %q，实际 %q", "true", result)
	}
}

// TestProvenanceFromMap_直接类型 验证 provenanceFromMap 传入 E2AProvenance 类型
func TestProvenanceFromMap_直接类型(t *testing.T) {
	original := E2AProvenance{
		SourceProtocol: E2ASourceProtocolA2A,
		Converter:      "a2a_adapter",
	}
	result := provenanceFromMap(original)
	if result.SourceProtocol != E2ASourceProtocolA2A {
		t.Errorf("SourceProtocol 期望 %q，实际 %q", E2ASourceProtocolA2A, result.SourceProtocol)
	}
}

// TestProvenanceFromMap_非map类型 验证 provenanceFromMap 传入非法类型
func TestProvenanceFromMap_非map类型(t *testing.T) {
	result := provenanceFromMap(42)
	if result.SourceProtocol != E2ASourceProtocolE2A {
		t.Errorf("非 map 类型期望默认值，实际 %q", result.SourceProtocol)
	}
}

// TestMigrateLegacyBinding_binding为A2A 验证 binding=a2a→SourceProtocol=a2a
func TestMigrateLegacyBinding_binding为A2A(t *testing.T) {
	data := map[string]any{"binding": "a2a"}
	prov := *NewE2AProvenance()
	result := migrateLegacyBinding(data, prov)
	if result.SourceProtocol != E2ASourceProtocolA2A {
		t.Errorf("SourceProtocol 期望 %q，实际 %q", E2ASourceProtocolA2A, result.SourceProtocol)
	}
}

// TestMigrateLegacyBinding_binding为Internal 验证 binding=internal→SourceProtocol=e2a
func TestMigrateLegacyBinding_binding为Internal(t *testing.T) {
	data := map[string]any{"binding": "internal"}
	prov := *NewE2AProvenance()
	result := migrateLegacyBinding(data, prov)
	if result.SourceProtocol != E2ASourceProtocolE2A {
		t.Errorf("SourceProtocol 期望 %q，实际 %q", E2ASourceProtocolE2A, result.SourceProtocol)
	}
}

// TestMigrateLegacyBinding_已迁移 验证已迁移时不重复迁移
func TestMigrateLegacyBinding_已迁移(t *testing.T) {
	data := map[string]any{"binding": "acp"}
	prov := E2AProvenance{
		SourceProtocol: E2ASourceProtocolE2A,
		Details:        map[string]any{"migrated_from_binding": "old_val"},
	}
	result := migrateLegacyBinding(data, prov)
	if result.SourceProtocol != E2ASourceProtocolE2A {
		t.Errorf("已迁移时期望不修改 SourceProtocol，实际 %q", result.SourceProtocol)
	}
}

// TestParamsWithOptionalLegacyPayload_payload含nil值 验证 payload 中 nil 值被跳过
func TestParamsWithOptionalLegacyPayload_payload含nil值(t *testing.T) {
	data := map[string]any{
		"params":  map[string]any{},
		"payload": map[string]any{"nil_key": nil, "valid": "val"},
	}
	result := paramsWithOptionalLegacyPayload(data)
	if _, ok := result["nil_key"]; ok {
		t.Error("nil 值不应被合并")
	}
	if result["valid"] != "val" {
		t.Error("有效值应被合并")
	}
}

// TestMergeParamsToACPPrompt_content转prompt 验证 content→单条 ContentBlock
func TestMergeParamsToACPPrompt_content转prompt(t *testing.T) {
	env := NewE2AEnvelope()
	env.Method = "session/prompt"
	env.Params = map[string]any{"content": "world"}
	result := MergeParamsToACPPrompt(env)
	blocks, ok := result["prompt"].([]map[string]any)
	if !ok || len(blocks) != 1 || blocks[0]["text"] != "world" {
		t.Errorf("content→prompt 期望 [{type:text text:world}]，实际 %v", result["prompt"])
	}
}

// TestMergeParamsToACPPrompt_query转prompt 验证 query→单条 ContentBlock
func TestMergeParamsToACPPrompt_query转prompt(t *testing.T) {
	env := NewE2AEnvelope()
	env.Method = "session/prompt"
	env.Params = map[string]any{"query": "search_term"}
	result := MergeParamsToACPPrompt(env)
	blocks, ok := result["prompt"].([]map[string]any)
	if !ok || len(blocks) != 1 || blocks[0]["text"] != "search_term" {
		t.Errorf("query→prompt 期望 [{type:text text:search_term}]，实际 %v", result["prompt"])
	}
}

// TestMergeParamsToACPPrompt_无文本字段 验证无文本字段时不添加 prompt
func TestMergeParamsToACPPrompt_无文本字段(t *testing.T) {
	env := NewE2AEnvelope()
	env.Method = "session/prompt"
	env.Params = map[string]any{}
	result := MergeParamsToACPPrompt(env)
	if _, ok := result["prompt"]; ok {
		t.Error("无文本字段时不应添加 prompt")
	}
}
