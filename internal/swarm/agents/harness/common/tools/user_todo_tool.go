// user_todo_tool.go 提供 code 模式用户个人待办工具 user_todos：
// 按 channel 隔离、markdown+frontmatter 持久化于 {workspace}/memory/user_todos/{safe_cid}.md。
// Python: jiuwenswarm/agents/harness/common/tools/user_todo_tool.py（10.6.24c）
package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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

// userTodoParamsNames 已知参数名集合（对齐 UserTodosParams dataclass 字段）。
var userTodoParamsNames = map[string]bool{
	"action": true, "channel_id": true, "todo_id": true, "title": true,
	"status": true, "priority": true, "due_at": true, "remind_at": true,
	"created_by": true, "description": true, "query": true,
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
func parseTodosFile(filePath string) []*TodoItem {
	data, err := os.ReadFile(filePath)
	if err != nil {
		// Python: os.path.exists → False 时返回 []；读失败同样容错为空
		return nil
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
			if t, ok := todoItemFromDict(current); ok {
				todos = append(todos, t)
			} else {
				logger.Warn(logComponent).Msg("Failed to parse todo item")
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
	return todos
}

// todoItemFromDict 从解析 map 构造 TodoItem（容错默认值）。
// Python: TodoItem.from_dict()
func todoItemFromDict(data map[string]string) (*TodoItem, bool) {
	if data["id"] == "" {
		return nil, false
	}
	now := time.Now().UTC().Format(time.RFC3339)
	status := TodoStatusPending
	if s := data["status"]; s != "" {
		status = TodoStatus(s)
	}
	priority := TodoPriorityMedium
	if p := data["priority"]; p != "" {
		priority = TodoPriority(p)
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
	}, true
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

// writeTodosFile 写回待办文件。
// Python: _write_todos_file()
func writeTodosFile(filePath string, todos []*TodoItem) {
	_ = os.MkdirAll(filepath.Dir(filePath), 0o755)
	var sb strings.Builder
	sb.WriteString("# User Todos\n\n")
	for _, todo := range todos {
		sb.WriteString(serializeTodoItem(todo))
	}
	_ = os.WriteFile(filePath, []byte(sb.String()), 0o644)
}
