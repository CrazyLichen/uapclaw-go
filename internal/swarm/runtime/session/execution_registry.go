package session

import (
	"context"
	"sort"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SelectOptions 执行查询条件。
// Python: SessionExecutionRegistry.select 的关键字参数
type SelectOptions struct {
	// SessionID 会话标识（必填）
	SessionID string
	// RequestID 请求标识过滤
	RequestID *string
	// ExecutionID 执行标识过滤（优先级最高）
	ExecutionID *string
	// Generation 代次过滤
	Generation *int
	// ActiveOnly 仅返回非终态
	ActiveOnly bool
}

// SessionExecutionRegistry 执行索引，不承担持久化会话仓储职责。
// Python: runtime/session/execution_registry.py SessionExecutionRegistry
//
// MarkTerminal 是唯一关闭 handle.Terminal 的地方（spec §2.1），
// 且不清 handle.Done（Done 由执行方 defer 关闭，置 nil 会让 <-Done 永久阻塞）。
type SessionExecutionRegistry struct {
	// mu 保护全部内部状态
	mu sync.Mutex
	// terminalCapacity 终态执行保留上限
	terminalCapacity int
	// terminalTTL 终态执行保留秒数
	terminalTTL float64
	// executions 执行标识 → 句柄
	executions map[string]*SessionExecutionHandle
	// bySession 会话标识 → 执行标识集合
	bySession map[string]map[string]struct{}
	// byRequest (会话标识, 请求标识) → 执行标识集合
	byRequest map[[2]string]map[string]struct{}
	// terminal 终态执行标识队列（FIFO）
	terminal []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionExecutionRegistry 创建执行注册表。
// Python: SessionExecutionRegistry.__init__(terminal_capacity=512, terminal_ttl=900.0)
// terminalCapacity 为负时 panic（对齐上游 raise ValueError）；terminalTTL 取下界 0。
func NewSessionExecutionRegistry(terminalCapacity int, terminalTTL float64) *SessionExecutionRegistry {
	if terminalCapacity < 0 {
		panic("terminalCapacity must be non-negative")
	}
	if terminalTTL < 0 {
		terminalTTL = 0
	}
	return &SessionExecutionRegistry{
		terminalCapacity: terminalCapacity,
		terminalTTL:      terminalTTL,
		executions:       map[string]*SessionExecutionHandle{},
		bySession:        map[string]map[string]struct{}{},
		byRequest:        map[[2]string]map[string]struct{}{},
	}
}

// Register 登记一个新执行，重复标识 panic。
// Python: SessionExecutionRegistry.register
func (r *SessionExecutionRegistry) Register(h *SessionExecutionHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictTerminalLocked()
	if _, exists := r.executions[h.ExecutionID]; exists {
		panic("duplicate execution_id: " + h.ExecutionID)
	}
	r.executions[h.ExecutionID] = h
	indexBySession(r.bySession, h.SessionID, h.ExecutionID)
	indexByRequest(r.byRequest, [2]string{h.SessionID, h.RequestID}, h.ExecutionID)
}

// MarkRunning 把 QUEUED 置为 RUNNING 并记录 StartedAt。
// Python: SessionExecutionRegistry.mark_running（仅 QUEUED 生效）
func (r *SessionExecutionRegistry) MarkRunning(h *SessionExecutionHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	markRunningLocked(h)
}

// MarkAwaitingControl 把交互标识并入执行的等待集合（非终态才生效）。
// Python: SessionExecutionRegistry.mark_awaiting_control
func (r *SessionExecutionRegistry) MarkAwaitingControl(h *SessionExecutionHandle, controlIDs map[string]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	markAwaitingControlLocked(h, controlIDs)
}

// MarkWaiting 置为 WAITING_FOR_CONTROL（非终态才生效）。
// Python: SessionExecutionRegistry.mark_waiting
func (r *SessionExecutionRegistry) MarkWaiting(h *SessionExecutionHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	markWaitingLocked(h)
}

// ResumeWaiting 把 WAITING_FOR_CONTROL 恢复为 RUNNING。
// Python: SessionExecutionRegistry.resume_waiting
func (r *SessionExecutionRegistry) ResumeWaiting(h *SessionExecutionHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	resumeWaitingLocked(h)
}

// MarkTerminal 置终态。这是**唯一**关闭 h.Terminal 的地方。
// Python: SessionExecutionRegistry.mark_terminal
// 已终态时幂等早返回（仍确保 Terminal 已关闭），不再改写其余字段。
func (r *SessionExecutionRegistry) MarkTerminal(h *SessionExecutionHandle, state SessionExecutionState, errText string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	markTerminalLocked(r, h, state, errText)
}

// ReleaseOwnerTask 终态后把执行移入历史队列。
// Python: SessionExecutionRegistry.release_owner_task
func (r *SessionExecutionRegistry) ReleaseOwnerTask(h *SessionExecutionHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releaseOwnerTaskLocked(h)
}

// SupersedeWaiting 取消同代次中工作种类属于 kinds 且处于 WAITING_FOR_CONTROL 的执行。
// Python: _new_execution 内的 superseded_kinds 分支
// 整段在 registry 锁内完成（读写 handle.State 与 CancellationRequested），
// 避免与并发的 MarkTerminal 竞争。
func (r *SessionExecutionRegistry) SupersedeWaiting(sessionID string, generation int, kinds map[SessionWorkKind]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictTerminalLocked()
	for _, h := range r.selectLocked(SelectOptions{
		SessionID: sessionID, Generation: &generation, ActiveOnly: true,
	}) {
		if _, want := kinds[h.WorkKind]; !want {
			continue
		}
		if h.State != SessionExecutionStateWaitingForControl {
			continue
		}
		h.CancellationRequested = true
		markTerminalLocked(r, h, SessionExecutionStateCancelled, "")
	}
}

// StateOf 原子读取执行状态。
// 锁外直接读 handle.State 会与并发 MarkTerminal 竞争（-race 实测），
// 因此对外一律经此方法。
func (r *SessionExecutionRegistry) StateOf(executionID string) SessionExecutionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.executions[executionID]; ok {
		return h.State
	}
	return ""
}

// CancelOf 原子取取消函数（可能为 nil）。
func (r *SessionExecutionRegistry) CancelOf(executionID string) context.CancelFunc {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.executions[executionID]; ok {
		return h.Cancel
	}
	return nil
}

// IsCancellationRequested 原子读取取消标记。
func (r *SessionExecutionRegistry) IsCancellationRequested(executionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.executions[executionID]; ok {
		return h.CancellationRequested
	}
	return false
}

// MarkCancellationRequested 原子置取消标记。
func (r *SessionExecutionRegistry) MarkCancellationRequested(executionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.executions[executionID]; ok {
		h.CancellationRequested = true
	}
}

// MarkTerminalIfActive 非终态时置终态，返回是否真的置了。
// 与 MarkTerminal 的区别：已终态时不重复登记终态队列。
func (r *SessionExecutionRegistry) MarkTerminalIfActive(executionID string, state SessionExecutionState) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.executions[executionID]
	if !ok || h.State.Terminal() {
		return false
	}
	markTerminalLocked(r, h, state, "")
	return true
}

// SnapshotOf 原子取单条执行快照（不存在返回 nil）。
func (r *SessionExecutionRegistry) SnapshotOf(executionID string) *SessionExecutionSnapshot {
	snap := r.SnapshotValue(executionID)
	if snap.ExecutionID == "" {
		return nil
	}
	return &snap
}

// SnapshotValue 原子取单条执行快照值（不存在返回零值，ExecutionID 为空）。
// 与 SnapshotOf 的区别：返回值而非指针，避免调用方误以为可空。
func (r *SessionExecutionRegistry) SnapshotValue(executionID string) SessionExecutionSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.executions[executionID]
	if !ok {
		return SessionExecutionSnapshot{}
	}
	return h.Snapshot()
}

// AgentOf 原子读取执行绑定的 agent。
func (r *SessionExecutionRegistry) AgentOf(executionID string) any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.executions[executionID]; ok {
		return h.Agent
	}
	return nil
}

// SetAgent 原子设置执行绑定的 agent。
func (r *SessionExecutionRegistry) SetAgent(executionID string, agent any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.executions[executionID]; ok {
		h.Agent = agent
	}
}

// WaitingControlIDsOf 原子取等待交互集合副本。
func (r *SessionExecutionRegistry) WaitingControlIDsOf(executionID string) map[string]struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]struct{}{}
	if h, ok := r.executions[executionID]; ok {
		for k := range h.WaitingControlIDs {
			out[k] = struct{}{}
		}
	}
	return out
}

// RemoveWaitingControlIDs 原子从等待集合移除指定标识。
func (r *SessionExecutionRegistry) RemoveWaitingControlIDs(executionID string, ids map[string]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.executions[executionID]; ok {
		for id := range ids {
			delete(h.WaitingControlIDs, id)
		}
	}
}

// UnionWaitingControlIDs 原子并集某会话某代次全部活跃执行的等待交互集合。
func (r *SessionExecutionRegistry) UnionWaitingControlIDs(sessionID string, generation int) map[string]struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictTerminalLocked()
	out := map[string]struct{}{}
	for _, h := range r.selectLocked(SelectOptions{
		SessionID: sessionID, Generation: &generation, ActiveOnly: true,
	}) {
		for id := range h.WaitingControlIDs {
			out[id] = struct{}{}
		}
	}
	return out
}

// StartedAtOf 原子读取开始时刻（nil 时回落 CreatedAt）。
func (r *SessionExecutionRegistry) StartedAtOf(executionID string) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.executions[executionID]
	if !ok {
		return 0
	}
	if h.StartedAt != nil {
		return *h.StartedAt
	}
	return h.CreatedAt
}

// Get 按执行标识取句柄。
// Python: SessionExecutionRegistry.get
func (r *SessionExecutionRegistry) Get(executionID string) *SessionExecutionHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictTerminalLocked()
	return r.executions[executionID]
}

// Select 按条件查询执行，结果按 CreatedAt 升序。
// Python: SessionExecutionRegistry.select
func (r *SessionExecutionRegistry) Select(opts SelectOptions) []*SessionExecutionHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictTerminalLocked()
	return r.selectLocked(opts)
}

// SnapshotsForSession 取某会话（可选代次）的全部执行快照。
// Python: SessionExecutionRegistry.snapshots_for_session
// 全流程持锁：Snapshot() 读 handle 的可变字段，锁外调用会与 MarkRunning /
// MarkTerminal 竞争（-race 实测）。
func (r *SessionExecutionRegistry) SnapshotsForSession(sessionID string, generation *int) []SessionExecutionSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictTerminalLocked()
	handles := r.selectLocked(SelectOptions{SessionID: sessionID, Generation: generation})
	out := make([]SessionExecutionSnapshot, 0, len(handles))
	for _, h := range handles {
		out = append(out, h.Snapshot())
	}
	return out
}

// Active 返回全部非终态执行。
// Python: SessionExecutionRegistry.active
func (r *SessionExecutionRegistry) Active() []*SessionExecutionHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictTerminalLocked()
	out := make([]*SessionExecutionHandle, 0, len(r.executions))
	for _, h := range r.executions {
		if !h.State.Terminal() {
			out = append(out, h)
		}
	}
	sortByCreatedAt(out)
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// indexBySession 把执行标识加入会话索引。
func indexBySession(m map[string]map[string]struct{}, key, executionID string) {
	set, ok := m[key]
	if !ok {
		set = map[string]struct{}{}
		m[key] = set
	}
	set[executionID] = struct{}{}
}

// indexByRequest 把执行标识加入 (会话, 请求) 索引。
func indexByRequest(m map[[2]string]map[string]struct{}, key [2]string, executionID string) {
	set, ok := m[key]
	if !ok {
		set = map[string]struct{}{}
		m[key] = set
	}
	set[executionID] = struct{}{}
}

// unindexBySession 从会话索引移除执行标识，空集合时删除键。
func unindexBySession(m map[string]map[string]struct{}, key, executionID string) {
	set, ok := m[key]
	if !ok {
		return
	}
	delete(set, executionID)
	if len(set) == 0 {
		delete(m, key)
	}
}

// unindexByRequest 从 (会话, 请求) 索引移除执行标识。
func unindexByRequest(m map[[2]string]map[string]struct{}, key [2]string, executionID string) {
	set, ok := m[key]
	if !ok {
		return
	}
	delete(set, executionID)
	if len(set) == 0 {
		delete(m, key)
	}
}

// markRunningLocked 加锁版 MarkRunning 逻辑。
func markRunningLocked(h *SessionExecutionHandle) {
	if h.State != SessionExecutionStateQueued {
		return
	}
	h.State = SessionExecutionStateRunning
	now := monotonicSeconds()
	h.StartedAt = &now
}

// markAwaitingControlLocked 加锁版 MarkAwaitingControl 逻辑。
func markAwaitingControlLocked(h *SessionExecutionHandle, controlIDs map[string]struct{}) {
	if h.State.Terminal() {
		return
	}
	if h.WaitingControlIDs == nil {
		h.WaitingControlIDs = map[string]struct{}{}
	}
	for id := range controlIDs {
		h.WaitingControlIDs[id] = struct{}{}
	}
}

// markWaitingLocked 加锁版 MarkWaiting 逻辑。
func markWaitingLocked(h *SessionExecutionHandle) {
	if h.State.Terminal() {
		return
	}
	h.State = SessionExecutionStateWaitingForControl
	if !h.RetainOwnerTask {
		h.Cancel = nil
	}
}

// resumeWaitingLocked 加锁版 ResumeWaiting 逻辑。
func resumeWaitingLocked(h *SessionExecutionHandle) {
	if h.State == SessionExecutionStateWaitingForControl {
		h.State = SessionExecutionStateRunning
	}
}

// markTerminalLocked 加锁版 MarkTerminal 逻辑。
// 幂等语义：已终态时只确保 Terminal 已发出，不改写其余字段。
// 不清 h.Done（Go 侧 Done 由执行方 defer 关闭，置 nil 会让 <-h.Done 永久阻塞）。
func markTerminalLocked(r *SessionExecutionRegistry, h *SessionExecutionHandle, state SessionExecutionState, errText string) {
	if !state.Terminal() {
		panic("terminal state required")
	}
	if h.State.Terminal() {
		h.Terminal.Set()
		return
	}
	h.State = state
	h.WaitingControlIDs = map[string]struct{}{}
	h.Agent = nil
	now := monotonicSeconds()
	h.FinishedAt = &now
	if errText != "" {
		h.Error = errText
	}
	if !h.RetainOwnerTask {
		h.Cancel = nil
	}
	h.Terminal.Set()
	// 已无执行方需要等待时，直接进入终态队列
	if h.Cancel == nil || isDoneClosed(h.Done) {
		r.terminal = append(r.terminal, h.ExecutionID)
	}
	r.evictTerminalLocked()
}

// releaseOwnerTaskLocked 加锁版 ReleaseOwnerTask 逻辑。
func (r *SessionExecutionRegistry) releaseOwnerTaskLocked(h *SessionExecutionHandle) {
	if h.State.Terminal() {
		r.terminal = append(r.terminal, h.ExecutionID)
		r.evictTerminalLocked()
	}
}

// isDoneClosed 判断 Done 是否已关闭。
func isDoneClosed(done chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// evictTerminalLocked 按容量与 TTL 淘汰终态执行。
// Python: SessionExecutionRegistry._evict_terminal
// 只有执行方已退出（Done 已关）时才真正从账本移除，与上游
// `handle.task is None or handle.task.done()` 语义一致。
func (r *SessionExecutionRegistry) evictTerminalLocked() {
	now := monotonicSeconds()
	for len(r.terminal) > 0 {
		executionID := r.terminal[0]
		h := r.executions[executionID]
		expired := h == nil || h.FinishedAt == nil || now-*h.FinishedAt > r.terminalTTL
		overCapacity := len(r.terminal) > r.terminalCapacity
		if !expired && !overCapacity {
			return
		}
		if h != nil && !isDoneClosed(h.Done) {
			// 执行方还在跑：本轮不淘汰（保留到 Done 关闭后再处理）
			return
		}
		r.terminal = r.terminal[1:]
		if h == nil || !h.State.Terminal() {
			continue
		}
		r.removeLocked(h)
	}
}

// removeLocked 从三个索引移除执行。
// Python: SessionExecutionRegistry._remove
func (r *SessionExecutionRegistry) removeLocked(h *SessionExecutionHandle) {
	delete(r.executions, h.ExecutionID)
	unindexBySession(r.bySession, h.SessionID, h.ExecutionID)
	unindexByRequest(r.byRequest, [2]string{h.SessionID, h.RequestID}, h.ExecutionID)
}

// selectLocked 加锁版 Select 逻辑。
func (r *SessionExecutionRegistry) selectLocked(opts SelectOptions) []*SessionExecutionHandle {
	var candidates []string
	switch {
	case opts.ExecutionID != nil:
		candidates = []string{*opts.ExecutionID}
	case opts.RequestID != nil:
		for id := range r.byRequest[[2]string{opts.SessionID, *opts.RequestID}] {
			candidates = append(candidates, id)
		}
	default:
		for id := range r.bySession[opts.SessionID] {
			candidates = append(candidates, id)
		}
	}
	selected := make([]*SessionExecutionHandle, 0, len(candidates))
	for _, id := range candidates {
		h := r.executions[id]
		if h == nil || h.SessionID != opts.SessionID {
			continue
		}
		if opts.Generation != nil && h.Generation != *opts.Generation {
			continue
		}
		if opts.ActiveOnly && h.State.Terminal() {
			continue
		}
		selected = append(selected, h)
	}
	sortByCreatedAt(selected)
	return selected
}

// sortByCreatedAt 按创建时刻升序排序。
func sortByCreatedAt(handles []*SessionExecutionHandle) {
	sort.Slice(handles, func(i, j int) bool { return handles[i].CreatedAt < handles[j].CreatedAt })
}
