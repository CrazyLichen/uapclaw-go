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
// 对齐 Python WebSocket 单连接模型：所有消息（请求/响应/推送/事件）走同一 recvCh。
// 适用场景：chat/serve/acp/app 等单进程模式，Gateway 和 AgentServer 运行在同一进程内。
// 将来跨进程模式使用 WebSocketTransport（也在本包中实现）。
//
// 对应 Python: jiuwenswarm/server/gateway_push/transport.py (进程内路径)
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

// logComponentCh 日志组件
const logComponentCh = logger.ComponentCommon

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrTransportClosed 传输通道已关闭
	ErrTransportClosed = errors.New("传输通道已关闭")
)

// 接口合规：ChannelTransport 实现 AgentTransport
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
	logger.Info(logComponentCh).
		Str("event_type", "channel_transport_created").
		Int("send_buf", sendBuf).
		Int("recv_buf", recvBuf).
		Msg("ChannelTransport 已创建")
	return t
}

// Send 发送 JSON 字节到 AgentServer（对齐 Python ws.send）。
func (t *ChannelTransport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		logger.Warn(logComponentCh).
			Str("event_type", "channel_transport_send_closed").
			Msg("发送失败：传输已关闭")
		return ErrTransportClosed
	}
	t.mu.Unlock()

	select {
	case t.sendCh <- data:
		logger.Debug(logComponentCh).
			Str("event_type", "channel_transport_send").
			Int("bytes", len(data)).
			Msg("JSON 字节已发送")
		return nil
	case <-ctx.Done():
		logger.Warn(logComponentCh).
			Str("event_type", "channel_transport_send_cancelled").
			Err(ctx.Err()).
			Msg("发送失败：上下文已取消")
		return ctx.Err()
	}
}

// Recv 返回接收通道（对齐 Python ws.recv）。
func (t *ChannelTransport) Recv() (<-chan []byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		logger.Warn(logComponentCh).
			Str("event_type", "channel_transport_recv_closed").
			Msg("接收失败：传输已关闭")
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
	logger.Info(logComponentCh).
		Str("event_type", "channel_transport_closed").
		Msg("ChannelTransport 已关闭")
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
