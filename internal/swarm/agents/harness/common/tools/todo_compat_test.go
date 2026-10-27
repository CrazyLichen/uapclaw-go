package tools

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// baseTodos 构造三任务基线。
func baseTodos() []hschema.TodoItem {
	return []hschema.TodoItem{
		{ID: "t1", Content: "任务一", Status: hschema.TodoStatusPending},
		{ID: "t2", Content: "任务二", Status: hschema.TodoStatusInProgress},
		{ID: "t3", Content: "任务三", Status: hschema.TodoStatusPending},
	}
}

// TestCompatibleUpdateTodos_deleted删除 验证 status=deleted 删除分支。
// Python: CompatibleTodoModifyTool._update_todos (todo_compat.py L25-85)
func TestCompatibleUpdateTodos_deleted删除(t *testing.T) {
	updates := []map[string]any{{"id": "t1", "status": "deleted"}}
	result, msg, err := compatibleUpdateTodos(baseTodos(), updates)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].ID != "t2" {
		t.Errorf("deleted 应移除 t1: %v", result)
	}
	if !strings.Contains(msg, "Successfully deleted 1 task(s) (IDs: t1)") {
		t.Errorf("删除消息异常: %s", msg)
	}
}

// TestCompatibleUpdateTodos_delete别名删除 验证 status=delete 同样删除。
// Python: status_value in ("deleted", "delete")
func TestCompatibleUpdateTodos_delete别名删除(t *testing.T) {
	updates := []map[string]any{{"id": "t1", "status": "delete"}}
	result, msg, err := compatibleUpdateTodos(baseTodos(), updates)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Errorf("delete 应移除 t1: %v", result)
	}
	if !strings.Contains(msg, "Successfully deleted 1 task(s)") {
		t.Errorf("删除消息异常: %s", msg)
	}
}

// TestCompatibleUpdateTodos_canceled归一为cancelled 验证 canceled 拼写归一。
// Python: if status_value == "canceled": status_value = "cancelled"
func TestCompatibleUpdateTodos_canceled归一为cancelled(t *testing.T) {
	updates := []map[string]any{{"id": "t1", "status": "canceled"}}
	result, _, err := compatibleUpdateTodos(baseTodos(), updates)
	if err != nil {
		t.Fatal(err)
	}
	if result[0].Status != hschema.TodoStatusCancelled {
		t.Errorf("canceled 应归一 cancelled: %v", result[0].Status)
	}
}

// TestCompatibleUpdateTodos_混合批量 验证删除+就地更新+状态更新混合。
func TestCompatibleUpdateTodos_混合批量(t *testing.T) {
	updates := []map[string]any{
		{"id": "t1", "status": "deleted"},
		{"id": "t2", "content": "改名"},
		{"id": "t3", "status": "completed"},
	}
	result, msg, err := compatibleUpdateTodos(baseTodos(), updates)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("混合批量应剩 2: %v", result)
	}
	if !strings.Contains(msg, "Successfully updated 2 task(s)") || !strings.Contains(msg, "Successfully deleted 1 task(s)") {
		t.Errorf("混合消息异常: %s", msg)
	}
	if result[0].Content != "改名" {
		t.Error("就地更新失败")
	}
	if result[1].Status != hschema.TodoStatusCompleted {
		t.Errorf("状态更新失败: %v", result[1].Status)
	}
}

// TestCompatibleUpdateTodos_无变更 验证空更新的 "No task changes applied"。
func TestCompatibleUpdateTodos_无变更(t *testing.T) {
	_, msg, err := compatibleUpdateTodos(baseTodos(), []map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if msg != "No task changes applied" {
		t.Errorf("空更新消息异常: %s", msg)
	}
}

// TestCompatibleUpdateTodos_未知ID报错 验证未知 ID 的错误文案。
// Python: reason=f"Batch update failed: Task with ID '{todo_id}' not found"
func TestCompatibleUpdateTodos_未知ID报错(t *testing.T) {
	_, _, err := compatibleUpdateTodos(baseTodos(), []map[string]any{{"id": "nope"}})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("未知 ID 应报错: %v", err)
	}
}

// TestCompatibleUpdateTodos_缺失id报错 验证缺 id 字段的错误文案。
// Python: reason="Batch update failed: Missing required field: 'id'"
func TestCompatibleUpdateTodos_缺失id报错(t *testing.T) {
	_, _, err := compatibleUpdateTodos(baseTodos(), []map[string]any{{"content": "x"}})
	if err == nil || !strings.Contains(err.Error(), "Missing required field: 'id'") {
		t.Errorf("缺 id 应报错: %v", err)
	}
}

// TestCompatibleUpdateTodos_非法状态报错 验证非法状态值报错。
// Python: current_todo.status = TodoStatus(status_value) → ValueError
func TestCompatibleUpdateTodos_非法状态报错(t *testing.T) {
	_, _, err := compatibleUpdateTodos(baseTodos(), []map[string]any{{"id": "t1", "status": "bogus"}})
	if err == nil {
		t.Error("非法状态应报错")
	}
}

// TestCompatibleUpdateTodos_双进行中报错 验证单一进行中校验。
// Python: self._validate_single_in_progress(updated_todos)
func TestCompatibleUpdateTodos_双进行中报错(t *testing.T) {
	updates := []map[string]any{
		{"id": "t1", "status": "in_progress"},
		{"id": "t2", "status": "in_progress"},
	}
	_, _, err := compatibleUpdateTodos(baseTodos(), updates)
	if err == nil || !strings.Contains(err.Error(), "超过一个任务被标记为 'in_progress'") {
		t.Errorf("双进行中应报错: %v", err)
	}
}

// TestNewCompatibleTodoModifyTool_卡名保持todo_modify 验证兼容工具卡名。
// Python: CompatibleTodoModifyTool 继承 SDK 工具，卡名不换。
func TestNewCompatibleTodoModifyTool_卡名保持todo_modify(t *testing.T) {
	_, todoTool := todo.CreateTodosTool(t.TempDir(), nil, "cn", "a1")
	modified := NewCompatibleTodoModifyTool(todoTool, "cn", "a1")
	if modified.Card().Name != "todo_modify" {
		t.Errorf("卡名应为 todo_modify: %s", modified.Card().Name)
	}
}
