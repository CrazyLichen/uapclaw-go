package message_handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// newExternalA2AMessage 构造 a2a 入站消息（外部协议会话 id 放在 SessionID）。
func newExternalA2AMessage(id, externalSessionID, params string) *schema.Message {
	msg := schema.NewReqMessage("a2a", externalSessionID, schema.ReqMethodChatSend,
		json.RawMessage(params))
	msg.ID = id
	return msg
}

// sessionCreateReplies 假 AgentServer 的 session.create 回复（返回固定本地会话 id）。
func sessionCreateReplies(localSessionID string) map[string]fakeReply {
	return map[string]fakeReply{
		string(schema.ReqMethodSessionCreate): func(rid string, _ map[string]any) *schema.AgentResponse {
			return schema.NewAgentResponse(rid, "a2a",
				schema.WithPayload(map[string]any{"session_id": localSessionID}))
		},
	}
}

// TestResolveExternalChannelSession_非a2a直通 web 等通道不改写会话，也不写 metadata。
func TestResolveExternalChannelSession_非a2a直通(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("web", "sess-web", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	msg.ID = "m-web"

	mh.resolveExternalChannelSession(context.Background(), msg)

	assert.Equal(t, "sess-web", msg.SessionID, "非 a2a 通道不改写 session_id")
	assert.Nil(t, msg.Metadata, "非 a2a 通道不写 metadata")
}

// TestResolveExternalChannelSession_空外部会话 a2a 但 session_id 为空时不动。
func TestResolveExternalChannelSession_空外部会话(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("a2a", "", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	msg.ID = "m-empty"

	mh.resolveExternalChannelSession(context.Background(), msg)

	assert.Equal(t, "", msg.SessionID)
	assert.Nil(t, msg.Metadata)
}

// TestResolveExternalChannelSession_首轮分配并复用 同一外部会话只分配一次，后续轮复用同一本地会话。
func TestResolveExternalChannelSession_首轮分配并复用(t *testing.T) {
	f, mh := newFakeAgentServer(t, sessionCreateReplies("local-a2a-1"))

	first := newExternalA2AMessage("m1", "ext-1", `{"mode":"agent"}`)
	mh.resolveExternalChannelSession(context.Background(), first)
	assert.Equal(t, "local-a2a-1", first.SessionID, "首轮应换成本地会话")
	assert.Equal(t, "ext-1", first.Metadata["external_session_id"], "外部原始 id 留档")

	second := newExternalA2AMessage("m2", "ext-1", `{"mode":"agent"}`)
	mh.resolveExternalChannelSession(context.Background(), second)
	assert.Equal(t, "local-a2a-1", second.SessionID, "同一外部会话复用本地会话")
	assert.Equal(t, 1, f.methodCount(string(schema.ReqMethodSessionCreate)), "只应分配一次")

	other := newExternalA2AMessage("m3", "ext-2", `{"mode":"agent"}`)
	mh.resolveExternalChannelSession(context.Background(), other)
	assert.Equal(t, "local-a2a-1", other.SessionID)
	assert.Equal(t, 2, f.methodCount(string(schema.ReqMethodSessionCreate)), "不同外部会话各分配一次")
}

// TestResolveExternalChannelSession_按params_mode分配 外部消息的 params.mode 参与会话创建（team 走 is_swarm）。
func TestResolveExternalChannelSession_按params_mode分配(t *testing.T) {
	var createParams map[string]any
	replies := map[string]fakeReply{
		string(schema.ReqMethodSessionCreate): func(rid string, req map[string]any) *schema.AgentResponse {
			if raw, ok := req["params"].(map[string]any); ok {
				createParams = raw
			}
			return schema.NewAgentResponse(rid, "a2a",
				schema.WithPayload(map[string]any{"session_id": "local-team-1"}))
		},
	}
	f, mh := newFakeAgentServer(t, replies)

	msg := newExternalA2AMessage("m-team", "ext-team", `{"mode":"team"}`)
	mh.resolveExternalChannelSession(context.Background(), msg)

	assert.Equal(t, "local-team-1", msg.SessionID)
	require.Equal(t, 1, f.methodCount(string(schema.ReqMethodSessionCreate)))
	require.NotNil(t, createParams, "应收到 session.create 请求参数")
	// mode 先经 channelModeFromStr 归一（与 /mode 分发同源）：legacy "team" → team.work.normal
	assert.Equal(t, "team.work.normal", createParams["mode"], "mode 取自外部消息 params.mode 并归一")
	assert.Equal(t, true, createParams["is_swarm"], "team 模式应带 is_swarm")
}

// TestResolveExternalChannelSession_分配失败保留原会话 session.create 失败时不改写会话、不缓存别名。
func TestResolveExternalChannelSession_分配失败(t *testing.T) {
	replies := map[string]fakeReply{
		string(schema.ReqMethodSessionCreate): func(rid string, _ map[string]any) *schema.AgentResponse {
			return &schema.AgentResponse{
				RequestID: rid,
				ChannelID: "a2a",
				OK:        false,
				Payload:   map[string]any{"error": "boom"},
			}
		},
	}
	f, mh := newFakeAgentServer(t, replies)

	msg := newExternalA2AMessage("m-fail", "ext-fail", `{}`)
	mh.resolveExternalChannelSession(context.Background(), msg)

	assert.Equal(t, "ext-fail", msg.SessionID, "分配失败保留外部会话 id")
	assert.Nil(t, msg.Metadata, "分配失败不写 external_session_id")

	msg2 := newExternalA2AMessage("m-fail-2", "ext-fail", `{}`)
	mh.resolveExternalChannelSession(context.Background(), msg2)
	assert.Equal(t, 2, f.methodCount(string(schema.ReqMethodSessionCreate)),
		"失败不缓存，下一次仍会重试分配")
}

// TestResolveExternalChannelSession_保留已有metadata 外部 id 已在 metadata 时不覆盖。
func TestResolveExternalChannelSession_保留已有metadata(t *testing.T) {
	_, mh := newFakeAgentServer(t, sessionCreateReplies("local-x"))
	mh.externalSessionAliases[externalSessionAliasKey{channelID: "a2a", externalID: "ext-x"}] = "local-x"

	msg := newExternalA2AMessage("m-x", "ext-x", `{}`)
	msg.Metadata = map[string]any{"external_session_id": "custom-value"}
	mh.resolveExternalChannelSession(context.Background(), msg)

	assert.Equal(t, "custom-value", msg.Metadata["external_session_id"], "已有值不覆盖")
	assert.Equal(t, "local-x", msg.SessionID)
	assert.False(t, strings.Contains(msg.SessionID, "ext-"), "会话 id 已换成本地会话")
}
