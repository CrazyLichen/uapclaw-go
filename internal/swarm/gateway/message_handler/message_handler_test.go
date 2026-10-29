package message_handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewMessageHandler_完整初始化 测试完整初始化
func TestNewMessageHandler_完整初始化(t *testing.T) {
	transport := transport.NewChannelTransport()
	agentClient := routing.NewAgentClient(transport)
	mh := NewMessageHandler(agentClient)

	assert.NotNil(t, mh)
	assert.NotNil(t, mh.userMessages)
	assert.NotNil(t, mh.robotMessages)
	assert.NotNil(t, mh.streamTasks)
	assert.NotNil(t, mh.channelStates)
}

// TestHandleMessage_正常入队 测试正常消息入队
func TestHandleMessage_正常入队(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend, json.RawMessage(`{}`))

	mh.HandleMessage(msg)
}

// TestHandleMessage_空消息 测试空消息
func TestHandleMessage_空消息(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.HandleMessage(nil)
}

// TestStartForwarding_启动停止 测试启动和停止
func TestStartForwarding_启动停止(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := mh.StartForwarding(ctx)
	assert.NoError(t, err)
	assert.True(t, mh.running.Load())

	err = mh.StopForwarding()
	assert.NoError(t, err)
	assert.False(t, mh.running.Load())
}

// TestStartForwarding_重复启动 测试重复启动
func TestStartForwarding_重复启动(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := mh.StartForwarding(ctx)
	assert.NoError(t, err)

	err = mh.StartForwarding(ctx)
	assert.NoError(t, err)

	_ = mh.StopForwarding()
}

// TestCancelAgentWorkForSession_空Session 测试空 sessionID
func TestCancelAgentWorkForSession_空Session(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("feishu_test", "", schema.ReqMethodChatCancel, json.RawMessage(`{}`))
	mh.CancelAgentWorkForSession(context.Background(), msg, "", true)
}

// TestCancelAgentWorkForSession_有流式任务 测试取消流式任务
// S-02 修复后：cancelStreamTask 的 Wait 由 streamFinalCleanup 无条件 Done
// 保证（注册即负责退出），无需测试侧"预 Done"workaround。
func TestCancelAgentWorkForSession_有流式任务(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	// 模拟真实 stream goroutine：select 等 ctx 取消后退出（defer Done）
	entry := &streamTaskEntry{}
	goroutineDone := make(chan struct{})
	entry.cancel = func() { close(goroutineDone) }
	entry.wg.Add(1)
	mh.registerStreamTask("req-1", "sess-1", nil, entry, true, "plan")
	go func() {
		defer entry.wg.Done()
		<-goroutineDone
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatCancel, json.RawMessage(`{}`))
	mh.CancelAgentWorkForSession(context.Background(), msg, "sess-1", true)

	mh.streamMu.RLock()
	_, exists := mh.streamTasks["req-1"]
	mh.streamMu.RUnlock()
	assert.False(t, exists, "任务应已被注销")
}

// TestSendProcessingStatus 测试发送 processing_status
func TestSendProcessingStatus(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	go func() {
		for range mh.robotMessages {
		}
	}()
	mh.SendProcessingStatus("sess-1", true)
	mh.SendProcessingStatus("sess-1", false)
}

// TestHandleChannelControl_非ChatSend 测试非 chat.send 方法
func TestHandleChannelControl_非ChatSend(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatResume, json.RawMessage(`{}`))
	handled := mh.handleChannelControl(msg)
	assert.False(t, handled, "非 chat.send 不应处理 slash 命令")
}

// TestHandleChannelControl_非受控渠道 测试非受控渠道
func TestHandleChannelControl_非受控渠道(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("web_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/new_session"}`))
	handled := mh.handleChannelControl(msg)
	assert.False(t, handled, "Web 渠道不在受控类型中")
}

// TestRegisterStreamTask 测试流式任务注册/注销
func TestRegisterStreamTask(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	metadata := map[string]any{"key": "value"}
	mh.registerStreamTask("req-1", "sess-1", metadata, &streamTaskEntry{cancel: func() {}}, true, "plan")

	sid := mh.getStreamSessionID("req-1")
	assert.Equal(t, "sess-1", sid)

	md := mh.getStreamMetadata("req-1")
	assert.Equal(t, "value", md["key"])

	mh.unregisterStreamTask("req-1")
	sid = mh.getStreamSessionID("req-1")
	assert.Equal(t, "", sid)
}

// TestPublishRobotMessages 测试出站消息入队
func TestPublishRobotMessages(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewEventMessage("web", "sess-1", schema.EventTypeChatDelta,
		map[string]any{"content": "hello"})
	go func() {
		<-mh.robotMessages
	}()
	mh.PublishRobotMessages(msg)
}

// TestCollectStreamTasksForSession 测试收集 session 的流式任务
func TestCollectStreamTasksForSession(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.registerStreamTask("req-1", "sess-1", nil, &streamTaskEntry{cancel: func() {}}, true, "plan")
	mh.registerStreamTask("req-2", "sess-1", nil, &streamTaskEntry{cancel: func() {}}, true, "plan")
	mh.registerStreamTask("req-3", "sess-2", nil, &streamTaskEntry{cancel: func() {}}, true, "plan")

	reqIDs := mh.collectStreamTasksForSession("sess-1")
	assert.Len(t, reqIDs, 2)

	reqIDs = mh.collectStreamTasksForSession("sess-2")
	assert.Len(t, reqIDs, 1)

	reqIDs = mh.collectStreamTasksForSession("sess-unknown")
	assert.Len(t, reqIDs, 0)
}

// TestHandleChatSend_无AgentClient 测试无 AgentClient 时处理
func TestHandleChatSend_无AgentClient(t *testing.T) {
	mh := createTestMessageHandler()
	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	agentMsg := mh.prepareAgentDispatchMessage(context.Background(), msg)
	mh.handleChatSend(context.Background(), msg, agentMsg)
}

// TestExtractTextFromParams 测试从 params 提取文本
func TestExtractTextFromParams(t *testing.T) {
	tests := []struct {
		name   string
		params json.RawMessage
		expect string
	}{
		{"空", nil, ""},
		{"空JSON", json.RawMessage(`{}`), ""},
		{"有content", json.RawMessage(`{"content":"/new_session"}`), "/new_session"},
		{"content非字符串", json.RawMessage(`{"content":123}`), ""},
		{"只有query", json.RawMessage(`{"query":"hello"}`), "hello"},
		{"query优先于content", json.RawMessage(`{"query":"from_query","content":"from_content"}`), "from_query"},
		{"query为空字符串时回退content", json.RawMessage(`{"query":"","content":"from_content"}`), "from_content"},
		{"query为非字符串时回退content", json.RawMessage(`{"query":123,"content":"from_content"}`), "from_content"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractTextFromParams(tc.params)
			assert.Equal(t, tc.expect, got)
		})
	}
}

// TestHandleAgentServerPush_CronPush 测试 cron push 通过 chunk payload 检测
func TestHandleAgentServerPush_CronPush(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	go func() {
		for range mh.robotMessages {
		}
	}()
	// cron 消息通过 chunk.Payload["event_type"] == "cron.response" 判断
	// 此测试确保 handleAgentServerPush 不会 panic
	mh.handleAgentServerPush(map[string]any{})
}

// TestE2AResponseToAgentRoundTrip 测试 E2A 转换往返一致性
func TestE2AResponseToAgentRoundTrip(t *testing.T) {
	e2aResp := e2a.NewE2AResponse()
	e2aResp.RequestID = "req-1"
	e2aResp.Channel = "web"
	e2aResp.SessionID = "sess-1"
	e2aResp.ResponseKind = e2a.E2AResponseKindE2AChunk
	e2aResp.Body = map[string]any{
		"event_type":  "chat.delta",
		"content":     "hello",
		"is_complete": false,
	}

	chunk, err := e2a.E2AResponseToAgentChunk(e2aResp)
	require.NoError(t, err)
	assert.Equal(t, "req-1", chunk.RequestID)
	assert.Equal(t, "web", chunk.ChannelID)
}

// TestConsumeRobotMessages_超时返回nil 测试空队列超时
func TestConsumeRobotMessages_超时返回nil(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := mh.ConsumeRobotMessages(10 * time.Millisecond)
	assert.Nil(t, msg)
}

// TestConsumeRobotMessages_正常消费 测试消费出站消息
func TestConsumeRobotMessages_正常消费(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	testMsg := schema.NewEventMessage("web", "sess-1", schema.EventTypeChatDelta, map[string]any{"content": "hello"})
	mh.PublishRobotMessages(testMsg)
	msg := mh.ConsumeRobotMessages(100 * time.Millisecond)
	assert.NotNil(t, msg)
}

// TestConsumeUserMessages_超时返回nil 测试空入站队列超时
func TestConsumeUserMessages_超时返回nil(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := mh.ConsumeUserMessages(10 * time.Millisecond)
	assert.Nil(t, msg)
}

// TestPublishUserMessagesNowait_正常写入 测试同步入站写入
func TestPublishUserMessagesNowait_正常写入(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	testMsg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	mh.PublishUserMessagesNowait(testMsg)
	msg := mh.ConsumeUserMessages(100 * time.Millisecond)
	assert.NotNil(t, msg)
}

// TestCancelAllStreamTasks 测试取消所有流式任务
func TestCancelAllStreamTasks(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	cancelled := false
	mh.registerStreamTask("req-1", "sess-1", nil, &streamTaskEntry{cancel: func() { cancelled = true }}, true, "plan")
	mh.registerStreamTask("req-2", "sess-2", nil, &streamTaskEntry{cancel: func() {}}, true, "plan")

	mh.cancelAllStreamTasks()

	assert.True(t, cancelled, "cancel 函数应被调用")
	assert.Empty(t, mh.streamTasks)
	assert.Empty(t, mh.streamSessions)
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// createTestMessageHandlerWithTransport 创建带 AgentClient 的测试 MessageHandler
func createTestMessageHandlerWithTransport() *MessageHandler {
	transport := transport.NewChannelTransport()
	agentClient := routing.NewAgentClient(transport)
	return NewMessageHandler(agentClient)
}

// TestNewMessageHandler_sessionMap装配 测试构造注入
func TestNewMessageHandler_sessionMap装配(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	// sessionMap 应在构造时装配（scope 未注册 config 时降级默认，不失败）
	assert.NotNil(t, mh.sessionMap, "sessionMap 应在构造时装配")
	// 未注册全局 config 时 hookHandler 为 nil（对齐 Python try/except 降级）
	assert.Nil(t, mh.gatewayHookHandler, "未注册全局 config 时 gatewayHookHandler 应为 nil")
}

// TestExtractIdentityTuple 四元组提取
func TestExtractIdentityTuple(t *testing.T) {
	// 全齐（provider 传 Python FeishuChannel.name="feishu"——与 channel_id 的
	// "feishu_enterprise:{app_id}" 是两个不同字段，各查各的）
	msg := schema.NewReqMessage("feishu_enterprise:app-1", "s",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu"),
		schema.WithChatID("oc-1"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))
	p, c, b, u, ok := extractIdentityTuple(msg)
	assert.True(t, ok)
	assert.Equal(t, "feishu", p)
	assert.Equal(t, "oc-1", c)
	assert.Equal(t, "bot-1", b)
	assert.Equal(t, "u-1", u)

	// 缺 user_id
	msg2 := schema.NewReqMessage("feishu_enterprise:app-1", "s",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu"),
		schema.WithChatID("oc-1"),
		schema.WithBotID("bot-1"))
	_, _, _, _, ok2 := extractIdentityTuple(msg2)
	assert.False(t, ok2, "缺任一字段应 ok=false")

	// 空白字符串按缺失处理（Python strip 语义）
	msg3 := schema.NewReqMessage("feishu_enterprise:app-1", "s",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("  "),
		schema.WithChatID("oc-1"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))
	_, _, _, _, ok3 := extractIdentityTuple(msg3)
	assert.False(t, ok3, "空白 provider 应按缺失处理")
}

// TestChannelIDMatchesSessionMapTypes 通道族匹配
func TestChannelIDMatchesSessionMapTypes(t *testing.T) {
	cases := []struct {
		cid  string
		want bool
	}{
		{"feishu_enterprise", true},
		{"feishu_enterprise:bot-2", true},
		{"feishu", false},
		{"web", false},
		{"", false},
		{"feishu_enterpriseX", false}, // 前缀必须带冒号
	}
	for _, c := range cases {
		if got := channelIDMatchesSessionMapTypes(c.cid); got != c.want {
			t.Errorf("channelIDMatchesSessionMapTypes(%q) = %v, want %v", c.cid, got, c.want)
		}
	}
}

// TestTriggerSessionStartHook_nilHandler 测试 nil handler 时静默跳过不 panic
func TestTriggerSessionStartHook_nilHandler(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	// gatewayHookHandler 为 nil（未注册全局 config）时不 panic
	assert.NotPanics(t, func() {
		mh.TriggerSessionStartHook("sess-1", "startup")
	})
}
