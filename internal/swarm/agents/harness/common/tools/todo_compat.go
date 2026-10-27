package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCompatibleTodoModifyTool 构造兼容版 todo_modify 工具。
// Python: CompatibleTodoModifyTool（继承 SDK TodoModifyTool 覆盖 _update_todos）
// Go 机制差异：不继承，用 NewTodoModifyToolWithUpdateFn 注入 compatibleUpdateTodos。
// 卡名保持 todo_modify（对齐 Python 子类不换名）。
//
// Python: install_todo_modify_compat_patch() 通过模块级 monkey-patch 替换
// TodoModifyTool 导出；Go 通过 DeepAdapter params.TodoModifyFactory 显式注入。
func NewCompatibleTodoModifyTool(todoTool todo.TodoTool, language, agentID string) tool.Tool {
	return todo.NewTodoModifyToolWithUpdateFn(todoTool, language, agentID, compatibleUpdateTodos)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// compatibleUpdateTodos 兼容模式批量更新。
// Python: CompatibleTodoModifyTool._update_todos (todo_compat.py L25-85)
// 语义：status ∈ {deleted, delete} → 收集删除；"canceled" → 归一 "cancelled"；
// 其余字段就地更新；最终过滤 deletedIDs → validateSingleInProgress；
// 消息 "Successfully updated N task(s)" + "Successfully deleted M task(s) (IDs: ...)"
// 或 "No task changes applied"。
// 注意：Python 版在 _update_todos 内 save_todos；Go 版外层 NewTodoModifyToolWithUpdateFn
// 统一 SaveTodos（分派结构差异，语义等价——spec 偏差 #2 同型）。
func compatibleUpdateTodos(todos []hschema.TodoItem, updates []map[string]any) ([]hschema.TodoItem, string, error) {
	if updates == nil {
		updates = []map[string]any{}
	}
	todoMap := make(map[string]*hschema.TodoItem, len(todos))
	for i := range todos {
		todoMap[todos[i].ID] = &todos[i]
	}
	deletedIDs := make(map[string]bool)
	updatedCount := 0

	for _, todoData := range updates {
		todoID := utils.StrValFromMap(todoData, "id")
		if todoID == "" {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", "Batch update failed: Missing required field: 'id'"),
			)
		}
		if _, ok := todoMap[todoID]; !ok {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("Batch update failed: Task with ID '%s' not found", todoID)),
			)
		}

		statusValue := utils.StrValFromMap(todoData, "status")
		if statusValue == "deleted" || statusValue == "delete" {
			deletedIDs[todoID] = true
			continue
		}

		currentTodo := todoMap[todoID]
		if _, exists := todoData["content"]; exists {
			currentTodo.Content = utils.StrValFromMap(todoData, "content")
		}
		if _, exists := todoData["activeForm"]; exists {
			currentTodo.ActiveForm = utils.StrValFromMap(todoData, "activeForm")
		}
		if _, exists := todoData["description"]; exists {
			currentTodo.Description = utils.StrValFromMap(todoData, "description")
		}
		if _, exists := todoData["status"]; exists {
			if statusValue == "canceled" {
				statusValue = "cancelled" // 归一（对齐 Python "canceled" → "cancelled"）
			}
			parsed, err := hschema.ParseTodoStatus(statusValue)
			if err != nil {
				return nil, "", exception.BuildError(
					exception.StatusToolTodosValidationInvalid,
					exception.WithParam("reason", fmt.Sprintf("无效的状态 '%s'（任务 '%s'）", statusValue, todoID)),
				)
			}
			currentTodo.Status = parsed
		}
		if _, exists := todoData["selected_model_id"]; exists {
			currentTodo.SelectedModelID = utils.StrValFromMap(todoData, "selected_model_id")
		}
		updatedCount++
	}

	updatedTodos := make([]hschema.TodoItem, 0, len(todos))
	for _, item := range todos {
		if !deletedIDs[item.ID] {
			updatedTodos = append(updatedTodos, item)
		}
	}
	if err := validateTodoSingleInProgress(updatedTodos); err != nil {
		return nil, "", err
	}

	var parts []string
	if updatedCount > 0 {
		parts = append(parts, fmt.Sprintf("Successfully updated %d task(s)", updatedCount))
	}
	if len(deletedIDs) > 0 {
		sorted := make([]string, 0, len(deletedIDs))
		for id := range deletedIDs {
			sorted = append(sorted, id)
		}
		sort.Strings(sorted) // Python sorted(deleted_ids)
		parts = append(parts, fmt.Sprintf("Successfully deleted %d task(s) (IDs: %s)", len(deletedIDs), strings.Join(sorted, ", ")))
	}
	if len(parts) == 0 {
		return updatedTodos, "No task changes applied", nil
	}
	return updatedTodos, strings.Join(parts, "; "), nil
}

// validateTodoSingleInProgress 单一进行中校验。
// agentcore todo 包 validateSingleInProgress 为非导出无法跨包调用，本地等价实现，
// 错误文案与 agentcore 版逐字一致（todo.go:1017 已核实）。
func validateTodoSingleInProgress(todos []hschema.TodoItem) error {
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
