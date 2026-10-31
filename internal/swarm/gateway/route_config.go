package gateway

import (
	"strings"

	"github.com/gorilla/websocket"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager/web"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RouteConfig 单条路由的配置（/acp、/tui 等）。
//
// Python: app_gateway.py RouteConfig dataclass（L208-219）。
// 回调字段一律直接引用 routing 包的函数类型（与 GatewayRouteBinding 同源），
// 不在本包重复声明同签名类型——避免两处定义漂移。
type RouteConfig struct {
	// Path 路由路径（如 "/acp"）
	Path string
	// ChannelID 渠道标识（如 "acp"/"tui"）
	ChannelID string
	// ForwardMethods 转发到 MessageHandler 的 method 集合
	ForwardMethods map[string]bool
	// ForwardNoLocalHandlerMethods 仅转发、无本地 handler 的 method 集合
	ForwardNoLocalHandlerMethods map[string]bool
	// LocalHandlers 本地 handler（method → handler）
	LocalHandlers map[string]LocalHandlerFunc
	// InboundInterceptor 入站拦截器（可 nil）
	InboundInterceptor routing.InterceptorFunc
	// OutboundInterceptor 出站拦截器（可 nil）
	OutboundInterceptor routing.InterceptorFunc
	// CleanupHandler 断连清理钩子（可 nil，DisconnectHandler 缺省时的兜底）
	CleanupHandler routing.CleanupFunc
	// DisconnectHandler 断连回调（可 nil，带 staleSessionKeys）
	DisconnectHandler routing.DisconnectFunc
}

// GatewayServerConfig GatewayServer 配置。
//
// Python: app_gateway.py GatewayServerConfig dataclass（L223-238）。
type GatewayServerConfig struct {
	// Enabled 是否启用（默认 true）
	Enabled bool
	// Host 监听地址（默认 127.0.0.1）
	Host string
	// Port 监听端口（默认 19001）
	Port int
	// Routes 路由表（path → RouteConfig）
	Routes map[string]*RouteConfig
	// Path 单路由便捷字段（兜底构造用）
	Path string
	// ChannelID 单路由便捷字段（兜底构造用）
	ChannelID string
}

// LocalHandlerFunc 本地 handler。
//
// 对齐 Python Callable[..., Awaitable[None]]，Python 签名 (ws, req_id, params, session_id)。
// 保留本包命名类型（仅 gateway 包内使用，不跨包共享）；routing 包的回调类型则直接引用。
type LocalHandlerFunc func(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGatewayServerConfig 创建默认配置。
//
// 默认值对齐 Python GatewayServerConfig：enabled=true, host="127.0.0.1", port=19001。
func NewGatewayServerConfig() *GatewayServerConfig {
	return &GatewayServerConfig{
		Enabled: true,
		Host:    "127.0.0.1",
		Port:    19001,
		Routes:  make(map[string]*RouteConfig),
	}
}

// BuildACPRouteBinding 构造 ACP 路由 binding。
//
// Python: _build_acp_route_binding（L723-736）——binding 只带
// path/channel_id/两个 forward 集合；拦截器不在此装填（Python 侧 binding 的
// inbound/outbound 为 None），由 InstallDefaultAcpHooks 在装配后按 `or` 语义补齐
// （对齐 _install_default_route_hooks L294-302）。install 亦为 nil（ACP 无本地 handler）。
func BuildACPRouteBinding(path, channelID string) *routing.RouteBinding {
	return &routing.RouteBinding{
		Path:                         path,
		ChannelID:                    channelID,
		ForwardMethods:               web.ForwardReqMethods,
		ForwardNoLocalHandlerMethods: web.ForwardNoLocalHandlerMethods,
	}
}

// BuildRouteConfigMap 将 RouteBinding 列表转为 path 索引的 RouteConfig map。
//
// Python: _build_route_config_map（L739-752）——8 字段透传，唯独 install 不搬
// （install 需 GatewayServer 实例，由装配循环单独执行）。
func BuildRouteConfigMap(bindings []*routing.RouteBinding) map[string]*RouteConfig {
	m := make(map[string]*RouteConfig, len(bindings))
	for _, b := range bindings {
		m[b.Path] = &RouteConfig{
			Path:                         b.Path,
			ChannelID:                    b.ChannelID,
			ForwardMethods:               b.ForwardMethods,
			ForwardNoLocalHandlerMethods: b.ForwardNoLocalHandlerMethods,
			LocalHandlers:                make(map[string]LocalHandlerFunc),
			InboundInterceptor:           b.InboundInterceptor,
			OutboundInterceptor:          b.OutboundInterceptor,
			CleanupHandler:               b.CleanupHandler,
			DisconnectHandler:            b.DisconnectHandler,
		}
	}
	return m
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyConfigFallback 兜底构造单路由（对齐 Python __post_init__，L236-238）。
//
// 无 routes 且 Path+ChannelID 齐全时自动造一条 RouteConfig 塞入 Routes。
func applyConfigFallback(cfg *GatewayServerConfig) {
	if len(cfg.Routes) > 0 {
		return
	}
	path := strings.TrimSpace(cfg.Path)
	channelID := strings.TrimSpace(cfg.ChannelID)
	if path != "" && channelID != "" {
		cfg.Routes[path] = &RouteConfig{
			Path:                         path,
			ChannelID:                    channelID,
			ForwardMethods:               make(map[string]bool),
			ForwardNoLocalHandlerMethods: make(map[string]bool),
			LocalHandlers:                make(map[string]LocalHandlerFunc),
		}
	}
}

// strVal 安全取字符串（对齐 Python str(x or "")，仅 string 取值，数字请用
// acp.NormalizeJSONRPCID）。
func strVal(v any) string {
	s, _ := v.(string)
	return s
}
