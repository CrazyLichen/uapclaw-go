package session

import (
	"container/heap"
	"context"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// processorEntry 处理器条目，同时存储 context 和 cancel。
// 用于 EnsureSessionProcessor 检测处理器是否已死（对齐 Python: task.done()），
// 以及 HasActiveTasks 准确判断任务是否仍在执行。
type processorEntry struct {
	// ctx 处理器 context，通过 select <-ctx.Done() 非阻塞探测存活状态
	ctx context.Context
	// cancel 处理器 cancel 函数
	cancel context.CancelFunc
}

// SessionManager 会话任务队列管理器，提供按 session 序列化执行、优先级排序和取消能力。
//
// Python: jiuwenswarm/server/runtime/session/session_manager.py
type SessionManager struct {
	// mu 保护以下所有 map 的并发访问
	mu sync.Mutex
	// sessionTasks session→当前执行任务的 cancel 函数
	sessionTasks map[string]context.CancelFunc
	// sessionTaskCtxs session→当前执行任务的 context（用于 HasActiveTasks 探测存活）
	sessionTaskCtxs map[string]context.Context
	// sessionPriorities session→优先级计数器（从 0 递减，LIFO 语义）
	sessionPriorities map[string]int
	// sessionQueues session→优先级堆
	sessionQueues map[string]*priorityHeap
	// sessionProcessors session→消费者 goroutine 的处理器条目（含 ctx 和 cancel）
	sessionProcessors map[string]*processorEntry
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
		sessionTaskCtxs:   make(map[string]context.Context),
		sessionPriorities: make(map[string]int),
		sessionQueues:     make(map[string]*priorityHeap),
		sessionProcessors: make(map[string]*processorEntry),
		sessionSignals:    make(map[string]chan struct{}),
	}
}

// GetSessionID 获取 session_id，空串返回 "default"。
//
// Python: SessionManager.get_session_id(session_id)
func GetSessionID(sessionID string) string {
	return NormalizeSessionID(sessionID)
}

// CancelSessionTask 取消指定 session 的非流式任务。
//
// Python: SessionManager.cancel_session_task(session_id, log_msg_prefix, wait_timeout)
func (sm *SessionManager) CancelSessionTask(ctx context.Context, sessionID string, logPrefix string, waitTimeout *time.Duration) error {
	sm.mu.Lock()
	cancelFn, ok := sm.sessionTasks[sessionID]
	sm.mu.Unlock()

	if !ok || cancelFn == nil {
		return nil
	}

	logger.Info(logComponent).Str("session_id", sessionID).Str("prefix", logPrefix).Msg("取消 session 非流式任务")
	cancelFn()

	// Python: if wait_timeout is None: await task（无限期等待任务完成）
	// Go: 等待 sessionTasks[sessionID] 变为 nil（processSessionQueue 在任务完成后设 nil）
	if waitTimeout != nil {
		// 有超时的等待
		deadline := time.After(*waitTimeout)
		sm.waitTaskDone(sessionID, deadline, ctx.Done())
	} else {
		// 无超时，无限期等待任务完成（对齐 Python: await task）
		sm.waitTaskDone(sessionID, nil, ctx.Done())
	}

	sm.mu.Lock()
	sm.sessionTasks[sessionID] = nil
	sm.mu.Unlock()

	logger.Info(logComponent).Str("session_id", sessionID).Str("prefix", logPrefix).Msg("session 任务已终止")
	return nil
}

// CancelAllSessionTasks 取消所有 session 的非流式任务。
//
// Python: SessionManager.cancel_all_session_tasks(log_msg_prefix)
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
// 如果处理器 context 已取消（等价 Python: task.done()），则重建队列和优先级。
//
// Python: SessionManager.ensure_session_processor(session_id)
func (sm *SessionManager) EnsureSessionProcessor(_ context.Context, sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 检查处理器是否存活（对齐 Python: if not processor.done()）
	if entry, procOk := sm.sessionProcessors[sessionID]; procOk {
		if sigCh, sigOk := sm.sessionSignals[sessionID]; sigOk && sigCh != nil {
			// 非阻塞探测 context 是否已取消
			select {
			case <-entry.ctx.Done():
				// 处理器已死，需要重建队列和优先级（对齐 Python）
				logger.Info(logComponent).Str("session_id", sessionID).Msg("Session 处理器已停止，重建队列和优先级")
			default:
				// 处理器仍然存活
				return nil
			}
		}
	}

	h := &priorityHeap{}
	heap.Init(h)
	sm.sessionQueues[sessionID] = h
	sm.sessionPriorities[sessionID] = 0

	sigCh := make(chan struct{}, 1)
	sm.sessionSignals[sessionID] = sigCh

	procCtx, procCancel := context.WithCancel(context.Background())
	sm.sessionProcessors[sessionID] = &processorEntry{ctx: procCtx, cancel: procCancel}

	go sm.processSessionQueue(procCtx, sessionID, sigCh)

	return nil
}

// SubmitTask 提交任务到 session 队列（异步）。
//
// Python: SessionManager.submit_task(session_id, task_func)
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
// Python: SessionManager.submit_and_wait(session_id, task_func)
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
	entry, ok := sm.sessionProcessors[sessionID]
	if !ok {
		return false
	}
	// 非阻塞探测处理器是否已停止
	select {
	case <-entry.ctx.Done():
		return false
	default:
		return true
	}
}

// HasActiveTasks 检查是否有活跃的 session 任务。
// 通过探测任务 context 是否已取消来判断（对齐 Python: not task.done()）。
func (sm *SessionManager) HasActiveTasks() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for sessionID, taskCtx := range sm.sessionTaskCtxs {
		if taskCtx != nil {
			select {
			case <-taskCtx.Done():
				// 任务 context 已取消，不算活跃
				continue
			default:
				// 任务仍在执行
				logger.Debug(logComponent).Str("session_id", sessionID).Msg("HasActiveTasks: 检测到活跃任务")
				return true
			}
		}
	}
	return false
}

// ──────────────────────────── 导出函数 ────────────────────────────

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
		sm.sessionTaskCtxs[sessionID] = taskCtx
		sm.mu.Unlock()

		// M-12: 完整对齐 Python 的 task 错误处理
		// Python: except Exception as e: logger.error(...) + handle_task_execution_failure
		result, err := item.task(taskCtx)
		if err != nil {
			// Python: logger.error(f"Task {task_id} execution failed: {e}", exc_info=True)
			logger.Error(logComponent).Str("session_id", sessionID).Err(err).Msg("Session 任务执行失败")
			// Python: _handle_task_execution_failure → 更新状态为 FAILED + 发布 TASK_FAILED 事件
			sm.handleTaskFailure(sessionID, err)
		}

		sm.mu.Lock()
		sm.sessionTasks[sessionID] = nil
		delete(sm.sessionTaskCtxs, sessionID)
		sm.mu.Unlock()
		taskCancel()
		// 避免未使用变量警告
		_ = result
	}
}

// handleTaskFailure 处理任务执行失败（对齐 Python: _handle_task_execution_failure）。
// 记录失败日志并发布 TASK_FAILED 事件。
func (sm *SessionManager) handleTaskFailure(sessionID string, taskErr error) {
	// Python: update_task_status(task_id, TaskStatus.FAILED, error_message=error_message)
	// Python: publish ControllerOutputChunk(type=TASK_FAILED, data=[TextDataFrame(text=error_message)])
	// Go 的 SessionManager 是轻量级队列管理器，没有 task status 跟踪和事件发布机制，
	// 因此这里仅记录日志。上游（TaskScheduler / AgentServer）负责状态更新和事件发布。
	logger.Warn(logComponent).
		Str("session_id", sessionID).
		Str("event_type", "TASK_FAILED").
		Err(taskErr).
		Msg("Session 任务失败，需上游处理状态更新和事件发布")
}

// cleanupSession 清理 session 相关的所有 map 条目。
func (sm *SessionManager) cleanupSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessionQueues, sessionID)
	delete(sm.sessionPriorities, sessionID)
	delete(sm.sessionTasks, sessionID)
	delete(sm.sessionTaskCtxs, sessionID)
	delete(sm.sessionProcessors, sessionID)
	delete(sm.sessionSignals, sessionID)

	logger.Info(logComponent).Str("session_id", sessionID).Msg("Session 任务处理器已关闭")
}

// waitTaskDone 等待 session 任务完成（sessionTasks[sessionID] 变为 nil）。
// deadline 为 nil 时无限期等待，对齐 Python: await task。
func (sm *SessionManager) waitTaskDone(sessionID string, deadline <-chan time.Time, ctxDone <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		sm.mu.Lock()
		done := sm.sessionTasks[sessionID] == nil
		sm.mu.Unlock()
		if done {
			return
		}
		select {
		case <-ticker.C:
			// 继续轮询
		case <-deadline:
			logger.Warn(logComponent).Str("session_id", sessionID).Msg("cancel_session_task 等待超时")
			return
		case <-ctxDone:
			return
		}
	}
}
