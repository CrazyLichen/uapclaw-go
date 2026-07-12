package web

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNormalizeGatewayMessage_content转query 测试 content→query 映射
func TestNormalizeGatewayMessage_content转query(t *testing.T) {
	params, _ := json.Marshal(map[string]any{"content": "你好"})
	msg := &schema.Message{
		ID:        "msg-1",
		Type:      schema.MessageTypeReq,
		ChannelID: "web",
		SessionID: "sess-1",
		Params:    params,
		OK:        true,
		ReqMethod: schema.ReqMethodChatSend,
	}

	result := NormalizeGatewayMessage(msg)

	// 验证 query 被设置
	var resultParams map[string]any
	_ = json.Unmarshal(result.Params, &resultParams)
	assert.Equal(t, "你好", resultParams["query"])
	assert.Equal(t, "你好", resultParams["content"])
}

// TestNormalizeGatewayMessage_已有query不覆盖 测试 params 中已有 query 时不覆盖
func TestNormalizeGatewayMessage_已有query不覆盖(t *testing.T) {
	params, _ := json.Marshal(map[string]any{"query": "原始查询", "content": "其他内容"})
	msg := &schema.Message{
		ID:        "msg-2",
		Type:      schema.MessageTypeReq,
		ChannelID: "web",
		SessionID: "sess-1",
		Params:    params,
		OK:        true,
		ReqMethod: schema.ReqMethodChatSend,
	}

	result := NormalizeGatewayMessage(msg)

	var resultParams map[string]any
	_ = json.Unmarshal(result.Params, &resultParams)
	assert.Equal(t, "原始查询", resultParams["query"])
}

// TestNormalizeGatewayMessage_resume转cancel 测试 resume→cancel+intent=resume
func TestNormalizeGatewayMessage_resume转cancel(t *testing.T) {
	params, _ := json.Marshal(map[string]any{})
	msg := &schema.Message{
		ID:        "msg-3",
		Type:      schema.MessageTypeReq,
		ChannelID: "web",
		SessionID: "sess-1",
		Params:    params,
		OK:        true,
		ReqMethod: schema.ReqMethodChatResume,
	}

	result := NormalizeGatewayMessage(msg)

	assert.Equal(t, schema.ReqMethodChatCancel, result.ReqMethod)
	var resultParams map[string]any
	_ = json.Unmarshal(result.Params, &resultParams)
	assert.Equal(t, "resume", resultParams["intent"])
}

// TestNormalizeGatewayMessage_resume已有intent不覆盖 测试 resume 时已有 intent 不覆盖
func TestNormalizeGatewayMessage_resume已有intent不覆盖(t *testing.T) {
	params, _ := json.Marshal(map[string]any{"intent": "pause"})
	msg := &schema.Message{
		ID:        "msg-4",
		Type:      schema.MessageTypeReq,
		ChannelID: "web",
		SessionID: "sess-1",
		Params:    params,
		OK:        true,
		ReqMethod: schema.ReqMethodChatResume,
	}

	result := NormalizeGatewayMessage(msg)

	assert.Equal(t, schema.ReqMethodChatCancel, result.ReqMethod)
	var resultParams map[string]any
	_ = json.Unmarshal(result.Params, &resultParams)
	assert.Equal(t, "pause", resultParams["intent"])
}

// TestNormalizeGatewayMessage_isStream推断 测试 is_stream 推断逻辑
func TestNormalizeGatewayMessage_isStream推断(t *testing.T) {
	tests := []struct {
		name       string
		reqMethod  schema.ReqMethod
		isStream   bool
		wantStream bool
	}{
		{"chat.send 默认流式", schema.ReqMethodChatSend, false, true},
		{"history.get 默认流式", schema.ReqMethodHistoryGet, false, true},
		{"chat.interrupt 非流式", schema.ReqMethodChatCancel, false, false},
		{"chat.user_answer 非流式", schema.ReqMethodChatAnswer, false, false},
		{"显式流式", schema.ReqMethodChatCancel, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &schema.Message{
				ID:        "msg-stream",
				Type:      schema.MessageTypeReq,
				ChannelID: "web",
				SessionID: "sess-1",
				Params:    json.RawMessage(`{}`),
				OK:        true,
				ReqMethod: tt.reqMethod,
				IsStream:  tt.isStream,
			}

			result := NormalizeGatewayMessage(msg)
			assert.Equal(t, tt.wantStream, result.IsStream)
		})
	}
}

// TestNormalizeGatewayMessage_不修改原始消息 测试 normalize 不修改原始消息
func TestNormalizeGatewayMessage_不修改原始消息(t *testing.T) {
	originalParams, _ := json.Marshal(map[string]any{"content": "原始"})
	msg := &schema.Message{
		ID:        "msg-orig",
		Type:      schema.MessageTypeReq,
		ChannelID: "web",
		SessionID: "sess-1",
		Params:    originalParams,
		OK:        true,
		ReqMethod: schema.ReqMethodChatSend,
	}

	_ = NormalizeGatewayMessage(msg)

	// 原始消息的 params 不应被修改
	var origParams map[string]any
	_ = json.Unmarshal(msg.Params, &origParams)
	_, hasQuery := origParams["query"]
	assert.False(t, hasQuery, "原始消息不应被添加 query 字段")
}

// TestNormalizeGatewayMessage_默认reqMethod 测试 req_method 为空时默认为 chat.send
func TestNormalizeGatewayMessage_默认reqMethod(t *testing.T) {
	msg := &schema.Message{
		ID:        "msg-default",
		Type:      schema.MessageTypeReq,
		ChannelID: "web",
		SessionID: "sess-1",
		Params:    json.RawMessage(`{}`),
		OK:        true,
	}

	result := NormalizeGatewayMessage(msg)

	assert.Equal(t, schema.ReqMethodChatSend, result.ReqMethod)
	assert.True(t, result.IsStream)
}

// TestBuildUserMessage_基本构造 测试从 RPC 参数构造 Message
func TestBuildUserMessage_基本构造(t *testing.T) {
	params := map[string]any{"query": "你好", "session_id": "sess-1"}
	msg := BuildUserMessage("req-1", "chat.send", params, "sess-1", nil)

	assert.Equal(t, "req-1", msg.ID)
	assert.Equal(t, schema.MessageTypeReq, msg.Type)
	assert.Equal(t, "web", msg.ChannelID)
	assert.Equal(t, "sess-1", msg.SessionID)
	assert.Equal(t, schema.ReqMethodChatSend, msg.ReqMethod)
	assert.True(t, msg.OK)
}

// TestForwardReqMethods_核心方法 测试核心方法在集合中
func TestForwardReqMethods_核心方法(t *testing.T) {
	coreMethods := []string{
		"chat.send", "chat.interrupt", "chat.resume", "chat.user_answer",
		"initialize", "history.get",
	}
	for _, method := range coreMethods {
		assert.True(t, ForwardReqMethods[method], "方法 %q 应在 ForwardReqMethods 中", method)
	}
}

// TestForwardNoLocalHandlerMethods_核心方法 测试核心方法在集合中
func TestForwardNoLocalHandlerMethods_核心方法(t *testing.T) {
	coreMethods := []string{
		"initialize", "acp.tool_response", "team.delete",
		"skills.list", "agents.list",
	}
	for _, method := range coreMethods {
		assert.True(t, ForwardNoLocalHandlerMethods[method], "方法 %q 应在 ForwardNoLocalHandlerMethods 中", method)
	}
}

// TestForwardNoLocalHandlerMethods_chat方法不在集合中 测试 chat 方法不在无本地 handler 集合中
func TestForwardNoLocalHandlerMethods_chat方法不在集合中(t *testing.T) {
	chatMethods := []string{"chat.send", "chat.interrupt", "chat.resume", "chat.user_answer"}
	for _, method := range chatMethods {
		assert.False(t, ForwardNoLocalHandlerMethods[method], "方法 %q 不应在 ForwardNoLocalHandlerMethods 中（有本地 handler）", method)
	}
}
