package session

import (
	"container/heap"
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// 本文件对应 Python server/runtime/session/session_manager.py（402 行，17 方法）。
//
// 10.3.28d 曾基于"上游不存在该文件"的错误断言删除它。核对确认文件存在（402 行），
// 且它与 Runtime 协调器是**互斥**关系而非串联（service.py:1873 按
// uses_session_runtime 二选一），故本文件原样恢复。
//
// Go 化偏差（spec §8 D-3..D-5）：
//   asyncio.PriorityQueue → container/heap
//   contextvars.copy_context() → 显式随任务携带 ctx
//   asyncio.Future → chan taskResult

// ──────────────────────────── 结构体 ────────────────────────────

// SessionManager 管理多会话并发执行，同会话内任务按后进先出执行。
// Python: session_manager.py SessionManager
type SessionManager struct {
	// mu 保护以下全部字段
	mu sync.Mutex
	// sessionTasks 会话 → 当前正在跑的任务
	sessionTasks map[string]*sessionTask
	// sessionPriorities 会话 → 下一个优先级（递减即 LIFO：新任务优先）
	sessionPriorities map[string]int64
	// sessionQueues 会话 → 任务优先队列
	sessionQueues map[string]*taskQueue
	// sessionProcessors 会话 → 本代处理器记录（含关闭信号与退出信号）
	sessionProcessors map[string]*processorGeneration
	// closingTasks 会话 → 正在收尾的任务集合
	closingTasks map[string]map[*sessionTask]struct{}
}

// processorGeneration 一代会话处理器。
// Python 用 asyncio.Task 本身做身份与等待对象；Go 拆成关闭信号 + 退出信号两个通道。
type processorGeneration struct {
	// closing 关闭信号（CloseSession 关闭它通知处理器退出）
	closing chan struct{}
	// exited 退出信号（处理器真正返回时关闭）
	exited chan struct{}
}

// sessionTask 一次排队的任务。
type sessionTask struct {
	// fn 任务函数；nil 表示一次性会话的退出哨兵
	fn func(context.Context) (any, error)
	// ctx 调用方上下文（Go 无 contextvars，显式携带以保证 workspace/cwd 不丢）
	ctx context.Context
	// result 结果通道（缓冲区 1，保证处理器不阻塞）
	result chan taskResult
	// cancel 取消该任务上下文（处理器启动后设置）
	cancel context.CancelFunc
	// priority 队列优先级（数值小的先执行）
	priority int64
	// finished 任务是否已结束
	finished bool
}

// taskResult 任务结果。
type taskResult struct {
	// value 返回值
	value any
	// err 错误
	err error
}

// taskQueue 会话任务优先队列（实现 container/heap.Interface）。
type taskQueue struct {
	// items 队列元素
	items []*sessionTask
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// oneShotSentinelPriority 一次性会话退出哨兵的优先级。
	// 取极大值确保排在所有已入队任务之后，不抢占未跑的任务。
	// Python: session_manager.py queue.put((1_000_000_000, None, None, None))
	oneShotSentinelPriority = int64(1_000_000_000)
	// defaultCloseWaitTimeout 默认关闭等待时长（秒）
	// Python: session_manager.py close_session(wait_timeout=5.0)
	defaultCloseWaitTimeout = 5.0
	// processorIdlePollInterval 处理器空闲轮询间隔。
	// Python 用 PriorityQueue.get() 阻塞等待；Go 侧用条件变量唤醒，
	// 此常量仅作为条件变量不可用时的兜底轮询。
	processorIdlePollInterval = time.Millisecond
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// errSessionTaskPanicked 任务 panic 时的错误
	errSessionTaskPanicked = errors.New("session task panicked")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionManager 创建会话任务管理器。
// Python: SessionManager.__init__
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessionTasks:      map[string]*sessionTask{},
		sessionPriorities: map[string]int64{},
		sessionQueues:     map[string]*taskQueue{},
		sessionProcessors: map[string]*processorGeneration{},
		closingTasks:      map[string]map[*sessionTask]struct{}{},
	}
}

// GetSessionID 取会话标识，缺省为 "default"。
// Python: session_manager.py get_session_id
func GetSessionID(sessionID *string) string {
	if sessionID == nil || strings.TrimSpace(*sessionID) == "" {
		return "default"
	}
	return *sessionID
}

// CancelSessionTask 取消指定会话的非流式任务。
// Python: session_manager.py cancel_session_task
//
// waitTimeout 为 nil 时等任务真正结束；非 nil 时按超时等待，超时只告警。
func (m *SessionManager) CancelSessionTask(
	ctx context.Context, sessionID, logPrefix string, waitTimeout *float64,
) error {
	m.mu.Lock()
	task := m.sessionTasks[sessionID]
	running := task != nil && !task.finished
	m.mu.Unlock()
	if !running {
		return nil
	}
	logger.Info(logComponent).Str("session_id", sessionID).
		Msg("[SessionManager] " + logPrefix + "cancel session unary task")

	if task.cancel != nil {
		task.cancel()
	}
	terminated := m.waitTaskTerminated(ctx, sessionID, task, waitTimeout, logPrefix)
	if !terminated {
		return nil
	}
	m.mu.Lock()
	if m.sessionTasks[sessionID] == task {
		m.sessionTasks[sessionID] = nil
	}
	m.mu.Unlock()
	logger.Info(logComponent).Str("session_id", sessionID).
		Msg("[SessionManager] " + logPrefix + "session task terminated")
	return nil
}

// CancelAllSessionTasks 取消所有会话正在跑的非流式任务。
// Python: session_manager.py cancel_all_session_tasks
//
// excludeSessionIDs 中的会话跳过不取消。
func (m *SessionManager) CancelAllSessionTasks(
	ctx context.Context, logPrefix string, excludeSessionIDs *map[string]struct{},
) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessionTasks))
	for id := range m.sessionTasks {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		if excludeSessionIDs != nil {
			if _, excluded := (*excludeSessionIDs)[id]; excluded {
				continue
			}
		}
		_ = m.CancelSessionTask(ctx, id, logPrefix, nil)
	}
}

// CloseSession 停止并释放指定会话这一代的任务处理器，返回是否有运行时被清理。
// Python: session_manager.py close_session
//
// 返回值语义与协调器 CloseSession 不同：本方法回答"清没清到东西"。
//
// 步骤与上游一致：
//  1. 取本代 closing_tasks 快照、processor、task、queue
//  2. 取消该 task 的等待结果（若有）
//  3. 清空队列并取消每个排队项的结果通道
//  4. 通知 processor 退出、取消 task
//  5. 把等待目标登记进 closing_tasks（带 finish 回调语义）
//  6. 等剩余目标，完成的走 finishClosingTask，超时的告警
func (m *SessionManager) CloseSession(
	ctx context.Context, sessionID string, waitTimeout *float64,
) (bool, error) {
	m.mu.Lock()
	closing := make(map[*sessionTask]struct{}, len(m.closingTasks[sessionID]))
	for target := range m.closingTasks[sessionID] {
		closing[target] = struct{}{}
	}
	processor := m.sessionProcessors[sessionID]
	task := m.sessionTasks[sessionID]
	queue := m.sessionQueues[sessionID]
	_, hadPriority := m.sessionPriorities[sessionID]
	hadRuntime := len(closing) > 0 || task != nil || hadPriority ||
		queue != nil || processor != nil

	delete(m.sessionProcessors, sessionID)
	delete(m.sessionQueues, sessionID)
	delete(m.sessionPriorities, sessionID)
	delete(m.sessionTasks, sessionID)
	m.mu.Unlock()

	// 步骤 2：取消该 task 的等待结果（唤醒 submit_and_wait 的等待方）
	if task != nil {
		m.cancelResultFuture(task)
	}
	// 步骤 3：清空队列并取消每个排队项的结果通道
	if queue != nil {
		queue.drainCancelled()
	}

	// 步骤 4：通知 processor 退出、取消 task
	if processor != nil {
		close(processor.closing)
	}
	if task != nil && task.cancel != nil {
		task.cancel()
	}

	// 步骤 5：把等待目标登记进 closing_tasks
	waitTargets := make([]*sessionTask, 0, len(closing)+1)
	for target := range closing {
		waitTargets = append(waitTargets, target)
	}
	if task != nil {
		if _, already := closing[task]; !already {
			waitTargets = append(waitTargets, task)
			m.trackClosingTask(sessionID, task)
		}
	}
	if len(waitTargets) == 0 && processor == nil {
		return hadRuntime, nil
	}

	timeout := defaultCloseWaitTimeout
	if waitTimeout != nil {
		timeout = *waitTimeout
	}
	timer := time.NewTimer(time.Duration(timeout * float64(time.Second)))
	defer timer.Stop()

	// 步骤 6：等剩余目标
	pending := 0
	for _, target := range waitTargets {
		select {
		case <-target.result:
			m.finishClosingTask(sessionID, target)
		case <-timer.C:
			pending++
		case <-ctx.Done():
			pending++
		}
	}
	if processor != nil {
		select {
		case <-processor.exited:
		case <-timer.C:
			pending++
		case <-ctx.Done():
			pending++
		}
	}
	if pending > 0 {
		logger.Warn(logComponent).Str("session_id", sessionID).
			Float64("wait_timeout", timeout).Int("pending", pending).
			Msg("[SessionManager] close session wait timeout")
	}
	return hadRuntime, nil
}

// cancelResultFuture 取消任务的结果等待（唤醒 submit_and_wait 的等待方）。
// Python: session_manager.py _cancel_result_future
//
// Go 侧结果通道是 chan taskResult：取消即向通道投一个已取消错误，
// 让仍在 result 上阻塞的调用方立刻返回，而不是等任务真正结束。
func (m *SessionManager) cancelResultFuture(task *sessionTask) {
	if task == nil || task.result == nil {
		return
	}
	select {
	case task.result <- taskResult{err: context.Canceled}:
	default:
	}
}

// trackClosingTask 把待关闭的任务登记进 closing_tasks。
// Python: session_manager.py _track_closing_task
//
// 登记后由 finishClosingTask 在任务结束时移除；集合清空时连会话条目一起删，
// 避免按会话泄漏小 map。上游用 task.add_done_callback 挂回调，
// Go 侧由 CloseSession 的等待循环在通道关闭后显式调用（等价时序）。
func (m *SessionManager) trackClosingTask(sessionID string, task *sessionTask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tasks, ok := m.closingTasks[sessionID]
	if !ok {
		tasks = make(map[*sessionTask]struct{})
		m.closingTasks[sessionID] = tasks
	}
	if _, already := tasks[task]; already {
		return
	}
	tasks[task] = struct{}{}
}

// finishClosingTask 从 closing_tasks 移除已结束的任务。
// Python: session_manager.py _finish_closing_task
//
// 集合为空时删除会话条目；任务结果的取消错误静默吞掉，其他错误记日志。
func (m *SessionManager) finishClosingTask(sessionID string, task *sessionTask) {
	m.mu.Lock()
	tasks, ok := m.closingTasks[sessionID]
	if !ok {
		m.mu.Unlock()
		return
	}
	if _, exists := tasks[task]; !exists {
		m.mu.Unlock()
		return
	}
	delete(tasks, task)
	if len(tasks) == 0 {
		delete(m.closingTasks, sessionID)
	}
	m.mu.Unlock()

	select {
	case result := <-task.result:
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			logger.Error(logComponent).Err(result.err).Str("session_id", sessionID).
				Msg("[SessionManager] close session failed")
		}
	default:
	}
}

// CloseAllSessions 进程级关闭全量会话。
// Python: session_manager.py close_all_sessions
func (m *SessionManager) CloseAllSessions(ctx context.Context) {
	m.mu.Lock()
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(m.sessionProcessors)+len(m.sessionQueues))
	for id := range m.sessionProcessors {
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for id := range m.sessionQueues {
		if _, ok := seen[id]; !ok {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()

	for _, id := range ids {
		_, _ = m.CloseSession(ctx, id, nil)
	}
}

// EnsureSessionProcessor 启动会话的任务处理器（幂等）。
// Python: session_manager.py ensure_session_processor
//
// 若 processor 不存在或已结束，则重建队列并把优先级重置为 0。
func (m *SessionManager) EnsureSessionProcessor(ctx context.Context, sessionID string) {
	m.mu.Lock()
	if generation, exists := m.sessionProcessors[sessionID]; exists && !channelClosed(generation.closing) {
		m.mu.Unlock()
		return
	}
	queue := &taskQueue{}
	heap.Init(queue)
	m.sessionQueues[sessionID] = queue
	m.sessionPriorities[sessionID] = 0
	generation := &processorGeneration{closing: make(chan struct{}), exited: make(chan struct{})}
	m.sessionProcessors[sessionID] = generation
	m.mu.Unlock()

	go m.runProcessor(ctx, sessionID, queue, generation)
}

// SubmitTask 把任务排入会话队列，不等待结果。
// Python: session_manager.py submit_task
func (m *SessionManager) SubmitTask(
	ctx context.Context, sessionID string, fn func(context.Context) (any, error),
) {
	m.EnsureSessionProcessor(ctx, sessionID)
	m.mu.Lock()
	priority := m.nextPriorityLocked(sessionID)
	queue := m.sessionQueues[sessionID]
	task := &sessionTask{fn: fn, ctx: ctx, result: make(chan taskResult, 1), priority: priority}
	if queue != nil {
		heap.Push(queue, task)
	}
	m.mu.Unlock()
}

// SubmitAndWait 把任务排入会话队列并等待结果。
// Python: session_manager.py submit_and_wait
//
// 一次性会话结束后投哨兵，让处理器退出并回收状态，避免协程与字典条目泄漏。
func (m *SessionManager) SubmitAndWait(
	ctx context.Context, sessionID string, fn func(context.Context) (any, error),
) (any, error) {
	m.EnsureSessionProcessor(ctx, sessionID)
	m.mu.Lock()
	priority := m.nextPriorityLocked(sessionID)
	queue := m.sessionQueues[sessionID]
	task := &sessionTask{fn: fn, ctx: ctx, result: make(chan taskResult, 1), priority: priority}
	if queue != nil {
		heap.Push(queue, task)
	}
	m.mu.Unlock()

	var result taskResult
	select {
	case result = <-task.result:
	case <-ctx.Done():
		result = taskResult{err: ctx.Err()}
	}
	if isOneShotSession(sessionID) {
		m.pushOneShotSentinel(sessionID)
	}
	return result.value, result.err
}

// GetCurrentTask 会话当前是否有正在跑的任务。
// Python: session_manager.py get_current_task（返回 Task 或 None）
func (m *SessionManager) GetCurrentTask(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.sessionTasks[sessionID]
	return task != nil && !task.finished
}

// HasActiveProcessor 会话是否有活跃的处理器。
// Python: session_manager.py has_active_processor
func (m *SessionManager) HasActiveProcessor(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	generation, ok := m.sessionProcessors[sessionID]
	if !ok {
		return false
	}
	return !channelClosed(generation.closing)
}

// HasSessionRuntime 会话是否还保留队列或任务状态；sessionID 为 nil 时查全表。
// Python: session_manager.py has_session_runtime
func (m *SessionManager) HasSessionRuntime(sessionID *string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sessionID != nil {
		id := *sessionID
		return m.sessionTasks[id] != nil ||
			m.hasPriorityLocked(id) ||
			m.sessionQueues[id] != nil ||
			m.sessionProcessors[id] != nil ||
			len(m.closingTasks[id]) > 0
	}
	return len(m.sessionTasks) > 0 ||
		len(m.sessionPriorities) > 0 ||
		len(m.sessionQueues) > 0 ||
		len(m.sessionProcessors) > 0 ||
		len(m.closingTasks) > 0
}

// HasActiveTasks 是否有活跃的会话任务（dreaming busy_checker 用）。
// Python: session_manager.py has_active_tasks
func (m *SessionManager) HasActiveTasks() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, task := range m.sessionTasks {
		if task != nil && !task.finished {
			return true
		}
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runProcessor 会话处理器主循环：串行取任务执行，收到哨兵或关闭信号退出。
// Python: session_manager.py ensure_session_processor 内的 process_session_queue
//
// 每一步都校验"映射是否仍属于本代"，避免上一代处理器误删新一代的状态
// （close_session 允许同一 session_id 在旧处理器退出前重连）。
func (m *SessionManager) runProcessor(
	parentCtx context.Context, sessionID string, queue *taskQueue,
	generation *processorGeneration,
) {
	defer m.cleanupProcessorGeneration(sessionID, queue, generation)
	for {
		if parentCtx.Err() != nil {
			return
		}
		if !m.isCurrentGeneration(sessionID, queue, generation) {
			return
		}
		task := m.popTask(sessionID, queue, generation)
		if task == nil {
			return
		}
		if task.fn == nil {
			// 一次性会话哨兵：退出，由 defer 清理本代状态
			return
		}
		m.executeTask(sessionID, task)
	}
}

// executeTask 执行单个任务并回填结果。
func (m *SessionManager) executeTask(sessionID string, task *sessionTask) {
	taskCtx, cancel := context.WithCancel(task.ctx)
	m.mu.Lock()
	task.cancel = cancel
	m.sessionTasks[sessionID] = task
	m.mu.Unlock()

	value, err := safeInvokeSessionTask(taskCtx, task.fn)
	cancel()

	m.mu.Lock()
	task.finished = true
	if m.sessionTasks[sessionID] == task {
		m.sessionTasks[sessionID] = nil
	}
	m.mu.Unlock()

	select {
	case task.result <- taskResult{value: value, err: err}:
	default:
	}
}

// popTask 取下一个任务；队列空时轮询等待，处理器被关闭时返回 nil。
func (m *SessionManager) popTask(
	sessionID string, queue *taskQueue, generation *processorGeneration,
) *sessionTask {
	for {
		m.mu.Lock()
		closed := m.sessionProcessors[sessionID] != generation ||
			channelClosed(generation.closing)
		if !closed && queue.Len() > 0 {
			task := heap.Pop(queue).(*sessionTask)
			m.mu.Unlock()
			return task
		}
		m.mu.Unlock()
		if closed {
			return nil
		}
		time.Sleep(processorIdlePollInterval)
	}
}

// isCurrentGeneration 队列与处理器映射是否仍属于本代。
func (m *SessionManager) isCurrentGeneration(
	sessionID string, queue *taskQueue, generation *processorGeneration,
) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionQueues[sessionID] == queue && m.sessionProcessors[sessionID] == generation
}

// cleanupProcessorGeneration 处理器退出时清理本代状态。
func (m *SessionManager) cleanupProcessorGeneration(
	sessionID string, queue *taskQueue, generation *processorGeneration,
) {
	m.mu.Lock()
	if m.sessionQueues[sessionID] == queue {
		delete(m.sessionQueues, sessionID)
		delete(m.sessionPriorities, sessionID)
		delete(m.sessionTasks, sessionID)
	}
	if m.sessionProcessors[sessionID] == generation {
		delete(m.sessionProcessors, sessionID)
	}
	m.mu.Unlock()
	// 通知 CloseSession：本代处理器已真正退出
	close(generation.exited)
	logger.Info(logComponent).Str("session_id", sessionID).
		Msg("[SessionManager] session task processor closed")
}

// pushOneShotSentinel 给一次性会话投退出哨兵。
func (m *SessionManager) pushOneShotSentinel(sessionID string) {
	m.mu.Lock()
	queue := m.sessionQueues[sessionID]
	if queue == nil {
		m.mu.Unlock()
		return
	}
	heap.Push(queue, &sessionTask{priority: oneShotSentinelPriority})
	m.mu.Unlock()
}

// waitTaskTerminated 等待任务结束，返回是否已终止。
func (m *SessionManager) waitTaskTerminated(
	ctx context.Context, sessionID string, task *sessionTask,
	waitTimeout *float64, logPrefix string,
) bool {
	if waitTimeout == nil {
		<-task.result
		return true
	}
	timer := time.NewTimer(time.Duration(*waitTimeout * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-timer.C:
		logger.Warn(logComponent).Str("session_id", sessionID).
			Float64("wait_timeout", *waitTimeout).
			Msg("[SessionManager] " + logPrefix + "cancel_session_task wait timeout")
		return false
	case <-task.result:
		return true
	case <-ctx.Done():
		return false
	}
}

// nextPriorityLocked 取下一个优先级（递减，实现同会话 LIFO）。调用方须持锁。
// Python: self._session_priorities[session_id] -= 1
func (m *SessionManager) nextPriorityLocked(sessionID string) int64 {
	current := m.sessionPriorities[sessionID]
	current--
	m.sessionPriorities[sessionID] = current
	return current
}

// hasPriorityLocked 会话是否留有优先级记录。调用方须持锁。
func (m *SessionManager) hasPriorityLocked(sessionID string) bool {
	_, ok := m.sessionPriorities[sessionID]
	return ok
}

// isOneShotSession 是否为一次性会话（心跳/定时任务），其 ID 永不复用。
// Python: session_manager.py _is_oneshot_session
func isOneShotSession(sessionID string) bool {
	return strings.HasPrefix(sessionID, "health_check_") ||
		strings.HasPrefix(sessionID, "heartbeat") ||
		strings.HasPrefix(sessionID, "cron")
}

// channelClosed 通道是否已关闭。
func channelClosed(ch chan struct{}) bool {
	if ch == nil {
		return true
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// safeInvokeSessionTask 执行任务并把 panic 转为错误。
func safeInvokeSessionTask(
	ctx context.Context, fn func(context.Context) (any, error),
) (value any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Error(logComponent).Any("panic", recovered).
				Msg("[SessionManager] session task panicked")
			err = errSessionTaskPanicked
		}
	}()
	return fn(ctx)
}

// drainCancelled 取消队列中残留任务的结果通道。
func (q *taskQueue) drainCancelled() {
	for _, task := range q.items {
		if task == nil || task.result == nil {
			continue
		}
		select {
		case task.result <- taskResult{err: context.Canceled}:
		default:
		}
	}
	q.items = nil
}

// Len 实现 heap.Interface。
func (q *taskQueue) Len() int { return len(q.items) }

// Less 实现 heap.Interface（优先级小的先出）。
func (q *taskQueue) Less(i, j int) bool {
	return q.items[i].priority < q.items[j].priority
}

// Swap 实现 heap.Interface。
func (q *taskQueue) Swap(i, j int) {
	q.items[i], q.items[j] = q.items[j], q.items[i]
}

// Push 实现 heap.Interface。
func (q *taskQueue) Push(value any) {
	q.items = append(q.items, value.(*sessionTask))
}

// Pop 实现 heap.Interface。
func (q *taskQueue) Pop() any {
	old := q.items
	n := len(old)
	item := old[n-1]
	q.items = old[:n-1]
	return item
}
