package gateway

import (
	"testing"

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
	bindings := []*routing.RouteBinding{
		{
			Path:      "/acp",
			ChannelID: "acp",
		},
	}
	m := BuildRouteConfigMap(bindings)
	assert.Len(t, m, 1)
	rc := m["/acp"]
	assert.Equal(t, "/acp", rc.Path)
	assert.Equal(t, "acp", rc.ChannelID)
	// 拦截器/断连钩子 ⤵️ 11.25/11.15 转换，当前置 nil
	assert.Nil(t, rc.InboundInterceptor)
	assert.Nil(t, rc.OutboundInterceptor)
	assert.Nil(t, rc.DisconnectHandler)
	// ForwardMethods/ForwardNoLocalHandlerMethods：binding 已删除字段，调用方单独赋值
	rc.ForwardMethods = map[string]bool{"chat.send": true}
	assert.True(t, rc.ForwardMethods["chat.send"])
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
	assert.Nil(t, b.InboundInterceptor)
	assert.Nil(t, b.Install)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestStrVal(t *testing.T) {
	assert.Equal(t, "x", strVal("x"))
	assert.Equal(t, "", strVal(nil))
	assert.Equal(t, "", strVal(123))
}
