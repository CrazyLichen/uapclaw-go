package routing

// ──────────────────────────── 结构体 ────────────────────────────

// RouteBinding 描述一个 WS 路径的完整消息处理策略。
// 对齐 Python: GatewayRouteBinding (routing/route_binding.py)。
// 预留多路径 WS 框架（/ws、/acp、/tui），当前仅实现 /ws（web 通道）。
type RouteBinding struct {
	// Path WS 路径，如 "/ws"、"/acp"、"/tui"
	Path string
	// ChannelID 通道标识，如 "web"、"acp"、"tui"
	ChannelID string
	// ForwardMethods 需要转发到 AgentServer 的方法集合
	ForwardMethods map[string]bool
	// ForwardNoLocalHandler 转发后不需要本地 ack 的方法集合
	ForwardNoLocalHandler map[string]bool
	// InboundInterceptor 入站拦截器（如 ACP JSON-RPC 翻译）
	InboundInterceptor InterceptorFunc
	// OutboundInterceptor 出站拦截器
	OutboundInterceptor InterceptorFunc
	// DisconnectHandler 连接断开回调
	DisconnectHandler DisconnectFunc
	// Install 在 GatewayServer 上注册本地 handler 的钩子
	Install InstallFunc
}

// ──────────────────────────── 枚举 ────────────────────────────

// InterceptorFunc 入站/出站拦截器函数类型。
type InterceptorFunc func(channel any, data []byte) ([]byte, error)

// DisconnectFunc 连接断开回调类型。
type DisconnectFunc func(channel any, sessionKeys []string)

// InstallFunc 在 GatewayServer 上注册本地 handler 的钩子类型。
type InstallFunc func(server any)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWebRouteBinding 创建默认 Web 通道路由绑定（/ws → web）。
func NewWebRouteBinding() *RouteBinding {
	return &RouteBinding{
		Path:                  "/ws",
		ChannelID:             "web",
		ForwardMethods:        make(map[string]bool),
		ForwardNoLocalHandler: make(map[string]bool),
	}
}
