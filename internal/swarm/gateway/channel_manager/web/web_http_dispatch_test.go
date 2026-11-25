package web

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestTrustClientTenantHeaders 回环信任判定（web_http_dispatch.py:48-60）。
func TestTrustClientTenantHeaders(t *testing.T) {
	require.True(t, trustClientTenantHeaders("127.0.0.1"))
	require.True(t, trustClientTenantHeaders("::1"))
	require.True(t, trustClientTenantHeaders("localhost"))
	require.True(t, trustClientTenantHeaders(" 127.0.0.1 "))
	require.False(t, trustClientTenantHeaders("192.168.1.5"))
	require.False(t, trustClientTenantHeaders(""))

	// env 显式开关优先（:49-54）
	t.Setenv("GATEWAY_WEB_HTTP_TRUST_CLIENT_HEADERS", "1")
	require.True(t, trustClientTenantHeaders("10.0.0.9"))
	t.Setenv("GATEWAY_WEB_HTTP_TRUST_CLIENT_HEADERS", "0")
	require.False(t, trustClientTenantHeaders("127.0.0.1"))
	t.Setenv("GATEWAY_WEB_HTTP_TRUST_CLIENT_HEADERS", "bogus") // 非法值忽略，回回环判定
	require.True(t, trustClientTenantHeaders("localhost"))
}

// TestResolveHTTPRoutingIdentity 非回环 identity 置空、回环归一化（:82-94）。
func TestResolveHTTPRoutingIdentity(t *testing.T) {
	h := http.Header{}
	h.Set("X-User-Id", "u1")
	h.Set("X-Group-Id", "g1")
	h.Set("X-Bot-Id", "b1")
	require.Empty(t, resolveHTTPRoutingIdentity(h, "8.8.8.8"))
	got := resolveHTTPRoutingIdentity(h, "127.0.0.1")
	require.Equal(t, "u1", got["user_id"])
	require.Equal(t, "g1", got["group_id"])
	require.Equal(t, "b1", got["bot_id"])
}

// TestBuildHTTPExt 白名单 ext + query 身份注入 + 企业 X-User-Token 段不移植（D-B2-6）。
// 对齐 web_http_dispatch.py:63-79。
func TestBuildHTTPExt(t *testing.T) {
	t.Setenv("JIUWENSWARM_REQUEST_EXT_FORWARD_HEADERS", "X-Custom")
	requestextSetForwardHeaders(nil)
	ext := buildHTTPExt(map[string]string{"X-Custom": "v"}, map[string][]string{"user_id": {"u1"}})
	require.Equal(t, map[string]any{"X-Custom": "v"}, ext) // query 仅经白名单过滤（:63-79/:165-176）
}

// TestDispatchHTTPRequestMessage构造 走 fake 回调验证 Message 形状、
// outbound 注册/注销与路由身份落 metadata（dispatch.py:97-228）。
func TestDispatchHTTPRequestMessage构造(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Host: "127.0.0.1", Port: 19001, Path: "/ws"}, nil, nil)
	var captured *schema.Message
	wc.OnMessage(func(msg *schema.Message) bool {
		captured = msg
		return true
	})

	outbound := newHTTPJSONOutbound(map[string]string{}, "")
	headers := http.Header{}
	headers.Set("X-Request-Id", "rid-1")
	headers.Set("X-App-Id", "app1")
	headers.Set("X-User-Id", "u1")

	peer, reqID, sid := dispatchHTTPRequest(wc, &outbound.httpOutboundBase, "session.list",
		map[string]any{}, headers, "rid-1", false, false, true, "127.0.0.1")
	require.Equal(t, "rid-1", reqID)
	require.NotEmpty(t, sid)
	require.NotNil(t, peer)

	require.NotNil(t, captured)
	require.Equal(t, "rid-1", captured.ID)
	require.Equal(t, "web", captured.ChannelID)
	require.Equal(t, "app1", captured.AppID)
	require.Equal(t, "u1", captured.UserID)
	require.False(t, captured.IsStream)
	require.Equal(t, "u1", captured.Metadata["user_id"]) // 路由身份落 metadata（:185-195）
}

// TestDispatchHTTPRequestSessionCreate注入 create_token 补齐与 envelope 会话号。
func TestDispatchHTTPRequestSessionCreate注入(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Host: "127.0.0.1", Port: 19001, Path: "/ws"}, nil, nil)
	wc.OnMessage(func(msg *schema.Message) bool { return true })
	outbound := newHTTPJSONOutbound(nil, "")
	_, _, sid := dispatchHTTPRequest(wc, &outbound.httpOutboundBase, "session.create",
		map[string]any{}, http.Header{}, "r2", false, false, true, "127.0.0.1")
	require.NotEmpty(t, sid)
	require.NotEqual(t, "", sid)
	// envelope 为 webhttp_ 前缀随机号（bindHTTPSession :52）
	require.Contains(t, sid, "webhttp_")
}

// TestDispatchHTTPRequestTrust边界 非回环来源不落 user_id。
func TestDispatchHTTPRequestTrust边界(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Host: "127.0.0.1", Port: 19001, Path: "/ws"}, nil, nil)
	var captured *schema.Message
	wc.OnMessage(func(msg *schema.Message) bool {
		captured = msg
		return true
	})
	outbound := newHTTPJSONOutbound(nil, "")
	headers := http.Header{}
	headers.Set("X-User-Id", "u1")
	dispatchHTTPRequest(wc, &outbound.httpOutboundBase, "session.list", map[string]any{}, headers,
		"r3", false, false, true, "10.0.0.5")
	require.NotNil(t, captured)
	_, hasUser := captured.Metadata["user_id"]
	require.False(t, hasUser) // 非回环不信任租户头（:128-129）
}
