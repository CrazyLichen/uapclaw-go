package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// ──────────────────────────── MessageType 枚举测试 ────────────────────────────

// TestAllMessageTypes 验证返回全部 3 个 MessageType 枚举值
func TestAllMessageTypes(t *testing.T) {
	all := AllMessageTypes()
	if len(all) != 3 {
		t.Fatalf("期望 3 个 MessageType，实际 %d", len(all))
	}
	expected := map[MessageType]bool{
		MessageTypeReq:   true,
		MessageTypeRes:   true,
		MessageTypeEvent: true,
	}
	for _, mt := range all {
		if !expected[mt] {
			t.Errorf("意外的 MessageType 值: %q", mt)
		}
	}
}

// TestParseMessageType_合法值 验证合法字符串解析
func TestParseMessageType_合法值(t *testing.T) {
	tests := []struct {
		input string
		want  MessageType
	}{
		{"req", MessageTypeReq},
		{"res", MessageTypeRes},
		{"event", MessageTypeEvent},
	}
	for _, tt := range tests {
		got, err := ParseMessageType(tt.input)
		if err != nil {
			t.Errorf("ParseMessageType(%q) 返回错误: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMessageType(%q) = %q, 期望 %q", tt.input, got, tt.want)
		}
	}
}

// TestParseMessageType_非法值 验证非法字符串返回错误
func TestParseMessageType_非法值(t *testing.T) {
	_, err := ParseMessageType("invalid")
	if err == nil {
		t.Error("ParseMessageType(\"invalid\") 期望返回错误，实际返回 nil")
	}
	_, err = ParseMessageType("")
	if err == nil {
		t.Error("ParseMessageType(\"\") 期望返回错误，实际返回 nil")
	}
}

// TestIsValidMessageType 验证 IsValidMessageType 判断
func TestIsValidMessageType(t *testing.T) {
	if !IsValidMessageType("req") {
		t.Error("IsValidMessageType(\"req\") 期望 true")
	}
	if !IsValidMessageType("res") {
		t.Error("IsValidMessageType(\"res\") 期望 true")
	}
	if !IsValidMessageType("event") {
		t.Error("IsValidMessageType(\"event\") 期望 true")
	}
	if IsValidMessageType("invalid") {
		t.Error("IsValidMessageType(\"invalid\") 期望 false")
	}
}

// TestMessageType_String 验证 String 方法
func TestMessageType_String(t *testing.T) {
	if MessageTypeReq.String() != "req" {
		t.Errorf("MessageTypeReq.String() = %q, 期望 \"req\"", MessageTypeReq.String())
	}
}

// TestMessageType_GoString 验证 GoString 方法
func TestMessageType_GoString(t *testing.T) {
	got := MessageTypeReq.GoString()
	want := `schema.MessageType("req")`
	if got != want {
		t.Errorf("MessageTypeReq.GoString() = %q, 期望 %q", got, want)
	}
}

// ──────────────────────────── Validate 测试 ────────────────────────────

// TestValidate_请求消息合法 验证合法 req 消息通过校验
func TestValidate_请求消息合法(t *testing.T) {
	msg := &Message{
		ID:        "test-id",
		Type:      MessageTypeReq,
		ChannelID: "web",
		SessionID: "sess-1",
		Params:    json.RawMessage(`{"query":"hello"}`),
		Timestamp: 1712345678.123,
		OK:        true,
		ReqMethod: ReqMethodChatSend,
	}
	if err := msg.Validate(); err != nil {
		t.Errorf("合法 req 消息 Validate 返回错误: %v", err)
	}
}

// TestValidate_响应消息合法 验证合法 res 消息通过校验
func TestValidate_响应消息合法(t *testing.T) {
	msg := &Message{
		ID:        "test-id",
		Type:      MessageTypeRes,
		ChannelID: "web",
		OK:        true,
		Payload:   map[string]any{"content": "ok"},
	}
	if err := msg.Validate(); err != nil {
		t.Errorf("合法 res 消息 Validate 返回错误: %v", err)
	}
}

// TestValidate_事件消息合法 验证合法 event 消息通过校验
func TestValidate_事件消息合法(t *testing.T) {
	msg := &Message{
		ID:        "test-id",
		Type:      MessageTypeEvent,
		ChannelID: "web",
		OK:        true,
		EventType: EventTypeChatDelta,
		Payload:   map[string]any{"content": "delta"},
	}
	if err := msg.Validate(); err != nil {
		t.Errorf("合法 event 消息 Validate 返回错误: %v", err)
	}
}

// TestValidate_ID为空 验证 ID 为空返回错误
func TestValidate_ID为空(t *testing.T) {
	msg := &Message{Type: MessageTypeReq, ChannelID: "web", ReqMethod: ReqMethodChatSend}
	if err := msg.Validate(); err == nil {
		t.Error("ID 为空时期望返回错误")
	}
}

// TestValidate_Type非法 验证非法 Type 返回错误
func TestValidate_Type非法(t *testing.T) {
	msg := &Message{ID: "test-id", Type: MessageType("invalid"), ChannelID: "web"}
	if err := msg.Validate(); err == nil {
		t.Error("非法 Type 时期望返回错误")
	}
}

// TestValidate_ChannelID为空 验证 ChannelID 为空返回错误
func TestValidate_ChannelID为空(t *testing.T) {
	msg := &Message{ID: "test-id", Type: MessageTypeReq, ReqMethod: ReqMethodChatSend}
	if err := msg.Validate(); err == nil {
		t.Error("ChannelID 为空时期望返回错误")
	}
}

// TestValidate_请求消息缺ReqMethod 验证 req 消息缺少 req_method 返回错误
func TestValidate_请求消息缺ReqMethod(t *testing.T) {
	msg := &Message{ID: "test-id", Type: MessageTypeReq, ChannelID: "web"}
	if err := msg.Validate(); err == nil {
		t.Error("req 消息缺少 req_method 时期望返回错误")
	}
}

// TestValidate_事件消息缺EventType 验证 event 消息缺少 event_type 返回错误
func TestValidate_事件消息缺EventType(t *testing.T) {
	msg := &Message{ID: "test-id", Type: MessageTypeEvent, ChannelID: "web"}
	if err := msg.Validate(); err == nil {
		t.Error("event 消息缺少 event_type 时期望返回错误")
	}
}

// ──────────────────────────── 工厂函数测试 ────────────────────────────

// TestNewReqMessage 验证 NewReqMessage 构造正确
func TestNewReqMessage(t *testing.T) {
	params := json.RawMessage(`{"query":"hello"}`)
	msg := NewReqMessage("web", "sess-1", ReqMethodChatSend, params)

	if msg.ID == "" {
		t.Error("ID 不应为空")
	}
	if len(msg.ID) != 32 {
		t.Errorf("ID 长度应为 32（UUID 无连字符），实际 %d", len(msg.ID))
	}
	if msg.Type != MessageTypeReq {
		t.Errorf("Type = %q, 期望 \"req\"", msg.Type)
	}
	if msg.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", msg.ChannelID)
	}
	if msg.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, 期望 \"sess-1\"", msg.SessionID)
	}
	if string(msg.Params) != `{"query":"hello"}` {
		t.Errorf("Params = %s, 期望 {\"query\":\"hello\"}", string(msg.Params))
	}
	if msg.Timestamp <= 0 {
		t.Error("Timestamp 应为正数")
	}
	if !msg.OK {
		t.Error("OK 应为 true")
	}
	if msg.ReqMethod != ReqMethodChatSend {
		t.Errorf("ReqMethod = %q, 期望 %q", msg.ReqMethod, ReqMethodChatSend)
	}
	if msg.EventType != "" {
		t.Errorf("EventType 应为零值，实际 %q", msg.EventType)
	}
	if msg.Payload != nil {
		t.Error("Payload 应为 nil")
	}
	if !msg.EnableStreaming {
		t.Error("EnableStreaming 应为 true（对齐 Python 默认值）")
	}
}

// TestNewReqMessage_WithOptions 验证 NewReqMessage 使用 Option
func TestNewReqMessage_WithOptions(t *testing.T) {
	params := json.RawMessage(`{}`)
	msg := NewReqMessage("web", "sess-1", ReqMethodChatSend, params,
		WithMode(ModeCodeNormal),
		WithIsStream(true),
		WithProvider("feishu"),
		WithChatID("chat-1"),
		WithUserID("user-1"),
		WithBotID("bot-1"),
		WithGroupDigitalAvatar(true),
		WithEnableMemory(true),
		WithEnableStreaming(false),
		WithMetadata(map[string]any{"key": "val"}),
	)

	if msg.Mode != ModeCodeNormal {
		t.Errorf("Mode = %q, 期望 %q", msg.Mode, ModeCodeNormal)
	}
	if !msg.IsStream {
		t.Error("IsStream 应为 true")
	}
	if msg.Provider != "feishu" {
		t.Errorf("Provider = %q, 期望 \"feishu\"", msg.Provider)
	}
	if msg.ChatID != "chat-1" {
		t.Errorf("ChatID = %q, 期望 \"chat-1\"", msg.ChatID)
	}
	if msg.UserID != "user-1" {
		t.Errorf("UserID = %q, 期望 \"user-1\"", msg.UserID)
	}
	if msg.BotID != "bot-1" {
		t.Errorf("BotID = %q, 期望 \"bot-1\"", msg.BotID)
	}
	if !msg.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 true")
	}
	if !msg.EnableMemory {
		t.Error("EnableMemory 应为 true")
	}
	if msg.EnableStreaming {
		t.Error("EnableStreaming 应为 false（被 Option 覆盖）")
	}
	if msg.Metadata["key"] != "val" {
		t.Error("Metadata[\"key\"] 期望 \"val\"")
	}
}

// TestNewResMessage 验证 NewResMessage 构造正确
func TestNewResMessage(t *testing.T) {
	payload := map[string]any{"content": "response"}
	msg := NewResMessage("web", "sess-1", true, payload)

	if msg.Type != MessageTypeRes {
		t.Errorf("Type = %q, 期望 \"res\"", msg.Type)
	}
	if !msg.OK {
		t.Error("OK 应为 true")
	}
	if msg.Payload["content"] != "response" {
		t.Errorf("Payload[\"content\"] = %v, 期望 \"response\"", msg.Payload["content"])
	}
	if msg.ReqMethod != "" {
		t.Errorf("ReqMethod 应为零值，实际 %q", msg.ReqMethod)
	}
	if msg.Params != nil {
		t.Error("Params 应为 nil")
	}
	if !msg.EnableStreaming {
		t.Error("EnableStreaming 应为 true（对齐 Python 默认值）")
	}
}

// TestNewResMessage_WithOptions 验证 NewResMessage 使用 Option 设置 EventType
func TestNewResMessage_WithOptions(t *testing.T) {
	payload := map[string]any{}
	msg := NewResMessage("web", "sess-1", true, payload,
		WithEventType(EventTypeChatFinal),
	)

	if msg.EventType != EventTypeChatFinal {
		t.Errorf("EventType = %q, 期望 %q", msg.EventType, EventTypeChatFinal)
	}
}

// TestNewEventMessage 验证 NewEventMessage 构造正确
func TestNewEventMessage(t *testing.T) {
	payload := map[string]any{"content": "delta"}
	msg := NewEventMessage("web", "sess-1", EventTypeChatDelta, payload)

	if msg.Type != MessageTypeEvent {
		t.Errorf("Type = %q, 期望 \"event\"", msg.Type)
	}
	if !msg.OK {
		t.Error("OK 应为 true")
	}
	if msg.EventType != EventTypeChatDelta {
		t.Errorf("EventType = %q, 期望 %q", msg.EventType, EventTypeChatDelta)
	}
	if msg.Payload["content"] != "delta" {
		t.Errorf("Payload[\"content\"] = %v, 期望 \"delta\"", msg.Payload["content"])
	}
	if msg.ReqMethod != "" {
		t.Errorf("ReqMethod 应为零值，实际 %q", msg.ReqMethod)
	}
	if msg.Params != nil {
		t.Error("Params 应为 nil")
	}
	if !msg.EnableStreaming {
		t.Error("EnableStreaming 应为 true（对齐 Python 默认值）")
	}
}

// TestNewEventMessage_WithOptions 验证 NewEventMessage 使用 Option 覆盖 EnableStreaming
func TestNewEventMessage_WithOptions(t *testing.T) {
	payload := map[string]any{}
	msg := NewEventMessage("web", "sess-1", EventTypeChatToolResult, payload,
		WithEnableStreaming(false),
	)

	if msg.EnableStreaming {
		t.Error("EnableStreaming 应为 false（被 Option 覆盖，对齐 cron 场景）")
	}
}

// ──────────────────────────── JSON 往返测试 ────────────────────────────

// TestMessageJSONRoundtrip_请求消息 验证 req 消息 JSON 序列化往返
func TestMessageJSONRoundtrip_请求消息(t *testing.T) {
	original := &Message{
		ID:                 "test-req-id",
		Type:               MessageTypeReq,
		ChannelID:          "web",
		SessionID:          "sess-1",
		Params:             json.RawMessage(`{"query":"hello","mode":"agent.plan"}`),
		Timestamp:          1712345678.123,
		OK:                 true,
		Provider:           "feishu",
		ChatID:             "chat-1",
		UserID:             "user-1",
		BotID:              "bot-1",
		ReqMethod:          ReqMethodChatSend,
		Mode:               ModeAgentPlan,
		IsStream:           true,
		StreamSeq:          3,
		StreamID:           "stream-1",
		GroupDigitalAvatar: true,
		EnableMemory:       true,
		EnableStreaming:    true,
		Metadata:           map[string]any{"method": "chat.send", "cwd": "/tmp"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type: got %q, want %q", decoded.Type, original.Type)
	}
	if decoded.ChannelID != original.ChannelID {
		t.Errorf("ChannelID: got %q, want %q", decoded.ChannelID, original.ChannelID)
	}
	if decoded.SessionID != original.SessionID {
		t.Errorf("SessionID: got %q, want %q", decoded.SessionID, original.SessionID)
	}
	if string(decoded.Params) != string(original.Params) {
		t.Errorf("Params: got %s, want %s", string(decoded.Params), string(original.Params))
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp: got %v, want %v", decoded.Timestamp, original.Timestamp)
	}
	if decoded.OK != original.OK {
		t.Errorf("OK: got %v, want %v", decoded.OK, original.OK)
	}
	if decoded.Provider != original.Provider {
		t.Errorf("Provider: got %q, want %q", decoded.Provider, original.Provider)
	}
	if decoded.ChatID != original.ChatID {
		t.Errorf("ChatID: got %q, want %q", decoded.ChatID, original.ChatID)
	}
	if decoded.UserID != original.UserID {
		t.Errorf("UserID: got %q, want %q", decoded.UserID, original.UserID)
	}
	if decoded.BotID != original.BotID {
		t.Errorf("BotID: got %q, want %q", decoded.BotID, original.BotID)
	}
	if decoded.ReqMethod != original.ReqMethod {
		t.Errorf("ReqMethod: got %q, want %q", decoded.ReqMethod, original.ReqMethod)
	}
	if decoded.Mode != original.Mode {
		t.Errorf("Mode: got %q, want %q", decoded.Mode, original.Mode)
	}
	if decoded.IsStream != original.IsStream {
		t.Errorf("IsStream: got %v, want %v", decoded.IsStream, original.IsStream)
	}
	if decoded.StreamSeq != original.StreamSeq {
		t.Errorf("StreamSeq: got %d, want %d", decoded.StreamSeq, original.StreamSeq)
	}
	if decoded.StreamID != original.StreamID {
		t.Errorf("StreamID: got %q, want %q", decoded.StreamID, original.StreamID)
	}
	if decoded.GroupDigitalAvatar != original.GroupDigitalAvatar {
		t.Errorf("GroupDigitalAvatar: got %v, want %v", decoded.GroupDigitalAvatar, original.GroupDigitalAvatar)
	}
	if decoded.EnableMemory != original.EnableMemory {
		t.Errorf("EnableMemory: got %v, want %v", decoded.EnableMemory, original.EnableMemory)
	}
	if decoded.EnableStreaming != original.EnableStreaming {
		t.Errorf("EnableStreaming: got %v, want %v", decoded.EnableStreaming, original.EnableStreaming)
	}
	if decoded.Metadata["method"] != "chat.send" {
		t.Errorf("Metadata[\"method\"]: got %v, want \"chat.send\"", decoded.Metadata["method"])
	}
}

// TestMessageJSONRoundtrip_响应消息 验证 res 消息 JSON 序列化往返
func TestMessageJSONRoundtrip_响应消息(t *testing.T) {
	original := &Message{
		ID:        "test-res-id",
		Type:      MessageTypeRes,
		ChannelID: "web",
		SessionID: "sess-1",
		Timestamp: 1712345678.456,
		OK:        true,
		Payload:   map[string]any{"content": "final answer", "is_complete": true},
		EventType: EventTypeChatFinal,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.Type != MessageTypeRes {
		t.Errorf("Type: got %q, want \"res\"", decoded.Type)
	}
	if decoded.Payload["content"] != "final answer" {
		t.Errorf("Payload[\"content\"]: got %v, want \"final answer\"", decoded.Payload["content"])
	}
	if decoded.EventType != EventTypeChatFinal {
		t.Errorf("EventType: got %q, want %q", decoded.EventType, EventTypeChatFinal)
	}
}

// TestMessageJSONRoundtrip_事件消息 验证 event 消息 JSON 序列化往返
func TestMessageJSONRoundtrip_事件消息(t *testing.T) {
	original := &Message{
		ID:                 "test-event-id",
		Type:               MessageTypeEvent,
		ChannelID:          "web",
		SessionID:          "sess-1",
		Timestamp:          1712345678.789,
		OK:                 true,
		Payload:            map[string]any{"content": "delta text"},
		EventType:          EventTypeChatDelta,
		GroupDigitalAvatar: true,
		EnableMemory:       true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.Type != MessageTypeEvent {
		t.Errorf("Type: got %q, want \"event\"", decoded.Type)
	}
	if decoded.EventType != EventTypeChatDelta {
		t.Errorf("EventType: got %q, want %q", decoded.EventType, EventTypeChatDelta)
	}
	if !decoded.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 true")
	}
}

// TestMessageJSONRoundtrip_最小消息 验证仅必填字段的消息往返
func TestMessageJSONRoundtrip_最小消息(t *testing.T) {
	original := &Message{
		ID:        "min-id",
		Type:      MessageTypeReq,
		ChannelID: "web",
		Params:    json.RawMessage(`{}`),
		Timestamp: 1712345678.0,
		OK:        true,
		ReqMethod: ReqMethodChatSend,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.ID != "min-id" {
		t.Errorf("ID: got %q, want \"min-id\"", decoded.ID)
	}
	if decoded.ReqMethod != ReqMethodChatSend {
		t.Errorf("ReqMethod: got %q, want %q", decoded.ReqMethod, ReqMethodChatSend)
	}
	// 可选字段应为零值
	if decoded.Provider != "" {
		t.Errorf("Provider 应为空，实际 %q", decoded.Provider)
	}
	if decoded.Payload != nil {
		t.Error("Payload 应为 nil")
	}
	if decoded.Metadata != nil {
		t.Error("Metadata 应为 nil")
	}
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

// TestNewMessageID 验证 NewMessageID 生成 UUID 格式
func TestNewMessageID(t *testing.T) {
	id := NewMessageID()
	if id == "" {
		t.Error("NewMessageID 返回空串")
	}
	// UUID v4 无连字符：32 hex 字符
	if len(id) != 32 {
		t.Errorf("ID 长度应为 32，实际 %d: %q", len(id), id)
	}
	// 两次生成应不同
	id2 := NewMessageID()
	if id == id2 {
		t.Error("两次生成的 ID 不应相同")
	}
}

// TestNowTimestamp 验证 NowTimestamp 返回合理时间戳
func TestNowTimestamp(t *testing.T) {
	ts := NowTimestamp()
	// 2024-01-01 00:00:00 UTC = 1704067200.0
	// 2030-01-01 00:00:00 UTC = 1893456000.0
	if ts < 1.7e9 || ts > 2.0e9 {
		t.Errorf("NowTimestamp = %v，不在合理范围内 [1.7e9, 2.0e9]", ts)
	}
}

// ──────────────────────────── omitempty 验证测试 ────────────────────────────

// TestMessageJSON_omitempty_payload 验证 payload 为 nil 时 JSON 省略
func TestMessageJSON_omitempty_payload(t *testing.T) {
	msg := &Message{
		ID:        "test-id",
		Type:      MessageTypeReq,
		ChannelID: "web",
		Params:    json.RawMessage(`{}`),
		Timestamp: 1712345678.0,
		OK:        true,
		ReqMethod: ReqMethodChatSend,
		// Payload 为 nil
		// Metadata 为 nil
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	jsonStr := string(data)
	// payload 和 metadata 应被省略
	if strings.Contains(jsonStr, "payload") {
		t.Errorf("Payload 为 nil 时 JSON 应省略 payload 字段，实际: %s", jsonStr)
	}
	if strings.Contains(jsonStr, "metadata") {
		t.Errorf("Metadata 为 nil 时 JSON 应省略 metadata 字段，实际: %s", jsonStr)
	}
}

// TestMessageJSON_omitempty_payload非nil 验证 payload 非 nil 时 JSON 输出
func TestMessageJSON_omitempty_payload非nil(t *testing.T) {
	msg := &Message{
		ID:        "test-id",
		Type:      MessageTypeRes,
		ChannelID: "web",
		Timestamp: 1712345678.0,
		OK:        true,
		Payload:   map[string]any{"content": "ok"},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, "payload") {
		t.Errorf("Payload 非 nil 时 JSON 应包含 payload 字段，实际: %s", jsonStr)
	}
}

// TestMessageJSON_omitempty_metadata非nil 验证 metadata 非 nil 时 JSON 输出
func TestMessageJSON_omitempty_metadata非nil(t *testing.T) {
	msg := &Message{
		ID:        "test-id",
		Type:      MessageTypeReq,
		ChannelID: "web",
		Params:    json.RawMessage(`{}`),
		Timestamp: 1712345678.0,
		OK:        true,
		ReqMethod: ReqMethodChatSend,
		Metadata:  map[string]any{"key": "val"},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, "metadata") {
		t.Errorf("Metadata 非 nil 时 JSON 应包含 metadata 字段，实际: %s", jsonStr)
	}
}

// TestWithSessionID 验证 WithSessionID 选项设置会话标识
func TestWithSessionID(t *testing.T) {
	msg := &Message{}
	WithSessionID("sess-1")(msg)
	if msg.SessionID != "sess-1" {
		t.Errorf("期望 SessionID=sess-1，实际 %s", msg.SessionID)
	}
}

// TestWithStreamSeq 验证 WithStreamSeq 选项设置流式序号
func TestWithStreamSeq(t *testing.T) {
	msg := &Message{}
	WithStreamSeq(42)(msg)
	if msg.StreamSeq != 42 {
		t.Errorf("期望 StreamSeq=42，实际 %d", msg.StreamSeq)
	}
}

// TestWithStreamID 验证 WithStreamID 选项设置流式标识
func TestWithStreamID(t *testing.T) {
	msg := &Message{}
	WithStreamID("stream-1")(msg)
	if msg.StreamID != "stream-1" {
		t.Errorf("期望 StreamID=stream-1，实际 %s", msg.StreamID)
	}
}
