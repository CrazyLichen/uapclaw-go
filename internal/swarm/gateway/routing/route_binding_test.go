package routing

import (
	"testing"

	"github.com/gorilla/websocket"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewWebRouteBinding 测试创建默认 Web 通道路由绑定。
func TestNewWebRouteBinding(t *testing.T) {
	rb := NewWebRouteBinding()
	if rb.Path != "/ws" {
		t.Errorf("期望 Path=/ws，实际 %s", rb.Path)
	}
	if rb.ChannelID != "web" {
		t.Errorf("期望 ChannelID=web，实际 %s", rb.ChannelID)
	}
	if rb.ForwardMethods == nil || rb.ForwardNoLocalHandlerMethods == nil {
		t.Error("forward 集合应初始化为空 map（不可为 nil）")
	}
}

// TestRouteBinding_字段默认值 测试 RouteBinding 字段默认值。
func TestRouteBinding_字段默认值(t *testing.T) {
	rb := &RouteBinding{
		Path:      "/acp",
		ChannelID: "acp",
	}
	if rb.InboundInterceptor != nil {
		t.Error("默认 InboundInterceptor 应为 nil")
	}
	if rb.OutboundInterceptor != nil {
		t.Error("默认 OutboundInterceptor 应为 nil")
	}
	if rb.CleanupHandler != nil {
		t.Error("默认 CleanupHandler 应为 nil")
	}
	if rb.DisconnectHandler != nil {
		t.Error("默认 DisconnectHandler 应为 nil")
	}
	if rb.Install != nil {
		t.Error("默认 Install 应为 nil")
	}
}

// TestRouteBinding_回调签名对齐Python 校验三个回调类型的签名与 Python
// GatewayRouteBinding 一致：首参 ws、data 为 map[string]any、返回 bool、
// stale keys 为 [][2]string。签名被改回 channel/[]byte/[]string 时本用例编译失败。
func TestRouteBinding_回调签名对齐Python(t *testing.T) {
	rb := &RouteBinding{
		Path:      "/acp",
		ChannelID: "acp",
		// Python: inbound_interceptor(ws, data: dict) -> bool
		InboundInterceptor: func(ws *websocket.Conn, data map[string]any) bool {
			return ws == nil && data == nil
		},
		// Python: outbound_interceptor(msg, ws) -> bool（Go 以 {"raw": msg} 包裹帧传入）
		OutboundInterceptor: func(ws *websocket.Conn, data map[string]any) bool {
			_, hasRaw := data["raw"]
			return ws == nil && hasRaw
		},
		// Python: cleanup_handler(ws) -> None
		CleanupHandler: func(ws *websocket.Conn) {},
		// Python: disconnect_handler(ws, stale_session_keys: list[tuple[str, str]]) -> None
		DisconnectHandler: func(ws *websocket.Conn, staleSessionKeys [][2]string) {},
		// Python: install(channel) -> None
		Install: func(server any) {},
	}
	if !rb.InboundInterceptor(nil, nil) {
		t.Error("InboundInterceptor(nil, nil) 应返回 true（测试桩语义）")
	}
	if !rb.OutboundInterceptor(nil, map[string]any{"raw": 1}) {
		t.Error("OutboundInterceptor 应能读到包裹帧的 raw 键")
	}
	rb.CleanupHandler(nil)
	rb.DisconnectHandler(nil, [][2]string{{"acp", "s1"}})
	rb.Install(nil)
}
