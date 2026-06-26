package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// ──────────────────────────── AgentRequest 工厂函数测试 ────────────────────────────

// TestNewAgentRequest 验证工厂函数默认值
func TestNewAgentRequest(t *testing.T) {
	params := json.RawMessage(`{"query":"hello"}`)
	req := NewAgentRequest("req-1", "web", ReqMethodChatSend, params)

	if req.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 \"req-1\"", req.RequestID)
	}
	if req.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", req.ChannelID)
	}
	if req.ReqMethod != ReqMethodChatSend {
		t.Errorf("ReqMethod = %q, 期望 %q", req.ReqMethod, ReqMethodChatSend)
	}
	if string(req.Params) != `{"query":"hello"}` {
		t.Errorf("Params = %s, 期望 {\"query\":\"hello\"}", string(req.Params))
	}
	if req.Timestamp <= 0 {
		t.Error("Timestamp 应为正数")
	}
	if req.IsStream {
		t.Error("IsStream 默认应为 false")
	}
	if req.SessionID != nil {
		t.Error("SessionID 默认应为 nil")
	}
	if req.ChatID != nil {
		t.Error("ChatID 默认应为 nil")
	}
	if req.Metadata != nil {
		t.Error("Metadata 默认应为 nil")
	}
	if req.EnableMemory != nil {
		t.Error("EnableMemory 默认应为 nil（三态未设置）")
	}
	if req.PermissionContext != nil {
		t.Error("PermissionContext 默认应为 nil")
	}
}

// TestNewAgentRequest_使用Option 验证通过 Option 设置各字段
func TestNewAgentRequest_使用Option(t *testing.T) {
	sessionID := "sess-1"
	chatID := "chat-1"
	params := json.RawMessage(`{}`)
	pc := NewPermissionContext(WithPermissionPrincipalUserID("user-1"))
	req := NewAgentRequest("req-1", "web", ReqMethodChatSend, params,
		WithAgentSessionID(sessionID),
		WithAgentChatID(chatID),
		WithAgentIsStream(true),
		WithAgentMetadata(map[string]any{"key": "val"}),
		WithAgentEnableMemory(true),
		WithAgentPermissionContext(pc),
	)

	if req.SessionID == nil || *req.SessionID != "sess-1" {
		t.Errorf("SessionID = %v, 期望 \"sess-1\"", req.SessionID)
	}
	if req.ChatID == nil || *req.ChatID != "chat-1" {
		t.Errorf("ChatID = %v, 期望 \"chat-1\"", req.ChatID)
	}
	if !req.IsStream {
		t.Error("IsStream 应为 true")
	}
	if req.Metadata["key"] != "val" {
		t.Error("Metadata[\"key\"] 期望 \"val\"")
	}
	if req.EnableMemory == nil || !*req.EnableMemory {
		t.Error("EnableMemory 应为 *true")
	}
	if req.PermissionContext == nil || req.PermissionContext.PrincipalUserID != "user-1" {
		t.Error("PermissionContext.PrincipalUserID 期望 \"user-1\"")
	}
}

// ──────────────────────────── AgentRequest Validate 测试 ────────────────────────────

// TestAgentRequest_Validate_正常 验证正常数据通过校验
func TestAgentRequest_Validate_正常(t *testing.T) {
	req := NewAgentRequest("req-1", "web", ReqMethodChatSend, json.RawMessage(`{}`))
	if err := req.Validate(); err != nil {
		t.Errorf("正常数据 Validate 返回错误: %v", err)
	}
}

// TestAgentRequest_Validate_requestID为空 验证 request_id 为空返回错误
func TestAgentRequest_Validate_requestID为空(t *testing.T) {
	req := &AgentRequest{ChannelID: "web", ReqMethod: ReqMethodChatSend}
	if err := req.Validate(); err == nil {
		t.Error("request_id 为空时期望返回错误")
	}
}

// TestAgentRequest_Validate_channelID为空 验证 channel_id 为空返回错误
func TestAgentRequest_Validate_channelID为空(t *testing.T) {
	req := &AgentRequest{RequestID: "req-1", ReqMethod: ReqMethodChatSend}
	if err := req.Validate(); err == nil {
		t.Error("channel_id 为空时期望返回错误")
	}
}

// TestAgentRequest_Validate_reqMethod为零值 验证 req_method 为零值返回错误
func TestAgentRequest_Validate_reqMethod为零值(t *testing.T) {
	req := &AgentRequest{RequestID: "req-1", ChannelID: "web"}
	if err := req.Validate(); err == nil {
		t.Error("req_method 为零值时期望返回错误")
	}
}

// ──────────────────────────── AgentRequest JSON 往返测试 ────────────────────────────

// TestAgentRequest_JSON往返 验证 JSON marshal/unmarshal 往返一致
func TestAgentRequest_JSON往返(t *testing.T) {
	sessionID := "sess-1"
	chatID := "chat-1"
	enableMemory := true
	original := &AgentRequest{
		RequestID:        "req-1",
		ChannelID:        "web",
		SessionID:        &sessionID,
		ChatID:           &chatID,
		ReqMethod:        ReqMethodChatSend,
		Params:           json.RawMessage(`{"query":"hello"}`),
		IsStream:         true,
		Timestamp:        1712345678.123,
		Metadata:         map[string]any{"method": "chat.send"},
		EnableMemory:     &enableMemory,
		PermissionContext: &PermissionContext{
			PrincipalUserID:  "user-1",
			TriggeringUserID: "sender-1",
			ChannelID:        "web",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded AgentRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, original.RequestID)
	}
	if decoded.ChannelID != original.ChannelID {
		t.Errorf("ChannelID: got %q, want %q", decoded.ChannelID, original.ChannelID)
	}
	if decoded.SessionID == nil || *decoded.SessionID != "sess-1" {
		t.Errorf("SessionID 往返不一致")
	}
	if decoded.ChatID == nil || *decoded.ChatID != "chat-1" {
		t.Errorf("ChatID 往返不一致")
	}
	if decoded.ReqMethod != original.ReqMethod {
		t.Errorf("ReqMethod: got %q, want %q", decoded.ReqMethod, original.ReqMethod)
	}
	if string(decoded.Params) != string(original.Params) {
		t.Errorf("Params: got %s, want %s", string(decoded.Params), string(original.Params))
	}
	if decoded.IsStream != original.IsStream {
		t.Errorf("IsStream: got %v, want %v", decoded.IsStream, original.IsStream)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp: got %v, want %v", decoded.Timestamp, original.Timestamp)
	}
	if decoded.EnableMemory == nil || !*decoded.EnableMemory {
		t.Errorf("EnableMemory 往返不一致")
	}
	if decoded.PermissionContext == nil || decoded.PermissionContext.PrincipalUserID != "user-1" {
		t.Errorf("PermissionContext 往返不一致")
	}
}

// TestAgentRequest_EnableMemory三态 验证 EnableMemory 三态序列化正确
func TestAgentRequest_EnableMemory三态(t *testing.T) {
	// nil 状态
	reqNil := &AgentRequest{RequestID: "req-1", ChannelID: "web", ReqMethod: ReqMethodChatSend}
	data, _ := json.Marshal(reqNil)
	if strings.Contains(string(data), "enable_memory") {
		t.Errorf("EnableMemory=nil 时 JSON 应省略，实际: %s", string(data))
	}

	// true 状态
	enableTrue := true
	reqTrue := &AgentRequest{RequestID: "req-1", ChannelID: "web", ReqMethod: ReqMethodChatSend, EnableMemory: &enableTrue}
	data, _ = json.Marshal(reqTrue)
	if !strings.Contains(string(data), `"enable_memory":true`) {
		t.Errorf("EnableMemory=true 时 JSON 应包含 enable_memory:true，实际: %s", string(data))
	}

	// false 状态
	enableFalse := false
	reqFalse := &AgentRequest{RequestID: "req-1", ChannelID: "web", ReqMethod: ReqMethodChatSend, EnableMemory: &enableFalse}
	data, _ = json.Marshal(reqFalse)
	if !strings.Contains(string(data), `"enable_memory":false`) {
		t.Errorf("EnableMemory=false 时 JSON 应包含 enable_memory:false，实际: %s", string(data))
	}
}

// ──────────────────────────── AgentResponse 工厂函数测试 ────────────────────────────

// TestNewAgentResponse 验证工厂函数默认值（OK=true）
func TestNewAgentResponse(t *testing.T) {
	resp := NewAgentResponse("req-1", "web")

	if resp.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 \"req-1\"", resp.RequestID)
	}
	if resp.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", resp.ChannelID)
	}
	if !resp.OK {
		t.Error("OK 默认应为 true（对齐 Python）")
	}
	if resp.Payload != nil {
		t.Error("Payload 默认应为 nil")
	}
	if resp.Metadata != nil {
		t.Error("Metadata 默认应为 nil")
	}
}

// TestNewAgentResponse_使用Option 验证通过 Option 设置各字段
func TestNewAgentResponse_使用Option(t *testing.T) {
	payload := json.RawMessage(`{"content":"answer"}`)
	resp := NewAgentResponse("req-1", "web",
		WithResponseOK(false),
		WithPayload(payload),
		WithResponseMetadata(map[string]any{"key": "val"}),
	)

	if resp.OK {
		t.Error("OK 应为 false（被 Option 覆盖）")
	}
	if string(resp.Payload) != `{"content":"answer"}` {
		t.Errorf("Payload = %s, 期望 {\"content\":\"answer\"}", string(resp.Payload))
	}
	if resp.Metadata["key"] != "val" {
		t.Error("Metadata[\"key\"] 期望 \"val\"")
	}
}

// ──────────────────────────── AgentResponse Validate 测试 ────────────────────────────

// TestAgentResponse_Validate_正常 验证正常数据通过校验
func TestAgentResponse_Validate_正常(t *testing.T) {
	resp := NewAgentResponse("req-1", "web")
	if err := resp.Validate(); err != nil {
		t.Errorf("正常数据 Validate 返回错误: %v", err)
	}
}

// TestAgentResponse_Validate_校验失败 验证缺少必填字段返回错误
func TestAgentResponse_Validate_校验失败(t *testing.T) {
	resp := &AgentResponse{RequestID: "", ChannelID: "web"}
	if err := resp.Validate(); err == nil {
		t.Error("request_id 为空时期望返回错误")
	}

	resp2 := &AgentResponse{RequestID: "req-1", ChannelID: ""}
	if err := resp2.Validate(); err == nil {
		t.Error("channel_id 为空时期望返回错误")
	}
}

// ──────────────────────────── AgentResponse JSON 往返测试 ────────────────────────────

// TestAgentResponse_JSON往返 验证 JSON marshal/unmarshal 往返一致
func TestAgentResponse_JSON往返(t *testing.T) {
	original := &AgentResponse{
		RequestID: "req-1",
		ChannelID: "web",
		OK:        true,
		Payload:   json.RawMessage(`{"content":"final answer"}`),
		Metadata:  map[string]any{"method": "chat.send"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded AgentResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, original.RequestID)
	}
	if decoded.ChannelID != original.ChannelID {
		t.Errorf("ChannelID: got %q, want %q", decoded.ChannelID, original.ChannelID)
	}
	if decoded.OK != original.OK {
		t.Errorf("OK: got %v, want %v", decoded.OK, original.OK)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("Payload: got %s, want %s", string(decoded.Payload), string(original.Payload))
	}
	if decoded.Metadata["method"] != "chat.send" {
		t.Errorf("Metadata[\"method\"]: got %v, want \"chat.send\"", decoded.Metadata["method"])
	}
}

// ──────────────────────────── AgentResponseChunk 基础测试 ────────────────────────────

// TestAgentResponseChunk_JSON序列化 验证骨架结构体 JSON 序列化/反序列化基本验证
func TestAgentResponseChunk_JSON序列化(t *testing.T) {
	original := &AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "web",
		Payload:    json.RawMessage(`{"content":"delta"}`),
		IsComplete: false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded AgentResponseChunk
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, original.RequestID)
	}
	if decoded.ChannelID != original.ChannelID {
		t.Errorf("ChannelID: got %q, want %q", decoded.ChannelID, original.ChannelID)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("Payload: got %s, want %s", string(decoded.Payload), string(original.Payload))
	}
	if decoded.IsComplete != original.IsComplete {
		t.Errorf("IsComplete: got %v, want %v", decoded.IsComplete, original.IsComplete)
	}
}

// TestAgentResponseChunk_IsComplete为true 验证完成标记
func TestAgentResponseChunk_IsComplete为true(t *testing.T) {
	chunk := &AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "web",
		Payload:    json.RawMessage(`{"content":"done"}`),
		IsComplete: true,
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	if !strings.Contains(string(data), `"is_complete":true`) {
		t.Errorf("IsComplete=true 时 JSON 应包含 is_complete:true，实际: %s", string(data))
	}
}
