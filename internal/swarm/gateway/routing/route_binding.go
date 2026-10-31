package routing

import "github.com/gorilla/websocket"

// ──────────────────────────── 结构体 ────────────────────────────

// RouteBinding 描述一个 WS 路径的完整消息处理策略（工厂装配的中间形态）。
//
// Python: GatewayRouteBinding (routing/route_binding.py L8-19)，9 字段一一对应：
// path / channel_id / forward_methods / forward_no_local_handler_methods /
// inbound_interceptor / outbound_interceptor / cleanup_handler / disconnect_handler /
// install。
//
// 装配链（对齐 app_gateway.py L1037-1069）：
//
//	bindings := []*RouteBinding{BuildACPRouteBinding(...), BuildTuiRouteBinding(...)}
//	routes := BuildRouteConfigMap(bindings)   // 转 RouteConfig，install 不搬
//	for _, b := range bindings {              // 循环里注册 external channel + 执行 install
//	    RegisterExternalChannel(routes[b.Path].ChannelID, gwServer)
//	    if b.Install != nil { b.Install(gwServer) }
//	}
//
// install 不搬进 RouteConfig 的原因与 Python 一致：它需要已构造好的 GatewayServer
// 实例（注册本地 handler），而 RouteConfig 必须在 server 构造前就绪。
type RouteBinding struct {
	// Path WS 路径，如 "/acp"、"/tui"
	Path string
	// ChannelID 通道标识，如 "acp"、"tui"
	ChannelID string
	// ForwardMethods 转发到 MessageHandler 的 method 集合
	// Python: forward_methods: frozenset[str]
	ForwardMethods map[string]bool
	// ForwardNoLocalHandlerMethods 仅转发、无本地 handler 的 method 集合
	// Python: forward_no_local_handler_methods: frozenset[str]
	ForwardNoLocalHandlerMethods map[string]bool
	// InboundInterceptor 入站拦截器（如 ACP JSON-RPC 翻译）
	InboundInterceptor InterceptorFunc
	// OutboundInterceptor 出站拦截器
	OutboundInterceptor InterceptorFunc
	// CleanupHandler 断连清理（无 DisconnectHandler 时的兜底）
	CleanupHandler CleanupFunc
	// DisconnectHandler 连接断开回调（带 staleSessionKeys）
	DisconnectHandler DisconnectFunc
	// Install 在 GatewayServer 上注册本地 handler 的钩子
	Install InstallFunc
}

// InterceptorFunc 入站/出站拦截器函数类型（返回 true 表示已处理、短路后续链路）。
//
// Python: inbound_interceptor(ws, data: dict) -> bool（app_gateway.py L586）/
// outbound_interceptor(msg, ws) -> bool（L462）。Go 侧出站统一以 {"raw": msg}
// 包裹帧传入，故两向共用同一签名。
//
// ⚠️ 历史：本类型曾误写为 func(channel any, data []byte) ([]byte, error)（2026-07-09
// 骨架凭空建模，零生产调用），与 Python 的 ws/map/bool 三处全不符，已按 Python 修正。
type InterceptorFunc func(ws *websocket.Conn, data map[string]any) bool

// CleanupFunc 断连清理函数类型（无 DisconnectHandler 时的兜底）。
//
// Python: cleanup_handler(ws) -> None（app_gateway.py L553）。
type CleanupFunc func(ws *websocket.Conn)

// DisconnectFunc 连接断开回调类型。
//
// Python: disconnect_handler(ws, stale_session_keys: list[tuple[str, str]]) -> None
// （app_gateway.py L542；唯一实现 _tui_disconnect 首参即 ws，见 tui_connect.py L2106）。
// ⚠️ 历史：本类型曾误写为 func(channel any, sessionKeys []string)（同上骨架），
// 首参与键类型均与 Python 不符，已修正。
type DisconnectFunc func(ws *websocket.Conn, staleSessionKeys [][2]string)

// InstallFunc 在 GatewayServer 上注册本地 handler 的钩子类型。
//
// Python: install: Callable[[Any], None]（route_binding.py L19）；调用点
// app_gateway.py L1067-1068 `binding.install(gateway_server)`。
// server 为 any：routing 包不能 import gateway（会成环），实现侧做类型断言。
type InstallFunc func(server any)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWebRouteBinding 创建默认 Web 通道路由绑定（/ws → web）。
//
// 生产零调用：/ws 路由由 web channel 自身装配，不走 binding 工厂链
// （Python 侧 /ws 也不在 route_bindings 列表里，见 app_gateway.py L1037-1055）。
func NewWebRouteBinding() *RouteBinding {
	return &RouteBinding{
		Path:                         "/ws",
		ChannelID:                    "web",
		ForwardMethods:               make(map[string]bool),
		ForwardNoLocalHandlerMethods: make(map[string]bool),
	}
}
