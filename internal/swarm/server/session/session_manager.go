package session

import (
	"container/heap"
	"context"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionManager 会话任务队列管理器，提供按 session 序列化执行、优先级排序和取消能力。
//
// 对齐 Python: jiuwenswarm/server/runtime/session/session_manager.py
type SessionManager struct {
	// mu 保护以下所有 map 的并发访问
	mu sync.Mutex
	// sessionTasks session→当前执行任务的 cancel 函数
	sessionTasks map[string]context.CancelFunc
	// sessionPriorities session→优先级计数器（从 0 递减，LIFO 语义）
	sessionPriorities map[string]int
	// sessionQueues session→优先级堆
	sessionQueues map[string]*priorityHeap
	// sessionProcessors session→消费者 goroutine 的 cancel 函数
	sessionProcessors map[string]context.CancelFunc
	// sessionSignals session→通知消费者有新任务的信号 channel
	sessionSignals map[string]chan struct{}
}

// priorityItem 优先级队列项。
type priorityItem struct {
	// priority 优先级（数值越小越先出队）
	priority int
	// task 任务函数
	task func(context.Context) (any, error)
}

// taskResult 任务执行结果。
type taskResult struct {
	// value 任务返回值
	value any
	// err 任务返回错误
	err error
}

// ──────────────────────────── 枚举 ────────────────────────────

// priorityHeap 基于 container/heap 的优先级队列（数值越小越先出队）。
type priorityHeap []*priorityItem

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionManager 创建新的 SessionManager 实例。
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessionTasks:      make(map[string]context.CancelFunc),
		sessionPriorities: make(map[string]int),
		sessionQueues:     make(map[string]*priorityHeap),
		sessionProcessors: make(map[string]context.CancelFunc),
		sessionSignals:    make(map[string]chan struct{}),
	}
}

// GetSessionID 获取 session_id，空串返回 "default"。
//
// 对齐 Python: SessionManager.get_session_id(session_id)
func GetSessionID(sessionID string) string {
	return NormalizeSessionID(sessionID)
}

// CancelSessionTask 取消指定 session 的非流式任务。
//
// 对齐 Python: SessionManager.cancel_session_task(session_id, log_msg_prefix, wait_timeout)
func (sm *SessionManager) CancelSessionTask(ctx context.Context, sessionID string, logPrefix string, waitTimeout *time.Duration) error {
	sm.mu.Lock()
	cancelFn, ok := sm.sessionTasks[sessionID]
	sm.mu.Unlock()

	if !ok || cancelFn == nil {
		return nil
	}

	logger.Info(logComponent).Str("session_id", sessionID).Str("prefix", logPrefix).Msg("取消 session 非流式任务")
	cancelFn()

	// 如果有等待超时，等待任务完成
	if waitTimeout != nil {
		select {
		case <-time.After(*waitTimeout):
			logger.Warn(logComponent).Str("session_id", sessionID).Dur("wait_timeout", *waitTimeout).Msg("cancel_session_task 等待超时")
		case <-ctx.Done():
		}
	}

	sm.mu.Lock()
	sm.sessionTasks[sessionID] = nil
	sm.mu.Unlock()

	logger.Info(logComponent).Str("session_id", sessionID).Str("prefix", logPrefix).Msg("session 任务已终止")
	return nil
}

// CancelAllSessionTasks 取消所有 session 的非流式任务。
//
// 对齐 Python: SessionManager.cancel_all_session_tasks(log_msg_prefix)
func (sm *SessionManager) CancelAllSessionTasks(ctx context.Context, logPrefix string) error {
	sm.mu.Lock()
	sessionIDs := make([]string, 0, len(sm.sessionTasks))
	for id := range sm.sessionTasks {
		sessionIDs = append(sessionIDs, id)
	}
	sm.mu.Unlock()

	for _, id := range sessionIDs {
		_ = sm.CancelSessionTask(ctx, id, logPrefix, nil)
	}
	return nil
}

// EnsureSessionProcessor 确保 session 的任务处理器在运行。
//
// 对齐 Python: SessionManager.ensure_session_processor(session_id)
func (sm *SessionManager) EnsureSessionProcessor(_ context.Context, sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, procOk := sm.sessionProcessors[sessionID]; procOk {
		if sigCh, sigOk := sm.sessionSignals[sessionID]; sigOk && sigCh != nil {
			return nil
		}
	}

	h := &priorityHeap{}
	heap.Init(h)
	sm.sessionQueues[sessionID] = h
	sm.sessionPriorities[sessionID] = 0

	sigCh := make(chan struct{}, 1)
	sm.sessionSignals[sessionID] = sigCh

	procCtx, procCancel := context.WithCancel(context.Background())
	sm.sessionProcessors[sessionID] = procCancel

	go sm.processSessionQueue(procCtx, sessionID, sigCh)

	return nil
}

// SubmitTask 提交任务到 session 队列（异步）。
//
// 对齐 Python: SessionManager.submit_task(session_id, task_func)
func (sm *SessionManager) SubmitTask(ctx context.Context, sessionID string, taskFunc func(context.Context) (any, error)) error {
	if err := sm.EnsureSessionProcessor(ctx, sessionID); err != nil {
		return err
	}

	sm.mu.Lock()
	sm.sessionPriorities[sessionID]--
	priority := sm.sessionPriorities[sessionID]
	heap.Push(sm.sessionQueues[sessionID], &priorityItem{priority: priority, task: taskFunc})
	sigCh := sm.sessionSignals[sessionID]
	sm.mu.Unlock()

	select {
	case sigCh <- struct{}{}:
	default:
	}

	return nil
}

// SubmitAndWait 提交任务到 session 队列并等待结果。
//
// 对齐 Python: SessionManager.submit_and_wait(session_id, task_func)
func (sm *SessionManager) SubmitAndWait(ctx context.Context, sessionID string, taskFunc func(context.Context) (any, error)) (any, error) {
	if err := sm.EnsureSessionProcessor(ctx, sessionID); err != nil {
		return nil, err
	}

	resultCh := make(chan taskResult, 1)

	wrappedTask := func(taskCtx context.Context) (any, error) {
		result, err := taskFunc(taskCtx)
		resultCh <- taskResult{value: result, err: err}
		return result, err
	}

	sm.mu.Lock()
	sm.sessionPriorities[sessionID]--
	priority := sm.sessionPriorities[sessionID]
	heap.Push(sm.sessionQueues[sessionID], &priorityItem{priority: priority, task: wrappedTask})
	sigCh := sm.sessionSignals[sessionID]
	sm.mu.Unlock()

	select {
	case sigCh <- struct{}{}:
	default:
	}

	select {
	case r := <-resultCh:
		return r.value, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// GetCurrentTask 获取当前 session 正在执行的任务 cancel 函数。
func (sm *SessionManager) GetCurrentTask(sessionID string) context.CancelFunc {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.sessionTasks[sessionID]
}

// HasActiveProcessor 检查 session 是否有活跃的处理器。
func (sm *SessionManager) HasActiveProcessor(sessionID string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	_, ok := sm.sessionProcessors[sessionID]
	return ok
}

// HasActiveTasks 检查是否有活跃的 session 任务。
func (sm *SessionManager) HasActiveTasks() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, cancelFn := range sm.sessionTasks {
		if cancelFn != nil {
			return true
		}
	}
	return false
}

// Len 实现 heap.Interface。
func (h priorityHeap) Len() int { return len(h) }

// Less 实现 heap.Interface，数值越小优先级越高。
func (h priorityHeap) Less(i, j int) bool { return h[i].priority < h[j].priority }

// Swap 实现 heap.Interface。
func (h priorityHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

// Push 实现 heap.Interface。
func (h *priorityHeap) Push(x any) {
	*h = append(*h, x.(*priorityItem))
}

// Pop 实现 heap.Interface。
func (h *priorityHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return item
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// processSessionQueue 处理 session 任务队列（先进后出执行，新任务优先）。
func (sm *SessionManager) processSessionQueue(ctx context.Context, sessionID string, sigCh chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			logger.Info(logComponent).Str("session_id", sessionID).Msg("Session 任务处理器被取消")
			sm.cleanupSession(sessionID)
			return
		case <-sigCh:
		}

		sm.mu.Lock()
		h, ok := sm.sessionQueues[sessionID]
		if !ok || h.Len() == 0 {
			sm.mu.Unlock()
			continue
		}
		item := heap.Pop(h).(*priorityItem)
		sm.mu.Unlock()

		if item.task == nil {
			sm.cleanupSession(sessionID)
			return
		}

		taskCtx, taskCancel := context.WithCancel(ctx)
		sm.mu.Lock()
		sm.sessionTasks[sessionID] = taskCancel
		sm.mu.Unlock()

		_, _ = item.task(taskCtx)

		sm.mu.Lock()
		sm.sessionTasks[sessionID] = nil
		sm.mu.Unlock()
		taskCancel()
	}
}

// cleanupSession 清理 session 相关的所有 map 条目。
func (sm *SessionManager) cleanupSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessionQueues, sessionID)
	delete(sm.sessionPriorities, sessionID)
	delete(sm.sessionTasks, sessionID)
	delete(sm.sessionProcessors, sessionID)
	delete(sm.sessionSignals, sessionID)

	logger.Info(logComponent).Str("session_id", sessionID).Msg("Session 任务处理器已关闭")
}
