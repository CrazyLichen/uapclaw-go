package todo

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// TestNewTodoModifyToolWithUpdateFn_注入生效 验证工厂注入原语的构造与卡片。
func TestNewTodoModifyToolWithUpdateFn_注入生效(t *testing.T) {
	ws := t.TempDir()
	_, todoTool := CreateTodosTool(ws, nil, "cn", "agent-1")
	fn := func(todos []hschema.TodoItem, updates []map[string]any) ([]hschema.TodoItem, string, error) {
		return todos, "injected", nil
	}
	modified := NewTodoModifyToolWithUpdateFn(todoTool, "cn", "agent-1", fn)
	if modified == nil {
		t.Fatal("NewTodoModifyToolWithUpdateFn 返回 nil")
	}
	if modified.Card().Name != "todo_modify" {
		t.Errorf("卡名异常: %s", modified.Card().Name)
	}
}

// TestNewTodoModifyToolWithUpdateFn_真实invoke走注入 验证 update 分派走注入的 updateFn。
func TestNewTodoModifyToolWithUpdateFn_真实invoke走注入(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool(t.TempDir(), fs, lockMgr)
	sessOpt := tool.WithToolSession(&mockSession{sessionID: "s1"})
	// 先创建待办事项（真实 invoke 需要 fs 中存在 todo 文件）
	createTool := NewTodoCreateTool(todoTool, "cn", "agent-1")
	_, _ = createTool.Invoke(t.Context(), map[string]any{
		"tasks": []any{
			map[string]any{"id": "x", "content": "任务", "activeForm": "a", "description": "d"},
		},
	}, sessOpt)

	fn := func(todos []hschema.TodoItem, updates []map[string]any) ([]hschema.TodoItem, string, error) {
		return todos, "injected message", nil
	}
	modified := NewTodoModifyToolWithUpdateFn(todoTool, "cn", "agent-1", fn)
	result, err := modified.Invoke(t.Context(), map[string]any{
		"action": "update", "todos": []any{map[string]any{"id": "x", "status": "pending"}},
	}, sessOpt)
	if err != nil {
		t.Fatalf("invoke err: %v", err)
	}
	if result["message"] != "injected message" {
		t.Errorf("注入 updateFn 未生效: %v", result)
	}
}

// TestNewTodoModifyToolWithUpdateFn_非update动作走原生 验证非 update 分派不被注入函数拦截。
func TestNewTodoModifyToolWithUpdateFn_非update动作走原生(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool(t.TempDir(), fs, lockMgr)
	sessOpt := tool.WithToolSession(&mockSession{sessionID: "s1"})
	createTool := NewTodoCreateTool(todoTool, "cn", "agent-1")
	_, _ = createTool.Invoke(t.Context(), map[string]any{
		"tasks": []any{
			map[string]any{"id": "x", "content": "任务", "activeForm": "a", "description": "d"},
		},
	}, sessOpt)

	fn := func(todos []hschema.TodoItem, updates []map[string]any) ([]hschema.TodoItem, string, error) {
		return todos, "should not appear", nil
	}
	modified := NewTodoModifyToolWithUpdateFn(todoTool, "cn", "agent-1", fn)
	// cancel 指定 id 走原生 cancelTodos（成功消息非注入文案）
	result, err := modified.Invoke(t.Context(), map[string]any{
		"action": "cancel", "ids": []any{"x"},
	}, sessOpt)
	if err != nil {
		t.Fatalf("invoke err: %v", err)
	}
	if result["message"] == "should not appear" {
		t.Error("cancel 分派不应走注入 fn")
	}
	if !strings.Contains(result["message"].(string), "Successfully cancelled") {
		t.Errorf("cancel 应走原生 cancelTodos: %v", result)
	}
}

// TestCreateTodosToolWithModifyFactory_替换modify项 验证工厂产物替换第 4 项 todo_modify。
func TestCreateTodosToolWithModifyFactory_替换modify项(t *testing.T) {
	ws := t.TempDir()
	factory := func(todoTool TodoTool, language, agentID string) tool.Tool {
		card, _ := tools.BuildToolCard("todo_modify", "TodoModifyTool", language, nil, agentID)
		fn := func(ctx context.Context, input TodoModifyInput, opts ...tool.ToolOption) (map[string]any, error) {
			return map[string]any{"message": "from factory"}, nil
		}
		invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
		return invokeFn
	}
	allTools, _ := CreateTodosToolWithModifyFactory(ws, nil, "cn", "agent-1", factory)
	if len(allTools) != 4 {
		t.Fatalf("期望 4 个工具，实际 %d", len(allTools))
	}
	if allTools[3].Card().Name != "todo_modify" {
		t.Errorf("第 4 个工具应为 todo_modify，实际 %s", allTools[3].Card().Name)
	}
	// 工厂产物 invoke 验证
	result, err := allTools[3].Invoke(t.Context(), map[string]any{"action": "update"}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	if result["message"] != "from factory" {
		t.Errorf("工厂产物未生效: %v", result)
	}
}

// TestCreateTodosToolWithModifyFactory_零值工厂等价原生 验证 nil 工厂走原生构造。
func TestCreateTodosToolWithModifyFactory_零值工厂等价原生(t *testing.T) {
	ws := t.TempDir()
	a, _ := CreateTodosTool(ws, nil, "cn", "agent-1")
	b, _ := CreateTodosToolWithModifyFactory(ws, nil, "cn", "agent-1", nil)
	if len(a) != len(b) || a[3].Card().Name != b[3].Card().Name {
		t.Error("零值工厂应与原生 CreateTodosTool 等价")
	}
}

// TestCancelTodos_导出包装 验证导出包装与原生 cancelTodos 行为一致。
func TestCancelTodos_导出包装(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Content: "a", Status: hschema.TodoStatusPending},
		{ID: "t2", Content: "b", Status: hschema.TodoStatusPending},
	}
	result, msg, err := CancelTodos(todos, []string{"t1"})
	if err != nil {
		t.Fatalf("CancelTodos err: %v", err)
	}
	if len(result) != 2 || result[0].Status != hschema.TodoStatusCancelled {
		t.Errorf("CancelTodos 结果异常: %v", result)
	}
	if msg == "" {
		t.Error("CancelTodos 消息为空")
	}
}
