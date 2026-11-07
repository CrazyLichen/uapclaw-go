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
