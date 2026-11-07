package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ──────────────────────────── 接口 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

// CoordinatorOptions 协调器构造参数。
//
// Python: RuntimeSessionCoordinator.__init__
type CoordinatorOptions struct {
	// Registry 执行注册表（nil 时自建）
	Registry *SessionExecutionRegistry
	// Scheduler 工作调度器（nil 时自建）
	Scheduler *SessionWorkScheduler
	// CancelTimeout 取消等待超时
	CancelTimeout time.Duration
	// StreamBufferSize 流式队列容量
	StreamBufferSize int
}

// UnaryOptions RunUnary / SubmitUnary 的可选参数。
//
// Python: RuntimeSessionCoordinator.run_unary 关键字参数
type UnaryOptions struct {
	// InteractionIDs 从结果提取交互标识
	InteractionIDs func(any) map[string]struct{}
	// WaitForTerminal 是否阻塞等业务终态（等 Terminal 通道）
	WaitForTerminal bool
	// TimeoutScope 超时 ctx（nil 不设超时）
	TimeoutScope context.Context
	// TimeoutError 超时错误文案
	TimeoutError string
	// Team 团队详情
	Team *TeamExecutionDetails
}

// ChildOptions 创建子执行的参数。
type ChildOptions struct {
	// ParentRequestID 父请求标识
	ParentRequestID string
	// ChannelID 渠道标识
	ChannelID string
	// Team 团队详情
	Team *TeamExecutionDetails
}

// StreamOptions RunStream / RunTeamStream 的可选参数。
//
// Python: run_stream / run_team_stream 关键字参数
type StreamOptions struct {
	// InteractionIDs 从元素提取交互标识
	InteractionIDs func(any) map[string]struct{}
	// ParentExecutionID 父执行标识
	ParentExecutionID string
	// ChannelID 渠道标识
	ChannelID string
	// Team Team 流专用详情
	Team *TeamExecutionDetails
	// Details Team 流的团队详情（与 Team 同义，供 RunTeamStream 使用）
	Details *TeamExecutionDetails
}

// streamItem 流式队列元素。
//
// Python: coordinator.py _StreamItem
type streamItem struct {
	// value 数据元素
	value any
	// err 错误
	err error
	// done 是否为终态标记
	done bool
}

// delivery 一次控制投递的在飞状态。
//
// Python: coordinator.py _Delivery
type delivery struct {
	// answered 本次投递声明的交互标识
	answered map[string]struct{}
	// settled 投递结束信号
	settled chan struct{}
	// once 保证只关闭一次
	once sync.Once
}

// sessionRecord 会话账本记录。
//
// Python: coordinator.py _SessionRecord
type sessionRecord struct {
	// sessionID 会话标识
	sessionID string
	// channelID 渠道标识
	channelID string
	// persistencePolicy 持久化策略
	persistencePolicy SessionPersistencePolicy
	// generation 代次
	generation int
	// state 会话状态
	state RuntimeSessionState
	// controlReady 交互就绪信号（有等待交互时清空）
	controlReady *flagSignal
	// deliveries 执行标识 → 在飞投递
	deliveries map[string]*delivery
}

// flagSignal 可置位/清空的一次性信号，等价 asyncio.Event。
type flagSignal struct {
	// mu 保护 set 与 ch
	mu sync.Mutex
	// ch 当前信号通道（置位时关闭；清空时重建）
	ch chan struct{}
	// set 是否已置位
	set bool
}

// RuntimeSessionCoordinator 单个 Runtime 的会话执行账本。
//
// Python: coordinator.py RuntimeSessionCoordinator
//
// 与上游差异（spec §8 D-1）：operation 闭包接收 ctx，因为 Go 的取消
// 只能靠 ctx 传递（Python 用 task.cancel() 硬取消协程）。
type RuntimeSessionCoordinator struct {
	// mu 保护以下全部字段
	mu sync.Mutex
	// registry 执行注册表
	registry *SessionExecutionRegistry
	// scheduler 工作调度器
	scheduler *SessionWorkScheduler
	// cancelTimeout 取消等待超时
	cancelTimeout time.Duration
	// streamBufferSize 流式队列容量
	streamBufferSize int
	// sessions 会话标识 → 账本记录
	sessions map[string]*sessionRecord
	// generations 会话标识 → 单调代次计数
	generations map[string]int
	// accepting 是否接受新工作
	accepting bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// teamWorkKinds 团队工作种类集合。
//
// Python: coordinator.py _TEAM_WORK_KINDS
var teamWorkKinds = map[SessionWorkKind]struct{}{
	SessionWorkKindTeamRequest:    {},
	SessionWorkKindTeamControl:    {},
	SessionWorkKindTeamBackground: {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// close 发出 settled 信号（幂等）。
//
// Python: _Delivery.settled.set()
func (d *delivery) close() { d.once.Do(func() { close(d.settled) }) }

// Set 置位（幂等）。
//
// Python: asyncio.Event.set()
func (f *flagSignal) Set() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.set {
		f.set = true
		close(f.ch)
	}
}

// Clear 清空（下次 Set 会重建通道）。
//
// Python: asyncio.Event.clear()
func (f *flagSignal) Clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.set {
		f.set = false
		f.ch = make(chan struct{})
	}
}

// Wait 返回等待通道（已置位时立即可读）。
//
// Python: asyncio.Event.wait()
func (f *flagSignal) Wait() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ch
}

// DefaultCoordinatorOptions 默认构造参数。
//
// Python: RuntimeSessionCoordinator.__init__ 默认值（cancel_timeout=5.0、buffer=64）
// spec §3 D21：cancel_timeout 固定 5 秒，不提供外部配置。
func DefaultCoordinatorOptions() CoordinatorOptions {
	return CoordinatorOptions{
		CancelTimeout:    5 * time.Second,
		StreamBufferSize: 64,
	}
}

// NewRuntimeSessionCoordinator 创建协调器。
//
// Python: RuntimeSessionCoordinator.__init__（stream_buffer_size 取 max(1,n)）
func NewRuntimeSessionCoordinator(opts CoordinatorOptions) *RuntimeSessionCoordinator {
	registry := opts.Registry
	if registry == nil {
		registry = NewSessionExecutionRegistry(512, 900.0)
	}
	scheduler := opts.Scheduler
	if scheduler == nil {
		scheduler = NewSessionWorkScheduler()
	}
	bufSize := opts.StreamBufferSize
	if bufSize < 1 {
		bufSize = 1
	}
	return &RuntimeSessionCoordinator{
		registry:         registry,
		scheduler:        scheduler,
		cancelTimeout:    opts.CancelTimeout,
		streamBufferSize: bufSize,
		sessions:         map[string]*sessionRecord{},
		generations:      map[string]int{},
		accepting:        true,
	}
}

// RegisterSession 注册或复用会话记录，必要时递增代次。
//
// Python: RuntimeSessionCoordinator.register_session
// 已存在且非 CLOSED 时只更新 channel 与 policy，不递增代次。
func (c *RuntimeSessionCoordinator) RegisterSession(
	sessionID, channelID string,
	policy SessionPersistencePolicy,
) (RuntimeSessionSnapshot, error) {
	normalized := trimSpace(sessionID)
	if normalized == "" {
		return RuntimeSessionSnapshot{}, errors.New("session_id is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.accepting {
		return RuntimeSessionSnapshot{}, errors.New("session coordinator is closed")
	}
	current, exists := c.sessions[normalized]
	if !exists || current.state == RuntimeSessionStateClosed {
		generation := c.generations[normalized] + 1
		c.generations[normalized] = generation
		current = &sessionRecord{
			sessionID:         normalized,
			channelID:         defaultChannel(channelID),
			persistencePolicy: policy,
			generation:        generation,
			state:             RuntimeSessionStateReady,
			controlReady:      newFlagSignal(),
			deliveries:        map[string]*delivery{},
		}
		c.sessions[normalized] = current
	} else {
		if channelID != "" {
			current.channelID = channelID
		}
		current.persistencePolicy = policy
	}
	return c.snapshotLocked(current), nil
}

// RunUnary 运行一次同步执行并返回结果。
//
// Python: RuntimeSessionCoordinator.run_unary
func (c *RuntimeSessionCoordinator) RunUnary(
	ctx context.Context,
	sessionID, requestID string,
	workKind SessionWorkKind,
	operation func(context.Context) (any, error),
	opts UnaryOptions,
) (any, error) {
	record, err := c.requireOpenSession(sessionID)
	if err != nil {
		return nil, err
	}
	handle, err := c.newExecution(record, requestID, workKind, "", opts.Team)
	if err != nil {
		return nil, err
	}
	handle.RetainOwnerTask = opts.WaitForTerminal
	return c.runUnary(ctx, record, handle, operation, opts)
}

// SubmitUnary 排队提交并立即返回快照。
//
// Python: RuntimeSessionCoordinator.submit_unary
func (c *RuntimeSessionCoordinator) SubmitUnary(
	ctx context.Context,
	sessionID, requestID string,
	workKind SessionWorkKind,
	operation func(context.Context) (any, error),
	opts UnaryOptions,
) (SessionExecutionSnapshot, error) {
	record, err := c.requireOpenSession(sessionID)
	if err != nil {
		return SessionExecutionSnapshot{}, err
	}
	generation := record.generation
	existing := c.registry.Select(SelectOptions{
		SessionID: sessionID, RequestID: &requestID, Generation: &generation,
	})
	if len(existing) > 0 {
		last := existing[len(existing)-1]
		if last.WorkKind != workKind {
			return SessionExecutionSnapshot{}, fmt.Errorf(
				"request_id already belongs to %s", last.WorkKind)
		}
		return last.Snapshot(), nil
	}
	handle, err := c.newExecution(record, requestID, workKind, "", nil)
	if err != nil {
		return SessionExecutionSnapshot{}, err
	}
	snapshot := handle.Snapshot()
	go func() {
		if workKind == SessionWorkKindSessionMessage {
			select {
			case <-record.controlReady.Wait():
			case <-ctx.Done():
				return
			}
		}
		_, _ = c.runUnary(ctx, record, handle, operation, opts)
	}()
	return snapshot, nil
}

// SnapshotSession 取单个会话快照。
//
// Python: RuntimeSessionCoordinator.snapshot_session
func (c *RuntimeSessionCoordinator) SnapshotSession(sessionID string) *RuntimeSessionSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.sessions[sessionID]
	if !ok {
		return nil
	}
	snap := c.snapshotLocked(record)
	return &snap
}

// SnapshotSessions 取全部会话快照。
//
// Python: RuntimeSessionCoordinator.snapshot_sessions
func (c *RuntimeSessionCoordinator) SnapshotSessions() []RuntimeSessionSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]RuntimeSessionSnapshot, 0, len(c.sessions))
	for _, record := range c.sessions {
		out = append(out, c.snapshotLocked(record))
	}
	return out
}

// GetExecution 取单次执行快照。
//
// Python: RuntimeSessionCoordinator.get_execution
func (c *RuntimeSessionCoordinator) GetExecution(executionID string) *SessionExecutionSnapshot {
	handle := c.registry.Get(executionID)
	if handle == nil {
		return nil
	}
	snap := handle.Snapshot()
	return &snap
}

// QuiesceSession 把会话置 QUIESCING 并返回生命周期上下文。
//
// Python: RuntimeSessionCoordinator.quiesce_session
// 只置状态、不取消任何执行。
func (c *RuntimeSessionCoordinator) QuiesceSession(sessionID string, generation *int) (*SessionLifecycleContext, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.sessions[sessionID]
	if !ok || (generation != nil && record.generation != *generation) {
		return nil, nil
	}
	if record.state == RuntimeSessionStateClosed {
		return nil, nil
	}
	record.state = RuntimeSessionStateQuiescing
	return &SessionLifecycleContext{SessionID: record.sessionID, Generation: record.generation}, nil
}

// NewExecutionForTest 创建子执行（导出以便跨包测试）。
//
// Python: RuntimeSessionCoordinator._new_execution
func (c *RuntimeSessionCoordinator) NewExecutionForTest(
	sessionID, requestID string,
	workKind SessionWorkKind,
	opts ChildOptions,
) (*SessionExecutionHandle, error) {
	record, err := c.requireOpenSession(sessionID)
	if err != nil {
		return nil, err
	}
	parentExecutionID := ""
	if opts.ParentRequestID != "" {
		generation := record.generation
		parents := c.registry.Select(SelectOptions{
			SessionID: sessionID, RequestID: &opts.ParentRequestID,
			Generation: &generation, ActiveOnly: true,
		})
		if len(parents) > 0 {
			parentExecutionID = parents[len(parents)-1].ExecutionID
		}
	}
	return c.newExecution(record, requestID, workKind, parentExecutionID, opts.Team)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newFlagSignal 创建已置位信号。
func newFlagSignal() *flagSignal {
	return &flagSignal{ch: make(chan struct{}), set: true}
}

// defaultChannel 渠道标识归一。
func defaultChannel(channelID string) string {
	if channelID == "" {
		return "default"
	}
	return channelID
}

// trimSpace 去首尾空白（避免引入 strings 仅为此）。
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// requireOpenSession 校验会话已注册且可接受工作。
//
// Python: RuntimeSessionCoordinator._require_open_session
func (c *RuntimeSessionCoordinator) requireOpenSession(sessionID string) (*sessionRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.accepting {
		return nil, errors.New("session coordinator is closed")
	}
	record, ok := c.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session is not registered: %s", sessionID)
	}
	if record.state == RuntimeSessionStateQuiescing || record.state == RuntimeSessionStateClosed {
		return nil, fmt.Errorf("session is not accepting work: %s", sessionID)
	}
	return record, nil
}

// newExecution 创建并登记一次执行。
//
// Python: RuntimeSessionCoordinator._new_execution
// 含 supersede 逻辑：新聊天/目标流执行会取消同代次处于 WAITING_FOR_CONTROL 的同类执行。
func (c *RuntimeSessionCoordinator) newExecution(
	record *sessionRecord,
	requestID string,
	workKind SessionWorkKind,
	parentExecutionID string,
	team *TeamExecutionDetails,
) (*SessionExecutionHandle, error) {
	_, isTeamWork := teamWorkKinds[workKind]
	if isTeamWork != (team != nil) {
		return nil, errors.New("Team execution details must match the work kind")
	}

	c.supersedeSiblings(record, workKind)

	handle := NewSessionExecutionHandle(
		newExecutionID(), record.sessionID, requestID, record.generation, workKind, record.channelID)
	handle.Team = team
	handle.ParentExecutionID = parentExecutionID
	if parentExecutionID != "" {
		if parent := c.registry.Get(parentExecutionID); parent != nil {
			handle.RootWorkKind = parent.RootWorkKind
			if handle.RootWorkKind == "" {
				handle.RootWorkKind = parent.WorkKind
			}
			handle.RootRequestID = parent.RootRequestID
			if handle.RootRequestID == "" {
				handle.RootRequestID = parent.RequestID
			}
			handle.Agent = parent.Agent
		}
	}
	c.registry.Register(handle)

	c.mu.Lock()
	record.state = RuntimeSessionStateActive
	c.mu.Unlock()
	return handle, nil
}

// supersedeSiblings 取消同代次中应被新执行取代的执行。
//
// Python: _new_execution 内的 superseded_kinds 分支
// 判定与置终态都在 registry 锁内（SupersedeWaiting），避免与并发
// MarkTerminal 读写 handle.State 竞争。
func (c *RuntimeSessionCoordinator) supersedeSiblings(record *sessionRecord, workKind SessionWorkKind) {
	var superseded []SessionWorkKind
	switch workKind {
	case SessionWorkKindChatUnary, SessionWorkKindChatStream:
		superseded = []SessionWorkKind{
			SessionWorkKindChatUnary, SessionWorkKindChatStream, SessionWorkKindHeartbeat,
		}
	case SessionWorkKindGoalStream:
		superseded = []SessionWorkKind{SessionWorkKindGoalStream, SessionWorkKindGoalAttach}
	default:
		return
	}
	want := map[SessionWorkKind]struct{}{}
	for _, k := range superseded {
		want[k] = struct{}{}
	}
	c.registry.SupersedeWaiting(record.sessionID, record.generation, want)
}

// runUnary 执行一次 unary 工作并维护状态。
//
// Python: RuntimeSessionCoordinator._run_unary
// scheduled 类走 lane（SESSION_MESSAGE 除外），其余直接在当前 goroutine 执行。
func (c *RuntimeSessionCoordinator) runUnary(
	parentCtx context.Context,
	record *sessionRecord,
	handle *SessionExecutionHandle,
	operation func(context.Context) (any, error),
	opts UnaryOptions,
) (any, error) {
	if handle.WorkKind.Scheduled() && handle.WorkKind != SessionWorkKindSessionMessage {
		return c.scheduler.SubmitAndWait(parentCtx, handle, func(opCtx context.Context) (any, error) {
			return c.executeUnaryTracked(opCtx, record, handle, operation, opts)
		})
	}

	opCtx, cancel := context.WithCancel(parentCtx)
	handle.Cancel = cancel
	defer func() {
		cancel()
		// 收尾完成：关闭 Done（spec §2.1，Done 由执行方 defer 关闭）
		handle.MarkDone()
	}()

	value, err := c.executeUnaryTracked(opCtx, record, handle, operation, opts)
	if err != nil {
		return nil, err
	}
	if opts.WaitForTerminal && !handle.State.Terminal() {
		select {
		case <-handle.Terminal.Wait():
		case <-opCtx.Done():
			return nil, opCtx.Err()
		}
		if handle.State == SessionExecutionStateCancelled {
			return nil, errors.New(handle.Error)
		}
		if handle.State == SessionExecutionStateFailed {
			return nil, errors.New(handle.Error)
		}
	}
	return value, nil
}

// executeUnaryTracked 执行并做状态迁移。
//
// Python: _run_unary 内的 tracked() 闭包
func (c *RuntimeSessionCoordinator) executeUnaryTracked(
	ctx context.Context,
	record *sessionRecord,
	handle *SessionExecutionHandle,
	operation func(context.Context) (any, error),
	opts UnaryOptions,
) (value any, err error) {
	c.registry.MarkRunning(handle)
	c.mu.Lock()
	record.state = RuntimeSessionStateActive
	c.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			c.registry.MarkTerminal(handle, SessionExecutionStateFailed, fmt.Sprint(r))
			c.refreshSessionState(record)
			panic(r)
		}
		switch {
		case err != nil && errors.Is(err, context.Canceled):
			c.registry.MarkTerminal(handle, SessionExecutionStateCancelled, err.Error())
		case err != nil:
			c.registry.MarkTerminal(handle, SessionExecutionStateFailed, err.Error())
		default:
			if opts.InteractionIDs != nil {
				c.registry.MarkAwaitingControl(handle, opts.InteractionIDs(value))
			}
			if len(handle.WaitingControlIDs) > 0 {
				c.registry.MarkWaiting(handle)
			} else {
				c.registry.MarkTerminal(handle, SessionExecutionStateSucceeded, "")
			}
		}
		c.refreshSessionState(record)
	}()

	return operation(ctx)
}

// refreshSessionState 依据活跃执行数刷新会话状态。
//
// Python: RuntimeSessionCoordinator._refresh_session_state
func (c *RuntimeSessionCoordinator) refreshSessionState(record *sessionRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshSessionStateLocked(record)
}

// refreshSessionStateLocked 加锁版刷新。
func (c *RuntimeSessionCoordinator) refreshSessionStateLocked(record *sessionRecord) {
	if c.sessions[record.sessionID] != record {
		return
	}
	if record.state == RuntimeSessionStateQuiescing || record.state == RuntimeSessionStateClosed {
		return
	}
	generation := record.generation
	active := c.registry.Select(SelectOptions{
		SessionID: record.sessionID, Generation: &generation, ActiveOnly: true,
	})
	c.refreshControlGateLocked(record, active)
	if len(active) > 0 {
		record.state = RuntimeSessionStateActive
	} else {
		record.state = RuntimeSessionStateReady
	}
}

// refreshControlGateLocked 依据等待交互刷新闸门。
//
// Python: RuntimeSessionCoordinator._refresh_control_gate
func (c *RuntimeSessionCoordinator) refreshControlGateLocked(record *sessionRecord, active []*SessionExecutionHandle) {
	if active == nil {
		generation := record.generation
		active = c.registry.Select(SelectOptions{
			SessionID: record.sessionID, Generation: &generation, ActiveOnly: true,
		})
	}
	for _, h := range active {
		if len(h.WaitingControlIDs) > 0 {
			record.controlReady.Clear()
			return
		}
	}
	record.controlReady.Set()
}

// snapshotLocked 生成会话快照。
//
// Python: RuntimeSessionCoordinator._snapshot
func (c *RuntimeSessionCoordinator) snapshotLocked(record *sessionRecord) RuntimeSessionSnapshot {
	generation := record.generation
	return RuntimeSessionSnapshot{
		SessionID:         record.sessionID,
		ChannelID:         record.channelID,
		PersistencePolicy: record.persistencePolicy,
		Generation:        record.generation,
		State:             record.state,
		Executions:        c.registry.SnapshotsForSession(record.sessionID, &generation),
	}
}

// newExecutionID 生成 32 位十六进制执行标识。
//
// Python: uuid.uuid4().hex
func newExecutionID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand 不可用时退化为时间戳，保证不 panic
		return fmt.Sprintf("%032x", uint64(time.Now().UnixNano()))
	}
	return hex.EncodeToString(buf[:])
}

// ──────────────────────────── 导出函数（流式） ────────────────────────────

// RunStream 运行一次流式执行，逐元素回调。
//
// Python: RuntimeSessionCoordinator.run_stream
// 团队种类必须走 RunTeamStream（上游断言文案逐字对齐）。
// emit 返回 false 表示消费方主动停止，将触发 cancel_execution。
func (c *RuntimeSessionCoordinator) RunStream(
	ctx context.Context,
	sessionID, requestID string,
	workKind SessionWorkKind,
	operation func(context.Context) (<-chan any, error),
	opts StreamOptions,
	emit func(any) bool,
) error {
	if _, isTeam := teamWorkKinds[workKind]; isTeam {
		return errors.New("Team work must use run_team_stream")
	}
	record, err := c.requireOpenSession(sessionID)
	if err != nil {
		return err
	}
	handle, err := c.newExecution(record, requestID, workKind, opts.ParentExecutionID, opts.Team)
	if err != nil {
		return err
	}
	return c.runStream(ctx, record, handle, operation, opts.InteractionIDs, emit)
}

// RunTeamStream 运行一次团队流式执行。
//
// Python: RuntimeSessionCoordinator.run_team_stream
// operation 接收 TeamExecutionContext（由协调器按 handle 组装）。
func (c *RuntimeSessionCoordinator) RunTeamStream(
	ctx context.Context,
	sessionID, requestID string,
	workKind SessionWorkKind,
	operation func(context.Context, TeamExecutionContext) (<-chan any, error),
	opts StreamOptions,
	emit func(any) bool,
) error {
	if _, isTeam := teamWorkKinds[workKind]; !isTeam {
		return errors.New("non-Team work must use run_stream")
	}
	details := opts.Details
	if details == nil {
		details = opts.Team
	}
	if details == nil {
		return errors.New("Team execution details must match the work kind")
	}
	record, err := c.requireOpenSession(sessionID)
	if err != nil {
		return err
	}
	handle, err := c.newExecution(record, requestID, workKind, opts.ParentExecutionID, details)
	if err != nil {
		return err
	}
	teamCtx := TeamExecutionContext{
		ExecutionLifecycleContext: ExecutionLifecycleContext{
			Session: SessionLifecycleContext{
				SessionID:  handle.SessionID,
				Generation: handle.Generation,
			},
			ExecutionID: handle.ExecutionID,
			RequestID:   handle.RequestID,
			WorkKind:    handle.WorkKind,
			ChannelID:   handle.ChannelID,
		},
		Details: *details,
	}
	contextual := func(ctx context.Context) (<-chan any, error) {
		return operation(ctx, teamCtx)
	}
	return c.runStream(ctx, record, handle, contextual, opts.InteractionIDs, emit)
}

// ──────────────────────────── 非导出函数（流式） ────────────────────────────

// runStream 流式生命周期：生产者 goroutine + 有界队列 + 消费者回调。
//
// Python: RuntimeSessionCoordinator._run_stream
//
// 关键约束（spec §7.4）：
//  1. 终态标记必须**阻塞**写入 queue，不能 select-default 丢弃，
//     否则消费者在缓冲满时会永远等待。
//  2. 生产者 defer 顺序：刷会话状态 → 投终态标记 → close(queue) → MarkDone。
//  3. use_lane 判定含 SESSION_MESSAGE 特例（虽有 scheduled 但不走 lane）。
func (c *RuntimeSessionCoordinator) runStream(
	parentCtx context.Context,
	record *sessionRecord,
	handle *SessionExecutionHandle,
	operation func(context.Context) (<-chan any, error),
	interactionIDs func(any) map[string]struct{},
	emit func(any) bool,
) error {
	queue := make(chan streamItem, c.streamBufferSize)
	opCtx, cancel := context.WithCancel(parentCtx)
	handle.Cancel = cancel

	produce := func(opCtx context.Context) (any, error) {
		c.registry.MarkRunning(handle)
		c.mu.Lock()
		record.state = RuntimeSessionStateActive
		c.mu.Unlock()

		var (
			stream       <-chan any
			terminalItem *streamItem
			opErr        error
		)
		defer func() {
			c.refreshSessionState(record)
			if terminalItem != nil {
				// 必须阻塞投递：缓冲满时丢终态会让消费者永久等待
				queue <- *terminalItem
			}
			close(queue)
			// spec §2.1：Done 最后关闭，表示收尾完成
			handle.MarkDone()
		}()

		stream, opErr = operation(opCtx)
		if opErr != nil {
			c.registry.MarkTerminal(handle, SessionExecutionStateFailed, opErr.Error())
			terminalItem = &streamItem{err: opErr, done: true}
			return nil, opErr
		}
		for item := range stream {
			if interactionIDs != nil {
				if ids := interactionIDs(item); len(ids) > 0 {
					c.registry.MarkAwaitingControl(handle, ids)
					c.mu.Lock()
					c.refreshControlGateLocked(record, nil)
					c.mu.Unlock()
				}
			}
			queue <- streamItem{value: item}
		}
		// 正常结束：有等待交互则置 WAITING，否则 SUCCEEDED
		if len(handle.WaitingControlIDs) > 0 {
			c.registry.MarkWaiting(handle)
		} else {
			c.registry.MarkTerminal(handle, SessionExecutionStateSucceeded, "")
		}
		terminalItem = &streamItem{done: true}
		return nil, nil
	}

	// use_lane：scheduled 且非 SESSION_MESSAGE
	// 上游注释：流式邮箱工作已持有宿主准入，放进聊天 lane 会死锁
	useLane := handle.WorkKind.Scheduled() && handle.WorkKind != SessionWorkKindSessionMessage

	produceDone := make(chan error, 1)
	go func() {
		if useLane {
			_, err := c.scheduler.SubmitAndWait(opCtx, handle, func(op context.Context) (any, error) {
				return produce(op)
			})
			produceDone <- err
			return
		}
		_, err := produce(opCtx)
		produceDone <- err
	}()

	completedNormally := false
	for item := range queue {
		if item.err != nil {
			// 生产者已结束，等它返回后上抛
			<-produceDone
			cancel()
			return item.err
		}
		if item.done {
			completedNormally = true
			break
		}
		if !emit(item.value) {
			// 消费方主动停止：继续 drain 队列直到生产者 close，
			// 避免生产者阻塞在 queue <- terminalItem（有界缓冲满）。
			for range queue {
			}
			break
		}
	}
	produceErr := <-produceDone
	cancel()
	if !completedNormally {
		_ = c.cancelExecutionInternal(handle.SessionID, handle.ExecutionID, nil)
	}
	return produceErr
}

// cancelExecutionInternal 内部取消（供 runStream 的异常清理使用）。
//
// CancelExecution 在后续步骤落地；此处直接取消 handle 的 ctx 并置终态，
// 语义等价（只针对单个已知执行）。
func (c *RuntimeSessionCoordinator) cancelExecutionInternal(
	sessionID, executionID string,
	_ *time.Duration,
) error {
	handle := c.registry.Get(executionID)
	if handle == nil {
		return nil
	}
	if handle.Cancel != nil {
		handle.Cancel()
	}
	if !handle.State.Terminal() {
		c.registry.MarkTerminal(handle, SessionExecutionStateCancelled, "")
	}
	if record, ok := c.sessionByID(sessionID); ok {
		c.refreshSessionState(record)
	}
	return nil
}

// sessionByID 取会话记录。
func (c *RuntimeSessionCoordinator) sessionByID(sessionID string) (*sessionRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.sessions[sessionID]
	return record, ok
}

// ──────────────────────────── 结构体（取消与关闭） ────────────────────────────

// CancelExecutionOptions CancelExecution 的查询条件。
//
// Python: RuntimeSessionCoordinator.cancel_execution 关键字参数
type CancelExecutionOptions struct {
	// RequestID 按请求标识取消
	RequestID *string
	// ExecutionID 按执行标识取消
	ExecutionID *string
	// Generation 按代次取消
	Generation *int
	// WaitTimeout 覆盖协调器默认超时
	WaitTimeout *time.Duration
	// ExcludedExecutionIDs 明确保留不取消的执行
	ExcludedExecutionIDs []string
}

// ──────────────────────────── 导出函数（取消与关闭） ────────────────────────────

// CancelExecution 取消匹配的执行及其后代。
//
// Python: RuntimeSessionCoordinator.cancel_execution
// 超时的执行**不**标 CANCELLED（保留原状态供下次重试）。
func (c *RuntimeSessionCoordinator) CancelExecution(
	sessionID string,
	opts CancelExecutionOptions,
) (CancelExecutionResult, error) {
	handles := c.registry.Select(SelectOptions{
		SessionID:   sessionID,
		RequestID:   opts.RequestID,
		ExecutionID: opts.ExecutionID,
		Generation:  opts.Generation,
		ActiveOnly:  true,
	})
	if len(handles) == 0 {
		return CancelExecutionResult{}, nil
	}
	handles = c.withDescendants(handles)
	if len(opts.ExcludedExecutionIDs) > 0 {
		excluded := map[string]struct{}{}
		for _, id := range opts.ExcludedExecutionIDs {
			excluded[id] = struct{}{}
		}
		filtered := handles[:0]
		for _, h := range handles {
			if _, skip := excluded[h.ExecutionID]; !skip {
				filtered = append(filtered, h)
			}
		}
		handles = filtered
	}
	if len(handles) == 0 {
		return CancelExecutionResult{}, nil
	}

	timeout := c.cancelTimeout
	if opts.WaitTimeout != nil {
		timeout = *opts.WaitTimeout
	}

	directIDs := map[string]struct{}{}
	var direct, work []*SessionExecutionHandle
	for _, h := range handles {
		if requiresDirectCancel(h) {
			direct = append(direct, h)
			directIDs[h.ExecutionID] = struct{}{}
			continue
		}
		if h.WorkKind.Scheduled() {
			work = append(work, h)
		}
	}
	// 非直接取消的执行：先标 CancellationRequested（对齐上游）
	for _, h := range handles {
		if _, isDirect := directIDs[h.ExecutionID]; !isDirect {
			c.registry.MarkCancellationRequested(h.ExecutionID)
		}
	}

	schedTimeouts := c.scheduler.CancelHandles(context.Background(), work, timeout)
	directTimeouts := c.requestDirectCancel(direct, timeout)
	timedOut := dedupeStrings(append(schedTimeouts, directTimeouts...))
	timedOutSet := map[string]struct{}{}
	for _, id := range timedOut {
		timedOutSet[id] = struct{}{}
	}

	cancelled := 0
	for _, h := range handles {
		if _, isTimedOut := timedOutSet[h.ExecutionID]; isTimedOut {
			continue
		}
		c.registry.MarkTerminalIfActive(h.ExecutionID, SessionExecutionStateCancelled)
		if c.registry.StateOf(h.ExecutionID) == SessionExecutionStateCancelled {
			cancelled++
		}
	}
	if record, ok := c.sessionByID(sessionID); ok {
		c.refreshSessionState(record)
	}
	return CancelExecutionResult{
		Matched:   len(handles),
		Cancelled: cancelled,
		TimedOut:  timedOut,
	}, nil
}

// CloseSession 关闭一个会话代次：静默 → 逐条取消 → 等 done → 置 CLOSED。
//
// Python: RuntimeSessionCoordinator.close_session
//
// 10 步（spec §7.5）：
//  1. 不存在或代次不符 → Existed=false
//  2. record.state = QUIESCING
//  3. 取该代次全部执行
//  4. active（非终态）/ settling（终态但 RetainOwnerTask 且 Done 未关）
//  5. direct = active 中 RequiresDirectCancel
//  6. directTimeouts = requestDirectCancel(direct)
//  7. scheduler.CloseSession(sid, gen)
//  8. scheduler 不存在时把未结束的 scheduled active 塞进 settling
//  9. settlingTimeouts = waitForHandles(settling)
//
// 10. 合并超时；非超时且未终态标 CANCELLED。有超时 → 保持 QUIESCING；无超时 → CLOSED
func (c *RuntimeSessionCoordinator) CloseSession(
	sessionID string,
	generation *int,
	waitTimeout *time.Duration,
) (CloseSessionResult, error) {
	timeout := c.cancelTimeout
	if waitTimeout != nil {
		timeout = *waitTimeout
	}
	c.mu.Lock()
	record, ok := c.sessions[sessionID]
	if !ok || (generation != nil && record.generation != *generation) {
		c.mu.Unlock()
		return CloseSessionResult{SessionID: sessionID, Generation: generation, Existed: false}, nil
	}
	record.state = RuntimeSessionStateQuiescing
	gen := record.generation
	c.mu.Unlock()

	executions := c.registry.Select(SelectOptions{SessionID: sessionID, Generation: &gen})
	var active, settling []*SessionExecutionHandle
	for _, h := range executions {
		switch {
		case !h.State.Terminal():
			active = append(active, h)
		case h.RetainOwnerTask && !isDoneClosed(h.Done):
			settling = append(settling, h)
		}
	}

	var direct []*SessionExecutionHandle
	for _, h := range active {
		if requiresDirectCancel(h) {
			direct = append(direct, h)
		}
	}
	directTimeouts := c.requestDirectCancel(direct, timeout)

	schedulerExisted, schedTimeouts := c.scheduler.CloseSession(sessionID, &gen, timeout)
	if !schedulerExisted {
		for _, h := range active {
			if h.WorkKind.Scheduled() && !isDoneClosed(h.Done) {
				settling = append(settling, h)
			}
		}
	}
	settlingTimeouts := c.waitForHandles(settling, timeout)

	timedOut := dedupeStrings(append(append(append([]string{}, directTimeouts...), schedTimeouts...), settlingTimeouts...))
	timedOutSet := map[string]struct{}{}
	for _, id := range timedOut {
		timedOutSet[id] = struct{}{}
	}
	for _, h := range active {
		c.registry.MarkCancellationRequested(h.ExecutionID)
		if _, isTimedOut := timedOutSet[h.ExecutionID]; isTimedOut {
			continue
		}
		c.registry.MarkTerminalIfActive(h.ExecutionID, SessionExecutionStateCancelled)
	}

	c.mu.Lock()
	if c.sessions[sessionID] == record && len(timedOut) == 0 {
		record.state = RuntimeSessionStateClosed
	}
	// 有超时：保持 QUIESCING（spec §2.2 最后一行 / D25）
	c.mu.Unlock()

	return CloseSessionResult{
		SessionID:  sessionID,
		Generation: &gen,
		Existed:    true,
		TimedOut:   timedOut,
	}, nil
}

// Close 关闭全部会话与调度器。
//
// Python: RuntimeSessionCoordinator.close
// 与上游差异：Go 无法返回元组，timedOut 用出参并返回 error（spec §8 D-10）。
func (c *RuntimeSessionCoordinator) Close(waitTimeout *time.Duration, timedOut *[]string) error {
	timeout := c.cancelTimeout
	if waitTimeout != nil {
		timeout = *waitTimeout
	}
	c.mu.Lock()
	c.accepting = false
	records := make([]*sessionRecord, 0, len(c.sessions))
	for _, r := range c.sessions {
		records = append(records, r)
	}
	c.mu.Unlock()

	var allTimedOut []string
	for _, r := range records {
		if r.state == RuntimeSessionStateClosed {
			continue
		}
		gen := r.generation
		res, err := c.CloseSession(r.sessionID, &gen, &timeout)
		if err != nil {
			return err
		}
		allTimedOut = append(allTimedOut, res.TimedOut...)
	}
	allTimedOut = dedupeStrings(allTimedOut)
	if timedOut != nil {
		*timedOut = allTimedOut
	}
	_ = c.scheduler.Close(timeout)
	if len(allTimedOut) > 0 {
		return &SessionCloseTimeoutError{SessionID: "runtime", ExecutionIDs: allTimedOut}
	}
	return nil
}

// ──────────────────────────── 非导出函数（取消与关闭） ────────────────────────────

// requiresDirectCancel 该执行是否走直接取消（发 ctx 取消）而非 lane 取消。
//
// Python: RuntimeSessionCoordinator._requires_direct_cancel
// SESSION_MESSAGE 虽属 scheduled，但强制走直接取消（spec D18）。
func requiresDirectCancel(h *SessionExecutionHandle) bool {
	if h.WorkKind == SessionWorkKindSessionMessage {
		return true
	}
	return !h.WorkKind.Scheduled()
}

// requestDirectCancel 分两批取消：fresh 先标记再取消并等；pending 只等。
//
// Python: RuntimeSessionCoordinator._request_direct_cancel
func (c *RuntimeSessionCoordinator) requestDirectCancel(
	handles []*SessionExecutionHandle,
	timeout time.Duration,
) []string {
	var pending, fresh []*SessionExecutionHandle
	for _, h := range handles {
		if c.registry.IsCancellationRequested(h.ExecutionID) {
			pending = append(pending, h)
		} else {
			fresh = append(fresh, h)
		}
	}
	for _, h := range fresh {
		c.registry.MarkCancellationRequested(h.ExecutionID)
	}
	cancelledTimeouts := c.cancelDirectHandles(fresh, timeout)
	pendingTimeouts := c.waitForHandles(pending, timeout)
	return append(cancelledTimeouts, pendingTimeouts...)
}

// cancelDirectHandles 对 fresh 批发取消并等待退出。
//
// Python: RuntimeSessionCoordinator._cancel_direct_handles
// 等的是 Done（执行方退出），不是 Terminal（spec §2.3）。
func (c *RuntimeSessionCoordinator) cancelDirectHandles(
	handles []*SessionExecutionHandle,
	timeout time.Duration,
) []string {
	for _, h := range handles {
		if isDoneClosed(h.Done) {
			continue
		}
		if cancel := c.registry.CancelOf(h.ExecutionID); cancel != nil {
			cancel()
		}
	}
	var timedOut []string
	for _, h := range handles {
		if isDoneClosed(h.Done) {
			continue
		}
		select {
		case <-h.Done:
		case <-time.After(timeout):
			timedOut = append(timedOut, h.ExecutionID)
		}
	}
	return timedOut
}

// waitForHandles 等待一批执行退出（等 Done）。
//
// Python: RuntimeSessionCoordinator._wait_for_handles
func (c *RuntimeSessionCoordinator) waitForHandles(
	handles []*SessionExecutionHandle,
	timeout time.Duration,
) []string {
	var timedOut []string
	deadline := time.After(timeout)
	for _, h := range handles {
		if isDoneClosed(h.Done) {
			continue
		}
		select {
		case <-h.Done:
		case <-deadline:
			timedOut = append(timedOut, h.ExecutionID)
		}
	}
	return timedOut
}

// withDescendants 展开执行的全部后代（跨代内同会话）。
//
// Python: RuntimeSessionCoordinator._with_descendants
func (c *RuntimeSessionCoordinator) withDescendants(handles []*SessionExecutionHandle) []*SessionExecutionHandle {
	selected := map[string]*SessionExecutionHandle{}
	for _, h := range handles {
		selected[h.ExecutionID] = h
	}
	frontier := make(map[string]struct{}, len(selected))
	for id := range selected {
		frontier[id] = struct{}{}
	}
	for len(frontier) > 0 {
		childIDs := map[string]struct{}{}
		scopes := map[SessionLifecycleContext]struct{}{}
		for _, h := range selected {
			scopes[SessionLifecycleContext{SessionID: h.SessionID, Generation: h.Generation}] = struct{}{}
		}
		for scope := range scopes {
			gen := scope.Generation
			actives := c.registry.Select(SelectOptions{
				SessionID: scope.SessionID, Generation: &gen, ActiveOnly: true,
			})
			for _, h := range actives {
				if _, isParent := frontier[h.ParentExecutionID]; !isParent {
					continue
				}
				if _, exists := selected[h.ExecutionID]; exists {
					continue
				}
				selected[h.ExecutionID] = h
				childIDs[h.ExecutionID] = struct{}{}
			}
		}
		frontier = childIDs
	}
	out := make([]*SessionExecutionHandle, 0, len(selected))
	for _, h := range selected {
		out = append(out, h)
	}
	sortByCreatedAt(out)
	return out
}

// ──────────────────────────── 结构体（control 通道） ────────────────────────────

// ControlDeliveryOptions 控制投递可选参数。
//
// Python: deliver_control / deliver_control_stream 关键字参数
type ControlDeliveryOptions struct {
	// InteractionIDs 从结果提取交互标识
	InteractionIDs func(any) map[string]struct{}
	// OnExecution 执行快照回调
	OnExecution func(SessionExecutionSnapshot)
}

// ──────────────────────────── 导出函数（control 通道） ────────────────────────────

// RecordInteraction 关联执行内抛出的交互标识。
//
// Python: RuntimeSessionCoordinator.record_interaction
func (c *RuntimeSessionCoordinator) RecordInteraction(
	sessionID, requestID string,
	controlIDs map[string]struct{},
) bool {
	c.mu.Lock()
	record, ok := c.sessions[sessionID]
	if !ok || record.state != RuntimeSessionStateActive {
		c.mu.Unlock()
		return false
	}
	gen := record.generation
	c.mu.Unlock()

	handles := c.registry.Select(SelectOptions{
		SessionID: sessionID, RequestID: &requestID, Generation: &gen, ActiveOnly: true,
	})
	for _, h := range handles {
		c.registry.MarkAwaitingControl(h, controlIDs)
		c.mu.Lock()
		c.refreshControlGateLocked(record, nil)
		c.mu.Unlock()
		return true
	}
	return false
}

// BindAgent 把 agent 实例绑到匹配执行。
//
// Python: RuntimeSessionCoordinator.bind_agent
func (c *RuntimeSessionCoordinator) BindAgent(sessionID, requestID string, agent any) {
	c.mu.Lock()
	record, ok := c.sessions[sessionID]
	if !ok {
		c.mu.Unlock()
		return
	}
	gen := record.generation
	c.mu.Unlock()

	for _, h := range c.registry.Select(SelectOptions{
		SessionID: sessionID, RequestID: &requestID, Generation: &gen, ActiveOnly: true,
	}) {
		c.registry.SetAgent(h.ExecutionID, agent)
	}
}

// ControlTargetAgent 取可接收控制的 agent。
//
// Python: RuntimeSessionCoordinator.control_target_agent
func (c *RuntimeSessionCoordinator) ControlTargetAgent(sessionID string, answered []string) any {
	c.mu.Lock()
	record, ok := c.sessions[sessionID]
	if !ok || record.state == RuntimeSessionStateClosed {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	parent := c.controlParent(sessionID, answered)
	if parent == nil {
		return nil
	}
	return c.registry.AgentOf(parent.ExecutionID)
}

// HasControlTarget 会话是否有可被控制恢复的活跃执行。
//
// Python: RuntimeSessionCoordinator.has_control_target
func (c *RuntimeSessionCoordinator) HasControlTarget(sessionID, requestID string) bool {
	c.mu.Lock()
	record, ok := c.sessions[sessionID]
	if !ok || record.state == RuntimeSessionStateClosed {
		c.mu.Unlock()
		return false
	}
	_, claimed := c.claimedExecutionIDLocked(record, requestID)
	c.mu.Unlock()
	if claimed {
		return true
	}
	return c.controlParent(sessionID, []string{requestID}) != nil
}

// WaitingInteractions 应答 requestID 能恢复的执行正在等待的全部交互。
//
// Python: RuntimeSessionCoordinator.waiting_interactions
// 上游用 _control_parent 查找（只认 RUNNING/WAITING_FOR_CONTROL 且等待集合含该 id）；
// 若该请求尚未进入等待态但已登记交互，_control_parent 会返回 nil。此处按 request_id
// 直查活跃执行，覆盖「刚 record_interaction 但状态未迁移」的窗口，语义是超集。
func (c *RuntimeSessionCoordinator) WaitingInteractions(sessionID, requestID string) map[string]struct{} {
	if parent := c.controlParent(sessionID, []string{requestID}); parent != nil {
		return c.registry.WaitingControlIDsOf(parent.ExecutionID)
	}
	c.mu.Lock()
	record, ok := c.sessions[sessionID]
	if !ok {
		c.mu.Unlock()
		return map[string]struct{}{}
	}
	gen := record.generation
	c.mu.Unlock()
	handles := c.registry.Select(SelectOptions{
		SessionID: sessionID, RequestID: &requestID, Generation: &gen, ActiveOnly: true,
	})
	out := map[string]struct{}{}
	for _, h := range handles {
		for id := range c.registry.WaitingControlIDsOf(h.ExecutionID) {
			out[id] = struct{}{}
		}
	}
	return out
}

// UnansweredInteractions 会话执行正在等待的交互，减去在飞投递已声明的部分。
//
// Python: RuntimeSessionCoordinator.unanswered_interactions
func (c *RuntimeSessionCoordinator) UnansweredInteractions(sessionID string) map[string]struct{} {
	c.mu.Lock()
	record, ok := c.sessions[sessionID]
	if !ok || record.state == RuntimeSessionStateClosed {
		c.mu.Unlock()
		return map[string]struct{}{}
	}
	gen := record.generation
	deliveries := make([]*delivery, 0, len(record.deliveries))
	for _, d := range record.deliveries {
		deliveries = append(deliveries, d)
	}
	c.mu.Unlock()

	waiting := c.registry.UnionWaitingControlIDs(sessionID, gen)
	for _, d := range deliveries {
		for id := range d.answered {
			delete(waiting, id)
		}
	}
	return waiting
}

// ControlTargetWorkKind 应答 requestID 的目标根执行的种类。
//
// Python: RuntimeSessionCoordinator.control_target_work_kind
func (c *RuntimeSessionCoordinator) ControlTargetWorkKind(sessionID, requestID string) (SessionWorkKind, bool) {
	parent := c.controlParent(sessionID, []string{requestID})
	if parent == nil {
		c.mu.Lock()
		if record, ok := c.sessions[sessionID]; ok {
			if eid, claimed := c.claimedExecutionIDLocked(record, requestID); claimed {
				parent = c.registry.Get(eid)
			}
		}
		c.mu.Unlock()
	}
	if parent == nil {
		return "", false
	}
	if root := c.heartbeatRoot(parent); root != nil {
		return root.WorkKind, true
	}
	return "", false
}

// DeliverControl 向运行中的会话工作投递输入，不占用其 lane。
//
// Python: RuntimeSessionCoordinator.deliver_control
func (c *RuntimeSessionCoordinator) DeliverControl(
	ctx context.Context,
	sessionID string,
	answered []string,
	operation func(context.Context) (any, error),
	opts ControlDeliveryOptions,
) (any, error) {
	record, parent, requestID, err := c.claimControlParent(sessionID, answered,
		"control input is already being delivered")
	if err != nil {
		return nil, err
	}
	claimed := c.registry.WaitingControlIDsOf(parent.ExecutionID)
	defer func() {
		c.releaseControlParent(record, parent)
		c.refreshSessionState(record)
	}()

	handle, err := c.newExecution(record, requestID, SessionWorkKindControlInput, parent.ExecutionID, nil)
	if err != nil {
		return nil, err
	}
	parentWasWaiting := c.registry.StateOf(parent.ExecutionID) == SessionExecutionStateWaitingForControl
	if parentWasWaiting {
		c.registry.ResumeWaiting(parent)
	}
	opCtx, cancel := context.WithCancel(ctx)
	handle.Cancel = cancel
	defer func() {
		cancel()
		handle.MarkDone()
	}()
	c.registry.MarkRunning(handle)
	if opts.OnExecution != nil {
		opts.OnExecution(handle.Snapshot())
	}

	value, opErr := operation(opCtx)
	if opErr != nil {
		if errors.Is(opErr, context.Canceled) {
			c.registry.MarkTerminal(handle, SessionExecutionStateCancelled, opErr.Error())
		} else {
			c.registry.MarkTerminal(handle, SessionExecutionStateFailed, opErr.Error())
		}
		if parentWasWaiting && !c.registry.StateOf(parent.ExecutionID).Terminal() {
			c.registry.MarkWaiting(parent)
		}
		return nil, opErr
	}

	c.mu.Lock()
	recordEnded := c.sessions[sessionID] != record ||
		record.state == RuntimeSessionStateQuiescing ||
		record.state == RuntimeSessionStateClosed
	c.mu.Unlock()
	if c.registry.StateOf(parent.ExecutionID).Terminal() || recordEnded {
		c.registry.MarkTerminal(handle, SessionExecutionStateCancelled, "")
		return nil, &SessionExecutionEndedError{
			Message: fmt.Sprintf("control input arrived after its execution ended: session=%s request=%s",
				sessionID, requestID),
		}
	}

	var controlIDs map[string]struct{}
	if opts.InteractionIDs != nil {
		controlIDs = opts.InteractionIDs(value)
	}
	root := c.heartbeatRoot(parent)
	switch {
	case len(controlIDs) > 0 && root != nil:
		c.registry.RemoveWaitingControlIDs(parent.ExecutionID, claimed)
		c.registry.MarkAwaitingControl(root, controlIDs)
		c.registry.MarkWaiting(root)
		c.registry.MarkTerminal(handle, SessionExecutionStateSucceeded, "")
	default:
		c.answered(parent, claimed, parentWasWaiting)
		if len(controlIDs) > 0 {
			c.registry.MarkAwaitingControl(handle, controlIDs)
			c.registry.MarkWaiting(handle)
		} else {
			c.registry.MarkTerminal(handle, SessionExecutionStateSucceeded, "")
		}
	}
	return value, nil
}

// ──────────────────────────── 非导出函数（control 通道） ────────────────────────────

// claimControlParent 认领应答 answered 所指的执行，若已有投递在飞则等待其结束。
//
// Python: RuntimeSessionCoordinator._claim_control_parent
func (c *RuntimeSessionCoordinator) claimControlParent(
	sessionID string,
	answered []string,
	duplicateMessage string,
) (*sessionRecord, *SessionExecutionHandle, string, error) {
	names := map[string]struct{}{}
	for _, id := range answered {
		names[id] = struct{}{}
	}
	for {
		record, err := c.requireOpenSession(sessionID)
		if err != nil {
			return nil, nil, "", err
		}
		c.mu.Lock()
		for _, d := range record.deliveries {
			covered := true
			for id := range names {
				if _, ok := d.answered[id]; !ok {
					covered = false
					break
				}
			}
			if covered {
				c.mu.Unlock()
				return nil, nil, "", &AnswerInFlightError{
					NoControlTargetError: &NoControlTargetError{Message: duplicateMessage},
				}
			}
		}
		c.mu.Unlock()

		parent := c.controlParent(sessionID, answered)
		if parent == nil {
			return nil, nil, "", &NoControlTargetError{
				Message: fmt.Sprintf("session has no active execution: %s", sessionID),
			}
		}
		parentWaiting := c.registry.WaitingControlIDsOf(parent.ExecutionID)
		matchedRequestID := ""
		for _, id := range answered {
			if _, ok := parentWaiting[id]; ok {
				matchedRequestID = id
				break
			}
		}
		if matchedRequestID == "" {
			return nil, nil, "", &NoControlTargetError{
				Message: fmt.Sprintf("session has no active execution: %s", sessionID),
			}
		}

		c.mu.Lock()
		if d, exists := record.deliveries[parent.ExecutionID]; exists {
			c.mu.Unlock()
			<-d.settled
			continue
		}
		record.deliveries[parent.ExecutionID] = &delivery{answered: names, settled: make(chan struct{})}
		c.mu.Unlock()
		return record, parent, matchedRequestID, nil
	}
}

// releaseControlParent 发放在飞投递的 settled 信号。
//
// Python: RuntimeSessionCoordinator._release_control_parent
func (c *RuntimeSessionCoordinator) releaseControlParent(record *sessionRecord, parent *SessionExecutionHandle) {
	c.mu.Lock()
	d, ok := record.deliveries[parent.ExecutionID]
	if ok {
		delete(record.deliveries, parent.ExecutionID)
	}
	c.mu.Unlock()
	if ok {
		d.close()
	}
}

// answered 把 claimed 从等待集合移除，并按需把父执行置终态。
//
// Python: RuntimeSessionCoordinator._answered
func (c *RuntimeSessionCoordinator) answered(
	parent *SessionExecutionHandle,
	claimed map[string]struct{},
	wasWaiting bool,
) {
	c.registry.RemoveWaitingControlIDs(parent.ExecutionID, claimed)
	remaining := c.registry.WaitingControlIDsOf(parent.ExecutionID)
	shouldTerminate := wasWaiting ||
		(c.registry.StateOf(parent.ExecutionID) == SessionExecutionStateWaitingForControl && len(remaining) == 0)
	if shouldTerminate {
		c.registry.MarkTerminal(parent, SessionExecutionStateSucceeded, "")
	}
}

// controlParent 找出应答 answered 中任一标识的执行（取最晚开始的）。
//
// Python: RuntimeSessionCoordinator._control_parent
func (c *RuntimeSessionCoordinator) controlParent(sessionID string, answered []string) *SessionExecutionHandle {
	c.mu.Lock()
	record, ok := c.sessions[sessionID]
	if !ok {
		c.mu.Unlock()
		return nil
	}
	gen := record.generation
	c.mu.Unlock()

	actives := c.registry.Select(SelectOptions{
		SessionID: sessionID, Generation: &gen, ActiveOnly: true,
	})
	var parents []*SessionExecutionHandle
	for _, h := range actives {
		state := c.registry.StateOf(h.ExecutionID)
		if state != SessionExecutionStateRunning && state != SessionExecutionStateWaitingForControl {
			continue
		}
		for _, id := range answered {
			if _, waiting := c.registry.WaitingControlIDsOf(h.ExecutionID)[id]; waiting {
				parents = append(parents, h)
				break
			}
		}
	}
	if len(parents) == 0 {
		return nil
	}
	best := parents[0]
	bestStart := c.registry.StartedAtOf(best.ExecutionID)
	for _, h := range parents[1:] {
		curStart := c.registry.StartedAtOf(h.ExecutionID)
		if curStart > bestStart {
			best = h
			bestStart = curStart
		}
	}
	return best
}

// claimedExecutionIDLocked 取正在投递某交互的执行标识（调用方持 c.mu）。
//
// Python: RuntimeSessionCoordinator._claimed_execution_id
func (c *RuntimeSessionCoordinator) claimedExecutionIDLocked(record *sessionRecord, requestID string) (string, bool) {
	for executionID, d := range record.deliveries {
		if _, ok := d.answered[requestID]; ok {
			return executionID, true
		}
	}
	return "", false
}

// heartbeatRoot 沿父链向上找 HEARTBEAT 根执行。
//
// Python: RuntimeSessionCoordinator._heartbeat_root
func (c *RuntimeSessionCoordinator) heartbeatRoot(handle *SessionExecutionHandle) *SessionExecutionHandle {
	current := handle
	seen := map[string]struct{}{}
	for {
		if _, dup := seen[current.ExecutionID]; dup {
			return nil
		}
		seen[current.ExecutionID] = struct{}{}
		if current.WorkKind == SessionWorkKindHeartbeat {
			return current
		}
		if current.ParentExecutionID == "" {
			return nil
		}
		parent := c.registry.Get(current.ParentExecutionID)
		if parent == nil || parent.SessionID != handle.SessionID || parent.Generation != handle.Generation {
			return nil
		}
		current = parent
	}
}
