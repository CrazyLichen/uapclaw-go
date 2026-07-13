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
	cm "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
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

// ConnectHook 连接建立钩子函数。
//
// 对齐 Python WebChannel._connect_hooks 中的 ConnectHook 签名。
type ConnectHook func(conn *websocket.Conn) error

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
	// onConfigSavedCb 配置保存回调
	onConfigSavedCb OnConfigSavedFunc
	// channelMgr 通道管理器（fallback 路由用）
	channelMgr *cm.ChannelManager
	// connectHooks 连接建立钩子列表
	connectHooks []ConnectHook
	// connectHooksMu 保护 connectHooks 的并发访问
	connectHooksMu sync.RWMutex
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
func NewWebChannel(cfg WebChannelConfig, channelMgr *cm.ChannelManager, onConfigSaved OnConfigSavedFunc) *WebChannel {
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
		channelMgr:      channelMgr,
	}

	return wc
}

// HandleWebSocket 处理 WebSocket 连接。
//
// 升级 HTTP 连接为 WebSocket，触发 onConnect 钩子，进入消息读取循环。
// 对齐 Python WebChannel._connection_handler 逻辑。
func (wc *WebChannel) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// path 校验（对齐 Python _connection_handler 中 URL path 检查）
	if r.URL.Path != wc.config.Path {
		http.Error(w, fmt.Sprintf("unsupported path: %s", r.URL.Path), http.StatusNotFound)
		return
	}

	// 解析 query 参数
	query := r.URL.Query()

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

	// 触发 onConnect 钩子（对齐 Python _connection_handler 中 _connect_hooks 遍历）
	wc.connectHooksMu.RLock()
	hooks := make([]ConnectHook, len(wc.connectHooks))
	copy(hooks, wc.connectHooks)
	wc.connectHooksMu.RUnlock()
	for _, hook := range hooks {
		if err := hook(conn); err != nil {
			logger.Warn(logComponent).
				Err(err).
				Msg("onConnect 钩子执行失败")
		}
	}

	// 生成默认 session_id（对齐 Python _handle_raw_message 中的 _make_session_id）
	defaultSessionID := MakeSessionID()

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
		sid := defaultSessionID
		if s, ok := params["session_id"].(string); ok && s != "" {
			sid = s
		}

		// 处理文件下载（对齐 Python _process_files）
		params = processFiles(params)

		// ─── 两层消息架构（对齐 Python _handle_raw_message）───
		// 第一层：构建 user_message 并通过 onMessageCb（normAndForward）转发
		handledByCallback := false
		if wc.onMessageCb != nil {
			userMessage := BuildUserMessage(req.ID, req.Method, params, sid, query)
			handledByCallback = wc.onMessageCb(userMessage)
		} else if wc.channelMgr != nil {
			// fallback: 对齐 Python self.bus.publish_user_messages
			userMessage := BuildUserMessage(req.ID, req.Method, params, sid, query)
			wc.channelMgr.DeliverToMessageHandler(userMessage)
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

// OnConnect 注册连接建立钩子。
//
// 对齐 Python WebChannel.on_connect(callback)，
// 新客户端接入时依次调用所有已注册的钩子。
func (wc *WebChannel) OnConnect(callback ConnectHook) {
	wc.connectHooksMu.Lock()
	defer wc.connectHooksMu.Unlock()
	wc.connectHooks = append(wc.connectHooks, callback)
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

	// 响应消息：构造完整 res 帧（对齐 Python L319-341）
	if msg.Type == schema.MessageTypeRes {
		var resPayload map[string]any
		if msg.Payload != nil {
			// 浅拷贝，避免修改原始 payload
			resPayload = make(map[string]any, len(msg.Payload))
			for k, v := range msg.Payload {
				resPayload[k] = v
			}
		} else {
			resPayload = make(map[string]any)
		}

		res := NewResFrame(msg.ID, msg.OK, resPayload, "", "")
		if !msg.OK {
			// ok=false 时从 payload 提取 error/code 到顶层
			if errText, ok := resPayload["error"].(string); ok && errText != "" {
				res.Error = errText
			}
			if codeText, ok := resPayload["code"].(string); ok && codeText != "" {
				res.Code = codeText
			}
		}
		resData, _ := res.Encode()
		wc.broadcastRaw(resData)
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
		payload := extractPureTextPayload(msg, eventName)
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
func (wc *WebChannel) ChannelType() cm.ChannelType {
	return cm.ChannelTypeWeb
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

	wc.broadcastRaw(data)
}

// broadcastRaw 向所有已连接客户端广播原始字节数据。
func (wc *WebChannel) broadcastRaw(data []byte) {
	wc.clientsMu.RLock()
	defer wc.clientsMu.RUnlock()

	for conn := range wc.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			logger.Warn(logComponent).
				Err(err).
				Msg("广播原始帧到客户端失败")
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
// 仅提取 content + session_id + role + member_name + cron(error fallback)
func extractPureTextPayload(msg *schema.Message, eventName string) map[string]any {
	payload := map[string]any{
		"session_id": msg.SessionID,
	}

	if msg.Payload != nil {
		// content 提取 + error fallback（对齐 Python L374-376）
		if content, ok := msg.Payload["content"]; ok {
			contentStr := fmt.Sprintf("%v", content)
			if contentStr != "" && contentStr != "<nil>" {
				payload["content"] = content
			} else if !msg.OK {
				if errVal, ok := msg.Payload["error"]; ok {
					payload["content"] = errVal
				}
			}
		} else if !msg.OK {
			// 无 content 且 ok=false，从 error 提取
			if errVal, ok := msg.Payload["error"]; ok {
				payload["content"] = errVal
			}
		}
		// 角色 + 成员名
		if role, ok := msg.Payload["role"]; ok {
			payload["role"] = role
		}
		if memberName, ok := msg.Payload["member_name"]; ok {
			payload["member_name"] = memberName
		}
		// cron 字段（对齐 Python L387-390）
		if eventName == "chat.final" {
			if cron, ok := msg.Payload["cron"]; ok {
				if _, isDict := cron.(map[string]any); isDict {
					payload["cron"] = cron
				}
			}
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
