package web

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestBindHTTPSession 对齐 outbound.py:32-70 四分支。
func TestBindHTTPSession(t *testing.T) {
	// session.create：剔除 params.session_id 并自动补 create_token（:48-52）
	env, out := bindHTTPSession("session.create", map[string]any{}, "", true)
	require.NotEmpty(t, env)
	require.NotEmpty(t, out["create_token"])

	env2, out2 := bindHTTPSession("cron.job.list", map[string]any{"session_id": "s1"}, "XSID", true)
	require.Equal(t, "s1", env2) // client_sid 优先于 header（:47）
	require.Equal(t, "s1", out2["session_id"])

	// bind_param=false 且 params/header 均无会话号 → envelope 生成但不注入 params（:60-65）
	env3, out3 := bindHTTPSession("config.get", map[string]any{}, "", false)
	require.NotEmpty(t, env3)
	_, has := out3["session_id"]
	require.False(t, has)

	// bind_param=false 但 header 有会话号（params 缺失计入 client_sid，:47）→ 注入
	_, out3b := bindHTTPSession("config.get", map[string]any{}, "XSID", false)
	require.Equal(t, "XSID", out3b["session_id"])

	// bind_param=false 且有 client_sid → 保留在 params（:61-62）
	_, out4 := bindHTTPSession("skills.list", map[string]any{"session_id": "s9"}, "", false)
	require.Equal(t, "s9", out4["session_id"])
}

// TestBindHTTPSessionSharedUserRoute agent_templates.*/无绑定方法按用户共用路由会话。
// Python: outbound.py:21-29 _shared_user_route_session_id + :53-58。
func TestBindHTTPSessionSharedUserRoute(t *testing.T) {
	env, _ := bindHTTPSession("agent_templates.list", map[string]any{"user_id": "u-1"}, "", true)
	require.Equal(t, "shared_user_u-1", env)

	env2, _ := bindHTTPSession("config.get", map[string]any{"user_id": "u.2"}, "", false)
	require.Equal(t, "shared_user_u_2", env2) // 非法字符归一为 _

	env3, _ := bindHTTPSession("config.get", map[string]any{}, "", false)
	require.Equal(t, "shared_user_default", env3)
}

// TestHttpJSONOutboundWaitResponse res 帧捕获。
func TestHttpJSONOutboundWaitResponse(t *testing.T) {
	ob := newHTTPJSONOutbound(nil, "")
	go ob.acceptFrame(map[string]any{"type": "res", "id": "r1", "ok": true,
		"payload": map[string]any{"x": 1}})
	frame := ob.waitResponse("r1", 1*time.Second)
	require.True(t, frame["ok"].(bool))
	require.Equal(t, "r1", frame["id"])
}

// TestHttpJSONOutboundChatError不打空等 单请求被收成 chat.error 事件直接转失败 res。
// Python: outbound.py:257-269。
func TestHttpJSONOutboundChatError不打空等(t *testing.T) {
	ob := newHTTPJSONOutbound(nil, "")
	go ob.acceptFrame(map[string]any{"type": "event", "event": "chat.error",
		"payload": map[string]any{"request_id": "r1", "error": "boom", "code": "BAD_REQUEST"}})
	frame := ob.waitResponse("r1", 1*time.Second)
	require.False(t, frame["ok"].(bool))
	require.Equal(t, "BAD_REQUEST", frame["code"])
	require.Equal(t, "boom", frame["error"])
}

// TestHttpJSONOutboundTimeout 超时帧形状。
// Python: outbound.py:227-245。
func TestHttpJSONOutboundTimeout(t *testing.T) {
	ob := newHTTPJSONOutbound(nil, "")
	frame := ob.waitResponse("r1", 30*time.Millisecond)
	require.Equal(t, "TIMEOUT", frame["code"])
	require.Equal(t, false, frame["ok"])
}

// TestHttpJSONOutboundCloseAfterClose 闭后 accept 静默丢弃、wait 收到 close 哨兵。
// Python: outbound.py:208-214 + :246-254（frame None → connection closed）。
func TestHttpJSONOutboundCloseAfterClose(t *testing.T) {
	ob := newHTTPJSONOutbound(nil, "")
	ob.close()
	ob.acceptFrame(map[string]any{"type": "res", "id": "r1", "ok": true, "payload": map[string]any{}})
	frame := ob.waitResponse("r1", 100*time.Millisecond)
	require.Equal(t, "INTERNAL_ERROR", frame["code"]) // connection closed
}

// TestIsSSEEndFrame个人版语义 chat.final 终止、processing_status(false) 不终止。
// Python: outbound.py:73-93。
func TestIsSSEEndFrame个人版语义(t *testing.T) {
	require.True(t, isSSEEndFrame(map[string]any{"event": "chat.final"}, ""))
	require.False(t, isSSEEndFrame(map[string]any{"event": "chat.processing_status",
		"payload": map[string]any{"is_processing": false}}, ""))
	require.True(t, isSSEEndFrame(map[string]any{"event": "chat.error"}, ""))
	require.True(t, isSSEEndFrame(map[string]any{"event": "history.message",
		"payload": map[string]any{"status": "done"}}, ""))
	require.False(t, isSSEEndFrame(map[string]any{"event": "chat.delta"}, ""))
}

// TestHttpSSEOutboundIterSSEFrames终止与keepalive 事件转发、res !ok 终止、keepalive 注释。
// Python: outbound.py:299-366。
func TestHttpSSEOutboundIterSSEFrames终止与keepalive(t *testing.T) {
	ob := newHTTPSSEOutbound(nil, "")
	ob.acceptFrame(map[string]any{"type": "res", "id": "r1", "ok": true, "payload": map[string]any{}})
	ob.acceptFrame(map[string]any{"type": "event", "event": "chat.delta",
		"payload": map[string]any{"text": "hi"}})
	ob.acceptFrame(map[string]any{"type": "res", "id": "r1", "ok": false,
		"error": "fail", "code": "BAD_REQUEST", "payload": map[string]any{}})

	var frames []map[string]any
	for frame := range ob.iterSSEFrames("r1", 0, 0, 30*time.Second) {
		frames = append(frames, frame)
	}
	require.Len(t, frames, 3) // res(accepted) + event + res(!ok) 终止
	require.Equal(t, "res", frames[0]["type"])
	require.Equal(t, "chat.delta", frames[1]["event"])
	require.Equal(t, false, frames[2]["ok"])
}

// TestHttpSSEOutboundIdleTimeout 空闲超窗产 chat.error 流帧。
// Python: outbound.py:327-333。
func TestHttpSSEOutboundIdleTimeout(t *testing.T) {
	ob := newHTTPSSEOutbound(nil, "")
	var frames []map[string]any
	for frame := range ob.iterSSEFrames("r1", 0, 50*time.Millisecond, 30*time.Second) {
		frames = append(frames, frame)
	}
	require.Len(t, frames, 1)
	require.Equal(t, "chat.error", frames[0]["event"])
	require.Contains(t, frames[0]["payload"].(map[string]any)["error"], "idle timeout")
}
