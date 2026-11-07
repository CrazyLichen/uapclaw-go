package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// startDispatchTestServer 启动装配好 handler 的测试 WS 服务并返回已连接的客户端。
// 对齐 TestWebChannel_WebSocket连接生命周期 的脚手架，供分发顺序用例复用。
func startDispatchTestServer(t *testing.T, wc *WebChannel) *websocket.Conn {
	t.Helper()
	require.NoError(t, wc.Start(t.Context()))
	t.Cleanup(func() { _ = wc.Stop(t.Context()) })

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// sendReq 发送一帧 req。
func sendReq(t *testing.T, conn *websocket.Conn, reqID, method string, params map[string]any) {
	t.Helper()
	req := map[string]any{"type": "req", "id": reqID, "method": method, "params": params}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, data))
}

// readFrame 读取一帧 res/event。
func readFrame(t *testing.T, conn *websocket.Conn, wait time.Duration) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(wait))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	var frame map[string]any
	require.NoError(t, json.Unmarshal(data, &frame))
	return frame
}

// ──────────────────────────── sendResponse 帧形状 ────────────────────────────

// TestSendResponse_成功帧恒带payload 对齐上游 send_response：payload 落 {} 且无 error/code。
func TestSendResponse_成功帧恒带payload(t *testing.T) {
	wc := newTestChannel()
	collector := &frameCollector{}
	wc.sendResponse(collector, "r1", true, nil, "", "")

	require.Len(t, collector.frames, 1)
	frame := collector.frames[0]
	assert.Equal(t, "res", frame["type"])
	assert.Equal(t, "r1", frame["id"])
	assert.Equal(t, true, frame["ok"])
	assert.Equal(t, map[string]any{}, frame["payload"])
	assert.NotContains(t, frame, "error")
	assert.NotContains(t, frame, "code")
}

// TestSendResponse_失败帧error兜底 空 error 文案落 "request failed"，且仅在失败帧出现。
func TestSendResponse_失败帧error兜底(t *testing.T) {
	wc := newTestChannel()
	collector := &frameCollector{}
	wc.sendResponse(collector, "r2", false, nil, "", "")

	require.Len(t, collector.frames, 1)
	frame := collector.frames[0]
	assert.Equal(t, false, frame["ok"])
	assert.Equal(t, "request failed", frame["error"])
	assert.NotContains(t, frame, "code")
}

// TestSendResponse_失败帧透传code 非空 code 与 error 一并写出。
func TestSendResponse_失败帧透传code(t *testing.T) {
	wc := newTestChannel()
	collector := &frameCollector{}
	wc.sendResponse(collector, "r3", false, nil, "boom", WsErrBadRequest)

	require.Len(t, collector.frames, 1)
	frame := collector.frames[0]
	assert.Equal(t, "boom", frame["error"])
	assert.Equal(t, WsErrBadRequest, frame["code"])
}

// ──────────────────────────── handler 异常兜底 ────────────────────────────

// TestInvokeMethodHandler_panic转INTERNAL_ERROR 对齐上游 _invoke_method_handler 异常分支。
// handler panic 不能打断读循环：必须转成 INTERNAL_ERROR 帧并返回 false。
func TestInvokeMethodHandler_panic转INTERNAL_ERROR(t *testing.T) {
	wc := newTestChannel()
	collector := &frameCollector{}
	panicky := func(ws wsSender, reqID string, _ map[string]any, _ string) {
		panic("boom")
	}

	called := wc.invokeMethodHandler(collector, panicky, "x.y", "r4", nil, "s1")

	assert.False(t, called, "panic 应视为 handler 未应答")
	require.Len(t, collector.frames, 1)
	frame := collector.frames[0]
	assert.Equal(t, false, frame["ok"])
	assert.Equal(t, WsErrInternalError, frame["code"])
	assert.Contains(t, frame["error"], "handler error")
}

// TestInvokeMethodHandler_正常返回true 正常 handler 视为已应答。
func TestInvokeMethodHandler_正常返回true(t *testing.T) {
	wc := newTestChannel()
	collector := &frameCollector{}
	ok := func(ws wsSender, reqID string, _ map[string]any, _ string) {
		wc.sendResponse(ws, reqID, true, map[string]any{"ok": true}, "", "")
	}

	assert.True(t, wc.invokeMethodHandler(collector, ok, "x.y", "r5", nil, "s1"))
	assert.Len(t, collector.frames, 1)
}

// ──────────────────────────── 分发顺序（对齐 web_connect.py:2180-2231） ────────────────────────────

// trailRecorder 记录 handler/回调的实际执行时刻（读循环 goroutine 写、测试 goroutine 读）。
type trailRecorder struct {
	// mu 保护 items
	mu sync.Mutex
	// items 执行序列
	items []string
}

// add 追加一条执行记录。
func (r *trailRecorder) add(tag string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, tag)
}

// snapshot 取当前执行序列的副本。
func (r *trailRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.items...)
}

// wrapWithTrail 用包装器替换已注册方法，记录 handler 实际执行时刻。
// 包装器与被包装 handler 同在读循环 goroutine 上执行，因此记录顺序即真实分发顺序。
func wrapWithTrail(wc *WebChannel, method string, recorder *trailRecorder, tag string) {
	orig := wc.methodHandlers[method]
	wc.registerMethod(method, func(ws wsSender, reqID string, params map[string]any, sid string) {
		recorder.add(tag)
		orig(ws, reqID, params, sid)
	}, false)
}

// TestDispatch_chatSend_先handler后回调 chat.send 属 _HANDLER_BEFORE_CALLBACK_METHODS。
// 上游顺序：本地 handler（写 ack）→ 回调转发；且 handler 已应答时不再补帧。
func TestDispatch_chatSend_先handler后回调(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})

	recorder := &trailRecorder{}
	wrapWithTrail(wc, "chat.send", recorder, "handler")
	wc.OnMessage(func(*schema.Message) bool {
		recorder.add("callback")
		return false
	})

	conn := startDispatchTestServer(t, wc)
	sendReq(t, conn, "r-chat-send", "chat.send", map[string]any{"session_id": "s1"})

	frame := readFrame(t, conn, 2*time.Second)
	assert.Equal(t, "res", frame["type"])
	assert.Equal(t, "r-chat-send", frame["id"])
	assert.Equal(t, true, frame["ok"])
	payload, _ := frame["payload"].(map[string]any)
	assert.Equal(t, true, payload["accepted"])
	assert.Equal(t, []string{"handler", "callback"}, recorder.snapshot(), "chat.send 必须先本地 ack 再转发")
}

// TestDispatch_普通转发方法_先回调后handler chat.interrupt 不在 handler-before-callback 集合。
// 上游顺序：回调转发 → 回调未处理 → 本地 handler 回 ack。
func TestDispatch_普通转发方法_先回调后handler(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})

	recorder := &trailRecorder{}
	wrapWithTrail(wc, "chat.interrupt", recorder, "handler")
	wc.OnMessage(func(*schema.Message) bool {
		recorder.add("callback")
		return false
	})

	conn := startDispatchTestServer(t, wc)
	sendReq(t, conn, "r-interrupt", "chat.interrupt", map[string]any{"session_id": "s1"})

	frame := readFrame(t, conn, 2*time.Second)
	assert.Equal(t, "res", frame["type"])
	assert.Equal(t, "r-interrupt", frame["id"])
	assert.Equal(t, true, frame["ok"])
	assert.Equal(t, []string{"callback", "handler"}, recorder.snapshot(), "非 chat.send 方法必须先回调再 handler")
}

// TestDispatch_回调已处理时短路handler 回调返回 true → 不再调用本地 handler。
func TestDispatch_回调已处理时短路handler(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	wc.registerMethod("test.shortcircuit", func(ws wsSender, reqID string, _ map[string]any, _ string) {
		wc.sendResponse(ws, reqID, true, map[string]any{"local": true}, "", "")
	}, false)

	recorder := &trailRecorder{}
	wrapWithTrail(wc, "test.shortcircuit", recorder, "handler")
	first := true
	wc.OnMessage(func(*schema.Message) bool {
		recorder.add("callback")
		if first {
			first = false
			return true
		}
		return false
	})

	conn := startDispatchTestServer(t, wc)
	sendReq(t, conn, "r-short", "test.shortcircuit", nil)
	// 回调声明已处理即短路，不写 local 帧；随后的方法响应证明短路没有卡住读循环
	sendReq(t, conn, "r-after", "no.such.method", nil)
	frame := readFrame(t, conn, 2*time.Second)
	assert.Equal(t, "r-after", frame["id"], "回调短路后不应写 local 帧")
	assert.Equal(t, []string{"callback", "callback"}, recorder.snapshot())
	assert.Equal(t, WsErrMethodNotFound, frame["code"])
}

// TestDispatch_localOnly不转发 local_only 方法只走本地 handler，回调不参与。
func TestDispatch_localOnly不转发(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	wc.registerMethod("test.localonly", func(ws wsSender, reqID string, _ map[string]any, _ string) {
		wc.sendResponse(ws, reqID, true, map[string]any{"local": true}, "", "")
	}, true)

	recorder := &trailRecorder{}
	wc.OnMessage(func(*schema.Message) bool {
		recorder.add("callback")
		return false
	})

	conn := startDispatchTestServer(t, wc)
	sendReq(t, conn, "r-local", "test.localonly", nil)

	frame := readFrame(t, conn, 2*time.Second)
	assert.Equal(t, true, frame["ok"])
	assert.Empty(t, recorder.snapshot(), "local_only 方法不应进回调")
}

// TestDispatch_localOnly未注册回METHOD_NOT_FOUND 对齐上游 `if handler is None` 分支。
func TestDispatch_localOnly未注册回METHOD_NOT_FOUND(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	// 仅打 local_only 标记、不注册 handler：模拟注册表与标记不一致
	wc.methodsMu.Lock()
	wc.localOnlyMethods["test.missing"] = true
	wc.methodsMu.Unlock()

	conn := startDispatchTestServer(t, wc)
	sendReq(t, conn, "r-missing", "test.missing", nil)

	frame := readFrame(t, conn, 2*time.Second)
	assert.Equal(t, false, frame["ok"])
	assert.Equal(t, WsErrMethodNotFound, frame["code"])
	assert.Contains(t, frame["error"], "unknown method")
}

// TestDispatch_未注册方法回METHOD_NOT_FOUND 无 handler 且无回调处理 → unknown method。
func TestDispatch_未注册方法回METHOD_NOT_FOUND(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)

	conn := startDispatchTestServer(t, wc)
	sendReq(t, conn, "r-unknown", "no.such.method", nil)

	frame := readFrame(t, conn, 2*time.Second)
	assert.Equal(t, false, frame["ok"])
	assert.Equal(t, WsErrMethodNotFound, frame["code"])
	assert.Equal(t, "unknown method: no.such.method", frame["error"])
}
