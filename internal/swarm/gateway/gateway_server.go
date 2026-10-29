package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/wsorigin"
	"github.com/uapclaw/uapclaw-go/internal/common/wswrite"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GatewayServer 通用多路由 WebSocket Gateway Server。
//
// 对齐 Python app_gateway.py GatewayServer 类（L240-720）：
// 支持多个路径（如 /acp、/tui），每条路径有独立 channel_id 和本地 handler；
// 本地 handler 优先处理请求，未处理或无匹配则 forward 到 MessageHandler。
//
// acpBridge ⤵️ 11.25：AcpGatewayBridge 注入点（构造注入 dispatchOnMessage/
// bindAcpSessionClient + _install_default_route_hooks + handleRawMessage/send
// 的 acp 分支），A1 空路由形态下不建模。
type GatewayServer struct {
	// config 服务器配置（路由表/监听地址）
	config *GatewayServerConfig
	// bus 对齐 Python router/_DummyBus 占位（Python self.bus 存而不用，
	// 恒传 _DummyBus 占位；Go 以 struct{} 零尺寸占位，不消费）
	bus struct{}
	// httpServer 独立 HTTP 服务器（对齐 Python websockets.serve）
	httpServer *http.Server
	// upgrader gorilla WebSocket 升级器
	upgrader websocket.Upgrader
	// running 运行状态
	running atomic.Bool
	// onMessageCb 入站消息回调（对齐 _on_message_cb）
	onMessageCb func(ctx context.Context, msg *schema.Message) error
	// clientsMu 保护以下三张表
	clientsMu sync.RWMutex
	// clients 在线连接集合（对齐 _clients set）
	clients map[*websocket.Conn]struct{}
	// requestToClient 请求路由表：(channel, reqID) → ws
	requestToClient map[[2]string]*websocket.Conn
	// sessionToClient 会话路由表：(channel, sessionID) → ws
	sessionToClient map[[2]string]*websocket.Conn
	// acpBridge ACP 桥（/acp 路由协议对端，⤴️ 11.25 回填；nil = 未装配）
	acpBridge *AcpGatewayBridge
	// wg 连接 handler 生命周期
	wg sync.WaitGroup
	// listenErr 启动错误回传 channel
	listenErr chan error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// gwReadLimit WebSocket 消息大小限制（8MB，对齐 Python ws_max_size 与 web_connect.go 先例）
const gwReadLimit = 8 * 1024 * 1024

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGatewayServer 创建多路由 WS 服务器实例。
//
// Python: GatewayServer.__init__（L240-262）——router 在 Python 侧仅作占位传递
// （_DummyBus），Go 侧以 bus 字段 struct{} 零尺寸占位，不消费。
func NewGatewayServer(config *GatewayServerConfig) *GatewayServer {
	if config == nil {
		config = NewGatewayServerConfig()
	}
	applyConfigFallback(config)
	return &GatewayServer{
		config: config,
		bus:    struct{}{},
		upgrader: websocket.Upgrader{
			CheckOrigin: wsorigin.GorillaCheckOrigin(),
		},
		clients:         make(map[*websocket.Conn]struct{}),
		requestToClient: make(map[[2]string]*websocket.Conn),
		sessionToClient: make(map[[2]string]*websocket.Conn),
		listenErr:       make(chan error, 1),
	}
}

// Start 启动监听（19001）。
//
// Python: GatewayServer.start()（L371-393）——已运行或未启用则直接返回。
func (s *GatewayServer) Start(ctx context.Context) error {
	if s.running.Load() || !s.config.Enabled {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.HandleConnection)
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", s.config.Host, s.config.Port),
		Handler: mux,
	}
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// 非阻塞回传启动错误（对齐 Python serve 失败路径）
			select {
			case s.listenErr <- err:
			default:
			}
			logger.Error(logComponent).
				Err(err).
				Str("addr", s.httpServer.Addr).
				Msg("gateway http server listen failed")
		}
	}()
	s.running.Store(true)

	paths := make([]string, 0, len(s.config.Routes))
	for p := range s.config.Routes {
		paths = append(paths, p)
	}
	logger.Info(logComponent).
		Str("addr", s.httpServer.Addr).
		Str("paths", strings.Join(paths, ", ")).
		Msg("Gateway server started")
	return nil
}

// Stop 停机：关连接 → 清表 → 关监听。
//
// Python: GatewayServer.stop()（L395-410）。
func (s *GatewayServer) Stop() error {
	s.running.Store(false)

	s.clientsMu.Lock()
	clients := make([]*websocket.Conn, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
		// 对齐 Python close(code=1001, reason="server shutdown")
		_ = c.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutdown"),
			time.Now().Add(time.Second))
	}
	s.clients = make(map[*websocket.Conn]struct{})
	s.requestToClient = make(map[[2]string]*websocket.Conn)
	s.sessionToClient = make(map[[2]string]*websocket.Conn)
	s.clientsMu.Unlock()

	for _, c := range clients {
		_ = c.Close()
	}
	if s.httpServer != nil {
		if err := s.httpServer.Close(); err != nil {
			logger.Error(logComponent).Err(err).Msg("failed to close gateway http server")
		}
	}
	s.wg.Wait()
	logger.Info(logComponent).Msg("Gateway server stopped")
	return nil
}

// OnMessage 设置入站消息回调。
//
// Python: GatewayServer.on_message()（L280-283）。
func (s *GatewayServer) OnMessage(cb func(ctx context.Context, msg *schema.Message) error) {
	s.onMessageCb = cb
}

// SetAcpBridge 注入 ACP 桥（装配分离，偏差 §9-10）。
// Python: GatewayServer.__init__ 内构造（L256-259）；Go 由 app_gateway 装配时注入。
func (s *GatewayServer) SetAcpBridge(b *AcpGatewayBridge) {
	s.clientsMu.Lock()
	s.acpBridge = b
	s.clientsMu.Unlock()
}

// InstallDefaultAcpHooks 把桥拦截器装进全部 acp 路由（对齐 _install_default_route_hooks）。
// Python: app_gateway.py L296-302——route.inbound_interceptor = route.inbound_interceptor or bridge.inbound_intercept
// 等三行（已装的不覆盖）。Go 同语义：仅装 nil 槽位。
// 另含 disconnect/cleanup 装填（Python L682 disconnect_handler）。
func (s *GatewayServer) InstallDefaultAcpHooks() {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if s.acpBridge == nil {
		return
	}
	for _, route := range s.config.Routes {
		if route.ChannelID != "acp" {
			continue
		}
		if route.InboundInterceptor == nil {
			route.InboundInterceptor = s.acpBridge.InboundIntercept
		}
		if route.OutboundInterceptor == nil {
			route.OutboundInterceptor = s.acpBridge.OutboundIntercept
		}
		if route.CleanupHandler == nil {
			route.CleanupHandler = s.acpBridge.Cleanup
		}
	}
}

// HandleConnection HTTP 处理入口：路由分流 → 升级 WS → 读循环。
//
// Python: GatewayServer._connection_handler()（L501-559）。
func (s *GatewayServer) HandleConnection(w http.ResponseWriter, r *http.Request) {
	route, matchedPath := s.resolveRoute(r.URL.Path)
	if route == nil {
		// 未注册路径：HTTP 层直接拒绝（对齐 Python close(1008, "unsupported path")）
		http.Error(w, fmt.Sprintf("unsupported path: %s", r.URL.Path), http.StatusNotFound)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("WebSocket upgrade failed")
		return
	}
	conn.SetReadLimit(gwReadLimit)

	s.registerConn(conn)

	// connection.ack 首帧（对齐 L513-525）
	ack := buildConnectionAckFrame(route.ChannelID)
	if err := s.writeMessage(conn, ack); err != nil {
		logger.Debug(logComponent).
			Err(err).
			Str("channel", route.ChannelID).
			Msg("failed to send connection.ack, discarding client")
		s.clientsMu.Lock()
		delete(s.clients, conn)
		s.clientsMu.Unlock()
		wswrite.Unregister(conn)
		return
	}

	s.wg.Add(1)
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Any("panic", r).Msg("connection handler panicked")
		}
	}()

	// 读循环（对齐 L527-530）。
	// ⚠️ 用 context.Background()：gorilla Upgrade 劫持连接后 r.Context()
	// 随原始请求结束而取消，不能作为消息处理链的 ctx 传播
	// M-02：handleRawMessage 返回 true（on_message 回调出错）时断开——
	// 对齐 Python 异常逃出 async for → websockets 关闭该连接的语义
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			// 对齐 ConnectionClosedError → logger.info
			logger.Info(logComponent).
				Str("channel", route.ChannelID).
				Msg("WebSocket connection closed")
			break
		}
		if s.handleRawMessage(context.Background(), conn, raw, matchedPath, route) {
			// 回调故障快速失败：主动关闭连接（走下方 finally 清理链）
			_ = conn.Close()
			break
		}
	}

	// finally 语义（对齐 L532-559）：清集合 + 清 stale 路由键 + 断连回调
	staleSessionKeys := s.unregisterConn(conn)

	if route.DisconnectHandler != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Warn(logComponent).
						Any("panic", r).
						Str("path", r2s(route)).
						Msg("GatewayServer disconnect handler failed")
				}
			}()
			route.DisconnectHandler(conn, staleSessionKeys)
		}()
	} else if route.CleanupHandler != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Warn(logComponent).
						Any("panic", r).
						Msg("GatewayServer cleanup handler failed")
				}
			}()
			route.CleanupHandler(conn)
		}()
	}
}

// Send 出站消息派发（实现 ExternalChannel 接口）。
//
// Python: GatewayServer.send()（L412-499）——四段路由：
// request 定向 → session 定向 → 广播 fallback → 帧发送。
// acp 分支 ⤵️ 11.25（L468-471）。
// 裸键回退不实现：Python L417-418/L427-429 的 get(key[1]) 是 dict 对 tuple
// 的意外行为，实际永不命中（键恒为 tuple），属死代码（spec §7-2 有意偏差）。
func (s *GatewayServer) Send(ctx context.Context, msg *schema.Message) error {
	var ws *websocket.Conn
	requestKey, ok := clientRouteKey(msg.ChannelID, msg.ID)
	if ok {
		s.clientsMu.RLock()
		ws = s.requestToClient[requestKey]
		s.clientsMu.RUnlock()
	}
	if ws == nil {
		sessionKey, ok := clientRouteKey(msg.ChannelID, msg.SessionID)
		if ok {
			s.clientsMu.RLock()
			ws = s.sessionToClient[sessionKey]
			s.clientsMu.RUnlock()
		}
	}

	// 定向发送前探测连接存活性（对齐 Python L428 ws.closed 检查）。
	// S-03 修复：区分"未命中"与"命中已死"两种语义（Python L428-451）——
	// 命中路由键但连接已死（TCP 半关闭、ReadMessage 尚未感知的窗口）时
	// Python 静默丢弃（return，仅 warning），绝不广播：路由键明确指向
	// 该死连接的会话，消息不应被同渠道无关客户端收到（防跨客户端消息
	// 泄漏/TUI 串会话输出）；仅"从未命中"（ws is None）才走广播 fallback。
	hitStale := ws != nil && !s.probeWritable(ws)

	if hitStale {
		// 命中路由键但连接已死：静默丢弃（对齐 Python L428/L451 的
		// closed 分支——仅 warning 后 return，不广播）
		logger.Warn(logComponent).
			Str("channel_id", msg.ChannelID).
			Str("session_id", msg.SessionID).
			Str("id", msg.ID).
			Msg("message dropped: routed WebSocket client closed")
		return nil
	}

	// acp 分支（对齐 L453-455：channel_id==acp 时经桥转换；未处理交回通用出站）
	s.clientsMu.RLock()
	bridge := s.acpBridge
	s.clientsMu.RUnlock()
	if msg.ChannelID == "acp" && bridge != nil && ws != nil {
		if bridge.SendMessage(ctx, msg, ws) {
			return nil
		}
	}

	if ws == nil {
		// 广播 fallback（对齐 L423-439）
		if msg.ChannelID != "" && msg.ChannelID != "acp" && msg.Type != schema.MessageTypeRes {
			clients := s.findChannelClients(msg.ChannelID)
			if len(clients) > 0 {
				frame := buildEventFrame(msg)
				data, _ := json.Marshal(frame)
				logger.Info(logComponent).
					Str("channel_id", msg.ChannelID).
					Int("clients", len(clients)).
					Str("id", msg.ID).
					Msg("broadcast fallback")
				for _, c := range clients {
					if err := s.writeMessage(c, data); err != nil {
						logger.Debug(logComponent).
							Err(err).
							Msg("broadcast send failed (client disconnected?)")
					}
				}
				return nil
			}
		}
		logger.Warn(logComponent).
			Str("channel_id", msg.ChannelID).
			Str("session_id", msg.SessionID).
			Str("id", msg.ID).
			Msg("message dropped: no WebSocket client found")
		return nil
	}

	// 出站拦截器（对齐 L443-467：只处理第一条 channel_id 匹配的路由，异常 Warn 继续）
	var interceptor GatewayInterceptorFunc
	s.clientsMu.RLock()
	for _, route := range s.config.Routes {
		if route.ChannelID == msg.ChannelID && route.OutboundInterceptor != nil {
			interceptor = route.OutboundInterceptor
			break
		}
	}
	s.clientsMu.RUnlock()
	if interceptor != nil {
		handled := func() (h bool) {
			defer func() {
				if r := recover(); r != nil {
					logger.Warn(logComponent).
						Any("panic", r).
						Str("channel_id", msg.ChannelID).
						Msg("GatewayServer outbound interceptor failed")
					h = false
				}
			}()
			return interceptor(ws, map[string]any{"raw": msg})
		}()
		if handled {
			return nil
		}
	}

	frame := buildOutboundFrame(msg)
	data, _ := json.Marshal(frame)
	if err := s.writeMessage(ws, data); err != nil {
		logger.Debug(logComponent).
			Err(err).
			Msg("gateway send failed (client disconnected?)")
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeMessage 串行化出站写：经 per-connection 写锁后写 TextMessage。
// [Go 扩展] gorilla/websocket 不允许并发 WriteMessage（触发
// "concurrent write to websocket connection" panic）；Python websockets
// 库 send 天然受事件循环串行化。Go 侧读循环线程（本地 handler ack）与
// ChannelManager 派发线程（定向/广播出站）会并发写同一连接，必须显式互斥。
func (s *GatewayServer) writeMessage(ws *websocket.Conn, data []byte) error {
	return wswrite.Write(ws, data)
}

// registerConn 注册连接：clients 集合 + 进程级写锁表。
func (s *GatewayServer) registerConn(conn *websocket.Conn) {
	s.clientsMu.Lock()
	s.clients[conn] = struct{}{}
	s.clientsMu.Unlock()
	wswrite.Register(conn)
}

// unregisterConn 清理连接：clients 集合 + 进程级写锁表，返回 stale 会话键。
func (s *GatewayServer) unregisterConn(conn *websocket.Conn) [][2]string {
	s.clientsMu.Lock()
	delete(s.clients, conn)
	var staleSessionKeys [][2]string
	for key, c := range s.sessionToClient {
		if c == conn {
			staleSessionKeys = append(staleSessionKeys, key)
			delete(s.sessionToClient, key)
		}
	}
	for key, c := range s.requestToClient {
		if c == conn {
			delete(s.requestToClient, key)
		}
	}
	s.clientsMu.Unlock()
	wswrite.Unregister(conn)
	return staleSessionKeys
}

// r2s 取路由路径（disconnect 日志辅助，避免与 recover 变量名冲突）。
func r2s(route *RouteConfig) string {
	if route == nil {
		return ""
	}
	return route.Path
}

// resolveRoute 按路径解析路由，支持尾斜杠变体。
//
// Python: _resolve_route（L304-316）四分支严格对齐：
// 空串 → (nil, 原值)；精确命中；尾斜杠剥除命中；尾斜杠补全命中；未命中 → (nil, p)。
func (s *GatewayServer) resolveRoute(requestPath string) (*RouteConfig, string) {
	p := strings.TrimSpace(requestPath)
	if p == "" {
		return nil, requestPath
	}
	if rc, ok := s.config.Routes[p]; ok {
		return rc, p
	}
	trimmed := strings.TrimSuffix(p, "/")
	if p != "/" && trimmed != p {
		if rc, ok := s.config.Routes[trimmed]; ok {
			return rc, trimmed
		}
	}
	if !strings.HasSuffix(p, "/") {
		withSlash := p + "/"
		if rc, ok := s.config.Routes[withSlash]; ok {
			return rc, withSlash
		}
	}
	return nil, p
}

// clientRouteKey 构造 (channel, scopedID) 路由键。
//
// Python: _client_route_key（L265-271）——任一空值返回不可用。
func clientRouteKey(channelID, scopedID string) ([2]string, bool) {
	channel := strings.TrimSpace(channelID)
	scope := strings.TrimSpace(scopedID)
	if channel == "" || scope == "" {
		return [2]string{}, false
	}
	return [2]string{channel, scope}, true
}

// findChannelClients 找到指定渠道的全部在线客户端。
//
// Python: _find_channel_clients（L273-278）。
// ⚠️ 有意偏差（spec §7-3）：不判 closed，直接返回，发送失败由调用方忽略
// （Python 广播 gather(return_exceptions=True) 本就逐 client 容错，行为等价）。
func (s *GatewayServer) findChannelClients(channelID string) []*websocket.Conn {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	var out []*websocket.Conn
	for key, c := range s.sessionToClient {
		if key[0] == channelID {
			out = append(out, c)
		}
	}
	return out
}

// handleRawMessage 单帧入站处理。
//
// Python: _handle_raw_message（L561-720）三层处理：
// 拦截器 → forward 集合 → 本地 handler。
// acp 分支 ⤵️ 11.25（L573-579 的 acp_bridge.handle_jsonrpc_request/is_jsonrpc_request
// 与 L665-668 的 acp early-return）。
// M-02：返回 true 表示 on_message 回调出错、须断开该连接（对齐 Python
// 异常传播语义——_handle_raw_message 无 except，异常逃出 connection_handler
// 的 async for → websockets 框架关闭该连接）；false 正常。
func (s *GatewayServer) handleRawMessage(ctx context.Context, ws *websocket.Conn, raw []byte, requestPath string, route *RouteConfig) bool {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		s.sendInlineError(ws, "", "invalid json")
		return false
	}

	// acp JSON-RPC 分支（对齐 L575-581：route.channel_id==acp 时优先交桥处理，
	// 请求未处理但 is_jsonrpc_request 为真时早退——顺序在入站拦截器之前）
	if route.ChannelID == "acp" {
		s.clientsMu.RLock()
		acpB := s.acpBridge
		s.clientsMu.RUnlock()
		if acpB != nil {
			if acpB.HandleJsonrpcRequest(ctx, ws, data) {
				return
			}
			if IsJsonrpcRequest(data) {
				// 是 JSON-RPC 请求但桥未处理（理论上不可达——HandleJsonrpcRequest 对
				// 请求恒返回 true）——防御性 return，避免落入 type=req 的 invalid request
				return
			}
		}
	}

	// 入站拦截器（对齐 L584-596，异常 Warn 继续）
	if route.InboundInterceptor != nil {
		handled := func() (h bool) {
			defer func() {
				if r := recover(); r != nil {
					logger.Warn(logComponent).
						Any("panic", r).
						Str("path", requestPath).
						Msg("GatewayServer inbound interceptor failed")
					h = false
				}
			}()
			return route.InboundInterceptor(ws, data)
		}()
		if handled {
			return false
		}
	}

	if data["type"] != "req" {
		s.sendInlineError(ws, "", "invalid request")
		return false
	}
	reqID := strings.TrimSpace(strVal(data["id"]))
	method := strings.TrimSpace(strVal(data["method"]))
	if reqID == "" || method == "" {
		s.sendInlineError(ws, reqID, "invalid request")
		return false
	}
	params, _ := data["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	sessionID := strings.TrimSpace(strVal(params["session_id"]))
	if sessionID == "" {
		sessionID = reqID // 对齐 L619 回退
	}

	// 1. forward 集合（对齐 L621-671）
	if route.ForwardMethods[method] {
		reqMethod, err := schema.ParseReqMethod(method)
		if err != nil {
			s.sendInlineError(ws, reqID, fmt.Sprintf("unknown method: %s", method))
			return false
		}

		// 注册路由键（对齐 L634-640）
		s.clientsMu.Lock()
		if key, ok := clientRouteKey(route.ChannelID, reqID); ok {
			s.requestToClient[key] = ws
		}
		if key, ok := clientRouteKey(route.ChannelID, sessionID); ok {
			s.sessionToClient[key] = ws
		}
		s.clientsMu.Unlock()

		// default_mode：tui → code.normal，其余 → agent.plan（对齐 L645）
		defaultMode := schema.ModeAgentPlan
		if route.ChannelID == "tui" {
			defaultMode = schema.ModeCodeNormal
		}
		mode := schema.ParseMode(strVal(params["mode"]), defaultMode)

		// params 拷贝 + setdefault("mode")（对齐 L649-651）
		paramsCopy := make(map[string]any, len(params))
		for k, v := range params {
			paramsCopy[k] = v
		}
		if _, has := paramsCopy["mode"]; !has {
			paramsCopy["mode"] = mode.String()
		}

		// metadata：method + 可选 cwd（对齐 L653-657）
		metadata := map[string]any{"method": method}
		if cwd, ok := params["cwd"].(string); ok && strings.TrimSpace(cwd) != "" {
			metadata["cwd"] = strings.TrimSpace(cwd)
		}

		isStream, _ := data["is_stream"].(bool)
		paramsJSON, _ := json.Marshal(paramsCopy)

		msg := &schema.Message{
			ID:        reqID,
			Type:      schema.MessageTypeReq,
			ChannelID: route.ChannelID,
			SessionID: sessionID,
			Params:    paramsJSON,
			Timestamp: schema.NowTimestamp(),
			OK:        true,
			ReqMethod: reqMethod,
			Mode:      mode,
			Metadata:  metadata,
			IsStream:  isStream,
		}

		if s.onMessageCb != nil {
			if err := s.onMessageCb(ctx, msg); err != nil {
				// M-02 修复：对齐 Python await 无 except 的真实语义——
				// 回调异常向上传播（_handle_raw_message → connection_handler
				// 的 async for → 逃出 handler → websockets 框架关闭该连接）。
				// 入站管线故障快速失败，避免客户端"消息静默黑洞"。
				logger.Error(logComponent).
					Err(err).
					Str("msg_id", msg.ID).
					Msg("gateway on_message callback failed, closing connection")
				return true
			}
		}

		// acp early-return（对齐 L665-668：ACP route 收到 legacy type=req 帧时
		// 转发后直接返回，不落入 unknown method / 无本地 ack 路径）
		if route.ChannelID == "acp" {
			return
		}

		// 仅转发集合 → 无本地 ack（对齐 L670-671）
		if route.ForwardNoLocalHandlerMethods[method] {
			return false
		}
	}

	// 2. 本地 handler（对齐 L674-707）
	if handler, ok := route.LocalHandlers[method]; ok {
		if err := handler(ws, reqID, params, sessionID); err != nil {
			// ws 已关判定（写探针对齐 getattr(ws,"closed")）
			if !s.probeWritable(ws) {
				logger.Warn(logComponent).
					Str("method", method).
					Err(err).
					Msg("GatewayServer local handler aborted on closed ws")
				return false
			}
			logger.Error(logComponent).
				Str("method", method).
				Err(err).
				Msg("GatewayServer local handler error")
			s.sendResponse(ws, reqID, false, nil, fmt.Sprintf("handler error: %s", err), "INTERNAL_ERROR")
		}
		return false
	}

	// 3. 无 forward 无本地（对齐 L710-717）
	s.sendInlineError(ws, reqID, fmt.Sprintf("unknown method: %s", method))
	return false
}

// probeWritable 写探针：判断 ws 是否仍可写（对齐 Python getattr(ws,"closed")）。
// [Go 扩展] 探针同样写连接（Ping + SetWriteDeadline），必须与数据帧写点
// 共用连接写锁，否则与本地 ack / 出站派发并发时触发 data race。
func (s *GatewayServer) probeWritable(ws *websocket.Conn) bool {
	writable := false
	wswrite.Lock(ws, func() {
		_ = ws.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
		defer func() { _ = ws.SetWriteDeadline(time.Time{}) }()
		writable = ws.WriteMessage(websocket.PingMessage, nil) == nil
	})
	return writable
}

// sendResponse 发送 res 帧。
//
// Python: send_response（L332-356）——发送失败仅 Debug。
// handler 错误路径帧形状带 payload（payload or {}）。
func (s *GatewayServer) sendResponse(ws *websocket.Conn, reqID string, ok bool, payload map[string]any, errMsg, code string) {
	writeResponseFrame(ws, reqID, ok, payload, errMsg, code, true)
}

// sendInlineError 发送内联错误帧（invalid json/invalid request/unknown method）。
//
// M-01 修复：Python 内联错误帧（L569/L601/L613/L632/L717）只有
// type/id/ok/error 四键、无 payload——与 send_response（本地 handler
// 错误帧带 payload or {}）是两套帧形状，Go 此前统一带 "payload": {}。
func (s *GatewayServer) sendInlineError(ws *websocket.Conn, reqID string, errMsg string) {
	writeResponseFrame(ws, reqID, false, nil, errMsg, "", false)
}

// writeResponseFrame 构造并写入 res 帧（包级，供 tui 本地 handler 复用）。
//
// 零行为变更：帧构造与写入逻辑自 sendResponse 提取（11.15 最小装配）。
// M-01：includePayload 控制是否带 payload 键——内联错误帧传 false
// 对齐 Python 内联四键形状；sendResponse/handler 错误路径传 true。
func writeResponseFrame(ws *websocket.Conn, reqID string, ok bool, payload map[string]any, errMsg, code string, includePayload bool) {
	frame := map[string]any{
		"type": "res",
		"id":   reqID,
		"ok":   ok,
	}
	if includePayload {
		if payload == nil {
			payload = map[string]any{}
		}
		frame["payload"] = payload
	}
	if !ok {
		if errMsg == "" {
			errMsg = "request failed"
		}
		frame["error"] = errMsg
		if code != "" {
			frame["code"] = code
		}
	}
	data, _ := json.Marshal(frame)
	wswrite.Lock(ws, func() {
		if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
			logger.Debug(logComponent).
				Err(err).
				Str("method", "send_response").
				Msg("send failed (client disconnected?)")
		}
	})
}

// sendEvent 发送 event 帧（供本地 handler 使用，11.15 TUI 消费）。
//
// Python: send_event（L358-369）——发送失败仅 Debug。
func (s *GatewayServer) sendEvent(ws *websocket.Conn, event string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	frame := map[string]any{
		"type":    "event",
		"event":   event,
		"payload": payload,
	}
	data, _ := json.Marshal(frame)
	wswrite.Lock(ws, func() {
		if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
			logger.Debug(logComponent).
				Err(err).
				Str("method", "send_event").
				Msg("send failed (client disconnected?)")
		}
	})
}

// buildConnectionAckFrame 构建 connection.ack 首帧。
//
// ⚠️ 与 transport.BuildConnectionAckFrame（AgentServer 侧 {status:ready}）格式不同，
// 不可复用（spec §7-8 有意偏差）。
// Python: _connection_handler L513-521 内联 json.dumps。
func buildConnectionAckFrame(channelID string) []byte {
	frame := map[string]any{
		"type":  "event",
		"event": "connection.ack",
		"payload": map[string]any{
			"protocol_version": "1.0",
			"transport":        channelID,
		},
	}
	data, _ := json.Marshal(frame)
	return data
}

// buildEventFrame 构建出站 event 帧（广播 fallback 与定向共用）。
//
// Python: send() 尾段（L478-499）+ 广播 fallback 的 _build_event_frame。
// payload 为 nil 时只带 session_id（Python str(payload or "") 分支在 Go 侧
// msg.Payload 为 map 类型，nil 时 str("") 无意义，简化省略 content 字段）。
func buildEventFrame(msg *schema.Message) map[string]any {
	eventName := "chat.final"
	if msg.EventType != "" {
		eventName = msg.EventType.String()
	}
	var payload map[string]any
	if msg.Payload != nil {
		payload = make(map[string]any, len(msg.Payload))
		for k, v := range msg.Payload {
			payload[k] = v
		}
		if _, has := payload["session_id"]; !has {
			payload["session_id"] = msg.SessionID
		}
	} else {
		payload = map[string]any{"session_id": msg.SessionID}
	}
	return map[string]any{
		"type":    "event",
		"event":   eventName,
		"payload": payload,
	}
}

// buildOutboundFrame 构建定向出站帧（res 或 event）。
//
// Python: send() L468-499 尾段。
func buildOutboundFrame(msg *schema.Message) map[string]any {
	if msg.Type == schema.MessageTypeRes {
		payload := map[string]any{}
		if msg.Payload != nil {
			for k, v := range msg.Payload {
				payload[k] = v
			}
		}
		frame := map[string]any{
			"type":    "res",
			"id":      msg.ID,
			"ok":      msg.OK,
			"payload": payload,
		}
		if !msg.OK {
			errText, _ := payload["error"].(string)
			if errText == "" {
				errText = "request failed"
			}
			frame["error"] = errText
		}
		return frame
	}
	return buildEventFrame(msg)
}
