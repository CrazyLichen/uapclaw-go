package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
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

// ──────────────────────────── 8.16 WorkflowCard 测试 ────────────────────────────

// TestWorkflowChunkType_常量值 测试枚举字符串值一比一对齐 Python base.py
func TestWorkflowChunkType_常量值(t *testing.T) {
	assert.Equal(t, WorkflowChunkType("interaction"), WorkflowChunkTypeInteraction)
	assert.Equal(t, WorkflowChunkType("output"), WorkflowChunkTypeOutput)
	assert.Equal(t, WorkflowChunkType("error"), WorkflowChunkTypeError)
}

// TestGenerateWorkflowKey 测试键拼接一比一对齐 f"{workflow_id}_{workflow_version}"
func TestGenerateWorkflowKey(t *testing.T) {
	assert.Equal(t, "wf1_v2", GenerateWorkflowKey("wf1", "v2"))
	// Python f-string 空串同样产生尾下划线，逐字对齐
	assert.Equal(t, "wf1_", GenerateWorkflowKey("wf1", ""))
}

// TestWorkflowCard_构造默认值 测试 ID 自动生成 32 位 hex、Version 默认空串
func TestWorkflowCard_构造默认值(t *testing.T) {
	card := NewWorkflowCard(schema.WithName("my_wf"), schema.WithDescription("工作流"))
	assert.Len(t, card.ID, 32)
	assert.NotContains(t, card.ID, "-")
	assert.Equal(t, "my_wf", card.Name)
	assert.Equal(t, "工作流", card.Description)
	// Python version: str = ''，默认空串
	assert.Equal(t, "", card.Version)
	assert.Nil(t, card.InputParams)
}

// TestWorkflowCard_ToolInfo 测试 tool_info 对齐 Python：input_params 空时传 {}
func TestWorkflowCard_ToolInfo(t *testing.T) {
	card := NewWorkflowCard(schema.WithName("my_wf"), schema.WithDescription("工作流"))
	info := card.ToolInfo()
	assert.NotNil(t, info, "nil input_params 应得到空 map 的 ToolInfo")

	card2 := NewWorkflowCard()
	card2.InputParams = map[string]any{"a": 1}
	assert.NotNil(t, card2.ToolInfo(), "非 nil input_params 透传")
}

// TestWorkflowCard_Ability 接口 测试实现 schema.Ability 三方法
func TestWorkflowCard_Ability接口(t *testing.T) {
	card := NewWorkflowCard(schema.WithName("wf"), schema.WithDescription("d"))
	var _ schema.Ability = card
	assert.Equal(t, "wf", card.AbilityName())
	assert.Equal(t, card.ID, card.AbilityID())
	assert.Equal(t, schema.AbilityKindWorkflow, card.AbilityKind())
}

// TestWorkflowCard_CardInterface 接口 测试身份元数据三访问器
func TestWorkflowCard_CardInterface接口(t *testing.T) {
	card := NewWorkflowCard(schema.WithName("n"), schema.WithDescription("d"), schema.WithID("abc"))
	var _ schema.CardInterface = card
	assert.Equal(t, "abc", card.GetID())
	assert.Equal(t, "n", card.GetName())
	assert.Equal(t, "d", card.GetDescription())
	assert.Equal(t, "id=abc,name=n", card.String())
}

// TestWorkflowChunk_别名 测试 WorkflowChunk=stream.Schema 别名能容纳三种 stream schema
func TestWorkflowChunk_别名(t *testing.T) {
	var chunks []WorkflowChunk
	chunks = append(chunks,
		stream.OutputSchema{Type: "output"},
		stream.TraceSchema{Type: "trace"},
		stream.CustomSchema{Type: "custom"})
	assert.Len(t, chunks, 3)
}

// TestWorkflowDrawable常量 测试常量值
func TestWorkflowDrawable常量(t *testing.T) {
	assert.Equal(t, "WORKFLOW_DRAWABLE", WORKFLOW_DRAWABLE)
}
