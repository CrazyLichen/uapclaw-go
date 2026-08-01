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

// BackgroundTask 轻量后台任务句柄，管理 goroutine 生命周期。
// 对齐 Python: BackgroundTask — 优先走 TaskManager（_manager_task），fallback 到 goroutine（_asyncio_task）
type BackgroundTask struct {
	// name 任务名称
	name string
	// group 任务分组
	group string
	// fn 任务函数
	fn func(ctx context.Context) error
	// cancel 取消函数
	cancel context.CancelFunc
	// done 完成信号通道
	done chan struct{}
	// err 执行错误
	err error
	// mu 互斥锁
	mu sync.Mutex
	// managerTask 关联的 TaskManager Task（如果通过 TaskManager 创建）
	// 对齐 Python: self._manager_task
	managerTask *Task
	// ready 就绪信号通道，对齐 Python: self._ready = asyncio.Event()
	// close(t.ready) 等价于 _ready.set()，表示任务已就绪（managerTask 已设置或 goroutine 已启动）
	ready chan struct{}
}

// Task 任务数据模型，包含状态机和完整生命周期信息。
// 对应 Python: Task 数据模型 + 状态机
type Task struct {
	// ID 任务唯一标识
	ID string
	// Name 任务名称
	Name string
	// Group 任务分组
	Group string
	// ParentID 父任务 ID
	ParentID string
	// Status 任务状态
	Status TaskStatus
	// Timeout 超时时间
	Timeout time.Duration
	// Result 任务结果
	Result any
	// Err 执行错误
	Err error
	// CreatedAt 创建时间
	CreatedAt time.Time
	// StartedAt 开始执行时间
	StartedAt *time.Time
	// FinishedAt 完成时间
	FinishedAt *time.Time
	// Metadata 元数据
	Metadata map[string]any
	// CancelReason 取消原因
	CancelReason string
	// CancelledBy 取消操作者
	CancelledBy string

	// cancel 取消函数
	cancel context.CancelFunc
	// done 完成信号通道
	done chan struct{}
	// mu 读写锁
	mu sync.RWMutex
}

// TaskManager 任务管理器单例，提供任务的创建、取消、查询等操作。
// 对应 Python: TaskManager 单例
type TaskManager struct {
	// registry 任务注册表
	registry map[string]*Task
	// mu 读写锁
	mu sync.RWMutex
	// cancelWaitTimeout 取消任务后等待函数完成的超时时间，默认 1s
	// 对齐 Python: BackgroundTask.cancel(timeout=1.0)
	cancelWaitTimeout time.Duration
}

// TaskResult 任务执行结果。
type TaskResult struct {
	// TaskID 任务标识
	TaskID string
	// Result 任务结果
	Result any
	// Err 执行错误
	Err error
}

// taskConfig 任务配置，由 TaskOption 函数设置。
type taskConfig struct {
	// name 任务名称
	name string
	// group 任务分组
	group string
	// timeout 超时时间
	timeout time.Duration
	// metadata 元数据
	metadata map[string]any
	// parentID 父任务 ID
	parentID string
}

// TaskOption 任务选项函数类型。
type TaskOption func(*taskConfig)

// ──────────────────────────── 枚举 ────────────────────────────

// TaskStatus 任务状态枚举。
type TaskStatus int

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

// defaultCancelWaitTimeout 取消任务后等待函数完成的默认超时时间。
// 对齐 Python: BackgroundTask.cancel(timeout=1.0)
const defaultCancelWaitTimeout = 1 * time.Second

// ──────────────────────────── 全局变量 ────────────────────────────

var taskManagerSingleton Singleton[TaskManager]

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTaskName 设置任务名称选项。
func WithTaskName(name string) TaskOption {
	return func(c *taskConfig) { c.name = name }
}

// IsTerminal 判断任务状态是否为终态（已完成/失败/已取消/超时）。
func (s TaskStatus) IsTerminal() bool {
	return s == TaskCompleted || s == TaskFailed || s == TaskCancelled || s == TaskTimeout
}

// String 返回任务状态的字符串表示。
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

// WithTaskGroup 设置任务分组选项。
func WithTaskGroup(group string) TaskOption {
	return func(c *taskConfig) { c.group = group }
}

// WithTaskTimeout 设置任务超时选项。
func WithTaskTimeout(timeout time.Duration) TaskOption {
	return func(c *taskConfig) { c.timeout = timeout }
}

// WithTaskMetadata 设置任务元数据选项。
func WithTaskMetadata(md map[string]any) TaskOption {
	return func(c *taskConfig) { c.metadata = md }
}

// WithTaskParentID 设置任务父 ID 选项。
func WithTaskParentID(id string) TaskOption {
	return func(c *taskConfig) { c.parentID = id }
}

// NewBackgroundTask 创建轻量后台任务句柄。
// 对齐 Python: BackgroundTask(group=group) — _ready 初始未设置
func NewBackgroundTask(name, group string, fn func(ctx context.Context) error) *BackgroundTask {
	return &BackgroundTask{
		name:  name,
		group: group,
		fn:    fn,
		done:  make(chan struct{}),
		ready: make(chan struct{}),
	}
}

// CreateBackgroundTask 创建后台任务，优先通过 TaskManager 注册，fallback 到直接 goroutine。
// 对齐 Python: create_background_task(coro, name, group, fallback_to_asyncio=True)
func CreateBackgroundTask(ctx context.Context, fn func(ctx context.Context) error, name string, group string) (*BackgroundTask, error) {
	manager := GetTaskManager()
	if manager != nil {
		// 优先通过 TaskManager 创建，对齐 Python: task = await create_task(...)
		task, err := manager.CreateTask(ctx, func(ctx context.Context) (any, error) { return nil, fn(ctx) },
			WithTaskName(name), WithTaskGroup(group))
		if err == nil {
			handle := NewBackgroundTask(name, group, fn)
			handle.managerTask = task
			// 对齐 Python: handle.set_manager_task(task) → _ready.set()
			// TaskManager 路径下 managerTask 已同步设置，立即就绪
			close(handle.ready)
			return handle, nil
		}
	}
	// Fallback: 直接 goroutine，对齐 Python: BackgroundTask.from_asyncio_task(...)
	handle := NewBackgroundTask(name, group, fn)
	handle.Start(ctx)
	return handle, nil
}

// StartBackgroundTask 从同步生命周期方法中创建后台任务。
// 对齐 Python: start_background_task(coro, name, group, fallback_to_asyncio=True)
// 同步版本：不等待 TaskManager 注册完成，直接 fallback 到 goroutine。
func StartBackgroundTask(fn func(ctx context.Context) error, name string, group string) *BackgroundTask {
	// 同步方法无法等待 async 的 TaskManager.CreateTask，直接 goroutine
	handle := NewBackgroundTask(name, group, fn)
	handle.Start(context.Background())
	return handle
}

// Start 启动后台任务 goroutine。
// 对齐 Python: BackgroundTask.from_asyncio_task → _ready.set() 立即就绪
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

	// goroutine 已启动，立即就绪，对齐 Python: _ready.set()
	close(t.ready)
}

// Stop 停止后台任务，等待完成或超时。
// 对齐 Python: BackgroundTask.cancel(reason, timeout) — 先等 _ready，再委托 _manager_task.cancel 或 asyncio_task.cancel
func (t *BackgroundTask) Stop(timeout time.Duration) error {
	// 先等就绪信号，对齐 Python: await self._ready.wait()
	select {
	case <-t.ready:
		// 就绪，继续操作
	case <-time.After(timeout):
		return fmt.Errorf("后台任务 %q 等待就绪超时，超时时间: %v", t.name, timeout)
	}

	// 检查是否有 managerTask，对齐 Python: if self._manager_task is not None
	t.mu.Lock()
	mgrTask := t.managerTask
	t.mu.Unlock()

	if mgrTask != nil {
		// 对齐 Python: await self._manager_task.cancel(reason=reason)
		GetTaskManager().Cancel(mgrTask.ID, "background_task_stop", "")
		// 对齐 Python: with anyio.move_on_after(timeout): await self._manager_task.wait()
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-mgrTask.done:
			mgrTask.mu.RLock()
			err := mgrTask.Err
			mgrTask.mu.RUnlock()
			return err
		case <-timer.C:
			return fmt.Errorf("后台任务 %q 停止超时，超时时间: %v", t.name, timeout)
		}
	}

	// 无 managerTask，用自身 cancel + done，对齐 Python: asyncio_task.cancel() + await asyncio_task
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

// Wait 等待后台任务完成，返回执行错误。
// 对齐 Python: BackgroundTask.wait() — 先等 _ready，再委托 _manager_task 或等 asyncio_task
func (t *BackgroundTask) Wait() error {
	// 先等就绪信号，对齐 Python: await self._ready.wait()
	<-t.ready

	// 检查是否有 managerTask，对齐 Python: if self._manager_task is not None
	t.mu.Lock()
	mgrTask := t.managerTask
	t.mu.Unlock()

	if mgrTask != nil {
		// 对齐 Python: return await self._manager_task.wait()
		_, err := mgrTask.Wait()
		return err
	}

	// 无 managerTask，等自身 done（goroutine 路径），对齐 Python: return await self._asyncio_task
	<-t.done
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// Done 返回完成信号通道。
func (t *BackgroundTask) Done() <-chan struct{} {
	return t.done
}

// Name 返回任务名称。
func (t *BackgroundTask) Name() string { return t.name }

// Group 返回任务分组。
func (t *BackgroundTask) Group() string { return t.group }

// IsTerminal 判断任务状态是否为终态。
func (t *Task) IsTerminal() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status.IsTerminal()
}

// DisplayName 返回任务显示名称（优先 Name，其次 ID 前 8 位）。
func (t *Task) DisplayName() string {
	if t.Name != "" {
		return t.Name
	}
	if len(t.ID) >= 8 {
		return t.ID[:8]
	}
	return t.ID
}

// Wait 等待任务完成，返回结果和错误。
func (t *Task) Wait() (any, error) {
	<-t.done
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Result, t.Err
}

// GetTaskManager 返回全局任务管理器单例。
func GetTaskManager() *TaskManager {
	return taskManagerSingleton.Get(func() *TaskManager {
		return &TaskManager{
			registry:          make(map[string]*Task),
			cancelWaitTimeout: defaultCancelWaitTimeout,
		}
	})
}

// CreateTask 创建并启动新任务。
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

// Cancel 取消指定任务。
// cancelledBy 指定发起取消的操作者 ID，对齐 Python Task.cancel(cancelled_by=)。
func (m *TaskManager) Cancel(taskID string, reason string, cancelledBy string) bool {
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
	if cancelledBy != "" {
		task.CancelledBy = cancelledBy
	}
	task.mu.Unlock()

	if task.cancel != nil {
		task.cancel()
	}
	return true
}

// CancelGroup 取消指定分组下的所有任务。
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
		if m.Cancel(task.ID, reason, "") {
			count++
		}
	}
	return count
}

// CancelAll 取消所有非终态任务。
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
		if m.Cancel(id, reason, "") {
			count++
		}
	}
	return count
}

// Get 根据任务 ID 获取任务。
func (m *TaskManager) Get(taskID string) (*Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.registry[taskID]
	return task, ok
}

// WaitGroup 等待指定分组下的所有任务完成。
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

// RemoveCompleted 移除所有已终态的任务记录。
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

// SetCancelWaitTimeout 设置取消任务后等待函数完成的超时时间。
// 对齐 Python: BackgroundTask.cancel(timeout=...) 的可配置超时参数。
func (m *TaskManager) SetCancelWaitTimeout(timeout time.Duration) {
	m.cancelWaitTimeout = timeout
}

// CascadeCancel 级联取消目标任务及其所有子任务。
// 对齐 Python: TaskManager._cascade_cancel(parent_id)，子任务 cancelledBy 为父任务 ID。
func (m *TaskManager) CascadeCancel(taskID string, reason string, cancelledBy string) int {
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
	if m.Cancel(taskID, reason, cancelledBy) {
		count++
	}

	// 递归取消子任务，cancelledBy 为父任务 ID，对齐 Python: child.cancelled_by = parent_id
	for _, child := range children {
		count += m.CascadeCancel(child.ID, "parent_cancelled", taskID)
	}

	return count
}

// GetTaskTree 返回任务及其子任务的树形字符串描述。
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
		case <-time.After(m.cancelWaitTimeout):
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
