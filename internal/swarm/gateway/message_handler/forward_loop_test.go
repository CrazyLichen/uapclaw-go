package message_handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestForwardLoop_消息处理 测试 forwardLoop 处理消息
func TestForwardLoop_消息处理(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动 forwardLoop
	go mh.forwardLoop(ctx)

	// 发送消息到 userMessages
	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"hello"}`),
		schema.WithIsStream(true),
	)

	select {
	case mh.userMessages <- msg:
	default:
		t.Fatal("userMessages channel 满了")
	}

	// 给 forwardLoop 一点时间处理
	time.Sleep(50 * time.Millisecond)
}

// TestHandleChatSend_ChatSend 测试 handleChatSend 处理 chat.send
func TestHandleChatSend_ChatSend(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"hello"}`),
		schema.WithIsStream(true),
	)
	agentMsg := mh.prepareAgentDispatchMessage(context.Background(), msg)
	mh.handleChatSend(context.Background(), msg, agentMsg)
}

// TestHandleChatCancel_取消 测试 handleChatCancel 处理 chat.cancel
func TestHandleChatCancel_取消(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatCancel,
		json.RawMessage(`{"intent":"cancel"}`),
	)
	mh.handleChatCancel(context.Background(), msg)
}

// TestHandleChatUserAnswer_回答 测试 handleChatUserAnswer 处理 chat.user_answer
func TestHandleChatUserAnswer_回答(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatAnswer,
		json.RawMessage(`{"request_id":"req-1","answer":"yes"}`),
	)
	mh.handleChatUserAnswer(context.Background(), msg)
}

// TestHandleChatSend_其他方法 测试 handleChatSend 处理其他方法
func TestHandleChatSend_其他方法(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodCommandAddDir,
		json.RawMessage(`{}`),
	)
	agentMsg := mh.prepareAgentDispatchMessage(context.Background(), msg)
	mh.handleChatSend(context.Background(), msg, agentMsg)
}

// TestMessageToE2A 测试 Message → E2A 转换
func TestMessageToE2A(t *testing.T) {
	_ = createTestMessageHandlerWithTransport() // 确保 transport 可用

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"hello"}`),
		schema.WithSessionID("sess-1"),
	)

	envelope := e2a.MessageToE2AOrFallback(msg)
	assert.NotNil(t, envelope)
	assert.Equal(t, "sess-1", envelope.SessionID)
}

// TestTriggerBeforeChatRequestHook 门控与 params 回写
// 对齐 Python message_handler.py L1856-1888 + L2502（步骤9）
func TestTriggerBeforeChatRequestHook(t *testing.T) {
	extensions.ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := extensions.CreateInstance(fw, map[string]any{}, nil)
	defer extensions.ResetInstance()
	reg.Register(extensions.GatewayBeforeChatRequest, func(ctx context.Context, data map[string]any) any {
		if p, ok := data["params"].(map[string]any); ok {
			p["query"] = "扩展改写后的query"
		}
		return nil
	}, 100)

	mh := createTestMessageHandlerWithTransport()

	// chat.send 触发 + 回写
	msg := schema.NewReqMessage("web", "s1", schema.ReqMethodChatSend, json.RawMessage(`{"query":"原始"}`))
	agentMsg := mh.prepareAgentDispatchMessage(context.Background(), msg)
	mh.triggerBeforeChatRequestHook(context.Background(), agentMsg)
	var p map[string]any
	_ = json.Unmarshal(agentMsg.Params, &p)
	if p["query"] != "扩展改写后的query" {
		t.Errorf("params.query = %v, 期望扩展改写后", p["query"])
	}

	// 非 chat 方法不触发（chat.cancel）
	msg2 := schema.NewReqMessage("web", "s1", schema.ReqMethodChatCancel, json.RawMessage(`{}`))
	agentMsg2 := mh.prepareAgentDispatchMessage(context.Background(), msg2)
	agentMsg2.Params = json.RawMessage(`{"query":"不应被改"}`)
	mh.triggerBeforeChatRequestHook(context.Background(), agentMsg2)
	var p2 map[string]any
	_ = json.Unmarshal(agentMsg2.Params, &p2)
	if p2["query"] != "不应被改" {
		t.Errorf("chat.cancel 不应触发: %v", p2["query"])
	}
}

// TestShouldTriggerBeforeChatRequestHook 测试门控方法
func TestShouldTriggerBeforeChatRequestHook(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	cases := []struct {
		method schema.ReqMethod
		want   bool
	}{
		{schema.ReqMethodChatSend, true},
		{schema.ReqMethodChatResume, true},
		{schema.ReqMethodChatAnswer, true},
		{schema.ReqMethodChatCancel, false},
		{schema.ReqMethodSessionList, false},
	}
	for _, c := range cases {
		msg := &schema.Message{ReqMethod: c.method}
		if got := mh.shouldTriggerBeforeChatRequestHook(msg); got != c.want {
			t.Errorf("shouldTrigger(%v) = %v, 期望 %v", c.method, got, c.want)
		}
	}
}
