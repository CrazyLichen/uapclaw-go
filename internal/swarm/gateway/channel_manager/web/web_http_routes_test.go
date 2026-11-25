package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestEnvelopeFromRes错误码映射 全分支表驱动（web_http_routes.py:197-208）。
func TestEnvelopeFromRes错误码映射(t *testing.T) {
	cases := map[string]int{
		"BAD_REQUEST": 400, "UNAUTHORIZED": 401, "FORBIDDEN": 403,
		"NOT_FOUND": 404, "METHOD_NOT_FOUND": 404, "CONFLICT": 409,
		"TIMEOUT": 504, "SERVICE_UNAVAILABLE": 503,
		"WORKSPACE_QUOTA_EXCEEDED": 403,
		"SOMETHING_ELSE":           500,
	}
	for code, status := range cases {
		body, got := envelopeFromRes(map[string]any{"ok": false, "code": code,
			"error": "msg", "payload": map[string]any{}}, "r1", "m")
		require.Equal(t, status, got, code)
		require.False(t, body["ok"].(bool))
		err := body["error"].(map[string]any)
		require.Equal(t, code, err["code"])
		require.Equal(t, "msg", err["message"])
	}

	// ok → 200；created 201 由调用方改写
	body, status := envelopeFromRes(map[string]any{"ok": true, "payload": map[string]any{"x": 1}}, "r1", "m")
	require.Equal(t, 200, status)
	require.Equal(t, map[string]any{"x": 1}, body["data"])
	require.Equal(t, "m", body["metadata"].(map[string]any)["rpc_method"])
	require.Equal(t, "web-http", body["metadata"].(map[string]any)["transport"])

	// payload 为 nil → data 空 dict（:186）
	body2, _ := envelopeFromRes(map[string]any{"ok": true}, "r1", "")
	require.Equal(t, map[string]any{}, body2["data"])
}

// TestChatIdentityConflict 四字段 × 组合（web_http_routes.py:640-666）。
func TestChatIdentityConflict(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-user-id", "u1")
	params := map[string]any{"user_id": "u2"}
	body, status, conflict := chatIdentityError("chat.send", headers, params, "r1")
	require.True(t, conflict)
	require.Equal(t, 400, status)
	require.Equal(t, "IDENTITY_CONFLICT", body["error"].(map[string]any)["code"])
	require.Equal(t, "user_id", body["error"].(map[string]any)["details"].(map[string]any)["field"])

	// body 空 → unset 放行
	_, _, conflict2 := chatIdentityError("chat.send", headers, map[string]any{}, "r1")
	require.False(t, conflict2)

	// 相等 → 放行
	_, _, conflict3 := chatIdentityError("chat.send", headers, map[string]any{"user_id": "u1"}, "r1")
	require.False(t, conflict3)

	// 非 chat 三方法 → 不查
	_, _, conflict4 := chatIdentityError("session.list", headers, map[string]any{"user_id": "u2"}, "r1")
	require.False(t, conflict4)
}

// TestCoerceQueryValue 对齐 web_http_routes.py:823-837。
func TestCoerceQueryValue(t *testing.T) {
	require.Equal(t, true, coerceQueryValue("true"))
	require.Equal(t, true, coerceQueryValue("yes"))
	require.Equal(t, false, coerceQueryValue("false"))
	require.Equal(t, false, coerceQueryValue("no"))
	require.Equal(t, 1, coerceQueryValue("1")) // "1" 不截胡成 true
	require.Equal(t, -2, coerceQueryValue("-2"))
	require.Equal(t, "abc", coerceQueryValue("abc"))
	require.Equal(t, "1x", coerceQueryValue("1x"))
}

// TestHTTPSessionCRUD全链 chi + fake 回调的 HTTP 全链（T5 集成面）。
func TestHTTPSessionCRUD全链(t *testing.T) {
	state, router := newTestRouterWithWebChannel()

	// POST /api/v1/sessions（body 可空 → 201）
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 201, rec.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.True(t, created["ok"].(bool))

	// session.create 的 params 已注入 create_token + envelope session_id
	require.NotEmpty(t, state.sessionCreateParams["create_token"])
	require.NotEmpty(t, state.lastForward.SessionID)
	require.Contains(t, state.lastForward.SessionID, "webhttp_")

	// GET /api/v1/sessions/{id}
	req2 := httptest.NewRequest("GET", "/api/v1/sessions/web_1", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, 200, rec2.Code)

	// PATCH pinned → session.pin；否则 rename
	req3 := httptest.NewRequest("PATCH", "/api/v1/sessions/web_1",
		bytes.NewReader([]byte(`{"pinned": true}`)))
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	require.Equal(t, "session.pin", state.lastMethod)
	req3b := httptest.NewRequest("PATCH", "/api/v1/sessions/web_1",
		bytes.NewReader([]byte(`{"title": "t"}`)))
	rec3b := httptest.NewRecorder()
	router.ServeHTTP(rec3b, req3b)
	require.Equal(t, "session.rename", state.lastMethod)

	// DELETE
	req4 := httptest.NewRequest("DELETE", "/api/v1/sessions/web_1", nil)
	rec4 := httptest.NewRecorder()
	router.ServeHTTP(rec4, req4)
	require.Equal(t, "session.delete", state.lastMethod)

	// GET /health
	req5 := httptest.NewRequest("GET", "/api/v1/health", nil)
	rec5 := httptest.NewRecorder()
	router.ServeHTTP(rec5, req5)
	require.Contains(t, rec5.Body.String(), `"status":"ready"`)

	// 响应头恒带 X-Request-Id
	require.NotEmpty(t, rec5.Header().Get("X-Request-Id"))
}

// TestHistoryGetCursor协议 游标转发（web_http_routes.py:131-163）。
func TestHistoryGetCursor协议(t *testing.T) {
	state, router := newTestRouterWithWebChannel()

	req := httptest.NewRequest("GET", "/api/v1/sessions/s1/history?cursor=&limit=20", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code)
	// 空白 cursor 必须转发为 nil 首页（空串会被 agentserver 判非法游标）
	params := state.lastForwardParams
	require.Contains(t, params, "cursor")
	v, ok := params["cursor"]
	require.True(t, ok)
	require.True(t, v == nil)
	require.Equal(t, float64(20), params["limit"]) // JSON 往返 int→float64

	// 只带 page_idx → 分页协议
	req2 := httptest.NewRequest("GET", "/api/v1/sessions/s1/history?page_idx=2", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, map[string]any{"session_id": "s1", "page_idx": float64(2)}, state.lastForwardParams)

	// Accept SSE → 走流式（此处仅验证进入流分支：响应 Content-Type）
	req3 := httptest.NewRequest("GET", "/api/v1/sessions/s1/history?page_idx=1", nil)
	req3.Header.Set("Accept", "text/event-stream")
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	require.Equal(t, "text/event-stream", rec3.Header().Get("Content-Type"))
}

// TestUnaryChatSend非流式 enable_streaming=false → unary。
func TestUnaryChatSend非流式(t *testing.T) {
	state, router := newTestRouterWithWebChannel()
	req := httptest.NewRequest("POST", "/api/v1/chat/completions",
		bytes.NewReader([]byte(`{"session_id":"s1","query":"ping","enable_streaming":false}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code)
	require.True(t, state.lastForward.IsStream) // Python :691 unary chat.send 仍 is_stream=true
	require.Equal(t, "chat.send", state.lastMethod)
}

// TestUnknownMethodEnvelope METHOD_NOT_FOUND → 404 envelope（非 500）。
func TestUnknownMethodEnvelope(t *testing.T) {
	// 无 OnMessage/无 channelMgr：dispatchWebRequest 走 METHOD_NOT_FOUND 分支
	wc := NewWebChannel(WebChannelConfig{Host: "127.0.0.1", Port: 19001, Path: "/ws"}, nil, nil)
	router := chi.NewRouter()
	RegisterWebHTTPAPIRoutes(router, wc)
	_ = wc
	req := httptest.NewRequest("GET", "/api/v1/models", nil) // mapped 行，RPC 未实现
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 404, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.False(t, body["ok"].(bool))
	require.Equal(t, "METHOD_NOT_FOUND", body["error"].(map[string]any)["code"])
}

// ──────────────────────────── 测试基建 ────────────────────────────

// testWebChannelState 记录转发参数供断言。
type testWebChannelState struct {
	wc                  *WebChannel
	lastForward         *schema.Message
	lastMethod          string
	sessionCreateParams map[string]any
	lastForwardParams   map[string]any
}

// newTestRouterWithWebChannel 构造带 /api/v1 路由与 fake 回调的测试环境。
func newTestRouterWithWebChannel() (*testWebChannelState, http.Handler) {
	wc := NewWebChannel(WebChannelConfig{Host: "127.0.0.1", Port: 19001, Path: "/ws"}, nil, nil)
	state := &testWebChannelState{wc: wc, sessionCreateParams: map[string]any{}}
	wc.OnMessage(func(msg *schema.Message) bool {
		state.lastForward = msg
		state.lastMethod = msg.Metadata["method"].(string)
		params := map[string]any{}
		_ = json.Unmarshal(msg.Params, &params)
		state.lastForwardParams = params
		state.sessionCreateParams = params
		// fake reply：模拟 AgentServer 经 MessageHandler → WebChannel.Send 回帧
		//（HTTP outbound 按 metadata.ws_id / session 桶寻址）。
		go func() {
			if msg.Metadata["method"] == "history.get" {
				// 事件帧：history.message 聚合一条 + done 终止（_history_json 收集语义）
				_ = wc.Send(context.Background(), &schema.Message{
					Type:      schema.MessageTypeReq,
					ID:        msg.ID,
					SessionID: msg.SessionID,
					OK:        true,
					Payload: map[string]any{"event_type": "history.message",
						"message": map[string]any{"role": "user"}, "status": "pending"},
				})
				_ = wc.Send(context.Background(), &schema.Message{
					Type:      schema.MessageTypeReq,
					ID:        msg.ID,
					SessionID: msg.SessionID,
					OK:        true,
					Payload: map[string]any{"event_type": "history.message",
						"message": map[string]any{"role": "assistant"}, "status": "done",
						"total_pages": 3, "page_idx": 2},
				})
				return
			}
			_ = wc.Send(context.Background(), &schema.Message{
				Type:      schema.MessageTypeRes,
				ID:        msg.ID,
				SessionID: msg.SessionID,
				OK:        true,
				Payload:   map[string]any{"echo": msg.Metadata["method"]},
			})
		}()
		return true
	})
	// RegisterWebHTTPAPIRoutes 直接注册到 chi 根路由
	router := chi.NewRouter()
	RegisterWebHTTPAPIRoutes(router, wc)
	return state, router
}
