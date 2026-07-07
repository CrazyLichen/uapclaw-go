package gateway_push

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentTransport Gateway → AgentServer 的传输抽象。
//
// 无论进程内还是跨进程，Gateway 与 AgentServer 之间统一经过 E2A 编解码，
// 仅传输层不同：
//   - 进程内：ChannelTransport（Go channel）
//   - 跨进程：WebSocketTransport（WebSocket）
//
// 对应 Python: jiuwenswarm/server/gateway_push/transport.py (GatewayPushTransport)
type AgentTransport interface {
	// Send 向 AgentServer 发送 E2A 请求信封
	Send(ctx context.Context, envelope *e2a.E2AEnvelope) error
	// Recv 返回 E2A 响应通道，调用方持续读取直到通道关闭
	Recv() (<-chan *e2a.E2AResponse, error)
	// Close 关闭传输通道，释放资源
	Close() error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
