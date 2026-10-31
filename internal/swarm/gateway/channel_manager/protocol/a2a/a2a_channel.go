package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cm "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// A2AChannelConfig A2A 入站通道配置。
//
// Python: a2a_connect.py A2AChannelConfig (L23-35)，11 字段一一对应；
// ChannelID 在 Python 为固定默认值 "a2a"（无 env），Go 同。
type A2AChannelConfig struct {
	// Enabled 是否启用通道（默认 false）
	Enabled bool
	// Host HTTP 监听地址
	Host string
	// Port HTTP 监听端口
	Port int
	// RPCPath JSON-RPC 入口路径
	RPCPath string
	// CardPath Agent Card 路径
	CardPath string
	// ExtendedCardPath Extended Agent Card 路径
	ExtendedCardPath string
	// ProtocolVersion 写入 AgentInterface 的协议版本
	ProtocolVersion string
	// ChannelID 通道标识
	ChannelID string
	// AppName Agent Card name
	AppName string
	// AppDescription Agent Card description
	AppDescription string
	// AppVersion Agent Card version
	AppVersion string
}

// pendingRequest 在途 A2A 请求的应答队列。
//
// Python: a2a_connect.py _PendingA2ARequest (L38-40)
type pendingRequest struct {
	// ch 内部消息回包队列（有界，满则丢弃告警，偏差 D11）
	ch chan *schema.Message
}

// A2AChannel A2A 入站通道。
//
// 协议栈交给 a2asrv，本类型只做网关桥：pending 配对、内部 Message 构造、HTTP 生命周期。
// Python: a2a_connect.py A2AChannel (L179-500)
type A2AChannel struct {
	// cfg 通道配置
	cfg A2AChannelConfig
	// mu 保护 pending / onMessageCb / srv
	mu sync.RWMutex
	// pending requestID → 应答队列
	pending map[string]*pendingRequest
	// onMessageCb 入站回调（由 ChannelManager.RegisterChannel 注入）
	onMessageCb func(*schema.Message) bool
	// running 运行标志
	running atomic.Bool
	// srv HTTP 服务器（Start 后非 nil）
	srv *http.Server
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentGateway
	// defaultListenPort A2A 默认监听端口
	defaultListenPort = 19100
	// defaultChannelID 通道标识默认值（Python 无 env，固定 "a2a"）
	defaultChannelID = "a2a"
	// pendingBufferSize 单个在途请求的应答队列容量（偏差 D11：有界背压）
	pendingBufferSize = 64
	// shutdownWakeupError 停止时唤醒在途请求的错误文案
	// Python: a2a_connect.py stop() L298
	shutdownWakeupError = "a2a channel stopped"
	// startProbeWindow Start 后的存活检查窗口（对齐 Python await sleep(0.2) 后判定）
	startProbeWindow = 200 * time.Millisecond
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ConfigFromEnv 从环境变量构造通道配置。
//
// getenv 可注入（生产传 os.Getenv），便于测试；所有字符串值 TrimSpace 后
// 为空则回落默认（对齐 Python str(...).strip() or 默认）。
// Python: app_gateway.py L1071-1107
func ConfigFromEnv(getenv func(string) string) A2AChannelConfig {
	cfg := A2AChannelConfig{
		Enabled:          a2aEnvBool(getenv("A2A_SERVER_ENABLED")),
		Host:             orDefault(strings.TrimSpace(getenv("A2A_SERVER_HOST")), "127.0.0.1"),
		Port:             defaultListenPort,
		RPCPath:          orDefault(strings.TrimSpace(getenv("A2A_SERVER_PATH")), "/a2a"),
		CardPath:         orDefault(strings.TrimSpace(getenv("A2A_SERVER_CARD_PATH")), "/.well-known/agent-card.json"),
		ExtendedCardPath: orDefault(strings.TrimSpace(getenv("A2A_SERVER_EXTENDED_CARD_PATH")), "/agent/authenticatedExtendedCard"),
		ProtocolVersion:  orDefault(strings.TrimSpace(getenv("A2A_SERVER_PROTOCOL_VERSION")), "1.0.0"),
		ChannelID:        defaultChannelID,
		// 偏差 D9：品牌串映射 JiuwenClaw → UapClaw
		AppName:        orDefault(strings.TrimSpace(getenv("A2A_SERVER_APP_NAME")), "UapClaw Gateway A2A Server"),
		AppDescription: orDefault(strings.TrimSpace(getenv("A2A_SERVER_APP_DESCRIPTION")), "A2A ingress for UapClaw Gateway"),
		AppVersion:     orDefault(strings.TrimSpace(getenv("A2A_SERVER_APP_VERSION")), "0.1.0"),
	}

	if raw := strings.TrimSpace(getenv("A2A_SERVER_PORT")); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			cfg.Port = p
		} else {
			// 审查惯例：Python int() 会直接抛 ValueError 炸装配；Go 静默回落但补 Warn 保证可观测
			logger.Warn(logComponent).
				Str("env", "A2A_SERVER_PORT").
				Str("value", raw).
				Msg("invalid A2A_SERVER_PORT, fallback to default")
		}
	}
	return cfg
}

// NewA2AChannel 创建 A2A 通道。
func NewA2AChannel(cfg A2AChannelConfig) *A2AChannel {
	return &A2AChannel{
		cfg:     cfg,
		pending: make(map[string]*pendingRequest),
	}
}

// ChannelID 返回通道标识（去空回落 "a2a"）。
//
// Python: a2a_connect.py channel_id property (L190-192)
func (ch *A2AChannel) ChannelID() string {
	if id := strings.TrimSpace(ch.cfg.ChannelID); id != "" {
		return id
	}
	return defaultChannelID
}

// ChannelType 返回通道类型枚举（偏差 D10：Go 侧新增 ChannelTypeA2A）。
func (ch *A2AChannel) ChannelType() cm.ChannelType { return cm.ChannelTypeA2A }

// Config 返回通道配置。
func (ch *A2AChannel) Config() any { return ch.cfg }

// IsRunning 返回通道是否运行中。
func (ch *A2AChannel) IsRunning() bool { return ch.running.Load() }

// OnMessage 注册入站回调。
//
// Python: a2a_connect.py on_message (L194-195)
func (ch *A2AChannel) OnMessage(callback func(*schema.Message) bool) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.onMessageCb = callback
}

// Send 把内部回包投递给对应的在途请求；未命中静默丢弃。
//
// Python: a2a_connect.py send (L305-309)
func (ch *A2AChannel) Send(_ context.Context, msg *schema.Message) error {
	if msg == nil {
		return nil
	}
	ch.mu.RLock()
	pending := ch.pending[msg.ID]
	ch.mu.RUnlock()
	if pending == nil {
		return nil
	}
	select {
	case pending.ch <- msg:
	default:
		// 偏差 D11：有界队列满则丢弃并告警，避免阻塞出站派发
		logger.Warn(logComponent).
			Str("msg_id", msg.ID).
			Msg("A2A pending queue full, dropping message")
	}
	return nil
}

// DispatchA2ARequest 构造内部 chat.send 请求并交给入站回调，返回应答队列。
//
// Python: a2a_connect.py dispatch_a2a_request (L311-343)
func (ch *A2AChannel) DispatchA2ARequest(
	_ context.Context,
	requestID, sessionID, query string,
	files []map[string]any,
	metadata map[string]any,
) (*pendingRequest, error) {
	ch.mu.Lock()
	cb := ch.onMessageCb
	pending := &pendingRequest{ch: make(chan *schema.Message, pendingBufferSize)}
	ch.pending[requestID] = pending
	ch.mu.Unlock()

	if cb == nil {
		ch.ClearPendingRequest(requestID)
		return nil, fmt.Errorf("A2AChannel on_message callback is not set")
	}

	rawParams, err := json.Marshal(BuildRequestParams(query, files))
	if err != nil {
		ch.ClearPendingRequest(requestID)
		return nil, fmt.Errorf("marshal request params: %w", err)
	}

	msg := &schema.Message{
		ID:        requestID,
		Type:      schema.MessageTypeReq,
		ChannelID: ch.ChannelID(),
		SessionID: sessionID,
		Params:    rawParams,
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		OK:        true,
		ReqMethod: schema.ReqMethodChatSend,
		IsStream:  true,
		Metadata:  metadata,
	}
	cb(msg)
	return pending, nil
}

// ClearPendingRequest 移除在途请求（不关闭队列，避免 Send 与关闭竞态）。
//
// Python: a2a_connect.py clear_pending_request (L345-346)
func (ch *A2AChannel) ClearPendingRequest(requestID string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	delete(ch.pending, requestID)
}

// Stop 停止通道：关闭 HTTP 服务并向所有在途请求投递唤醒消息。
//
// Python: a2a_connect.py stop (L276-303)
func (ch *A2AChannel) Stop(ctx context.Context) error {
	ch.running.Store(false)

	ch.mu.Lock()
	srv := ch.srv
	ch.mu.Unlock()

	var shutdownErr error
	if srv != nil {
		shutdownErr = srv.Shutdown(ctx)
	}

	ch.mu.Lock()
	for id, pending := range ch.pending {
		wakeup := &schema.Message{
			ID:        "a2a_shutdown",
			Type:      schema.MessageTypeEvent,
			ChannelID: ch.ChannelID(),
			Timestamp: float64(time.Now().UnixNano()) / 1e9,
			OK:        false,
			Payload:   map[string]any{"error": shutdownWakeupError, "is_complete": true},
			EventType: schema.EventTypeChatError,
		}
		select {
		case pending.ch <- wakeup:
		default:
			logger.Warn(logComponent).Str("request_id", id).Msg("failed to deliver A2A shutdown wakeup")
		}
	}
	ch.pending = make(map[string]*pendingRequest)
	ch.mu.Unlock()

	logger.Info(logComponent).Msg("A2A channel stopped")
	return shutdownErr
}

// C 返回应答队列的只读通道（executor 与测试消费用）。
func (p *pendingRequest) C() <-chan *schema.Message { return p.ch }

// ──────────────────────────── 非导出函数 ────────────────────────────
// orDefault 值非空则返回，否则返回默认值（对齐 Python `x.strip() or 默认`）。
func orDefault(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

// a2aEnvBool 解析布尔型 env（1/true/yes/on 为真）。
//
// Python: app_gateway.py L1071-1075 的集合判断
func a2aEnvBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
