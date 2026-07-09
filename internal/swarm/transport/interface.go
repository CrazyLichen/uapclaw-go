package transport

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// AgentTransport Gateway ↔ AgentServer 的传输抽象。
//
// 对齐 Python WebSocket 单连接模型：send(json_str) / recv() → json_str。
// 不感知 E2A 协议语义，只负责 JSON 字节传输。
// 所有服务端→客户端消息（普通响应、server_push、connection.ack）统一走 Recv 通道，
// 由 AgentClient 的 receiverLoop 做应用层区分。
//
// 无论进程内还是跨进程，Gateway 与 AgentServer 之间统一经过此接口：
//   - 进程内：ChannelTransport（Go channel，在 server/gateway_push 包实现）
//   - 跨进程：WebSocketTransport（WebSocket，后续实现）
//
// 对应 Python: jiuwenswarm/server/gateway_push/transport.py (GatewayPushTransport)
type AgentTransport interface {
	// Send 发送 JSON 字节到对端（对齐 Python ws.send(json_str)）
	Send(ctx context.Context, data []byte) error
	// Recv 返回接收通道，每条消息为 JSON 字节（对齐 Python ws.recv()）
	Recv() (<-chan []byte, error)
	// Close 关闭传输通道，释放资源
	Close() error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
