package harness_providers

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// 本文件对应 Python openjiuwen/harness_providers/base.py（902 行）：
// 内置 protocol provider 共享的「串行轮次」骨架。
//
// 本包的每个 provider 驱动一次外部 agent 会话，接受的输入一次跑一个：
// 输入变成一个待决轮次，监督器按接受顺序执行待决轮次，每个轮次恰好发一条
// STARTED 与一条终态 TurnLifecycleEvent。骨架拥有生命周期状态机、有界观测
// 缓冲、后续队列、交互记账与 checkpoint 发布；provider 只把自己的 SDK
// 翻译成 OpenSession / ExecuteTurn / Steer / InterruptTurn / CloseSession。
//
// ⚠️ Go 与 Python 的结构差异（spec Q1，规则 9.6 登记的偏差）：
// Python 是 ABC 模板方法基类，子类**继承**并覆盖钩子方法，基类方法体内
// `self._execute_turn(...)` 会动态派发到子类。Go 的嵌入只有静态方法提升，
// 基类方法体内的 `h.ExecuteTurn(...)` 会绑定到基类版本，子类覆盖永不生效
// （编译通过、无告警、功能静默失效——go-embedding-virtual-dispatch-trap）。
// 因此基座内部一律通过 hooks 接口字段动态派发；provider 同时嵌入
// *SerializedTurnHarness 以获得 Emit/Transition 等能力。

// logComponent 本包的日志组件。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体（接口定义）────────────────────────────

// providerHooks provider 定制钩子集合。
//
// 这是基座**向内**调用 provider 的唯一扩展面（对应 Python 的 ABC 抽象方法
// 与可覆盖方法）。3 个 required 与 8 个 optional 全部由 provider 自己实现。
//
// ⚠️ 为什么没有「默认实现集合」可嵌入（spec Q3 实施修正 / 偏差 D14）：
// optional 的某个方法（如 ListModels）与 SerializedTurnHarness 的公开方法
// 同名，provider 同时嵌入基座与默认实现结构体会被判为歧义、编译失败。
// 因此每个 provider 自己写满 8 个 optional；不需要的能力按
// UnsupportedHarnessCapabilityError 报「不支持」，错误消息中的 provider 名
// 由 provider 自己填（与 Python f-string 逐字一致）。
type providerHooks interface {
	// ── required（对应 base.py 的 @abstractmethod，3 个）──

	// OpenSession 启动 provider 运行时并返回其原生会话 ID。
	// Python: base.py:198-200 _open_session
	OpenSession(ctx context.Context, harnessContext harnessprotocol.HarnessContext) (*string, error)

	// CloseSession 释放 provider 运行时；必须为执行中的轮次解阻塞。
	// Python: base.py:202-204 _close_session
	CloseSession(ctx context.Context) error

	// ExecuteTurn 把一个已接受的输入跑到底，期间发射观测事件。
	// Python: base.py:206-208 _execute_turn
	ExecuteTurn(ctx context.Context, turn *PendingTurn) (
		harnessprotocol.TurnEventKind, harnessprotocol.TurnResult, error)

	// ── optional（对应 base.py 的有默认体的方法，8 个）──

	// ValidateContext 宿主不兼容时快速失败；provider 可追加 SDK 限制。
	// Python: base.py:191-196 _validate_context
	ValidateContext(ctx context.Context, harnessContext harnessprotocol.HarnessContext) error

	// Steer 在声明 STEER 能力时把内容注入活跃轮次。
	// Python: base.py:210-218 _steer
	Steer(ctx context.Context, turn *PendingTurn,
		content harnessprotocol.HarnessInput, messageID string) error

	// InterruptTurn 在声明 abort 能力时要求 provider 停止活跃轮次。
	// Python: base.py:220-223 _interrupt_turn
	InterruptTurn(ctx context.Context, turn *PendingTurn, mode harnessprotocol.AbortMode) error

	// ListModels 在声明 MODEL_DISCOVERY 能力时探测 provider 的模型目录。
	// Python: base.py:225-227 _list_models
	ListModels(ctx context.Context) ([]harnessprotocol.ModelOption, error)

	// AuthFallbackPayload 描述已配置的认证回退模型；false 表示未配置。
	// Python: base.py:229-239 _auth_fallback_payload
	AuthFallbackPayload() (harnessprotocol.JsonObject, bool)

	// ReconnectForAuth 在回退或主模型上重连厂商运行时。
	// Python: base.py:241-252 _reconnect_for_auth
	ReconnectForAuth(ctx context.Context, fallback bool) error

	// OnAuthEndpointChanged 记录会话已切到回退（或主）模型。
	// Python: base.py:254-256 _on_auth_endpoint_changed（默认 no-op）
	OnAuthEndpointChanged(ctx context.Context, fallback bool) error

	// ApplyModelSelection 把活跃会话切到 selection。
	// Python: base.py:258-265 _apply_model_selection
	ApplyModelSelection(ctx context.Context, selection harnessprotocol.ModelSelection) error
}

// PendingTurn 一个已接受、等待或正在执行的输入。
// Python: base.py:88-99 PendingTurn
type PendingTurn struct {
	// Content 输入内容
	Content harnessprotocol.HarnessInput
	// MessageID 消息标识（回执报告给宿主的那个）
	MessageID string
	// TurnID 轮次标识
	TurnID string
	// AcceptedMode 实际接受的投递模式
	AcceptedMode harnessprotocol.DeliveryMode
	// flags 可变命令标记（abort/stop），原子整体替换。
	// Python 是三个裸字段（abort_requested / abort_mode / stop_requested），
	// 单线程无竞争；Go 侧由 Send/Abort/Stop 写、provider 的 ExecuteTurn 无锁读，
	// 必须原子化。合并成一个不可变结构体是为了消除「读到 abort 但 mode 还没写」
	// 的窗口期（spec Q16 / 偏差 D7）。
	flags atomic.Pointer[turnFlags]
}

// turnFlags PendingTurn 的可变命令标记（不可变，整体替换）。
type turnFlags struct {
	// abortRequested 是否已请求中止
	abortRequested bool
	// abortMode 中止模式（abortRequested 为 true 时有效）
	abortMode *harnessprotocol.AbortMode
	// stopRequested 是否因 harness stop 而中止
	stopRequested bool
}

// TurnTiming 轮次开始时捕获的墙钟与单调时钟锚点。
// Python: base.py:101-115 TurnTiming
type TurnTiming struct {
	// StartedAt 墙钟开始时刻（Unix 秒）
	StartedAt float64
	// StartedMonotonic 单调时钟开始时刻
	StartedMonotonic time.Time
}

// ProviderStartupError provider 运行时无法启动。
// Python: base.py:76-85 ProviderStartupError
//
// Error 携带 provider 中立的失败信息，宿主无需解析异常文本即可分类启动失败。
type ProviderStartupError struct {
	// Msg 错误消息
	Msg string
	// Error 与 provider 无关的失败
	Error harnessprotocol.TurnError
}

// supervisorHandle 一个异步 teardown/监督协程的完成信号。
// Go 不能 await goroutine，用 done channel 表达「已结束」
// （Python 的 asyncio.Task 可 await）。
type supervisorHandle struct {
	// done 协程退出时关闭
	done chan struct{}
}

// HarnessOptions SerializedTurnHarness 的构造参数。
type HarnessOptions struct {
	// Card provider 身份（必填）
	Card harnessprotocol.HarnessCard
	// Hooks provider 钩子（必填；通常是 provider 自身）
	Hooks providerHooks
	// EventBufferCapacity 观测缓冲容量；0 表示用上游默认 1024
	EventBufferCapacity int
}

// SerializedTurnHarness 在串行轮次循环之上实现 HarnessProtocol 的基座。
// Python: base.py:118-125 SerializedTurnHarness
//
// provider 嵌入本类型以获得 Emit / Transition / PublishCheckpoint 与
// HarnessProtocol 的全部 14 个公开方法；基座通过 hooks 字段回调 provider。
//
// 命令方法并发安全：状态变更经 commandLock 与单监督协程串行化。
type SerializedTurnHarness struct {
	// ── 静态配置 ──
	// card provider 身份与已声明能力（Python: 类属性 card）
	card harnessprotocol.HarnessCard
	// hooks provider 定制钩子（Go 新增，替代 ABC 继承）
	hooks providerHooks
	// bufferConfig 有界观测缓冲配置（Python: _buffer_config）
	bufferConfig harnessprotocol.EventBufferConfig

	// ── 生命周期状态 ──
	// state 当前相位。用 atomic 保护：State() 被宿主高频繁读，
	// Transition 在监督协程里写（spec Q11 / 偏差 D5）。
	state atomic.Pointer[harnessprotocol.HarnessState]
	// harnessContext 活跃周期的上下文（Python: _context）
	harnessContext *harnessprotocol.HarnessContext
	// sessionID provider 原生会话标识（Python: _session_id）
	sessionID *string
	// eventBuffer 观测缓冲（Python: _event_buffer）
	eventBuffer *BoundedEventBuffer
	// sequence 事件序号自增（Python: _sequence）。
	// atomic：Emit 可从监督协程与 teardown 协程并发调用（race 实测），
	// Python 侧由 asyncio 单线程保护。
	sequence atomic.Int64
	// pending 待决轮次队列（Python: deque[PendingTurn]）
	pending []*PendingTurn
	// activeTurn 正在执行的轮次（Python: _active_turn）
	activeTurn *PendingTurn
	// supervisor 监督协程句柄（Python: _supervisor_task）
	supervisor *supervisorHandle
	// stopHandle teardown 协程句柄（Python: _stop_task）
	stopHandle *supervisorHandle
	// cycleStarted 周期是否已开始（Python: _cycle_started）
	cycleStarted bool
	// stopping 是否正在停止（Python: _stopping）
	stopping bool

	// ── 锁 ──
	// commandLock 串行化状态变更（Python: _command_lock）
	commandLock sync.Mutex
	// lifecycleLock 串行化 start/stop（Python: _lifecycle_lock）
	lifecycleLock sync.Mutex
	// stateOnce 保证 state 字段初始化一次
	stateOnce sync.Once

	// ── 交互 ──
	// pendingInteractions 挂起的交互请求（Python: _pending_interactions）
	pendingInteractions map[string]harnessprotocol.HarnessInteractionHandler

	// ── checkpoint ──
	// latestCheckpoint 最新快照（Python: _latest_checkpoint）
	latestCheckpoint *harnessprotocol.HarnessCheckpoint
	// checkpointSequence 快照序号（Python: _checkpoint_sequence）
	checkpointSequence int
	// checkpointStorageRevision 存储修订号（Python: _checkpoint_storage_revision）
	checkpointStorageRevision *string

	// ── 模型 ──
	// pendingModelSelection 轮次运行中接受的模型切换，下一轮前应用
	// （Python: _pending_model_selection）
	pendingModelSelection *harnessprotocol.ModelSelection
	// fallbackActivated 认证回退是否已服务本周期（Python: _fallback_activated）
	fallbackActivated bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// 认证回退的协议常量。
// Python: harness_protocol 的 AUTH_FALLBACK_REQUEST_TYPE / AUTH_FALLBACK_EVENT_TYPE
const (
	// AuthFallbackRequestType 认证回退的宿主批准请求类型
	AuthFallbackRequestType = "auth_fallback"
	// AuthFallbackEventType 认证回退的声明事件类型
	AuthFallbackEventType = "auth_fallback"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSerializedTurnHarness 构造一个处于 TERMINATED 态的基座。
// Python: base.py:128-154 SerializedTurnHarness.__init__
func NewSerializedTurnHarness(opts HarnessOptions) *SerializedTurnHarness {
	capacity := opts.EventBufferCapacity
	if capacity <= 0 {
		capacity = 1024 // base.py:128 event_buffer_capacity 默认值
	}
	bufferConfig, err := harnessprotocol.NewEventBufferConfig(
		capacity, harnessprotocol.EventOverflowPolicyBlock)
	if err != nil {
		// 容量非法属编程错误；退化为上游默认值并保持可用
		bufferConfig = harnessprotocol.EventBufferConfig{
			Capacity: 1024,
			Overflow: harnessprotocol.EventOverflowPolicyBlock,
		}
	}
	harness := &SerializedTurnHarness{
		card:                opts.Card,
		hooks:               opts.Hooks,
		bufferConfig:        bufferConfig,
		pendingInteractions: make(map[string]harnessprotocol.HarnessInteractionHandler),
	}
	initial := harnessprotocol.HarnessStateTerminated // base.py:133
	harness.state.Store(&initial)
	return harness
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// currentFlags 返回本轮的当前标记快照（nil 视为全 false）。
func (t *PendingTurn) currentFlags() turnFlags {
	if flags := t.flags.Load(); flags != nil {
		return *flags
	}
	return turnFlags{}
}

// requestAbort 原子地置中断标记（Abort 用，锁外调用）。
func (t *PendingTurn) requestAbort(mode harnessprotocol.AbortMode) {
	current := t.currentFlags()
	current.abortRequested = true
	current.abortMode = &mode
	t.flags.Store(&current)
}

// requestStop 原子地置 stop 标记（同时置 abort，对应 base.py:320-321）。
func (t *PendingTurn) requestStop() {
	current := t.currentFlags()
	current.abortRequested = true
	current.stopRequested = true
	t.flags.Store(&current)
}

// abortRequested 读中断标记（provider 的 ExecuteTurn 用，无锁）。
func (t *PendingTurn) abortRequested() bool { return t.currentFlags().abortRequested }

// stopRequested 读 stop 标记（无锁）。
func (t *PendingTurn) stopRequested() bool { return t.currentFlags().stopRequested }

// NewTurnTiming 捕获当前时刻作为轮次起点。
// Python: base.py:105-106 的 field(default_factory=time.time/monotonic)
func NewTurnTiming() TurnTiming {
	return TurnTiming{
		StartedAt:        unixSeconds(),
		StartedMonotonic: time.Now(),
	}
}

// CompletedAt 返回墙钟完成时刻（Unix 秒）。
// Python: base.py:108-111 TurnTiming.completed_at
func (t TurnTiming) CompletedAt() float64 { return unixSeconds() }

// DurationMs 返回非负的单调时钟耗时（毫秒）。
// Python: base.py:113-115 TurnTiming.duration_ms
func (t TurnTiming) DurationMs() int {
	elapsed := time.Since(t.StartedMonotonic).Milliseconds()
	if elapsed < 0 {
		return 0
	}
	return int(elapsed)
}

// unixSeconds 当前 Unix 秒（浮点，与 Python time.time() 一致）。
func unixSeconds() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// ──────────────────────────── 导出函数：只读面 ────────────────────────────

// Card 返回静态实现身份与已声明能力。
// Python: base.py:126 类属性 card
func (h *SerializedTurnHarness) Card() harnessprotocol.HarnessCard { return h.card }

// State 返回当前高层生命周期相位。
// Python: base.py:165-167 state（property）
func (h *SerializedTurnHarness) State() harnessprotocol.HarnessState {
	if current := h.state.Load(); current != nil {
		return *current
	}
	return harnessprotocol.HarnessStateTerminated
}

// ProviderSessionID 返回 provider 原生会话标识（尚不可用时为 nil）。
// Python: base.py:169-171 provider_session_id
func (h *SerializedTurnHarness) ProviderSessionID() *string { return h.sessionID }

// EventBufferConfig 返回观测使用的有界背压策略。
// Python: base.py:173-175 event_buffer_config
func (h *SerializedTurnHarness) EventBufferConfig() harnessprotocol.EventBufferConfig {
	return h.bufferConfig
}

// Context 返回活跃周期的上下文；周期之间为 nil。
// Python: base.py:177-180 context
func (h *SerializedTurnHarness) Context() *harnessprotocol.HarnessContext {
	return h.harnessContext
}

// ActiveTurn 返回正在执行的轮次（若有）。
// Python: base.py:182-185 active_turn
//
// activeTurn 的写路径均在 commandLock 内（监督协程 / Stop），读取也须加锁，
// 否则与监督协程的并发写入构成 data race。
func (h *SerializedTurnHarness) ActiveTurn() *PendingTurn {
	h.commandLock.Lock()
	defer h.commandLock.Unlock()
	return h.activeTurn
}

// FallbackActivated 认证回退模型是否服务本会话。
// Python: base.py:160-163 fallback_activated
func (h *SerializedTurnHarness) FallbackActivated() bool { return h.fallbackActivated }

// ──────────────────────────── 结构体：Emit 选项 ────────────────────────────

// EmitOptions Emit 的可选关联字段。
// Python: base.py:673-694 _emit 的关键字参数
type EmitOptions struct {
	// Turn 事件所属的轮次（nil 表示不属于任何轮次）
	Turn *PendingTurn
	// ItemID 事件关于的 provider item
	ItemID string
	// ProviderSessionID 覆盖信封的 provider 会话标识（空串表示用 h.sessionID）
	ProviderSessionID string
	// CausationIDs 轮次输入消息之外的额外原因（如产生工具项的模型请求）
	CausationIDs []string
	// Timestamp 事件被观测的时刻（供 provider 为保因果序而延迟发送的事件）；
	// nil 表示用当前时间
	Timestamp *float64
}

// ──────────────────────────── 导出函数：事件发射 ────────────────────────────

// Emit 把一条事件放进观测流。
// Python: base.py:673-716 _emit
//
// 三步：信封组装（sequence 自增 / correlationID=turn.MessageID /
// causationIDs 去重）→ NewHarnessEvent 校验 → buffer.Put（满则阻塞背压）。
func (h *SerializedTurnHarness) Emit(ctx context.Context,
	payload harnessprotocol.HarnessEventPayload, opts EmitOptions) error {
	harnessContext := h.harnessContext
	buffer := h.eventBuffer
	if harnessContext == nil || buffer == nil {
		return &harnessprotocol.HarnessProtocolError{
			Msg: "cannot emit a " + h.card.Name + " event outside an active cycle",
		}
	}
	h.sequence.Add(1)

	// causes = (turn.message_id,) + 去重后的 causation_ids
	// Python: base.py:700-702
	var causes []string
	if opts.Turn != nil {
		causes = append(causes, opts.Turn.MessageID)
	}
	for _, cause := range opts.CausationIDs {
		if opts.Turn != nil && cause == opts.Turn.MessageID {
			continue
		}
		causes = append(causes, cause)
	}

	timestamp := unixSeconds()
	if opts.Timestamp != nil {
		timestamp = *opts.Timestamp
	}

	var turnID string
	var correlationID string
	if opts.Turn != nil {
		turnID = opts.Turn.TurnID
		correlationID = opts.Turn.MessageID
	}

	event, err := harnessprotocol.NewHarnessEvent(harnessprotocol.HarnessEventOptions{
		Sequence:          int(h.sequence.Load()),
		Timestamp:         timestamp,
		Event:             payload,
		HostSessionID:     harnessContext.HostSessionID,
		AgentID:           harnessContext.AgentID,
		ProviderSessionID: emitProviderSession(opts.ProviderSessionID, h.sessionID),
		TurnID:            turnID,
		ItemID:            opts.ItemID,
		CorrelationID:     correlationID,
		CausationIDs:      causes,
	})
	if err != nil {
		return err
	}
	return buffer.Put(ctx, event)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// emitProviderSession 解析信封的 provider 会话标识。
// Python: base.py:710 `provider_session_id or self._session_id`
//
// Python 的 or 是真值判断：空串也 fallback。Go 若只用 nil 判断，
// 传入指向空串的指针不会 fallback，与上游不一致。
func emitProviderSession(override string, sessionID *string) string {
	if override != "" {
		return override
	}
	if sessionID != nil {
		return *sessionID
	}
	return ""
}

// transition 相位变更并发射 StateChangedEvent；同相位不发射。
// Python: base.py:666-671 _transition
//
// ⚠️ 不持 commandLock：emit 可能阻塞在满缓冲上，持锁会与监督协程死锁
// （spec Q10 / 偏差 D4 的通用原则）。
func (h *SerializedTurnHarness) transition(
	ctx context.Context, newState harnessprotocol.HarnessState) error {
	oldState := h.State()
	if oldState == newState {
		return nil
	}
	stored := newState
	h.state.Store(&stored)
	payload, err := harnessprotocol.NewStateChangedEvent(oldState, newState)
	if err != nil {
		return err
	}
	return h.Emit(ctx, payload, EmitOptions{})
}

// validateContext 基础校验后交给 hooks 追加。
// Python: base.py:191-196 _validate_context
func (h *SerializedTurnHarness) validateContext(
	ctx context.Context, harnessContext harnessprotocol.HarnessContext) error {
	// 基础校验（Python 超类实现：card.validate_host）
	if err := h.card.ValidateHost(harnessContext.ProtocolVersion, harnessContext.HostCapabilities); err != nil {
		return err
	}
	// provider 追加（Python 的子类覆盖后调 super() 再追加）
	return h.hooks.ValidateContext(ctx, harnessContext)
}

// ──────────────────────────── 导出函数：生命周期 ────────────────────────────

// Start 校验宿主、开启 provider 会话并稳定在 IDLE。
// Python: base.py:271-298 start
//
// 13 项状态重置的顺序不可调（对齐 base.py:278-291）。
func (h *SerializedTurnHarness) Start(
	ctx context.Context, harnessContext harnessprotocol.HarnessContext) error {
	h.lifecycleLock.Lock()
	defer h.lifecycleLock.Unlock()

	// 步骤 1：周期已开始则报错（base.py:275-276）
	if h.cycleStarted {
		return &harnessprotocol.HarnessStateError{Msg: h.card.Name + " harness is already started"}
	}
	// 步骤 2：校验宿主（base.py:277）
	if err := h.validateContext(ctx, harnessContext); err != nil {
		return err
	}
	// 步骤 3：重置 13 项状态（base.py:278-291）
	h.harnessContext = &harnessContext
	h.eventBuffer = NewBoundedEventBuffer(h.bufferConfig.Capacity)
	h.sequence.Store(0)
	h.pending = nil
	h.activeTurn = nil
	h.supervisor = nil
	h.stopHandle = nil
	h.stopping = false
	h.pendingInteractions = make(map[string]harnessprotocol.HarnessInteractionHandler)
	h.latestCheckpoint = nil
	h.checkpointSequence = 0
	h.checkpointStorageRevision = nil
	h.pendingModelSelection = nil
	h.fallbackActivated = false

	// 步骤 4：开会话，失败则回滚（base.py:292-296）
	sessionID, err := h.hooks.OpenSession(ctx, harnessContext)
	if err != nil {
		h.rollbackStart(ctx)
		return err
	}
	h.sessionID = sessionID
	// 步骤 5：标记周期已开始并转 IDLE（base.py:297-298）
	h.cycleStarted = true
	return h.transition(ctx, harnessprotocol.HarnessStateIdle)
}

// Stop 停止 provider、终止已接受轮次并关闭事件流。
// Python: base.py:309-325 stop
//
// ⚠️ 上游的 `if supervisor is not asyncio.current_task()` 判断在 Go 侧省略
// （spec Q12 核查：_do_stop 只有 stop() 一个调用点，supervisor 内部只读
// _stopping 标志、从不调 stop，该判断是防御性冗余代码）。
func (h *SerializedTurnHarness) Stop(ctx context.Context) error {
	h.lifecycleLock.Lock()
	if !h.cycleStarted {
		h.lifecycleLock.Unlock()
		return nil
	}
	if h.stopHandle != nil {
		// 已有 teardown 在跑：多方等待同一完成信号（spec Q13，对应 shield）
		handle := h.stopHandle
		h.lifecycleLock.Unlock()
		<-handle.done
		return nil
	}
	// 首次 stop：置停止标记并中止活跃轮次（base.py:316-321）
	h.commandLock.Lock()
	h.stopping = true
	active := h.activeTurn
	h.commandLock.Unlock()
	if active != nil {
		active.requestStop() // 原子置 abort+stop（偏差 D7）
	}
	handle := &supervisorHandle{done: make(chan struct{})}
	h.stopHandle = handle
	h.lifecycleLock.Unlock()

	go func() {
		defer close(handle.done)
		h.doStop(ctx)
	}()
	<-handle.done
	return nil
}

// ──────────────────────────── 导出函数：命令 ────────────────────────────

// Send 接受一个输入：转向活跃轮次或排队一个串行轮次。
// Python: base.py:375-423 send
func (h *SerializedTurnHarness) Send(ctx context.Context,
	content harnessprotocol.HarnessInput, mode harnessprotocol.DeliveryMode) (harnessprotocol.SendReceipt, error) {
	// STEER 分支（base.py:383-399）
	if mode == harnessprotocol.DeliveryModeSteer {
		return h.sendSteer(ctx, content)
	}
	// 排队分支（base.py:401-423）
	return h.sendQueued(ctx, content, mode)
}

// Abort 中止活跃轮次；无轮次运行时是 no-op。
// Python: base.py:429-442 abort
func (h *SerializedTurnHarness) Abort(ctx context.Context, mode harnessprotocol.AbortMode) error {
	capability := harnessprotocol.HarnessCapabilityGracefulAbort
	if mode == harnessprotocol.AbortModeForce {
		capability = harnessprotocol.HarnessCapabilityForceAbort
	}
	if !h.card.Supports(capability) {
		return &harnessprotocol.UnsupportedHarnessCapabilityError{
			Msg: h.card.Name + " does not support " + string(capability),
		}
	}
	h.commandLock.Lock()
	active := h.activeTurn
	h.commandLock.Unlock()
	if active == nil {
		return nil
	}
	// 原子置标记（偏差 D7）：锁外写，避免持锁等待
	active.requestAbort(mode)

	if err := h.cancelPendingInteractions(ctx, harnessprotocol.InteractionCancelReasonTurnAborted); err != nil {
		return err
	}
	return h.hooks.InterruptTurn(ctx, active, mode)
}

// Pause 基座不支持 pause/resume。
// Python: base.py:444-445 pause
func (h *SerializedTurnHarness) Pause(ctx context.Context) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: h.card.Name + " does not support pause/resume",
	}
}

// Resume 基座不支持 pause/resume。
// Python: base.py:447-449 resume
func (h *SerializedTurnHarness) Resume(
	ctx context.Context, query *harnessprotocol.HarnessInput) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: h.card.Name + " does not support pause/resume",
	}
}

// ExportCheckpoint 返回最新快照（可能为 nil）。
// Python: base.py:451-452 export_checkpoint
func (h *SerializedTurnHarness) ExportCheckpoint(
	ctx context.Context) (*harnessprotocol.HarnessCheckpoint, error) {
	return h.latestCheckpoint, nil
}

// ListModels 探测 provider 提供的模型；已启动或未启动均可。
// Python: base.py:458-463 list_models
func (h *SerializedTurnHarness) ListModels(
	ctx context.Context) ([]harnessprotocol.ModelOption, error) {
	if !h.card.Supports(harnessprotocol.HarnessCapabilityModelDiscovery) {
		return nil, &harnessprotocol.UnsupportedHarnessCapabilityError{
			Msg: h.card.Name + " does not support model discovery",
		}
	}
	return h.hooks.ListModels(ctx)
}

// SetModel 空闲时立即切换模型/强度，否则延后到下一轮前。
// Python: base.py:465-481 set_model
func (h *SerializedTurnHarness) SetModel(
	ctx context.Context, selection harnessprotocol.ModelSelection) error {
	if !h.card.Supports(harnessprotocol.HarnessCapabilityModelSelection) {
		return &harnessprotocol.UnsupportedHarnessCapabilityError{
			Msg: h.card.Name + " does not support model selection",
		}
	}
	h.commandLock.Lock()
	if err := h.requireAccepting(); err != nil {
		h.commandLock.Unlock()
		return err
	}
	merged := MergeModelSelection(h.pendingModelSelection, selection)
	if h.activeTurn != nil || len(h.pending) > 0 {
		h.pendingModelSelection = &merged
		h.commandLock.Unlock()
		return nil
	}
	// 没有后续轮次的切换在此折入；否则下一轮会再次应用它覆盖本次
	// （base.py:476-478）
	h.pendingModelSelection = nil
	h.commandLock.Unlock()
	// 锁外应用：ApplyModelSelection 可能阻塞（偏差 D4 原则）
	return h.hooks.ApplyModelSelection(ctx, merged)
}

// Events 返回周期级有序观测流。
// Python: base.py:359-363 events
//
// ⚠️ 接口不能返回 error，而上游此处抛 HarnessStateError。
// 无活跃周期时返回一个立即耗尽的游标；「无周期」的信号由 Emit 的错误体现。
func (h *SerializedTurnHarness) Events() harnessprotocol.HarnessEventCursor {
	return h.cursorFor("", false)
}

// TurnEvents 返回单轮有限观测流。
// Python: base.py:365-369 turn_events
func (h *SerializedTurnHarness) TurnEvents(turnID *string) harnessprotocol.HarnessEventCursor {
	expected := ""
	if turnID != nil {
		expected = *turnID
	}
	return h.cursorFor(expected, true)
}

// ──────────────────────────── 非导出函数：Send 分支 ────────────────────────────

// sendSteer STEER 分支：直接把内容注入活跃轮次。
// Python: base.py:383-399
func (h *SerializedTurnHarness) sendSteer(
	ctx context.Context, content harnessprotocol.HarnessInput) (harnessprotocol.SendReceipt, error) {
	if !h.card.Supports(harnessprotocol.HarnessCapabilitySteer) {
		return harnessprotocol.SendReceipt{}, &harnessprotocol.UnsupportedHarnessCapabilityError{
			Msg: h.card.Name + " does not support steering",
		}
	}
	h.commandLock.Lock()
	if err := h.requireAccepting(); err != nil {
		h.commandLock.Unlock()
		return harnessprotocol.SendReceipt{}, err
	}
	active := h.activeTurn
	state := h.State()
	h.commandLock.Unlock()
	if active == nil || state != harnessprotocol.HarnessStateRunning {
		return harnessprotocol.SendReceipt{}, &harnessprotocol.HarnessStateError{
			Msg: "there is no active turn to steer",
		}
	}
	// message_id 在钩子运行前铸造：provider 标记出站消息需要的就是回执要报的那个
	// id（base.py:391-393）
	messageID := "message-" + newID()
	if err := h.hooks.Steer(ctx, active, content, messageID); err != nil {
		return harnessprotocol.SendReceipt{}, err
	}
	return harnessprotocol.SendReceipt{
		MessageID:    messageID,
		TurnID:       active.TurnID,
		AcceptedMode: harnessprotocol.DeliveryModeSteer,
	}, nil
}

// sendQueued 排队分支：入队并确保监督协程在跑。
// Python: base.py:401-423
func (h *SerializedTurnHarness) sendQueued(ctx context.Context,
	content harnessprotocol.HarnessInput, mode harnessprotocol.DeliveryMode) (harnessprotocol.SendReceipt, error) {
	h.commandLock.Lock()
	if err := h.requireAccepting(); err != nil {
		h.commandLock.Unlock()
		return harnessprotocol.SendReceipt{}, err
	}
	// AUTO 在有前序轮次时降级 FOLLOW_UP（base.py:403-404）
	hasEarlierTurn := h.activeTurn != nil || len(h.pending) > 0
	acceptedMode := mode
	if mode == harnessprotocol.DeliveryModeAuto && hasEarlierTurn {
		acceptedMode = harnessprotocol.DeliveryModeFollowUp
	}
	pendingTurn := &PendingTurn{
		Content:      content,
		MessageID:    "message-" + newID(),
		TurnID:       "turn-" + newID(),
		AcceptedMode: acceptedMode,
	}
	h.pending = append(h.pending, pendingTurn)
	var handle *supervisorHandle
	if h.supervisor == nil {
		handle = &supervisorHandle{done: make(chan struct{})}
		h.supervisor = handle
	}
	h.commandLock.Unlock()

	if handle != nil {
		// 监督协程必须脱离调用方 ctx 的取消（调用方 Send 返回后 ctx 可能被取消）
		go h.superviseTurns(context.WithoutCancel(ctx), handle)
	}
	return harnessprotocol.SendReceipt{
		MessageID:    pendingTurn.MessageID,
		TurnID:       pendingTurn.TurnID,
		AcceptedMode: pendingTurn.AcceptedMode,
	}, nil
}

// requireAccepting 校验可以接受命令。
// Python: base.py:425-427 _require_accepting
func (h *SerializedTurnHarness) requireAccepting() error {
	if !h.cycleStarted || h.stopping || h.State() == harnessprotocol.HarnessStateTerminated {
		return &harnessprotocol.HarnessStateError{
			Msg: "cannot send to a stopped " + h.card.Name + " harness",
		}
	}
	return nil
}

// cursorFor 取观测游标；无活跃缓冲时返回立即耗尽的游标。
func (h *SerializedTurnHarness) cursorFor(turnID string, perTurn bool) harnessprotocol.HarnessEventCursor {
	buffer := h.eventBuffer
	if buffer == nil {
		return &emptyCursor{}
	}
	cursor, err := buffer.Cursor(turnID, perTurn)
	if err != nil {
		return &emptyCursor{}
	}
	return cursor
}

// emptyCursor 立即耗尽的空游标（无活跃周期时 Events/TurnEvents 的返回值）。
type emptyCursor struct{}

// Iter 返回自身。
func (c *emptyCursor) Iter() harnessprotocol.HarnessEventCursor { return c }

// Next 立即返回耗尽哨兵。
func (c *emptyCursor) Next(
	ctx context.Context) (*harnessprotocol.HarnessEvent, error) {
	return nil, harnessprotocol.ErrEventStreamDone
}

// Close 无操作。
func (c *emptyCursor) Close(ctx context.Context) error { return nil }

// ──────────────────────────── 非导出函数：回滚与 teardown ────────────────────────────

// rollbackStart 启动失败后的清理。
// Python: base.py:300-307 _rollback_start
func (h *SerializedTurnHarness) rollbackStart(ctx context.Context) {
	if err := h.hooks.CloseSession(ctx); err != nil {
		logger.Debug(logComponent).Str("provider", h.card.Name).Err(err).
			Msg("session rollback after failed start raised")
	}
	h.eventBuffer = nil
	h.harnessContext = nil
	h.sessionID = nil
}

// doStop teardown 的 9 个步骤，顺序不可调。
// Python: base.py:327-353 _do_stop
//
// 关键顺序：先 close_session（解除轮次阻塞）再 close buffer（解除消费阻塞）。
// 反序会让轮次协程在已关闭的 buffer 上 Put 拿到 ErrEventBufferClosed 并转
// FAILED，而上游意图是让轮次自然收敛后再关流。
func (h *SerializedTurnHarness) doStop(ctx context.Context) {
	// 步骤 1：取消全部挂起交互（base.py:328）
	_ = h.cancelPendingInteractions(ctx, harnessprotocol.InteractionCancelReasonHarnessStopped)
	// 步骤 2：关 provider 会话，失败仅记录继续（base.py:329-332）
	if err := h.hooks.CloseSession(ctx); err != nil {
		logger.Error(logComponent).Str("provider", h.card.Name).Err(err).
			Msg("provider session close failed during stop")
	}
	// 步骤 3：等监督协程收敛（base.py:334-339）
	h.commandLock.Lock()
	supervisor := h.supervisor
	h.commandLock.Unlock()
	if supervisor != nil {
		<-supervisor.done
	}
	// 步骤 4：取走排队轮次并清空状态（base.py:341-345）
	h.commandLock.Lock()
	queued := h.pending
	h.pending = nil
	h.activeTurn = nil
	h.supervisor = nil
	h.commandLock.Unlock()
	// 步骤 5：中止排队轮次（base.py:346-347）
	if len(queued) > 0 {
		h.abortQueuedTurns(ctx, queued)
	}
	// 步骤 6：转 TERMINATED（base.py:348）
	if err := h.transition(ctx, harnessprotocol.HarnessStateTerminated); err != nil {
		logger.Warn(logComponent).Str("provider", h.card.Name).Err(err).
			Msg("failed to transition to terminated")
	}
	// 步骤 7：关事件缓冲，唤醒全部消费协程（base.py:349-351）
	if buffer := h.eventBuffer; buffer != nil {
		_ = buffer.Close(ctx)
	}
	// 步骤 8-9：清上下文与周期标记（base.py:352-353）
	h.harnessContext = nil
	h.cycleStarted = false
}

// ──────────────────────────── 非导出函数：监督循环 ────────────────────────────

// superviseTurns 串行执行待决轮次。
// Python: base.py:487-531 _supervise_turns
//
// ⚠️ 偏差 D4（spec Q10）：Python 在 base.py:526 持命令锁 await
// _transition(IDLE)。Go 的 sync.Mutex 是线程级的，持锁等 Emit → buffer.Put
// 会与消费者死锁（asyncio 的锁是协程级，等待只挂起协程）。
// 因此把 IDLE 转移移到锁外：锁内只算 needIdle，出锁后再发射。
func (h *SerializedTurnHarness) superviseTurns(ctx context.Context, handle *supervisorHandle) {
	defer close(handle.done)
	defer h.clearSupervisor(handle)

	for {
		h.commandLock.Lock()
		var active *PendingTurn
		var queued []*PendingTurn
		if h.stopping {
			queued = h.pending
			h.pending = nil
			active = nil
		} else if len(h.pending) == 0 {
			h.commandLock.Unlock()
			return
		} else {
			active = h.pending[0]
			h.pending = h.pending[1:]
			h.activeTurn = active
		}
		selection := h.pendingModelSelection
		h.pendingModelSelection = nil
		h.commandLock.Unlock() // ← 出锁，后续全部在锁外

		if active == nil {
			// 停止中：为排队轮次发 STARTED + ABORTED（base.py:503-505）
			h.abortQueuedTurns(ctx, queued)
			return
		}

		// base.py:507-508
		if err := h.transition(ctx, harnessprotocol.HarnessStateRunning); err != nil {
			logger.Warn(logComponent).Str("provider", h.card.Name).Err(err).
				Msg("failed to transition to running")
		}
		h.emitTurnLifecycle(ctx, active, harnessprotocol.TurnEventKindStarted, nil)
		// base.py:509-510
		if selection != nil {
			h.applyDeferredModelSelection(ctx, *selection, active)
		}

		// base.py:511-513
		terminalKind, result := h.runProviderTurn(ctx, active)
		if terminalKind == harnessprotocol.TurnEventKindFailed &&
			h.tryAuthFallback(ctx, result.Error, active) {
			terminalKind, result = h.runProviderTurn(ctx, active)
		}
		h.emitTurnLifecycle(ctx, active, terminalKind, &result)

		// base.py:516-522：锁内只判状态，不发射
		h.commandLock.Lock()
		h.activeTurn = nil
		needIdle := false
		stoppingNow := h.stopping // 锁内快照，锁外读会与 Stop() 写构成 race
		queued = nil
		if h.stopping {
			queued = h.pending
			h.pending = nil
		} else if len(h.pending) == 0 {
			needIdle = true
		}
		h.commandLock.Unlock()

		// base.py:523-526 的 IDLE 转移：锁外发射（偏差 D4）
		if needIdle {
			if err := h.transition(ctx, harnessprotocol.HarnessStateIdle); err != nil {
				logger.Warn(logComponent).Str("provider", h.card.Name).Err(err).
					Msg("failed to transition to idle")
			}
		}
		// base.py:527-531
		if len(queued) > 0 {
			h.abortQueuedTurns(ctx, queued)
			return
		}
		if stoppingNow {
			return
		}
	}
}

// clearSupervisor 清空监督协程句柄（仅当仍是自己时）。
// Python: base.py:650-652 _clear_supervisor_task
func (h *SerializedTurnHarness) clearSupervisor(handle *supervisorHandle) {
	h.commandLock.Lock()
	defer h.commandLock.Unlock()
	if h.supervisor == handle {
		h.supervisor = nil
	}
}

// runProviderTurn 执行一轮并把异常转成失败结果。
// Python: base.py:533-538 _run_provider_turn
func (h *SerializedTurnHarness) runProviderTurn(ctx context.Context, turn *PendingTurn) (
	harnessprotocol.TurnEventKind, harnessprotocol.TurnResult) {
	kind, result, err := h.hooks.ExecuteTurn(ctx, turn)
	if err != nil {
		// base.py:537
		logger.Error(logComponent).Str("provider", h.card.Name).Str("turn_id", turn.TurnID).Err(err).
			Msg("turn crashed inside the provider")
		return h.crashResult(turn, err)
	}
	return kind, result
}

// emitTurnLifecycle 发射一条轮次生命周期事件（构造失败时静默跳过）。
func (h *SerializedTurnHarness) emitTurnLifecycle(ctx context.Context,
	turn *PendingTurn, kind harnessprotocol.TurnEventKind, result *harnessprotocol.TurnResult) {
	payload, err := harnessprotocol.NewTurnLifecycleEvent(kind, result)
	if err != nil {
		return
	}
	_ = h.Emit(ctx, payload, EmitOptions{Turn: turn})
}

// abortQueuedTurns 为每个排队轮次发 STARTED + ABORTED。
// Python: base.py:654-660 _abort_queued_turns
func (h *SerializedTurnHarness) abortQueuedTurns(ctx context.Context, queued []*PendingTurn) {
	for _, turn := range queued {
		h.emitTurnLifecycle(ctx, turn, harnessprotocol.TurnEventKindStarted, nil)
		stopResult := BuildQueuedStopResult(h.card.Name)
		h.emitTurnLifecycle(ctx, turn, harnessprotocol.TurnEventKindAborted, &stopResult)
	}
}

// crashResult 把 provider 内的异常转成终态结果。
// Python: base.py:634-648 _crash_result
func (h *SerializedTurnHarness) crashResult(turn *PendingTurn, err error) (
	harnessprotocol.TurnEventKind, harnessprotocol.TurnResult) {
	if turn.abortRequested() {
		kind := harnessprotocol.TurnTerminationKindUserAbort
		if turn.stopRequested() {
			kind = harnessprotocol.TurnTerminationKindHarnessStop
		}
		message := h.card.Name + " turn was interrupted"
		termination, buildErr := harnessprotocol.NewTurnTermination(kind, &message, nil, nil)
		if buildErr != nil {
			return harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnResult{}
		}
		result, buildErr := harnessprotocol.NewTurnResult(
			harnessprotocol.TurnStatusInterrupted,
			harnessprotocol.ResultOptions{Termination: &termination})
		if buildErr != nil {
			return harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnResult{}
		}
		return harnessprotocol.TurnEventKindAborted, result
	}
	crashName := errorTypeName(err)
	category := "sdk_error"
	message := h.card.Name + " turn crashed: " + crashName
	turnError, buildErr := harnessprotocol.NewTurnError(message, &crashName, &category, nil, nil)
	if buildErr != nil {
		return harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnResult{}
	}
	result, buildErr := harnessprotocol.NewTurnResult(
		harnessprotocol.TurnStatusFailed,
		harnessprotocol.ResultOptions{Error: &turnError})
	if buildErr != nil {
		return harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnResult{}
	}
	return harnessprotocol.TurnEventKindFailed, result
}

// ──────────────────────────── 非导出函数：认证回退 ────────────────────────────

// tryAuthFallback 在监督循环内运行回退，绝不让异常逃逸。
// Python: base.py:540-550 _try_auth_fallback
//
// 每一轮都必须到达终态；因任何原因断掉的回退都保留原失败。
func (h *SerializedTurnHarness) tryAuthFallback(ctx context.Context,
	turnError *harnessprotocol.TurnError, turn *PendingTurn) bool {
	activated, err := h.activateAuthFallback(ctx, turnError, turn)
	if err != nil {
		// base.py:549
		logger.Error(logComponent).Str("provider", h.card.Name).Err(err).
			Msg("authentication fallback broke; the turn keeps its failure")
		return false
	}
	return activated
}

// activateAuthFallback 登录失败时把会话切到回退模型一次。
// Python: base.py:552-612 _activate_auth_fallback
//
// 门卫：只有 auth_required 类别才进入（请求没到达模型，重放不会重复工作）；
// 已激活过或轮次已请求中止则不进入；未配置回退则不进入。
// 宿主先批准切换；拒绝时恢复主模型，使成员绝不在未记录的模型上运行。
func (h *SerializedTurnHarness) activateAuthFallback(ctx context.Context,
	turnError *harnessprotocol.TurnError, turn *PendingTurn) (bool, error) {
	// 门卫 1（base.py:565-566）
	if turnError == nil || turnError.Category == nil || *turnError.Category != "auth_required" {
		return false, nil
	}
	// 门卫 2（base.py:567-568）
	if h.fallbackActivated || turn.abortRequested() {
		return false, nil
	}
	// 门卫 3（base.py:569-571）
	payload, ok := h.hooks.AuthFallbackPayload()
	if !ok {
		return false, nil
	}
	// 步骤 1：重连到回退（base.py:572-576）
	if err := h.hooks.ReconnectForAuth(ctx, true); err != nil {
		logger.Warn(logComponent).Str("provider", h.card.Name).Err(err).
			Msg("authentication fallback activation failed")
		return false, nil
	}
	// 步骤 2：宿主批准（base.py:577-583）
	ratified, err := h.confirmProviderExtension(ctx, AuthFallbackRequestType, payload)
	if err != nil {
		// 抛异常或越协议应答的处理器没有持久化这次切换：当作拒绝
		logger.Warn(logComponent).Str("provider", h.card.Name).Err(err).
			Msg("host failed to ratify the authentication fallback")
		ratified = false
	}
	// 步骤 3：宿主拒绝 → 恢复主模型（base.py:584-595）
	if !ratified {
		logger.Warn(logComponent).Str("provider", h.card.Name).
			Msg("host declined the authentication fallback; restoring the primary model")
		if err := h.hooks.ReconnectForAuth(ctx, false); err != nil {
			logger.Warn(logComponent).Str("provider", h.card.Name).Err(err).
				Msg("restoring the primary model failed")
			return false, nil
		}
		_ = h.hooks.OnAuthEndpointChanged(ctx, false)
		return false, nil
	}
	// 步骤 4（base.py:596）
	h.fallbackActivated = true
	// 步骤 5：记录端点变更（base.py:597-602）
	if err := h.hooks.OnAuthEndpointChanged(ctx, true); err != nil {
		// 宿主已持久化切换且会话已在回退上；记账失败不应让重放落空
		logger.Warn(logComponent).Str("provider", h.card.Name).Err(err).
			Msg("recording the authentication fallback failed")
	}
	// 步骤 6：发 ProviderEvent（base.py:603-611）
	if event, buildErr := harnessprotocol.NewProviderEvent(
		h.card.Name, AuthFallbackEventType, "1", payload); buildErr == nil {
		_ = h.Emit(ctx, event, EmitOptions{Turn: turn})
	}
	return true, nil
}

// applyDeferredModelSelection 应用轮次运行中接受的切换；失败只报告不致命。
// Python: base.py:614-632 _apply_deferred_model_selection
//
// 调用方的 set_model 早已返回，失败无法抛回给它；该轮仍在旧模型上跑，
// 宿主看到 WARNING 诊断而非静默无操作。
func (h *SerializedTurnHarness) applyDeferredModelSelection(ctx context.Context,
	selection harnessprotocol.ModelSelection, turn *PendingTurn) {
	if err := h.hooks.ApplyModelSelection(ctx, selection); err != nil {
		logger.Warn(logComponent).Str("provider", h.card.Name).Err(err).
			Msg("deferred model selection failed")
		data := harnessprotocol.JsonObject{}
		if selection.Model != nil {
			data["model"] = *selection.Model
		}
		if selection.Effort != nil {
			data["effort"] = *selection.Effort
		}
		diagnostic, buildErr := harnessprotocol.NewDiagnosticEvent(
			harnessprotocol.DiagnosticLevelWarning,
			h.card.Name+" could not switch model: "+errorTypeName(err),
			data)
		if buildErr == nil {
			_ = h.Emit(ctx, diagnostic, EmitOptions{Turn: turn})
		}
	}
}

// ──────────────────────────── 非导出函数：交互 ────────────────────────────

// hasHostInteractions 宿主是否提供了交互处理器。
// Python: base.py:722-724 _has_host_interactions
func (h *SerializedTurnHarness) hasHostInteractions() bool {
	return h.harnessContext != nil && h.harnessContext.Interactions != nil
}

// requestInteraction 等待宿主对 request 的应答；无处理器时返回 nil。
// Python: base.py:726-738 _request_interaction
func (h *SerializedTurnHarness) requestInteraction(ctx context.Context,
	request harnessprotocol.HarnessInteractionRequest) (harnessprotocol.HarnessInteractionResponse, error) {
	harnessContext := h.harnessContext
	if harnessContext == nil || harnessContext.Interactions == nil {
		return nil, nil
	}
	handler := harnessContext.Interactions
	requestID := interactionRequestID(request)
	h.commandLock.Lock()
	h.pendingInteractions[requestID] = handler
	h.commandLock.Unlock()
	defer func() {
		h.commandLock.Lock()
		delete(h.pendingInteractions, requestID)
		h.commandLock.Unlock()
	}()
	response, err := handler.Handle(ctx, request)
	if err != nil {
		return nil, err
	}
	return harnessprotocol.ValidateInteractionResponse(request, response)
}

// confirmProviderExtension 请宿主批准一个 provider 特定决定。
// Python: base.py:740-765 _confirm_provider_extension
//
// 宿主声明 PROVIDER_INTERACTION 且提供了处理器时发 ProviderInteractionRequest；
// true 表示宿主完成了请求。没有这样的宿主时决定权在 provider 自己，返回 true。
func (h *SerializedTurnHarness) confirmProviderExtension(ctx context.Context,
	requestType string, payload harnessprotocol.JsonObject) (bool, error) {
	harnessContext := h.harnessContext
	if harnessContext == nil || harnessContext.Interactions == nil {
		return true, nil
	}
	if !harnessContext.HostCapabilities[harnessprotocol.HostCapabilityProviderInteraction] {
		return true, nil
	}
	h.commandLock.Lock()
	active := h.activeTurn
	h.commandLock.Unlock()

	var turnID *string
	if active != nil {
		turnIDValue := active.TurnID
		turnID = &turnIDValue
	}
	request, err := harnessprotocol.NewProviderInteractionRequest(
		h.card.Name+":"+requestType+":"+newID(),
		h.card.Name, requestType, "1",
		harnessprotocol.ProviderInteractionRequestOptions{
			Payload:           payload,
			ProviderSessionID: h.sessionID,
			TurnID:            turnID,
		})
	if err != nil {
		return false, err
	}
	response, err := h.requestInteraction(ctx, &request)
	if err != nil {
		return false, err
	}
	if response == nil {
		return false, nil
	}
	return interactionResponseCompleted(response), nil
}

// cancelPendingInteractions 批量撤销挂起交互；返回首个错误。
// Python: base.py:767-774 _cancel_pending_interactions
func (h *SerializedTurnHarness) cancelPendingInteractions(ctx context.Context,
	reason harnessprotocol.InteractionCancelReason) error {
	h.commandLock.Lock()
	pending := make(map[string]harnessprotocol.HarnessInteractionHandler, len(h.pendingInteractions))
	for requestID, handler := range h.pendingInteractions {
		pending[requestID] = handler
	}
	h.pendingInteractions = make(map[string]harnessprotocol.HarnessInteractionHandler)
	h.commandLock.Unlock()

	var firstErr error
	for requestID, handler := range pending {
		option := harnessprotocol.CancelOption(func(target *harnessprotocol.InteractionCancelReason) {
			*target = reason
		})
		if err := handler.Cancel(ctx, requestID, option); err != nil {
			// base.py:774
			logger.Error(logComponent).Str("provider", h.card.Name).
				Str("request_id", requestID).Err(err).
				Msg("failed to cancel interaction")
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// ──────────────────────────── 非导出函数：checkpoint ────────────────────────────

// PublishCheckpoint 记录最新快照并尽力推送给宿主 sink。
// Python: base.py:780-820 _publish_checkpoint
//
// 关键：先保存 latestCheckpoint 再推送——即使推送失败，
// ExportCheckpoint 仍能返回最新快照（base.py:803）。
func (h *SerializedTurnHarness) PublishCheckpoint(ctx context.Context,
	data harnessprotocol.JsonObject, reason harnessprotocol.CheckpointReason,
	schemaVersion string) (*harnessprotocol.HarnessCheckpoint, error) {
	harnessContext := h.harnessContext
	if harnessContext == nil {
		return nil, nil
	}
	h.checkpointSequence++
	sequence := h.checkpointSequence
	checkpoint, err := harnessprotocol.NewHarnessCheckpoint(harnessprotocol.CheckpointOptions{
		Provider:          h.card.Name,
		SchemaVersion:     schemaVersion,
		AgentID:           harnessContext.AgentID,
		HostSessionID:     harnessContext.HostSessionID,
		CheckpointID:      newID(),
		Sequence:          sequence,
		Data:              data,
		ProviderSessionID: h.sessionID,
	})
	if err != nil {
		return nil, err
	}
	h.latestCheckpoint = &checkpoint

	sink := harnessContext.CheckpointSink
	if sink == nil {
		return &checkpoint, nil
	}
	receipt, saveErr := sink.Save(ctx, checkpoint, reason, h.checkpointStorageRevision)
	if saveErr != nil {
		if isCheckpointConflict(saveErr) {
			// base.py:813-815
			logger.Warn(logComponent).Str("provider", h.card.Name).
				Str("checkpoint_id", checkpoint.CheckpointID).
				Msg("checkpoint rejected as stale")
			return &checkpoint, nil
		}
		// base.py:816-818
		logger.Error(logComponent).Str("provider", h.card.Name).Err(saveErr).
			Msg("checkpoint sink failed")
		return &checkpoint, nil
	}
	revision := receipt.StorageRevision
	h.checkpointStorageRevision = &revision
	return &checkpoint, nil
}

// RestoredCheckpointData 返回属于本 provider 的 checkpoint 数据并校验作用域。
// Python: base.py:822-836 _restored_checkpoint_data
func (h *SerializedTurnHarness) RestoredCheckpointData(
	harnessContext harnessprotocol.HarnessContext) (harnessprotocol.JsonObject, error) {
	checkpoint := harnessContext.Checkpoint
	if checkpoint == nil {
		return nil, nil
	}
	if checkpoint.Provider != h.card.Name {
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "checkpoint provider " + checkpoint.Provider + " does not belong to " + h.card.Name,
		}
	}
	if checkpoint.AgentID != harnessContext.AgentID ||
		checkpoint.HostSessionID != harnessContext.HostSessionID {
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "checkpoint scope does not match the harness context",
		}
	}
	if checkpoint.Sequence > h.checkpointSequence {
		h.checkpointSequence = checkpoint.Sequence
	}
	if checkpoint.Revision != nil {
		revision := *checkpoint.Revision
		h.checkpointStorageRevision = &revision
	}
	return checkpoint.Data, nil
}

// ──────────────────────────── 导出函数：包级辅助 ────────────────────────────

// BuildQueuedStopResult 返回已接受但在执行前被停止的轮次的终态结果。
// Python: base.py:839-852 build_queued_stop_result
func BuildQueuedStopResult(providerName string) harnessprotocol.TurnResult {
	now := unixSeconds()
	zeroDuration := 0
	message := providerName + " stopped before the queued turn started"
	termination, err := harnessprotocol.NewTurnTermination(
		harnessprotocol.TurnTerminationKindHarnessStop, &message, nil, nil)
	if err != nil {
		return harnessprotocol.TurnResult{Status: harnessprotocol.TurnStatusInterrupted}
	}
	result, err := harnessprotocol.NewTurnResult(
		harnessprotocol.TurnStatusInterrupted,
		harnessprotocol.ResultOptions{
			Termination: &termination,
			StartedAt:   &now,
			CompletedAt: &now,
			DurationMS:  &zeroDuration,
		})
	if err != nil {
		return harnessprotocol.TurnResult{Status: harnessprotocol.TurnStatusInterrupted}
	}
	return result
}

// InterruptedResultOptions InterruptedResult 的可选参数。
// Python: base.py:855-863 的 keyword 参数
type InterruptedResultOptions struct {
	// ProviderName provider 名（必填）
	ProviderName string
	// Timing 轮次的时钟锚点（必填）
	Timing TurnTiming
	// Messages 轮次内的消息
	Messages []harnessprotocol.TurnMessage
	// FinalOutput 最终输出
	FinalOutput harnessprotocol.JsonValue
	// Usage 用量
	Usage *harnessprotocol.TurnUsage
}

// InterruptedResult 构造与轮次被停止方式匹配的 INTERRUPTED 结果。
// Python: base.py:855-881 interrupted_result
func InterruptedResult(turn *PendingTurn, opts InterruptedResultOptions) harnessprotocol.TurnResult {
	kind := harnessprotocol.TurnTerminationKindUserAbort
	message := opts.ProviderName + " turn was aborted"
	if turn.stopRequested() {
		kind = harnessprotocol.TurnTerminationKindHarnessStop
		message = opts.ProviderName + " stopped before the turn completed"
	}
	termination, err := harnessprotocol.NewTurnTermination(kind, &message, nil, nil)
	if err != nil {
		return harnessprotocol.TurnResult{Status: harnessprotocol.TurnStatusInterrupted}
	}
	startedAt := opts.Timing.StartedAt
	completedAt := opts.Timing.CompletedAt()
	durationMS := opts.Timing.DurationMs()
	result, err := harnessprotocol.NewTurnResult(
		harnessprotocol.TurnStatusInterrupted,
		harnessprotocol.ResultOptions{
			Messages:    opts.Messages,
			FinalOutput: opts.FinalOutput,
			Termination: &termination,
			Usage:       opts.Usage,
			StartedAt:   &startedAt,
			CompletedAt: &completedAt,
			DurationMS:  &durationMS,
		})
	if err != nil {
		return harnessprotocol.TurnResult{Status: harnessprotocol.TurnStatusInterrupted}
	}
	return result
}

// MergeModelSelection 把 update 叠加到 current 上；update 未设置的字段保留。
// Python: base.py:884-891 merge_model_selection
func MergeModelSelection(current *harnessprotocol.ModelSelection,
	update harnessprotocol.ModelSelection) harnessprotocol.ModelSelection {
	if current == nil {
		return update
	}
	merged := harnessprotocol.ModelSelection{
		Model:  current.Model,
		Effort: current.Effort,
	}
	if update.Model != nil {
		merged.Model = update.Model
	}
	if update.Effort != nil {
		merged.Effort = update.Effort
	}
	return merged
}

// ──────────────────────────── 非导出函数：协议助手 ────────────────────────────

// newID 生成 32 位十六进制标识。
// Python: base.py:393/455/798 的 uuid.uuid4().hex
func newID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

// errorTypeName 取错误的类型名。
// Python: base.py:628/644 的 type(exc).__name__
func errorTypeName(err error) string {
	if err == nil {
		return ""
	}
	typed := reflect.TypeOf(err)
	if typed == nil {
		return ""
	}
	if typed.Kind() == reflect.Ptr {
		typed = typed.Elem()
	}
	return typed.Name()
}

// interactionRequestID 取交互请求的 request_id。
// Python: base.py:733/770 的 request.request_id
func interactionRequestID(request harnessprotocol.HarnessInteractionRequest) string {
	switch typed := request.(type) {
	case *harnessprotocol.ToolApprovalRequest:
		if typed != nil {
			return typed.RequestID
		}
	case *harnessprotocol.UserInputRequest:
		if typed != nil {
			return typed.RequestID
		}
	case *harnessprotocol.McpElicitationRequest:
		if typed != nil {
			return typed.RequestID
		}
	case *harnessprotocol.DynamicToolCallRequest:
		if typed != nil {
			return typed.RequestID
		}
	case *harnessprotocol.ProviderInteractionRequest:
		if typed != nil {
			return typed.RequestID
		}
	}
	return ""
}

// interactionResponseCompleted 应答状态是否为 COMPLETED。
// Python: base.py:765 `response.status is InteractionResponseStatus.COMPLETED`
func interactionResponseCompleted(response harnessprotocol.HarnessInteractionResponse) bool {
	switch typed := response.(type) {
	case *harnessprotocol.UserInputResponse:
		if typed != nil {
			return typed.Status == harnessprotocol.InteractionResponseStatusCompleted
		}
	case *harnessprotocol.McpElicitationResponse:
		if typed != nil {
			return typed.Status == harnessprotocol.InteractionResponseStatusCompleted
		}
	case *harnessprotocol.DynamicToolCallResponse:
		if typed != nil {
			return typed.Status == harnessprotocol.InteractionResponseStatusCompleted
		}
	case *harnessprotocol.ProviderInteractionResponse:
		if typed != nil {
			return typed.Status == harnessprotocol.InteractionResponseStatusCompleted
		}
	}
	return false
}

// isCheckpointConflict 错误是否为 checkpoint CAS 冲突。
// Python: base.py:813 的 except CheckpointConflictError
func isCheckpointConflict(err error) bool {
	var conflict *harnessprotocol.CheckpointConflictError
	return errors.As(err, &conflict)
}
