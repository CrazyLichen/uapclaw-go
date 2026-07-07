package gateway_push

import (
	"context"
	"errors"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChannelTransport 进程内传输实现，基于 Go channel 在 Gateway 与 AgentServer 之间传递 E2A 消息。
//
// 适用场景：chat/serve/acp/app 等单进程模式，Gateway 和 AgentServer 运行在同一进程内。
//
// 对应 Python: jiuwenswarm/server/gateway_push/transport.py (进程内路径)
type ChannelTransport struct {
	// sendCh 请求通道：Gateway → AgentServer
	sendCh chan *e2a.E2AEnvelope
	// recvCh 响应通道：AgentServer → Gateway
	recvCh chan *e2a.E2AResponse
	// mu 保护 closed 标志的并发访问
	mu sync.Mutex
	// closed 是否已关闭
	closed bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultSendBufferSize 请求通道默认缓冲大小
	defaultSendBufferSize = 64
	// defaultRecvBufferSize 响应通道默认缓冲大小
	defaultRecvBufferSize = 128
)

// logComponent 日志组件
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrTransportClosed 传输通道已关闭
	ErrTransportClosed = errors.New("传输通道已关闭")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChannelTransport 创建 ChannelTransport 实例，使用默认缓冲大小。
func NewChannelTransport() *ChannelTransport {
	return NewChannelTransportWithBuffer(defaultSendBufferSize, defaultRecvBufferSize)
}

// NewChannelTransportWithBuffer 创建 ChannelTransport 实例，指定通道缓冲大小。
func NewChannelTransportWithBuffer(sendBuf, recvBuf int) *ChannelTransport {
	if sendBuf <= 0 {
		sendBuf = defaultSendBufferSize
	}
	if recvBuf <= 0 {
		recvBuf = defaultRecvBufferSize
	}
	t := &ChannelTransport{
		sendCh: make(chan *e2a.E2AEnvelope, sendBuf),
		recvCh: make(chan *e2a.E2AResponse, recvBuf),
	}
	logger.Info(logComponent).
		Str("event_type", "channel_transport_created").
		Int("send_buf", sendBuf).
		Int("recv_buf", recvBuf).
		Msg("ChannelTransport 已创建")
	return t
}

// Send 向 AgentServer 发送 E2A 请求信封。
// 如果传输已关闭或上下文取消，返回错误。
func (t *ChannelTransport) Send(ctx context.Context, envelope *e2a.E2AEnvelope) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_send_closed").
			Str("request_id", envelope.RequestID).
			Msg("发送失败：传输已关闭")
		return ErrTransportClosed
	}
	t.mu.Unlock()

	select {
	case t.sendCh <- envelope:
		logger.Debug(logComponent).
			Str("event_type", "channel_transport_send").
			Str("request_id", envelope.RequestID).
			Str("method", envelope.Method).
			Msg("请求信封已发送")
		return nil
	case <-ctx.Done():
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_send_cancelled").
			Str("request_id", envelope.RequestID).
			Err(ctx.Err()).
			Msg("发送失败：上下文已取消")
		return ctx.Err()
	}
}

// Recv 返回 E2A 响应通道。调用方持续读取直到通道关闭。
func (t *ChannelTransport) Recv() (<-chan *e2a.E2AResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_recv_closed").
			Msg("接收失败：传输已关闭")
		return nil, ErrTransportClosed
	}
	return t.recvCh, nil
}

// Close 关闭传输通道，释放资源。
// 关闭后 Send 和 Recv 均不可用。
func (t *ChannelTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	close(t.sendCh)
	close(t.recvCh)
	logger.Info(logComponent).
		Str("event_type", "channel_transport_closed").
		Msg("ChannelTransport 已关闭")
	return nil
}

// SendCh 返回请求通道，供 AgentServer 消费。
// AgentServer 通过此通道读取 Gateway 发来的 E2AEnvelope。
func (t *ChannelTransport) SendCh() <-chan *e2a.E2AEnvelope {
	return t.sendCh
}

// RecvCh 返回响应通道的写入端，供 AgentServer 写入响应。
// AgentServer 通过此通道向 Gateway 发送 E2AResponse。
func (t *ChannelTransport) RecvCh() chan<- *e2a.E2AResponse {
	return t.recvCh
}

// ──────────────────────────── 非导出函数 ────────────────────────────
