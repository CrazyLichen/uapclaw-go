package message_handler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestSetOutboundPipeline 覆盖出处管线与两个 config 回调的注入。
func TestSetOutboundPipeline(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	called := ""
	mh.SetOutboundPipeline(nil,
		func() map[string]any { return map[string]any{} },
		func(channelID string, update map[string]any) { called = channelID },
	)
	require.NotNil(t, mh.getConfigRaw)
	require.NotNil(t, mh.updateChannelInConfig)

	mh.updateChannelInConfig("feishu_test", map[string]any{"default_mode": "code.normal"})
	assert.Equal(t, "feishu_test", called)
}

// TestGetChannelDefaultState_读配置 覆盖 config 存在的两条分支。
func TestGetChannelDefaultState_读配置(t *testing.T) {
	// 配置里显式指定 default_session_id
	mh := createTestMessageHandlerWithTransport()
	mh.SetOutboundPipeline(nil, func() map[string]any {
		return map[string]any{"channels": map[string]any{
			"feishu_test": map[string]any{
				"default_session_id": "sid-from-config",
				"default_mode":       "code.normal",
			},
		}}
	}, func(string, map[string]any) {})

	state := mh.getChannelDefaultState("feishu_test")
	assert.Equal(t, "sid-from-config", state.SessionID)
	assert.Equal(t, ChannelModeCodeNormal, state.Mode)

	// 配置里无 default_session_id → 生成一个
	state = mh.getChannelDefaultState("feishu_test")
	assert.NotEmpty(t, state.SessionID)

	// 渠道不在配置中 → 走兜底
	state = mh.getChannelDefaultState("wecom_other")
	assert.NotEmpty(t, state.SessionID)
	assert.Equal(t, ChannelModeAgentPlan, state.Mode)
}

// TestSaveChannelStateToConfig 覆盖回调缺失/状态缺失/正常写回三条分支。
func TestSaveChannelStateToConfig(t *testing.T) {
	// 回调缺失
	mh := createTestMessageHandlerWithTransport()
	mh.saveChannelStateToConfig("feishu_test")

	// 回调存在但状态缺失
	var gotUpdate map[string]any
	mh.SetOutboundPipeline(nil, func() map[string]any { return nil },
		func(channelID string, update map[string]any) { gotUpdate = update })
	mh.saveChannelStateToConfig("feishu_test")
	assert.Nil(t, gotUpdate)

	// 状态存在 → 写回 default_session_id / default_mode
	mh.statesMu.Lock()
	mh.channelStates["feishu_test"] = &ChannelControlState{SessionID: "sid-1", Mode: ChannelModeCodePlan}
	mh.statesMu.Unlock()
	mh.saveChannelStateToConfig("feishu_test")
	require.NotNil(t, gotUpdate)
	assert.Equal(t, "sid-1", gotUpdate["default_session_id"])
	assert.Equal(t, "code.plan", gotUpdate["default_mode"])
}

// TestChannelModeString_非法值 覆盖未知模式的兜底（对齐 ParseChannelMode 的 agent.plan 回落）。
func TestChannelModeString_非法值(t *testing.T) {
	assert.Equal(t, "agent.plan", ChannelModeString(ChannelMode(99)))
}

// TestGetGatewayHookHandlerForTest 覆盖 hook handler 只读访问器。
func TestGetGatewayHookHandlerForTest(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	assert.Nil(t, mh.GetGatewayHookHandlerForTest(), "未注入时返回 nil")
}

// TestHandleMessage_入队与溢出 覆盖正常入队与队列满丢弃两条分支。
func TestHandleMessage_入队与溢出(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"query":"hi"}`), schema.WithProvider("feishu"))

	mh.HandleMessage(nil) // nil 直接返回
	mh.HandleMessage(msg)

	got := mh.ConsumeUserMessages(1000000 * 1000)
	require.NotNil(t, got)
	assert.Equal(t, msg.ID, got.ID)

	// 队列填满后继续投递应走 default 分支（丢弃并告警）
	for i := 0; i < cap(mh.userMessages)+8; i++ {
		mh.HandleMessage(schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
			json.RawMessage(`{"query":"x"}`), schema.WithProvider("feishu")))
	}
}

// TestRememberUserQueryContext 覆盖 chat.send 记录/补充消息跳过/参数缺失。
func TestRememberUserQueryContext(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	// nil 与 非 chat.send 早退
	mh.rememberUserQueryContext(nil)
	mh.rememberUserQueryContext(schema.NewReqMessage("feishu_test", "s", schema.ReqMethodSessionList,
		json.RawMessage(`{"query":"x"}`)))

	// 空 sessionID 早退
	mh.rememberUserQueryContext(schema.NewReqMessage("feishu_test", "", schema.ReqMethodChatSend,
		json.RawMessage(`{"query":"x"}`)))

	// 空 params 早退
	mh.rememberUserQueryContext(schema.NewReqMessage("feishu_test", "s2", schema.ReqMethodChatSend, nil))

	// 非法 JSON 早退
	mh.rememberUserQueryContext(schema.NewReqMessage("feishu_test", "s3", schema.ReqMethodChatSend,
		json.RawMessage(`not-json`)))

	// is_supplement=true 跳过
	mh.rememberUserQueryContext(schema.NewReqMessage("feishu_test", "s4", schema.ReqMethodChatSend,
		json.RawMessage(`{"query":"x","is_supplement":true}`)))
	assert.Equal(t, "", mh.getSessionLastUserQuery("s4"))

	// 正常记录 query
	mh.rememberUserQueryContext(schema.NewReqMessage("feishu_test", "s5", schema.ReqMethodChatSend,
		json.RawMessage(`{"query":"记住我"}`)))
	assert.Equal(t, "记住我", mh.getSessionLastUserQuery("s5"))
}
