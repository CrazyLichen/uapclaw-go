package recommendation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 构造 chunk 辅助 ────────────────────────────

// newChunk 构造一个流式 chunk。
func newChunk(eventType, content string) *swarmschema.AgentResponseChunk {
	payload := map[string]any{"event_type": eventType}
	if content != "" {
		payload["content"] = content
	}
	return &swarmschema.AgentResponseChunk{
		RequestID: "req-1",
		Payload:   payload,
	}
}

// newFinalChunk 构造 chat.final chunk。
func newFinalChunk(content string) *swarmschema.AgentResponseChunk {
	return newChunk("chat.final", content)
}

// newDeltaChunk 构造 chat.delta chunk。
func newDeltaChunk(content string) *swarmschema.AgentResponseChunk {
	return newChunk("chat.delta", content)
}

// newToolCallChunk 构造 chat.tool_call chunk。
func newToolCallChunk() *swarmschema.AgentResponseChunk {
	return newChunk("chat.tool_call", "")
}

// newErrorChunk 构造 chat.error chunk。
func newErrorChunk(message string) *swarmschema.AgentResponseChunk {
	chunk := newChunk("chat.error", "")
	chunk.Payload["error"] = message
	return chunk
}

// newTriggerRequest 构造一个触发请求。
func newTriggerRequest(onDelivered func(string)) *ProactiveTriggerRequest {
	return &ProactiveTriggerRequest{
		SessionID: "sess-1",
		Decision: &RecommendationDecision{
			Type: "skill_recommend", Target: "翻译技能", Reason: "能翻文档",
		},
		Query:       "指令",
		RecID:       "rec_1_abcdef01",
		OnDelivered: onDelivered,
	}
}

// ──────────────────────────── TriggerMainAgentOnHost 边界 ────────────────────────────

// TestTriggerMainAgentOnHost_边界 nil 入参返回 false。
// Python: proactive_adapter.py:120
func TestTriggerMainAgentOnHost_边界(t *testing.T) {
	host := &mockMainAgentHost{}
	assert.False(t, TriggerMainAgentOnHost(context.Background(), nil, newTriggerRequest(nil)))
	assert.False(t, TriggerMainAgentOnHost(context.Background(), host, nil))
	assert.False(t, TriggerMainAgentOnHost(context.Background(), host, &ProactiveTriggerRequest{}),
		"decision 为 nil 时返回 false")
}

// TestTriggerMainAgentOnHost_无agent 用户尚未在该 channel 用过 → 跳过。
// Python: proactive_adapter.py:177-182
func TestTriggerMainAgentOnHost_无agent(t *testing.T) {
	host := &mockMainAgentHost{agent: nil}
	assert.False(t, TriggerMainAgentOnHost(context.Background(), host, newTriggerRequest(nil)))
}

// TestTriggerMainAgentOnHost_session忙跳过 避让检查生效。
// Python: proactive_adapter.py:187-193
func TestTriggerMainAgentOnHost_session忙(t *testing.T) {
	host := &mockMainAgentHost{
		agent:     &mockMainAgentHandle{},
		executing: true,
	}
	assert.False(t, TriggerMainAgentOnHost(context.Background(), host, newTriggerRequest(nil)))
}

// TestTriggerMainAgentOnHost_正常触发 返回 true 并后台跑完回调 onDelivered。
// Python: proactive_adapter.py:120
func TestTriggerMainAgentOnHost_正常触发(t *testing.T) {
	delivered := make(chan string, 1)
	host := &mockMainAgentHost{
		agent: &mockMainAgentHandle{chunks: []*swarmschema.AgentResponseChunk{
			newDeltaChunk("帮"),
			newDeltaChunk("你翻"),
			newFinalChunk("帮你翻文档"),
		}},
		pushResult: true,
	}

	triggered := TriggerMainAgentOnHost(context.Background(), host, newTriggerRequest(func(msg string) {
		delivered <- msg
	}))
	require.True(t, triggered)

	select {
	case msg := <-delivered:
		assert.Equal(t, "帮你翻文档", msg, "应回传 final 的完整话术")
	case <-time.After(2 * time.Second):
		t.Fatal("onDelivered 未被调用（超时）")
	}
}

// TestTriggerMainAgentOnHost_默认channel channel 为空时用 "web"。
func TestTriggerMainAgentOnHost_默认channel(t *testing.T) {
	host := &mockMainAgentHost{
		agent:      &mockMainAgentHandle{chunks: []*swarmschema.AgentResponseChunk{newFinalChunk("话术")}},
		pushResult: true,
	}
	request := newTriggerRequest(nil)
	request.ChannelID = ""

	require.True(t, TriggerMainAgentOnHost(context.Background(), host, request))
	// 后台 goroutine 异步推完，等 channel_id 落到 mock 上
	assert.Eventually(t, func() bool {
		return host.PushedChannel() == defaultAgentChannel
	}, 2*time.Second, 10*time.Millisecond, "应使用默认 channel")
}

// TestTriggerMainAgentOnHost_并发去重 同 channel+session 第二次被拒。
// Python: proactive_adapter.py:220-224
func TestTriggerMainAgentOnHost_并发去重(t *testing.T) {
	host := &mockMainAgentHost{
		agent: &mockMainAgentHandle{chunks: nil}, // 空流，立即结束
	}
	request := newTriggerRequest(nil)
	key := defaultAgentChannel + ":sess-1"

	// 手动占住在飞标记，模拟第一次仍在跑
	require.True(t, acquireInflightPush(key))
	defer releaseInflightPush(key)

	assert.False(t, TriggerMainAgentOnHost(context.Background(), host, request),
		"已有在飞推送时应拒绝")
}

// ──────────────────────────── chunk 打标与送达判定 ────────────────────────────

// TestPushProactiveChunks_source打标 话术轮 chunk 被注入 source/proactive_*。
// Python: proactive_adapter.py:313-325
func TestPushProactiveChunks_source打标(t *testing.T) {
	host := &mockMainAgentHost{pushResult: true}
	agent := &mockMainAgentHandle{chunks: []*swarmschema.AgentResponseChunk{
		newDeltaChunk("部分1"),
		newFinalChunk("完整话术"),
	}}
	agentRequest, err := buildProactiveAgentRequest(newTriggerRequest(nil), "web")
	require.NoError(t, err)

	pushProactiveChunks(context.Background(), host, agent, agentRequest, newTriggerRequest(nil), "web")

	require.NotEmpty(t, host.Pushed())
	for _, p := range host.Pushed() {
		payload, _ := p["payload"].(map[string]any)
		require.NotNil(t, payload)
		assert.Equal(t, proactiveSourceSource, payload["source"], "话术轮 chunk 应打 source 标记")
		assert.Equal(t, "skill_recommend", payload["proactive_type"])
		assert.Equal(t, "翻译技能", payload["proactive_target"])
		assert.Equal(t, "rec_1_abcdef01", payload["proactive_rec_id"])
	}
}

// TestPushProactiveChunks_卡片送达后停止透传 话术轮 final 之后的 chunk 不再推。
// Python: proactive_adapter.py:395-408
func TestPushProactiveChunks_卡片送达后停止透传(t *testing.T) {
	host := &mockMainAgentHost{pushResult: true}
	agent := &mockMainAgentHandle{chunks: []*swarmschema.AgentResponseChunk{
		newFinalChunk("推荐话术"),
		newFinalChunk("话术后的自我延续"),
		newDeltaChunk("延续增量"),
	}}
	agentRequest, _ := buildProactiveAgentRequest(newTriggerRequest(nil), "web")

	pushProactiveChunks(context.Background(), host, agent, agentRequest, newTriggerRequest(nil), "web")

	assert.Len(t, host.Pushed(), 1, "卡片交付后其后的 chunk 应被抑制")
	payload, _ := host.Pushed()[0]["payload"].(map[string]any)
	assert.Equal(t, "推荐话术", payload["content"])
}

// TestPushProactiveChunks_工具轮final被抑制 工具轮总结不抢占卡片锚点。
// Python: proactive_adapter.py:414-419
func TestPushProactiveChunks_工具轮final被抑制(t *testing.T) {
	host := &mockMainAgentHost{pushResult: true}
	agent := &mockMainAgentHandle{chunks: []*swarmschema.AgentResponseChunk{
		newToolCallChunk(),
		newFinalChunk("命令执行完毕…"), // 工具轮总结，应被抑制
		newFinalChunk("真正的推荐话术"),
	}}
	agentRequest, _ := buildProactiveAgentRequest(newTriggerRequest(nil), "web")

	var captured string
	request := newTriggerRequest(func(msg string) { captured = msg })

	pushProactiveChunks(context.Background(), host, agent, agentRequest, request, "web")

	// tool_call chunk 本身要透传（用户需看到工具执行），被抑制的只是工具轮总结 final
	pushed := host.Pushed()
	require.Len(t, pushed, 2, "tool_call 保留 + 话术 final；工具轮总结 final 被抑制")
	toolPayload, _ := pushed[0]["payload"].(map[string]any)
	assert.Equal(t, "chat.tool_call", toolPayload["event_type"])
	finalPayload, _ := pushed[1]["payload"].(map[string]any)
	assert.Equal(t, "真正的推荐话术", finalPayload["content"])
	assert.Equal(t, "真正的推荐话术", captured, "onDelivered 应收到话术而非工具总结")
}

// TestPushProactiveChunks_chatError不算送达 失败 chunk 翻转 delivered。
// Python: proactive_adapter.py:462-470
func TestPushProactiveChunks_chatError不算送达(t *testing.T) {
	host := &mockMainAgentHost{pushResult: true}
	agent := &mockMainAgentHandle{chunks: []*swarmschema.AgentResponseChunk{
		newErrorChunk("429 budget exceeded"),
	}}
	agentRequest, _ := buildProactiveAgentRequest(newTriggerRequest(nil), "web")

	called := false
	request := newTriggerRequest(func(string) { called = true })

	pushProactiveChunks(context.Background(), host, agent, agentRequest, request, "web")

	assert.False(t, called, "chat.error 不应回调 onDelivered")
	require.Len(t, host.Pushed(), 1, "错误 chunk 仍应透传前端（让用户看到报错）")
	payload, _ := host.Pushed()[0]["payload"].(map[string]any)
	_, hasSource := payload["source"]
	assert.False(t, hasSource, "错误 chunk 不应打 source 标记（否则被渲染成推荐卡片）")
}

// TestPushProactiveChunks_推送失败不算送达 SendPush 返回 false 时不回调。
// Python: proactive_adapter.py:438-446
func TestPushProactiveChunks_推送失败不算送达(t *testing.T) {
	host := &mockMainAgentHost{pushResult: false}
	agent := &mockMainAgentHandle{chunks: []*swarmschema.AgentResponseChunk{
		newFinalChunk("话术"),
	}}
	agentRequest, _ := buildProactiveAgentRequest(newTriggerRequest(nil), "web")

	called := false
	pushProactiveChunks(context.Background(), host, agent, agentRequest,
		newTriggerRequest(func(string) { called = true }), "web")

	assert.False(t, called, "ws 未送达时不应计数")
}

// TestPushProactiveChunks_无正文不算送达 空流不回调。
func TestPushProactiveChunks_无正文不算送达(t *testing.T) {
	host := &mockMainAgentHost{pushResult: true}
	agent := &mockMainAgentHandle{chunks: nil}
	agentRequest, _ := buildProactiveAgentRequest(newTriggerRequest(nil), "web")

	called := false
	pushProactiveChunks(context.Background(), host, agent, agentRequest,
		newTriggerRequest(func(string) { called = true }), "web")

	assert.False(t, called, "0 token 产出不应算送达")
}

// TestPushProactiveChunks_流启动失败 不 panic 也不回调。
func TestPushProactiveChunks_流启动失败(t *testing.T) {
	host := &mockMainAgentHost{pushResult: true}
	agent := &mockMainAgentHandle{err: assert.AnError}
	agentRequest, _ := buildProactiveAgentRequest(newTriggerRequest(nil), "web")

	called := false
	assert.NotPanics(t, func() {
		pushProactiveChunks(context.Background(), host, agent, agentRequest,
			newTriggerRequest(func(string) { called = true }), "web")
	})
	assert.False(t, called)
}

// TestPushProactiveChunks_只有delta也能送达 final 缺失时用 delta 拼接兜底。
// Python: proactive_adapter.py:451-453
func TestPushProactiveChunks_只有delta(t *testing.T) {
	host := &mockMainAgentHost{pushResult: true}
	agent := &mockMainAgentHandle{chunks: []*swarmschema.AgentResponseChunk{
		newDeltaChunk("增量1"),
		newDeltaChunk("增量2"),
	}}
	agentRequest, _ := buildProactiveAgentRequest(newTriggerRequest(nil), "web")

	var captured string
	pushProactiveChunks(context.Background(), host, agent, agentRequest,
		newTriggerRequest(func(msg string) { captured = msg }), "web")

	assert.Equal(t, "增量1增量2", captured, "无 final 时应用 delta 拼接兜底")
}

// TestPushProactiveChunks_已存在的source不被覆盖 setdefault 语义。
func TestPushProactiveChunks_已存在source(t *testing.T) {
	host := &mockMainAgentHost{pushResult: true}
	chunk := newFinalChunk("话术")
	chunk.Payload["source"] = "other_source"

	agent := &mockMainAgentHandle{chunks: []*swarmschema.AgentResponseChunk{chunk}}
	agentRequest, _ := buildProactiveAgentRequest(newTriggerRequest(nil), "web")

	pushProactiveChunks(context.Background(), host, agent, agentRequest, newTriggerRequest(nil), "web")

	require.Len(t, host.pushed, 1)
	payload, _ := host.Pushed()[0]["payload"].(map[string]any)
	assert.Equal(t, "other_source", payload["source"], "已存在的 source 不应被覆盖")
}

// TestPushProactiveChunks_onDelivered panic被捕获 不崩溃后台 goroutine。
func TestPushProactiveChunks_onDelivered_panic(t *testing.T) {
	host := &mockMainAgentHost{pushResult: true}
	agent := &mockMainAgentHandle{chunks: []*swarmschema.AgentResponseChunk{newFinalChunk("话术")}}
	agentRequest, _ := buildProactiveAgentRequest(newTriggerRequest(nil), "web")

	assert.NotPanics(t, func() {
		pushProactiveChunks(context.Background(), host, agent, agentRequest,
			newTriggerRequest(func(string) { panic("boom") }), "web")
	})
}

// ──────────────────────────── InitProactiveEngine ────────────────────────────

// TestInitProactiveEngine 装配后三个回调都已设置。
// Python: proactive_adapter.py:393
func TestInitProactiveEngine(t *testing.T) {
	host := &mockMainAgentHost{agent: &mockMainAgentHandle{}}
	engine := InitProactiveEngine(context.Background(), host, map[string]any{"enabled": true})

	require.NotNil(t, engine)
	engine.mu.RLock()
	defer engine.mu.RUnlock()
	assert.NotNil(t, engine.agentBuilder, "应注入 builder")
	assert.NotNil(t, engine.checkAgentAvailableCallback)
	assert.NotNil(t, engine.sendNotificationCallback)
	assert.NotNil(t, engine.triggerMainAgentCallback)
}

// TestInitProactiveEngine_通知回调 通知回调推 chat.final 带 notification source。
// Python: proactive_adapter.py:404-427
func TestInitProactiveEngine_通知回调(t *testing.T) {
	host := &mockMainAgentHost{pushResult: true}
	engine := InitProactiveEngine(context.Background(), host, map[string]any{"enabled": true})

	engine.mu.RLock()
	send := engine.sendNotificationCallback
	engine.mu.RUnlock()
	require.NotNil(t, send)

	require.NoError(t, send("web", "上限提醒"))
	require.Len(t, host.pushed, 1)
	payload, _ := host.Pushed()[0]["payload"].(map[string]any)
	assert.Equal(t, "上限提醒", payload["content"])
	assert.Equal(t, notificationSource, payload["source"])
	assert.Equal(t, "chat.final", payload["event_type"])
}

// TestInitProactiveEngine_检查回调 检查回调走 GetAgentNoWait。
func TestInitProactiveEngine_检查回调(t *testing.T) {
	host := &mockMainAgentHost{agent: &mockMainAgentHandle{}}
	engine := InitProactiveEngine(context.Background(), host, nil)

	engine.mu.RLock()
	check := engine.checkAgentAvailableCallback
	engine.mu.RUnlock()
	require.NotNil(t, check)
	assert.True(t, check("web"))

	// host 无 agent 时返回 false
	emptyHost := &mockMainAgentHost{}
	engine2 := InitProactiveEngine(context.Background(), emptyHost, nil)
	engine2.mu.RLock()
	check2 := engine2.checkAgentAvailableCallback
	engine2.mu.RUnlock()
	assert.False(t, check2("web"))
}

// TestInitProactiveEngine_nil_host 通知回调在 host 为 nil 时返回错误。
func TestInitProactiveEngine_nil_host(t *testing.T) {
	engine := InitProactiveEngine(context.Background(), nil, nil)
	require.NotNil(t, engine)

	engine.mu.RLock()
	check := engine.checkAgentAvailableCallback
	send := engine.sendNotificationCallback
	engine.mu.RUnlock()

	assert.False(t, check("web"))
	assert.Error(t, send("web", "msg"))
}

// ──────────────────────────── BuildProactiveAgent ────────────────────────────

// TestBuildProactiveAgent_无模型配置 无 default models 时返回 nil（不 panic）。
// Python: proactive_adapter.py:95-99
func TestBuildProactiveAgent_无模型配置(t *testing.T) {
	// 真实环境下可能无模型配置，函数必须安全返回
	assert.NotPanics(t, func() {
		agent := BuildProactiveAgent(context.Background())
		if agent != nil {
			_ = agent.Stop(context.Background())
		}
	})
}

// ──────────────────────────── 辅助 ────────────────────────────
