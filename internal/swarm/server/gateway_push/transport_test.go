package gateway_push

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
)

// TestNewChannelPushTransport 测试创建实例。
func TestNewChannelPushTransport(t *testing.T) {
	transport := NewChannelPushTransport()
	if transport == nil {
		t.Error("NewChannelPushTransport 不应返回 nil")
	}
}

// TestChannelPushTransport_SendPush_无单例 测试无单例时返回错误。
func TestChannelPushTransport_SendPush_无单例(t *testing.T) {
	server.ResetInstance()
	transport := NewChannelPushTransport()
	ctx := t.Context()
	msg := map[string]any{"request_id": "req-1"}
	err := transport.SendPush(ctx, msg)
	if err == nil {
		t.Error("无单例时 SendPush 应返回错误")
	}
}

// TestGatewayPushTransport接口合规 测试 ChannelPushTransport 实现 GatewayPushTransport。
func TestGatewayPushTransport接口合规(t *testing.T) {
	var _ GatewayPushTransport = (*ChannelPushTransport)(nil)
}
