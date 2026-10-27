package rails

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
)

// TestWithTodoModifyFactory_选项设置 验证 Option 透传到 rail 字段。
func TestWithTodoModifyFactory_选项设置(t *testing.T) {
	r := NewTaskPlanningRail()
	if r.todoModifyFactory != nil {
		t.Error("默认应为 nil")
	}
	f := func(todoTool todo.TodoTool, language, agentID string) tool.Tool { return nil }
	WithTodoModifyFactory(f)(r)
	if r.todoModifyFactory == nil {
		t.Error("WithTodoModifyFactory 未生效")
	}
}

// TestNewTaskPlanningRail_零值工厂构造 验证默认构造不因零值工厂 panic。
func TestNewTaskPlanningRail_零值工厂构造(t *testing.T) {
	r := NewTaskPlanningRail(WithTodoModifyFactory(nil))
	if r.todoModifyFactory != nil {
		t.Error("显式 nil 工厂应保持 nil")
	}
}
