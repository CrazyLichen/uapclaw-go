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
	// pushCh 推送通道：AgentServer → Gateway（server_push 主动推送）
	pushCh chan map[string]any
	// onServerPushCb push 消息回调，对齐 Python set_server_push_handler
	onServerPushCb func(msg map[string]any)
	// pushCancel push 消费 goroutine 的取消函数
	pushCancel context.CancelFunc
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
	// defaultPushBufferSize 推送通道默认缓冲大小
	defaultPushBufferSize = 128
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
		pushCh: make(chan map[string]any, defaultPushBufferSize),
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

	// 停止 push 消费 goroutine
	if t.pushCancel != nil {
		t.pushCancel()
	}

	close(t.sendCh)
	close(t.recvCh)
	close(t.pushCh)
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

// PushCh 返回推送通道的读取端，供内部 goroutine 消费。
func (t *ChannelTransport) PushCh() <-chan map[string]any {
	return t.pushCh
}

// SetServerPushHandler 实现 GatewayPushTransport 接口，注册 push 回调。
// 对齐 Python WebSocketAgentServerClient.set_server_push_handler：
// 注册回调后，从 pushCh 读取的消息将通过回调投递。
func (t *ChannelTransport) SetServerPushHandler(handler func(msg map[string]any)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onServerPushCb = handler

	// 停止旧的 push 消费 goroutine
	if t.pushCancel != nil {
		t.pushCancel()
	}

	// 启动新的 push 消费 goroutine：从 pushCh 读取 → 调回调
	ctx, cancel := context.WithCancel(context.Background())
	t.pushCancel = cancel
	go t.drainPushCh(ctx, handler)
	logger.Info(logComponent).
		Str("event_type", "server_push_handler_set").
		Msg("ServerPushHandler 已注册")
}

// SendPush 实现 GatewayPushTransport 接口，向 Gateway 推送消息。
// AgentServer 通过此方法主动推送消息到 Gateway。
func (t *ChannelTransport) SendPush(msg map[string]any) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_send_push_closed").
			Msg("推送失败：传输已关闭")
		return ErrTransportClosed
	}
	t.mu.Unlock()

	select {
	case t.pushCh <- msg:
		logger.Debug(logComponent).
			Str("event_type", "channel_transport_send_push").
			Msg("推送消息已发送")
		return nil
	default:
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_send_push_full").
			Msg("推送失败：通道已满，丢弃消息")
		return nil
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// drainPushCh 持续从 pushCh 读取消息并通过回调投递。
// 对齐 Python _message_receiver_loop 中 push 帧的分发逻辑：
// push 帧绕过 per-request 队列，直接通过回调投递。
func (t *ChannelTransport) drainPushCh(ctx context.Context, handler func(msg map[string]any)) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-t.pushCh:
			if !ok {
				return
			}
			if handler != nil && msg != nil {
				handler(msg)
			}
		}
	}
}
