package gateway

import (
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewGatewayServerConfig_默认值(t *testing.T) {
	cfg := NewGatewayServerConfig()
	assert.True(t, cfg.Enabled)
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, 19001, cfg.Port)
	assert.NotNil(t, cfg.Routes)
	assert.Empty(t, cfg.Routes)
}

func TestNewGatewayServerConfig_兜底单路由(t *testing.T) {
	cfg := NewGatewayServerConfig()
	cfg.Path = "/acp"
	cfg.ChannelID = "acp"
	applyConfigFallback(cfg) // 兜底逻辑（对齐 __post_init__）
	assert.Len(t, cfg.Routes, 1)
	rc, ok := cfg.Routes["/acp"]
	assert.True(t, ok)
	assert.Equal(t, "/acp", rc.Path)
	assert.Equal(t, "acp", rc.ChannelID)
}

func TestNewGatewayServerConfig_兜底跳过条件(t *testing.T) {
	// routes 非空 → 跳过兜底
	cfg := NewGatewayServerConfig()
	cfg.Routes["/x"] = &RouteConfig{Path: "/x", ChannelID: "x"}
	cfg.Path = "/y"
	cfg.ChannelID = "y"
	applyConfigFallback(cfg)
	assert.Len(t, cfg.Routes, 1)

	// Path 或 ChannelID 缺失 → 不兜底
	cfg2 := NewGatewayServerConfig()
	cfg2.Path = "/y"
	applyConfigFallback(cfg2)
	assert.Empty(t, cfg2.Routes)

	cfg3 := NewGatewayServerConfig()
	cfg3.ChannelID = "y"
	applyConfigFallback(cfg3)
	assert.Empty(t, cfg3.Routes)
}

func TestBuildRouteConfigMap_字段透传(t *testing.T) {
	// 8 字段透传（install 不搬，对齐 Python _build_route_config_map L739-752）
	inbound := func(*websocket.Conn, map[string]any) bool { return true }
	outbound := func(*websocket.Conn, map[string]any) bool { return false }
	cleanup := func(*websocket.Conn) {}
	disconnect := func(*websocket.Conn, [][2]string) {}
	bindings := []*routing.RouteBinding{
		{
			Path:                         "/acp",
			ChannelID:                    "acp",
			ForwardMethods:               map[string]bool{"chat.send": true},
			ForwardNoLocalHandlerMethods: map[string]bool{"initialize": true},
			InboundInterceptor:           inbound,
			OutboundInterceptor:          outbound,
			CleanupHandler:               cleanup,
			DisconnectHandler:            disconnect,
		},
	}
	m := BuildRouteConfigMap(bindings)
	assert.Len(t, m, 1)
	rc := m["/acp"]
	assert.Equal(t, "/acp", rc.Path)
	assert.Equal(t, "acp", rc.ChannelID)
	assert.True(t, rc.ForwardMethods["chat.send"])
	assert.True(t, rc.ForwardNoLocalHandlerMethods["initialize"])
	assert.NotNil(t, rc.InboundInterceptor)
	assert.NotNil(t, rc.OutboundInterceptor)
	assert.NotNil(t, rc.CleanupHandler)
	assert.NotNil(t, rc.DisconnectHandler)
	assert.True(t, rc.InboundInterceptor(nil, nil))
	assert.False(t, rc.OutboundInterceptor(nil, nil))
	assert.NotNil(t, rc.LocalHandlers)
}

func TestBuildRouteConfigMap_空列表(t *testing.T) {
	m := BuildRouteConfigMap(nil)
	assert.NotNil(t, m)
	assert.Empty(t, m)
}

func TestBuildACPRouteBinding(t *testing.T) {
	b := BuildACPRouteBinding("/acp", "acp")
	assert.Equal(t, "/acp", b.Path)
	assert.Equal(t, "acp", b.ChannelID)
	// forward 集合随 binding 携带（Python _build_acp_route_binding 同形）
	assert.True(t, b.ForwardMethods["chat.send"])
	assert.True(t, b.ForwardNoLocalHandlerMethods["initialize"])
	// 拦截器与 Install 由装配后补：binding 阶段为 nil
	assert.Nil(t, b.InboundInterceptor)
	assert.Nil(t, b.OutboundInterceptor)
	assert.Nil(t, b.CleanupHandler)
	assert.Nil(t, b.DisconnectHandler)
	assert.Nil(t, b.Install)
}

// TestRegisterLocalHandler 路由存在/不存在两态（对齐 Python register_local_handler L324-330）。
func TestRegisterLocalHandler(t *testing.T) {
	routes := map[string]*RouteConfig{
		"/tui": {Path: "/tui", ChannelID: "tui", LocalHandlers: map[string]LocalHandlerFunc{}},
	}
	gs := NewGatewayServer(&GatewayServerConfig{Routes: routes})
	h := func(*websocket.Conn, string, map[string]any, string) error { return nil }

	gs.RegisterLocalHandler("/tui", "chat.send", h)
	assert.NotNil(t, routes["/tui"].LocalHandlers["chat.send"])

	// 路由缺失 → 兜底新建（channel_id = path 去斜杠）
	gs.RegisterLocalHandler("/newpath", "x.y", h)
	created := routes["/newpath"]
	if assert.NotNil(t, created) {
		assert.Equal(t, "newpath", created.ChannelID)
		assert.NotNil(t, created.LocalHandlers["x.y"])
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestStrVal(t *testing.T) {
	assert.Equal(t, "x", strVal("x"))
	assert.Equal(t, "", strVal(nil))
	assert.Equal(t, "", strVal(123))
}
