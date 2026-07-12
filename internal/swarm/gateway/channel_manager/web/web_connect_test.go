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
