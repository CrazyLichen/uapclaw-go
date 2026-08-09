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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// todoFileName 待办事项持久化文件名
	todoFileName = "todo.json"
)

// ──────────────────────────── 全局变量 ────────────────────────────

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
			exception.WithParam("reason", fmt.Sprintf("待办文件未找到: %s", filePath)),
		)
	}
	if result == nil || result.Data == nil || result.Data.Content == "" {
		// 对齐 Python L123-127: 空内容视为文件不存在
		return nil, exception.BuildError(
			exception.StatusToolTodosLoadFailed,
			exception.WithParam("reason", fmt.Sprintf("待办文件为空: %s", filePath)),
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
			exception.WithParam("reason", fmt.Sprintf("加载待办列表失败，read_file 读取失败: %s", err.Error())),
		)
	}

	items := make([]hschema.TodoItem, 0, len(rawList))
	for _, raw := range rawList {
		items = append(items, hschema.TodoItem{}.FromDict(raw))
	}
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Int("item_count", len(items)).
		Msg("LoadTodos 加载待办事项成功")
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
	data, err := json.MarshalIndent(dicts, "", "  ")
	if err != nil {
		logger.Error(logComponent).
			Str("file_path", filePath).
			Err(err).
			Msg("SaveTodos JSON 编码失败")
		return exception.BuildError(
			exception.StatusToolTodosSaveFailed,
			exception.WithParam("reason", fmt.Sprintf("保存待办列表失败，JSON 编码失败: %s", err.Error())),
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
			exception.WithParam("reason", fmt.Sprintf("保存待办列表失败，write_file 写入失败: %s", err.Error())),
		)
	}
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Int("item_count", len(todos)).
		Msg("SaveTodos 保存待办事项成功")
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
	card, _ := tools.BuildToolCard("todo_create", "TodoCreateTool", language, nil, agentID)

	fn := func(ctx context.Context, input TodoCreateInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python: TodoCreateTool.invoke 的 try/except 异常包装
		result, err := func() (map[string]any, error) {
			sessionID, err := extractSessionID(opts)
			if err != nil {
				return nil, err
			}

			// 校验输入
			if len(input.Tasks) == 0 {
				return nil, exception.BuildError(
					exception.StatusToolTodosValidationInvalid,
					exception.WithParam("reason", "'tasks' 参数为必填项，且必须为 JSON 数组"),
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
						exception.WithParam("reason", fmt.Sprintf("索引 %d 处的任务缺少 'content' 字段", i)),
					)
				}
				if task.ActiveForm == "" {
					return nil, exception.BuildError(
						exception.StatusToolTodosValidationInvalid,
						exception.WithParam("reason", fmt.Sprintf("索引 %d 处的任务缺少 'activeForm' 字段", i)),
					)
				}
				if task.Description == "" {
					return nil, exception.BuildError(
						exception.StatusToolTodosValidationInvalid,
						exception.WithParam("reason", fmt.Sprintf("索引 %d 处的任务缺少 'description' 字段", i)),
					)
				}
				if _, exists := idSet[task.ID]; exists {
					return nil, exception.BuildError(
						exception.StatusToolTodosValidationInvalid,
						exception.WithParam("reason", fmt.Sprintf("索引 %d 处的任务 ID '%s' 重复", i, task.ID)),
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
		}()
		if err != nil {
			// 对齐 Python: tool_logger.error(event_type=TOOL_CALL_ERROR) + build_error(TOOL_TODOS_INVOKE_FAILED)
			logger.Error(logComponent).Err(err).
				Str("event_type", "TOOL_CALL_ERROR").
				Str("tool_name", "todo_create").
				Msg("Todo create tool invocation failed")
			return nil, exception.BuildError(exception.StatusToolTodosInvokeFailed,
				exception.WithParam("reason", err.Error()),
			)
		}
		return result, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// NewTodoListTool 创建待办事项列表工具。
// 对齐 Python: TodoListTool.__init__
func NewTodoListTool(todoTool TodoTool, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("todo_list", "TodoListTool", language, nil, agentID)

	fn := func(ctx context.Context, _ TodoListInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python: TodoListTool.invoke 的 try/except 异常包装
		result, err := func() (map[string]any, error) {
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
		}()
		if err != nil {
			// 对齐 Python: tool_logger.error(event_type=TOOL_CALL_ERROR) + build_error(TOOL_TODOS_INVOKE_FAILED)
			logger.Error(logComponent).Err(err).
				Str("event_type", "TOOL_CALL_ERROR").
				Str("tool_name", "todo_list").
				Msg("Todo list tool invocation failed")
			return nil, exception.BuildError(exception.StatusToolTodosInvokeFailed,
				exception.WithParam("reason", err.Error()),
			)
		}
		return result, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// NewTodoGetTool 创建待办事项详情查询工具。
// 对齐 Python: TodoGetTool.__init__
func NewTodoGetTool(todoTool TodoTool, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("todo_get", "TodoGetTool", language, nil, agentID)

	fn := func(ctx context.Context, input TodoGetInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python: TodoGetTool.invoke 的 try/except 异常包装
		result, err := func() (map[string]any, error) {
			sessionID, err := extractSessionID(opts)
			if err != nil {
				return nil, err
			}

			if input.ID == "" {
				return nil, exception.BuildError(
					exception.StatusToolTodosValidationInvalid,
					exception.WithParam("reason", "任务 ID 为必填项"),
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
				exception.WithParam("reason", fmt.Sprintf("未找到 ID 为 '%s' 的任务", input.ID)),
			)
		}()
		if err != nil {
			// 对齐 Python: tool_logger.error(event_type=TOOL_CALL_ERROR) + build_error(TOOL_TODOS_INVOKE_FAILED)
			logger.Error(logComponent).Err(err).
				Str("event_type", "TOOL_CALL_ERROR").
				Str("tool_name", "todo_get").
				Msg("Todo get tool invocation failed")
			return nil, exception.BuildError(exception.StatusToolTodosInvokeFailed,
				exception.WithParam("reason", err.Error()),
			)
		}
		return result, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// NewTodoModifyTool 创建待办事项修改工具。
// 对齐 Python: TodoModifyTool.__init__
func NewTodoModifyTool(todoTool TodoTool, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("todo_modify", "TodoModifyTool", language, nil, agentID)

	fn := func(ctx context.Context, input TodoModifyInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python: TodoModifyTool.invoke 的 try/except 异常包装
		result, err := func() (map[string]any, error) {
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
				updatedTodos, msg, err = updateTodos(todos, input.Todos)
			case "delete":
				updatedTodos, msg, err = deleteTodos(todos, input.IDs)
			case "cancel":
				updatedTodos, msg, err = cancelTodos(todos, input.IDs)
			case "append":
				updatedTodos, msg, err = appendTodos(todos, input.Todos)
			case "insert_after":
				if input.TodoData == nil {
					return nil, exception.BuildError(
						exception.StatusToolTodosValidationInvalid,
						exception.WithParam("reason", "无效的插入操作输入: 'todo_data' 必须为包含 'target_id' 和 'items' 的对象"),
					)
				}
				// 对齐 Python: _validate_todo_data_structure(todo_data) — 先校验每个 item 必填字段
				for _, item := range input.TodoData.Items {
					if err := validateSingleTodoItem(item); err != nil {
						return nil, err
					}
				}
				updatedTodos, msg, err = insertAfterTodos(todos, input.TodoData.TargetID, input.TodoData.Items)
			case "insert_before":
				if input.TodoData == nil {
					return nil, exception.BuildError(
						exception.StatusToolTodosValidationInvalid,
						exception.WithParam("reason", "无效的插入操作输入: 'todo_data' 必须为包含 'target_id' 和 'items' 的对象"),
					)
				}
				// 对齐 Python: _validate_todo_data_structure(todo_data) — 先校验每个 item 必填字段
				for _, item := range input.TodoData.Items {
					if err := validateSingleTodoItem(item); err != nil {
						return nil, err
					}
				}
				updatedTodos, msg, err = insertBeforeTodos(todos, input.TodoData.TargetID, input.TodoData.Items)
			default:
				return nil, exception.BuildError(
					exception.StatusToolTodosValidationInvalid,
					exception.WithParam("reason", fmt.Sprintf("无效操作: %s", input.Action)),
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
		}()
		if err != nil {
			// 对齐 Python: tool_logger.error(event_type=TOOL_CALL_ERROR) + build_error(TOOL_TODOS_INVOKE_FAILED)
			logger.Error(logComponent).Err(err).
				Str("event_type", "TOOL_CALL_ERROR").
				Str("tool_name", "todo_modify").
				Msg("Todo modify tool invocation failed")
			return nil, exception.BuildError(exception.StatusToolTodosInvokeFailed,
				exception.WithParam("reason", err.Error()),
			)
		}
		return result, nil
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

// newTodoTool 创建待办事项工具基类。
// 对齐 Python: TodoTool.__init__
func newTodoTool(workspace string, fs sys_operation.FsOperation, lockManager *TodoLockManager) TodoTool {
	return TodoTool{
		workspace:   workspace,
		fs:          fs,
		lockManager: lockManager,
	}
}

// extractSessionID 从工具选项中提取会话 ID
func extractSessionID(opts []tool.ToolOption) (string, error) {
	callOpts := tool.NewToolCallOptions(opts...)
	session := callOpts.Session
	if session == nil {
		return "", exception.BuildError(
			exception.StatusToolTodosInvokeFailed,
			exception.WithParam("error_msg", "会话 ID 为必填项"),
		)
	}
	sessionID := session.GetSessionID()
	if sessionID == "" {
		return "", exception.BuildError(
			exception.StatusToolTodosInvokeFailed,
			exception.WithParam("error_msg", "会话 ID 为必填项"),
		)
	}
	return sessionID, nil
}

// strVal 从 map 中提取字符串值，不存在或非字符串类型时返回空字符串
func strVal(data map[string]any, key string) string {
	val, _ := data[key].(string)
	return val
}

// strValDefault 从 map 中提取字符串值，不存在或非字符串类型时返回默认值
func strValDefault(data map[string]any, key string, defaultVal string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return defaultVal
}

// uniqueIDs 对 ID 列表做去重，返回 map[string]struct{}
// 对齐 Python: delete_ids = set(ids)
func uniqueIDs(ids []string) map[string]struct{} {
	result := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		result[id] = struct{}{}
	}
	return result
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
	return strings.TrimSpace(result)
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

// updateTodos 执行 update 操作
// 对齐 Python: TodoModifyTool._update_todos L662-691
// 先就地修改 todos，修改完后调用 validateSingleInProgress 校验最终状态
func updateTodos(todos []hschema.TodoItem, updates []map[string]any) ([]hschema.TodoItem, string, error) {
	if len(updates) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "update 操作需要 'todos' 参数"),
		)
	}

	// 对齐 Python L663: 构建 todo_map 用于查找
	todoMap := make(map[string]*hschema.TodoItem, len(todos))
	for i := range todos {
		todoMap[todos[i].ID] = &todos[i]
	}

	// 对齐 Python L664-688: 逐个就地修改
	updatedCount := 0
	for _, todoData := range updates {
		todoID := strVal(todoData, "id")
		if todoID == "" {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", "批量更新失败: 缺少必填字段 'id'"),
			)
		}
		currentTodo, ok := todoMap[todoID]
		if !ok {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("批量更新失败: 未找到 ID 为 '%s' 的任务", todoID)),
			)
		}
		if _, exists := todoData["content"]; exists {
			currentTodo.Content = strVal(todoData, "content")
		}
		if _, exists := todoData["activeForm"]; exists {
			currentTodo.ActiveForm = strVal(todoData, "activeForm")
		}
		if _, exists := todoData["description"]; exists {
			currentTodo.Description = strVal(todoData, "description")
		}
		if _, exists := todoData["status"]; exists {
			parsed, err := hschema.ParseTodoStatus(strVal(todoData, "status"))
			if err != nil {
				return nil, "", exception.BuildError(
					exception.StatusToolTodosValidationInvalid,
					exception.WithParam("reason", fmt.Sprintf("无效的状态 '%s'（任务 '%s'）", strVal(todoData, "status"), todoID)),
				)
			}
			currentTodo.Status = parsed
		}
		if _, exists := todoData["selected_model_id"]; exists {
			currentTodo.SelectedModelID = strVal(todoData, "selected_model_id")
		}
		updatedCount++
	}

	// 对齐 Python L689: 先修改后校验
	if err := validateSingleInProgress(todos); err != nil {
		return nil, "", err
	}

	return todos, fmt.Sprintf("已成功更新 %d 个任务", updatedCount), nil
}

// deleteTodos 执行 delete 操作
// 对齐 Python: TodoModifyTool._delete_todos L635-647
// 入口对 ids 做去重，对齐 Python delete_ids = set(ids)
func deleteTodos(todos []hschema.TodoItem, ids []string) ([]hschema.TodoItem, string, error) {
	if len(ids) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "delete 操作的 'ids' 必须为非空的任务 ID 列表"),
		)
	}
	// 对齐 Python L638: 入口去重
	deleteIDs := uniqueIDs(ids)
	deletedCount := 0
	remainingTodos := make([]hschema.TodoItem, 0, len(todos))
	for _, item := range todos {
		if _, exists := deleteIDs[item.ID]; exists {
			deletedCount++
		} else {
			remainingTodos = append(remainingTodos, item)
		}
	}
	// 对齐 Python L644-645: 全部 ID 都不存在时的提示
	if deletedCount == 0 {
		return remainingTodos, fmt.Sprintf("No tasks deleted: None of the provided IDs (%s) were found", strings.Join(ids, ", ")), nil
	}
	// 对齐 Python L647: 用 delete_ids(set) 格式化
	idStrs := make([]string, 0, len(deleteIDs))
	for id := range deleteIDs {
		idStrs = append(idStrs, id)
	}
	return remainingTodos, fmt.Sprintf("Successfully deleted %d task(s) (IDs: %s)", deletedCount, strings.Join(idStrs, ", ")), nil
}

// cancelTodos 执行 cancel 操作
// 对齐 Python: TodoModifyTool._cancel_todos L649-660
// 不存在的 ID 静默跳过，全部不存在时返回提示消息。
func cancelTodos(todos []hschema.TodoItem, ids []string) ([]hschema.TodoItem, string, error) {
	if len(ids) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "cancel 操作的 'ids' 必须为非空的任务 ID 列表"),
		)
	}
	cancelledCount := 0
	var cancelledIDs []string
	// 对齐 Python: 遍历 todos 检查 id 是否在 ids 集合中，避免 ids 重复导致 cancelledIDs 重复
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}
	for i := range todos {
		if _, ok := idSet[todos[i].ID]; ok {
			todos[i].Status = hschema.TodoStatusCancelled
			cancelledCount++
			cancelledIDs = append(cancelledIDs, todos[i].ID)
		}
	}
	// 对齐 Python L657-658: 全部 ID 都不存在时的提示
	if cancelledCount == 0 {
		return todos, fmt.Sprintf("No tasks cancelled: None of the provided IDs (%s) were found", strings.Join(ids, ", ")), nil
	}
	return todos, fmt.Sprintf("Successfully cancelled %d task(s) (IDs: %s)", cancelledCount, strings.Join(cancelledIDs, ", ")), nil
}

// appendTodos 执行 append 操作
// 对齐 Python: TodoModifyTool._append_todos L693-707
// 先校验单项+检查ID唯一性+追加到 todos，追加完后调用 validateSingleInProgress 校验
func appendTodos(todos []hschema.TodoItem, newItems []map[string]any) ([]hschema.TodoItem, string, error) {
	if len(newItems) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "append 操作需要 'todos' 参数"),
		)
	}
	// 对齐 Python L694: 检查 ID 唯一性
	todoIDs := make(map[string]struct{}, len(todos))
	for _, item := range todos {
		todoIDs[item.ID] = struct{}{}
	}
	// 对齐 Python L695-704: 校验单项+检查ID唯一+追加
	for _, todoData := range newItems {
		if err := validateSingleTodoItem(todoData); err != nil {
			return nil, "", err
		}
		todoID := strVal(todoData, "id")
		if _, exists := todoIDs[todoID]; exists {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("批量追加失败: 任务 ID '%s' 已存在", todoID)),
			)
		}
		todoItem := todoItemFromMap(todoData)
		todos = append(todos, todoItem)
		todoIDs[todoID] = struct{}{}
	}
	// 对齐 Python L705: 先追加后校验
	if err := validateSingleInProgress(todos); err != nil {
		return nil, "", err
	}
	return todos, fmt.Sprintf("已成功追加 %d 个任务", len(newItems)), nil
}

// insertAfterTodos 执行 insert_after 操作
// 对齐 Python: TodoModifyTool._insert_after_todos L709-730
// 先校验目标任务状态+校验新任务+检查ID唯一性+构造结果列表，构造完后调用 validateSingleInProgress 校验
func insertAfterTodos(todos []hschema.TodoItem, targetID string, items []map[string]any) ([]hschema.TodoItem, string, error) {
	if targetID == "" {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "无效输入: todo_data 的 'target_id' 必须为非空字符串"),
		)
	}
	if len(items) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "无效输入: todo_data 的 'items' 必须为非空的待办对象列表"),
		)
	}

	// 对齐 Python L711-713: 校验目标任务状态
	targetIndex, err := validateTargetTaskStatus(todos, targetID, []hschema.TodoStatus{hschema.TodoStatusInProgress, hschema.TodoStatusPending})
	if err != nil {
		return nil, "", err
	}

	// 对齐 Python L714-724: 校验 ID 唯一性+构造插入列表
	existingIDs := make(map[string]struct{}, len(todos))
	for _, item := range todos {
		existingIDs[item.ID] = struct{}{}
	}
	insertTodos := make([]hschema.TodoItem, 0, len(items))
	for _, todoData := range items {
		todoID := strVal(todoData, "id")
		if _, exists := existingIDs[todoID]; exists {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("插入失败: 任务 ID '%s' 已存在", todoID)),
			)
		}
		insertTodos = append(insertTodos, todoItemFromMap(todoData))
		existingIDs[todoID] = struct{}{}
	}

	// 对齐 Python L725-727: 构造结果列表
	result := make([]hschema.TodoItem, 0, len(todos)+len(items))
	result = append(result, todos[:targetIndex+1]...)
	result = append(result, insertTodos...)
	result = append(result, todos[targetIndex+1:]...)

	// 对齐 Python L728: 先构造后校验
	if err := validateSingleInProgress(result); err != nil {
		return nil, "", err
	}

	return result, fmt.Sprintf("已成功在目标任务 (ID: '%s') 之后插入 %d 个任务", targetID, len(insertTodos)), nil
}

// insertBeforeTodos 执行 insert_before 操作
// 对齐 Python: TodoModifyTool._insert_before_todos L732-753
// 先校验目标任务状态+校验新任务+检查ID唯一性+构造结果列表，构造完后调用 validateSingleInProgress 校验
func insertBeforeTodos(todos []hschema.TodoItem, targetID string, items []map[string]any) ([]hschema.TodoItem, string, error) {
	if targetID == "" {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "无效输入: todo_data 的 'target_id' 必须为非空字符串"),
		)
	}
	if len(items) == 0 {
		return nil, "", exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "无效输入: todo_data 的 'items' 必须为非空的待办对象列表"),
		)
	}

	// 对齐 Python L734-736: 校验目标任务状态（insert_before 只允许 pending）
	targetIndex, err := validateTargetTaskStatus(todos, targetID, []hschema.TodoStatus{hschema.TodoStatusPending})
	if err != nil {
		return nil, "", err
	}

	// 对齐 Python L737-747: 校验 ID 唯一性+构造插入列表
	existingIDs := make(map[string]struct{}, len(todos))
	for _, item := range todos {
		existingIDs[item.ID] = struct{}{}
	}
	insertTodos := make([]hschema.TodoItem, 0, len(items))
	for _, todoData := range items {
		todoID := strVal(todoData, "id")
		if _, exists := existingIDs[todoID]; exists {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("插入失败: 任务 ID '%s' 已存在", todoID)),
			)
		}
		insertTodos = append(insertTodos, todoItemFromMap(todoData))
		existingIDs[todoID] = struct{}{}
	}

	// 对齐 Python L748-750: 构造结果列表
	result := make([]hschema.TodoItem, 0, len(todos)+len(items))
	result = append(result, todos[:targetIndex]...)
	result = append(result, insertTodos...)
	result = append(result, todos[targetIndex:]...)

	// 对齐 Python L751: 先构造后校验
	if err := validateSingleInProgress(result); err != nil {
		return nil, "", err
	}

	return result, fmt.Sprintf("已成功在目标任务 (ID: '%s') 之前插入 %d 个任务", targetID, len(insertTodos)), nil
}

// validateSingleInProgress 校验同一时间只能有一个 in_progress 任务
// 对齐 Python: TodoModifyTool._validate_single_in_progress L600-606
// Python 实现也是简单 sum 计数，Go 与 Python 完全对齐
func validateSingleInProgress(todos []hschema.TodoItem) error {
	inProgressCount := 0
	for _, item := range todos {
		if item.Status == hschema.TodoStatusInProgress {
			inProgressCount++
		}
	}
	if inProgressCount > 1 {
		return exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "超过一个任务被标记为 'in_progress'（仅允许一个）"),
		)
	}
	return nil
}

// validateTargetTaskStatus 校验目标任务状态是否在允许列表中，并返回目标索引
// 对齐 Python: TodoModifyTool._validate_target_task_status L579-598
// 返回目标在列表中的索引，未找到或状态不允许时返回错误
func validateTargetTaskStatus(todos []hschema.TodoItem, targetID string, allowedStatuses []hschema.TodoStatus) (int, error) {
	for idx, item := range todos {
		if item.ID == targetID {
			for _, allowed := range allowedStatuses {
				if item.Status == allowed {
					return idx, nil
				}
			}
			return -1, exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("目标任务状态 '%s' 不允许插入操作", item.Status.String())),
			)
		}
	}
	return -1, exception.BuildError(
		exception.StatusToolTodosValidationInvalid,
		exception.WithParam("reason", fmt.Sprintf("当前待办列表中未找到 ID 为 '%s' 的目标任务", targetID)),
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
			validationErrors = append(validationErrors, fmt.Sprintf("缺少必填字段: '%s'", field))
		}
	}
	// 校验 status 合法值
	if statusStr, ok := item["status"].(string); ok {
		if _, err := hschema.ParseTodoStatus(statusStr); err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("无效的状态 '%s'", statusStr))
		}
	} else if _, exists := item["status"]; exists {
		validationErrors = append(validationErrors, "无效的状态类型: 必须为字符串")
	}
	if len(validationErrors) > 0 {
		return exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", fmt.Sprintf("待办数据校验错误: %s", strings.Join(validationErrors, "; "))),
		)
	}
	return nil
}

// todoItemFromMap 从 map 构造 TodoItem
// 对齐 Python: TodoModifyTool._convert_to_todo_item L625-633
// 所有字符串字段 TrimSpace，id 为空时自动生成 uuid
func todoItemFromMap(data map[string]any) hschema.TodoItem {
	id := strings.TrimSpace(strVal(data, "id"))
	if id == "" {
		id = uuid.New().String()
	}
	content := strings.TrimSpace(strVal(data, "content"))
	activeForm := strings.TrimSpace(strValDefault(data, "activeForm", ""))
	description := strings.TrimSpace(strValDefault(data, "description", ""))
	selectedModelID := strings.TrimSpace(strValDefault(data, "selected_model_id", ""))

	item := hschema.TodoItem{
		ID:              id,
		Content:         content,
		ActiveForm:      activeForm,
		Description:     description,
		Status:          hschema.TodoStatusPending,
		SelectedModelID: selectedModelID,
	}

	if statusStr := strVal(data, "status"); statusStr != "" {
		if parsed, err := hschema.ParseTodoStatus(statusStr); err == nil {
			item.Status = parsed
		}
	}

	return item
}
