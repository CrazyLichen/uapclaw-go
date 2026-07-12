package web

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
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewWebChannel_默认配置(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	// 注册 RPC handlers（对齐 Python _register_web_handlers）
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})
	assert.Equal(t, "127.0.0.1", wc.config.Host)
	assert.Equal(t, 19000, wc.config.Port)
	assert.Equal(t, "/ws", wc.config.Path)
	assert.NotNil(t, wc.dispatcher)
	assert.NotNil(t, wc.upgrader.CheckOrigin)
}

func TestNewWebChannel_自定义配置(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{
		Enabled: true,
		Host:    "0.0.0.0",
		Port:    8080,
		Path:    "/custom-ws",
	}, nil, nil)
	assert.Equal(t, "0.0.0.0", wc.config.Host)
	assert.Equal(t, 8080, wc.config.Port)
	assert.Equal(t, "/custom-ws", wc.config.Path)
}

func TestWebChannel_BaseChannel接口(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)

	// 验证 WebChannel 实现 BaseChannel 接口
	var _ channel_manager.BaseChannel = wc

	assert.Equal(t, "web", wc.ChannelID())
	assert.Equal(t, channel_manager.ChannelTypeWeb, wc.ChannelType())
	assert.False(t, wc.IsRunning())
}

func TestWebChannel_StartStop(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)

	err := wc.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, wc.IsRunning())

	err = wc.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, wc.IsRunning())
}

func TestWebChannel_Addr(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{
		Enabled: true,
		Host:    "0.0.0.0",
		Port:    9090,
	}, nil, nil)
	assert.Equal(t, "0.0.0.0:9090", wc.Addr())
}

func TestWebChannel_ClientCount(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	assert.Equal(t, 0, wc.ClientCount())
}

func TestWebChannel_OnMessage(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	called := false
	wc.OnMessage(func(_ *schema.Message) bool {
		called = true
		return true
	})
	assert.NotNil(t, wc.onMessageCb)
	wc.onMessageCb(nil)
	assert.True(t, called)
}

func TestWebChannel_Config(t *testing.T) {
	cfg := WebChannelConfig{Enabled: true, Host: "localhost", Port: 3000}
	wc := NewWebChannel(cfg, nil, nil)
	result := wc.Config()
	retCfg, ok := result.(WebChannelConfig)
	require.True(t, ok)
	assert.Equal(t, "localhost", retCfg.Host)
}

func TestWebChannel_Send(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	// 无客户端时 Send 不应报错
	msg := &schema.Message{
		SessionID: "sess_1",
		EventType: schema.EventTypeChatFinal,
		OK:        true,
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_WebSocket连接生命周期(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	// 注册 RPC handlers（对齐 Python _register_web_handlers）
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})
	require.NoError(t, wc.Start(context.Background()))
	defer func() { _ = wc.Stop(context.Background()) }()

	// 启动 HTTP 测试服务器，路径需匹配 config.Path（默认 /ws）
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	// 构建 WS URL（路径为 /ws）
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	// 连接 WebSocket
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// 注意：AgentClient 为 nil 时，onConnect 钩子跳过 connection.ack（对齐 Python 行为）
	// 手动注册一个 ack 钩子用于测试
	// 此处直接发送 RPC 请求验证通道工作

	// 发送 RPC 请求
	reqFrame := ReqFrame{
		Type:   "req",
		ID:     "test-1",
		Method: "channel.get",
	}
	reqData, _ := reqFrame.Encode()
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, reqData))

	// 读取响应
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, respData, err := conn.ReadMessage()
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.Equal(t, "res", resp["type"])
	assert.Equal(t, "test-1", resp["id"])
	assert.True(t, resp["ok"].(bool))

	// 验证客户端数量
	assert.Equal(t, 1, wc.ClientCount())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestDetermineEventName_从EventType(t *testing.T) {
	msg := &schema.Message{EventType: schema.EventTypeChatFinal}
	assert.Equal(t, "chat.final", determineEventName(msg))
}

func TestDetermineEventName_从PayloadEventType(t *testing.T) {
	msg := &schema.Message{
		Payload: map[string]any{"event_type": "chat.chunk"},
	}
	assert.Equal(t, "chat.chunk", determineEventName(msg))
}

func TestDetermineEventName_默认chatFinal(t *testing.T) {
	msg := &schema.Message{}
	assert.Equal(t, "chat.final", determineEventName(msg))
}

func TestDetermineEventName_PayloadEventType为空字符串(t *testing.T) {
	msg := &schema.Message{
		Payload: map[string]any{"event_type": ""},
	}
	assert.Equal(t, "chat.final", determineEventName(msg))
}

func TestIsFullPayloadEvent_已知事件(t *testing.T) {
	assert.True(t, isFullPayloadEvent("connection.ack"))
	assert.True(t, isFullPayloadEvent("chat.tool_call"))
	assert.True(t, isFullPayloadEvent("chat.processing_status"))
	assert.True(t, isFullPayloadEvent("chat.interrupt_result"))
	assert.True(t, isFullPayloadEvent("chat.error"))
	assert.True(t, isFullPayloadEvent("chat.session_result"))
}

func TestIsFullPayloadEvent_通配符前缀(t *testing.T) {
	assert.True(t, isFullPayloadEvent("team.update"))
	assert.True(t, isFullPayloadEvent("harness.log"))
}

func TestIsFullPayloadEvent_非全量事件(t *testing.T) {
	assert.False(t, isFullPayloadEvent("chat.final"))
	assert.False(t, isFullPayloadEvent("chat.chunk"))
	assert.False(t, isFullPayloadEvent("unknown.event"))
}

func TestExtractPureTextPayload_有content(t *testing.T) {
	msg := &schema.Message{
		SessionID: "sess-1",
		OK:        true,
		Payload:   map[string]any{"content": "你好", "role": "assistant", "member_name": "agent1"},
	}
	payload := extractPureTextPayload(msg, "chat.final")
	assert.Equal(t, "sess-1", payload["session_id"])
	assert.Equal(t, "你好", payload["content"])
	assert.Equal(t, "assistant", payload["role"])
	assert.Equal(t, "agent1", payload["member_name"])
}

func TestExtractPureTextPayload_无content有error(t *testing.T) {
	msg := &schema.Message{
		SessionID: "sess-1",
		OK:        false,
		Payload:   map[string]any{"error": "something went wrong"},
	}
	payload := extractPureTextPayload(msg, "chat.final")
	assert.Equal(t, "something went wrong", payload["content"])
}

func TestExtractPureTextPayload_cron字段(t *testing.T) {
	msg := &schema.Message{
		SessionID: "sess-1",
		OK:        true,
		Payload: map[string]any{
			"content": "done",
			"cron":    map[string]any{"enabled": true},
		},
	}
	payload := extractPureTextPayload(msg, "chat.final")
	assert.Contains(t, payload, "cron")
}

func TestExtractPureTextPayload_无Payload(t *testing.T) {
	msg := &schema.Message{
		SessionID: "sess-1",
		OK:        true,
	}
	payload := extractPureTextPayload(msg, "chat.final")
	assert.Equal(t, "sess-1", payload["session_id"])
}

func TestWebChannel_Send_nil消息(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	err := wc.Send(context.Background(), nil)
	assert.NoError(t, err)
}

func TestWebChannel_Send_响应消息(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		ID:        "res-1",
		Type:      schema.MessageTypeRes,
		SessionID: "sess-1",
		OK:        true,
		Payload:   map[string]any{"result": "ok"},
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_Send_响应消息失败(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		ID:        "res-1",
		Type:      schema.MessageTypeRes,
		SessionID: "sess-1",
		OK:        false,
		Payload:   map[string]any{"error": "some error", "code": "INTERNAL_ERROR"},
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_Send_全量事件(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		SessionID: "sess-1",
		EventType: schema.EventTypeChatToolCall,
		Payload:   map[string]any{"tool": "search"},
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_Send_纯文本事件(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		SessionID: "sess-1",
		EventType: schema.EventTypeChatFinal,
		OK:        true,
		Payload:   map[string]any{"content": "最终回复"},
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_Send_interruptResult(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		SessionID: "sess-1",
		EventType: schema.EventTypeChatInterruptResult,
		OK:        true,
		Payload:   map[string]any{"intent": "cancel"},
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_Send_从Payload推断事件名(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		SessionID: "sess-1",
		Payload:   map[string]any{"event_type": "chat.processing_status", "is_processing": true},
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_broadcastInterruptSideEffect_cancel(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		SessionID: "sess-1",
		Payload:   map[string]any{"intent": "cancel"},
	}
	// 不会 panic（无客户端时广播为空操作）
	wc.broadcastInterruptSideEffect(msg)
}

func TestWebChannel_broadcastInterruptSideEffect_pause(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		SessionID: "sess-1",
		Payload:   map[string]any{"intent": "pause"},
	}
	wc.broadcastInterruptSideEffect(msg)
}

func TestWebChannel_broadcastInterruptSideEffect_无Payload(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		SessionID: "sess-1",
	}
	wc.broadcastInterruptSideEffect(msg)
}

func TestWebChannel_OnConnect(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	called := false
	wc.OnConnect(func(_ *websocket.Conn) error {
		called = true
		return nil
	})
	assert.Len(t, wc.connectHooks, 1)
	// 手动触发钩子
	require.NoError(t, wc.connectHooks[0](nil))
	assert.True(t, called)
}

func TestHandleWebSocket_路径不匹配(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})

	mux := http.NewServeMux()
	mux.HandleFunc("/", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/wrong-path")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestWebChannel_Send_响应消息无Payload(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		ID:        "res-1",
		Type:      schema.MessageTypeRes,
		SessionID: "sess-1",
		OK:        true,
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_Send_全量事件无SessionID(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		EventType: schema.EventTypeChatToolCall,
		Payload:   map[string]any{"tool": "search"},
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_Send_全量事件Payload含SessionID(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		EventType: schema.EventTypeChatToolCall,
		Payload:   map[string]any{"tool": "search", "session_id": "sess-1"},
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_Send_全量事件NilPayload(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		EventType: schema.EventTypeChatToolCall,
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_Send_纯文本事件ErrorFallback(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		EventType: schema.EventTypeChatFinal,
		OK:        false,
		Payload:   map[string]any{"error": "some error"},
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_Send_纯文本事件Content为空ErrorFallback(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	msg := &schema.Message{
		EventType: schema.EventTypeChatFinal,
		OK:        false,
		Payload:   map[string]any{"content": "", "error": "fallback error"},
	}
	err := wc.Send(context.Background(), msg)
	assert.NoError(t, err)
}

func TestWebChannel_broadcastEvent(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	// 无客户端时不应 panic
	wc.broadcastEvent("test.event", map[string]any{"key": "val"})
}

func TestWebChannel_broadcastRaw(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	// 无客户端时不应 panic
	wc.broadcastRaw([]byte(`{"type":"event"}`))
}

func TestWebChannel_writeError(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	// writeError 需要一个连接，但无客户端连接测试不 panic
	// 通过 WebSocket 测试 writeError
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})
	require.NoError(t, wc.Start(context.Background()))
	defer func() { _ = wc.Stop(context.Background()) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// 发送无效 JSON 触发 writeError
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("invalid json")))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, respData, err := conn.ReadMessage()
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.Equal(t, "res", resp["type"])
	assert.False(t, resp["ok"].(bool))
}

func TestWebChannel_WebSocket连接_无效帧(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})
	require.NoError(t, wc.Start(context.Background()))
	defer func() { _ = wc.Stop(context.Background()) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// 发送无效 JSON 触发 BAD_REQUEST
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("not json")))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, respData, err := conn.ReadMessage()
	require.NoError(t, err)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.False(t, resp["ok"].(bool))
}

func TestWebChannel_WebSocket连接_帧校验失败(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})
	require.NoError(t, wc.Start(context.Background()))
	defer func() { _ = wc.Stop(context.Background()) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// 发送 type 不为 req 的帧
	invalidFrame := map[string]any{"type": "event", "id": "test-1", "method": "test"}
	data, _ := json.Marshal(invalidFrame)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, data))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, respData, err := conn.ReadMessage()
	require.NoError(t, err)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.False(t, resp["ok"].(bool))
}

func TestWebChannel_WebSocket连接_未知方法(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})
	require.NoError(t, wc.Start(context.Background()))
	defer func() { _ = wc.Stop(context.Background()) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	reqFrame := ReqFrame{Type: "req", ID: "test-1", Method: "nonexistent.method"}
	reqData, _ := reqFrame.Encode()
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, reqData))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, respData, err := conn.ReadMessage()
	require.NoError(t, err)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.False(t, resp["ok"].(bool))
	assert.Equal(t, WsErrMethodNotFound, resp["code"])
}

func TestWebChannel_WebSocket连接_带params(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})
	require.NoError(t, wc.Start(context.Background()))
	defer func() { _ = wc.Stop(context.Background()) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	reqFrame := ReqFrame{
		Type:   "req",
		ID:     "test-2",
		Method: "channel.get",
		Params: json.RawMessage(`{}`),
	}
	reqData, _ := reqFrame.Encode()
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, reqData))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, respData, err := conn.ReadMessage()
	require.NoError(t, err)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.True(t, resp["ok"].(bool))
}

func TestWebChannel_WebSocket连接_带sessionID(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})
	require.NoError(t, wc.Start(context.Background()))
	defer func() { _ = wc.Stop(context.Background()) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	reqFrame := ReqFrame{
		Type:   "req",
		ID:     "test-3",
		Method: "channel.get",
		Params: json.RawMessage(`{"session_id": "sess_custom"}`),
	}
	reqData, _ := reqFrame.Encode()
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, reqData))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, respData, err := conn.ReadMessage()
	require.NoError(t, err)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.True(t, resp["ok"].(bool))
}
