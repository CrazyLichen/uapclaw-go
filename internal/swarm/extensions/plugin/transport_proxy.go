package plugin

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"

	extproto "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin/proto"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GRPCPluginTransport 扩展插件 transport 的宿主侧代理，实现 transport.AgentTransport。
//
// 将 AgentTransport 的 Send/Recv/Close 映射到插件 gRPC 的
// TransportSend/TransportRecv（server-streaming）/TransportClose 三个 RPC。
// AgentClient 通过本代理与扩展提供的 transport 通信，E2A 语义仍留在
// AgentClient 的 receiverLoop（规则 6：Transport 层为纯 byte 管道）。
type GRPCPluginTransport struct {
	// pc 插件客户端
	pc *PluginClient
	// recvCh 接收通道（惰性启动的 TransportRecv 流写入）
	recvCh chan []byte
	// mu 保护 closed 与 recvStarted
	mu sync.Mutex
	// closed 是否已关闭
	closed bool
	// recvStarted 接收 goroutine 是否已启动
	recvStarted bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// recvChannelBuffer 接收通道缓冲（对齐 ChannelTransport defaultRecvBufferSize）
	recvChannelBuffer = 128
	// recvStallTimeout 通道长期满时丢弃帧的超时（防 Close 与阻塞发送竞态 panic）
	recvStallTimeout = 5 * time.Second
	// proxyLogComponent 日志组件（基础设施层）
	proxyLogComponent = logger.ComponentCommon
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrPluginTransportClosed 代理已关闭
var ErrPluginTransportClosed = errors.New("插件 transport 代理已关闭")

// 编译期断言：GRPCPluginTransport 满足 AgentTransport 接口
var _ transport.AgentTransport = (*GRPCPluginTransport)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGRPCPluginTransport 创建插件 transport 代理
func NewGRPCPluginTransport(pc *PluginClient) *GRPCPluginTransport {
	return &GRPCPluginTransport{pc: pc}
}

// Send 发送 JSON 字节到扩展侧 transport（对齐 AgentTransport.Send）
func (t *GRPCPluginTransport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return ErrPluginTransportClosed
	}
	t.mu.Unlock()

	_, err := t.pc.RawProtoClient().TransportSend(ctx, transportSendRequestOf(data))
	return err
}

// Recv 返回接收通道（对齐 AgentTransport.Recv）。
// 首次调用惰性启动 goroutine 消费 TransportRecv server-streaming，
// 流断开或出错时关闭通道（对齐 ChannelTransport 的 Recv 语义）。
func (t *GRPCPluginTransport) Recv() (<-chan []byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrPluginTransportClosed
	}
	if !t.recvStarted {
		t.recvStarted = true
		t.recvCh = make(chan []byte, recvChannelBuffer)
		go t.pumpRecvLoop()
	}
	return t.recvCh, nil
}

// Close 关闭代理（幂等）：先标记关闭，再通知扩展侧
func (t *GRPCPluginTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	recvCh := t.recvCh
	t.mu.Unlock()

	if recvCh != nil {
		close(recvCh)
	}
	// 通知插件侧关闭（ctx 背景即可，出错不阻断宿主关闭流程）
	_, _ = t.pc.RawProtoClient().TransportClose(context.Background(), transportCloseRequestOf())
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pumpRecvLoop 消费 TransportRecv 流并写入 recvCh。
//
// 写入用 select 防护：通道长期满（消费方停摆）时丢弃帧并 warn，
// 避免 Close 的 close(recvCh) 与阻塞发送竞态 panic（send on closed channel）。
func (t *GRPCPluginTransport) pumpRecvLoop() {
	stream, err := t.pc.RawProtoClient().TransportRecv(context.Background(), transportRecvRequestOf())
	if err != nil {
		t.closeRecvCh()
		return
	}
	for {
		resp, err := stream.Recv()
		if err != nil {
			t.closeRecvCh()
			return
		}
		data := resp.GetData()
		t.mu.Lock()
		closed := t.closed
		t.mu.Unlock()
		if closed {
			return
		}
		select {
		case t.recvCh <- data:
		case <-time.After(recvStallTimeout):
			// 通道长期满：丢弃帧并记录 warn（对齐 ChannelTransport 缓冲满丢弃语义）
			logger.Warn(proxyLogComponent).
				Str("event_type", "plugin_transport_recv_dropped").
				Int("bytes", len(data)).
				Msg("recv 通道缓冲满，丢弃帧")
		}
	}
}

// closeRecvCh 关闭接收通道（幂等保护：closed 标记未置位时才关）
func (t *GRPCPluginTransport) closeRecvCh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.recvCh != nil && !t.closed {
		close(t.recvCh)
	}
}

// transportSendRequestOf 构造 TransportSend 请求
func transportSendRequestOf(data []byte) *extproto.TransportSendRequest {
	return &extproto.TransportSendRequest{Data: data}
}

// transportRecvRequestOf 构造 TransportRecv 请求
func transportRecvRequestOf() *extproto.TransportRecvRequest {
	return &extproto.TransportRecvRequest{}
}

// transportCloseRequestOf 构造 TransportClose 请求
func transportCloseRequestOf() *extproto.TransportCloseRequest {
	return &extproto.TransportCloseRequest{}
}
