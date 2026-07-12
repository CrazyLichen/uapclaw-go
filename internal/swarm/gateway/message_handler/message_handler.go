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

	// sessionLastUserQuery 最近用户查询文本：sessionID → query（对齐 Python _session_last_user_query）
	sessionLastUserQuery map[string]string

	// mu 互斥锁
	mu sync.Mutex
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
		agentClient:          agentClient,
		userMessages:         make(chan *schema.Message, 256),
		robotMessages:        make(chan *schema.Message, 256),
		streamTasks:          make(map[string]context.CancelFunc),
		streamSessions:       make(map[string]string),
		streamMetadata:       make(map[string]map[string]any),
		streamModes:          make(map[string]string),
		channelStates:        make(map[string]*ChannelControlState),
		sessionLastUserQuery: make(map[string]string),
	}
}

// HandleMessage 处理入站消息（用户→Agent）。
//
// 将消息写入 userMessages channel，由 forwardLoop 异步消费。
// 对齐 Python handle_message：先调用 _remember_user_query_context，再 put_nowait。
//
// 对齐 Python: MessageHandler.handle_message()
func (mh *MessageHandler) HandleMessage(msg *schema.Message) {
	if msg == nil {
		return
	}
	// 记录用户查询上下文（对齐 Python _remember_user_query_context）
	mh.rememberUserQueryContextFromMsg(msg)
	select {
	case mh.userMessages <- msg:
		logger.Debug(logComponent).
			Str("event_type", "handle_inbound").
			Str("msg_id", msg.ID).
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
// timeout <= 0 时非阻塞：有消息立即返回，无消息返回 nil（对齐 Python get_nowait()）。
// timeout > 0 时带超时等待。
//
// 对齐 Python: MessageHandler.consume_robot_messages()
func (mh *MessageHandler) ConsumeRobotMessages(timeout time.Duration) *schema.Message {
	if timeout <= 0 {
		// 非阻塞模式：对齐 Python get_nowait()，确定性读取
		select {
		case msg := <-mh.robotMessages:
			return msg
		default:
			return nil
		}
	}
	// 带超时模式
	select {
	case msg := <-mh.robotMessages:
		return msg
	case <-time.After(timeout):
		return nil
	}
}

// ConsumeUserMessages 从入站队列消费一条消息，超时返回 nil。
//
// timeout <= 0 时非阻塞：有消息立即返回，无消息返回 nil（对齐 Python get_nowait()）。
// timeout > 0 时带超时等待。
//
// 对齐 Python: MessageHandler.consume_user_messages()
func (mh *MessageHandler) ConsumeUserMessages(timeout time.Duration) *schema.Message {
	if timeout <= 0 {
		// 非阻塞模式：对齐 Python get_nowait()，确定性读取
		select {
		case msg := <-mh.userMessages:
			return msg
		default:
			return nil
		}
	}
	// 带超时模式
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

// GetSessionLastUserQuery 获取指定会话的最近用户查询文本。
//
// 对齐 Python: MessageHandler._get_session_last_user_query(session_id)
func (mh *MessageHandler) GetSessionLastUserQuery(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	mh.mu.Lock()
	defer mh.mu.Unlock()
	return mh.sessionLastUserQuery[sessionID]
}

// rememberUserQueryContextFromMsg 从消息中提取并记录用户查询上下文。
//
// 对齐 Python: MessageHandler._remember_user_query_context(msg)
// 仅对 chat.send 消息记录，跳过 is_supplement=True 的消息。
func (mh *MessageHandler) rememberUserQueryContextFromMsg(msg *schema.Message) {
	// 仅处理 chat.send
	if msg.ReqMethod != schema.ReqMethodChatSend {
		return
	}
	// 从 msg.Params 提取参数（Params 为 json.RawMessage）
	params := make(map[string]any)
	if len(msg.Params) == 0 || json.Unmarshal(msg.Params, &params) != nil {
		return
	}
	// 跳过 supplement 消息
	if isSupplement, _ := params["is_supplement"].(bool); isSupplement {
		return
	}
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		return
	}
	// 提取 query 或 content
	var query string
	if q, ok := params["query"].(string); ok && q != "" {
		query = q
	} else if c, ok := params["content"].(string); ok && c != "" {
		query = c
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	// 截断到 8000 字符（对齐 Python query[:8000]）
	if len(query) > 8000 {
		query = query[:8000]
	}
	mh.mu.Lock()
	defer mh.mu.Unlock()
	mh.sessionLastUserQuery[sessionID] = query
}

// rememberUserQueryContext 记录用户查询上下文（直接传参版本）
//
// 对齐 Python _remember_user_query_context：记录 chat.send 的 query 上下文。
func (mh *MessageHandler) rememberUserQueryContext(sessionID, query string) {
	if sessionID == "" || query == "" {
		return
	}
	if len(query) > 8000 {
		query = query[:8000]
	}
	mh.mu.Lock()
	defer mh.mu.Unlock()
	mh.sessionLastUserQuery[sessionID] = query
}
