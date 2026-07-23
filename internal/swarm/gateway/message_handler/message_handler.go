package message_handler

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OutboundPipeline 出站管道接口，对齐 Python _outbound_pipeline。
//
// 预留给 11.12 IM Pipeline 回填，当前未使用。
type OutboundPipeline interface {
	// Apply 对出站消息应用管道处理
	Apply(msg *schema.Message) (*schema.Message, error)
}

// streamTaskEntry 流式任务条目，包含取消函数和退出等待。
// 对齐 Python: asyncio.Task 的 cancel + gather 等待语义
type streamTaskEntry struct {
	// cancel 取消流式任务的 context.CancelFunc
	cancel context.CancelFunc
	// wg 用于等待流式处理 goroutine 完全退出
	wg sync.WaitGroup
}

// MessageHandler 消息处理器
// 入站：Channel → MessageHandler → AgentClient → AgentServer
// 出站：AgentServer → AgentClient → MessageHandler → ChannelManager → Channel
type MessageHandler struct {
	// agentClient AgentServer 客户端（封装 Transport 通信）
	agentClient *routing.AgentClient

	// userMessages 入站消息 channel（Channel → MessageHandler）
	userMessages chan *schema.Message
	// robotMessages 出站消息 channel（MessageHandler → ChannelManager）
	robotMessages chan *schema.Message

	// running 是否正在运行
	running atomic.Bool
	// cancelFunc 取消函数
	cancelFunc context.CancelFunc

	// streamMu 流式任务锁
	streamMu sync.RWMutex
	// streamTasks 流式任务追踪：requestID → streamTaskEntry
	streamTasks map[string]*streamTaskEntry
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

	// evolutionMu evolution 审批状态锁
	evolutionMu sync.RWMutex
	// pendingEvolutionApproval evolution 待审批映射：sessionID → approvalRequestID
	// 对齐 Python _pending_evolution_approval
	pendingEvolutionApproval map[string]string
	// queuedSupplementInput 排队的补充输入：sessionID → {new_input, attachments}
	// 对齐 Python _queued_supplement_input
	queuedSupplementInput map[string]map[string]any
	// sessionEvolutionInProgress 正在演进审批的 session 集合
	// 对齐 Python _session_evolution_in_progress
	sessionEvolutionInProgress map[string]bool

	// streamEmitsProcessingStatus 流式 processing_status 追踪：requestID → should emit
	// 对齐 Python _stream_emits_processing_status
	streamEmitsProcessingStatus map[string]bool

	// queryMu 用户查询上下文锁
	queryMu sync.RWMutex
	// sessionLastUserQuery 用户最近查询：sessionID → last query
	// 对齐 Python _session_last_user_query
	sessionLastUserQuery map[string]string

	// getConfigRaw 读取 config 原始数据回调（对齐 Python _get_config_raw，由外部注入）
	getConfigRaw func() map[string]any
	// updateChannelInConfig 更新 config 中渠道配置回调（对齐 Python _update_channel_in_config，由外部注入）
	updateChannelInConfig func(channelID string, update map[string]any)

	// outboundPipeline 出站管道（对齐 Python _outbound_pipeline，预留给 11.12 IM Pipeline 回填）
	// TODO(#11.12): 实现 OutboundPipeline interface（等 11.12 IM Pipeline 回填）
	outboundPipeline OutboundPipeline
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageHandler 创建消息处理器。
//
// 对齐 Python: MessageHandler(agent_client) — 只需 1 个参数。
func NewMessageHandler(agentClient *routing.AgentClient) *MessageHandler {
	return &MessageHandler{
		agentClient:                 agentClient,
		userMessages:                make(chan *schema.Message, 256),
		robotMessages:               make(chan *schema.Message, 256),
		streamTasks:                 make(map[string]*streamTaskEntry),
		streamSessions:              make(map[string]string),
		streamMetadata:              make(map[string]map[string]any),
		streamModes:                 make(map[string]string),
		channelStates:               make(map[string]*ChannelControlState),
		pendingEvolutionApproval:    make(map[string]string),
		queuedSupplementInput:       make(map[string]map[string]any),
		sessionEvolutionInProgress:  make(map[string]bool),
		streamEmitsProcessingStatus: make(map[string]bool),
		sessionLastUserQuery:        make(map[string]string),
	}
}

// HandleMessage 处理入站消息（用户→Agent）。
//
// 将消息写入 userMessages channel，由 forwardLoop 异步消费。
// 对齐 Python handle_message：非阻塞写入，channel 满时丢弃并记录警告。
//
// 对齐 Python: MessageHandler.handle_message()
func (mh *MessageHandler) HandleMessage(msg *schema.Message) {
	if msg == nil {
		return
	}
	// 对齐 Python handle_message：入队前记录用户查询上下文
	mh.rememberUserQueryContext(msg)
	select {
	case mh.userMessages <- msg:
		logger.Info(logComponent).
			Str("event_type", "handle_inbound").
			Str("msg_id", msg.ID).
			Str("channel_id", msg.ChannelID).
			Str("session_id", msg.SessionID).
			Msg("入站消息已入队")
	default:
		logger.Warn(logComponent).
			Str("event_type", "handle_inbound_dropped").
			Str("msg_id", msg.ID).
			Msg("入站消息队列已满，丢弃消息")
	}
}

// ConsumeRobotMessages 从出站队列消费一条消息，超时返回 nil。
//
// 供 ChannelManager 的出站派发循环调用。
// 对齐 Python: MessageHandler.consume_robot_messages()
func (mh *MessageHandler) ConsumeRobotMessages(timeout time.Duration) *schema.Message {
	select {
	case msg := <-mh.robotMessages:
		return msg
	case <-time.After(timeout):
		return nil
	}
}

// ConsumeUserMessages 从入站队列消费一条消息，超时返回 nil。
//
// 对齐 Python: MessageHandler.consume_user_messages()
func (mh *MessageHandler) ConsumeUserMessages(timeout time.Duration) *schema.Message {
	select {
	case msg := <-mh.userMessages:
		return msg
	case <-time.After(timeout):
		return nil
	}
}

// PublishUserMessagesNowait 将消息同步写入入站队列，满时丢弃。
//
// 对齐 Python: MessageHandler.publish_user_messages_nowait()
func (mh *MessageHandler) PublishUserMessagesNowait(msg *schema.Message) {
	if msg == nil {
		return
	}
	select {
	case mh.userMessages <- msg:
	default:
		logger.Warn(logComponent).
			Str("event_type", "inbound_queue_full").
			Str("msg_id", msg.ID).
			Msg("入站消息队列已满，丢弃消息")
	}
}

// StartForwarding 启动入站转发循环。
//
// 对齐 Python start_forwarding：启动 forwardLoop，
// 并通过 SetServerPushHandler 注册 push 回调（对齐 Python set_server_push_handler）。
// 出站派发循环由 ChannelManager.StartDispatch 启动。
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

	// 注册 push 回调（对齐 Python set_server_push_handler）
	if mh.agentClient != nil {
		mh.agentClient.SetServerPushHandler(func(msg map[string]any) {
			mh.handleAgentServerPush(msg)
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// cancelAllStreamTasks 取消所有流式任务
//
// 对齐 Python stop_forwarding (L2851-2883)：
// 清理所有流式任务映射 + evolution/query/processing_status 状态。
func (mh *MessageHandler) cancelAllStreamTasks() {
	mh.streamMu.Lock()
	defer mh.streamMu.Unlock()

	for reqID, entry := range mh.streamTasks {
		if entry != nil && entry.cancel != nil {
			entry.cancel()
		}
		logger.Debug(logComponent).
			Str("event_type", "stream_task_cancelled").
			Str("request_id", reqID).
			Msg("流式任务已取消")
	}
	mh.streamTasks = make(map[string]*streamTaskEntry)
	mh.streamSessions = make(map[string]string)
	mh.streamMetadata = make(map[string]map[string]any)
	mh.streamModes = make(map[string]string)
	mh.streamEmitsProcessingStatus = make(map[string]bool)

	// 清理 evolution 状态
	mh.evolutionMu.Lock()
	mh.sessionEvolutionInProgress = make(map[string]bool)
	mh.pendingEvolutionApproval = make(map[string]string)
	mh.queuedSupplementInput = make(map[string]map[string]any)
	mh.evolutionMu.Unlock()

	// 清理用户查询上下文
	mh.queryMu.Lock()
	mh.sessionLastUserQuery = make(map[string]string)
	mh.queryMu.Unlock()
}

// rememberUserQueryContext 记录用户查询上下文。
//
// 对齐 Python _remember_user_query_context (L223-235)：
// 记录 chat.send 的 query 上下文，供 supplement 构造 continuation query 使用。
// 仅记录 chat.send 消息，跳过 is_supplement=True 的消息，query 截断到 8000 字符。
func (mh *MessageHandler) rememberUserQueryContext(msg *schema.Message) {
	if msg == nil {
		return
	}
	// 对齐 Python _is_chat_send_message：仅记录 chat.send 消息
	if msg.ReqMethod != schema.ReqMethodChatSend {
		return
	}
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		return
	}
	if len(msg.Params) == 0 {
		return
	}
	var paramsMap map[string]any
	if err := json.Unmarshal(msg.Params, &paramsMap); err != nil {
		return
	}
	// 对齐 Python：跳过 is_supplement=True 的消息
	if isSupplement, _ := paramsMap["is_supplement"].(bool); isSupplement {
		return
	}
	query := ""
	if q, ok := paramsMap["query"]; ok {
		if s, isStr := q.(string); isStr && s != "" {
			query = s
		}
	}
	if query == "" {
		if c, ok := paramsMap["content"]; ok {
			if s, isStr := c.(string); isStr && s != "" {
				query = s
			}
		}
	}
	if query == "" {
		return
	}
	// 对齐 Python：截断到 8000 字符
	if len(query) > 8000 {
		query = query[:8000]
	}
	mh.queryMu.Lock()
	mh.sessionLastUserQuery[sessionID] = query
	mh.queryMu.Unlock()
}

// getSessionLastUserQuery 获取指定 session 的最近一次用户查询。
//
// 对齐 Python _get_session_last_user_query (L236-237)。
func (mh *MessageHandler) getSessionLastUserQuery(sessionID string) string {
	mh.queryMu.RLock()
	defer mh.queryMu.RUnlock()
	return mh.sessionLastUserQuery[sessionID]
}
