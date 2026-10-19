package transport

import (
	"context"
	"errors"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChannelTransport 进程内传输实现，基于 Go channel 在 Gateway 与 AgentServer 之间传递 JSON 字节。
//
// Python: WebSocket 单连接模型：所有消息（请求/响应/推送/事件）走同一 recvCh。
// 适用场景：chat/serve/acp/app 等单进程模式，Gateway 和 AgentServer 运行在同一进程内。
// 将来跨进程模式使用 WebSocketTransport（也在本包中实现）。
//
// Python: jiuwenswarm/server/gateway_push/transport.py (进程内路径)
type ChannelTransport struct {
	// sendCh 请求通道：Gateway → AgentServer（JSON 字节）
	sendCh chan []byte
	// recvCh 响应通道：AgentServer → Gateway（JSON 字节，统一承载响应/推送/事件）
	recvCh chan []byte
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

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件
var logComponent = logger.ComponentAgentServer

var (
	// ErrTransportClosed 传输通道已关闭
	ErrTransportClosed = errors.New("传输通道已关闭")
)

// _ 编译时验证 ChannelTransport 满足 AgentTransport 接口
var _ AgentTransport = (*ChannelTransport)(nil)

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
		sendCh: make(chan []byte, sendBuf),
		recvCh: make(chan []byte, recvBuf),
	}
	logger.Info(logComponent).
		Str("event_type", "channel_transport_created").
		Int("send_buf", sendBuf).
		Int("recv_buf", recvBuf).
		Msg("ChannelTransport created")
	return t
}

// Send 发送 JSON 字节到 AgentServer（对齐 Python ws.send）。
// M-42: 全程持锁（closed 检查 + channel 发送同一临界区），
// 对齐 Python send_request 的 `async with self._lock: await self._ws.send(...)` 持锁发送语义。
// Close 同样需要拿锁——Send 卡在缓冲满时 Close 阻塞等锁，
// 但 Send 有 ctx.Done 出口终会释放锁，无死锁。
func (t *ChannelTransport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_send_closed").
			Msg("send failed: transport closed")
		return ErrTransportClosed
	}

	select {
	case t.sendCh <- data:
		logger.Debug(logComponent).
			Str("event_type", "channel_transport_send").
			Int("bytes", len(data)).
			Msg("JSON bytes sent")
		return nil
	case <-ctx.Done():
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_send_cancelled").
			Err(ctx.Err()).
			Msg("send failed: context cancelled")
		return ctx.Err()
	}
}

// TrySendToRecvCh 非阻塞发送到响应通道（AgentServer → Gateway 方向）。
// M-42: 供 agent_server sendToGateway 使用——原先裸 `ct.RecvCh() <- data`
// 绕过 Send 的锁保护，Close 后 send on closed channel panic。
// 持锁 + closed 检查 + select default，保持原非阻塞丢弃语义（缓冲满时丢弃返回 false）。
func (t *ChannelTransport) TrySendToRecvCh(data []byte) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return false
	}
	select {
	case t.recvCh <- data:
		return true
	default:
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_recv_dropped").
			Int("bytes", len(data)).
			Msg("recv channel buffer full, message dropped")
		return false
	}
}

// Recv 返回接收通道（对齐 Python ws.recv）。
func (t *ChannelTransport) Recv() (<-chan []byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_recv_closed").
			Msg("receive failed: transport closed")
		return nil, ErrTransportClosed
	}
	return t.recvCh, nil
}

// Close 关闭传输通道，释放资源。
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
		Msg("ChannelTransport closed")
	return nil
}

// SendCh 返回请求通道的读取端，供 AgentServer 消费。
func (t *ChannelTransport) SendCh() <-chan []byte {
	return t.sendCh
}

// RecvCh 返回响应通道的写入端，供 AgentServer 写入响应。
func (t *ChannelTransport) RecvCh() chan<- []byte {
	return t.recvCh
}

// ──────────────────────────── 非导出函数 ────────────────────────────
