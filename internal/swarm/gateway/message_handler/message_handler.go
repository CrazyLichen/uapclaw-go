package message_handler

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageHandler 消息处理器
// 入站：Channel → MessageHandler → AgentClient → AgentServer
// 出站：AgentServer → AgentClient → MessageHandler → Channel
type MessageHandler struct {
	// agentClient AgentServer 客户端（封装 Transport 通信）
	agentClient *routing.AgentClient
	// channelMgr 渠道管理器
	channelMgr *channel_manager.ChannelManager

	// userMessages 入站消息 channel（Channel → MessageHandler）
	userMessages chan *schema.Message
	// robotMessages 出站消息 channel（MessageHandler → Channel）
	robotMessages chan *schema.Message

	// running 是否正在运行
	running atomic.Bool
	// cancelFunc 取消函数
	cancelFunc context.CancelFunc

	// streamMu 流式任务锁
	streamMu sync.RWMutex
	// streamTasks 流式任务追踪：requestID → cancel
	streamTasks map[string]context.CancelFunc
	// streamSessions 流式会话映射：requestID → sessionID
	streamSessions map[string]string
	// streamMetadata 流式元数据映射：requestID → metadata
	streamMetadata map[string]map[string]any
	// streamModes 流式模式映射：requestID → mode
	streamModes map[string]string

	// statesMu 渠道状态锁
	statesMu sync.RWMutex
	// channelStates 渠道状态映射：channelKey → state
	channelStates map[string]*ChannelControlState

	// mu 互斥锁
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageHandler 创建消息处理器
func NewMessageHandler(agentClient *routing.AgentClient, channelMgr *channel_manager.ChannelManager) *MessageHandler {
	return &MessageHandler{
		agentClient:    agentClient,
		channelMgr:     channelMgr,
		userMessages:   make(chan *schema.Message, 256),
		robotMessages:  make(chan *schema.Message, 256),
		streamTasks:    make(map[string]context.CancelFunc),
		streamSessions: make(map[string]string),
		streamMetadata: make(map[string]map[string]any),
		streamModes:    make(map[string]string),
		channelStates:  make(map[string]*ChannelControlState),
	}
}

// HandleInbound 处理入站消息（用户→Agent）
//
// 将消息写入 userMessages channel，由 forwardLoop 异步消费。
// 对齐 Python handle_message：非阻塞写入，channel 满时丢弃并记录警告。
func (mh *MessageHandler) HandleInbound(_ context.Context, msg *schema.Message) error {
	if msg == nil {
		return nil
	}
	select {
	case mh.userMessages <- msg:
		logger.Debug(logComponent).
			Str("event_type", "handle_inbound").
			Str("msg_id", msg.ID).
			Str("session_id", msg.SessionID).
			Msg("入站消息已入队")
		return nil
	default:
		logger.Warn(logComponent).
			Str("event_type", "handle_inbound_dropped").
			Str("msg_id", msg.ID).
			Msg("入站消息队列已满，丢弃消息")
		return nil
	}
}

// StartForwarding 启动入站转发和出站循环
//
// 对齐 Python start_forwarding：启动 forwardLoop + outboundLoop，
// 并通过 SetServerPushHandler 注册 push 回调（对齐 Python set_server_push_handler）。
func (mh *MessageHandler) StartForwarding(ctx context.Context) error {
	if mh.running.Load() {
		logger.Warn(logComponent).Msg("MessageHandler 已在运行")
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	mh.cancelFunc = cancel
	mh.running.Store(true)

	// 启动入站转发循环
	go mh.forwardLoop(ctx)

	// 启动出站消息循环
	go mh.outboundLoop(ctx)

	// 注册 push 回调（对齐 Python set_server_push_handler）
	if mh.agentClient != nil {
		mh.agentClient.SetServerPushHandler(func(msg map[string]any) {
			mh.HandleServerPush(msg)
		})
	}

	logger.Info(logComponent).
		Str("event_type", "message_handler_started").
		Msg("MessageHandler 转发循环已启动")
	return nil
}

// StopForwarding 停止转发循环
//
// 对齐 Python stop_forwarding：取消所有流式任务 + 取消上下文。
func (mh *MessageHandler) StopForwarding() error {
	if !mh.running.Load() {
		return nil
	}

	// 取消所有流式任务
	mh.cancelAllStreamTasks()

	// 取消上下文
	if mh.cancelFunc != nil {
		mh.cancelFunc()
	}

	mh.running.Store(false)
	logger.Info(logComponent).
		Str("event_type", "message_handler_stopped").
		Msg("MessageHandler 转发循环已停止")
	return nil
}

// StartOutboundLoop 启动出站消息循环（Agent→用户）
//
// 从 robotMessages channel 读取消息，通过 ChannelManager 广播到各渠道。
// 对齐 Python publish_robot_messages。
func (mh *MessageHandler) StartOutboundLoop(ctx context.Context) error {
	go mh.outboundLoop(ctx)
	return nil
}

// outboundLoop 出站消息主循环
func (mh *MessageHandler) outboundLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			logger.Info(logComponent).Msg("出站循环退出")
			return
		case msg := <-mh.robotMessages:
			if msg == nil {
				continue
			}
			mh.dispatchOutbound(ctx, msg)
		}
	}
}

// dispatchOutbound 分发出站消息到渠道
func (mh *MessageHandler) dispatchOutbound(ctx context.Context, msg *schema.Message) {
	if mh.channelMgr == nil {
		logger.Warn(logComponent).
			Str("event_type", "dispatch_outbound_no_channel_mgr").
			Msg("ChannelManager 为空，无法分发出站消息")
		return
	}

	if err := mh.channelMgr.BroadcastToChannels(ctx, msg); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "dispatch_outbound_error").
			Err(err).
			Str("msg_id", msg.ID).
			Msg("分发出站消息失败")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cancelAllStreamTasks 取消所有流式任务
func (mh *MessageHandler) cancelAllStreamTasks() {
	mh.streamMu.Lock()
	defer mh.streamMu.Unlock()

	for reqID, cancel := range mh.streamTasks {
		if cancel != nil {
			cancel()
		}
		logger.Debug(logComponent).
			Str("event_type", "stream_task_cancelled").
			Str("request_id", reqID).
			Msg("流式任务已取消")
	}
	mh.streamTasks = make(map[string]context.CancelFunc)
	mh.streamSessions = make(map[string]string)
	mh.streamMetadata = make(map[string]map[string]any)
	mh.streamModes = make(map[string]string)
}

// rememberUserQueryContext 记录用户查询上下文
//
// 对齐 Python _remember_user_query_context：记录 chat.send 的 query 上下文。
func (mh *MessageHandler) rememberUserQueryContext(sessionID, query string) {
	if sessionID == "" || query == "" {
		return
	}
	mh.mu.Lock()
	defer mh.mu.Unlock()
	// sessionLastUserQuery 在后续需要时扩展
	_ = query
}
