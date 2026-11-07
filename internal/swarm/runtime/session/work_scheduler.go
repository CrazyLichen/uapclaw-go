package session

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// workResult 排队工作的执行结果。
type workResult struct {
	// value 返回值
	value any
	// err 错误
	err error
}

// queuedWork 一次排队中的工作。
// Python: work_scheduler.py _QueuedWork
type queuedWork struct {
	// handle 对应执行句柄
	handle *SessionExecutionHandle
	// operation 待执行的闭包（接收调度器建的 ctx）
	operation func(context.Context) (any, error)
	// result 结果通道（等价 asyncio.Future）
	result chan workResult
	// resultOnce 保证结果只投递一次
	resultOnce sync.Once
	// cancelled 是否已取消（processor 取出后据此跳过执行）
	cancelled bool
	// cancelledMu 保护 cancelled
	cancelledMu sync.Mutex
}

// deliver 投递结果（幂等）。
func (q *queuedWork) deliver(r workResult) {
	q.resultOnce.Do(func() {
		q.result <- r
		close(q.result)
	})
}

// markCancelled 标记取消。
func (q *queuedWork) markCancelled() {
	q.cancelledMu.Lock()
	q.cancelled = true
	q.cancelledMu.Unlock()
}

// isCancelled 是否已取消。
func (q *queuedWork) isCancelled() bool {
	q.cancelledMu.Lock()
	defer q.cancelledMu.Unlock()
	return q.cancelled
}

// lane 每会话串行执行 lane。
// Python: work_scheduler.py _Lane
type lane struct {
	// sessionID 会话标识
	sessionID string
	// generation 代次
	generation int
	// priority 当前优先级计数（latest_first 类递减）
	priority int
	// sequence 提交序号
	sequence int
	// processor lane 消费者 goroutine 完成信号
	processor chan struct{}
	// current 正在执行的项
	current *queuedWork
	// currentTask 正在执行项的完成信号
	currentTask chan struct{}
	// queue 优先队列
	queue *priorityQueue
}

// priorityItem 优先队列项。
// Python: asyncio.PriorityQueue 的 (group, priority, sequence) 三元组
type priorityItem struct {
	// group 组别（0=latest_first 类，1=普通类）
	group int
	// priority 组内优先级
	priority int
	// sequence 提交序号（同组同优先级按 FIFO）
	sequence int
	// work 排队工作
	work *queuedWork
}

// priorityQueue heap.Interface 实现。
type priorityQueue []*priorityItem

// SessionWorkScheduler 每会话一次一项、跨会话并行。
// Python: work_scheduler.py SessionWorkScheduler
//
// 与上游差异：Python 用 asyncio.PriorityQueue 阻塞式 get，Go 无同构原语，
// 用 sync.Cond 做阻塞唤醒（不用轮询，避免时序抖动）。
type SessionWorkScheduler struct {
	// mu 保护以下全部字段（同时作为 cond 的锁）
	mu sync.Mutex
	// cond 队列就绪条件变量
	cond *sync.Cond
	// lanes 会话标识 → lane
	lanes map[string]*lane
	// queuedIndex 会话标识 → (执行标识 → 排队项)
	queuedIndex map[string]map[string]*queuedWork
	// closing 会话标识 → 正在关闭的完成信号集合
	closing map[string]map[chan struct{}]struct{}
	// closed 调度器是否已关闭
	closed bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Len 队列长度。
func (q priorityQueue) Len() int { return len(q) }

// Less 三元组比较：group → priority → sequence，均升序。
// Python: asyncio.PriorityQueue 对元组做字典序比较
func (q priorityQueue) Less(i, j int) bool {
	if q[i].group != q[j].group {
		return q[i].group < q[j].group
	}
	if q[i].priority != q[j].priority {
		return q[i].priority < q[j].priority
	}
	return q[i].sequence < q[j].sequence
}

// Swap 交换两项。
func (q priorityQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

// Push 入队。
func (q *priorityQueue) Push(x any) { *q = append(*q, x.(*priorityItem)) }

// Pop 出队。
func (q *priorityQueue) Pop() any {
	old := *q
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*q = old[:n-1]
	return item
}

// NewSessionWorkScheduler 创建调度器。
// Python: SessionWorkScheduler.__init__
func NewSessionWorkScheduler() *SessionWorkScheduler {
	s := &SessionWorkScheduler{
		lanes:       map[string]*lane{},
		queuedIndex: map[string]map[string]*queuedWork{},
		closing:     map[string]map[chan struct{}]struct{}{},
	}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// SubmitAndWait 提交并等待一次执行完成。
// Python: SessionWorkScheduler.submit_and_wait
// 优先级：latest_first 类 → group=0、priority 递减（后到先跑）；
// 其余 → group=1、priority=0、按 sequence FIFO。
func (s *SessionWorkScheduler) SubmitAndWait(
	ctx context.Context,
	h *SessionExecutionHandle,
	operation func(context.Context) (any, error),
) (any, error) {
	item := &queuedWork{
		handle:    h,
		operation: operation,
		result:    make(chan workResult, 1),
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fmt.Errorf("session scheduler is closed")
	}
	ln, err := s.ensureLaneLocked(h.SessionID, h.Generation)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	ln.sequence++
	var group, priority int
	if h.WorkKind.LatestFirst() {
		ln.priority--
		group, priority = 0, ln.priority
	} else {
		group, priority = 1, 0
	}
	heap.Push(ln.queue, &priorityItem{
		group: group, priority: priority, sequence: ln.sequence, work: item,
	})
	if s.queuedIndex[h.SessionID] == nil {
		s.queuedIndex[h.SessionID] = map[string]*queuedWork{}
	}
	s.queuedIndex[h.SessionID][h.ExecutionID] = item
	s.cond.Broadcast()
	s.mu.Unlock()

	select {
	case res, ok := <-item.result:
		if !ok {
			return nil, context.Canceled
		}
		return res.value, res.err
	case <-ctx.Done():
		// 提交方放弃等待：把排队项标记取消，processor 取出后跳过
		s.mu.Lock()
		if idx := s.queuedIndex[h.SessionID]; idx != nil {
			if stillQueued, ok := idx[h.ExecutionID]; ok && stillQueued == item {
				item.markCancelled()
			}
		}
		s.mu.Unlock()
		return nil, ctx.Err()
	}
}

// CancelHandles 取消一批执行：运行中的发 ctx 取消，排队中的直接摘掉。
// Python: SessionWorkScheduler.cancel_handles
// 返回超时未停的执行标识。
func (s *SessionWorkScheduler) CancelHandles(
	_ context.Context,
	handles []*SessionExecutionHandle,
	waitTimeout time.Duration,
) []string {
	ids := make(map[string]struct{}, len(handles))
	for _, h := range handles {
		ids[h.ExecutionID] = struct{}{}
	}

	s.mu.Lock()
	waitTargets := map[string]chan struct{}{}
	cancels := map[string]context.CancelFunc{}
	for _, ln := range s.lanes {
		if ln.current == nil || ln.currentTask == nil {
			continue
		}
		if _, want := ids[ln.current.handle.ExecutionID]; !want {
			continue
		}
		waitTargets[ln.current.handle.ExecutionID] = ln.currentTask
		if ln.current.handle.Cancel != nil {
			cancels[ln.current.handle.ExecutionID] = ln.current.handle.Cancel
		}
	}
	var queuedToCancel []*queuedWork
	seen := map[string]struct{}{}
	for _, h := range handles {
		if _, waiting := waitTargets[h.ExecutionID]; waiting {
			continue
		}
		idx := s.queuedIndex[h.SessionID]
		if idx == nil {
			continue
		}
		item, ok := idx[h.ExecutionID]
		if !ok {
			continue
		}
		if _, dup := seen[h.ExecutionID]; dup {
			continue
		}
		seen[h.ExecutionID] = struct{}{}
		delete(idx, h.ExecutionID)
		queuedToCancel = append(queuedToCancel, item)
	}
	s.mu.Unlock()

	// 排队项：标记取消并立即投递取消结果
	for _, item := range queuedToCancel {
		item.markCancelled()
		item.deliver(workResult{err: context.Canceled})
	}
	// 运行中的：发 ctx 取消
	for _, cancel := range cancels {
		cancel()
	}
	if len(waitTargets) == 0 {
		return nil
	}
	var timedOut []string
	for executionID, done := range waitTargets {
		select {
		case <-done:
		case <-time.After(waitTimeout):
			timedOut = append(timedOut, executionID)
		}
	}
	return timedOut
}

// CloseSession 关闭一个会话 lane：摘掉排队项、取消当前执行。
// Python: SessionWorkScheduler.close_session
// 返回 (lane 是否存在, 超时未停的执行标识)。
func (s *SessionWorkScheduler) CloseSession(
	sessionID string,
	generation *int,
	waitTimeout time.Duration,
) (bool, []string) {
	s.mu.Lock()
	ln, ok := s.lanes[sessionID]
	if !ok || (generation != nil && ln.generation != *generation) {
		s.mu.Unlock()
		return false, nil
	}
	delete(s.lanes, sessionID)
	currentID := ""
	var currentCancel context.CancelFunc
	if ln.current != nil {
		currentID = ln.current.handle.ExecutionID
		currentCancel = ln.current.handle.Cancel
	}
	// 收集需要等待的完成信号
	targets := make(map[chan struct{}]struct{}, 2)
	if ln.processor != nil {
		targets[ln.processor] = struct{}{}
	}
	if ln.currentTask != nil {
		targets[ln.currentTask] = struct{}{}
	}
	closingSet := s.closing[sessionID]
	if closingSet == nil {
		closingSet = map[chan struct{}]struct{}{}
		s.closing[sessionID] = closingSet
	}
	for ch := range targets {
		closingSet[ch] = struct{}{}
	}
	queued := s.takeQueuedLocked(sessionID)
	s.cond.Broadcast()
	s.mu.Unlock()

	// 排空排队项
	for _, item := range queued {
		item.markCancelled()
		item.deliver(workResult{err: context.Canceled})
	}
	// 取消当前执行
	if currentCancel != nil {
		currentCancel()
	}

	var timedOut []string
	deadline := time.After(waitTimeout)
	for ch := range targets {
		select {
		case <-ch:
		case <-deadline:
			if currentID != "" {
				timedOut = append(timedOut, currentID)
			}
		}
	}

	s.mu.Lock()
	delete(s.closing, sessionID)
	s.mu.Unlock()
	return true, dedupeStrings(timedOut)
}

// Close 关闭调度器全部 lane。
// Python: SessionWorkScheduler.close
// 返回超时未停的执行标识。
func (s *SessionWorkScheduler) Close(waitTimeout time.Duration) []string {
	s.mu.Lock()
	s.closed = true
	ids := make([]string, 0, len(s.lanes))
	for id := range s.lanes {
		ids = append(ids, id)
	}
	s.cond.Broadcast()
	s.mu.Unlock()

	var timedOut []string
	for _, id := range ids {
		_, t := s.CloseSession(id, nil, waitTimeout)
		timedOut = append(timedOut, t...)
	}
	return dedupeStrings(timedOut)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureLaneLocked 取或建 lane 并启动消费者。
// Python: SessionWorkScheduler._ensure_lane
func (s *SessionWorkScheduler) ensureLaneLocked(sessionID string, generation int) (*lane, error) {
	if ln, ok := s.lanes[sessionID]; ok {
		if ln.generation != generation {
			return nil, fmt.Errorf("session generation mismatch: %s expected=%d actual=%d",
				sessionID, ln.generation, generation)
		}
		return ln, nil
	}
	q := &priorityQueue{}
	heap.Init(q)
	ln := &lane{
		sessionID:  sessionID,
		generation: generation,
		processor:  make(chan struct{}),
		queue:      q,
	}
	s.lanes[sessionID] = ln
	go s.process(sessionID, ln)
	return ln, nil
}

// process lane 消费者主循环。
// Python: SessionWorkScheduler._process
func (s *SessionWorkScheduler) process(sessionID string, ln *lane) {
	defer func() {
		s.mu.Lock()
		if s.lanes[sessionID] == ln {
			delete(s.lanes, sessionID)
		}
		queued := s.takeQueuedLocked(sessionID)
		s.mu.Unlock()
		for _, item := range queued {
			item.markCancelled()
			item.deliver(workResult{err: context.Canceled})
		}
		close(ln.processor)
	}()

	for {
		s.mu.Lock()
		for {
			if s.lanes[sessionID] != ln {
				s.mu.Unlock()
				return
			}
			if ln.queue.Len() > 0 {
				break
			}
			s.cond.Wait()
		}
		item := heap.Pop(ln.queue).(*priorityItem).work
		if idx := s.queuedIndex[sessionID]; idx != nil {
			delete(idx, item.handle.ExecutionID)
		}
		ln.current = item
		if item.isCancelled() {
			ln.current = nil
			s.mu.Unlock()
			continue
		}
		done := make(chan struct{})
		ln.currentTask = done
		opCtx, cancel := context.WithCancel(context.Background())
		item.handle.Cancel = cancel
		s.mu.Unlock()

		go func() {
			defer close(done)
			value, err := item.operation(opCtx)
			item.deliver(workResult{value: value, err: err})
			cancel()
		}()

		<-done

		s.mu.Lock()
		ln.current = nil
		ln.currentTask = nil
		s.mu.Unlock()
	}
}

// takeQueuedLocked 取走并清空某会话的全部排队项。
// Python: SessionWorkScheduler._drain_queued
func (s *SessionWorkScheduler) takeQueuedLocked(sessionID string) []*queuedWork {
	idx := s.queuedIndex[sessionID]
	delete(s.queuedIndex, sessionID)
	ln, ok := s.lanes[sessionID]
	if !ok || ln.queue == nil {
		if idx == nil {
			return nil
		}
		out := make([]*queuedWork, 0, len(idx))
		for _, item := range idx {
			out = append(out, item)
		}
		return out
	}
	seen := map[*queuedWork]struct{}{}
	out := make([]*queuedWork, 0, len(idx)+ln.queue.Len())
	for _, item := range idx {
		seen[item] = struct{}{}
		out = append(out, item)
	}
	for ln.queue.Len() > 0 {
		item := heap.Pop(ln.queue).(*priorityItem).work
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

// dedupeStrings 字符串切片去重保序。
func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
