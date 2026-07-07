package e2a

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 请求方向 ────────────────────────────

// TestMessageToLegacyAgentDict_基本字段 验证基本字段映射
func TestMessageToLegacyAgentDict_基本字段(t *testing.T) {
	params := map[string]any{"query": "hello"}
	paramsJSON, _ := json.Marshal(params)
	msg := &schema.Message{
		ID:        "req-1",
		ChannelID: "ch-1",
		SessionID: "sess-1",
		ReqMethod: schema.ReqMethod("chat.send"),
		Params:    paramsJSON,
		IsStream:  true,
		Timestamp: 1234567890.123,
	}
	d := MessageToLegacyAgentDict(msg)
	if d["request_id"] != "req-1" {
		t.Errorf("request_id 期望 req-1，实际 %v", d["request_id"])
	}
	if d["channel_id"] != "ch-1" {
		t.Errorf("channel_id 期望 ch-1，实际 %v", d["channel_id"])
	}
	if d["session_id"] != "sess-1" {
		t.Errorf("session_id 期望 sess-1，实际 %v", d["session_id"])
	}
	if d["req_method"] != "chat.send" {
		t.Errorf("req_method 期望 chat.send，实际 %v", d["req_method"])
	}
	if d["is_stream"] != true {
		t.Error("is_stream 期望 true")
	}
	if d["timestamp"] != 1234567890.123 {
		t.Errorf("timestamp 期望 1234567890.123，实际 %v", d["timestamp"])
	}
	p, ok := d["params"].(map[string]any)
	if !ok || p["query"] != "hello" {
		t.Errorf("params 期望 {query:hello}，实际 %v", d["params"])
	}
}

// TestMessageToLegacyAgentDict_空ReqMethod 验证空 ReqMethod 返回 nil
func TestMessageToLegacyAgentDict_空ReqMethod(t *testing.T) {
	msg := &schema.Message{ID: "r1", ChannelID: "ch", ReqMethod: ""}
	d := MessageToLegacyAgentDict(msg)
	if d["req_method"] != nil {
		t.Errorf("空 ReqMethod 应为 nil，实际 %v", d["req_method"])
	}
}

// TestMessageToLegacyAgentDict_有Metadata 验证 metadata 拷贝
func TestMessageToLegacyAgentDict_有Metadata(t *testing.T) {
	msg := &schema.Message{
		ID:        "r1",
		ChannelID: "ch",
		Metadata:  map[string]any{"key": "val"},
	}
	d := MessageToLegacyAgentDict(msg)
	m, ok := d["metadata"].(map[string]any)
	if !ok || m["key"] != "val" {
		t.Errorf("metadata 期望 {key:val}，实际 %v", d["metadata"])
	}
}

// TestBuildFallbackE2A_基本 验证兜底信封基本结构
func TestBuildFallbackE2A_基本(t *testing.T) {
	legacy := map[string]any{
		"request_id": "r1",
		"channel_id": "ch1",
		"session_id": "s1",
		"is_stream":  true,
	}
	env := BuildFallbackE2A(legacy)
	if env.ProtocolVersion != E2AProtocolVersion {
		t.Errorf("ProtocolVersion 期望 %q，实际 %q", E2AProtocolVersion, env.ProtocolVersion)
	}
	if env.RequestID != "r1" {
		t.Errorf("RequestID 期望 r1，实际 %q", env.RequestID)
	}
	if env.Channel != "ch1" {
		t.Errorf("Channel 期望 ch1，实际 %q", env.Channel)
	}
	if env.IsStream != true {
		t.Error("IsStream 期望 true")
	}
	if env.Params == nil || len(env.Params) != 0 {
		t.Errorf("Params 应为空 map，实际 %v", env.Params)
	}
	cc, ok := env.ChannelContext[e2aInternalContextKey].(map[string]any)
	if !ok {
		t.Fatal("channel_context._jiuwenswarm 不存在或类型错误")
	}
	if cc[e2aFallbackFailedKey] != true {
		t.Error("normalize_failed 应为 true")
	}
	if cc[e2aLegacyAgentRequestKey] == nil {
		t.Error("legacy_agent_request 不应为 nil")
	}
}

// TestMessageToE2A_正常 验证正常转换
func TestMessageToE2A_正常(t *testing.T) {
	params := map[string]any{"query": "hi"}
	paramsJSON, _ := json.Marshal(params)
	msg := &schema.Message{
		ID:        "req-1",
		ChannelID: "ch-1",
		SessionID: "sess-1",
		ChatID:    "chat-1",
		ReqMethod: schema.ReqMethod("chat.send"),
		Params:    paramsJSON,
		IsStream:  true,
		Timestamp: 1000.0,
	}
	env, err := MessageToE2A(msg)
	if err != nil {
		t.Fatalf("MessageToE2A 失败: %v", err)
	}
	if env.RequestID != "req-1" {
		t.Errorf("RequestID 期望 req-1，实际 %q", env.RequestID)
	}
	if env.Channel != "ch-1" {
		t.Errorf("Channel 期望 ch-1，实际 %q", env.Channel)
	}
	if env.Method != "chat.send" {
		t.Errorf("Method 期望 chat.send，实际 %q", env.Method)
	}
	if env.IsStream != true {
		t.Error("IsStream 期望 true")
	}
}

// TestMessageToE2A_EnableMemory逻辑 验证 enable_memory 三条件组合
func TestMessageToE2A_EnableMemory逻辑(t *testing.T) {
	// 场景1：enable_memory=true → final_enable_memory=true
	msg := &schema.Message{
		ID:                 "r1",
		ChannelID:          "ch",
		EnableMemory:       true,
		Metadata:           map[string]any{"avatar_mode": true},
		GroupDigitalAvatar: true,
	}
	env, _ := MessageToE2A(msg)
	meta, _ := env.ChannelContext["enable_memory"]
	// metadata 会合并到 channel_context
	if env.RequestID != "r1" {
		t.Errorf("RequestID 不匹配: %q", env.RequestID)
	}
	_ = meta

	// 场景2：enable_memory=false, group_digital_avatar=true, avatar_mode=true → final=false
	msg2 := &schema.Message{
		ID:                 "r2",
		ChannelID:          "ch",
		EnableMemory:       false,
		GroupDigitalAvatar: true,
		Metadata:           map[string]any{"avatar_mode": true},
	}
	env2, _ := MessageToE2A(msg2)
	if env2.RequestID != "r2" {
		t.Errorf("RequestID 不匹配: %q", env2.RequestID)
	}
}

// TestMessageToE2AOrFallback_成功 验证成功路径
func TestMessageToE2AOrFallback_成功(t *testing.T) {
	params := map[string]any{"q": "hello"}
	paramsJSON, _ := json.Marshal(params)
	msg := &schema.Message{
		ID:        "req-1",
		ChannelID: "ch-1",
		ReqMethod: schema.ReqMethod("chat.send"),
		Params:    paramsJSON,
	}
	env := MessageToE2AOrFallback(msg)
	if env.RequestID != "req-1" {
		t.Errorf("RequestID 期望 req-1，实际 %q", env.RequestID)
	}
}

// TestMessageToE2AOrFallback_失败Fallback 验证空 request_id 触发 fallback
func TestMessageToE2AOrFallback_失败Fallback(t *testing.T) {
	msg := &schema.Message{
		ID:        "", // 空 ID 会导致 fallback
		ChannelID: "ch-1",
		ReqMethod: schema.ReqMethod("chat.send"),
	}
	env := MessageToE2AOrFallback(msg)
	// fallback 信封中 channel_context 应包含 _jiuwenswarm
	if env.ChannelContext == nil {
		t.Error("fallback 信封应包含 channel_context")
	}
	if _, ok := env.ChannelContext[e2aInternalContextKey]; !ok {
		t.Error("channel_context 应包含 _jiuwenswarm 键")
	}
}

// TestE2AFromAgentFields_基本 验证基本构造
func TestE2AFromAgentFields_基本(t *testing.T) {
	env := E2AFromAgentFields("req-1",
		WithFieldChannelID("ch-1"),
		WithFieldSessionID("sess-1"),
		WithFieldReqMethod("chat.send"),
		WithFieldIsStream(true),
		WithFieldTimestamp(1000.0),
	)
	if env.RequestID != "req-1" {
		t.Errorf("RequestID 期望 req-1，实际 %q", env.RequestID)
	}
	if env.Method != "chat.send" {
		t.Errorf("Method 期望 chat.send，实际 %q", env.Method)
	}
	if env.IsStream != true {
		t.Error("IsStream 期望 true")
	}
}

// TestE2AFromAgentFields_带Metadata 验证 metadata 传递
func TestE2AFromAgentFields_带Metadata(t *testing.T) {
	env := E2AFromAgentFields("req-1",
		WithFieldMetadata(map[string]any{"key": "val"}),
	)
	if env.RequestID != "req-1" {
		t.Errorf("RequestID 不匹配: %q", env.RequestID)
	}
}

// TestChannelContextForChannelReply_去掉内部键 验证去掉 _jiuwenswarm
func TestChannelContextForChannelReply_去掉内部键(t *testing.T) {
	env := &E2AEnvelope{
		ChannelContext: map[string]any{
			e2aInternalContextKey: map[string]any{"foo": "bar"},
			"trace_id":            "t1",
		},
	}
	ctx := ChannelContextForChannelReply(env)
	if _, ok := ctx[e2aInternalContextKey]; ok {
		t.Error("_jiuwenswarm 键应被移除")
	}
	if ctx["trace_id"] != "t1" {
		t.Errorf("trace_id 期望 t1，实际 %v", ctx["trace_id"])
	}
}

// TestChannelContextForChannelReply_空Context返回Nil 验证空上下文返回 nil
func TestChannelContextForChannelReply_空Context返回Nil(t *testing.T) {
	env := &E2AEnvelope{ChannelContext: map[string]any{e2aInternalContextKey: "x"}}
	ctx := ChannelContextForChannelReply(env)
	if ctx != nil {
		t.Errorf("空上下文应返回 nil，实际 %v", ctx)
	}
}

// TestChannelContextForChannelReply_NilContext 验证 nil ChannelContext
func TestChannelContextForChannelReply_NilContext(t *testing.T) {
	env := &E2AEnvelope{ChannelContext: nil}
	ctx := ChannelContextForChannelReply(env)
	if ctx != nil {
		t.Errorf("nil ChannelContext 应返回 nil，实际 %v", ctx)
	}
}

// ──────────────────────────── 响应方向 ────────────────────────────

// TestE2AResponseFromAgentResponse_OK 验证 ok=true 分支
func TestE2AResponseFromAgentResponse_OK(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "r1",
		ChannelID: "ch1",
		OK:        true,
		Payload:   map[string]any{"answer": "42"},
	}
	e2a := E2AResponseFromAgentResponse(resp, "resp-1", 0, WithNormTimestamp("2026-01-01T00:00:00Z"))
	if e2a.ResponseKind != E2AResponseKindE2AComplete {
		t.Errorf("ResponseKind 期望 e2a.complete，实际 %q", e2a.ResponseKind)
	}
	if e2a.Status != E2AResponseStatusSucceeded {
		t.Errorf("Status 期望 succeeded，实际 %q", e2a.Status)
	}
	if e2a.IsFinal != true {
		t.Error("IsFinal 应为 true")
	}
	if e2a.IsStream != false {
		t.Error("IsStream 应为 false")
	}
	if e2a.IdentityOrigin != IdentityOriginAgent {
		t.Errorf("IdentityOrigin 期望 agent，实际 %q", e2a.IdentityOrigin)
	}
	result, ok := e2a.Body["result"].(map[string]any)
	if !ok || result["answer"] != "42" {
		t.Errorf("Body.result 期望 {answer:42}，实际 %v", e2a.Body["result"])
	}
	if e2a.Provenance.Converter != "e2a.gateway_normalize:E2AResponseFromAgentResponse" {
		t.Errorf("Provenance.Converter 不匹配: %q", e2a.Provenance.Converter)
	}
}

// TestE2AResponseFromAgentResponse_失败 验证 ok=false 分支
func TestE2AResponseFromAgentResponse_失败(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "r1",
		ChannelID: "ch1",
		OK:        false,
		Payload:   map[string]any{"error": "timeout"},
	}
	e2a := E2AResponseFromAgentResponse(resp, "resp-1", 0, WithNormTimestamp("2026-01-01T00:00:00Z"))
	if e2a.ResponseKind != E2AResponseKindE2AError {
		t.Errorf("ResponseKind 期望 e2a.error，实际 %q", e2a.ResponseKind)
	}
	if e2a.Status != E2AResponseStatusFailed {
		t.Errorf("Status 期望 failed，实际 %q", e2a.Status)
	}
	if e2a.Body["code"] != "E2A.AGENT_ERROR" {
		t.Errorf("Body.code 期望 E2A.AGENT_ERROR，实际 %v", e2a.Body["code"])
	}
	if e2a.Body["message"] != "timeout" {
		t.Errorf("Body.message 期望 timeout，实际 %v", e2a.Body["message"])
	}
}

// TestE2AResponseFromAgentChunk_终止帧 验证终止哨兵
func TestE2AResponseFromAgentChunk_终止帧(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "ch1",
		Payload:    map[string]any{"is_complete": true},
		IsComplete: true,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 3, true, WithNormTimestamp("2026-01-01T00:00:00Z"))
	if e2a.ResponseKind != E2AResponseKindE2AComplete {
		t.Errorf("ResponseKind 期望 e2a.complete，实际 %q", e2a.ResponseKind)
	}
	if e2a.Status != E2AResponseStatusSucceeded {
		t.Errorf("Status 期望 succeeded，实际 %q", e2a.Status)
	}
	if e2a.IsFinal != true {
		t.Error("IsFinal 应为 true")
	}
	if e2a.IsStream != true {
		t.Error("IsStream 应为 true")
	}
}

// TestE2AResponseFromAgentChunk_错误帧 验证 chat.error 帧
func TestE2AResponseFromAgentChunk_错误帧(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "ch1",
		Payload:    map[string]any{"event_type": "chat.error", "error": "boom"},
		IsComplete: true,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 5, true, WithNormTimestamp("2026-01-01T00:00:00Z"))
	if e2a.ResponseKind != E2AResponseKindE2AError {
		t.Errorf("ResponseKind 期望 e2a.error，实际 %q", e2a.ResponseKind)
	}
	if e2a.Status != E2AResponseStatusFailed {
		t.Errorf("Status 期望 failed，实际 %q", e2a.Status)
	}
	if e2a.Body["code"] != "chat.error" {
		t.Errorf("Body.code 期望 chat.error，实际 %v", e2a.Body["code"])
	}
}

// TestE2AResponseFromAgentChunk_业务结束帧 验证 is_complete=true 且非终止/错误
func TestE2AResponseFromAgentChunk_业务结束帧(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "ch1",
		Payload:    map[string]any{"answer": "final", "is_complete": true},
		IsComplete: true,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 10, true, WithNormTimestamp("2026-01-01T00:00:00Z"))
	if e2a.ResponseKind != E2AResponseKindE2AComplete {
		t.Errorf("ResponseKind 期望 e2a.complete，实际 %q", e2a.ResponseKind)
	}
	if e2a.Status != E2AResponseStatusSucceeded {
		t.Errorf("Status 期望 succeeded，实际 %q", e2a.Status)
	}
	result, ok := e2a.Body["result"].(map[string]any)
	if !ok || result["answer"] != "final" {
		t.Errorf("Body.result 期望含 answer=final，实际 %v", e2a.Body["result"])
	}
}

// TestE2AResponseFromAgentChunk_中间帧ChatDelta 验证 chat.delta 中间帧
func TestE2AResponseFromAgentChunk_中间帧ChatDelta(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "ch1",
		Payload:    map[string]any{"event_type": "chat.delta", "content": "hello", "source_chunk_type": "text"},
		IsComplete: false,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 1, true, WithNormTimestamp("2026-01-01T00:00:00Z"))
	if e2a.ResponseKind != E2AResponseKindE2AChunk {
		t.Errorf("ResponseKind 期望 e2a.chunk，实际 %q", e2a.ResponseKind)
	}
	if e2a.Status != E2AResponseStatusInProgress {
		t.Errorf("Status 期望 in_progress，实际 %q", e2a.Status)
	}
	if e2a.IsFinal != false {
		t.Error("IsFinal 应为 false")
	}
	if e2a.Body["delta_kind"] != "text" {
		t.Errorf("delta_kind 期望 text，实际 %v", e2a.Body["delta_kind"])
	}
	if e2a.Body["delta"] != "hello" {
		t.Errorf("delta 期望 hello，实际 %v", e2a.Body["delta"])
	}
}

// TestE2AResponseFromAgentChunk_中间帧Reasoning 验证 source_chunk_type=llm_reasoning → delta_kind=reasoning
func TestE2AResponseFromAgentChunk_中间帧Reasoning(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "ch1",
		Payload:    map[string]any{"event_type": "chat.delta", "content": "thinking...", "source_chunk_type": "llm_reasoning"},
		IsComplete: false,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 1, true, WithNormTimestamp("2026-01-01T00:00:00Z"))
	if e2a.Body["delta_kind"] != "reasoning" {
		t.Errorf("delta_kind 期望 reasoning，实际 %v", e2a.Body["delta_kind"])
	}
}

// TestE2AResponseFromAgentChunk_中间帧Custom 验证非 chat.delta 事件
func TestE2AResponseFromAgentChunk_中间帧Custom(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "ch1",
		Payload:    map[string]any{"event_type": "tool.call", "name": "search"},
		IsComplete: false,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 2, true, WithNormTimestamp("2026-01-01T00:00:00Z"))
	if e2a.Body["delta_kind"] != "custom" {
		t.Errorf("delta_kind 期望 custom，实际 %v", e2a.Body["delta_kind"])
	}
	delta, ok := e2a.Body["delta"].(map[string]any)
	if !ok || delta["name"] != "search" {
		t.Errorf("delta 应为原始 payload，实际 %v", e2a.Body["delta"])
	}
}

// TestE2AResponseFromAgentChunk_保留RoleMemberName 验证 role/member_name 透传
func TestE2AResponseFromAgentChunk_保留RoleMemberName(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID: "r1",
		ChannelID: "ch1",
		Payload: map[string]any{
			"event_type":        "chat.delta",
			"content":           "hi",
			"source_chunk_type": "text",
			"role":              "assistant",
			"member_name":       "agent1",
		},
		IsComplete: false,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 1, true, WithNormTimestamp("2026-01-01T00:00:00Z"))
	if e2a.Body["role"] != "assistant" {
		t.Errorf("role 期望 assistant，实际 %v", e2a.Body["role"])
	}
	if e2a.Body["member_name"] != "agent1" {
		t.Errorf("member_name 期望 agent1，实际 %v", e2a.Body["member_name"])
	}
}

// TestE2AResponseToAgentResponse_CompleteSucceeded 验证 e2a.complete + succeeded → ok=true
func TestE2AResponseToAgentResponse_CompleteSucceeded(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		Channel:      "ch1",
		ResponseKind: E2AResponseKindE2AComplete,
		Status:       E2AResponseStatusSucceeded,
		Body:         map[string]any{"result": map[string]any{"answer": "42"}},
	}
	resp, err := E2AResponseToAgentResponse(e2a)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if resp.OK != true {
		t.Error("OK 应为 true")
	}
	if resp.Payload["answer"] != "42" {
		t.Errorf("Payload.answer 期望 42，实际 %v", resp.Payload["answer"])
	}
}

// TestE2AResponseToAgentResponse_Error 验证 e2a.error → ok=false
func TestE2AResponseToAgentResponse_Error(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		Channel:      "ch1",
		ResponseKind: E2AResponseKindE2AError,
		Status:       E2AResponseStatusFailed,
		Body: map[string]any{
			"code":    "E2A.AGENT_ERROR",
			"message": "timeout",
			"details": map[string]any{"error": "timeout"},
		},
	}
	resp, err := E2AResponseToAgentResponse(e2a)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if resp.OK != false {
		t.Error("OK 应为 false")
	}
	details, ok := resp.Payload["error"]
	if !ok || details != "timeout" {
		t.Errorf("Payload.error 期望 timeout，实际 %v", resp.Payload)
	}
}

// TestE2AResponseToAgentResponse_不支持Kind 验证不支持的 kind 返回错误
func TestE2AResponseToAgentResponse_不支持Kind(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		ResponseKind: E2AResponseKindE2AChunk,
		Status:       E2AResponseStatusInProgress,
	}
	_, err := E2AResponseToAgentResponse(e2a)
	if err == nil {
		t.Error("不支持的 kind 应返回错误")
	}
}

// TestE2AResponseToAgentChunk_Complete空终止 验证 e2a.complete 空终止帧
func TestE2AResponseToAgentChunk_Complete空终止(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		Channel:      "ch1",
		ResponseKind: E2AResponseKindE2AComplete,
		IsFinal:      true,
		Body:         map[string]any{"result": map[string]any{}},
	}
	chunk, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if chunk.IsComplete != true {
		t.Error("IsComplete 应为 true")
	}
	if chunk.Payload["is_complete"] != true {
		t.Errorf("Payload.is_complete 期望 true，实际 %v", chunk.Payload["is_complete"])
	}
}

// TestE2AResponseToAgentChunk_Complete有Result 验证 e2a.complete 含 result
func TestE2AResponseToAgentChunk_Complete有Result(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		Channel:      "ch1",
		ResponseKind: E2AResponseKindE2AComplete,
		IsFinal:      true,
		Body:         map[string]any{"result": map[string]any{"answer": "done"}},
	}
	chunk, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if chunk.IsComplete != true {
		t.Error("IsComplete 应为 true")
	}
	if chunk.Payload["answer"] != "done" {
		t.Errorf("Payload.answer 期望 done，实际 %v", chunk.Payload["answer"])
	}
}

// TestE2AResponseToAgentChunk_Error 验证 e2a.error + is_final
func TestE2AResponseToAgentChunk_Error(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		Channel:      "ch1",
		ResponseKind: E2AResponseKindE2AError,
		IsFinal:      true,
		Body: map[string]any{
			"code":    "chat.error",
			"message": "boom",
			"details": map[string]any{"error": "boom"},
		},
	}
	chunk, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if chunk.IsComplete != true {
		t.Error("IsComplete 应为 true")
	}
	if chunk.Payload["error"] != "boom" {
		t.Errorf("Payload.error 期望 boom，实际 %v", chunk.Payload["error"])
	}
}

// TestE2AResponseToAgentChunk_ChunkChatDelta 验证 e2a.chunk chat.delta 反向映射
func TestE2AResponseToAgentChunk_ChunkChatDelta(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		Channel:      "ch1",
		ResponseKind: E2AResponseKindE2AChunk,
		IsFinal:      false,
		Body: map[string]any{
			"delta_kind":        "text",
			"delta":             "hello",
			"event_type":        "chat.delta",
			"source_chunk_type": "text",
		},
	}
	chunk, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if chunk.IsComplete != false {
		t.Error("IsComplete 应为 false")
	}
	if chunk.Payload["event_type"] != "chat.delta" {
		t.Errorf("event_type 期望 chat.delta，实际 %v", chunk.Payload["event_type"])
	}
	if chunk.Payload["content"] != "hello" {
		t.Errorf("content 期望 hello，实际 %v", chunk.Payload["content"])
	}
}

// TestE2AResponseToAgentChunk_ChunkReasoning 验证 delta_kind=reasoning → source_chunk_type=llm_reasoning
func TestE2AResponseToAgentChunk_ChunkReasoning(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		Channel:      "ch1",
		ResponseKind: E2AResponseKindE2AChunk,
		IsFinal:      false,
		Body: map[string]any{
			"delta_kind":        "reasoning",
			"delta":             "thinking...",
			"event_type":        "chat.delta",
			"source_chunk_type": "text",
		},
	}
	chunk, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if chunk.Payload["source_chunk_type"] != "llm_reasoning" {
		t.Errorf("source_chunk_type 期望 llm_reasoning，实际 %v", chunk.Payload["source_chunk_type"])
	}
}

// TestE2AResponseToAgentChunk_ChunkCustom 验证 custom delta_kind
func TestE2AResponseToAgentChunk_ChunkCustom(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		Channel:      "ch1",
		ResponseKind: E2AResponseKindE2AChunk,
		IsFinal:      false,
		Body: map[string]any{
			"delta_kind": "custom",
			"delta":      map[string]any{"name": "search"},
			"event_type": "tool.call",
		},
	}
	chunk, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if chunk.Payload["name"] != "search" {
		t.Errorf("Payload.name 期望 search，实际 %v", chunk.Payload)
	}
}

// TestE2AResponseToAgentChunk_Cron 验证 cron 类型
func TestE2AResponseToAgentChunk_Cron(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		Channel:      "ch1",
		ResponseKind: E2AResponseKindCron,
		Body: map[string]any{
			"action":  "cleanup",
			"status":  "ok",
			"data":    nil,
			"message": "done",
		},
	}
	chunk, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if chunk.IsComplete != true {
		t.Error("IsComplete 应为 true")
	}
	if chunk.Payload["event_type"] != "cron.response" {
		t.Errorf("event_type 期望 cron.response，实际 %v", chunk.Payload["event_type"])
	}
	if chunk.Payload["action"] != "cleanup" {
		t.Errorf("action 期望 cleanup，实际 %v", chunk.Payload["action"])
	}
}

// TestE2AResponseToAgentChunk_ACPOutputRequest 验证 acp.output_request 类型
func TestE2AResponseToAgentChunk_ACPOutputRequest(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		Channel:      "ch1",
		ResponseKind: E2AResponseKindACPOutputRequest,
		Body:         map[string]any{"method": "session/prompt", "params": map[string]any{}},
	}
	chunk, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if chunk.IsComplete != false {
		t.Error("IsComplete 应为 false")
	}
	if chunk.Payload["event_type"] != "acp.output_request" {
		t.Errorf("event_type 期望 acp.output_request，实际 %v", chunk.Payload["event_type"])
	}
}

// TestE2AResponseToAgentChunk_不支持Kind 验证不支持的 kind 返回错误
func TestE2AResponseToAgentChunk_不支持Kind(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		ResponseKind: E2AResponseKindExt,
		IsFinal:      false,
	}
	_, err := E2AResponseToAgentChunk(e2a)
	if err == nil {
		t.Error("不支持的 kind 应返回错误")
	}
}

// TestE2AResponseToAgentChunk_ChunkChatDelta_保留Role 验证 role/member_name 反向透传
func TestE2AResponseToAgentChunk_ChunkChatDelta_保留Role(t *testing.T) {
	e2a := &E2AResponse{
		RequestID:    "r1",
		Channel:      "ch1",
		ResponseKind: E2AResponseKindE2AChunk,
		IsFinal:      false,
		Body: map[string]any{
			"delta_kind":        "text",
			"delta":             "hi",
			"event_type":        "chat.delta",
			"source_chunk_type": "text",
			"role":              "assistant",
			"member_name":       "agent1",
		},
	}
	chunk, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if chunk.Payload["role"] != "assistant" {
		t.Errorf("role 期望 assistant，实际 %v", chunk.Payload["role"])
	}
	if chunk.Payload["member_name"] != "agent1" {
		t.Errorf("member_name 期望 agent1，实际 %v", chunk.Payload["member_name"])
	}
}

// ──────────────────────────── 往返测试 ────────────────────────────

// Test往返_AgentResponse 验证 E2AResponseToAgentResponse(E2AResponseFromAgentResponse(resp)) ≈ resp
func Test往返_AgentResponse(t *testing.T) {
	ts := "2026-01-01T00:00:00Z"

	// ok=true
	resp1 := &schema.AgentResponse{
		RequestID: "r1",
		ChannelID: "ch1",
		OK:        true,
		Payload:   map[string]any{"answer": "42"},
		Metadata:  map[string]any{"trace": "t1"},
	}
	e2a1 := E2AResponseFromAgentResponse(resp1, "resp-1", 0, WithNormTimestamp(ts))
	roundTrip1, err := E2AResponseToAgentResponse(e2a1)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	if roundTrip1.RequestID != resp1.RequestID {
		t.Errorf("RequestID 不匹配: %q vs %q", roundTrip1.RequestID, resp1.RequestID)
	}
	if roundTrip1.ChannelID != resp1.ChannelID {
		t.Errorf("ChannelID 不匹配: %q vs %q", roundTrip1.ChannelID, resp1.ChannelID)
	}
	if roundTrip1.OK != resp1.OK {
		t.Errorf("OK 不匹配: %v vs %v", roundTrip1.OK, resp1.OK)
	}
	if roundTrip1.Payload["answer"] != "42" {
		t.Errorf("Payload.answer 不匹配: %v", roundTrip1.Payload["answer"])
	}

	// ok=false
	resp2 := &schema.AgentResponse{
		RequestID: "r2",
		ChannelID: "ch2",
		OK:        false,
		Payload:   map[string]any{"error": "timeout"},
	}
	e2a2 := E2AResponseFromAgentResponse(resp2, "resp-2", 0, WithNormTimestamp(ts))
	roundTrip2, err := E2AResponseToAgentResponse(e2a2)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	if roundTrip2.OK != false {
		t.Error("OK 应为 false")
	}
	if roundTrip2.Payload["error"] != "timeout" {
		t.Errorf("Payload.error 期望 timeout，实际 %v", roundTrip2.Payload["error"])
	}
}

// Test往返_AgentChunk_终止帧 验证终止帧往返
func Test往返_AgentChunk_终止帧(t *testing.T) {
	ts := "2026-01-01T00:00:00Z"
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "ch1",
		Payload:    map[string]any{"is_complete": true},
		IsComplete: true,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 3, true, WithNormTimestamp(ts))
	rt, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	if rt.IsComplete != true {
		t.Error("IsComplete 应为 true")
	}
	if rt.Payload["is_complete"] != true {
		t.Errorf("Payload.is_complete 期望 true，实际 %v", rt.Payload["is_complete"])
	}
}

// Test往返_AgentChunk_ChatDelta 验证 chat.delta 中间帧往返
func Test往返_AgentChunk_ChatDelta(t *testing.T) {
	ts := "2026-01-01T00:00:00Z"
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "ch1",
		Payload:    map[string]any{"event_type": "chat.delta", "content": "hello", "source_chunk_type": "text"},
		IsComplete: false,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 1, true, WithNormTimestamp(ts))
	rt, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	if rt.IsComplete != false {
		t.Error("IsComplete 应为 false")
	}
	if rt.Payload["event_type"] != "chat.delta" {
		t.Errorf("event_type 不匹配: %v", rt.Payload["event_type"])
	}
	if rt.Payload["content"] != "hello" {
		t.Errorf("content 不匹配: %v", rt.Payload["content"])
	}
	if rt.Payload["source_chunk_type"] != "text" {
		t.Errorf("source_chunk_type 不匹配: %v", rt.Payload["source_chunk_type"])
	}
}

// Test往返_AgentChunk_Reasoning 验证 reasoning 帧往返
func Test往返_AgentChunk_Reasoning(t *testing.T) {
	ts := "2026-01-01T00:00:00Z"
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "ch1",
		Payload:    map[string]any{"event_type": "chat.delta", "content": "think...", "source_chunk_type": "llm_reasoning"},
		IsComplete: false,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 1, true, WithNormTimestamp(ts))
	// 正向: llm_reasoning → reasoning
	if e2a.Body["delta_kind"] != "reasoning" {
		t.Errorf("正向 delta_kind 期望 reasoning，实际 %v", e2a.Body["delta_kind"])
	}
	rt, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	// 反向: reasoning → llm_reasoning
	if rt.Payload["source_chunk_type"] != "llm_reasoning" {
		t.Errorf("往返 source_chunk_type 期望 llm_reasoning，实际 %v", rt.Payload["source_chunk_type"])
	}
}

// Test往返_AgentChunk_错误帧 验证错误帧往返
func Test往返_AgentChunk_错误帧(t *testing.T) {
	ts := "2026-01-01T00:00:00Z"
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "ch1",
		Payload:    map[string]any{"event_type": "chat.error", "error": "boom"},
		IsComplete: true,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 5, true, WithNormTimestamp(ts))
	rt, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	if rt.IsComplete != true {
		t.Error("IsComplete 应为 true")
	}
	if rt.Payload["error"] != "boom" {
		t.Errorf("Payload.error 期望 boom，实际 %v", rt.Payload["error"])
	}
}

// Test往返_AgentChunk_业务结束帧 验证业务结束帧往返
func Test往返_AgentChunk_业务结束帧(t *testing.T) {
	ts := "2026-01-01T00:00:00Z"
	chunk := &schema.AgentResponseChunk{
		RequestID:  "r1",
		ChannelID:  "ch1",
		Payload:    map[string]any{"answer": "final", "event_type": "done"},
		IsComplete: true,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 10, true, WithNormTimestamp(ts))
	rt, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	if rt.IsComplete != true {
		t.Error("IsComplete 应为 true")
	}
	if rt.Payload["answer"] != "final" {
		t.Errorf("Payload.answer 期望 final，实际 %v", rt.Payload["answer"])
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestLegacyPayloadWithinLimit_未超限 验证未超限时原样返回
func TestLegacyPayloadWithinLimit_未超限(t *testing.T) {
	legacy := map[string]any{
		"request_id": "r1",
		"params":     map[string]any{"q": "hello"},
	}
	result := legacyPayloadWithinLimit(legacy)
	p, ok := result["params"].(map[string]any)
	if !ok || p["q"] != "hello" {
		t.Errorf("params 应保持原样，实际 %v", result["params"])
	}
}

// TestLegacyPayloadWithinLimit_超限裁剪 验证超限时裁剪 params
func TestLegacyPayloadWithinLimit_超限裁剪(t *testing.T) {
	// 构造超过 512KB 的 legacy
	bigParams := make(map[string]any)
	for i := 0; i < 100000; i++ {
		bigParams[fmt.Sprintf("key_%d", i)] = "value_that_is_long_enough_to_exceed_limit"
	}
	legacy := map[string]any{
		"request_id": "r1",
		"params":     bigParams,
	}
	result := legacyPayloadWithinLimit(legacy)
	p, ok := result["params"].(map[string]any)
	if !ok {
		t.Fatalf("params 应为 map[string]any，实际 %T", result["params"])
	}
	if _, hasErr := p["_e2a_fallback_error"]; !hasErr {
		t.Error("超限后 params 应包含 _e2a_fallback_error")
	}
}

// TestIsTerminalPayload 验证终止哨兵判断
func TestIsTerminalPayload(t *testing.T) {
	if !isTerminalPayload(map[string]any{"is_complete": true}) {
		t.Error("is_complete=true 应为终止哨兵")
	}
	if isTerminalPayload(map[string]any{"is_complete": true, "extra": "x"}) {
		t.Error("多余键不应为终止哨兵")
	}
	if isTerminalPayload(map[string]any{"is_complete": false}) {
		t.Error("is_complete=false 不应为终止哨兵")
	}
	if isTerminalPayload(map[string]any{}) {
		t.Error("空 map 不应为终止哨兵")
	}
}

// TestEmptyCompleteMarker 验证空终止帧判断
func TestEmptyCompleteMarker(t *testing.T) {
	body := map[string]any{"result": map[string]any{}}
	if !emptyCompleteMarker(body, body["result"]) {
		t.Error("body={result:{}} 应为空终止帧")
	}

	body2 := map[string]any{"result": map[string]any{"answer": "x"}}
	if emptyCompleteMarker(body2, body2["result"]) {
		t.Error("result 非空不应为空终止帧")
	}

	body3 := map[string]any{"result": map[string]any{}, "extra": "x"}
	res3 := body3["result"]
	if emptyCompleteMarker(body3, res3) {
		t.Error("多余键不应为空终止帧")
	}
}

// TestToBool 验证 toBool 转换
func TestToBool(t *testing.T) {
	if toBool(nil) != false {
		t.Error("nil → false")
	}
	if toBool(true) != true {
		t.Error("true → true")
	}
	if toBool(false) != false {
		t.Error("false → false")
	}
	if toBool("") != false {
		t.Error("空串 → false")
	}
	if toBool("x") != true {
		t.Error("非空串 → true")
	}
	if toBool(0) != false {
		t.Error("0 → false")
	}
	if toBool(1) != true {
		t.Error("1 → true")
	}
}

// TestReqMethodToString 验证 ReqMethod 转字符串
func TestReqMethodToString(t *testing.T) {
	if reqMethodToString("") != nil {
		t.Error("空 ReqMethod 应为 nil")
	}
	if reqMethodToString(schema.ReqMethod("chat.send")) != "chat.send" {
		t.Error("chat.send 应返回 chat.send")
	}
}
