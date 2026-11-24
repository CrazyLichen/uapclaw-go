//go:build test

package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/ability"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
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

// TestCommitWorkflowInterrupt 占位 ToolMessage + 存态 + invokeInputs.result。
// Python: react_agent.py _commit_interrupt L2633-1657 workflow 分支。
func TestCommitWorkflowInterrupt(t *testing.T) {
	a := &ReActAgent{}
	modelCtx := &fakeModelContext{}
	sess := session.NewSession(session.WithSessionID("test_commit_wf"))
	invokeInputs := &interfaces.InvokeInputs{}
	out := &workflow.WorkflowOutput{State: workflow.WorkflowExecutionStateInputRequired}
	st := &agentschema.InterruptionState{
		BaseInterruptionState: agentschema.BaseInterruptionState{
			AIMessage: llmschema.NewAssistantMessage("ai"),
			Iteration: 1,
		},
		InterruptedWorkflows: map[string]*agentschema.WorkflowInterruptEntry{
			"wf_a": {
				ToolCall:               &llmschema.ToolCall{ID: "t1", Name: "wf_a"},
				ComponentIDs:           []string{"q1"},
				WorkflowExecutionState: out,
			},
		},
		PendingWorkflowID:  "wf_a",
		PendingComponentID: "q1",
	}
	result, err := a.CommitWorkflowInterrupt(context.Background(), st, modelCtx, sess, invokeInputs)
	require.NoError(t, err)
	// 结果 == buildInterruptResult 输出（Python L2658-1660）
	assert.Equal(t, "interrupt", result["result_type"])
	// 占位 ToolMessage（Python L2645-1650）
	require.Len(t, modelCtx.messages, 1)
	toolMsg, ok := modelCtx.messages[0].(*llmschema.ToolMessage)
	require.True(t, ok)
	assert.Equal(t, "t1", toolMsg.ToolCallID)
	assert.Equal(t, "[INTERRUPTED - Waiting for user input]", toolMsg.Content.String())
	// invokeInputs.result 同值（Python L2660）
	assert.Equal(t, result, invokeInputs.Result)
	// 中断态已写入 session（Python L2657）
	assert.NotNil(t, a.loadInterruptionState(sess))
}

// TestHandleResumeWorkflow_步2轮转 双 workflow 只反馈第一个 → 轮转第二个并 commit。
// Python: react_agent.py _handle_resume L2679-1694 步1+步2。
func TestHandleResumeWorkflow_步2轮转(t *testing.T) {
	a := &ReActAgent{}
	modelCtx := &fakeModelContext{}
	sess := session.NewSession(session.WithSessionID("test_resume_rotate"))
	invokeInputs := &interfaces.InvokeInputs{}
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)
	mkEntry := func(name, compID string) *agentschema.WorkflowInterruptEntry {
		return &agentschema.WorkflowInterruptEntry{
			ToolCall:               &llmschema.ToolCall{ID: "t_" + name, Name: name},
			ComponentIDs:           []string{compID},
			WorkflowExecutionState: &workflow.WorkflowOutput{State: workflow.WorkflowExecutionStateInputRequired},
		}
	}
	st := &agentschema.InterruptionState{
		BaseInterruptionState: agentschema.BaseInterruptionState{
			AIMessage:     llmschema.NewAssistantMessage("ai"),
			Iteration:     2,
			OriginalQuery: "orig",
		},
		InterruptedWorkflows: map[string]*agentschema.WorkflowInterruptEntry{
			"wf_a": mkEntry("wf_a", "q1"),
			"wf_b": mkEntry("wf_b", "q2"),
		},
		PendingWorkflowID:  "wf_a",
		PendingComponentID: "q1",
	}
	result, err := a.handleResumeWorkflow(context.Background(), st, "用户反馈A", cbc, modelCtx, sess, invokeInputs)
	require.NoError(t, err)
	require.NotNil(t, result) // 仍有中断 → 返回 interrupt 结果
	assert.Equal(t, "interrupt", result["result_type"])
	// 步1：pending（wf_a）反馈已记录
	assert.NotNil(t, st.InterruptedWorkflows["wf_a"].CollectedInput)
	// 步2：pending 轮转到 wf_b
	assert.Equal(t, "wf_b", st.PendingWorkflowID)
	assert.Equal(t, "q2", st.PendingComponentID)
	// commit 副作用：invokeInputs.result 已设置、中断态已存
	assert.Equal(t, result, invokeInputs.Result)
	assert.NotNil(t, a.loadInterruptionState(sess))
	// ResumeStartIterationKey 未写入（步3 未走）
	_, ok := cbc.GetExtra(agentschema.ResumeStartIterationKey)
	assert.False(t, ok)
}

// TestHandleResumeWorkflow_步1输入补全 buildInteractiveInput 三形态。
// Python: react_agent.py _build_interactive_input L2754-1766。
func TestHandleResumeWorkflow_步1输入补全(t *testing.T) {
	a := &ReActAgent{}
	// 形态1：已是 InteractiveInput → 缺失组件补 fallback（取现有首个值）
	ii, err := interaction.NewInteractiveInput()
	require.NoError(t, err)
	ii.UserInputs["q1"] = "答案1"
	out := a.buildInteractiveInput(ii, []string{"q2", "q3"})
	got := out.(*interaction.InteractiveInput)
	assert.Equal(t, "答案1", got.UserInputs["q2"])
	assert.Equal(t, "答案1", got.UserInputs["q3"])
	// 形态2：非 InteractiveInput + 有组件 ID → 逐个绑定 str(query)
	out = a.buildInteractiveInput("纯文本反馈", []string{"q9"})
	got = out.(*interaction.InteractiveInput)
	assert.Equal(t, "纯文本反馈", got.UserInputs["q9"])
	// 形态3：非 InteractiveInput + 无组件 ID → raw_inputs
	out = a.buildInteractiveInput("纯文本反馈", nil)
	got = out.(*interaction.InteractiveInput)
	assert.Equal(t, "纯文本反馈", got.RawInputs)
}

// TestDeepCloneHelpers 深拷贝助手（D-26-2：JSON 往返等价 deepcopy）。
func TestDeepCloneHelpers(t *testing.T) {
	msg := llmschema.NewAssistantMessage("hello")
	clone := deepCloneAssistantMessage(msg)
	assert.Equal(t, msg.Content.String(), clone.Content.String())
	assert.NotSame(t, msg, clone)
	tc := &llmschema.ToolCall{ID: "t1", Name: "wf", Arguments: `{"k":"v"}`}
	tcClone := deepCloneToolCall(tc)
	assert.Equal(t, tc.ID, tcClone.ID)
	assert.Equal(t, tc.Name, tcClone.Name)
	assert.Equal(t, tc.Arguments, tcClone.Arguments)
	assert.NotSame(t, tc, tcClone)
	// nil 守卫
	assert.Nil(t, deepCloneAssistantMessage(nil))
	assert.Nil(t, deepCloneToolCall(nil))
}

// TestWriteInvokeResultToStream_WorkflowInterrupt 过滤单发（接缝5，6.12 占位回填）。
// Python: react_agent.py _write_invoke_result_to_stream L3398-1631。
func TestWriteInvokeResultToStream_WorkflowInterrupt(t *testing.T) {
	a := &ReActAgent{}
	mkSchema := func(id string) *stream.OutputSchema {
		return &stream.OutputSchema{
			Type:    interaction.InteractionType,
			Index:   1,
			Payload: &interaction.InteractionOutput{ID: id},
		}
	}
	wfOut := &workflow.WorkflowOutput{
		State:  workflow.WorkflowExecutionStateInputRequired,
		Result: []stream.Schema{mkSchema("q1"), mkSchema("q2"), mkSchema("q1")},
	}
	sess := newRecordingSessionFacade()
	// pendingID=q1 → 只写两块（过滤单发，Python L3425-1430）
	a.WriteInvokeResultToStream(context.Background(), map[string]any{
		"result_type":              "interrupt",
		"workflow_execution_state": wfOut,
		"component_ids":            []string{"q1"},
	}, sess)
	chunks := drainSessionStream(t, sess)
	require.Len(t, chunks, 2)
	for _, c := range chunks {
		assert.Equal(t, "q1", c.(*stream.OutputSchema).Payload.(*interaction.InteractionOutput).ID)
	}
	// pendingID 为空 → 全透传（Python L3421-1423）
	sess2 := newRecordingSessionFacade()
	a.WriteInvokeResultToStream(context.Background(), map[string]any{
		"result_type":              "interrupt",
		"workflow_execution_state": wfOut,
		"component_ids":            []string{},
	}, sess2)
	assert.Len(t, drainSessionStream(t, sess2), 3)
	// 非 interrupt → 正常 answer 块（既有语义不变）
	sess3 := newRecordingSessionFacade()
	a.WriteInvokeResultToStream(context.Background(), map[string]any{
		"result_type": "answer",
		"output":      "done",
	}, sess3)
	ans := drainSessionStream(t, sess3)
	require.Len(t, ans, 1)
	os := ans[0].(*stream.OutputSchema)
	assert.Equal(t, "answer", os.Type)
	assert.Equal(t, "done", os.Payload.(map[string]any)["output"])
}

// recordingSessionFacade 记录 WriteStream 的会话门面（fakeSessionFacade 边界替身扩展）。
type recordingSessionFacade struct {
	*fakeSessionFacade
	written []any
}

func newRecordingSessionFacade() *recordingSessionFacade {
	return &recordingSessionFacade{fakeSessionFacade: &fakeSessionFacade{sessionID: "test_write_stream"}}
}

func (f *recordingSessionFacade) GetState(_ state.StateKey) (any, error) { return nil, nil }

func (f *recordingSessionFacade) WriteStream(_ context.Context, schema any) error {
	f.written = append(f.written, schema)
	return nil
}

// drainSessionStream 取出已写入的流块。
func drainSessionStream(_ *testing.T, sess *recordingSessionFacade) []stream.Schema {
	var out []stream.Schema
	for _, c := range sess.written {
		if sc, ok := c.(stream.Schema); ok {
			out = append(out, sc)
		}
	}
	return out
}

// fakeWorkflowForResume D-26-5 对象旁路测试用的最小 Workflow 替身，
// 捕获最近一次 Invoke 收到的 inputs 以断言 InteractiveInput 对象形态直达。
type fakeWorkflowForResume struct {
	card       *workflow.WorkflowCard
	lastInputs any
}

func (f *fakeWorkflowForResume) Invoke(_ context.Context, inputs any, _ ...interfaces.WorkflowOption) (any, error) {
	f.lastInputs = inputs
	return map[string]any{"output": "resumed"}, nil
}

func (f *fakeWorkflowForResume) Stream(_ context.Context, _ any, _ ...interfaces.WorkflowOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema)
	close(ch)
	return ch, nil
}

func (f *fakeWorkflowForResume) Card() *workflow.WorkflowCard { return f.card }

// TestHandleResumeWorkflow_步3对象旁路与清理 步3 全收齐 → 对象经 cbc extra 直达
// workflow（InteractiveInput 指针形态，非 JSON round-trip 降级 map），执行完成后
// WorkflowResumeInputsKey 按 finally 语义清理。
// Python: react_agent.py L2707-1708（arguments 走私）+ L2696-1734（步3）。
func TestHandleResumeWorkflow_步3对象旁路与清理(t *testing.T) {
	wfCard := workflow.NewWorkflowCard(schema.WithName("wf_a"), schema.WithDescription("恢复测试"))
	fw := &fakeWorkflowForResume{card: wfCard}
	frm := resources_manager.NewResourceMgr()
	require.NoError(t, frm.AddWorkflow(wfCard, resources_manager.WorkflowProvider(
		func(_ context.Context, _ *workflow.WorkflowCard) (interfaces.Workflow, error) { return fw, nil },
	)))

	am := ability.NewAbilityManager(frm)
	am.Add(wfCard) // 注册能力卡（executeWorkflow 按 am.workflows[name] 路由）
	a := &ReActAgent{abilityManager: am}
	modelCtx := &fakeModelContext{}
	sess := session.NewSession(session.WithSessionID("test_resume_object"))
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)

	// 两个 entry 预填 CollectedInput → 步3 全收齐
	tcA := &llmschema.ToolCall{ID: "call_a", Name: "wf_a", Arguments: `{}`}
	iiA, err := interaction.NewInteractiveInput()
	require.NoError(t, err)
	iiA.UserInputs["q1"] = "答案A"
	st := &agentschema.InterruptionState{
		BaseInterruptionState: agentschema.BaseInterruptionState{
			AIMessage:     llmschema.NewAssistantMessage("ai"),
			Iteration:     1,
			OriginalQuery: "orig",
		},
		InterruptedWorkflows: map[string]*agentschema.WorkflowInterruptEntry{
			"wf_a": {
				ToolCall:               tcA,
				ComponentIDs:           []string{"q1"},
				WorkflowExecutionState: &workflow.WorkflowOutput{State: workflow.WorkflowExecutionStateInputRequired},
				CollectedInput:         iiA,
			},
		},
		PendingWorkflowID:  "wf_a",
		PendingComponentID: "q1",
	}
	result, err := a.handleResumeWorkflow(context.Background(), st, iiA, cbc, modelCtx, sess, &interfaces.InvokeInputs{})
	require.NoError(t, err)
	assert.Nil(t, result) // 全完成 → nil 续跑
	// 对象形态直达：同一 InteractiveInput 指针（非 map 降级）——D-26-5 核心断言
	require.IsType(t, &interaction.InteractiveInput{}, fw.lastInputs)
	assert.Same(t, iiA, fw.lastInputs)
	// 续跑起点已写入
	si, ok := cbc.GetExtra(agentschema.ResumeStartIterationKey)
	require.True(t, ok)
	assert.Equal(t, 2, si)
	// finally 清理：WorkflowResumeInputsKey 不残留
	_, ok = cbc.GetExtra(agentschema.WorkflowResumeInputsKey)
	assert.False(t, ok, "恢复输入 extra 应按 finally 语义清理")
}
