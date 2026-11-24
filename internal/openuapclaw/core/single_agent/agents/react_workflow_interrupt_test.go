//go:build test

package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/ability"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
)

// TestReActAgent_IsInterrupted_WorkflowOutput 判 WorkflowOutput INPUT_REQUIRED。
// Python: react_agent.py _is_interrupted L2515-1522 WorkflowOutput 分支。
func TestReActAgent_IsInterrupted_WorkflowOutput(t *testing.T) {
	a := &ReActAgent{}
	out := &workflow.WorkflowOutput{State: workflow.WorkflowExecutionStateInputRequired}
	assert.True(t, a.isInterrupted(out))
	outDone := &workflow.WorkflowOutput{State: workflow.WorkflowExecutionStateCompleted}
	assert.False(t, a.isInterrupted(outDone))
	// 非 WorkflowOutput 非 list → false（Python L2521-1522）
	assert.False(t, a.isInterrupted("plain"))
	assert.False(t, a.isInterrupted(nil))
}

// TestReActAgent_IsInterrupted_List 判 list 中 __interaction__ 块。
// Python: react_agent.py _is_interrupted L2520-1521 list 分支。
func TestReActAgent_IsInterrupted_List(t *testing.T) {
	a := &ReActAgent{}
	list := []stream.Schema{
		&stream.OutputSchema{Type: "answer"},
		&stream.OutputSchema{Type: interaction.InteractionType},
	}
	assert.True(t, a.isInterrupted(list))
	assert.False(t, a.isInterrupted([]stream.Schema{&stream.OutputSchema{Type: "answer"}}))
}

// TestReActAgent_ExtractComponentIDs_WorkflowOutput 提取 payload.id 并 sorted 稳定序。
// Python: react_agent.py _extract_component_ids L2525-1540 WorkflowOutput 分支。
func TestReActAgent_ExtractComponentIDs_WorkflowOutput(t *testing.T) {
	a := &ReActAgent{}
	mk := func(id string) *stream.OutputSchema {
		return &stream.OutputSchema{
			Type:    interaction.InteractionType,
			Payload: &interaction.InteractionOutput{ID: id},
		}
	}
	out := &workflow.WorkflowOutput{Result: []stream.Schema{mk("c2"), mk("c1"), mk("c3")}}
	ids := a.extractComponentIDs(out)
	require.Len(t, ids, 3)
	// sorted 稳定序（Python L2540 sort）
	assert.Equal(t, []string{"c1", "c2", "c3"}, ids)
	// 非 list Result → 空（Python L2537 isinstance 判定）
	assert.Empty(t, a.extractComponentIDs(&workflow.WorkflowOutput{Result: "str"}))
}

// TestReActAgent_ExtractComponentIDs_List list 版取 payload 的 component_id 键。
// Python: react_agent.py _extract_component_ids L2543-1550 list 分支。
func TestReActAgent_ExtractComponentIDs_List(t *testing.T) {
	a := &ReActAgent{}
	list := []stream.Schema{
		&stream.OutputSchema{Type: interaction.InteractionType, Payload: map[string]any{"component_id": "c9"}},
		&stream.OutputSchema{Type: interaction.InteractionType, Payload: map[string]any{"component_id": "c5"}},
	}
	assert.Equal(t, []string{"c5", "c9"}, a.extractComponentIDs(list))
	// 非交互块跳过；空输入返回空列表
	assert.Empty(t, a.extractComponentIDs([]stream.Schema{&stream.OutputSchema{Type: "answer"}}))
	assert.Empty(t, a.extractComponentIDs("plain"))
}

// TestReActAgent_ExtractWorkflowID 经 ability_manager 反查 workflow_id，miss fallback 名称。
// Python: react_agent.py _extract_workflow_id L2531-1536。
func TestReActAgent_ExtractWorkflowID(t *testing.T) {
	// 真实 AbilityManager（feedback-real-chain-testing：禁窄替身）
	a := &ReActAgent{abilityManager: ability.NewAbilityManager(nil)}
	card := tool.NewToolCard("wf_tool", "测试工作流工具", nil, nil)
	a.abilityManager.Add(card)
	registered := a.abilityManager.Get("wf_tool")
	require.NotNil(t, registered)
	tc := &schema.ToolCall{ID: "t1", Name: "wf_tool"}
	// 命中 → 返回 ability ID（Python L2533-1535）
	assert.Equal(t, registered.AbilityID(), a.extractWorkflowID(tc))
	// miss → fallback tool_call.name（Python L2536）
	tcMiss := &schema.ToolCall{ID: "t2", Name: "unknown_wf"}
	assert.Equal(t, "unknown_wf", a.extractWorkflowID(tcMiss))
}
