package rail

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInvokeInputs_EventKind 验证 InvokeInputs.EventKind() 返回 "invoke"
func TestInvokeInputs_EventKind(t *testing.T) {
	assert.Equal(t, "invoke", (&InvokeInputs{}).EventKind())
}

// TestModelCallInputs_EventKind 验证 ModelCallInputs.EventKind() 返回 "model_call"
func TestModelCallInputs_EventKind(t *testing.T) {
	assert.Equal(t, "model_call", (&ModelCallInputs{}).EventKind())
}

// TestToolCallInputs_EventKind 验证 ToolCallInputs.EventKind() 返回 "tool_call"
func TestToolCallInputs_EventKind(t *testing.T) {
	assert.Equal(t, "tool_call", (&ToolCallInputs{}).EventKind())
}

// TestTaskIterationInputs_EventKind 验证 TaskIterationInputs.EventKind() 返回 "task_iteration"
func TestTaskIterationInputs_EventKind(t *testing.T) {
	assert.Equal(t, "task_iteration", (&TaskIterationInputs{}).EventKind())
}

// TestEventInputs_接口满足 验证各 Inputs struct 编译期满足 EventInputs 接口
func TestEventInputs_接口满足(t *testing.T) {
	var _ EventInputs = (*InvokeInputs)(nil)
	var _ EventInputs = (*ModelCallInputs)(nil)
	var _ EventInputs = (*ToolCallInputs)(nil)
	var _ EventInputs = (*TaskIterationInputs)(nil)
}
