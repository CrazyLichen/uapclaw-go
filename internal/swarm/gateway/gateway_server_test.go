package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestGatewayServer 构造带单条路由的测试服务器（随机端口由 httptest 分配）。
func newTestGatewayServer(t *testing.T, route *RouteConfig) (*GatewayServer, *httptest.Server) {
	t.Helper()
	cfg := NewGatewayServerConfig()
	if route != nil {
		cfg.Routes[route.Path] = route
	}
	s := NewGatewayServer(cfg)
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.HandleConnection)
	server := httptest.NewServer(mux)
	t.Cleanup(func() {
		_ = s.Stop()
		server.Close()
	})
	return s, server
}

// dialWS 连接测试服务器指定路径。
func dialWS(t *testing.T, server *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + path
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// sendReqFrame 发送 req 帧触发 handleRawMessage 注册路由键（真实协议路径，
// 注册的是服务器侧 conn——直接向表里塞 dialWS 返回的客户端 conn 是错误方向）。
func sendReqFrame(t *testing.T, conn *websocket.Conn, reqID, sessionID string) {
	t.Helper()
	params := map[string]any{}
	if sessionID != "" {
		params["session_id"] = sessionID
	}
	req, _ := json.Marshal(map[string]any{
		"type": "req", "id": reqID, "method": "chat.send", "params": params,
	})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, req))
}

// forwardRoute 返回带转发集合的路由（chat.send 转发且无本地 ack，避免杂帧）。
func forwardRoute(path, channelID string) *RouteConfig {
	return &RouteConfig{
		Path:                         path,
		ChannelID:                    channelID,
		ForwardMethods:               map[string]bool{"chat.send": true},
		ForwardNoLocalHandlerMethods: map[string]bool{"chat.send": true},
	}
}

// waitForwarded 等 onMessage 回调（路由键注册完成的同步点：注册先于回调）。
func waitForwarded(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onMessage not invoked")
	}
}

// readFrame 读取一帧并解析为 map。
func readFrame(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

// ──────────────────────────── resolveRoute 单元测试 ────────────────────────────

func TestResolveRoute_四分支(t *testing.T) {
	cfg := NewGatewayServerConfig()
	cfg.Routes["/acp"] = &RouteConfig{Path: "/acp", ChannelID: "acp"}
	s := NewGatewayServer(cfg)

	// 精确命中
	r, matched := s.resolveRoute("/acp")
	require.NotNil(t, r)
	assert.Equal(t, "/acp", matched)

	// 尾斜杠剥除（/acp/ → /acp）
	r, matched = s.resolveRoute("/acp/")
	require.NotNil(t, r)
	assert.Equal(t, "/acp", matched)

	// 尾斜杠补全（路由表含 /acp/ 时 /acp 命中）
	cfg2 := NewGatewayServerConfig()
	cfg2.Routes["/acp/"] = &RouteConfig{Path: "/acp/", ChannelID: "acp"}
	s2 := NewGatewayServer(cfg2)
	r, matched = s2.resolveRoute("/acp")
	require.NotNil(t, r)
	assert.Equal(t, "/acp/", matched)

	// 未命中
	r, _ = s.resolveRoute("/nope")
	assert.Nil(t, r)

	// 空串
	r, matched = s.resolveRoute("")
	assert.Nil(t, r)
	assert.Equal(t, "", matched)
}

func TestClientRouteKey_空值(t *testing.T) {
	_, ok := clientRouteKey("", "x")
	assert.False(t, ok)
	_, ok = clientRouteKey("ch", "")
	assert.False(t, ok)
	_, ok = clientRouteKey("  ", "x")
	assert.False(t, ok)
	k, ok := clientRouteKey("ch", "x")
	assert.True(t, ok)
	assert.Equal(t, [2]string{"ch", "x"}, k)
}

// ──────────────────────────── 端到端测试 ────────────────────────────

func Test端到端_握手ack与req转发(t *testing.T) {
	var got *schema.Message
	done := make(chan struct{}, 1)
	route := &RouteConfig{
		Path:           "/tui",
		ChannelID:      "tui",
		ForwardMethods: map[string]bool{"chat.send": true},
	}
	s, server := newTestGatewayServer(t, route)
	s.OnMessage(func(ctx context.Context, msg *schema.Message) error {
		got = msg
		done <- struct{}{}
		return nil
	})

	conn := dialWS(t, server, "/tui")

	// 首帧 connection.ack
	ack := readFrame(t, conn)
	assert.Equal(t, "event", ack["type"])
	assert.Equal(t, "connection.ack", ack["event"])
	payload := ack["payload"].(map[string]any)
	assert.Equal(t, "1.0", payload["protocol_version"])
	assert.Equal(t, "tui", payload["transport"])

	// 发 req 帧
	req := map[string]any{
		"type":   "req",
		"id":     "r1",
		"method": "chat.send",
		"params": map[string]any{
			"session_id": "s1",
			"cwd":        "/tmp",
			"mode":       "code.normal",
			"query":      "hi",
		},
	}
	reqData, _ := json.Marshal(req)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, reqData))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onMessage callback not invoked")
	}

	// 断言消息字段
	assert.Equal(t, "r1", got.ID)
	assert.Equal(t, "tui", got.ChannelID)
	assert.Equal(t, "s1", got.SessionID)
	assert.Equal(t, schema.ReqMethodChatSend, got.ReqMethod)
	assert.Equal(t, schema.ModeCodeNormal, got.Mode)
	// req 帧未带 is_stream 字段 → false（对齐 Python bool(data.get("is_stream", False))）
	assert.False(t, got.IsStream)

	// metadata：method + cwd
	assert.Equal(t, "chat.send", got.Metadata["method"])
	assert.Equal(t, "/tmp", got.Metadata["cwd"])

	// params 中 mode 已 setdefault
	var params map[string]any
	require.NoError(t, json.Unmarshal(got.Params, &params))
	assert.Equal(t, "code.normal", params["mode"])
}

func Test端到端_sessionID回退reqID(t *testing.T) {
	var got *schema.Message
	done := make(chan struct{}, 1)
	route := &RouteConfig{
		Path:           "/tui",
		ChannelID:      "tui",
		ForwardMethods: map[string]bool{"chat.send": true},
	}
	s, server := newTestGatewayServer(t, route)
	s.OnMessage(func(ctx context.Context, msg *schema.Message) error {
		got = msg
		done <- struct{}{}
		return nil
	})

	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // 消费 connection.ack

	// params 无 session_id → 回退 reqID
	req, _ := json.Marshal(map[string]any{
		"type": "req", "id": "r2", "method": "chat.send", "params": map[string]any{},
	})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, req))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onMessage callback not invoked")
	}
	assert.Equal(t, "r2", got.SessionID)
}

func Test端到端_不支持的路径(t *testing.T) {
	_, server := newTestGatewayServer(t, &RouteConfig{Path: "/acp", ChannelID: "acp"})
	// httptest 形态下未注册路径直接 HTTP 404 拒绝（对齐 resolveRoute 返回 nil 分支）
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/nope"
	_, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.Error(t, err)
}

func Test端到端_invalid_json(t *testing.T) {
	route := &RouteConfig{Path: "/acp", ChannelID: "acp"}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/acp")
	readFrame(t, conn) // 消费 connection.ack 首帧

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("not json")))

	res := readFrame(t, conn)
	assert.Equal(t, "res", res["type"])
	assert.Equal(t, "", res["id"])
	assert.False(t, res["ok"].(bool))
	assert.Equal(t, "invalid json", res["error"])
}

func Test端到端_非req类型(t *testing.T) {
	route := &RouteConfig{Path: "/acp", ChannelID: "acp"}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/acp")
	readFrame(t, conn) // connection.ack

	req, _ := json.Marshal(map[string]any{"type": "event", "id": "x"})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, req))
	res := readFrame(t, conn)
	assert.False(t, res["ok"].(bool))
	assert.Equal(t, "invalid request", res["error"])
}

func Test端到端_空id或空method(t *testing.T) {
	route := &RouteConfig{Path: "/acp", ChannelID: "acp"}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/acp")
	readFrame(t, conn) // connection.ack

	// 空 method
	req, _ := json.Marshal(map[string]any{"type": "req", "id": "x", "method": "", "params": map[string]any{}})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, req))
	res := readFrame(t, conn)
	assert.False(t, res["ok"].(bool))
	assert.Equal(t, "invalid request", res["error"])
	assert.Equal(t, "x", res["id"])
}

func Test端到端_unknown_method(t *testing.T) {
	route := &RouteConfig{
		Path:           "/acp",
		ChannelID:      "acp",
		ForwardMethods: map[string]bool{"chat.send": true},
	}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/acp")
	readFrame(t, conn) // connection.ack

	req, _ := json.Marshal(map[string]any{
		"type": "req", "id": "r9", "method": "no.such", "params": map[string]any{},
	})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, req))
	res := readFrame(t, conn)
	assert.False(t, res["ok"].(bool))
	assert.Contains(t, res["error"], "unknown method")
}

func Test端到端_本地handler异常_INTERNAL_ERROR(t *testing.T) {
	route := &RouteConfig{
		Path:      "/tui",
		ChannelID: "tui",
		LocalHandlers: map[string]LocalHandlerFunc{
			"boom": func(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
				return assert.AnError
			},
		},
	}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	req, _ := json.Marshal(map[string]any{
		"type": "req", "id": "b1", "method": "boom", "params": map[string]any{},
	})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, req))
	res := readFrame(t, conn)
	assert.False(t, res["ok"].(bool))
	assert.Contains(t, res["error"], "handler error")
	assert.Equal(t, "INTERNAL_ERROR", res["code"])
}

func Test端到端_本地handler成功(t *testing.T) {
	route := &RouteConfig{
		Path:      "/tui",
		ChannelID: "tui",
		LocalHandlers: map[string]LocalHandlerFunc{
			"echo": func(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
				return nil // 本地 handler 自行决定响应（对齐 Python：无自动 ack）
			},
		},
	}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	req, _ := json.Marshal(map[string]any{
		"type": "req", "id": "e1", "method": "echo", "params": map[string]any{},
	})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, req))
	// handler 返回 nil 无自动 ack——2s 内无帧到达即验证（对齐 Python 行为）
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, _, err := conn.ReadMessage()
	require.Error(t, err) // 超时说明没有发多余帧
}

func Test端到端_入站拦截器短路(t *testing.T) {
	intercepted := make(chan struct{}, 1)
	route := &RouteConfig{
		Path:      "/acp",
		ChannelID: "acp",
		InboundInterceptor: func(ws *websocket.Conn, data map[string]any) bool {
			intercepted <- struct{}{}
			return true // 短路
		},
		ForwardMethods: map[string]bool{"chat.send": true},
	}
	s, server := newTestGatewayServer(t, route)
	onMsg := make(chan struct{}, 1)
	s.OnMessage(func(ctx context.Context, msg *schema.Message) error {
		onMsg <- struct{}{}
		return nil
	})

	conn := dialWS(t, server, "/acp")
	readFrame(t, conn) // connection.ack

	req, _ := json.Marshal(map[string]any{
		"type": "req", "id": "i1", "method": "chat.send", "params": map[string]any{},
	})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, req))

	select {
	case <-intercepted:
	case <-time.After(2 * time.Second):
		t.Fatal("inbound interceptor not invoked")
	}
	select {
	case <-onMsg:
		t.Fatal("onMessage should be short-circuited by interceptor")
	case <-time.After(300 * time.Millisecond):
	}
}

// ──────────────────────────── Send 出站测试 ────────────────────────────

func TestSend_request定向(t *testing.T) {
	done := make(chan struct{}, 1)
	s, server := newTestGatewayServer(t, forwardRoute("/tui", "tui"))
	s.OnMessage(func(ctx context.Context, msg *schema.Message) error {
		done <- struct{}{}
		return nil
	})
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	// 走真实协议路径注册路由键（handleRawMessage 内注册服务器侧 conn）
	sendReqFrame(t, conn, "r1", "s1")
	waitForwarded(t, done)

	err := s.Send(context.Background(), &schema.Message{
		ID: "r1", Type: schema.MessageTypeRes, ChannelID: "tui",
		SessionID: "s1", OK: true, Payload: map[string]any{"k": "v"},
	})
	require.NoError(t, err)

	frame := readFrame(t, conn)
	assert.Equal(t, "res", frame["type"])
	assert.Equal(t, "r1", frame["id"])
	assert.True(t, frame["ok"].(bool))
}

func TestSend_res失败帧带error(t *testing.T) {
	done := make(chan struct{}, 1)
	s, server := newTestGatewayServer(t, forwardRoute("/tui", "tui"))
	s.OnMessage(func(ctx context.Context, msg *schema.Message) error {
		done <- struct{}{}
		return nil
	})
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	sendReqFrame(t, conn, "r1", "s1")
	waitForwarded(t, done)

	err := s.Send(context.Background(), &schema.Message{
		ID: "r1", Type: schema.MessageTypeRes, ChannelID: "tui",
		OK:      false,
		Payload: map[string]any{"error": "boom"},
	})
	require.NoError(t, err)

	frame := readFrame(t, conn)
	assert.Equal(t, "res", frame["type"])
	assert.False(t, frame["ok"].(bool))
	assert.Equal(t, "boom", frame["error"])
}

func TestSend_广播fallback(t *testing.T) {
	route := &RouteConfig{Path: "/tui", ChannelID: "tui"}
	s, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	// 不注册任何键 → 广播 fallback：findChannelClients 空仍 dropped（不 panic）
	err := s.Send(context.Background(), &schema.Message{
		ID: "r3", Type: "event", ChannelID: "tui", SessionID: "sx",
	})
	assert.NoError(t, err)
}

func TestSend_无客户端dropped(t *testing.T) {
	route := &RouteConfig{Path: "/tui", ChannelID: "tui"}
	s, _ := newTestGatewayServer(t, route)
	// 不注册任何键 → Warn 丢弃（不 panic、无 error 返回）
	err := s.Send(context.Background(), &schema.Message{
		ID: "r4", Type: "event", ChannelID: "tui", SessionID: "sx",
	})
	assert.NoError(t, err)
}

func TestSend_acp渠道不广播(t *testing.T) {
	// Python：channel_id == "acp" 不走广播 fallback
	route := &RouteConfig{Path: "/acp", ChannelID: "acp"}
	s, _ := newTestGatewayServer(t, route)
	err := s.Send(context.Background(), &schema.Message{
		ID: "r5", Type: "event", ChannelID: "acp", SessionID: "s",
	})
	assert.NoError(t, err) // dropped（acp 不广播）
}

func TestSend_广播fallback_有channel客户端(t *testing.T) {
	// session 定向 miss 后 findChannelClients 命中 → event 帧广播
	done := make(chan struct{}, 1)
	s, server := newTestGatewayServer(t, forwardRoute("/tui", "tui"))
	s.OnMessage(func(ctx context.Context, msg *schema.Message) error {
		done <- struct{}{}
		return nil
	})
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	// 先注册 s1 的 session 键（真实协议路径），再发一条 session=s2 的消息：
	// request/session 定向均 miss → findChannelClients("tui") 命中 conn → 广播
	sendReqFrame(t, conn, "q1", "s1")
	waitForwarded(t, done)

	err := s.Send(context.Background(), &schema.Message{
		ID: "r7", Type: "event", ChannelID: "tui", SessionID: "s2",
		EventType: schema.EventTypeChatFinal,
		Payload:   map[string]any{"content": "done"},
	})
	require.NoError(t, err)

	frame := readFrame(t, conn)
	assert.Equal(t, "event", frame["type"])
	assert.Equal(t, "chat.final", frame["event"])
	payload := frame["payload"].(map[string]any)
	assert.Equal(t, "s2", payload["session_id"])
	assert.Equal(t, "done", payload["content"])
}

func TestSend_出站拦截器短路(t *testing.T) {
	done := make(chan struct{}, 1)
	route := &RouteConfig{
		Path: "/tui", ChannelID: "tui",
		OutboundInterceptor: func(ws *websocket.Conn, data map[string]any) bool {
			return true // 短路出站
		},
		ForwardMethods:               map[string]bool{"chat.send": true},
		ForwardNoLocalHandlerMethods: map[string]bool{"chat.send": true},
	}
	s, server := newTestGatewayServer(t, route)
	s.OnMessage(func(ctx context.Context, msg *schema.Message) error {
		done <- struct{}{}
		return nil
	})
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	// 真实协议路径注册 session 键
	sendReqFrame(t, conn, "q1", "s1")
	waitForwarded(t, done)

	err := s.Send(context.Background(), &schema.Message{
		ID: "r6", Type: "event", ChannelID: "tui", SessionID: "s1",
	})
	require.NoError(t, err)

	// 拦截器短路 → 无帧到达（300ms 内无帧）
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, _, readErr := conn.ReadMessage()
	require.Error(t, readErr)
}

// ──────────────────────────── 断连与停机测试 ────────────────────────────

func Test断连_stale清理与disconnect回调(t *testing.T) {
	staleCh := make(chan [][2]string, 1)
	route := &RouteConfig{
		Path:      "/tui",
		ChannelID: "tui",
		DisconnectHandler: func(ws *websocket.Conn, keys [][2]string) {
			staleCh <- keys
		},
		ForwardMethods:               map[string]bool{"chat.send": true},
		ForwardNoLocalHandlerMethods: map[string]bool{"chat.send": true},
	}
	done := make(chan struct{}, 1)
	s, server := newTestGatewayServer(t, route)
	s.OnMessage(func(ctx context.Context, msg *schema.Message) error {
		done <- struct{}{}
		return nil
	})
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	// 真实协议路径注册 session 键（handleRawMessage 注册的是服务器侧 conn）
	sendReqFrame(t, conn, "q1", "s1")
	waitForwarded(t, done)
	_ = conn.Close()

	select {
	case keys := <-staleCh:
		require.Len(t, keys, 1)
		assert.Equal(t, [2]string{"tui", "s1"}, keys[0])
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect handler not invoked")
	}

	// stale 键已清理
	s.clientsMu.RLock()
	_, exists := s.sessionToClient[[2]string{"tui", "s1"}]
	s.clientsMu.RUnlock()
	assert.False(t, exists)
}

func Test断连_cleanup兜底回调(t *testing.T) {
	cleanupCh := make(chan struct{}, 1)
	route := &RouteConfig{
		Path:  "/tui",
		CleanupHandler: func(ws *websocket.Conn) {
			cleanupCh <- struct{}{}
		},
	}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	_ = conn.Close()

	select {
	case <-cleanupCh:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup handler not invoked")
	}
}

func Test断连_disconnect优先于cleanup(t *testing.T) {
	var order []string
	orderCh := make(chan string, 2)
	route := &RouteConfig{
		Path: "/tui",
		DisconnectHandler: func(ws *websocket.Conn, keys [][2]string) {
			orderCh <- "disconnect"
		},
		CleanupHandler: func(ws *websocket.Conn) {
			orderCh <- "cleanup"
		},
	}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack
	_ = conn.Close()

	select {
	case first := <-orderCh:
		assert.Equal(t, "disconnect", first) // disconnect 优先（对齐 Python elif）
	case <-time.After(2 * time.Second):
		t.Fatal("no handler invoked")
	}
	_ = order
}

// ──────────────────────────── Start/Stop 测试 ────────────────────────────

func TestStart_未启用直接返回(t *testing.T) {
	cfg := NewGatewayServerConfig()
	cfg.Enabled = false
	s := NewGatewayServer(cfg)
	err := s.Start(context.Background())
	assert.NoError(t, err)
	assert.False(t, s.running.Load())
}

func TestStartStop_生命周期(t *testing.T) {
	cfg := NewGatewayServerConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = 0 // 随机端口
	cfg.Routes["/acp"] = &RouteConfig{Path: "/acp", ChannelID: "acp"}
	s := NewGatewayServer(cfg)

	require.NoError(t, s.Start(context.Background()))
	assert.True(t, s.running.Load())

	require.NoError(t, s.Stop())
	assert.False(t, s.running.Load())
}

// ──────────────────────────── 低覆盖分支补测 ────────────────────────────

func TestNewGatewayServer_nil配置兜底(t *testing.T) {
	s := NewGatewayServer(nil)
	// 兜底默认配置：Enabled=true + 空路由表（Path/ChannelID 均空不造路由）
	assert.NotNil(t, s.config)
	assert.True(t, s.config.Enabled)
	assert.Empty(t, s.config.Routes)
}

func TestStart_已运行幂等(t *testing.T) {
	cfg := NewGatewayServerConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Routes["/acp"] = &RouteConfig{Path: "/acp", ChannelID: "acp"}
	s := NewGatewayServer(cfg)
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })
	// 二次 Start 幂等（不重建 httpServer、不重复监听）
	require.NoError(t, s.Start(context.Background()))
}

func TestHandleConnection_断连回调panic恢复(t *testing.T) {
	route := &RouteConfig{
		Path:      "/tui",
		ChannelID: "tui",
		DisconnectHandler: func(ws *websocket.Conn, keys [][2]string) {
			panic("boom")
		},
	}
	s, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack
	_ = conn.Close()
	// 回调 panic 被 recover 吞掉——进程存活、无崩溃即通过
	time.Sleep(200 * time.Millisecond)
	_ = s
}

func TestHandleConnection_cleanup回调panic恢复(t *testing.T) {
	route := &RouteConfig{
		Path:  "/tui",
		CleanupHandler: func(ws *websocket.Conn) {
			panic("boom")
		},
	}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack
	_ = conn.Close()
	time.Sleep(200 * time.Millisecond)
}

func TestR2s_空路由(t *testing.T) {
	assert.Equal(t, "", r2s(nil))
	assert.Equal(t, "/tui", r2s(&RouteConfig{Path: "/tui"}))
}

func TestSendEvent_帧格式(t *testing.T) {
	done := make(chan struct{}, 1)
	route := &RouteConfig{
		Path:      "/tui",
		ChannelID: "tui",
		LocalHandlers: map[string]LocalHandlerFunc{
			// 本地 handler 内调用 sendEvent（真实使用路径：11.15 TUI 消费）
			"emit": func(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
				s := NewGatewayServer(nil)
				s.sendEvent(ws, "tui.state", map[string]any{"phase": "busy"})
				s.sendEvent(ws, "tui.state", nil) // nil payload 兜底空 map
				done <- struct{}{}
				return nil
			},
		},
	}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	req, _ := json.Marshal(map[string]any{
		"type": "req", "id": "e9", "method": "emit", "params": map[string]any{},
	})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, req))
	<-done

	frame := readFrame(t, conn)
	assert.Equal(t, "event", frame["type"])
	assert.Equal(t, "tui.state", frame["event"])
	payload := frame["payload"].(map[string]any)
	assert.Equal(t, "busy", payload["phase"])

	// nil payload 兜底为空 map
	frame2 := readFrame(t, conn)
	assert.Equal(t, "event", frame2["type"])
	payload2 := frame2["payload"].(map[string]any)
	assert.Empty(t, payload2)
}

func TestBuildOutboundFrame_res失败缺error字段兜底(t *testing.T) {
	// ok=false 且 payload 无 error 字段 → 兜底 "request failed"
	frame := buildOutboundFrame(&schema.Message{
		ID: "x1", Type: schema.MessageTypeRes, OK: false, Payload: nil,
	})
	assert.Equal(t, "request failed", frame["error"])
}

func TestSendResponse_失败缺errMsg兜底(t *testing.T) {
	done := make(chan struct{}, 1)
	route := &RouteConfig{
		Path:      "/tui",
		ChannelID: "tui",
		LocalHandlers: map[string]LocalHandlerFunc{
			"noop": func(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
				s := NewGatewayServer(nil)
				s.sendResponse(ws, reqID, false, nil, "", "")
				done <- struct{}{}
				return nil
			},
		},
	}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	req, _ := json.Marshal(map[string]any{
		"type": "req", "id": "n1", "method": "noop", "params": map[string]any{},
	})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, req))
	<-done

	frame := readFrame(t, conn)
	assert.Equal(t, "res", frame["type"])
	assert.False(t, frame["ok"].(bool))
	assert.Equal(t, "request failed", frame["error"])
	assert.Equal(t, map[string]any{}, frame["payload"])
	// code 为空不输出
	_, hasCode := frame["code"]
	assert.False(t, hasCode)
}

func TestSendResponse_ok帧不带error字段(t *testing.T) {
	done := make(chan struct{}, 1)
	route := &RouteConfig{
		Path:      "/tui",
		ChannelID: "tui",
		LocalHandlers: map[string]LocalHandlerFunc{
			"okres": func(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
				s := NewGatewayServer(nil)
				s.sendResponse(ws, reqID, true, map[string]any{"r": 1}, "", "")
				done <- struct{}{}
				return nil
			},
		},
	}
	_, server := newTestGatewayServer(t, route)
	conn := dialWS(t, server, "/tui")
	readFrame(t, conn) // connection.ack

	req, _ := json.Marshal(map[string]any{
		"type": "req", "id": "o1", "method": "okres", "params": map[string]any{},
	})
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, req))
	<-done

	frame := readFrame(t, conn)
	assert.Equal(t, "res", frame["type"])
	assert.True(t, frame["ok"].(bool))
	// JSON 反序列化后 int 变 float64
	assert.Equal(t, map[string]any{"r": float64(1)}, frame["payload"])
	_, hasErr := frame["error"]
	assert.False(t, hasErr)
}
