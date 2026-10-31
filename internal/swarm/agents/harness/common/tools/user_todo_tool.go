// user_todo_tool.go 提供 code 模式用户个人待办工具 user_todos：
// 按 channel 隔离、markdown+frontmatter 持久化于 {workspace}/memory/user_todos/{safe_cid}.md。
// Python: jiuwenswarm/agents/harness/common/tools/user_todo_tool.py（10.6.24c）
package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TodoStatus 待办状态枚举。
// Python: user_todo_tool.TodoStatus(str, Enum)
type TodoStatus string

// TodoPriority 待办优先级枚举。
// Python: user_todo_tool.TodoPriority(str, Enum)
type TodoPriority string

// TodoItem 用户待办事项。
// Python: user_todo_tool.TodoItem dataclass
type TodoItem struct {
	// ID 唯一标识（uuid 前 8 位）
	ID string
	// Title 标题
	Title string
	// Status 状态
	Status TodoStatus
	// Priority 优先级
	Priority TodoPriority
	// DueAt 到期时间（ISO 格式，可空）
	DueAt string
	// RemindAt 提醒时间（ISO 格式，可空）
	RemindAt string
	// CreateAt 创建时间
	CreateAt string
	// UpdateAt 更新时间
	UpdateAt string
	// CreatedBy 创建者（可空）
	CreatedBy string
	// Description 详细描述
	Description string
}

// ──────────────────────────── 枚举 ────────────────────────────

const (
	// TodoStatusPending 待处理
	TodoStatusPending TodoStatus = "pending"
	// TodoStatusInProgress 进行中
	TodoStatusInProgress TodoStatus = "in_progress"
	// TodoStatusCompleted 已完成
	TodoStatusCompleted TodoStatus = "completed"
	// TodoStatusCancelled 已取消
	TodoStatusCancelled TodoStatus = "cancelled"
)

const (
	// TodoPriorityHigh 高
	TodoPriorityHigh TodoPriority = "high"
	// TodoPriorityMedium 中
	TodoPriorityMedium TodoPriority = "medium"
	// TodoPriorityLow 低
	TodoPriorityLow TodoPriority = "low"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// userTodosToolName 工具名。
	userTodosToolName = "user_todos"
	// userTodosToolDescription 工具描述（Python @tool 逐字复制）。
	userTodosToolDescription = "Manage the user's personal todo items and schedule (meetings, reminders, plans). " +
		"Proactively invoke when the user mentions future arrangements or plans. " +
		"NOTE: This is the user's personal todo tool — do NOT use it for internal agent " +
		"task planning (use todo_create/todo_insert for internal task tracking)."
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// globalWorkspaceDir 全局 workspace 目录（Python _global_workspace_dir 默认 "."）。
	globalWorkspaceDir = "."
	// globalWorkspaceMu 保护 globalWorkspaceDir 并发读写。
	globalWorkspaceMu sync.RWMutex
	// userTodoSanitizeRe channel 清洗正则（对齐 Python [^a-zA-Z0-9_\-]）。
	userTodoSanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9_\-]`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetGlobalWorkspaceDir 设置全局 workspace 目录。
// Python: set_global_workspace_dir()
func SetGlobalWorkspaceDir(dir string) {
	globalWorkspaceMu.Lock()
	defer globalWorkspaceMu.Unlock()
	globalWorkspaceDir = dir
}

// GetGlobalWorkspaceDir 读取全局 workspace 目录（测试用）。
func GetGlobalWorkspaceDir() string {
	globalWorkspaceMu.RLock()
	defer globalWorkspaceMu.RUnlock()
	return globalWorkspaceDir
}

// GetDecoratedTools 返回 user_todos 工具列表。
// Python: get_decorated_tools() → [user_todos]
func GetDecoratedTools() []tool.Tool {
	return []tool.Tool{newUserTodosTool()}
}

// NewUserTodosTool 创建 user_todos 工具实例。
func NewUserTodosTool() tool.Tool {
	return newUserTodosTool()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getTodosDir 待办目录路径。
// Python: _get_todos_dir() → workspace/memory/user_todos
func getTodosDir() string {
	globalWorkspaceMu.RLock()
	ws := globalWorkspaceDir
	globalWorkspaceMu.RUnlock()
	return filepath.Join(ws, "memory", "user_todos")
}

// sanitizeChannelID 清洗 channel ID 防路径穿越。
// Python: _sanitize_channel_id() → re.sub(r'[^a-zA-Z0-9_\-]', '_', raw_id)
func sanitizeChannelID(rawID string) string {
	return userTodoSanitizeRe.ReplaceAllString(rawID, "_")
}

// getTodosFilePath 按 channel 定位待办文件。
// Python: _get_todos_file_path()
func getTodosFilePath(channelID string) string {
	return filepath.Join(getTodosDir(), sanitizeChannelID(channelID)+".md")
}

// parseTodosFile 解析 markdown 待办文件（frontmatter+body）。
// Python: _parse_todos_file()——lookahead 正则 RE2 不支持，改逐行状态机（偏差#2，产出一致）。
// 坏条目（非法 status/priority）对齐 Python except Exception: warn + 跳过该条，
// 不向上传播错误。
func parseTodosFile(filePath string) ([]*TodoItem, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		// Python: 仅 os.path.exists 为 False 时返回 []；其他读错误向上抛（工具回 success=false）
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取待办文件失败: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	var todos []*TodoItem
	var current map[string]string
	var body []string
	inFrontmatter := false
	flush := func() {
		if current != nil && current["id"] != "" {
			if len(body) > 0 {
				current["description"] = strings.TrimSpace(strings.Join(body, "\n"))
			}
			if t, terr := todoItemFromDict(current); terr == nil {
				todos = append(todos, t)
			} else {
				// Python L157-158: except Exception → warn + 跳过（M-04：
				// TodoStatus/TodoPriority Enum 构造对非成员抛 ValueError）
				logger.Warn(logComponent).Err(terr).Msg("Failed to parse todo item")
			}
		}
		current = nil
		body = nil
	}
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == "---" {
			if inFrontmatter {
				// frontmatter 结束，进入 body 收集
				inFrontmatter = false
				continue
			}
			// 新条目开始（Python 正则按 ^---\n 分隔）
			flush()
			current = map[string]string{}
			inFrontmatter = true
			continue
		}
		if inFrontmatter && current != nil {
			if k, v, ok := strings.Cut(trimmed, ":"); ok {
				current[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		} else if current != nil {
			// body 区：到下一个 --- 前全部累积（对齐 (?=^---\s*\n|\Z)）
			body = append(body, line)
		}
	}
	flush()
	return todos, nil
}

// todoItemFromDict 从解析 map 构造 TodoItem。
// Python: TodoItem.from_dict()——status/priority 非成员值经 TodoStatus()/
// TodoPriority() Enum 构造抛 ValueError，M-04 严格对齐：非法值返回 error。
func todoItemFromDict(data map[string]string) (*TodoItem, error) {
	if data["id"] == "" {
		return nil, fmt.Errorf("todo item missing id")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	status := TodoStatusPending
	if s := data["status"]; s != "" {
		sv, ok := parseTodoStatus(s)
		if !ok {
			return nil, fmt.Errorf("invalid status value: %q", s)
		}
		status = sv
	}
	priority := TodoPriorityMedium
	if p := data["priority"]; p != "" {
		pv, ok := parseTodoPriority(p)
		if !ok {
			return nil, fmt.Errorf("invalid priority value: %q", p)
		}
		priority = pv
	}
	return &TodoItem{
		ID:          data["id"],
		Title:       data["title"],
		Status:      status,
		Priority:    priority,
		DueAt:       data["due_at"],
		RemindAt:    data["remind_at"],
		CreateAt:    strOr(data["create_at"], now),
		UpdateAt:    strOr(data["update_at"], now),
		CreatedBy:   data["created_by"],
		Description: data["description"],
	}, nil
}

// parseTodoStatus 校验并解析 status（M-04 门卫，对齐 Python TodoStatus()
// Enum 构造对非成员抛 ValueError——Go 显式校验拒绝非法值）。
func parseTodoStatus(s string) (TodoStatus, bool) {
	switch s {
	case string(TodoStatusPending):
		return TodoStatusPending, true
	case string(TodoStatusInProgress):
		return TodoStatusInProgress, true
	case string(TodoStatusCompleted):
		return TodoStatusCompleted, true
	case string(TodoStatusCancelled):
		return TodoStatusCancelled, true
	}
	return TodoStatus(""), false
}

// parseTodoPriority 校验并解析 priority（M-04 门卫，对齐 Python
// TodoPriority() Enum 构造语义）。
func parseTodoPriority(s string) (TodoPriority, bool) {
	switch s {
	case string(TodoPriorityHigh):
		return TodoPriorityHigh, true
	case string(TodoPriorityMedium):
		return TodoPriorityMedium, true
	case string(TodoPriorityLow):
		return TodoPriorityLow, true
	}
	return TodoPriority(""), false
}

// strOr 空串回退。
func strOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// serializeTodoItem 序列化单条待办为 markdown frontmatter 块。
// Python: _serialize_todo_item()——逐行 key: value，空可选字段输出 "key: "
func serializeTodoItem(todo *TodoItem) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("id: " + todo.ID + "\n")
	sb.WriteString("title: " + todo.Title + "\n")
	sb.WriteString("status: " + string(todo.Status) + "\n")
	sb.WriteString("priority: " + string(todo.Priority) + "\n")
	sb.WriteString("due_at: " + todo.DueAt + "\n")
	sb.WriteString("remind_at: " + todo.RemindAt + "\n")
	sb.WriteString("create_at: " + todo.CreateAt + "\n")
	sb.WriteString("update_at: " + todo.UpdateAt + "\n")
	sb.WriteString("created_by: " + todo.CreatedBy + "\n")
	sb.WriteString("---\n")
	if todo.Description != "" {
		sb.WriteString(todo.Description + "\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// writeTodosFile 写回待办文件（同目录临时文件 + rename 原子替换）。
//
// Python: _write_todos_file() —— 写失败会抛异常，工具层回 success=false。
// Go 侧必须同样上抛：原实现 `_ = os.WriteFile` 会让磁盘写失败仍向模型报成功。
// 原子替换避免并发读方看到半截文件。
func writeTodosFile(filePath string, todos []*TodoItem) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建待办目录失败: %w", err)
	}
	var sb strings.Builder
	sb.WriteString("# User Todos\n\n")
	for _, todo := range todos {
		sb.WriteString(serializeTodoItem(todo))
	}
	tmp, err := os.CreateTemp(dir, ".user_todos-*.tmp")
	if err != nil {
		return fmt.Errorf("创建待办临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.WriteString(sb.String()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入待办临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("刷盘待办临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭待办临时文件失败: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("设置待办文件权限失败: %w", err)
	}
	if err := os.Rename(tmpName, filePath); err != nil {
		return fmt.Errorf("替换待办文件失败: %w", err)
	}
	return nil
}

// todosFileLocks 进程内 per-path 互斥表（读-改-写串行化）。
//
// Python 侧读改写之间没有 await，单事件循环下不会交错；Go 的 goroutine 真并行
// （网关并发会话 / cron / 团队子代理都命中同一 <channel>.md），必须显式加锁。
var todosFileLocks sync.Map // map[string]*sync.Mutex

// lockTodosFile 获取文件级互斥，返回解锁函数。
func lockTodosFile(filePath string) func() {
	v, _ := todosFileLocks.LoadOrStore(filePath, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// newUserTodosTool 构造 MapFunction 工具卡。
// Python: @tool(name="user_todos", description=...)
func newUserTodosTool() tool.Tool {
	strParam := func(name, desc string, required bool) *schema.Param {
		return &schema.Param{Name: name, Description: desc, Type: schema.ParamTypeString, Required: required}
	}
	card := tool.NewToolCard(
		userTodosToolName,
		userTodosToolDescription,
		[]*schema.Param{
			strParam("action", "Operation type, one of: list, get, create, update, delete, search", true),
			strParam("channel_id", "Channel ID for per-channel isolation (defaults to global channel_id)", false),
			strParam("todo_id", "Todo item ID (for get, update, delete)", false),
			strParam("title", "Todo title (for create, update)", false),
			strParam("status", "Status (pending, in_progress, completed, cancelled)", false),
			strParam("priority", "Priority (high, medium, low)", false),
			strParam("due_at", "Due time (ISO format)", false),
			strParam("remind_at", "Reminder time (ISO format)", false),
			strParam("created_by", "Creator", false),
			strParam("description", "Detailed description", false),
			strParam("query", "Search keyword (for search action)", false),
		},
		nil,
	)
	mf, _ := tool.NewMapFunction(card, handleUserTodos, nil)
	return mf
}

// resolveUserTodoChannel 解析待办 channel：params > ctx(cron 绑定) > "default"。
// Python: cid = params.channel_id or _ctx_channel_id.get()（偏差#5：contextvar→ctx values）
func resolveUserTodoChannel(ctx context.Context, paramsChannelID string) string {
	if paramsChannelID != "" {
		return paramsChannelID
	}
	if rcc := cronbridge.FromRuntimeCronContext(ctx); rcc != nil && rcc.ChannelID != "" {
		return rcc.ChannelID
	}
	return "default"
}

// resolveUserTodoCreatedBy 解析创建者：params > ctx > ""。
// Python: params.created_by or _ctx_created_by.get()（jiuwenswarm 无 set 调用点，ctx 恒默认 ""）
func resolveUserTodoCreatedBy(ctx context.Context, paramsCreatedBy string) string {
	if paramsCreatedBy != "" {
		return paramsCreatedBy
	}
	return ""
}

// todoStr 取字符串参数（缺省 ""）。
func todoStr(inputs map[string]any, key string) string {
	if v, ok := inputs[key].(string); ok {
		return v
	}
	return ""
}

// handleUserTodos 处理用户待办六 action。
// Python: _handle_user_todos()——整体 try/except → {"success": false, "error": ...}
func handleUserTodos(ctx context.Context, inputs map[string]any) (result map[string]any, err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Any("panic", r).Msg("user_todos failed")
			result = map[string]any{"success": false, "error": fmt.Sprintf("%v", r)}
			err = nil
		}
	}()
	fail := func(msg string) map[string]any {
		return map[string]any{"success": false, "error": msg}
	}

	cid := resolveUserTodoChannel(ctx, todoStr(inputs, "channel_id"))
	filePath := getTodosFilePath(cid)
	action := todoStr(inputs, "action")

	switch action {
	case "list":
		todos, readErr := parseTodosFile(filePath)
		if readErr != nil {
			return fail(readErr.Error()), nil
		}
		out := make([]map[string]any, 0, len(todos))
		for _, t := range todos {
			out = append(out, todoToDict(t))
		}
		return map[string]any{"success": true, "channel_id": cid, "todos": out, "count": len(todos)}, nil

	case "get":
		todoID := todoStr(inputs, "todo_id")
		if todoID == "" {
			return fail("todo_id is required for get action"), nil
		}
		todos, readErr := parseTodosFile(filePath)
		if readErr != nil {
			return fail(readErr.Error()), nil
		}
		for _, t := range todos {
			if t.ID == todoID {
				return map[string]any{"success": true, "todo": todoToDict(t)}, nil
			}
		}
		return fail("Todo not found: " + todoID), nil

	case "create":
		title := todoStr(inputs, "title")
		if title == "" {
			return fail("title is required for create action"), nil
		}
		// read-modify-write 全程持 per-path 锁：并发 create/update 否则会丢更新
		unlock := lockTodosFile(filePath)
		defer unlock()
		todos, readErr := parseTodosFile(filePath)
		if readErr != nil {
			return fail(readErr.Error()), nil
		}
		// remind_at 推算：due_at 且无 remind_at → due-5min；解析失败 → ""
		finalRemindAt := todoStr(inputs, "remind_at")
		dueAt := todoStr(inputs, "due_at")
		if dueAt != "" && finalRemindAt == "" {
			if due, perr := time.Parse(time.RFC3339, dueAt); perr == nil {
				finalRemindAt = due.Add(-5 * time.Minute).Format(time.RFC3339)
			} else {
				finalRemindAt = ""
			}
		}
		// M-04 门卫：status/priority 非法非空值拒绝（Python TodoStatus()
		// 抛 ValueError → 外层 except → {"success": false}）
		status := TodoStatusPending
		if s := todoStr(inputs, "status"); s != "" {
			sv, ok := parseTodoStatus(s)
			if !ok {
				return fail("invalid status value: " + s), nil
			}
			status = sv
		}
		priority := TodoPriorityMedium
		if p := todoStr(inputs, "priority"); p != "" {
			pv, ok := parseTodoPriority(p)
			if !ok {
				return fail("invalid priority value: " + p), nil
			}
			priority = pv
		}
		now := time.Now().UTC().Format(time.RFC3339)
		newTodo := &TodoItem{
			ID:          uuidShort8(),
			Title:       title,
			Status:      status,
			Priority:    priority,
			DueAt:       dueAt,
			RemindAt:    finalRemindAt,
			CreateAt:    now,
			UpdateAt:    now,
			CreatedBy:   resolveUserTodoCreatedBy(ctx, todoStr(inputs, "created_by")),
			Description: todoStr(inputs, "description"),
		}
		todos = append(todos, newTodo)
		if werr := writeTodosFile(filePath, todos); werr != nil {
			return fail(werr.Error()), nil
		}
		logger.Info(logComponent).Str("todo_id", newTodo.ID).Str("channel", cid).Msg("Created todo")
		return map[string]any{"success": true, "todo": todoToDict(newTodo), "message": "Todo created: " + title}, nil

	case "update":
		todoID := todoStr(inputs, "todo_id")
		if todoID == "" {
			return fail("todo_id is required for update action"), nil
		}
		unlock := lockTodosFile(filePath)
		defer unlock()
		todos, readErr := parseTodosFile(filePath)
		if readErr != nil {
			return fail(readErr.Error()), nil
		}
		var found *TodoItem
		for _, t := range todos {
			if t.ID == todoID {
				found = t
				break
			}
		}
		if found == nil {
			return fail("Todo not found: " + todoID), nil
		}
		// 存在性判断对齐 Python is not None（显式空串也更新）；
		// status/priority 非法非空值 M-04 门卫拒绝（Python TodoStatus()
		// 抛 ValueError → 外层 except → {"success": false}）；空串跳过
		// 为 Go 已知偏差#8（todoStr 无法区分缺省与空串）
		if v, ok := inputs["title"].(string); ok {
			found.Title = v
		}
		if v, ok := inputs["status"].(string); ok && v != "" {
			sv, valid := parseTodoStatus(v)
			if !valid {
				return fail("invalid status value: " + v), nil
			}
			found.Status = sv
		}
		if v, ok := inputs["priority"].(string); ok && v != "" {
			pv, valid := parseTodoPriority(v)
			if !valid {
				return fail("invalid priority value: " + v), nil
			}
			found.Priority = pv
		}
		if v, ok := inputs["due_at"].(string); ok {
			found.DueAt = v
		}
		if v, ok := inputs["remind_at"].(string); ok {
			found.RemindAt = v
		}
		if v, ok := inputs["created_by"].(string); ok {
			found.CreatedBy = v
		}
		if v, ok := inputs["description"].(string); ok {
			found.Description = v
		}
		found.UpdateAt = time.Now().UTC().Format(time.RFC3339)
		if werr := writeTodosFile(filePath, todos); werr != nil {
			return fail(werr.Error()), nil
		}
		logger.Info(logComponent).Str("todo_id", todoID).Str("channel", cid).Msg("Updated todo")
		return map[string]any{"success": true, "todo": todoToDict(found), "message": "Todo updated: " + found.Title}, nil

	case "delete":
		todoID := todoStr(inputs, "todo_id")
		if todoID == "" {
			return fail("todo_id is required for delete action"), nil
		}
		unlock := lockTodosFile(filePath)
		defer unlock()
		todos, readErr := parseTodosFile(filePath)
		if readErr != nil {
			return fail(readErr.Error()), nil
		}
		var found *TodoItem
		for _, t := range todos {
			if t.ID == todoID {
				found = t
				break
			}
		}
		if found == nil {
			return fail("Todo not found: " + todoID), nil
		}
		kept := todos[:0]
		for _, t := range todos {
			if t.ID != todoID {
				kept = append(kept, t)
			}
		}
		if werr := writeTodosFile(filePath, kept); werr != nil {
			return fail(werr.Error()), nil
		}
		logger.Info(logComponent).Str("todo_id", todoID).Str("channel", cid).Msg("Deleted todo")
		return map[string]any{"success": true, "message": "Todo deleted: " + found.Title}, nil

	case "search":
		query := todoStr(inputs, "query")
		if query == "" {
			return fail("query is required for search action"), nil
		}
		q := strings.ToLower(query)
		todos, readErr := parseTodosFile(filePath)
		if readErr != nil {
			return fail(readErr.Error()), nil
		}
		var matched []*TodoItem
		for _, t := range todos {
			if strings.Contains(strings.ToLower(t.Title), q) || strings.Contains(strings.ToLower(t.Description), q) {
				matched = append(matched, t)
			}
		}
		out := make([]map[string]any, 0, len(matched))
		for _, t := range matched {
			out = append(out, todoToDict(t))
		}
		return map[string]any{"success": true, "channel_id": cid, "query": query, "todos": out, "count": len(matched)}, nil

	default:
		return fail("Unknown action: " + action + ". Valid actions: list, get, create, update, delete, search"), nil
	}
}

// todoToDict TodoItem → map（Python to_dict，键序一致）。
func todoToDict(t *TodoItem) map[string]any {
	return map[string]any{
		"id": t.ID, "title": t.Title, "status": string(t.Status), "priority": string(t.Priority),
		"due_at": t.DueAt, "remind_at": t.RemindAt, "create_at": t.CreateAt, "update_at": t.UpdateAt,
		"created_by": t.CreatedBy, "description": t.Description,
	}
}

// uuidShort8 生成 uuid 前 8 位（对齐 Python str(uuid.uuid4())[:8]）。
func uuidShort8() string {
	u := uuid.New()
	return strings.ReplaceAll(u.String(), "-", "")[:8]
}
