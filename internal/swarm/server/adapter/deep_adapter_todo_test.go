package adapter

import (
	"testing"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// TestCancelPendingTodos_无实例返回nil 验证 instance nil 的防御分支。
// Python: _cancel_pending_todos (interface_deep.py:4358-4359)
func TestCancelPendingTodos_无实例返回nil(t *testing.T) {
	d := NewDeepAdapter()
	if got := d.cancelPendingTodos(t.Context(), "s1"); got != nil {
		t.Errorf("instance nil 应返回 nil, got %v", got)
	}
}

// TestFormatTodosForFrontendLocal_过滤cancelled 验证前端格式化的过滤与映射。
// 对齐 commrails.formatTodosForFrontend (stream_event_emit.go:234)
func TestFormatTodosForFrontendLocal_过滤cancelled(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "1", Content: "a", Status: hschema.TodoStatusPending},
		{ID: "2", Content: "b", Status: hschema.TodoStatusCancelled},
		{ID: "3", Content: "c", Status: hschema.TodoStatusCompleted},
	}
	got := formatTodosForFrontendLocal(todos)
	if len(got) != 2 {
		t.Fatalf("应过滤 cancelled 剩 2: %v", got)
	}
	if got[0]["status"] != "pending" || got[1]["status"] != "completed" {
		t.Error("状态映射异常")
	}
	if got[0]["id"] != "1" || got[0]["content"] != "a" || got[0]["activeForm"] != "" {
		t.Errorf("字段映射异常: %v", got[0])
	}
}

// TestFormatTodosForFrontendLocal_进行中状态 验证 in_progress 映射。
func TestFormatTodosForFrontendLocal_进行中状态(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "9", Content: "x", ActiveForm: "正在做 x", Status: hschema.TodoStatusInProgress},
	}
	got := formatTodosForFrontendLocal(todos)
	if len(got) != 1 || got[0]["status"] != "in_progress" {
		t.Fatalf("in_progress 映射异常: %v", got)
	}
	if got[0]["activeForm"] != "正在做 x" {
		t.Errorf("activeForm 异常: %v", got[0])
	}
}

// TestFormatTodosForFrontendLocal_空列表 验证空列表返回空切片非 nil。
func TestFormatTodosForFrontendLocal_空列表(t *testing.T) {
	got := formatTodosForFrontendLocal(nil)
	if len(got) != 0 {
		t.Errorf("空列表应返回空: %v", got)
	}
}
