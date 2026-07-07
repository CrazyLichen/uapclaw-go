package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/wsorigin"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WebChannelConfig Web 通道配置。
//
// 对齐 Python WebChannelConfig 中各字段和默认值。
type WebChannelConfig struct {
	// Enabled 是否启用
	Enabled bool
	// Host 监听地址，默认 "127.0.0.1"
	Host string
	// Port 监听端口，默认 19000
	Port int
	// Path WS 路径，默认 "/ws"
	Path string
	// AllowFrom Origin 白名单
	AllowFrom []string
}

// WebChannel Web 通道，实现 BaseChannel 接口。
//
// 管理 WebSocket 连接生命周期、RPC 请求分发和事件推送。
//
// 对应 Python: jiuwenswarm/gateway/channel_manager/web/web_connect.py (WebChannel)
type WebChannel struct {
	// config 通道配置
	config WebChannelConfig
	// clients 活跃 WebSocket 连接集合
	clients map[*websocket.Conn]bool
	// clientsMu 保护 clients 的并发访问
	clientsMu sync.RWMutex
	// dispatcher RPC 分发器
	dispatcher *RPCDispatcher
	// upgrader gorilla WebSocket 升级器
	upgrader websocket.Upgrader
	// running 是否正在运行
	running bool
	// runningMu 保护 running 的并发访问
	runningMu sync.RWMutex
	// onMessageCb 入站消息回调
	onMessageCb func(*schema.Message)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultWebHost 默认监听地址
	defaultWebHost = "127.0.0.1"
	// defaultWebPort 默认监听端口
	defaultWebPort = 19000
	// defaultWSPath 默认 WebSocket 路径
	defaultWSPath = "/ws"
	// wsReadLimit WebSocket 消息大小限制（8MB）
	wsReadLimit = 8 * 1024 * 1024
	// protocolVersion 帧协议版本
	protocolVersion = "1.0"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWebChannel 创建 Web 通道实例。
//
// 初始化 RPCDispatcher、WebSocket Upgrader 和连接管理。
func NewWebChannel(cfg WebChannelConfig) *WebChannel {
	// 填充默认值
	if cfg.Host == "" {
		cfg.Host = defaultWebHost
	}
	if cfg.Port == 0 {
		cfg.Port = defaultWebPort
	}
	if cfg.Path == "" {
		cfg.Path = defaultWSPath
	}

	wc := &WebChannel{
		config:  cfg,
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: wsorigin.GorillaCheckOrigin(),
		},
	}

	// 创建事件推送回调：向所有客户端广播事件帧
	sendEvent := func(event string, payload map[string]any) {
		wc.broadcastEvent(event, payload)
	}

	wc.dispatcher = NewAppRPCHandlers(sendEvent)

	return wc
}

// HandleWebSocket 处理 WebSocket 连接。
//
// 升级 HTTP 连接为 WebSocket，发送 connection.ack，进入消息读取循环。
// 对齐 Python WebChannel._handle_connection 逻辑。
func (wc *WebChannel) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := wc.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Msg("WebSocket 升级失败")
		return
	}

	// 设置消息大小限制
	conn.SetReadLimit(wsReadLimit)

	// 加入连接集合
	wc.clientsMu.Lock()
	wc.clients[conn] = true
	wc.clientsMu.Unlock()

	logger.Info(logComponent).
		Str("event_type", "ws_connected").
		Str("remote_addr", conn.RemoteAddr().String()).
		Int("total_clients", wc.clientCount()).
		Msg("WebSocket 客户端已连接")

	// 发送 connection.ack 事件
	sessionID := MakeSessionID()
	ackPayload := map[string]any{
		"session_id":       sessionID,
		"mode":             "BUILD",
		"tools":            []any{},
		"protocol_version": protocolVersion,
	}
	ackFrame := NewEventFrame("connection.ack", ackPayload, 0, "")
	data, _ := ackFrame.Encode()
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.Error(logComponent).
			Err(err).
			Msg("发送 connection.ack 失败")
	}

	// 消息读取循环
	defer func() {
		// 连接断开：从集合移除
		wc.clientsMu.Lock()
		delete(wc.clients, conn)
		wc.clientsMu.Unlock()
		conn.Close()

		logger.Info(logComponent).
			Str("event_type", "ws_disconnected").
			Str("remote_addr", conn.RemoteAddr().String()).
			Int("total_clients", wc.clientCount()).
			Msg("WebSocket 客户端已断开")
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				logger.Warn(logComponent).
					Err(err).
					Msg("WebSocket 读取异常")
			}
			return
		}

		// 解析请求帧
		req, err := DecodeReqFrame(message)
		if err != nil {
			wc.writeError(conn, "", "JSON 解码失败: "+err.Error(), WsErrBadRequest)
			continue
		}

		// 校验帧
		if err := req.Validate(); err != nil {
			wc.writeError(conn, req.ID, err.Error(), WsErrBadRequest)
			continue
		}

		// 解析 params
		var params map[string]any
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params) // 解析失败使用空 map
		}
		if params == nil {
			params = make(map[string]any)
		}

		// 自动生成 session_id
		sid := sessionID
		if s, ok := params["session_id"].(string); ok && s != "" {
			sid = s
		}

		// 分发 RPC 请求
		result, err := wc.dispatcher.Dispatch(req.Method, params, sid)
		if err != nil {
			code := WsErrInternalError
			if strings.Contains(err.Error(), "未找到") {
				code = WsErrMethodNotFound
			}
			wc.writeError(conn, req.ID, err.Error(), code)
			continue
		}

		// 写入成功响应
		res := NewResFrame(req.ID, true, result, "", "")
		resData, _ := res.Encode()
		if err := conn.WriteMessage(websocket.TextMessage, resData); err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("method", req.Method).
				Msg("写入响应失败")
		}
	}
}

// Config 返回当前通道配置。
func (wc *WebChannel) Config() any {
	return wc.config
}

// Start 启动 Web 通道。
//
// 设置 running 标志。HTTP 服务器由 GatewayServer 启动。
func (wc *WebChannel) Start(_ context.Context) error {
	wc.runningMu.Lock()
	defer wc.runningMu.Unlock()

	wc.running = true
	logger.Info(logComponent).
		Str("host", wc.config.Host).
		Int("port", wc.config.Port).
		Str("path", wc.config.Path).
		Msg("WebChannel 已启动")
	return nil
}

// Stop 停止 Web 通道，关闭所有客户端连接。
func (wc *WebChannel) Stop(_ context.Context) error {
	wc.runningMu.Lock()
	wc.running = false
	wc.runningMu.Unlock()

	// 关闭所有客户端连接
	wc.clientsMu.Lock()
	for conn := range wc.clients {
		conn.Close()
	}
	wc.clients = make(map[*websocket.Conn]bool)
	wc.clientsMu.Unlock()

	logger.Info(logComponent).Msg("WebChannel 已停止")
	return nil
}

// Send 向所有客户端广播消息。
//
// 将 schema.Message 转为事件帧推送到所有 WebSocket 连接。
func (wc *WebChannel) Send(_ context.Context, msg *schema.Message) error {
	payload := map[string]any{
		"session_id": msg.SessionID,
		"event_type": string(msg.EventType),
	}
	if msg.OK {
		payload["ok"] = true
	}

	wc.broadcastEvent(string(msg.EventType), payload)
	return nil
}

// OnMessage 注册入站消息回调。
func (wc *WebChannel) OnMessage(callback func(*schema.Message)) {
	wc.onMessageCb = callback
}

// IsRunning 返回通道是否正在运行。
func (wc *WebChannel) IsRunning() bool {
	wc.runningMu.RLock()
	defer wc.runningMu.RUnlock()
	return wc.running
}

// ChannelID 返回通道唯一标识。
func (wc *WebChannel) ChannelID() string {
	return "web"
}

// ChannelType 返回通道类型。
func (wc *WebChannel) ChannelType() channel_manager.ChannelType {
	return channel_manager.ChannelTypeWeb
}

// Addr 返回监听地址字符串。
func (wc *WebChannel) Addr() string {
	return fmt.Sprintf("%s:%d", wc.config.Host, wc.config.Port)
}

// ClientCount 返回当前连接的客户端数量。
func (wc *WebChannel) ClientCount() int {
	return wc.clientCount()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// broadcastEvent 向所有已连接客户端广播事件帧。
func (wc *WebChannel) broadcastEvent(event string, payload map[string]any) {
	frame := NewEventFrame(event, payload, 0, "")
	data, err := frame.Encode()
	if err != nil {
		logger.Error(logComponent).
			Str("event", event).
			Err(err).
			Msg("编码事件帧失败")
		return
	}

	wc.clientsMu.RLock()
	defer wc.clientsMu.RUnlock()

	for conn := range wc.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			logger.Warn(logComponent).
				Str("event", event).
				Err(err).
				Msg("广播事件到客户端失败")
		}
	}
}

// writeError 写入错误响应帧。
func (wc *WebChannel) writeError(conn *websocket.Conn, reqID, errMsg, code string) {
	res := NewResFrame(reqID, false, make(map[string]any), errMsg, code)
	data, _ := res.Encode()
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.Error(logComponent).
			Err(err).
			Msg("写入错误响应失败")
	}
}

// clientCount 返回当前客户端连接数。
func (wc *WebChannel) clientCount() int {
	wc.clientsMu.RLock()
	defer wc.clientsMu.RUnlock()
	return len(wc.clients)
}
