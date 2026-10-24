package gateway

import (
	"context"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager/web"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InboundGatewayServer Gateway 内部入站服务：渠道消息经队列转发到 MessageHandler。
//
// 对齐 Python _InboundGatewayServer（app_gateway.py L132-173，
// asyncio.Queue + 独立 task 的双层队列结构）。
//
// 满队语义为有意偏差（spec §7-1）：Python asyncio.Queue 无界；
// Go 用容量 256 的 chan + 满时 Warn 丢弃，与下游
// MessageHandler.userMessages（256 有界 chan）链路容量一致。
type InboundGatewayServer struct {
	// inboundHandler serveLoop 消费的处理器
	inboundHandler func(ctx context.Context, msg *schema.Message)
	// queue 消息队列（容量 256）
	queue chan *schema.Message
	// taskDone serveLoop 退出信号（对齐 _task cancel/await）
	taskDone chan struct{}
	// running 运行状态
	running atomic.Bool
	// cancel serveLoop context 取消函数（对齐 task.cancel()）
	cancel context.CancelFunc
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// inboundQueueCapacity 入站队列容量（对齐 MessageHandler.userMessages 链路容量 256）
const inboundQueueCapacity = 256

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInboundGatewayServer 创建入站服务实例。
//
// Python: _InboundGatewayServer(inbound_handler)。
func NewInboundGatewayServer(inboundHandler func(ctx context.Context, msg *schema.Message)) *InboundGatewayServer {
	return &InboundGatewayServer{
		inboundHandler: inboundHandler,
		queue:          make(chan *schema.Message, inboundQueueCapacity),
		taskDone:       make(chan struct{}),
	}
}

// Start 启动 serveLoop（幂等：已启动则直接返回）。
//
// Python: _InboundGatewayServer.start()——if self._task is not None: return。
func (s *InboundGatewayServer) Start(ctx context.Context) error {
	if s.running.Load() {
		return nil
	}
	s.running.Store(true)
	serveCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go s.serveLoop(serveCtx)
	return nil
}

// Stop 停止 serveLoop 并等待退出。
//
// Python: _InboundGatewayServer.stop()——cancel + await + CancelledError 吞掉。
func (s *InboundGatewayServer) Stop() {
	s.running.Store(false)
	if s.cancel != nil {
		s.cancel()
	}
	if s.taskDone != nil {
		<-s.taskDone
	}
}

// HandleMessage 入队一条消息。
//
// Python: handle_message——await queue.put(msg); return True。
// 满队时 Warn 丢弃（有意偏差，见类型注释）。返回值恒 nil，
// 签名匹配 GatewayServer.OnMessage 回调类型。
func (s *InboundGatewayServer) HandleMessage(ctx context.Context, msg *schema.Message) error {
	select {
	case s.queue <- msg:
	default:
		logger.Warn(logComponent).
			Str("msg_id", msg.ID).
			Msg("gateway inbound queue full, dropping message")
	}
	return nil
}

// NormalizeAndForwardMessage 标准化并入站转发到 MessageHandler。
//
// Python: _normalize_and_forward_message（app_gateway.py L125-129）。
// 与 web.MakeNormAndForward 的本质区别：本函数恒转发不短路（无三层路由
// 判断）——入站路由判断已在 handleRawMessage 的 ForwardMethods 集合处完成。
func NormalizeAndForwardMessage(msg *schema.Message, channelMgr *channel_manager.ChannelManager) {
	normalized := web.NormalizeGatewayMessage(msg)
	channelMgr.DeliverToMessageHandler(normalized)
	logger.Info(logComponent).
		Str("msg_id", msg.ID).
		Str("channel_id", msg.ChannelID).
		Msg("Gateway inbound -> MessageHandler")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// serveLoop 消费循环。
//
// Python: _serve_loop（L162-173）——每条消息独立异常隔离。
func (s *InboundGatewayServer) serveLoop(ctx context.Context) {
	defer close(s.taskDone)
	for s.running.Load() {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.queue:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error(logComponent).
							Any("panic", r).
							Msg("Gateway inbound handling failed")
					}
				}()
				s.inboundHandler(ctx, msg)
			}()
		}
	}
}
