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

// BackgroundTask 后台任务句柄，支持优雅停止。
//
// 对应 Python: BackgroundTask
// 基于 context.WithCancel 的轻量级后台 goroutine 生命周期管理。
// 适用于简单的后台任务场景，无需完整的状态机追踪。
type BackgroundTask struct {
	name   string
	group  string
	fn     func(ctx context.Context) error
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	mu     sync.Mutex
}

// Task 受管理的异步任务，带状态机和元数据。
//
// 对应 Python: Task (task_manager/task.py)
// 支持完整的状态机：PENDING → RUNNING → COMPLETED/FAILED/CANCELLED/TIMEOUT。
// 可配置超时、分组、父任务关联、元数据等。
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

// TaskManager 任务管理器，统一管理所有异步任务。
//
// 对应 Python: TaskManager (task_manager/manager.py)
// 提供任务的创建、取消、分组管理、等待等功能。
// 使用 Singleton 模式，全局唯一实例。
type TaskManager struct {
	registry map[string]*Task
	mu       sync.RWMutex
}

// TaskResult 任务等待结果。
type TaskResult struct {
	TaskID string
	Result any
	Err    error
}

// taskConfig 任务配置，由 Functional Options 设置。
type taskConfig struct {
	name     string
	group    string
	timeout  time.Duration
	metadata map[string]any
	parentID string
}

// ──────────────────────────── 枚举 ────────────────────────────

// TaskStatus 任务状态。
type TaskStatus int

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

// TaskOption 任务选项。
type TaskOption func(*taskConfig)

// ──────────────────────────── 全局变量 ────────────────────────────

// taskManagerSingleton 全局 TaskManager 单例持有器。
var taskManagerSingleton Singleton[TaskManager]

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTaskName 设置任务名称。
func WithTaskName(name string) TaskOption {
	return func(c *taskConfig) { c.name = name }
}

// IsTerminal 判断是否为终态。
func (s TaskStatus) IsTerminal() bool {
	return s == TaskCompleted || s == TaskFailed || s == TaskCancelled || s == TaskTimeout
}

// String 返回状态名称。
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

// WithTaskGroup 设置任务分组。
func WithTaskGroup(group string) TaskOption {
	return func(c *taskConfig) { c.group = group }
}

// WithTaskTimeout 设置任务超时。
func WithTaskTimeout(timeout time.Duration) TaskOption {
	return func(c *taskConfig) { c.timeout = timeout }
}

// WithTaskMetadata 设置任务元数据。
func WithTaskMetadata(md map[string]any) TaskOption {
	return func(c *taskConfig) { c.metadata = md }
}

// WithTaskParentID 设置父任务 ID。
func WithTaskParentID(id string) TaskOption {
	return func(c *taskConfig) { c.parentID = id }
}

// NewBackgroundTask 创建后台任务。
//
// fn 为任务执行函数，通过 ctx 实现取消语义。
// 创建后需调用 Start 启动。
func NewBackgroundTask(name, group string, fn func(ctx context.Context) error) *BackgroundTask {
	return &BackgroundTask{
		name:  name,
		group: group,
		fn:    fn,
		done:  make(chan struct{}),
	}
}

// Start 启动后台任务。
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

// Stop 优雅停止（发送取消信号并等待完成或超时）。
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
		return fmt.Errorf("background task %q stop timed out after %v", t.name, timeout)
	}
}

// Wait 等待任务完成，返回任务错误（如有）。
func (t *BackgroundTask) Wait() error {
	<-t.done
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// Done 返回只读 channel，任务结束时关闭。
func (t *BackgroundTask) Done() <-chan struct{} {
	return t.done
}

// Name 返回任务名称。
func (t *BackgroundTask) Name() string { return t.name }

// Group 返回任务分组。
func (t *BackgroundTask) Group() string { return t.group }

// IsTerminal 判断任务是否为终态。
func (t *Task) IsTerminal() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status.IsTerminal()
}

// DisplayName 返回任务显示名称。
func (t *Task) DisplayName() string {
	if t.Name != "" {
		return t.Name
	}
	if len(t.ID) >= 8 {
		return t.ID[:8]
	}
	return t.ID
}

// Wait 等待任务完成，返回结果或错误。
func (t *Task) Wait() (any, error) {
	<-t.done
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Result, t.Err
}

// GetTaskManager 获取全局 TaskManager 单例。
func GetTaskManager() *TaskManager {
	return taskManagerSingleton.Get(func() *TaskManager {
		return &TaskManager{
			registry: make(map[string]*Task),
		}
	})
}

// CreateTask 创建并启动受管理的异步任务。
//
// 对应 Python: TaskManager.create_task()
// fn 为任务执行函数，返回结果和错误。
// 通过 TaskOption 配置名称、分组、超时等。
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
		return nil, fmt.Errorf("task %s already exists", taskID)
	}
	m.registry[taskID] = task
	m.mu.Unlock()

	// 启动 goroutine 执行任务
	taskCtx, cancel := context.WithCancel(ctx)
	task.cancel = cancel

	go m.executeTask(taskCtx, task, fn)

	return task, nil
}

// Cancel 取消指定任务。
//
// 对应 Python: Task.cancel()
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

// CancelGroup 取消指定分组的所有任务。
//
// 对应 Python: TaskManager.cancel_group()
// 返回被取消的任务数量。
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

// CancelAll 取消所有运行中的任务。
//
// 对应 Python: TaskManager.cancel_all()
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

// Get 获取指定任务。
func (m *TaskManager) Get(taskID string) (*Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.registry[taskID]
	return task, ok
}

// WaitGroup 等待指定分组的所有任务完成。
//
// 对应 Python: TaskManager.wait_group()
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

// WaitAll 等待所有任务完成。
//
// 对应 Python: TaskManager.wait_all()
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

// RemoveCompleted 清理已完成的任务。
//
// 对应 Python: TaskManager.remove_completed()
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

// CascadeCancel 级联取消：取消指定任务及其所有子任务。
//
// 对应 Python: TaskManager.cascade_cancel()
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

// GetTaskTree 获取任务树的字符串表示。
//
// 对应 Python: TaskManager.get_task_tree()
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

// executeTask 执行任务的核心逻辑，管理状态机转换。
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
		task.Err = fmt.Errorf("task timeout after %v", task.Timeout)
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

// buildTreeRecursive 递归构建任务树。
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
