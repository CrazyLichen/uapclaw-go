// utils 包提供通用工具函数。
//
// background.go 实现后台任务和任务管理器。
// 对应 Python：
//   - openjiuwen/core/common/background_tasks.py（轻量后台任务句柄）
//   - openjiuwen/core/common/task_manager/task.py（Task 数据模型 + 状态机）
//   - openjiuwen/core/common/task_manager/manager.py（TaskManager 单例）
//
// Go 版本使用 goroutine + context 替代 Python 的 asyncio + anyio，
// 使用 sync 替代 asyncio.Lock，使用 channel 替代 asyncio.Event。

package utils

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ──────────────────────────── 结构体 ────────────────────────────

type BackgroundTask struct {
	name   string
	group  string
	fn     func(ctx context.Context) error
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	mu     sync.Mutex
}

type Task struct {
	ID           string
	Name         string
	Group        string
	ParentID     string
	Status       TaskStatus
	Timeout      time.Duration
	Result       any
	Err          error
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Metadata     map[string]any
	CancelReason string
	CancelledBy  string

	// 内部字段
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.RWMutex
}

type TaskManager struct {
	registry map[string]*Task
	mu       sync.RWMutex
}

type TaskResult struct {
	TaskID string
	Result any
	Err    error
}

type taskConfig struct {
	name     string
	group    string
	timeout  time.Duration
	metadata map[string]any
	parentID string
}

// ──────────────────────────── 枚举 ────────────────────────────

type TaskStatus int

type TaskOption func(*taskConfig)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TaskPending 待执行。
	TaskPending TaskStatus = iota
	// TaskRunning 执行中。
	TaskRunning
	// TaskCompleted 已完成。
	TaskCompleted
	// TaskFailed 失败。
	TaskFailed
	// TaskCancelled 已取消。
	TaskCancelled
	// TaskTimeout 超时。
	TaskTimeout
)

// ──────────────────────────── 全局变量 ────────────────────────────

var taskManagerSingleton Singleton[TaskManager]

// ──────────────────────────── 导出函数 ────────────────────────────

func WithTaskName(name string) TaskOption {
	return func(c *taskConfig) { c.name = name }
}

func (s TaskStatus) IsTerminal() bool {
	return s == TaskCompleted || s == TaskFailed || s == TaskCancelled || s == TaskTimeout
}

func (s TaskStatus) String() string {
	switch s {
	case TaskPending:
		return "PENDING"
	case TaskRunning:
		return "RUNNING"
	case TaskCompleted:
		return "COMPLETED"
	case TaskFailed:
		return "FAILED"
	case TaskCancelled:
		return "CANCELLED"
	case TaskTimeout:
		return "TIMEOUT"
	default:
		return "UNKNOWN"
	}
}

func WithTaskGroup(group string) TaskOption {
	return func(c *taskConfig) { c.group = group }
}

func WithTaskTimeout(timeout time.Duration) TaskOption {
	return func(c *taskConfig) { c.timeout = timeout }
}

func WithTaskMetadata(md map[string]any) TaskOption {
	return func(c *taskConfig) { c.metadata = md }
}

func WithTaskParentID(id string) TaskOption {
	return func(c *taskConfig) { c.parentID = id }
}

func NewBackgroundTask(name, group string, fn func(ctx context.Context) error) *BackgroundTask {
	return &BackgroundTask{
		name:  name,
		group: group,
		fn:    fn,
		done:  make(chan struct{}),
	}
}

func (t *BackgroundTask) Start(ctx context.Context) {
	ctx, t.cancel = context.WithCancel(ctx)

	go func() {
		defer close(t.done)
		if err := t.fn(ctx); err != nil {
			t.mu.Lock()
			t.err = err
			t.mu.Unlock()
		}
	}()
}

func (t *BackgroundTask) Stop(timeout time.Duration) error {
	if t.cancel != nil {
		t.cancel()
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-t.done:
		return t.err
	case <-timer.C:
		return fmt.Errorf("后台任务 %q 停止超时，超时时间: %v", t.name, timeout)
	}
}

func (t *BackgroundTask) Wait() error {
	<-t.done
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

func (t *BackgroundTask) Done() <-chan struct{} {
	return t.done
}

func (t *BackgroundTask) Name() string { return t.name }

func (t *BackgroundTask) Group() string { return t.group }

func (t *Task) IsTerminal() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status.IsTerminal()
}

func (t *Task) DisplayName() string {
	if t.Name != "" {
		return t.Name
	}
	if len(t.ID) >= 8 {
		return t.ID[:8]
	}
	return t.ID
}

func (t *Task) Wait() (any, error) {
	<-t.done
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Result, t.Err
}

func GetTaskManager() *TaskManager {
	return taskManagerSingleton.Get(func() *TaskManager {
		return &TaskManager{
			registry: make(map[string]*Task),
		}
	})
}

func (m *TaskManager) CreateTask(ctx context.Context, fn func(ctx context.Context) (any, error), opts ...TaskOption) (*Task, error) {
	cfg := &taskConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	taskID := uuid.New().String()
	now := time.Now()

	task := &Task{
		ID:        taskID,
		Name:      cfg.name,
		Group:     cfg.group,
		ParentID:  cfg.parentID,
		Status:    TaskPending,
		Timeout:   cfg.timeout,
		Metadata:  cfg.metadata,
		CreatedAt: now,
		done:      make(chan struct{}),
	}

	// 注册任务
	m.mu.Lock()
	if _, exists := m.registry[taskID]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("任务 %s 已存在", taskID)
	}
	m.registry[taskID] = task
	m.mu.Unlock()

	// 启动 goroutine 执行任务
	taskCtx, cancel := context.WithCancel(ctx)
	task.cancel = cancel

	go m.executeTask(taskCtx, task, fn)

	return task, nil
}

func (m *TaskManager) Cancel(taskID string, reason string) bool {
	m.mu.RLock()
	task, ok := m.registry[taskID]
	m.mu.RUnlock()

	if !ok {
		return false
	}

	task.mu.Lock()
	if task.Status.IsTerminal() {
		task.mu.Unlock()
		return false
	}
	task.CancelReason = reason
	task.CancelledBy = taskID
	task.mu.Unlock()

	if task.cancel != nil {
		task.cancel()
	}
	return true
}

func (m *TaskManager) CancelGroup(group string, reason string) int {
	m.mu.RLock()
	var tasks []*Task
	for _, task := range m.registry {
		if task.Group == group {
			tasks = append(tasks, task)
		}
	}
	m.mu.RUnlock()

	count := 0
	for _, task := range tasks {
		if m.Cancel(task.ID, reason) {
			count++
		}
	}
	return count
}

func (m *TaskManager) CancelAll(reason string) int {
	m.mu.RLock()
	var taskIDs []string
	for id, task := range m.registry {
		if !task.IsTerminal() {
			taskIDs = append(taskIDs, id)
		}
	}
	m.mu.RUnlock()

	count := 0
	for _, id := range taskIDs {
		if m.Cancel(id, reason) {
			count++
		}
	}
	return count
}

func (m *TaskManager) Get(taskID string) (*Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.registry[taskID]
	return task, ok
}

func (m *TaskManager) WaitGroup(ctx context.Context, group string) []TaskResult {
	m.mu.RLock()
	var tasks []*Task
	for _, task := range m.registry {
		if task.Group == group {
			tasks = append(tasks, task)
		}
	}
	m.mu.RUnlock()

	results := make([]TaskResult, 0, len(tasks))
	for _, task := range tasks {
		select {
		case <-ctx.Done():
			return results
		case <-task.done:
			task.mu.RLock()
			results = append(results, TaskResult{
				TaskID: task.ID,
				Result: task.Result,
				Err:    task.Err,
			})
			task.mu.RUnlock()
		}
	}
	return results
}

func (m *TaskManager) WaitAll(ctx context.Context) []TaskResult {
	m.mu.RLock()
	tasks := make([]*Task, 0, len(m.registry))
	for _, task := range m.registry {
		tasks = append(tasks, task)
	}
	m.mu.RUnlock()

	results := make([]TaskResult, 0, len(tasks))
	for _, task := range tasks {
		select {
		case <-ctx.Done():
			return results
		case <-task.done:
			task.mu.RLock()
			results = append(results, TaskResult{
				TaskID: task.ID,
				Result: task.Result,
				Err:    task.Err,
			})
			task.mu.RUnlock()
		}
	}
	return results
}

func (m *TaskManager) RemoveCompleted() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for id, task := range m.registry {
		if task.IsTerminal() {
			delete(m.registry, id)
			count++
		}
	}
	return count
}

func (m *TaskManager) CascadeCancel(taskID string, reason string) int {
	m.mu.RLock()
	var children []*Task
	for _, task := range m.registry {
		if task.ParentID == taskID && !task.IsTerminal() {
			children = append(children, task)
		}
	}
	m.mu.RUnlock()

	// 先取消目标任务
	count := 0
	if m.Cancel(taskID, reason) {
		count++
	}

	// 递归取消子任务
	for _, child := range children {
		count += m.CascadeCancel(child.ID, "parent_cancelled")
	}

	return count
}

func (m *TaskManager) GetTaskTree(taskID string) string {
	var lines []string
	m.buildTreeRecursive(taskID, &lines, 0)
	if len(lines) == 0 {
		return ""
	}
	result := lines[0]
	for i := 1; i < len(lines); i++ {
		result += "\n" + lines[i]
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func (m *TaskManager) executeTask(ctx context.Context, task *Task, fn func(ctx context.Context) (any, error)) {
	task.mu.Lock()
	task.Status = TaskRunning
	now := time.Now()
	task.StartedAt = &now
	task.mu.Unlock()

	// 如果设置了超时，包装 context
	var execCtx context.Context
	var timeoutCancel context.CancelFunc
	if task.Timeout > 0 {
		execCtx, timeoutCancel = context.WithTimeout(ctx, task.Timeout)
		defer timeoutCancel()
	} else {
		execCtx = ctx
	}

	// 使用 channel 等待任务函数完成或 context 取消
	type fnResult struct {
		result any
		err    error
	}
	resultCh := make(chan fnResult, 1)

	go func() {
		result, err := fn(execCtx)
		resultCh <- fnResult{result: result, err: err}
	}()

	// 等待结果或 context 取消
	var fnRes fnResult
	select {
	case fnRes = <-resultCh:
		// 任务函数正常返回
	case <-execCtx.Done():
		// Context 被取消或超时，等待任务函数完成
		select {
		case fnRes = <-resultCh:
			// 任务函数在取消后也返回了
		case <-time.After(100 * time.Millisecond):
			// 任务函数未及时返回，视为被取消/超时
		}
	}

	task.mu.Lock()
	defer task.mu.Unlock()

	finishedAt := time.Now()
	task.FinishedAt = &finishedAt

	// 优先检查 context 状态（超时/取消比函数返回值更有权威性）
	if execCtx.Err() == context.DeadlineExceeded {
		task.Status = TaskTimeout
		task.Err = fmt.Errorf("任务超时，超时时间: %v", task.Timeout)
	} else if ctx.Err() == context.Canceled || execCtx.Err() == context.Canceled {
		task.Status = TaskCancelled
		if task.CancelReason == "" {
			task.CancelReason = "context_cancelled"
		}
	} else if fnRes.err != nil {
		task.Status = TaskFailed
		task.Err = fnRes.err
	} else {
		task.Status = TaskCompleted
		task.Result = fnRes.result
	}

	close(task.done)
}

func (m *TaskManager) buildTreeRecursive(taskID string, lines *[]string, indent int) {
	m.mu.RLock()
	task, ok := m.registry[taskID]
	if !ok {
		m.mu.RUnlock()
		return
	}

	// 收集子任务
	var childIDs []string
	for id, t := range m.registry {
		if t.ParentID == taskID {
			childIDs = append(childIDs, id)
		}
	}
	m.mu.RUnlock()

	prefix := ""
	for i := 0; i < indent; i++ {
		prefix += "  "
	}
	if indent > 0 {
		prefix += "+- "
	}

	task.mu.RLock()
	statusInfo := fmt.Sprintf("[%s]", task.Status)
	if task.CancelledBy != "" {
		statusInfo += fmt.Sprintf(" (cancelled by: %s, reason: %s)", task.CancelledBy, task.CancelReason)
	} else if task.CancelReason != "" {
		statusInfo += fmt.Sprintf(" (reason: %s)", task.CancelReason)
	}
	line := fmt.Sprintf("%s%s %s", prefix, task.DisplayName(), statusInfo)
	task.mu.RUnlock()

	*lines = append(*lines, line)

	for _, childID := range childIDs {
		m.buildTreeRecursive(childID, lines, indent+1)
	}
}
