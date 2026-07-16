package gateway_push

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GatewayPushTransport AgentServer → Gateway 的推送传输协议。
//
// 对齐 Python: jiuwenswarm/server/gateway_push/transport.py (GatewayPushTransport)
// 所有 server_push 场景统一通过此接口推送，不直接操作底层 Transport。
type GatewayPushTransport interface {
	// SendPush 向 Gateway 发送一条 server_push 语义的消息。
	//
	// msg 格式与 Python AgentWebSocketServer.send_push 入参一致：
	//   {request_id, channel_id, session_id, payload, metadata?, response_kind?}
	// 内部自动调 BuildServerPushWire 编码为 E2A wire 格式。
	SendPush(ctx context.Context, msg map[string]any) error
}

// ChannelPushTransport 进程内推送实现，通过 AgentServer 单例发送。
//
// 对齐 Python: jiuwenswarm/server/gateway_push/transport.py
// （WebSocket 网关推送传输）
//
// Python 通过 AgentWebSocketServer.get_instance().send_push(msg) 推送，
// Go 侧同样通过 server.GetInstance().SendPush(msg) 推送。
type ChannelPushTransport struct{}

// ──────────────────────────── 常量 ────────────────────────────

// logComponentPush 推送日志组件
const logComponentPush = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

// 接口合规：ChannelPushTransport 实现 GatewayPushTransport
var _ GatewayPushTransport = (*ChannelPushTransport)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChannelPushTransport 创建 ChannelPushPushTransport 实例。
func NewChannelPushTransport() *ChannelPushTransport {
	return &ChannelPushTransport{}
}

// SendPush 通过 AgentServer 单例向 Gateway 推送消息。
func (t *ChannelPushTransport) SendPush(ctx context.Context, msg map[string]any) error {
	s := server.GetInstance()
	if s == nil {
		logger.Warn(logComponentPush).Msg("ChannelPushTransport: AgentServer 单例未初始化")
		return fmt.Errorf("AgentServer 单例未初始化")
	}
	return s.SendPush(ctx, msg)
}
