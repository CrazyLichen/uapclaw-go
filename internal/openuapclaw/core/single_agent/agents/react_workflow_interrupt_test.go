//go:build test

package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/ability"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
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
	tc := &llmschema.ToolCall{ID: "t1", Name: "wf_tool"}
	// 命中 → 返回 ability ID（Python L2533-1535）
	assert.Equal(t, registered.AbilityID(), a.extractWorkflowID(tc))
	// miss → fallback tool_call.name（Python L2536）
	tcMiss := &llmschema.ToolCall{ID: "t2", Name: "unknown_wf"}
	assert.Equal(t, "unknown_wf", a.extractWorkflowID(tcMiss))
}

// TestAfterExecuteToolCall_WorkflowInterrupt 收集全部被中断工作流（含 pending 取首个）。
// Python: react_agent.py _after_execute_tool_call L2554-1596。
func TestAfterExecuteToolCall_WorkflowInterrupt(t *testing.T) {
	a := &ReActAgent{}
	mkInteraction := func(id string) *stream.OutputSchema {
		return &stream.OutputSchema{
			Type:    interaction.InteractionType,
			Payload: &interaction.InteractionOutput{ID: id},
		}
	}
	interruptedOut := &workflow.WorkflowOutput{
		State:  workflow.WorkflowExecutionStateInputRequired,
		Result: []stream.Schema{mkInteraction("q1")},
	}
	interruptedOut2 := &workflow.WorkflowOutput{
		State:  workflow.WorkflowExecutionStateInputRequired,
		Result: []stream.Schema{mkInteraction("q2")},
	}
	okOut := &workflow.WorkflowOutput{State: workflow.WorkflowExecutionStateCompleted}
	results := []agentschema.ExecuteResult{
		{Result: okOut},
		{Result: interruptedOut},
		{Result: interruptedOut2},
	}
	toolCalls := []*llmschema.ToolCall{{ID: "t1", Name: "wf_a"}, {ID: "t2", Name: "wf_b"}, {ID: "t3", Name: "wf_c"}}
	aiMsg := llmschema.NewAssistantMessage("calling workflows")
	st := a.afterExecuteToolCall(results, toolCalls, aiMsg, 3, "orig")
	require.NotNil(t, st)
	require.Len(t, st.InterruptedWorkflows, 2)
	// pending 取第一个被中断项（Python L2581-1585）
	assert.Equal(t, "wf_b", st.PendingWorkflowID)
	assert.Equal(t, []string{"q1"}, st.InterruptedWorkflows["wf_b"].ComponentIDs)
	assert.Equal(t, interruptedOut, st.InterruptedWorkflows["wf_b"].WorkflowExecutionState)
	assert.Equal(t, "wf_c", st.InterruptedWorkflows["wf_c"].ToolCall.Name)
	assert.Equal(t, 3, st.BaseInterruptionState.Iteration)
	assert.Equal(t, "orig", st.BaseInterruptionState.OriginalQuery)
	// 全部完成 → nil（Python L2593-1595）
	assert.Nil(t, a.afterExecuteToolCall([]agentschema.ExecuteResult{{Result: okOut}}, toolCalls[:1], aiMsg, 1, ""))
}

// TestInterruptionState_SaveLoadClearRoundtrip 中断态存取往返（真实 Session）。
// Python: react_agent.py _save/_load/_clear_interruption_state L2611-1623。
func TestInterruptionState_SaveLoadClearRoundtrip(t *testing.T) {
	a := &ReActAgent{}
	sess := session.NewSession(session.WithSessionID("test_interrupt_state"))
	require.Nil(t, a.loadInterruptionState(sess)) // 未存 → nil（Python L2617）
	st := &agentschema.InterruptionState{
		BaseInterruptionState: agentschema.BaseInterruptionState{
			AIMessage:     llmschema.NewAssistantMessage("hi"),
			Iteration:     2,
			OriginalQuery: "q",
		},
		InterruptedWorkflows: map[string]*agentschema.WorkflowInterruptEntry{
			"wf_a": {ComponentIDs: []string{"c1"}},
		},
		PendingWorkflowID:  "wf_a",
		PendingComponentID: "c1",
	}
	a.saveInterruptionState(st, sess)
	loaded := a.loadInterruptionState(sess)
	require.NotNil(t, loaded)
	assert.Equal(t, st.PendingWorkflowID, loaded.PendingWorkflowID)
	assert.Equal(t, 2, loaded.BaseInterruptionState.Iteration)
	// nil session 守卫（Python if session:）
	a.saveInterruptionState(st, nil)
	assert.Nil(t, a.loadInterruptionState(nil))
	a.clearInterruptionState(nil) // 不 panic
	a.clearInterruptionState(sess)
	assert.Nil(t, a.loadInterruptionState(sess))
}

// TestBuildInterruptResult 组装 interrupt 结果 dict。
// Python: react_agent.py _build_interrupt_result L2625-1631。
func TestBuildInterruptResult(t *testing.T) {
	a := &ReActAgent{}
	out := &workflow.WorkflowOutput{State: workflow.WorkflowExecutionStateInputRequired}
	st := &agentschema.InterruptionState{
		InterruptedWorkflows: map[string]*agentschema.WorkflowInterruptEntry{
			"wf_a": {WorkflowExecutionState: out},
		},
		PendingWorkflowID:  "wf_a",
		PendingComponentID: "q1",
	}
	res := a.buildInterruptResult(st)
	assert.Equal(t, "interrupt", res["result_type"])
	assert.Equal(t, out, res["workflow_execution_state"])
	assert.Equal(t, []string{"q1"}, res["component_ids"])
}
