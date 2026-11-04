package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

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
	// AppName Agent Card 中的名称
	AppName string
	// AppDescription Agent Card 中的描述
	AppDescription string
	// AppVersion Agent Card 中的版本号
	AppVersion string
}

// pendingRequest 在途 A2A 请求的应答队列。
//
// Python: a2a_connect.py _PendingA2ARequest (L38-40)
type pendingRequest struct {
	// ch 内部消息回包队列（有界，满则丢弃告警，偏差 D11）
	ch chan *schema.Message
	// terminal 终态保底槽（容量 1）。
	//
	// M-01：主队列满时终态消息若被丢弃，executor 永远收不到收尾事件，任务会
	// 永久停在 WORKING、SSE 连接常驻。终态改投本槽（容量 1 即可，单请求只有一个
	// 终态），既不阻塞出站派发（dispatchRobotMessages 为全渠道单 goroutine），
	// 也不丢终态。
	terminal chan *schema.Message
}

// deliverTerminal 向终态保底槽投递（满则覆盖旧值，保证最新终态一定可读）。
func (p *pendingRequest) deliverTerminal(msg *schema.Message) {
	select {
	case p.terminal <- msg:
		return
	default:
	}
	// 槽已满：丢弃旧的，换新的（单写者：出站派发 goroutine / Cancel / Stop）
	select {
	case <-p.terminal:
	default:
	}
	select {
	case p.terminal <- msg:
	default:
	}
}

// A2AChannel A2A 入站通道。
//
// 协议栈交给 a2asrv，本类型只做网关桥：pending 配对、内部 Message 构造、HTTP 生命周期。
// Python: a2a_connect.py A2AChannel (L179-500)
type A2AChannel struct {
	// cfg 通道配置
	cfg A2AChannelConfig
	// mu 保护 pending / onMessageCb / srv / state
	mu sync.RWMutex
	// pending requestID → 应答队列
	pending map[string]*pendingRequest
	// onMessageCb 入站回调（由 ChannelManager.RegisterChannel 注入）
	onMessageCb func(*schema.Message) bool
	// running 运行标志
	running atomic.Bool
	// srv HTTP 服务器（Start 后非 nil）
	srv *http.Server
	// state 启停状态机（T-07）：Start 与 Stop 互斥，消除「Stop 先到、Start 随后照常监听」
	state a2aChannelState
	// stopCh 通道级停止信号：Stop 时 close，executor 的 select 直接退出。
	//
	// S-04：SDK 用 context.WithoutCancel 派生执行 ctx（a2a-go 的
	// internal/taskexec/local_manager.go），ctx.Done() 永不触发，只能靠自有信号；
	// 该信号不受应答队列容量影响，避免队列满时唤醒被丢弃而吃满停机超时。
	stopCh chan struct{}
	// stopOnce 保证 stopCh 只关闭一次（Stop 可能被调用多次）
	stopOnce sync.Once
}

// a2aChannelState 通道启停状态。
type a2aChannelState int

const (
	// a2aStateIdle 未启动
	a2aStateIdle a2aChannelState = iota
	// a2aStateStarting 启动中（已过状态检查、尚未完成监听/探活）
	a2aStateStarting
	// a2aStateRunning 运行中
	a2aStateRunning
	// a2aStateStopping 停止中（Start 收尾时需自行关闭）
	a2aStateStopping
)

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

// ──────────────────────────── 全局变量 ────────────────────────────

// a2aChannelBaseChannelAssert 编译期断言：A2AChannel 必须实现 channel_manager.BaseChannel。
// 任一方法签名漂移（Config/Start/Stop/Send/OnMessage/IsRunning/ChannelID/ChannelType）
// 都会在此处直接编译失败，而非等到注册点变更后运行时才暴露。
var _ cm.BaseChannel = (*A2AChannel)(nil)

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
		// M-03：默认值取 SDK 的 Version（"1.0"）。a2asrv 把请求 ctx 的协议版本钉死为
		// a2a.Version 并做严格相等比较（a2asrv/intercepted_handler.go），卡片里写
		// "1.0.0" 会让按卡片回填 A2A-Version 的第三方客户端一律被 -32009 拒绝。
		ProtocolVersion: orDefault(strings.TrimSpace(getenv("A2A_SERVER_PROTOCOL_VERSION")), string(sdka2a.Version)),
		ChannelID:       defaultChannelID,
		// 偏差 D9：品牌串映射 JiuwenClaw → UapClawSwarm
		AppName:        orDefault(strings.TrimSpace(getenv("A2A_SERVER_APP_NAME")), "UapClawSwarm Gateway A2A Server"),
		AppDescription: orDefault(strings.TrimSpace(getenv("A2A_SERVER_APP_DESCRIPTION")), "A2A ingress for UapClawSwarm Gateway"),
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
		stopCh:  make(chan struct{}),
	}
}

// Stopped 返回通道级停止信号（executor 的 select 分支用）。
//
// S-04：Stop 关闭该 channel，所有在途 Execute 立即退出，不依赖应答队列投递。
func (ch *A2AChannel) Stopped() <-chan struct{} { return ch.stopCh }

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
	// M-01：终态走独立保底槽，主队列满也不会丢终态
	if IsTerminalMessage(msg) {
		pending.deliverTerminal(msg)
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
	pending := &pendingRequest{
		ch:       make(chan *schema.Message, pendingBufferSize),
		terminal: make(chan *schema.Message, 1),
	}
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
	// M-05：cb 返回 false 表示「通道已注销 / 处理器为空」，此时无人回包，
	// 必须清理 pending 并向 executor 返错（executor 已有转 FAILED 的分支），
	// 否则任务永久挂在 WORKING、HTTP 侧只看到超时。
	if !cb(msg) {
		ch.ClearPendingRequest(requestID)
		return nil, fmt.Errorf("A2AChannel on_message callback rejected request %s", requestID)
	}
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

// Stop 停止通道：先投递唤醒/关闭停止信号，再关闭 HTTP 服务。
//
// Python: a2a_connect.py stop (L276-303)
//
// S-04：顺序必须是「唤醒 → Shutdown」。SSE handler（executor）只有收到终态、
// yield 失败或通道停止信号才返回，而 http.Server.Shutdown 只等 handler 返回、
// 不强断连接；若先 Shutdown 再唤醒，两者互等到 ctx 超时（默认 5s），
// 且后续通道会拿到已过期的 ctx。SDK 执行 ctx 是 WithoutCancel 派生的，
// ctx.Done() 不可依赖，故用通道级 stopCh 兜底（不占应答队列容量）。
func (ch *A2AChannel) Stop(ctx context.Context) error {
	ch.mu.Lock()
	switch ch.state {
	case a2aStateIdle:
		// 从未启动且无在途请求：无需关闭任何东西（可能仍在 Start 之前）。
		// 仍有 pending 时继续走唤醒流程，避免调用方拿到的队列永远等不到收尾。
		if len(ch.pending) == 0 {
			ch.mu.Unlock()
			return nil
		}
		ch.state = a2aStateStopping
	case a2aStateStarting:
		// 启动窗口内到达：交给 Start 收尾时自行关闭，避免「起了却停不掉」
		ch.state = a2aStateStopping
		ch.running.Store(false)
		ch.mu.Unlock()
		return nil
	default:
		ch.state = a2aStateStopping
	}
	srv := ch.srv
	ch.mu.Unlock()

	ch.running.Store(false)
	// 1) 通道级停止信号：所有在途 Execute 直接退出
	ch.stopOnce.Do(func() { close(ch.stopCh) })

	// 2) 投递唤醒（终态槽 + 主队列），并清空 pending
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
		pending.deliverTerminal(wakeup)
		select {
		case pending.ch <- wakeup:
		default:
			logger.Warn(logComponent).Str("request_id", id).Msg("failed to deliver A2A shutdown wakeup")
		}
	}
	ch.pending = make(map[string]*pendingRequest)
	ch.mu.Unlock()

	// 3) 等活跃 handler 返回；超时则强制关闭连接
	var shutdownErr error
	if srv != nil {
		shutdownErr = srv.Shutdown(ctx)
		if shutdownErr != nil {
			logger.Warn(logComponent).Err(shutdownErr).Msg("A2A shutdown timed out, forcing close")
			if closeErr := srv.Close(); closeErr != nil {
				logger.Warn(logComponent).Err(closeErr).Msg("A2A forced close failed")
			}
		}
	}

	ch.mu.Lock()
	ch.srv = nil
	ch.state = a2aStateIdle
	ch.mu.Unlock()

	logger.Info(logComponent).Msg("A2A channel stopped")
	return shutdownErr
}

// C 返回应答队列的只读通道（executor 与测试消费用）。
func (p *pendingRequest) C() <-chan *schema.Message { return p.ch }

// Terminal 返回终态保底槽的只读通道。
func (p *pendingRequest) Terminal() <-chan *schema.Message { return p.terminal }

// deliverCanceled 向指定请求投递一条取消终态，唤醒仍阻塞在 pending 上的 Execute。
//
// M-02：客户端 tasks/cancel 时 SDK 只把执行标记为 CANCELED 并结束订阅，原 Execute
// 仍阻塞在应答队列上（执行 ctx 被 SDK 以 WithoutCancel 派生，不会随取消结束）。
// 投一条 event_type=chat.interrupt_result 的终态后，terminalState 直接映射为
// Canceled，Execute 立即收尾并 defer 清理 pending。
func (ch *A2AChannel) deliverCanceled(requestID string) {
	ch.mu.RLock()
	pending := ch.pending[requestID]
	ch.mu.RUnlock()
	if pending == nil {
		return
	}
	pending.deliverTerminal(&schema.Message{
		ID:        requestID,
		Type:      schema.MessageTypeEvent,
		ChannelID: ch.ChannelID(),
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		OK:        false,
		Payload:   map[string]any{"is_complete": true},
		EventType: schema.EventTypeChatInterruptResult,
	})
}

// Start 启动 A2A HTTP 服务（未启用则直接返回）。
//
// 对齐 Python start (L197-274)：绑定失败或服务提前退出都向调用方返错。
// ctx 不参与（生命周期由 Stop 控制），形参保持接口一致。
//
// T-07：状态机（idle → starting → running）与 Stop 互斥。原实现只靠
// running 快速判断，Stop 若在 Listen 之前到达会看到 srv==nil 直接返回，
// 而 Start 随后照常监听且把 running 置真，服务再也无法关闭；并发两次 Start
// 也会双 Listen 并覆盖 srv。路径非法时在进入 Serve 之前返错（Go 1.22+ 的
// ServeMux 对缺前导 "/" 会当 host pattern 静默失效，对非法通配符则 panic，
// 而 Start 跑在 go ch.Start(ctx) 里、无 recover 会带走整个进程）。
func (ch *A2AChannel) Start(_ context.Context) (err error) {
	ch.mu.Lock()
	if ch.state != a2aStateIdle {
		ch.mu.Unlock()
		return nil
	}
	if !ch.cfg.Enabled {
		ch.mu.Unlock()
		logger.Info(logComponent).Msg("A2A channel disabled by config")
		return nil
	}
	ch.state = a2aStateStarting
	ch.mu.Unlock()

	// 兜底：任何残余 panic 不得带走进程
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Any("panic", r).Msg("A2A channel start panicked")
			ch.resetStateAfterStartFailure()
			err = fmt.Errorf("a2a channel start panicked: %v", r)
		}
	}()

	if routeErr := validateA2ARoutes(ch.cfg); routeErr != nil {
		ch.resetStateAfterStartFailure()
		return routeErr
	}

	srv := &http.Server{
		Handler:           ch.newMux(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", ch.cfg.Host, ch.cfg.Port)
	ln, listenErr := net.Listen("tcp", addr)
	if listenErr != nil {
		ch.resetStateAfterStartFailure()
		return fmt.Errorf("listen %s: %w", addr, listenErr)
	}

	// 启动窗口内可能已被 Stop 标记：直接放弃监听
	ch.mu.Lock()
	if ch.state == a2aStateStopping {
		ch.state = a2aStateIdle
		ch.mu.Unlock()
		_ = ln.Close()
		logger.Info(logComponent).Msg("A2A start aborted: stop requested during startup")
		return nil
	}
	ch.srv = srv
	ch.mu.Unlock()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	// 存活检查窗口，对齐 Python start (L263-267)：sleep(0.2) 后若 task 已完成则抛出异常
	select {
	case serveErr := <-errCh:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			ch.mu.Lock()
			ch.srv = nil
			ch.state = a2aStateIdle
			ch.mu.Unlock()
			return serveErr
		}
	case <-time.After(startProbeWindow):
	}

	ch.mu.Lock()
	if ch.state == a2aStateStopping {
		ch.state = a2aStateIdle
		ch.srv = nil
		ch.mu.Unlock()
		_ = srv.Close()
		logger.Info(logComponent).Msg("A2A start aborted: stop requested during probe window")
		return nil
	}
	ch.state = a2aStateRunning
	ch.mu.Unlock()

	ch.running.Store(true)
	logger.Info(logComponent).
		Str("url", fmt.Sprintf("http://%s%s", addr, ch.cfg.RPCPath)).
		Msg("A2A channel started")

	// 常驻监视：探活窗口之后 Serve 仍可能失败（监听被关闭等），
	// 对齐 Python app_gateway.py L1111-1124 的 a2a_task.add_done_callback 语义
	go ch.watchServe(errCh)
	return nil
}

// resetStateAfterStartFailure 启动失败时把状态机复位为 idle（Stop 已请求则保持 stopping）。
func (ch *A2AChannel) resetStateAfterStartFailure() {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if ch.state != a2aStateStopping {
		ch.state = a2aStateIdle
	}
}

// validateA2ARoutes 校验三条路由路径的合法性。
//
// T-07：ServeMux 对缺前导 "/" 的 pattern 会当 host pattern 静默失效，对非法通配符
// 直接 panic。这里在监听前返回可读错误，避免「路由 404 查不出原因」或「启动即崩进程」。
func validateA2ARoutes(cfg A2AChannelConfig) error {
	paths := map[string]string{
		"rpc_path":           cfg.RPCPath,
		"card_path":          cfg.CardPath,
		"extended_card_path": cfg.ExtendedCardPath,
	}
	seen := make(map[string]string, len(paths))
	for field, path := range paths {
		if path == "" {
			return fmt.Errorf("a2a channel config %s is empty", field)
		}
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("a2a channel config %s=%q must start with \"/\"", field, path)
		}
		if strings.ContainsAny(path, " \t") {
			return fmt.Errorf("a2a channel config %s=%q must not contain whitespace", field, path)
		}
		if other, dup := seen[path]; dup {
			return fmt.Errorf("a2a channel config %s and %s share path %q", other, field, path)
		}
		seen[path] = field
		// 通配符只允许出现在最后一段且花括号成对（ServeMux 的硬性约束）
		if strings.Contains(path, "{") || strings.Contains(path, "}") {
			if strings.Count(path, "{") != strings.Count(path, "}") {
				return fmt.Errorf("a2a channel config %s=%q has unbalanced braces", field, path)
			}
		}
	}
	return nil
}

// watchServe 常驻读取 Serve 返回值，运行期异常时降级运行态并记 Error。
//
// 正常关闭（Stop → Shutdown 使 Serve 返回 http.ErrServerClosed）保持静默。
func (ch *A2AChannel) watchServe(errCh <-chan error) {
	err, ok := <-errCh
	if !ok {
		return
	}
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return
	}
	ch.running.Store(false)
	ch.mu.Lock()
	ch.state = a2aStateIdle
	ch.mu.Unlock()
	logger.Error(logComponent).
		Err(err).
		Str("addr", fmt.Sprintf("%s:%d", ch.cfg.Host, ch.cfg.Port)).
		Msg("A2A server stopped unexpectedly")
}

// newMux 组装三条路由：JSON-RPC / Agent Card / Extended Agent Card。
//
// Python: A2AFastAPIApplication.build(rpc_url, agent_card_url, extended_agent_card_url) (L244-252)
func (ch *A2AChannel) newMux() *http.ServeMux {
	card := BuildAgentCard(&ch.cfg)
	// 扩展卡与公共卡同源（Python 未配置独立扩展卡，spec §9-D6）
	extendedProducer := func(context.Context, *sdka2a.GetExtendedAgentCardRequest) (*sdka2a.AgentCard, error) {
		return card, nil
	}
	requestHandler := a2asrv.NewHandler(
		executorFor(ch),
		a2asrv.WithExtendedAgentCardProducer(a2asrv.ExtendedAgentCardProducerFn(extendedProducer)),
	)

	mux := http.NewServeMux()
	// Python: create_jsonrpc_routes 路由声明 methods=['POST']，非 POST 应 405
	mux.Handle(ch.cfg.RPCPath, postOnly(a2asrv.NewJSONRPCHandler(requestHandler)))
	mux.Handle(ch.cfg.CardPath, a2asrv.NewStaticAgentCardHandler(card))
	if ch.cfg.ExtendedCardPath != "" {
		mux.Handle(ch.cfg.ExtendedCardPath, a2asrv.NewAgentCardHandler(
			a2asrv.AgentCardProducerFn(func(context.Context) (*sdka2a.AgentCard, error) {
				return card, nil
			}),
		))
	}
	return mux
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// postOnly 只放行 POST 请求，其余返回 405（对齐 Python 路由 methods=['POST']）。
func postOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

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
