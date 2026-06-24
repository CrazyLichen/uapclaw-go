package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestWorkflowExecutionState_常量值 测试枚举值对齐 Python
func TestWorkflowExecutionState_常量值(t *testing.T) {
	assert.Equal(t, WorkflowExecutionState("COMPLETED"), WorkflowExecutionStateCompleted)
	assert.Equal(t, WorkflowExecutionState("INPUT_REQUIRED"), WorkflowExecutionStateInputRequired)
	assert.Equal(t, WorkflowExecutionState("ERROR"), WorkflowExecutionStateError)
}

// TestWorkflowOutput_字段赋值 测试 WorkflowOutput 字段读写
func TestWorkflowOutput_字段赋值(t *testing.T) {
	wo := WorkflowOutput{
		Result: map[string]any{"key": "value"},
		State:  WorkflowExecutionStateCompleted,
	}
	assert.Equal(t, WorkflowExecutionStateCompleted, wo.State)
	assert.NotNil(t, wo.Result)
}

// TestWorkflowOutput_中断状态 测试 INPUT_REQUIRED 中断信号
func TestWorkflowOutput_中断状态(t *testing.T) {
	wo := WorkflowOutput{
		Result: nil,
		State:  WorkflowExecutionStateInputRequired,
	}
	assert.Equal(t, WorkflowExecutionStateInputRequired, wo.State)
	assert.Nil(t, wo.Result)
}
