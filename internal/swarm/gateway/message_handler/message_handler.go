package message_handler

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	hooksgw "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/hooks"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OutboundPipeline 出站管道接口，对齐 Python _outbound_pipeline。
// Python apply(msg) 原地修改 metadata 返回 None；Go 以 error 返回不可恢复错误
// （常规短路返回 nil）。具体类型 im_pipeline.IMOutboundPipeline 隐式实现（破环：
// 避免 import im_pipeline）。
type OutboundPipeline interface {
	// Apply 对出站消息应用路由决策，原地修改 msg
	// Python: publish_robot_messages L1196-1200 await self._outbound_pipeline.apply(msg)
	Apply(ctx context.Context, msg *schema.Message) error
}

// InboundPipelineAPI 入站管道接口，对齐 Python _inbound_pipeline。
// 具体类型 im_pipeline.IMInboundPipeline 隐式实现（破环：避免 import im_pipeline）。
// Python: _forward_loop L2440-2447
type InboundPipelineAPI interface {
	// Apply 对入站消息应用管道处理，返回是否继续转发
	Apply(ctx context.Context, msg *schema.Message) bool
}

// streamTaskEntry 流式任务条目，包含取消函数和退出等待。
// Python: asyncio.Task 的 cancel + gather 等待语义
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
	// Python: _pending_evolution_approval
	pendingEvolutionApproval map[string]string
	// queuedSupplementInput 排队的补充输入：sessionID → {new_input, attachments}
	// Python: _queued_supplement_input
	queuedSupplementInput map[string]map[string]any
	// sessionEvolutionInProgress 正在演进审批的 session 集合
	// Python: _session_evolution_in_progress
	sessionEvolutionInProgress map[string]bool

	// streamEmitsProcessingStatus 流式 processing_status 追踪：requestID → should emit
	// Python: _stream_emits_processing_status
	streamEmitsProcessingStatus map[string]bool

	// queryMu 用户查询上下文锁
	queryMu sync.RWMutex
	// sessionLastUserQuery 用户最近查询：sessionID → last query
	// Python: _session_last_user_query
	sessionLastUserQuery map[string]string

	// getConfigRaw 读取 config 原始数据回调（对齐 Python _get_config_raw，由外部注入）
	getConfigRaw func() map[string]any
	// updateChannelInConfig 更新 config 中渠道配置回调（对齐 Python _update_channel_in_config，由外部注入）
	updateChannelInConfig func(channelID string, update map[string]any)

	// sessionMap 企业通道会话映射（对齐 Python _session_map，11.7）
	sessionMap *routing.SessionMap
	// gatewayHookHandler Gateway 层钩子处理器（对齐 Python _gateway_hook_handler，11.13；装配失败为 nil）
	gatewayHookHandler *hooksgw.GatewayHookHandler

	// outboundPipeline 出站管道（对齐 Python _outbound_pipeline，11.12）
	// Python: set_outbound_pipeline 注入；Go 由 app_gateway 装配时设置
	outboundPipeline OutboundPipeline

	// inboundPipeline 入站管道（对齐 Python _inbound_pipeline，11.12）
	// Python: set_inbound_pipeline 注入；Go 由 app_gateway 装配时设置
	inboundPipeline InboundPipelineAPI

	// cronController cron 控制器（对齐 Python _cron_controller，11.10）
	// Python: set_cron_controller 注入；Go 由 app_gateway 装配时设置
	cronController CronControllerAPI
}

// CronControllerAPI handleCronPushPayload 所需的 CronController 接口。
// gateway/cron.CronController 隐式实现（破环：避免 import gateway/cron）。
// 对齐 Python: _handle_cron_push_payload 中调用的 CronController 方法面。
type CronControllerAPI interface {
	// ListJobs 列出全部任务
	ListJobs() ([]map[string]any, error)
	// GetJob 获取单个任务
	GetJob(jobID string) (map[string]any, error)
	// CreateJob 创建任务
	CreateJob(params map[string]any) (map[string]any, error)
	// UpdateJob 更新任务
	UpdateJob(jobID string, patch map[string]any) (map[string]any, error)
	// DeleteJob 删除任务
	DeleteJob(jobID string) (bool, error)
	// ToggleJob 启停任务
	ToggleJob(jobID string, enabled bool) (map[string]any, error)
	// PreviewJob 预览下 N 次执行
	PreviewJob(jobID string, count int) ([]map[string]any, error)
	// RunNow 立即执行
	RunNow(jobID string) (string, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageHandler 创建消息处理器。
//
// Python: MessageHandler(agent_client) — 只需 1 个参数。
func NewMessageHandler(agentClient *routing.AgentClient) *MessageHandler {
	m := &MessageHandler{
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

	// 11.7：企业通道会话映射（scope 从 config 读一次）
	// Python: self._session_map = SessionMap() (L155)
	m.sessionMap = routing.NewSessionMap(nil)

	// 11.13：Gateway hooks handler 装配（失败 Warn + nil，对齐 Python L163-170）
	// Python: load_hooks_config(get_config()) + GatewayHookHandler(config)
	m.gatewayHookHandler = m.initGatewayHookHandler()

	return m
}

// HandleMessage 处理入站消息（用户→Agent）。
//
// 将消息写入 userMessages channel，由 forwardLoop 异步消费。
// Python: handle_message：非阻塞写入，channel 满时丢弃并记录警告。
//
// Python: MessageHandler.handle_message()
func (mh *MessageHandler) HandleMessage(msg *schema.Message) {
	if msg == nil {
		return
	}
	// Python: handle_message：入队前记录用户查询上下文
	mh.rememberUserQueryContext(msg)
	select {
	case mh.userMessages <- msg:
		logger.Info(logComponent).
			Str("event_type", "handle_inbound").
			Str("msg_id", msg.ID).
			Str("channel_id", msg.ChannelID).
			Str("session_id", msg.SessionID).
			Msg("inbound message enqueued")
	default:
		logger.Warn(logComponent).
			Str("event_type", "handle_inbound_dropped").
			Str("msg_id", msg.ID).
			Msg("inbound message queue full, dropping message")
	}
}

// ConsumeRobotMessages 从出站队列消费一条消息，超时返回 nil。
//
// 供 ChannelManager 的出站派发循环调用。
// Python: MessageHandler.consume_robot_messages()
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
// Python: MessageHandler.consume_user_messages()
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
// Python: MessageHandler.publish_user_messages_nowait()
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
			Msg("inbound message queue full, dropping message")
	}
}

// TriggerSessionStartHook 供 Channel 层调用，触发 SessionStart hook。
//
// Python: trigger_session_start_hook (L172-177)：asyncio.create_task fire-and-forget。
func (mh *MessageHandler) TriggerSessionStartHook(sessionID, source string) {
	if mh.gatewayHookHandler == nil {
		return
	}
	go mh.gatewayHookHandler.OnSessionStart(context.Background(), sessionID, source)
}

// GetGatewayHookHandlerForTest 返回 gatewayHookHandler（仅供跨包测试断言使用）。
func (mh *MessageHandler) GetGatewayHookHandlerForTest() *hooksgw.GatewayHookHandler {
	return mh.gatewayHookHandler
}

// StartForwarding 启动入站转发循环。
//
// Python: start_forwarding：启动 forwardLoop，
// 并通过 SetServerPushHandler 注册 push 回调（对齐 Python set_server_push_handler）。
// 出站派发循环由 ChannelManager.StartDispatch 启动。
func (mh *MessageHandler) StartForwarding(ctx context.Context) error {
	if mh.running.Load() {
		logger.Warn(logComponent).Msg("MessageHandler already running")
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
		Msg("MessageHandler forwarding loop started")
	return nil
}

// StopForwarding 停止转发循环
//
// Python: stop_forwarding：取消所有流式任务 + 取消上下文。
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
		Msg("MessageHandler forwarding loop stopped")
	return nil
}

// SetCronController 注入 cron 控制器。
// 对齐 Python: set_cron_controller (message_handler.py L1674-1675)。
func (mh *MessageHandler) SetCronController(cc CronControllerAPI) {
	mh.cronController = cc
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cancelAllStreamTasks 取消所有流式任务
//
// Python: stop_forwarding (L2851-2883)：
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
			Msg("streaming task cancelled")
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
// Python: _remember_user_query_context (L223-235)：
// 记录 chat.send 的 query 上下文，供 supplement 构造 continuation query 使用。
// 仅记录 chat.send 消息，跳过 is_supplement=True 的消息，query 截断到 8000 字符。
func (mh *MessageHandler) rememberUserQueryContext(msg *schema.Message) {
	if msg == nil {
		return
	}
	// Python: _is_chat_send_message：仅记录 chat.send 消息
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
	// Python: 跳过 is_supplement=True 的消息
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
	// Python: 截断到 8000 字符
	if len(query) > 8000 {
		query = query[:8000]
	}
	mh.queryMu.Lock()
	mh.sessionLastUserQuery[sessionID] = query
	mh.queryMu.Unlock()
}

// getSessionLastUserQuery 获取指定 session 的最近一次用户查询。
//
// Python: _get_session_last_user_query (L236-237)。
func (mh *MessageHandler) getSessionLastUserQuery(sessionID string) string {
	mh.queryMu.RLock()
	defer mh.queryMu.RUnlock()
	return mh.sessionLastUserQuery[sessionID]
}

// initGatewayHookHandler 装配 Gateway hooks handler。
//
// Python: L163-170 try/except——LoadHooksConfig 失败时 Warn + 返回 nil。
func (mh *MessageHandler) initGatewayHookHandler() *hooksgw.GatewayHookHandler {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Any("panic", r).
				Msg("Failed to init GatewayHookHandler")
		}
	}()
	cfg := hookscfg.GetGlobalConfigForScope()
	if cfg == nil {
		logger.Warn(logComponent).
			Msg("Failed to init GatewayHookHandler: global config not registered")
		return nil
	}
	configBase, err := cfg.Load()
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("Failed to init GatewayHookHandler")
		return nil
	}
	hooksCfg := hookscfg.LoadHooksConfig(configBase)
	return hooksgw.NewGatewayHookHandler(hooksCfg)
}
