package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskFilter 任务过滤器。
// 对应 Python: TaskFilter(BaseModel)
type TaskFilter struct {
	// TaskID 任务ID，支持单个或多个
	TaskID any // string 或 []string
	// SessionID 会话ID
	SessionID string
	// UserID 用户ID
	UserID string
	// Priority 优先级，支持整数或 "highest"
	Priority any // int 或 "highest"
	// Status 任务状态
	Status schema.TaskStatus
	// WithChildren 是否包含子任务
	WithChildren bool
	// IsRoot 是否只查根任务
	IsRoot bool
}

// TaskManagerState 任务管理器可序列化状态。
// 对应 Python: TaskManagerState(BaseModel)
type TaskManagerState struct {
	// Tasks 任务字典
	Tasks map[string]*schema.Task `json:"tasks"`
	// PriorityIndex 优先级索引
	PriorityIndex map[int][]string `json:"priority_index"`
	// ParentToChildren 父→子关系索引
	ParentToChildren map[string]map[string]struct{} `json:"parent_to_children"`
	// ChildrenToParent 子→父关系索引
	ChildrenToParent map[string]string `json:"children_to_parent"`
	// RootTasks 根任务集合
	RootTasks map[string]struct{} `json:"root_tasks"`
}

// TaskManager 任务管理器。
// 对应 Python: TaskManager
type TaskManager struct {
	// config 配置
	config *config.ControllerConfig
	// tasks 任务字典 taskID → *Task
	tasks map[string]*schema.Task
	// priorityIndex 优先级索引 priority → []taskID
	priorityIndex map[int][]string
	// parentToChildren 父→子关系索引 parentTaskID → {childTaskID}
	parentToChildren map[string]map[string]struct{}
	// childToParent 子→父关系索引 childTaskID → parentTaskID
	childToParent map[string]string
	// rootTasks 根任务集合
	rootTasks map[string]struct{}
	// mu 读写锁
	mu sync.RWMutex
	// onTaskSubmitted SUBMITTED 状态通知回调
	onTaskSubmitted func()
}

// taskStatusConfig 任务状态更新内部配置。
type taskStatusConfig struct {
	// withChildren 是否同时更新子任务
	withChildren bool
	// isRecursive 是否递归更新子任务
	isRecursive bool
	// withErrorMessage 错误消息
	withErrorMessage string
}

// taskPriorityConfig 任务优先级更新内部配置。
type taskPriorityConfig struct {
	// withChildren 是否同时更新子任务
	withChildren bool
	// isRecursive 是否递归更新子任务
	isRecursive bool
}

// TaskStatusOption 任务状态更新选项函数。
type TaskStatusOption func(*taskStatusConfig)

// TaskPriorityOption 任务优先级更新选项函数。
type TaskPriorityOption func(*taskPriorityConfig)

// ──────────────────────────── 常量 ────────────────────────────
const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ToMap 将 TaskManagerState 序列化为 map[string]any。
// 用于 Controller 保存状态到 session。
func (s *TaskManagerState) ToMap() map[string]any {
	data, err := json.Marshal(s)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("TaskManagerState 序列化失败")
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		logger.Error(logComponent).Err(err).Msg("TaskManagerState 反序列化到 map 失败")
		return nil
	}
	return result
}

// TaskManagerStateFromMap 从 map[string]any 反序列化为 TaskManagerState。
// 用于 Controller 从 session 恢复状态。
func TaskManagerStateFromMap(data map[string]any) (*TaskManagerState, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化 map 失败: %w", err)
	}
	var state TaskManagerState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("反序列化 TaskManagerState 失败: %w", err)
	}
	return &state, nil
}

// NewTaskManager 创建新的 TaskManager 实例。
// 对应 Python: TaskManager.__init__
func NewTaskManager(cfg *config.ControllerConfig) *TaskManager {
	return &TaskManager{
		config:           cfg,
		tasks:            make(map[string]*schema.Task),
		priorityIndex:    make(map[int][]string),
		parentToChildren: make(map[string]map[string]struct{}),
		childToParent:    make(map[string]string),
		rootTasks:        make(map[string]struct{}),
	}
}

// WithChildren 设置状态/优先级更新时同时更新子任务。
func WithChildren(enabled bool) TaskStatusOption {
	return func(c *taskStatusConfig) { c.withChildren = enabled }
}

// WithChildrenPriority 设置优先级更新时同时更新子任务。
func WithChildrenPriority(enabled bool) TaskPriorityOption {
	return func(c *taskPriorityConfig) { c.withChildren = enabled }
}

// IsRecursive 设置状态/优先级更新时递归更新子任务。
func IsRecursive(enabled bool) TaskStatusOption {
	return func(c *taskStatusConfig) { c.isRecursive = enabled }
}

// IsRecursivePriority 设置优先级更新时递归更新子任务。
func IsRecursivePriority(enabled bool) TaskPriorityOption {
	return func(c *taskPriorityConfig) { c.isRecursive = enabled }
}

// WithErrorMessage 设置状态更新时的错误消息。
func WithErrorMessage(msg string) TaskStatusOption {
	return func(c *taskStatusConfig) { c.withErrorMessage = msg }
}

// AddTask 添加任务，更新索引，触发 onTaskSubmitted 回调。
// 对应 Python: TaskManager.add_task
func (tm *TaskManager) AddTask(_ context.Context, task *schema.Task) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// 检查任务ID是否已存在
	if _, exists := tm.tasks[task.TaskID]; exists {
		logger.Error(logComponent).
			Str("task_id", task.TaskID).
			Str("event_type", "TASK_ADD_ERROR").
			Msg("任务ID已存在，添加失败")
		return exception.NewBaseError(exception.StatusAgentControllerTaskParamError,
			exception.WithMsg(fmt.Sprintf("任务ID已存在: %s", task.TaskID)),
		)
	}

	// 添加任务
	tm.tasks[task.TaskID] = task

	// 更新优先级索引
	tm.priorityIndex[task.Priority] = append(tm.priorityIndex[task.Priority], task.TaskID)

	// 更新层级索引
	if task.ParentTaskID != "" {
		// 有父任务：加入父→子索引
		if tm.parentToChildren[task.ParentTaskID] == nil {
			tm.parentToChildren[task.ParentTaskID] = make(map[string]struct{})
		}
		tm.parentToChildren[task.ParentTaskID][task.TaskID] = struct{}{}
		tm.childToParent[task.TaskID] = task.ParentTaskID
	} else {
		// 无父任务：加入根任务集合
		tm.rootTasks[task.TaskID] = struct{}{}
	}

	logger.Info(logComponent).
		Str("task_id", task.TaskID).
		Str("session_id", task.SessionID).
		Int("priority", task.Priority).
		Str("status", string(task.Status)).
		Msg("任务添加成功")

	// 触发 SUBMITTED 通知
	tm.notifyIfSubmitted([]*schema.Task{task})

	return nil
}

// GetTask 按条件查询任务，返回深拷贝。filter 为 nil 时返回全部。
// 对应 Python: TaskManager.get_task
func (tm *TaskManager) GetTask(_ context.Context, filter *TaskFilter) ([]*schema.Task, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// filter 为 nil 时返回全部任务的深拷贝
	if filter == nil {
		result := make([]*schema.Task, 0, len(tm.tasks))
		for _, task := range tm.tasks {
			result = append(result, tm.deepCopyTask(task))
		}
		return result, nil
	}

	// 检查 priority="highest" 不支持
	if p, ok := filter.Priority.(string); ok && p == "highest" {
		logger.Error(logComponent).
			Str("event_type", "TASK_GET_ERROR").
			Msg("GetTask 不支持 priority=highest，请使用 PopTask")
		return nil, exception.NewBaseError(exception.StatusAgentControllerTaskParamError,
			exception.WithMsg("GetTask 不支持 priority=highest，请使用 PopTask"),
		)
	}

	// 按条件过滤
	var matched []*schema.Task
	for _, task := range tm.tasks {
		if tm.matchFilter(task, filter) {
			matched = append(matched, task)
		}
	}

	// 按 TaskID 过滤（如果指定了）
	if filter.TaskID != nil {
		matched = tm.filterByTaskID(matched, filter.TaskID)
	}

	// 包含子任务
	if filter.WithChildren {
		matched = tm.appendChildren(matched)
	}

	// 返回深拷贝
	result := make([]*schema.Task, len(matched))
	for i, task := range matched {
		result[i] = tm.deepCopyTask(task)
	}

	logger.Info(logComponent).
		Int("matched_count", len(result)).
		Msg("任务查询完成")

	return result, nil
}

// PopTask 查询并移除任务。filter 不能为 nil。
// 对应 Python: TaskManager.pop_task
func (tm *TaskManager) PopTask(ctx context.Context, filter *TaskFilter) ([]*schema.Task, error) {
	if filter == nil {
		logger.Error(logComponent).
			Str("event_type", "TASK_POP_ERROR").
			Msg("PopTask 的 filter 不能为 nil")
		return nil, exception.NewBaseError(exception.StatusAgentControllerTaskParamError,
			exception.WithMsg("PopTask 的 filter 不能为 nil"),
		)
	}

	// priority="highest" 时取最大优先级
	if p, ok := filter.Priority.(string); ok && p == "highest" {
		maxPriority := tm.maxPriority()
		if maxPriority < 0 {
			return []*schema.Task{}, nil
		}
		filter = &TaskFilter{
			TaskID:       filter.TaskID,
			SessionID:    filter.SessionID,
			UserID:       filter.UserID,
			Priority:     maxPriority,
			Status:       filter.Status,
			WithChildren: filter.WithChildren,
			IsRoot:       filter.IsRoot,
		}
		logger.Info(logComponent).
			Int("highest_priority", maxPriority).
			Msg("PopTask 使用最高优先级")
	}

	// 先查询
	tasks, err := tm.GetTask(ctx, filter)
	if err != nil {
		return nil, err
	}

	// 再移除
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for _, task := range tasks {
		tm.removeTaskLocked(task.TaskID)
	}

	logger.Info(logComponent).
		Int("popped_count", len(tasks)).
		Msg("任务弹出完成")

	return tasks, nil
}

// UpdateTask 更新任务，同步更新索引。返回任务是否存在。
// 对应 Python: TaskManager.update_task
func (tm *TaskManager) UpdateTask(_ context.Context, task *schema.Task) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	oldTask, exists := tm.tasks[task.TaskID]
	if !exists {
		logger.Warn(logComponent).
			Str("task_id", task.TaskID).
			Msg("更新任务失败：任务不存在")
		return false
	}

	// 从旧优先级索引移除
	tm.removeFromPriorityIndex(oldTask.Priority, oldTask.TaskID)

	// 加入新优先级索引
	tm.priorityIndex[task.Priority] = append(tm.priorityIndex[task.Priority], task.TaskID)

	// 处理父任务变更：从旧层级索引移除
	if oldTask.ParentTaskID != task.ParentTaskID {
		// 从旧的父→子索引移除
		if oldTask.ParentTaskID != "" {
			if children, ok := tm.parentToChildren[oldTask.ParentTaskID]; ok {
				delete(children, oldTask.TaskID)
				if len(children) == 0 {
					delete(tm.parentToChildren, oldTask.ParentTaskID)
				}
			}
			delete(tm.childToParent, oldTask.TaskID)
		} else {
			// 旧任务没有父任务，从根任务移除
			delete(tm.rootTasks, oldTask.TaskID)
		}

		// 加入新层级索引
		if task.ParentTaskID != "" {
			if tm.parentToChildren[task.ParentTaskID] == nil {
				tm.parentToChildren[task.ParentTaskID] = make(map[string]struct{})
			}
			tm.parentToChildren[task.ParentTaskID][task.TaskID] = struct{}{}
			tm.childToParent[task.TaskID] = task.ParentTaskID
		} else {
			tm.rootTasks[task.TaskID] = struct{}{}
		}
	}

	// 更新任务
	tm.tasks[task.TaskID] = task

	logger.Info(logComponent).
		Str("task_id", task.TaskID).
		Int("new_priority", task.Priority).
		Str("new_status", string(task.Status)).
		Msg("任务更新成功")

	return true
}

// RemoveTask 按条件删除任务。
// 对应 Python: TaskManager.remove_task
// 删除父任务时，未被删除的子任务提升为根任务。
func (tm *TaskManager) RemoveTask(ctx context.Context, filter *TaskFilter) error {
	// 先查询要删除的任务
	tasks, err := tm.GetTask(ctx, filter)
	if err != nil {
		return err
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	for _, task := range tasks {
		tm.removeTaskLocked(task.TaskID)
	}

	logger.Info(logComponent).
		Int("removed_count", len(tasks)).
		Msg("任务删除完成")

	return nil
}

// UpdateTaskStatus 更新任务状态。
// 对应 Python: TaskManager.update_task_status
func (tm *TaskManager) UpdateTaskStatus(_ context.Context, taskID string, newStatus schema.TaskStatus, opts ...TaskStatusOption) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// 解析选项
	cfg := &taskStatusConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	task, exists := tm.tasks[taskID]
	if !exists {
		logger.Error(logComponent).
			Str("task_id", taskID).
			Str("event_type", "TASK_STATUS_UPDATE_ERROR").
			Msg("更新状态失败：任务不存在")
		return exception.NewBaseError(exception.StatusAgentControllerTaskParamError,
			exception.WithMsg(fmt.Sprintf("任务不存在: %s", taskID)),
		)
	}

	// 更新状态
	oldStatus := task.Status
	task.Status = newStatus

	// 设置错误消息
	if cfg.withErrorMessage != "" {
		task.ErrorMessage = cfg.withErrorMessage
	}

	logger.Info(logComponent).
		Str("task_id", taskID).
		Str("old_status", string(oldStatus)).
		Str("new_status", string(newStatus)).
		Msg("任务状态更新")

	// 更新子任务
	if cfg.withChildren {
		children := tm.collectDirectChildren(taskID)
		for _, childID := range children {
			if child, ok := tm.tasks[childID]; ok {
				child.Status = newStatus
				if cfg.withErrorMessage != "" {
					child.ErrorMessage = cfg.withErrorMessage
				}
				logger.Info(logComponent).
					Str("task_id", childID).
					Str("new_status", string(newStatus)).
					Msg("子任务状态更新")
			}
		}
	}

	// 递归更新子任务
	if cfg.isRecursive {
		allChildren := make(map[string]struct{})
		tm.collectAllChildren(taskID, allChildren)
		for childID := range allChildren {
			if child, ok := tm.tasks[childID]; ok {
				child.Status = newStatus
				if cfg.withErrorMessage != "" {
					child.ErrorMessage = cfg.withErrorMessage
				}
				logger.Info(logComponent).
					Str("task_id", childID).
					Str("new_status", string(newStatus)).
					Msg("递归子任务状态更新")
			}
		}
	}

	// 触发 SUBMITTED 通知
	tm.notifyIfSubmitted([]*schema.Task{task})

	return nil
}

// SetPriority 设置任务优先级。
// 对应 Python: TaskManager.set_priority
func (tm *TaskManager) SetPriority(_ context.Context, taskID string, newPriority int, opts ...TaskPriorityOption) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// 解析选项
	cfg := &taskPriorityConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	task, exists := tm.tasks[taskID]
	if !exists {
		logger.Error(logComponent).
			Str("task_id", taskID).
			Str("event_type", "TASK_PRIORITY_UPDATE_ERROR").
			Msg("设置优先级失败：任务不存在")
		return exception.NewBaseError(exception.StatusAgentControllerTaskParamError,
			exception.WithMsg(fmt.Sprintf("任务不存在: %s", taskID)),
		)
	}

	// 从旧优先级索引移除，加入新优先级索引
	oldPriority := task.Priority
	tm.removeFromPriorityIndex(oldPriority, taskID)
	tm.priorityIndex[newPriority] = append(tm.priorityIndex[newPriority], taskID)
	task.Priority = newPriority

	logger.Info(logComponent).
		Str("task_id", taskID).
		Int("old_priority", oldPriority).
		Int("new_priority", newPriority).
		Msg("任务优先级更新")

	// 更新直接子任务优先级
	if cfg.withChildren {
		children := tm.collectDirectChildren(taskID)
		for _, childID := range children {
			if child, ok := tm.tasks[childID]; ok {
				tm.removeFromPriorityIndex(child.Priority, childID)
				tm.priorityIndex[newPriority] = append(tm.priorityIndex[newPriority], childID)
				child.Priority = newPriority
				logger.Info(logComponent).
					Str("task_id", childID).
					Int("new_priority", newPriority).
					Msg("子任务优先级更新")
			}
		}
	}

	// 递归更新子任务优先级
	if cfg.isRecursive {
		allChildren := make(map[string]struct{})
		tm.collectAllChildren(taskID, allChildren)
		for childID := range allChildren {
			if child, ok := tm.tasks[childID]; ok {
				tm.removeFromPriorityIndex(child.Priority, childID)
				tm.priorityIndex[newPriority] = append(tm.priorityIndex[newPriority], childID)
				child.Priority = newPriority
				logger.Info(logComponent).
					Str("task_id", childID).
					Int("new_priority", newPriority).
					Msg("递归子任务优先级更新")
			}
		}
	}

	return nil
}

// GetChildTask 获取子任务列表。
// 对应 Python: TaskManager.get_child_task
func (tm *TaskManager) GetChildTask(_ context.Context, taskID string, isRecursive bool) ([]*schema.Task, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if _, exists := tm.tasks[taskID]; !exists {
		logger.Error(logComponent).
			Str("task_id", taskID).
			Str("event_type", "TASK_GET_CHILD_ERROR").
			Msg("获取子任务失败：任务不存在")
		return nil, exception.NewBaseError(exception.StatusAgentControllerTaskParamError,
			exception.WithMsg(fmt.Sprintf("任务不存在: %s", taskID)),
		)
	}

	var childIDs []string
	if isRecursive {
		allChildren := make(map[string]struct{})
		tm.collectAllChildren(taskID, allChildren)
		for id := range allChildren {
			childIDs = append(childIDs, id)
		}
	} else {
		childIDs = tm.collectDirectChildren(taskID)
	}

	result := make([]*schema.Task, 0, len(childIDs))
	for _, id := range childIDs {
		if task, ok := tm.tasks[id]; ok {
			result = append(result, tm.deepCopyTask(task))
		}
	}

	logger.Info(logComponent).
		Str("task_id", taskID).
		Bool("is_recursive", isRecursive).
		Int("child_count", len(result)).
		Msg("获取子任务完成")

	return result, nil
}

// GetState 获取可序列化状态快照。
// 对应 Python: TaskManager.get_state
func (tm *TaskManager) GetState(_ context.Context) (*TaskManagerState, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// 深拷贝任务字典
	tasksCopy := make(map[string]*schema.Task, len(tm.tasks))
	for id, task := range tm.tasks {
		tasksCopy[id] = tm.deepCopyTask(task)
	}

	// 深拷贝优先级索引
	priorityCopy := make(map[int][]string, len(tm.priorityIndex))
	for p, ids := range tm.priorityIndex {
		idsCopy := make([]string, len(ids))
		copy(idsCopy, ids)
		priorityCopy[p] = idsCopy
	}

	// 深拷贝父→子索引
	ptcCopy := make(map[string]map[string]struct{}, len(tm.parentToChildren))
	for parent, children := range tm.parentToChildren {
		childrenCopy := make(map[string]struct{}, len(children))
		for k := range children {
			childrenCopy[k] = struct{}{}
		}
		ptcCopy[parent] = childrenCopy
	}

	// 深拷贝子→父索引
	ctpCopy := make(map[string]string, len(tm.childToParent))
	for k, v := range tm.childToParent {
		ctpCopy[k] = v
	}

	// 深拷贝根任务集合
	rootCopy := make(map[string]struct{}, len(tm.rootTasks))
	for k := range tm.rootTasks {
		rootCopy[k] = struct{}{}
	}

	logger.Info(logComponent).
		Int("task_count", len(tasksCopy)).
		Msg("获取状态快照完成")

	return &TaskManagerState{
		Tasks:            tasksCopy,
		PriorityIndex:    priorityCopy,
		ParentToChildren: ptcCopy,
		ChildrenToParent: ctpCopy,
		RootTasks:        rootCopy,
	}, nil
}

// LoadState 从快照恢复状态。
// 对应 Python: TaskManager.load_state
func (tm *TaskManager) LoadState(_ context.Context, state *TaskManagerState) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if state == nil {
		logger.Error(logComponent).
			Str("event_type", "TASK_LOAD_STATE_ERROR").
			Msg("加载状态失败：state 为 nil")
		return exception.NewBaseError(exception.StatusAgentControllerTaskParamError,
			exception.WithMsg("state 不能为 nil"),
		)
	}

	// 恢复任务字典
	tm.tasks = make(map[string]*schema.Task, len(state.Tasks))
	for id, task := range state.Tasks {
		tm.tasks[id] = tm.deepCopyTask(task)
	}

	// 恢复优先级索引
	tm.priorityIndex = make(map[int][]string, len(state.PriorityIndex))
	for p, ids := range state.PriorityIndex {
		idsCopy := make([]string, len(ids))
		copy(idsCopy, ids)
		tm.priorityIndex[p] = idsCopy
	}

	// 恢复父→子索引
	tm.parentToChildren = make(map[string]map[string]struct{}, len(state.ParentToChildren))
	for parent, children := range state.ParentToChildren {
		childrenCopy := make(map[string]struct{}, len(children))
		for k := range children {
			childrenCopy[k] = struct{}{}
		}
		tm.parentToChildren[parent] = childrenCopy
	}

	// 恢复子→父索引
	tm.childToParent = make(map[string]string, len(state.ChildrenToParent))
	for k, v := range state.ChildrenToParent {
		tm.childToParent[k] = v
	}

	// 恢复根任务集合
	tm.rootTasks = make(map[string]struct{}, len(state.RootTasks))
	for k := range state.RootTasks {
		tm.rootTasks[k] = struct{}{}
	}

	logger.Info(logComponent).
		Int("task_count", len(tm.tasks)).
		Msg("加载状态快照完成")

	return nil
}

// ClearState 清空所有状态。
// 对应 Python: TaskManager.clear_state
func (tm *TaskManager) ClearState(_ context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.tasks = make(map[string]*schema.Task)
	tm.priorityIndex = make(map[int][]string)
	tm.parentToChildren = make(map[string]map[string]struct{})
	tm.childToParent = make(map[string]string)
	tm.rootTasks = make(map[string]struct{})

	logger.Info(logComponent).Msg("状态已清空")

	return nil
}

// SetOnTaskSubmitted 注册 SUBMITTED 状态通知回调。
// 对应 Python: TaskManager.set_on_task_submitted
func (tm *TaskManager) SetOnTaskSubmitted(callback func()) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.onTaskSubmitted = callback
}

// Config 返回当前配置。
func (tm *TaskManager) Config() *config.ControllerConfig {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.config
}

// SetConfig 设置配置。
func (tm *TaskManager) SetConfig(cfg *config.ControllerConfig) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.config = cfg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// notifyIfSubmitted 有 SUBMITTED 任务时触发回调。
// 对应 Python: TaskManager._notify_if_submitted
// 调用方必须持有 tm.mu 锁。
func (tm *TaskManager) notifyIfSubmitted(tasks []*schema.Task) {
	for _, task := range tasks {
		if task.Status == schema.TaskSubmitted {
			if tm.onTaskSubmitted != nil {
				logger.Info(logComponent).
					Str("task_id", task.TaskID).
					Msg("触发 SUBMITTED 通知回调")
				// 在回调中释放锁再获取，避免死锁
				// 但由于 notifyIfSubmitted 在锁内调用，
				// 回调不应再访问 TaskManager 的锁保护字段
				tm.onTaskSubmitted()
			}
			return
		}
	}
}

// matchFilter 判断任务是否匹配过滤器。
// 调用方必须持有 tm.mu.RLock。
func (tm *TaskManager) matchFilter(task *schema.Task, filter *TaskFilter) bool {
	// SessionID 过滤
	if filter.SessionID != "" && task.SessionID != filter.SessionID {
		return false
	}

	// Priority 过滤
	if filter.Priority != nil {
		switch p := filter.Priority.(type) {
		case int:
			if task.Priority != p {
				return false
			}
		}
	}

	// Status 过滤
	if filter.Status != "" && task.Status != filter.Status {
		return false
	}

	// IsRoot 过滤
	if filter.IsRoot {
		if _, isRoot := tm.rootTasks[task.TaskID]; !isRoot {
			return false
		}
	}

	return true
}

// filterByTaskID 按 TaskID 过滤任务列表。
func (tm *TaskManager) filterByTaskID(tasks []*schema.Task, taskID any) []*schema.Task {
	var result []*schema.Task
	switch id := taskID.(type) {
	case string:
		for _, task := range tasks {
			if task.TaskID == id {
				result = append(result, task)
			}
		}
	case []string:
		idSet := make(map[string]struct{}, len(id))
		for _, s := range id {
			idSet[s] = struct{}{}
		}
		for _, task := range tasks {
			if _, ok := idSet[task.TaskID]; ok {
				result = append(result, task)
			}
		}
	}
	return result
}

// appendChildren 为匹配的任务追加子任务。
// 调用方必须持有 tm.mu.RLock。
func (tm *TaskManager) appendChildren(tasks []*schema.Task) []*schema.Task {
	seen := make(map[string]struct{})
	for _, task := range tasks {
		seen[task.TaskID] = struct{}{}
	}

	var result []*schema.Task
	result = append(result, tasks...)

	for _, task := range tasks {
		if children, ok := tm.parentToChildren[task.TaskID]; ok {
			for childID := range children {
				if _, alreadySeen := seen[childID]; !alreadySeen {
					if child, childExists := tm.tasks[childID]; childExists {
						result = append(result, child)
						seen[childID] = struct{}{}
					}
				}
			}
		}
	}

	return result
}

// removeTaskLocked 从内部数据结构中移除任务。
// 调用方必须持有 tm.mu 写锁。
// 删除父任务时，未被删除的子任务提升为根任务。
func (tm *TaskManager) removeTaskLocked(taskID string) {
	task, exists := tm.tasks[taskID]
	if !exists {
		return
	}

	// 从优先级索引移除
	tm.removeFromPriorityIndex(task.Priority, taskID)

	// 处理子任务：提升为根任务
	if children, ok := tm.parentToChildren[taskID]; ok {
		for childID := range children {
			// 子任务的父引用移除，提升为根任务
			delete(tm.childToParent, childID)
			tm.rootTasks[childID] = struct{}{}
		}
		delete(tm.parentToChildren, taskID)
	}

	// 从父→子索引中移除自身（如果自己是子任务）
	if task.ParentTaskID != "" {
		if parentChildren, ok := tm.parentToChildren[task.ParentTaskID]; ok {
			delete(parentChildren, taskID)
			if len(parentChildren) == 0 {
				delete(tm.parentToChildren, task.ParentTaskID)
			}
		}
		delete(tm.childToParent, taskID)
	} else {
		// 从根任务集合移除
		delete(tm.rootTasks, taskID)
	}

	// 从任务字典移除
	delete(tm.tasks, taskID)

	logger.Info(logComponent).
		Str("task_id", taskID).
		Msg("任务已移除")
}

// removeFromPriorityIndex 从优先级索引中移除指定任务ID。
// 调用方必须持有 tm.mu 写锁。
func (tm *TaskManager) removeFromPriorityIndex(priority int, taskID string) {
	ids, ok := tm.priorityIndex[priority]
	if !ok {
		return
	}
	for i, id := range ids {
		if id == taskID {
			tm.priorityIndex[priority] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	if len(tm.priorityIndex[priority]) == 0 {
		delete(tm.priorityIndex, priority)
	}
}

// maxPriority 返回当前最大优先级，无任务时返回 -1。
// 调用方必须持有 tm.mu.RLock。
func (tm *TaskManager) maxPriority() int {
	maxP := -1
	for p := range tm.priorityIndex {
		if p > maxP {
			maxP = p
		}
	}
	return maxP
}

// collectAllChildren 递归收集所有后代任务ID。
// 调用方必须持有 tm.mu 读锁或写锁。
func (tm *TaskManager) collectAllChildren(parentID string, childrenSet map[string]struct{}) {
	directChildren, ok := tm.parentToChildren[parentID]
	if !ok {
		return
	}
	for childID := range directChildren {
		if _, alreadyAdded := childrenSet[childID]; alreadyAdded {
			continue
		}
		childrenSet[childID] = struct{}{}
		tm.collectAllChildren(childID, childrenSet)
	}
}

// collectDirectChildren 收集直接子任务ID列表。
// 调用方必须持有 tm.mu 读锁或写锁。
func (tm *TaskManager) collectDirectChildren(parentID string) []string {
	children, ok := tm.parentToChildren[parentID]
	if !ok {
		return nil
	}
	result := make([]string, 0, len(children))
	for id := range children {
		result = append(result, id)
	}
	return result
}

// deepCopyTask 深拷贝任务对象。
// 对应 Python: copy.deepcopy(task)
func (tm *TaskManager) deepCopyTask(task *schema.Task) *schema.Task {
	if task == nil {
		return nil
	}
	// 使用 JSON 序列化/反序列化实现深拷贝
	data, err := json.Marshal(task)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("task_id", task.TaskID).
			Str("event_type", "TASK_DEEP_COPY_ERROR").
			Msg("深拷贝任务失败，返回原引用")
		return task
	}
	var copy schema.Task
	if err := json.Unmarshal(data, &copy); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("task_id", task.TaskID).
			Str("event_type", "TASK_DEEP_COPY_ERROR").
			Msg("深拷贝任务反序列化失败，返回原引用")
		return task
	}
	return &copy
}
