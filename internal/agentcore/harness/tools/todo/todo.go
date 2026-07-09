package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TodoLockManager 待办事项锁管理器，为每个会话分配独立互斥锁。
// 对齐 Python: TodoLockManager
type TodoLockManager struct {
	// mu 全局读写锁
	mu sync.RWMutex
	// locks 会话级互斥锁映射
	locks map[string]*sync.Mutex
}

// TodoTool 待办事项工具基类，封装持久化读写逻辑。
// 对齐 Python: TodoTool
type TodoTool struct {
	// workspace 工作区根路径
	workspace string
	// fs 文件系统操作接口
	fs sys_operation.FsOperation
	// lockManager 锁管理器
	lockManager *TodoLockManager
}

// TodoCreateInput todo_create 工具的输入参数
type TodoCreateInput struct {
	// Tasks 待办任务列表
	Tasks []TodoTaskInput `json:"tasks"`
}

// TodoTaskInput 单个待办任务输入
type TodoTaskInput struct {
	// ID 任务唯一标识
	ID string `json:"id"`
	// Content 任务摘要描述
	Content string `json:"content"`
	// ActiveForm 进行中表述
	ActiveForm string `json:"activeForm"`
	// Description 详细说明
	Description string `json:"description"`
	// SelectedModelID 选定的模型标识
	SelectedModelID string `json:"selected_model_id,omitempty"`
}

// TodoListInput todo_list 工具的输入参数（无参数）
type TodoListInput struct{}

// TodoGetInput todo_get 工具的输入参数
type TodoGetInput struct {
	// ID 任务唯一标识
	ID string `json:"id"`
}

// TodoModifyInput todo_modify 工具的输入参数
type TodoModifyInput struct {
	// Action 操作类型
	Action string `json:"action"`
	// IDs 要操作的任务 ID 列表（delete/cancel）
	IDs []string `json:"ids,omitempty"`
	// Todos 待办事项数组（update/append）
	Todos []map[string]any `json:"todos,omitempty"`
	// TodoData 插入操作数据（insert_after/insert_before）
	TodoData *TodoInsertData `json:"todo_data,omitempty"`
}

// TodoInsertData 插入操作数据
type TodoInsertData struct {
	// TargetID 目标任务 ID
	TargetID string `json:"target_id"`
	// Items 要插入的任务列表
	Items []map[string]any `json:"items"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// todoFileName 待办事项持久化文件名
	todoFileName = "todo.json"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTodoLockManager 创建待办事项锁管理器。
// 对齐 Python: TodoLockManager.__init__
func NewTodoLockManager() *TodoLockManager {
	return &TodoLockManager{
		locks: make(map[string]*sync.Mutex),
	}
}

// Operation 获取指定会话的互斥锁，不存在则创建。
// 对齐 Python: TodoLockManager.operation
func (m *TodoLockManager) Operation(sessionID string) *sync.Mutex {
	m.mu.RLock()
	lock, ok := m.locks[sessionID]
	m.mu.RUnlock()
	if ok {
		return lock
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// 双重检查
	if lock, ok = m.locks[sessionID]; ok {
		return lock
	}
	lock = &sync.Mutex{}
	m.locks[sessionID] = lock
	return lock
}

// CleanupSession 清除指定会话的互斥锁。
// 对齐 Python: TodoLockManager.cleanup_session
func (m *TodoLockManager) CleanupSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.locks, sessionID)
}

// newTodoTool 创建待办事项工具基类。
// 对齐 Python: TodoTool.__init__
func newTodoTool(workspace string, fs sys_operation.FsOperation, lockManager *TodoLockManager) TodoTool {
	return TodoTool{
		workspace:   workspace,
		fs:          fs,
		lockManager: lockManager,
	}
}

// GetFilePath 返回指定会话的待办事项文件绝对路径。
// 对齐 Python: TodoTool._get_file_path + os.path.abspath
func (t *TodoTool) GetFilePath(sessionID string) string {
	relPath := filepath.Join(t.workspace, sessionID, todoFileName)
	absPath, err := filepath.Abs(relPath)
	if err != nil {
		return relPath
	}
	return absPath
}

// LoadTodos 从文件加载待办事项列表。
// 对齐 Python: TodoTool.load_todos
// 文件不存在或读取失败时返回 error，对齐 Python 抛异常行为。
func (t *TodoTool) LoadTodos(ctx context.Context, sessionID string) ([]hschema.TodoItem, error) {
	filePath := t.GetFilePath(sessionID)
	result, err := t.fs.ReadFile(ctx, filePath)
	if err != nil {
		// 对齐 Python L123-127: 文件不存在时 raise error
		logger.Warn(logComponent).
			Str("file_path", filePath).
			Err(err).
			Msg("LoadTodos 读取文件失败")
		return nil, exception.BuildError(
			exception.StatusToolTodosLoadFailed,
			exception.WithParam("reason", fmt.Sprintf("Todo file not found: %s", filePath)),
		)
	}
	if result == nil || result.Data == nil || result.Data.Content == "" {
		// 对齐 Python L123-127: 空内容视为文件不存在
		return nil, exception.BuildError(
			exception.StatusToolTodosLoadFailed,
			exception.WithParam("reason", fmt.Sprintf("Todo file is empty: %s", filePath)),
		)
	}

	var rawList []map[string]any
	if err := json.Unmarshal([]byte(result.Data.Content), &rawList); err != nil {
		logger.Error(logComponent).
			Str("file_path", filePath).
			Err(err).
			Msg("LoadTodos JSON 解码失败")
		return nil, exception.BuildError(
			exception.StatusToolTodosLoadFailed,
			exception.WithParam("reason", fmt.Sprintf("json decode failed: %s", err.Error())),
		)
	}

	items := make([]hschema.TodoItem, 0, len(rawList))
	for _, raw := range rawList {
		items = append(items, hschema.TodoItem{}.FromDict(raw))
	}
	return items, nil
}

// SaveTodos 将待办事项列表保存到文件。
// 对齐 Python: TodoTool.save_todos
func (t *TodoTool) SaveTodos(ctx context.Context, sessionID string, todos []hschema.TodoItem) error {
	filePath := t.GetFilePath(sessionID)
	dicts := make([]map[string]any, len(todos))
	for i, item := range todos {
		dicts[i] = item.ToDict()
	}
	data, err := json.Marshal(dicts)
	if err != nil {
		logger.Error(logComponent).
			Str("file_path", filePath).
			Err(err).
			Msg("SaveTodos JSON 编码失败")
		return exception.BuildError(
			exception.StatusToolTodosSaveFailed,
			exception.WithParam("reason", fmt.Sprintf("json encode failed: %s", err.Error())),
		)
	}

	_, err = t.fs.WriteFile(ctx, filePath, string(data))
	if err != nil {
		logger.Error(logComponent).
			Str("file_path", filePath).
			Err(err).
			Msg("SaveTodos 写入文件失败")
		return exception.BuildError(
			exception.StatusToolTodosSaveFailed,
			exception.WithParam("reason", fmt.Sprintf("write file failed: %s", err.Error())),
		)
	}
	return nil
}

// CleanupSession 清除指定会话的锁和持久化文件。
// 对齐 Python: TodoTool.cleanup_session
func (t *TodoTool) CleanupSession(sessionID string) {
	t.lockManager.CleanupSession(sessionID)
}

// NewTodoCreateTool 创建待办事项创建工具。
// 对齐 Python: TodoCreateTool.__init__
func NewTodoCreateTool(todoTool TodoTool, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("todo_create", "todo_create", language, nil, agentID)

	fn := func(ctx context.Context, input TodoCreateInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 获取 sessionID
		sessionID, err := extractSessionID(opts)
		if err != nil {
			return nil, err
		}

		// 校验输入
		if len(input.Tasks) == 0 {
			return nil, exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", "tasks is required and must not be empty"),
			)
		}

		// 校验每个 task 的必填字段和 ID 唯一性
		// 对齐 Python L296-299: 优先使用 model 提供的 id，为空时自动生成 uuid
		idSet := make(map[string]struct{})
		for i := range input.Tasks {
			task := &input.Tasks[i]
			if task.ID == "" {
				task.ID = uuid.New().String()
			}
			if task.Content == "" {
				return nil, exception.BuildError(
					exception.StatusToolTodosValidationInvalid,
					exception.WithParam("reason", fmt.Sprintf("task %s: content is required", task.ID)),
				)
			}
			if task.ActiveForm == "" {
				return nil, exception.BuildError(
					exception.StatusToolTodosValidationInvalid,
					exception.WithParam("reason", fmt.Sprintf("task %s: activeForm is required", task.ID)),
				)
			}
			if task.Description == "" {
				return nil, exception.BuildError(
					exception.StatusToolTodosValidationInvalid,
					exception.WithParam("reason", fmt.Sprintf("task %s: description is required", task.ID)),
				)
			}
			if _, exists := idSet[task.ID]; exists {
				return nil, exception.BuildError(
					exception.StatusToolTodosValidationInvalid,
					exception.WithParam("reason", fmt.Sprintf("duplicate task id: %s", task.ID)),
				)
			}
			idSet[task.ID] = struct{}{}
		}

		// 构造 TodoItem 列表
		todoItems := make([]hschema.TodoItem, len(input.Tasks))
		for i, task := range input.Tasks {
			status := hschema.TodoStatusPending
			if i == 0 {
				status = hschema.TodoStatusInProgress
			}
			todoItems[i] = hschema.TodoItem{
				ID:              task.ID,
				Content:         task.Content,
				ActiveForm:      task.ActiveForm,
				Description:     task.Description,
				Status:          status,
				SelectedModelID: task.SelectedModelID,
			}
		}

		// 加锁、保存
		lock := todoTool.lockManager.Operation(sessionID)
		lock.Lock()
		defer lock.Unlock()

		if err := todoTool.SaveTodos(ctx, sessionID, todoItems); err != nil {
			return nil, err
		}

		logger.Info(logComponent).
			Str("session_id", sessionID).
			Int("task_count", len(todoItems)).
			Msg("TodoCreateTool 创建待办事项成功")

		// 格式化结果字符串
		// 对齐 Python: TodoCreateTool._format_create_result L250-266
		resultStr := formatCreateResult(todoItems)
		return map[string]any{
			"message": resultStr,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// NewTodoListTool 创建待办事项列表工具。
// 对齐 Python: TodoListTool.__init__
func NewTodoListTool(todoTool TodoTool, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("todo_list", "todo_list", language, nil, agentID)

	fn := func(ctx context.Context, _ TodoListInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 获取 sessionID
		sessionID, err := extractSessionID(opts)
		if err != nil {
			return nil, err
		}

		// 加锁、加载
		lock := todoTool.lockManager.Operation(sessionID)
		lock.Lock()
		defer lock.Unlock()

		todos, err := todoTool.LoadTodos(ctx, sessionID)
		if err != nil {
			return nil, err
		}

		// 过滤掉已完成和已取消的任务，返回简化视图
		// 对齐 Python L362-377: 只包含 id/content/status/depends_on
		type simplifiedTask struct {
			ID        string   `json:"id"`
			Content   string   `json:"content"`
			Status    string   `json:"status"`
			DependsOn []string `json:"depends_on"`
		}
		var simplified []simplifiedTask
		for _, item := range todos {
			if item.Status != hschema.TodoStatusCompleted && item.Status != hschema.TodoStatusCancelled {
				simplified = append(simplified, simplifiedTask{
					ID:        item.ID,
					Content:   item.Content,
					Status:    item.Status.String(),
					DependsOn: item.DependsOn,
				})
			}
		}

		return map[string]any{
			"tasks": simplified,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// NewTodoGetTool 创建待办事项详情查询工具。
// 对齐 Python: TodoGetTool.__init__
func NewTodoGetTool(todoTool TodoTool, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("todo_get", "todo_get", language, nil, agentID)

	fn := func(ctx context.Context, input TodoGetInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 获取 sessionID
		sessionID, err := extractSessionID(opts)
		if err != nil {
			return nil, err
		}

		if input.ID == "" {
			return nil, exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", "id is required"),
			)
		}

		// 加锁、加载
		lock := todoTool.lockManager.Operation(sessionID)
		lock.Lock()
		defer lock.Unlock()

		todos, err := todoTool.LoadTodos(ctx, sessionID)
		if err != nil {
			return nil, err
		}

		// 按 ID 查找
		for _, item := range todos {
			if item.ID == input.ID {
				return map[string]any{
					"todo": item.ToDict(),
				}, nil
			}
		}

		return nil, exception.BuildError(
			exception.StatusToolTodosInvokeFailed,
			exception.WithParam("reason", fmt.Sprintf("todo item with id %s not found", input.ID)),
		)
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// NewTodoModifyTool 创建待办事项修改工具。
// 对齐 Python: TodoModifyTool.__init__
func NewTodoModifyTool(todoTool TodoTool, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("todo_modify", "todo_modify", language, nil, agentID)

	fn := func(ctx context.Context, input TodoModifyInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 获取 sessionID
		sessionID, err := extractSessionID(opts)
		if err != nil {
			return nil, err
		}

		// 加锁、加载
		lock := todoTool.lockManager.Operation(sessionID)
		lock.Lock()
		defer lock.Unlock()

		todos, err := todoTool.LoadTodos(ctx, sessionID)
		if err != nil {
			return nil, err
		}

		// 根据 action 分派
		var updatedTodos []hschema.TodoItem
		var msg string
		switch input.Action {
		case "update":
			updatedTodos, msg, err = todoModifyUpdate(todos, input.Todos)
		case "delete":
			updatedTodos, msg, err = todoModifyDelete(todos, input.IDs)
		case "cancel":
			updatedTodos, msg, err = todoModifyCancel(todos, input.IDs)
		case "append":
			updatedTodos, msg, err = todoModifyAppend(todos, input.Todos)
		case "insert_after":
			if input.TodoData == nil {
				return nil, exception.BuildError(
					exception.StatusToolTodosValidationInvalid,
					exception.WithParam("reason", "todo_data is required for insert_after action"),
				)
			}
			updatedTodos, msg, err = todoModifyInsertAfter(todos, input.TodoData.TargetID, input.TodoData.Items)
		case "insert_before":
			if input.TodoData == nil {
				return nil, exception.BuildError(
					exception.StatusToolTodosValidationInvalid,
					exception.WithParam("reason", "todo_data is required for insert_before action"),
				)
			}
			updatedTodos, msg, err = todoModifyInsertBefore(todos, input.TodoData.TargetID, input.TodoData.Items)
		default:
			return nil, exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("unsupported action: %s", input.Action)),
			)
		}

		if err != nil {
			return nil, err
		}

		// 保存
		if err := todoTool.SaveTodos(ctx, sessionID, updatedTodos); err != nil {
			return nil, err
		}

		logger.Info(logComponent).
			Str("session_id", sessionID).
			Str("action", input.Action).
			Int("task_count", len(updatedTodos)).
			Msg("TodoModifyTool 修改待办事项成功")

		return map[string]any{
			"message": msg,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// CreateTodosTool 创建全部待办事项工具集，同时返回 TodoTool 基类供 Rail 调用 LoadTodos/SaveTodos/CleanupSession。
// 对齐 Python: create_todos_tool
func CreateTodosTool(workspace string, fs sys_operation.FsOperation, language, agentID string) ([]tool.Tool, TodoTool) {
	lockManager := NewTodoLockManager()
	todoTool := newTodoTool(workspace, fs, lockManager)
	tools := []tool.Tool{
		NewTodoCreateTool(todoTool, language, agentID),
		NewTodoListTool(todoTool, language, agentID),
		NewTodoGetTool(todoTool, language, agentID),
		NewTodoModifyTool(todoTool, language, agentID),
	}
	return tools, todoTool
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractSessionID 从工具选项中提取会话 ID
func extractSessionID(opts []tool.ToolOption) (string, error) {
	callOpts := tool.NewToolCallOptions(opts...)
	session := callOpts.Session
	if session == nil {
		return "", exception.BuildError(
			exception.StatusToolTodosInvokeFailed,
			exception.WithParam("reason", "Session ID is required"),
		)
	}
	sessionID := ""
	if sess, ok := session.(interface{ GetSessionID() string }); ok {
		sessionID = sess.GetSessionID()
	}
	if sessionID == "" {
		return "", exception.BuildError(
			exception.StatusToolTodosInvokeFailed,
			exception.WithParam("reason", "Session ID is required"),
		)
	}
	return sessionID, nil
}

// formatCreateResult 将创建的待办事项格式化为可读结果字符串。
// 对齐 Python: TodoCreateTool._format_create_result L250-266
// 包含 "Successfully created N task(s):" 前缀、状态图标、model 信息、"Next step" 引导提示。
func formatCreateResult(items []hschema.TodoItem) string {
	if len(items) == 0 {
		return ""
	}
	result := fmt.Sprintf("Successfully created %d task(s):\n", len(items))
	for _, item := range items {
		icon, ok := hschema.StatusIcons[item.Status]
		if !ok {
			icon = "[ ]"
		}
		modelInfo := ""
		if item.SelectedModelID != "" {
			modelInfo = fmt.Sprintf(" (model: %s)", item.SelectedModelID)
		}
		result += fmt.Sprintf("  %s task_id: %s , content: %s%s\n", icon, item.ID, item.Content, modelInfo)
	}
	firstTask := items[0].Content
	result += fmt.Sprintf("\nNext step: Immediately execute task '%s'", firstTask)
	return result
}

// formatTodoItems 将待办事项列表格式化为可读字符串
func formatTodoItems(items []hschema.TodoItem) string {
	if len(items) == 0 {
		return ""
	}
	result := ""
	for i, item := range items {
		if i > 0 {
			result += "\n"
		}
		icon, ok := hschema.StatusIcons[item.Status]
		if !ok {
			icon = "[?]"
		}
		result += fmt.Sprintf("%s %s (id=%s)", icon, item.Content, item.ID)
	}
	return result
}

// todoModifyUpdate 执行 update 操作
// 对齐 Python: TodoModifyTool._update_todos L662-691
func todoModifyUpdate(todos []hschema.TodoItem, updates []map[string]any) ([]hschema.TodoItem, string, error) {
	if len(updates) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "todos is required for update action"),
		)
	}

	// 收集所有要设为 in_progress 的 ID，以及将被从 in_progress 移除的 ID
	var inProgressIDs []string
	removingFromInProgress := make(map[string]struct{})
	for _, update := range updates {
		id, _ := update["id"].(string)
		if status, ok := update["status"].(string); ok {
			if status == "in_progress" {
				inProgressIDs = append(inProgressIDs, id)
			}
			// 如果现有任务是 in_progress 且被更新为非 in_progress 状态，则将其从现有计数中移除
			if status != "in_progress" {
				for _, item := range todos {
					if item.ID == id && item.Status == hschema.TodoStatusInProgress {
						removingFromInProgress[id] = struct{}{}
					}
				}
			}
		}
	}
	if err := validateSingleInProgress(todos, inProgressIDs, removingFromInProgress); err != nil {
		return nil, "", err
	}

	// 逐个更新
	updatedCount := 0
	for _, update := range updates {
		id, _ := update["id"].(string)
		if id == "" {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", "each update item must have an id field"),
			)
		}
		found := false
		for i := range todos {
			if todos[i].ID == id {
				found = true
				if content, ok := update["content"].(string); ok {
					todos[i].Content = content
				}
				if activeForm, ok := update["activeForm"].(string); ok {
					todos[i].ActiveForm = activeForm
				}
				if description, ok := update["description"].(string); ok {
					todos[i].Description = description
				}
				if status, ok := update["status"].(string); ok {
					parsed, err := hschema.ParseTodoStatus(status)
					if err != nil {
						return nil, "", exception.BuildError(
							exception.StatusToolTodosValidationInvalid,
							exception.WithParam("reason", fmt.Sprintf("invalid status %q for task %s", status, id)),
						)
					}
					todos[i].Status = parsed
				}
				if selectedModelID, ok := update["selected_model_id"].(string); ok && selectedModelID != "" {
					todos[i].SelectedModelID = selectedModelID
				}
				break
			}
		}
		if !found {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("todo item with id %s not found", id)),
			)
		}
		updatedCount++
	}
	return todos, fmt.Sprintf("Successfully updated %d task(s)", updatedCount), nil
}

// todoModifyDelete 执行 delete 操作
// 对齐 Python: TodoModifyTool._delete_todos L635-647
func todoModifyDelete(todos []hschema.TodoItem, ids []string) ([]hschema.TodoItem, string, error) {
	if len(ids) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "ids is required for delete action"),
		)
	}
	deleteSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		deleteSet[id] = struct{}{}
	}
	deletedCount := 0
	result := make([]hschema.TodoItem, 0, len(todos))
	for _, item := range todos {
		if _, exists := deleteSet[item.ID]; exists {
			deletedCount++
		} else {
			result = append(result, item)
		}
	}
	// 对齐 Python L644-645: 全部 ID 都不存在时的提示
	if deletedCount == 0 {
		return result, fmt.Sprintf("No tasks deleted: None of the provided IDs (%s) were found", strings.Join(ids, ", ")), nil
	}
	return result, fmt.Sprintf("Successfully deleted %d task(s) (IDs: %s)", deletedCount, strings.Join(ids, ", ")), nil
}

// todoModifyCancel 执行 cancel 操作
// 对齐 Python: TodoModifyTool._cancel_todos L649-660
// 不存在的 ID 静默跳过，全部不存在时返回提示消息。
func todoModifyCancel(todos []hschema.TodoItem, ids []string) ([]hschema.TodoItem, string, error) {
	if len(ids) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "ids is required for cancel action"),
		)
	}
	cancelledCount := 0
	var cancelledIDs []string
	for _, id := range ids {
		for i := range todos {
			if todos[i].ID == id {
				todos[i].Status = hschema.TodoStatusCancelled
				cancelledCount++
				cancelledIDs = append(cancelledIDs, id)
				break
			}
		}
	}
	// 对齐 Python L657-658: 全部 ID 都不存在时的提示
	if cancelledCount == 0 {
		return todos, fmt.Sprintf("No tasks cancelled: None of the provided IDs (%s) were found", strings.Join(ids, ", ")), nil
	}
	return todos, fmt.Sprintf("Successfully cancelled %d task(s) (IDs: %s)", cancelledCount, strings.Join(cancelledIDs, ", ")), nil
}

// todoModifyAppend 执行 append 操作
// 对齐 Python: TodoModifyTool._append_todos L693-707
func todoModifyAppend(todos []hschema.TodoItem, newItems []map[string]any) ([]hschema.TodoItem, string, error) {
	if len(newItems) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "todos is required for append action"),
		)
	}
	// 校验新任务
	var inProgressIDs []string
	for _, item := range newItems {
		if err := validateSingleTodoItem(item); err != nil {
			return nil, "", err
		}
		if status, ok := item["status"].(string); ok && status == "in_progress" {
			id, _ := item["id"].(string)
			inProgressIDs = append(inProgressIDs, id)
		}
	}
	if err := validateSingleInProgress(todos, inProgressIDs, nil); err != nil {
		return nil, "", err
	}

	// 检查 ID 唯一性
	existingIDs := make(map[string]struct{}, len(todos))
	for _, item := range todos {
		existingIDs[item.ID] = struct{}{}
	}
	for _, item := range newItems {
		id, _ := item["id"].(string)
		if _, exists := existingIDs[id]; exists {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("Batch append failed: Task with ID '%s' is duplicated", id)),
			)
		}
		existingIDs[id] = struct{}{}
	}

	// 追加
	for _, raw := range newItems {
		todoItem := todoItemFromMap(raw)
		todos = append(todos, todoItem)
	}
	return todos, fmt.Sprintf("Successfully appended %d task(s)", len(newItems)), nil
}

// todoModifyInsertAfter 执行 insert_after 操作
// 对齐 Python: TodoModifyTool._insert_after_todos L709-730
func todoModifyInsertAfter(todos []hschema.TodoItem, targetID string, items []map[string]any) ([]hschema.TodoItem, string, error) {
	if targetID == "" {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "target_id is required for insert_after action"),
		)
	}
	if len(items) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "items is required for insert_after action"),
		)
	}

	// 校验目标任务状态
	if err := validateTargetTaskStatus(todos, targetID, []hschema.TodoStatus{hschema.TodoStatusInProgress, hschema.TodoStatusPending}); err != nil {
		return nil, "", err
	}

	// 校验新任务
	var inProgressIDs []string
	for _, item := range items {
		if err := validateSingleTodoItem(item); err != nil {
			return nil, "", err
		}
		if status, ok := item["status"].(string); ok && status == "in_progress" {
			id, _ := item["id"].(string)
			inProgressIDs = append(inProgressIDs, id)
		}
	}
	if err := validateSingleInProgress(todos, inProgressIDs, nil); err != nil {
		return nil, "", err
	}

	// 校验 ID 唯一性
	existingIDs := make(map[string]struct{}, len(todos))
	for _, item := range todos {
		existingIDs[item.ID] = struct{}{}
	}
	for _, item := range items {
		id, _ := item["id"].(string)
		if _, exists := existingIDs[id]; exists {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("Insert failed: Task with ID '%s' already exists", id)),
			)
		}
		existingIDs[id] = struct{}{}
	}

	// 插入
	targetIdx := -1
	for i, item := range todos {
		if item.ID == targetID {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", fmt.Sprintf("target task %s not found", targetID)),
		)
	}

	newItems := make([]hschema.TodoItem, len(items))
	for i, raw := range items {
		newItems[i] = todoItemFromMap(raw)
	}

	result := make([]hschema.TodoItem, 0, len(todos)+len(items))
	result = append(result, todos[:targetIdx+1]...)
	result = append(result, newItems...)
	result = append(result, todos[targetIdx+1:]...)
	return result, fmt.Sprintf("Successfully inserted %d task(s) after target task, id: '%s'", len(newItems), targetID), nil
}

// todoModifyInsertBefore 执行 insert_before 操作
// 对齐 Python: TodoModifyTool._insert_before_todos L732-753
func todoModifyInsertBefore(todos []hschema.TodoItem, targetID string, items []map[string]any) ([]hschema.TodoItem, string, error) {
	if targetID == "" {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "target_id is required for insert_before action"),
		)
	}
	if len(items) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "items is required for insert_before action"),
		)
	}

	// 校验目标任务状态（insert_before 只允许 pending）
	if err := validateTargetTaskStatus(todos, targetID, []hschema.TodoStatus{hschema.TodoStatusPending}); err != nil {
		return nil, "", err
	}

	// 校验新任务
	var inProgressIDs []string
	for _, item := range items {
		if err := validateSingleTodoItem(item); err != nil {
			return nil, "", err
		}
		if status, ok := item["status"].(string); ok && status == "in_progress" {
			id, _ := item["id"].(string)
			inProgressIDs = append(inProgressIDs, id)
		}
	}
	if err := validateSingleInProgress(todos, inProgressIDs, nil); err != nil {
		return nil, "", err
	}

	// 校验 ID 唯一性
	existingIDs := make(map[string]struct{}, len(todos))
	for _, item := range todos {
		existingIDs[item.ID] = struct{}{}
	}
	for _, item := range items {
		id, _ := item["id"].(string)
		if _, exists := existingIDs[id]; exists {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("Insert failed: Task with ID '%s' already exists", id)),
			)
		}
		existingIDs[id] = struct{}{}
	}

	// 插入
	targetIdx := -1
	for i, item := range todos {
		if item.ID == targetID {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", fmt.Sprintf("target task %s not found", targetID)),
		)
	}

	newItems := make([]hschema.TodoItem, len(items))
	for i, raw := range items {
		newItems[i] = todoItemFromMap(raw)
	}

	result := make([]hschema.TodoItem, 0, len(todos)+len(items))
	result = append(result, todos[:targetIdx]...)
	result = append(result, newItems...)
	result = append(result, todos[targetIdx:]...)
	return result, fmt.Sprintf("Successfully inserted %d task(s) before target task, id: '%s'", len(newItems), targetID), nil
}

// validateSingleInProgress 校验同一时间只能有一个 in_progress 任务
// removingFromInProgress: 即将从 in_progress 状态移除的任务 ID 集合（用于 update 场景）
func validateSingleInProgress(existingTodos []hschema.TodoItem, newInProgressIDs []string, removingFromInProgress map[string]struct{}) error {
	// 统计现有 in_progress 数量（排除即将被移除的）
	if removingFromInProgress == nil {
		removingFromInProgress = make(map[string]struct{})
	}
	var currentInProgress []string
	for _, item := range existingTodos {
		if item.Status == hschema.TodoStatusInProgress {
			if _, removing := removingFromInProgress[item.ID]; !removing {
				currentInProgress = append(currentInProgress, item.ID)
			}
		}
	}
	// 如果当前有 in_progress 且新操作也要设 in_progress，检查是否冲突
	total := len(currentInProgress) + len(newInProgressIDs)
	// 如果新操作中的 in_progress 与现有的重叠（update 场景），不重复计算
	if len(currentInProgress) > 0 && len(newInProgressIDs) > 0 {
		existingSet := make(map[string]struct{}, len(currentInProgress))
		for _, id := range currentInProgress {
			existingSet[id] = struct{}{}
		}
		overlap := 0
		for _, id := range newInProgressIDs {
			if _, ok := existingSet[id]; ok {
				overlap++
			}
		}
		total = len(currentInProgress) + len(newInProgressIDs) - overlap
	}
	if total > 1 {
		return exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "only one task can be in_progress at a time"),
		)
	}
	return nil
}

// validateTargetTaskStatus 校验目标任务状态是否在允许列表中
func validateTargetTaskStatus(todos []hschema.TodoItem, targetID string, allowedStatuses []hschema.TodoStatus) error {
	for _, item := range todos {
		if item.ID == targetID {
			for _, allowed := range allowedStatuses {
				if item.Status == allowed {
					return nil
				}
			}
			allowedStrs := make([]string, len(allowedStatuses))
			for i, s := range allowedStatuses {
				allowedStrs[i] = s.String()
			}
			return exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("target task %s status is %s, must be %v", targetID, item.Status.String(), allowedStrs)),
			)
		}
	}
	return exception.BuildError(
		exception.StatusToolTodosValidationInvalid,
		exception.WithParam("reason", fmt.Sprintf("target task %s not found", targetID)),
	)
}

// validateSingleTodoItem 校验单个待办事项的必填字段和 status 合法值。
// 对齐 Python: TodoModifyTool._validate_single_todo_item L608-623
// 必填字段: id, content, activeForm, description, status
func validateSingleTodoItem(item map[string]any) error {
	var validationErrors []string
	requiredFields := []string{"content", "activeForm", "description", "status", "id"}
	for _, field := range requiredFields {
		if _, exists := item[field]; !exists {
			validationErrors = append(validationErrors, fmt.Sprintf("Missing required field: '%s'", field))
		}
	}
	// 校验 status 合法值
	if statusStr, ok := item["status"].(string); ok {
		if _, err := hschema.ParseTodoStatus(statusStr); err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("Invalid status '%s'", statusStr))
		}
	} else if _, exists := item["status"]; exists {
		validationErrors = append(validationErrors, "Invalid status type: must be a string")
	}
	if len(validationErrors) > 0 {
		return exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", fmt.Sprintf("Todo data validation error: %s", strings.Join(validationErrors, "; "))),
		)
	}
	return nil
}

// todoItemFromMap 从 map 构造 TodoItem
func todoItemFromMap(data map[string]any) hschema.TodoItem {
	id, _ := data["id"].(string)
	if id == "" {
		id = uuid.New().String()
	}
	content, _ := data["content"].(string)
	activeForm, _ := data["activeForm"].(string)
	description, _ := data["description"].(string)
	selectedModelID, _ := data["selected_model_id"].(string)

	item := hschema.TodoItem{
		ID:              id,
		Content:         content,
		ActiveForm:      activeForm,
		Description:     description,
		Status:          hschema.TodoStatusPending,
		SelectedModelID: selectedModelID,
	}

	if statusStr, ok := data["status"].(string); ok {
		if parsed, err := hschema.ParseTodoStatus(statusStr); err == nil {
			item.Status = parsed
		}
	}

	return item
}
