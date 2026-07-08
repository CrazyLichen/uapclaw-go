package message_handler

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestMergeAgentMetadata_两者都有 测试合并双方 metadata
func TestMergeAgentMetadata_两者都有(t *testing.T) {
	reqMD := map[string]any{"sender_id": "user1", "channel": "feishu"}
	respMD := map[string]any{"channel": "dingtalk", "extra": "value"}

	merged := MergeAgentMetadata(reqMD, respMD)

	if merged["sender_id"] != "user1" {
		t.Errorf("sender_id 应来自请求侧，实际：%v", merged["sender_id"])
	}
	if merged["channel"] != "dingtalk" {
		t.Errorf("channel 应被响应侧覆盖，实际：%v", merged["channel"])
	}
	if merged["extra"] != "value" {
		t.Errorf("extra 应来自响应侧，实际：%v", merged["extra"])
	}
}

// TestMergeAgentMetadata_仅请求 测试仅请求侧有 metadata
func TestMergeAgentMetadata_仅请求(t *testing.T) {
	reqMD := map[string]any{"key": "val"}
	merged := MergeAgentMetadata(reqMD, nil)
	if merged["key"] != "val" {
		t.Errorf("key 应来自请求侧，实际：%v", merged["key"])
	}
}

// TestMergeAgentMetadata_都为空 测试双方都为空
func TestMergeAgentMetadata_都为空(t *testing.T) {
	merged := MergeAgentMetadata(nil, nil)
	if merged != nil {
		t.Errorf("双方为空应返回 nil，实际：%v", merged)
	}
	merged = MergeAgentMetadata(map[string]any{}, map[string]any{})
	if merged != nil {
		t.Errorf("双方空 map 应返回 nil，实际：%v", merged)
	}
}

// TestResponseToMessage_事件消息 测试 payload 含 event_type 时构造事件消息
func TestResponseToMessage_事件消息(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-1",
		ChannelID: "feishu_test",
		OK:        true,
		Payload:   map[string]any{"event_type": "chat.delta", "content": "hello"},
		Metadata:  map[string]any{"custom": "value"},
	}

	msg := ResponseToMessage(resp, "sess-1", nil)

	if msg.Type != schema.MessageTypeEvent {
		t.Errorf("含 event_type 的响应应构造事件消息，实际 type=%v", msg.Type)
	}
	if msg.EventType != schema.EventTypeChatDelta {
		t.Errorf("EventType 应为 chat.delta，实际：%v", msg.EventType)
	}
	if msg.SessionID != "sess-1" {
		t.Errorf("SessionID 应为 sess-1，实际：%q", msg.SessionID)
	}
	if msg.OK != true {
		t.Error("事件消息 OK 应为 true")
	}
}

// TestResponseToMessage_普通响应 测试无 event_type 时构造响应消息
func TestResponseToMessage_普通响应(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-2",
		ChannelID: "web_test",
		OK:        true,
		Payload:   map[string]any{"content": "result"},
	}

	msg := ResponseToMessage(resp, "sess-2", nil)

	if msg.Type != schema.MessageTypeRes {
		t.Errorf("无 event_type 应构造响应消息，实际 type=%v", msg.Type)
	}
	if msg.EventType != schema.EventTypeChatFinal {
		t.Errorf("响应消息 EventType 应为 chat.final，实际：%v", msg.EventType)
	}
}

// TestResponseToMessage_metadata合并 测试 metadata 合并
func TestResponseToMessage_metadata合并(t *testing.T) {
	reqMD := map[string]any{"sender_id": "user1", "group_digital_avatar": true}
	resp := &schema.AgentResponse{
		RequestID: "req-3",
		ChannelID: "test",
		OK:        true,
		Payload:   nil,
		Metadata:  map[string]any{"extra": "from_resp"},
	}

	msg := ResponseToMessage(resp, "sess-3", reqMD)

	if !msg.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应从 metadata 提取为 true")
	}
	if msg.Metadata["sender_id"] != "user1" {
		t.Error("metadata 应包含请求侧的 sender_id")
	}
	if msg.Metadata["extra"] != "from_resp" {
		t.Error("metadata 应包含响应侧的 extra")
	}
}

// TestChunkToMessage_基本 测试基本流式 chunk 转换
func TestChunkToMessage_基本(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "req-4",
		ChannelID:  "web_test",
		Payload:    map[string]any{"event_type": "chat.delta", "content": "hello"},
		IsComplete: false,
	}

	msg := ChunkToMessage(chunk, "sess-4", nil)

	if msg.Type != schema.MessageTypeEvent {
		t.Errorf("chunk 转换应构造事件消息，实际 type=%v", msg.Type)
	}
	if msg.EventType != schema.EventTypeChatDelta {
		t.Errorf("EventType 应为 chat.delta，实际：%v", msg.EventType)
	}
	if msg.SessionID != "sess-4" {
		t.Errorf("SessionID 应为 sess-4，实际：%q", msg.SessionID)
	}
}

// TestChunkToMessage_metadata提取 测试 metadata 字段提取
func TestChunkToMessage_metadata提取(t *testing.T) {
	metadata := map[string]any{
		"group_digital_avatar": true,
		"enable_memory":        false,
	}

	chunk := &schema.AgentResponseChunk{
		RequestID:  "req-5",
		ChannelID:  "test",
		Payload:    map[string]any{"content": "chunk"},
		IsComplete: false,
	}

	msg := ChunkToMessage(chunk, "sess-5", metadata)

	if !msg.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应从 metadata 提取为 true")
	}
	if msg.EnableMemory {
		t.Error("EnableMemory 应从 metadata 提取为 false")
	}
}

// TestIsTerminalStreamChunk 测试终止哨兵识别
func TestIsTerminalStreamChunk(t *testing.T) {
	// 非终止：is_complete=false
	chunk1 := &schema.AgentResponseChunk{RequestID: "r1", ChannelID: "c", IsComplete: false}
	if IsTerminalStreamChunk(chunk1) {
		t.Error("is_complete=false 不应为终止哨兵")
	}

	// 终止：is_complete=true + payload=nil
	chunk2 := &schema.AgentResponseChunk{RequestID: "r2", ChannelID: "c", IsComplete: true, Payload: nil}
	if !IsTerminalStreamChunk(chunk2) {
		t.Error("is_complete=true + payload=nil 应为终止哨兵")
	}

	// 终止：is_complete=true + payload 仅含 {"is_complete": true}
	chunk3 := &schema.AgentResponseChunk{
		RequestID:  "r3",
		ChannelID:  "c",
		IsComplete: true,
		Payload:    map[string]any{"is_complete": true},
	}
	if !IsTerminalStreamChunk(chunk3) {
		t.Error("仅含 is_complete=true 的 payload 应为终止哨兵")
	}

	// 非终止：payload 含 event_type
	chunk4 := &schema.AgentResponseChunk{
		RequestID:  "r4",
		ChannelID:  "c",
		IsComplete: true,
		Payload:    map[string]any{"event_type": "chat.final"},
	}
	if IsTerminalStreamChunk(chunk4) {
		t.Error("含 event_type 不应为终止哨兵")
	}

	// 非终止：payload 含 content
	chunk5 := &schema.AgentResponseChunk{
		RequestID:  "r5",
		ChannelID:  "c",
		IsComplete: true,
		Payload:    map[string]any{"content": "some text"},
	}
	if IsTerminalStreamChunk(chunk5) {
		t.Error("含 content 不应为终止哨兵")
	}
}

// TestToBool 测试 toBool 辅助函数
func TestToBool(t *testing.T) {
	tests := []struct {
		input  any
		expect bool
	}{
		{true, true},
		{false, false},
		{1, true},
		{0, false},
		{"true", true},
		{"", false},
		{nil, false},
		{3.14, true},
		{0.0, false},
	}
	for _, tc := range tests {
		if got := toBool(tc.input); got != tc.expect {
			t.Errorf("toBool(%v) = %v, want %v", tc.input, got, tc.expect)
		}
	}
}
