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
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
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
	// onMessageCb 入站消息回调（返回 true 表示已处理，短路后续 handler）
	onMessageCb func(*schema.Message) bool
	// agentClient AgentServer 客户端（nil 表示无需等待，直接发 connection.ack）
	agentClient *routing.AgentClient
	// onConfigSavedCb 配置保存回调
	onConfigSavedCb OnConfigSavedFunc
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
// onMessage 通过 RegisterChannelWithInbound 设置，构造时不注入。
// 对齐 Python: WebChannel.__init__ + _register_web_handlers(bind)。
func NewWebChannel(cfg WebChannelConfig, onConfigSaved OnConfigSavedFunc) *WebChannel {
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
		onConfigSavedCb: onConfigSaved,
	}

	// 创建事件推送回调：向所有客户端广播事件帧
	sendEvent := func(event string, payload map[string]any) {
		wc.broadcastEvent(event, payload)
	}

	// 注册 RPC handlers（消息转发由两层架构第一层处理，本地 handler 仅返回 ack）
	wc.dispatcher = RegisterWebHandlers(sendEvent, onConfigSaved)

	return wc
}

// HandleWebSocket 处理 WebSocket 连接。
//
// 升级 HTTP 连接为 WebSocket，等待 AgentServer 就绪后发送 connection.ack，进入消息读取循环。
// 对齐 Python WebChannel._handle_connection 逻辑（_on_connect 中检查 server_ready）。
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

	// 等待 AgentServer 就绪后再发 connection.ack
	// 对齐 Python: _on_connect 中检查 agent_client.server_ready
	if wc.agentClient != nil {
		if !wc.agentClient.WaitServerReady(r.Context()) {
			logger.Warn(logComponent).Msg("AgentServer 未就绪，关闭 WebSocket 连接")
			_ = conn.Close()
			wc.clientsMu.Lock()
			delete(wc.clients, conn)
			wc.clientsMu.Unlock()
			return
		}
	}

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
		_ = conn.Close()

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

		// ─── 两层消息架构（对齐 Python _handle_raw_message）───
		// 第一层：构建 user_message 并通过 onMessageCb（normAndForward）转发
		handledByCallback := false
		if wc.onMessageCb != nil {
			userMessage := BuildUserMessage(req.ID, req.Method, params, sid)
			handledByCallback = wc.onMessageCb(userMessage)
		}

		if handledByCallback {
			// handledByCallback=true 时短路后续本地 handler
			// 对齐 Python: if handled_by_callback: return
			continue
		}

		// 第二层：本地 method handler（返回 ack 响应给客户端）
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
		_ = conn.Close()
	}
	wc.clients = make(map[*websocket.Conn]bool)
	wc.clientsMu.Unlock()

	logger.Info(logComponent).Msg("WebChannel 已停止")
	return nil
}

// Send 向所有客户端广播消息，根据事件类型选择 full-payload 或 pure-text 路由。
//
// 对齐 Python web_connect.py send() (L313-L414)：
//   - msg.type == "res" → 构造 res 帧
//   - 确定事件名（默认 chat.final，优先 msg.EventType，fallback payload.event_type）
//   - full-payload 事件：透传完整 payload
//   - pure-text 事件：提取 content + session_id + role + member_name
//   - interrupt_result 副作用：自动广播 processing_status
func (wc *WebChannel) Send(_ context.Context, msg *schema.Message) error {
	if msg == nil {
		return nil
	}

	// 响应消息：构造 res 帧（后续通过 RPC response 路径发送，此处为 fallback）
	if msg.Type == schema.MessageTypeRes {
		payload := msg.Payload
		if payload == nil {
			payload = make(map[string]any)
		}
		payload["session_id"] = msg.SessionID
		wc.broadcastEvent(string(msg.EventType), payload)
		return nil
	}

	// 确定事件名
	eventName := determineEventName(msg)

	// 判断是否为 full-payload 事件
	if isFullPayloadEvent(eventName) {
		// full-payload：透传完整 payload
		payload := msg.Payload
		if payload == nil {
			payload = make(map[string]any)
		}
		// 确保 session_id 存在
		if _, ok := payload["session_id"]; !ok {
			payload["session_id"] = msg.SessionID
		}
		wc.broadcastEvent(eventName, payload)
	} else {
		// pure-text：提取核心字段
		payload := extractPureTextPayload(msg)
		wc.broadcastEvent(eventName, payload)
	}

	// interrupt_result 副作用：自动广播 processing_status
	if eventName == string(schema.EventTypeChatInterruptResult) {
		wc.broadcastInterruptSideEffect(msg)
	}

	return nil
}

// OnMessage 注册入站消息回调。
//
// 对齐 Python BaseChannel.on_message，返回 true 表示已处理。
func (wc *WebChannel) OnMessage(callback func(*schema.Message) bool) {
	wc.onMessageCb = callback
}

// SetAgentClient 设置 AgentServer 客户端。
//
// 在 HandleWebSocket 中等待 AgentServer 就绪后再发送 connection.ack，
// 对齐 Python: _on_connect 中检查 agent_client.server_ready。
func (wc *WebChannel) SetAgentClient(client *routing.AgentClient) {
	wc.agentClient = client
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

// determineEventName 确定消息的事件名
//
// 优先级：msg.EventType → payload.event_type → 默认 chat.final
func determineEventName(msg *schema.Message) string {
	if msg.EventType != "" {
		return string(msg.EventType)
	}
	if msg.Payload != nil {
		if et, ok := msg.Payload["event_type"]; ok {
			if s, isStr := et.(string); isStr && s != "" {
				return s
			}
		}
	}
	return "chat.final"
}

// isFullPayloadEvent 判断事件是否需要透传完整 payload
//
// 对齐 Python web_connect.py send() 中的 full-payload 事件列表
func isFullPayloadEvent(eventName string) bool {
	// full-payload 事件白名单
	fullPayloadEvents := map[string]bool{
		"connection.ack":            true,
		"todo.updated":              true,
		"chat.tool_call":            true,
		"chat.tool_result":          true,
		"chat.processing_status":    true,
		"chat.interrupt_result":     true,
		"chat.evolution_status":     true,
		"chat.error":                true,
		"heartbeat.relay":           true,
		"context.usage":             true,
		"context.compression_state": true,
		"chat.ask_user_question":    true,
		"chat.subtask_update":       true,
		"history.message":           true,
		"chat.session_result":       true,
		"chat.usage_metadata":       true,
		"chat.usage_summary":        true,
		"chat.file":                 true,
	}

	if fullPayloadEvents[eventName] {
		return true
	}

	// 通配符前缀匹配：team.*, harness.*
	if strings.HasPrefix(eventName, "team.") || strings.HasPrefix(eventName, "harness.") {
		return true
	}

	return false
}

// extractPureTextPayload 提取纯文本事件的核心字段
//
// 对齐 Python web_connect.py send() 中的 pure-text 路径：
// 仅提取 content + session_id + role + member_name
func extractPureTextPayload(msg *schema.Message) map[string]any {
	payload := map[string]any{
		"session_id": msg.SessionID,
	}

	if msg.Payload != nil {
		if content, ok := msg.Payload["content"]; ok {
			payload["content"] = content
		}
		if role, ok := msg.Payload["role"]; ok {
			payload["role"] = role
		}
		if memberName, ok := msg.Payload["member_name"]; ok {
			payload["member_name"] = memberName
		}
	}

	return payload
}

// broadcastInterruptSideEffect interrupt_result 事件的副作用：
// 自动广播 processing_status 事件
//
// 对齐 Python web_connect.py send() 中 interrupt_result 后的处理：
//   - intent=pause/supplement/resume → is_processing=true
//   - intent=cancel → is_processing=false
func (wc *WebChannel) broadcastInterruptSideEffect(msg *schema.Message) {
	isProcessing := true // 默认 true（pause/supplement/resume）
	if msg.Payload != nil {
		if intent, ok := msg.Payload["intent"]; ok {
			if s, isStr := intent.(string); isStr && s == "cancel" {
				isProcessing = false
			}
		}
	}

	statusPayload := map[string]any{
		"is_processing": isProcessing,
		"session_id":    msg.SessionID,
	}
	wc.broadcastEvent("chat.processing_status", statusPayload)
}
