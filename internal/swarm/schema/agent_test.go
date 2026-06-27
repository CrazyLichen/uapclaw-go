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
	payload := map[string]any{"content": "answer"}
	resp := NewAgentResponse("req-1", "web",
		WithResponseOK(false),
		WithPayload(payload),
		WithResponseMetadata(map[string]any{"key": "val"}),
	)

	if resp.OK {
		t.Error("OK 应为 false（被 Option 覆盖）")
	}
	if resp.Payload["content"] != "answer" {
		t.Errorf("Payload[\"content\"] = %v, 期望 \"answer\"", resp.Payload["content"])
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
		Payload:   map[string]any{"content": "final answer"},
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
	if decoded.Payload["content"] != "final answer" {
		t.Errorf("Payload[\"content\"]: got %v, want \"final answer\"", decoded.Payload["content"])
	}
	if decoded.Metadata["method"] != "chat.send" {
		t.Errorf("Metadata[\"method\"]: got %v, want \"chat.send\"", decoded.Metadata["method"])
	}
}

// ──────────────────────────── AgentResponseChunk 工厂函数测试 ────────────────────────────

// TestNewAgentResponseChunk 验证工厂函数默认值
func TestNewAgentResponseChunk(t *testing.T) {
	payload := map[string]any{"event_type": "chat.delta", "content": "hello"}
	chunk := NewAgentResponseChunk("req-1", "web", payload)

	if chunk.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 \"req-1\"", chunk.RequestID)
	}
	if chunk.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", chunk.ChannelID)
	}
	if chunk.Payload["event_type"] != "chat.delta" {
		t.Errorf("Payload[\"event_type\"] = %v, 期望 \"chat.delta\"", chunk.Payload["event_type"])
	}
	if chunk.IsComplete {
		t.Error("IsComplete 默认应为 false")
	}
}

// TestNewAgentResponseChunk_使用Option 验证通过 Option 设置各字段
func TestNewAgentResponseChunk_使用Option(t *testing.T) {
	payload := map[string]any{"event_type": "chat.delta", "content": "hello"}
	newPayload := map[string]any{"event_type": "chat.final", "content": "answer"}
	chunk := NewAgentResponseChunk("req-1", "web", payload,
		WithChunkIsComplete(true),
		WithChunkPayload(newPayload),
	)

	if !chunk.IsComplete {
		t.Error("IsComplete 应为 true（被 Option 覆盖）")
	}
	if chunk.Payload["event_type"] != "chat.final" {
		t.Errorf("Payload[\"event_type\"] = %v, 期望 \"chat.final\"", chunk.Payload["event_type"])
	}
}

// TestNewTerminalChunk 验证终止哨兵工厂函数
func TestNewTerminalChunk(t *testing.T) {
	chunk := NewTerminalChunk("req-1", "web")

	if chunk.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 \"req-1\"", chunk.RequestID)
	}
	if chunk.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", chunk.ChannelID)
	}
	if !chunk.IsComplete {
		t.Error("IsComplete 应为 true（终止哨兵）")
	}
	if chunk.Payload["is_complete"] != true {
		t.Errorf("Payload[\"is_complete\"] = %v, 期望 true", chunk.Payload["is_complete"])
	}
}

// ──────────────────────────── AgentResponseChunk Validate 测试 ────────────────────────────

// TestAgentResponseChunk_Validate_正常 验证正常数据通过校验
func TestAgentResponseChunk_Validate_正常(t *testing.T) {
	chunk := NewAgentResponseChunk("req-1", "web", map[string]any{})
	if err := chunk.Validate(); err != nil {
		t.Errorf("正常数据 Validate 返回错误: %v", err)
	}
}

// TestAgentResponseChunk_Validate_requestID为空 验证 request_id 为空返回错误
func TestAgentResponseChunk_Validate_requestID为空(t *testing.T) {
	chunk := &AgentResponseChunk{ChannelID: "web"}
	if err := chunk.Validate(); err == nil {
		t.Error("request_id 为空时期望返回错误")
	}
}

// TestAgentResponseChunk_Validate_channelID为空 验证 channel_id 为空返回错误
func TestAgentResponseChunk_Validate_channelID为空(t *testing.T) {
	chunk := &AgentResponseChunk{RequestID: "req-1"}
	if err := chunk.Validate(); err == nil {
		t.Error("channel_id 为空时期望返回错误")
	}
}

// ──────────────────────────── AgentResponseChunk IsTerminal 测试 ────────────────────────────

// TestAgentResponseChunk_IsTerminal_空payload 验证形态 A：payload 为空，is_complete=true
func TestAgentResponseChunk_IsTerminal_空payload(t *testing.T) {
	chunk := &AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "web",
		IsComplete: true,
	}
	if !chunk.IsTerminal() {
		t.Error("payload 为空且 is_complete=true 时应为终止哨兵")
	}
}

// TestAgentResponseChunk_IsTerminal_标准形态 验证形态 B：payload={"is_complete":true}，is_complete=true
func TestAgentResponseChunk_IsTerminal_标准形态(t *testing.T) {
	chunk := NewTerminalChunk("req-1", "web")
	if !chunk.IsTerminal() {
		t.Error("NewTerminalChunk 创建的 chunk 应为终止哨兵")
	}
}

// TestAgentResponseChunk_IsTerminal_中间chunk 验证 is_complete=false 不是终止哨兵
func TestAgentResponseChunk_IsTerminal_中间chunk(t *testing.T) {
	chunk := NewAgentResponseChunk("req-1", "web", map[string]any{"event_type": "chat.delta", "content": "hi"})
	if chunk.IsTerminal() {
		t.Error("is_complete=false 的中间 chunk 不应为终止哨兵")
	}
}

// TestAgentResponseChunk_IsTerminal_含eventtype 验证含 event_type 的结束 chunk 不是终止哨兵
func TestAgentResponseChunk_IsTerminal_含eventtype(t *testing.T) {
	chunk := &AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "web",
		Payload:    map[string]any{"event_type": "chat.error", "error": "something"},
		IsComplete: true,
	}
	if chunk.IsTerminal() {
		t.Error("含 event_type 的结束 chunk 不应为终止哨兵")
	}
}

// TestAgentResponseChunk_IsTerminal_含content 验证含 content 的结束 chunk 不是终止哨兵
func TestAgentResponseChunk_IsTerminal_含content(t *testing.T) {
	chunk := &AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "web",
		Payload:    map[string]any{"content": "final answer"},
		IsComplete: true,
	}
	if chunk.IsTerminal() {
		t.Error("含 content 的结束 chunk 不应为终止哨兵")
	}
}

// TestAgentResponseChunk_IsTerminal_含error 验证含 error 的结束 chunk 不是终止哨兵
func TestAgentResponseChunk_IsTerminal_含error(t *testing.T) {
	chunk := &AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "web",
		Payload:    map[string]any{"error": "bad"},
		IsComplete: true,
	}
	if chunk.IsTerminal() {
		t.Error("含 error 的结束 chunk 不应为终止哨兵")
	}
}

// ──────────────────────────── AgentResponseChunk JSON 往返测试 ────────────────────────────

// TestAgentResponseChunk_JSON往返 验证 JSON marshal/unmarshal 往返一致
func TestAgentResponseChunk_JSON往返(t *testing.T) {
	original := &AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "web",
		Payload:    map[string]any{"event_type": "chat.delta", "content": "hello"},
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
	if decoded.Payload["event_type"] != "chat.delta" {
		t.Errorf("Payload[\"event_type\"]: got %v, want \"chat.delta\"", decoded.Payload["event_type"])
	}
	if decoded.IsComplete != original.IsComplete {
		t.Errorf("IsComplete: got %v, want %v", decoded.IsComplete, original.IsComplete)
	}
}

// TestAgentResponseChunk_IsComplete为true 验证完成标记序列化
func TestAgentResponseChunk_IsComplete为true(t *testing.T) {
	chunk := &AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "web",
		Payload:    map[string]any{"content": "done"},
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

// TestAgentResponseChunk_终止哨兵JSON往返 验证 NewTerminalChunk 创建的 chunk JSON 往返一致
func TestAgentResponseChunk_终止哨兵JSON往返(t *testing.T) {
	original := NewTerminalChunk("req-1", "web")

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded AgentResponseChunk
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.RequestID != "req-1" {
		t.Errorf("RequestID: got %q, want \"req-1\"", decoded.RequestID)
	}
	if !decoded.IsComplete {
		t.Error("IsComplete 应为 true")
	}
	if !decoded.IsTerminal() {
		t.Error("往返后的终止哨兵仍应被 IsTerminal() 识别")
	}
}
