package gateway

import (
	"strings"

	"github.com/gorilla/websocket"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RouteConfig 单条路由的配置（/acp、/tui 等）。
//
// Python: app_gateway.py RouteConfig dataclass（L208-219）。
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
	InboundInterceptor GatewayInterceptorFunc
	// OutboundInterceptor 出站拦截器（可 nil）
	OutboundInterceptor GatewayInterceptorFunc
	// CleanupHandler 断连清理钩子（可 nil，DisconnectHandler 缺省时的兜底）
	CleanupHandler CleanupFunc
	// DisconnectHandler 断连回调（可 nil，带 staleSessionKeys）
	DisconnectHandler DisconnectFunc
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

// LocalHandlerFunc 本地 handler（对齐 Python Callable[..., Awaitable[None]]，
// Python 签名 (ws, req_id, params, session_id)）。
type LocalHandlerFunc func(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error

// GatewayInterceptorFunc 入/出站拦截器（对齐 Python Awaitable[bool]，返回 true 短路）。
type GatewayInterceptorFunc func(ws *websocket.Conn, data map[string]any) bool

// CleanupFunc 断连清理（对齐 cleanup_handler，无 disconnect_handler 时的兜底）。
type CleanupFunc func(ws *websocket.Conn)

// DisconnectFunc 断连回调（对齐 disconnect_handler，带 staleSessionKeys）。
type DisconnectFunc func(ws *websocket.Conn, staleSessionKeys [][2]string)

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

// BuildACPRouteBinding 从路径与渠道标识构建 ACP 路由绑定。
//
// Python: _build_acp_route_binding（L723-736）。
// 注意：Python 的 on_message_cb 参数未存入 binding（死参数），Go 侧不保留。
// forwardMethods/forwardNoLocal 由调用方直接放进 RouteConfig
// （见 BuildRouteConfigMap 注释），本函数只装 binding 本体字段。
// A1 空路由形态下暂无调用方，11.25 接线时使用。
func BuildACPRouteBinding(path, channelID string) *routing.RouteBinding {
	return &routing.RouteBinding{
		Path:      path,
		ChannelID: channelID,
	}
}

// BuildRouteConfigMap 将 RouteBinding 列表转为 path 索引的 RouteConfig map。
//
// Python: _build_route_config_map（L739-752），8 字段透传。
// Go 侧三处类型差异（对齐既有骨架决策）：
//  1. ForwardMethods/ForwardNoLocalHandlerMethods：routing.RouteBinding 已删除这两个字段
//     （运行时路由用 web 包集合变量），调用方（11.25/11.15）在 binding 之外
//     直接对返回的 RouteConfig 赋值
//  2. InboundInterceptor/OutboundInterceptor：routing.InterceptorFunc 是 byte 层签名
//     func(channel any, data []byte) ([]byte, error)，本包 GatewayInterceptorFunc 是
//     帧层签名 func(ws *websocket.Conn, data map[string]any) bool——转换逻辑随 ACP 桥
//     实现（⤵️ 11.25），本函数置 nil
//  3. DisconnectHandler：routing.DisconnectFunc 签名 func(channel any, sessionKeys []string)
//     与本包 DisconnectFunc func(ws *websocket.Conn, staleSessionKeys [][2]string) 不同，
//     同样随桥/路由装配时转换（⤵️ 11.25/11.15），本函数置 nil
func BuildRouteConfigMap(bindings []*routing.RouteBinding) map[string]*RouteConfig {
	m := make(map[string]*RouteConfig)
	for _, b := range bindings {
		m[b.Path] = &RouteConfig{
			Path:          b.Path,
			ChannelID:     b.ChannelID,
			LocalHandlers: make(map[string]LocalHandlerFunc),
			// InboundInterceptor/OutboundInterceptor/DisconnectHandler/CleanupHandler
			// ⤵️ 11.25/11.15：byte 层→帧层转换随 ACP 桥/TUI 装配实现
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

// strVal 安全取字符串（对齐 Python str(x or "")）。
func strVal(v any) string {
	s, _ := v.(string)
	return s
}
