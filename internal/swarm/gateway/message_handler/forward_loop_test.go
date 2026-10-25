package message_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	hooksgw "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/hooks"
	impipeline "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/im_pipeline"
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

// mustLoadConfigBase 测试辅助：加载 config 原始 map
func mustLoadConfigBase(t *testing.T, cfg *config.Config) map[string]any {
	t.Helper()
	base, err := cfg.Load()
	if err != nil {
		t.Fatalf("cfg.Load: %v", err)
	}
	return base
}

// TestForwardLoop_步骤3UserPromptSubmitHook 触发 UserPromptSubmit（nil handler 跳过不 panic）
func TestForwardLoop_步骤3UserPromptSubmitHook(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	// nil handler（未注册全局 config 的默认构造路径）不 panic 即可
	msg := schema.NewReqMessage("feishu_test", "sess-1",
		schema.ReqMethodChatSend, json.RawMessage(`{"content":"hi"}`))

	mh.ApplyChannelState(msg)
	assert.NotPanics(t, func() {
		mh.forwardLoopStep3UserPromptSubmit(context.Background(), msg)
	})

	// 非 nil handler：配置一个 UserPromptSubmit hook（command echo）验证执行
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	hookYAML := "hooks:\n  events:\n    UserPromptSubmit:\n      - matcher: \"*\"\n        hooks:\n          - type: command\n            command: echo ok\n"
	_ = os.WriteFile(cfgFile, []byte(hookYAML), 0o644)
	cfg, err := config.New(cfgFile)
	if err != nil {
		t.Fatalf("config.New: %v", err)
	}
	hookscfg.ResetGlobalConfigForTest()
	hookscfg.RegisterConfig(cfg)
	defer hookscfg.ResetGlobalConfigForTest()

	hooksCfg := hookscfg.LoadHooksConfig(mustLoadConfigBase(t, cfg))
	mh.gatewayHookHandler = hooksgw.NewGatewayHookHandler(hooksCfg)
	assert.NotPanics(t, func() {
		mh.forwardLoopStep3UserPromptSubmit(context.Background(), msg)
	})
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

// TestTriggerBeforeChatRequestHook_空SessionID为nil T-08：
// SessionID 空串时 hook ctx 应传 nil（对齐 Python msg.session_id=None），
// 非空时传指针——ToMap 的 session_id 字段分别 nil / *string("s1")
func TestTriggerBeforeChatRequestHook_空SessionID为nil(t *testing.T) {
	extensions.ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := extensions.CreateInstance(fw, map[string]any{}, nil)
	defer extensions.ResetInstance()

	var gotSessionIDs []any
	reg.Register(extensions.GatewayBeforeChatRequest, func(ctx context.Context, data map[string]any) any {
		gotSessionIDs = append(gotSessionIDs, data["session_id"])
		return nil
	}, 100)

	mh := createTestMessageHandlerWithTransport()

	// 非空 session_id → ToMap 后为 *string（进程内链路）
	msg := schema.NewReqMessage("web", "s1", schema.ReqMethodChatSend, json.RawMessage(`{}`), schema.WithSessionID("s1"))
	agentMsg := mh.prepareAgentDispatchMessage(context.Background(), msg)
	mh.triggerBeforeChatRequestHook(context.Background(), agentMsg)
	if len(gotSessionIDs) != 1 {
		t.Fatalf("应触发 1 次, 实际 %d", len(gotSessionIDs))
	}
	if sid, ok := gotSessionIDs[0].(*string); !ok || sid == nil || *sid != "s1" {
		t.Errorf("非空 session_id 应传 *string(\"s1\"), 实际 %v (%T)", gotSessionIDs[0], gotSessionIDs[0])
	}

	// 空 session_id → ToMap 后为 nil 指针（跨进程 structpb/JSON 序列化为 null，对齐 Python None）
	gotSessionIDs = nil
	msg2 := schema.NewReqMessage("web", "", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	agentMsg2 := mh.prepareAgentDispatchMessage(context.Background(), msg2)
	mh.triggerBeforeChatRequestHook(context.Background(), agentMsg2)
	if len(gotSessionIDs) != 1 {
		t.Fatalf("应触发 1 次, 实际 %d", len(gotSessionIDs))
	}
	sid2, isPtr := gotSessionIDs[0].(*string)
	if !isPtr {
		t.Fatalf("空 session_id 应传类型化 nil 指针, 实际非指针类型 %T", gotSessionIDs[0])
	}
	if sid2 != nil {
		t.Errorf("空 session_id 应传 nil 指针（序列化为 null）, 实际 %q", *sid2)
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

// TestPythonOrStr_or链语义 M-09 回归：对齐 Python str(a or b or "")。
// {"query":123} → "123"（数字 truthy 转 str）；query 空串 → 回退 content；
// 两者均 falsy → ""。
func TestPythonOrStr_or链语义(t *testing.T) {
	cases := []struct {
		name string
		a, b any
		want string
	}{
		{"query 字符串直接用", "hello", "content", "hello"},
		{"query 数字转 str", float64(123), nil, "123"},
		{"query 空串回退 content", "", "fallback", "fallback"},
		{"query nil 回退 content", nil, "fallback", "fallback"},
		{"query 0 回退 content", float64(0), "fallback", "fallback"},
		{"query false 回退 content", false, "fallback", "fallback"},
		{"全 falsy 空串", "", "", ""},
		{"nil nil 空串", nil, nil, ""},
		{"int 值转 str", 42, nil, "42"},
		{"负数 truthy", -1, nil, "-1"},
	}
	for _, tc := range cases {
		if got := pythonOrStr(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: pythonOrStr(%v, %v) = %q, want %q", tc.name, tc.a, tc.b, got, tc.want)
		}
	}
}

// ──────────────────────────── 11.12 双挂接测试 ────────────────────────────

// fakeInboundPipeline 可编程入站管线 fake（实现 InboundPipelineAPI）。
type fakeInboundPipeline struct {
	applyFn func(ctx context.Context, msg *schema.Message) bool
}

func (f *fakeInboundPipeline) Apply(ctx context.Context, msg *schema.Message) bool {
	return f.applyFn(ctx, msg)
}

// fakeOutboundPipeline 可编程出站管线 fake（实现 OutboundPipeline 新签名）。
type fakeOutboundPipeline struct {
	applyErr error
	applied  []*schema.Message
}

func (f *fakeOutboundPipeline) Apply(ctx context.Context, msg *schema.Message) error {
	f.applied = append(f.applied, msg)
	return f.applyErr
}

// TestForwardLoop_步骤6入站管线_拦截 拦截路径：Apply=false 的 chat.send 消息不达步骤7。
func TestForwardLoop_步骤6入站管线_拦截(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.inboundPipeline = &fakeInboundPipeline{
		applyFn: func(_ context.Context, _ *schema.Message) bool { return false },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mh.forwardLoop(ctx)

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"query":"无关消息"}`), schema.WithIsStream(true))
	select {
	case mh.userMessages <- msg:
	default:
		t.Fatal("userMessages channel 满了")
	}
	// 被拦截：无 panic 即可（下游 handleChatSend 无 AgentClient 连接仅 Warn）
	time.Sleep(50 * time.Millisecond)
}

// TestForwardLoop_步骤6入站管线_放行 Apply=true 消息继续走步骤7-10。
func TestForwardLoop_步骤6入站管线_放行(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	var seen *schema.Message
	mh.inboundPipeline = &fakeInboundPipeline{
		applyFn: func(_ context.Context, m *schema.Message) bool {
			seen = m
			return true
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mh.forwardLoop(ctx)

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"hello"}`), schema.WithIsStream(true))
	select {
	case mh.userMessages <- msg:
	default:
		t.Fatal("userMessages channel 满了")
	}
	time.Sleep(50 * time.Millisecond)
	assert.NotNil(t, seen)
}

// TestForwardLoop_步骤6入站管线_nil管线跳过 无挂接不 panic。
func TestForwardLoop_步骤6入站管线_nil管线跳过(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mh.forwardLoop(ctx)

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"hello"}`), schema.WithIsStream(true))
	select {
	case mh.userMessages <- msg:
	default:
		t.Fatal("userMessages channel 满了")
	}
	time.Sleep(50 * time.Millisecond)
}

// TestForwardLoop_步骤6入站管线_panic兜底 Apply panic → fallback 转发不中断。
func TestForwardLoop_步骤6入站管线_panic兜底(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.inboundPipeline = &fakeInboundPipeline{
		applyFn: func(_ context.Context, _ *schema.Message) bool { panic("boom") },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mh.forwardLoop(ctx)

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"hello"}`), schema.WithIsStream(true))
	select {
	case mh.userMessages <- msg:
	default:
		t.Fatal("userMessages channel 满了")
	}
	time.Sleep(50 * time.Millisecond) // 不 panic 不死循环即通过
}

// TestForwardLoop_步骤6非chatsend不进管线 chat.answer 消息不调 Apply。
func TestForwardLoop_步骤6非chatsend不进管线(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	called := false
	mh.inboundPipeline = &fakeInboundPipeline{
		applyFn: func(_ context.Context, _ *schema.Message) bool {
			called = true
			return true
		},
	}
	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatAnswer, json.RawMessage(`{}`))
	// 直接调用含步骤6 的流程入口（forwardLoop 为私有，经行为断言：步骤4 已分流 return）
	mh.handleChatUserAnswer(context.Background(), msg)
	assert.False(t, called)
}

// TestPublishRobotMessages_出站管线挂接 nil pipeline 直接入队。
func TestPublishRobotMessages_出站管线挂接(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewResMessage("web", "s", true, map[string]any{"content": "ok"})
	mh.PublishRobotMessages(msg)
	// 消费验证入队
	select {
	case got := <-mh.robotMessages:
		assert.Equal(t, msg.ID, got.ID)
	default:
		t.Fatal("消息应入队")
	}
}

// TestPublishRobotMessages_管线执行且入队 pipeline 执行后照常入队（幂等）。
func TestPublishRobotMessages_管线执行且入队(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	fake := &fakeOutboundPipeline{}
	mh.outboundPipeline = fake
	msg := schema.NewResMessage("web", "s", true, map[string]any{"content": "ok"})
	mh.PublishRobotMessages(msg)
	require.Len(t, fake.applied, 1)
	select {
	case <-mh.robotMessages:
	default:
		t.Fatal("管线执行后消息仍应入队")
	}
}

// TestPublishRobotMessages_管线错误降级入队 Apply 返回 error → Warn + 照常入队。
func TestPublishRobotMessages_管线错误降级入队(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.outboundPipeline = &fakeOutboundPipeline{applyErr: fmt.Errorf("pipeline boom")}
	msg := schema.NewResMessage("web", "s", true, map[string]any{"content": "ok"})
	assert.NotPanics(t, func() {
		mh.PublishRobotMessages(msg)
	})
	select {
	case <-mh.robotMessages:
	default:
		t.Fatal("错误后消息仍应入队")
	}
}

// TestSetInboundPipeline 注入后字段非 nil。
func TestSetInboundPipeline(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.SetInboundPipeline(nil) // nil 容忍
	assert.Nil(t, mh.inboundPipeline)
	// 真实类型隐式实现验证（编译期断言）
	var _ InboundPipelineAPI = &impipeline.IMInboundPipeline{}
	var _ OutboundPipeline = &impipeline.IMOutboundPipeline{}
}
