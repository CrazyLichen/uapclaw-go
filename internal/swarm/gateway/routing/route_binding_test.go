package routing

import "testing"

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
	if rb.ForwardMethods == nil {
		t.Error("ForwardMethods 不应为 nil")
	}
	if rb.ForwardNoLocalHandler == nil {
		t.Error("ForwardNoLocalHandler 不应为 nil")
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
	if rb.DisconnectHandler != nil {
		t.Error("默认 DisconnectHandler 应为 nil")
	}
	if rb.Install != nil {
		t.Error("默认 Install 应为 nil")
	}
}
