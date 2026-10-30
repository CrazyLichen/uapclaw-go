package message_handler

// 11.25 Task 6：ACP 会话别名链测试（⑦⑧）。
// Python: message_handler.py L1222-1331（判定/ensure/resolve/prepare）、
// L1643-1644（出站 resolve）。

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestIsSessionMapStyleSessionID :: 5/6 段全非空判定（Python L1222-1226）。
func TestIsSessionMapStyleSessionID(t *testing.T) {
	cases := []struct {
		sid  string
		want bool
	}{
		{"a::b::c::d::e", true},
		{"a::b::c::d::e::f", true},
		{"a::b::c::d", false},
		{"a::b::c::d::e::f::g", false},
		{"a::::c::d::e", false}, // 空段
		{"", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, isSessionMapStyleSessionID(c.sid), c.sid)
	}
}

// TestIsKnownJiuwenswarmSessionID 12 前缀 + :: 兜底（Python L1228-1235）。
func TestIsKnownJiuwenswarmSessionID(t *testing.T) {
	// 12 前缀逐个（含未实现渠道 feishu_ 等，偏差 §9-12 字面保留）
	for _, p := range knownJiuwenswarmSessionPrefixes {
		assert.True(t, isKnownJiuwenswarmSessionID(p+"xxx"), p)
	}
	assert.False(t, isKnownJiuwenswarmSessionID("unknown_prefix_1"))
	assert.True(t, isKnownJiuwenswarmSessionID("a::b::c::d::e"))
	assert.False(t, isKnownJiuwenswarmSessionID(""))
	assert.False(t, isKnownJiuwenswarmSessionID("   "))
}

// TestResolveAcpInternalSessionID_AliasCreate 无应答 ensure 失败 → ("", false) 放行。
// （design §4.4 拍板：ensure 失败不阻断消息转发；真实应答回写路径由 e2e Task 11 覆盖。）
func TestResolveAcpInternalSessionID_AliasCreate(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	external := "ide-session-1"

	internal, aliased := mh.resolveAcpInternalSessionID(external)
	// 无 AgentServer 应答时 ensure 失败 → ("", false) 放行
	assert.Empty(t, internal)
	assert.False(t, aliased)
}

// TestResolveAcpInternalSessionID_CacheHit 缓存命中直接返回（不触发 ensure）。
func TestResolveAcpInternalSessionID_CacheHit(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.acpSessionAliases["ext-ch"] = "int-ch"

	internal, aliased := mh.resolveAcpInternalSessionID("ext-ch")
	assert.Equal(t, "int-ch", internal)
	assert.True(t, aliased)

	// 同值映射 aliased=false
	mh.acpSessionAliases["self-ch"] = "self-ch"
	internal2, aliased2 := mh.resolveAcpInternalSessionID("self-ch")
	assert.Equal(t, "self-ch", internal2)
	assert.False(t, aliased2)
}

// TestResolveAcpInternalSessionID_KnownIdPassthrough 已知前缀 → desired=原样。
// （ensure 仍执行；空 client 失败路径同上。）
func TestResolveAcpInternalSessionID_KnownIdPassthrough(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	internal, _ := mh.resolveAcpInternalSessionID("sess_known_1")
	assert.Empty(t, internal)
}

// TestPrepareAgentDispatchMessage_NonAcpPassthrough 非 acp 渠道 / initialize / session.create 直通。
func TestPrepareAgentDispatchMessage_NonAcpPassthrough(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := &schema.Message{ID: "m1", Type: schema.MessageTypeReq,
		ChannelID: "web", SessionID: "s1", ReqMethod: schema.ReqMethodChatSend}
	got := mh.prepareAgentDispatchMessage(context.TODO(), msg)
	assert.Same(t, msg, got) // 非 acp：同一指针直通

	acpInit := &schema.Message{ID: "m2", Type: schema.MessageTypeReq,
		ChannelID: "acp", ReqMethod: schema.ReqMethodInitialize}
	assert.Same(t, acpInit, mh.prepareAgentDispatchMessage(context.TODO(), acpInit))

	acpCreate := &schema.Message{ID: "m3", Type: schema.MessageTypeReq,
		ChannelID: "acp", ReqMethod: schema.ReqMethodSessionCreate}
	assert.Same(t, acpCreate, mh.prepareAgentDispatchMessage(context.TODO(), acpCreate))
}

// TestPrepareAgentDispatchMessage_AcpRewrite 别名命中 → sessionID/params 改写 + metadata 记原值。
func TestPrepareAgentDispatchMessage_AcpRewrite(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	// 预灌别名缓存（绕过 ensure 网络依赖）
	mh.acpSessionAliases["ext-1"] = "acp_internal_1"

	msg := &schema.Message{ID: "m4", Type: schema.MessageTypeReq,
		ChannelID: "acp", SessionID: "ext-1",
		ReqMethod: schema.ReqMethodChatSend,
		Params:    []byte(`{"query":"hi"}`)}
	got := mh.prepareAgentDispatchMessage(context.TODO(), msg)

	assert.Equal(t, "acp_internal_1", got.SessionID)
	var p map[string]any
	assert.NoError(t, json.Unmarshal(got.Params, &p))
	assert.Equal(t, "acp_internal_1", p["session_id"])
	assert.Equal(t, "hi", p["query"])
	// aliased=true → metadata 记 acp_original_session_id
	assert.Equal(t, "ext-1", got.Metadata["acp_original_session_id"])
	// 原 msg 未被改写（replace 语义）
	assert.Equal(t, "ext-1", msg.SessionID)
}

// TestPrepareAgentDispatchMessage_AliasNoRewriteWhenSame 已知会话 desired=原样 → 无 metadata。
func TestPrepareAgentDispatchMessage_AliasNoRewriteWhenSame(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.acpSessionAliases["sess_same_1"] = "sess_same_1" // aliased=false

	msg := &schema.Message{ID: "m5", Type: schema.MessageTypeReq,
		ChannelID: "acp", SessionID: "sess_same_1",
		ReqMethod: schema.ReqMethodChatSend,
		Params:    []byte(`{}`)}
	got := mh.prepareAgentDispatchMessage(context.TODO(), msg)
	assert.Equal(t, "sess_same_1", got.SessionID)
	assert.Nil(t, got.Metadata["acp_original_session_id"])
}

// TestResolveAcpExternalSessionID 三级回落（Python L1313-1331）。
func TestResolveAcpExternalSessionID(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	// 1. metadata 键优先
	assert.Equal(t, "ext-meta",
		mh.resolveAcpExternalSessionID("int-1", map[string]any{"acp_original_session_id": "ext-meta"}))
	// 2. 别名反查
	mh.acpSessionAliases["ext-2"] = "int-2"
	assert.Equal(t, "ext-2", mh.resolveAcpExternalSessionID("int-2", nil))
	// 3. 原样返回
	assert.Equal(t, "int-3", mh.resolveAcpExternalSessionID("int-3", nil))
	// 空入参 → ""
	assert.Empty(t, mh.resolveAcpExternalSessionID("", nil))
	assert.Empty(t, mh.resolveAcpExternalSessionID("  ", nil))
}
