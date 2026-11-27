package interrupt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
)

// ──────────────────── 测试辅助 ────────────────────

// fakeInterruptAgent InterruptAgent 的测试桩
type fakeInterruptAgent struct {
	ce ceinterface.ContextEngine
}

func (f *fakeInterruptAgent) ContextEngine() ceinterface.ContextEngine { return f.ce }

// ──────────────────── 导出函数 ────────────────────

func TestNewToolInterruptHandler(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	assert.NotNil(t, h)
	assert.Equal(t, InterruptionKey, h.key)
}

func TestBuildInterruptState_无中断(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)

	aiMessage := &llmschema.AssistantMessage{}
	results := []saschema.ExecuteResult{
		{Result: map[string]any{"result": "ok"}, ToolMsg: nil},
	}
	toolCalls := []*llmschema.ToolCall{{ID: "tc1", Name: "tool1"}}

	state, payloads := h.BuildInterruptState(results, toolCalls, aiMessage, 1, "query")
	assert.Nil(t, state)
	assert.Nil(t, payloads)
}

func TestBuildInterruptState_ToolInterruptException(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)

	req := &saschema.InterruptRequest{Message: "需要确认", AutoConfirmKey: "confirm_key"}
	tie := &saschema.ToolInterruptException{Request: req}
	aiMessage := &llmschema.AssistantMessage{}

	results := []saschema.ExecuteResult{
		{Result: tie, ToolMsg: nil},
	}
	toolCalls := []*llmschema.ToolCall{{ID: "tc1", Name: "tool1"}}

	state, payloads := h.BuildInterruptState(results, toolCalls, aiMessage, 2, "query")
	require.NotNil(t, state)
	assert.Equal(t, 2, state.Iteration)
	assert.Equal(t, "query", state.OriginalQuery)
	assert.Len(t, state.InterruptedTools, 1)
	assert.NotNil(t, payloads)
}

func TestBuildInterruptState_子Agent中断(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)

	subResult := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"inner_1"},
		"state": []any{
			&stream.OutputSchema{
				Type:  interaction.InteractionType,
				Index: 0,
				Payload: &interaction.InteractionOutput{
					ID: "inner_1",
					Value: &saschema.ToolCallInterruptRequest{
						Request:    &saschema.InterruptRequest{Message: "子Agent确认"},
						ToolName:   "sub_tool",
						ToolCallID: "inner_1",
					},
				},
			},
		},
	}
	aiMessage := &llmschema.AssistantMessage{}
	results := []saschema.ExecuteResult{{Result: subResult, ToolMsg: nil}}
	toolCalls := []*llmschema.ToolCall{{ID: "tc_outer", Name: "agent_tool"}}

	state, payloads := h.BuildInterruptState(results, toolCalls, aiMessage, 3, "query")
	require.NotNil(t, state)
	assert.Len(t, state.InterruptedTools, 1)
	entry, ok := state.InterruptedTools["tc_outer"]
	require.True(t, ok)
	assert.True(t, entry.IsSubAgent)
	assert.NotNil(t, payloads)
}

func TestSave_Load_Clear(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	sess := session.NewSession()

	loaded := h.Load(sess)
	assert.Nil(t, loaded)

	intState := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 5},
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{
			"tc1": {ToolCall: &llmschema.ToolCall{ID: "tc1"}},
		},
	}
	h.Save(intState, sess)
	loaded = h.Load(sess)
	require.NotNil(t, loaded)
	assert.Equal(t, 5, loaded.Iteration)

	h.Clear(sess)
	loaded = h.Load(sess)
	assert.Nil(t, loaded)
}

func TestSave_Load_nilSession(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)

	intState := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 1},
	}
	h.Save(intState, nil)
	assert.Nil(t, h.Load(nil))
	h.Clear(nil)
}

func TestCommitInterrupt(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	sess := session.NewSession()

	intState := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 1},
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{
			"tc1": {ToolCall: &llmschema.ToolCall{ID: "tc1"}},
		},
	}
	invokeInputs := &interfaces.InvokeInputs{}
	subAgentOutputs := []PayloadEntry{
		{InnerID: "inner_1", Payload: &saschema.ToolCallInterruptRequest{
			Request:    &saschema.InterruptRequest{Message: "确认"},
			ToolName:   "tool1",
			ToolCallID: "tc1",
		}},
	}

	result, err := h.CommitInterrupt(context.Background(), intState, nil, sess, invokeInputs, subAgentOutputs)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "interrupt", result["result_type"])
	assert.NotNil(t, invokeInputs.Result)

	loaded := h.Load(sess)
	require.NotNil(t, loaded)
	assert.Equal(t, 1, loaded.Iteration)
}

func TestCommitInterrupt_nilSession(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)

	intState := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 1},
	}
	result, err := h.CommitInterrupt(context.Background(), intState, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestWriteInterruptToStream(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)

	result := map[string]any{
		"state": []any{
			&stream.OutputSchema{Type: "interaction", Index: 0, Payload: "test"},
		},
	}

	err := h.WriteInterruptToStream(context.Background(), result, nil)
	assert.NoError(t, err)

	sess := session.NewSession()
	err = h.WriteInterruptToStream(context.Background(), result, sess)
	assert.NoError(t, err)
}

func TestWriteInterruptToStream_空State(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)

	result := map[string]any{"result_type": "interrupt"}
	err := h.WriteInterruptToStream(context.Background(), result, nil)
	assert.NoError(t, err)

	result2 := map[string]any{"state": "not_a_list"}
	err = h.WriteInterruptToStream(context.Background(), result2, nil)
	assert.NoError(t, err)
}

func TestHandleResume_无新中断(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	sess := session.NewSession()

	intState := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 3},
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{
			"tc1": {
				ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "tool1", Arguments: `{"arg1":"val1"}`},
				InterruptRequests: map[string]saschema.InterruptRequester{
					"tc1": &saschema.InterruptRequest{Message: "确认"},
				},
			},
		},
	}
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)

	executeFn := func(ctx context.Context, cbc *interfaces.AgentCallbackContext, toolCalls []*llmschema.ToolCall, sess sessioninterfaces.SessionFacade, modelCtx ceinterface.ModelContext) ([]saschema.ExecuteResult, error) {
		return []saschema.ExecuteResult{{Result: map[string]any{"result": "ok"}, ToolMsg: nil}}, nil
	}

	resumeCtx := &ResumeContext{
		State:           intState,
		UserInput:       "user_response",
		Ctx:             cbc,
		Session:         sess,
		InvokeInputs:    &interfaces.InvokeInputs{},
		ExecuteToolCall: executeFn,
	}

	result, err := h.HandleResume(context.Background(), resumeCtx)
	require.NoError(t, err)
	assert.Nil(t, result)
	v, _ := cbc.GetExtra(ResumeStartIterationKey)
	assert.Equal(t, 4, v)
}

func TestHandleResume_有新中断(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	sess := session.NewSession()

	req := &saschema.InterruptRequest{Message: "再次确认"}
	intState := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 2},
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{
			"tc1": {
				ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "tool1", Arguments: `{}`},
				InterruptRequests: map[string]saschema.InterruptRequester{
					"tc1": &saschema.InterruptRequest{Message: "确认"},
				},
			},
		},
	}
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)

	executeFn := func(ctx context.Context, cbc *interfaces.AgentCallbackContext, toolCalls []*llmschema.ToolCall, sess sessioninterfaces.SessionFacade, modelCtx ceinterface.ModelContext) ([]saschema.ExecuteResult, error) {
		return []saschema.ExecuteResult{{Result: &saschema.ToolInterruptException{Request: req, ToolCall: &llmschema.ToolCall{ID: "tc1"}}, ToolMsg: nil}}, nil
	}

	resumeCtx := &ResumeContext{
		State:           intState,
		UserInput:       "user_response",
		Ctx:             cbc,
		Session:         sess,
		InvokeInputs:    &interfaces.InvokeInputs{},
		ExecuteToolCall: executeFn,
	}

	result, err := h.HandleResume(context.Background(), resumeCtx)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "interrupt", result["result_type"])
}

func TestBuildInterruptResult_空Payloads(t *testing.T) {
	result := BuildInterruptResult(nil)
	require.NotNil(t, result)
	assert.Equal(t, "interrupt", result["result_type"])
	interruptIDs, _ := result["interrupt_ids"].([]string)
	assert.Empty(t, interruptIDs)
	stateOutputs, _ := result["state"].([]any)
	assert.Empty(t, stateOutputs)
}

func TestBuildInterruptResult_ToolCallInterruptRequest(t *testing.T) {
	payloads := []PayloadEntry{
		{InnerID: "inner_1", Payload: &saschema.ToolCallInterruptRequest{
			Request:    &saschema.InterruptRequest{Message: "确认"},
			ToolName:   "tool1",
			ToolCallID: "tc1",
		}},
	}
	result := BuildInterruptResult(payloads)
	require.NotNil(t, result)
	assert.Equal(t, "interrupt", result["result_type"])

	// interrupt_ids 存为 []any（对齐 Python 通用 list）——消费方统一按 []any 断言
	interruptIDs, _ := result["interrupt_ids"].([]any)
	assert.Equal(t, []any{"inner_1"}, interruptIDs)

	stateOutputs, _ := result["state"].([]any)
	assert.Len(t, stateOutputs, 1)
	os, ok := stateOutputs[0].(*stream.OutputSchema)
	require.True(t, ok)
	assert.Equal(t, interaction.InteractionType, os.Type)
}

func TestBuildInterruptResult_OutputSchema(t *testing.T) {
	os := &stream.OutputSchema{Type: interaction.InteractionType, Index: 0, Payload: "test"}
	payloads := []PayloadEntry{
		{InnerID: "inner_1", Payload: os},
	}
	result := BuildInterruptResult(payloads)
	require.NotNil(t, result)

	stateOutputs, _ := result["state"].([]any)
	assert.Len(t, stateOutputs, 1)
	assert.Equal(t, os, stateOutputs[0])
}

// ──────────────────── 非导出函数 ────────────────────

func TestIsSubAgentInterrupt_正向(t *testing.T) {
	result := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"id1"},
	}
	assert.True(t, isSubAgentInterrupt(result))
}

func TestIsSubAgentInterrupt_tuple包装(t *testing.T) {
	// isSubAgentInterrupt 现在直接接收 map[string]any，
	// 不再支持 [2]any tuple 包装（调用方已负责解包 ToolCallResult.Result）
	dict := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"id1"},
	}
	assert.True(t, isSubAgentInterrupt(dict))
}

func TestIsSubAgentInterrupt_反向(t *testing.T) {
	assert.False(t, isSubAgentInterrupt(map[string]any{"result_type": "interrupt"}))
	assert.False(t, isSubAgentInterrupt(map[string]any{"result_type": "other", "interrupt_ids": []string{}}))
	assert.False(t, isSubAgentInterrupt("string"))
	assert.False(t, isSubAgentInterrupt(42))
	assert.False(t, isSubAgentInterrupt(nil))
}

func TestHandleToolInterruptException(t *testing.T) {
	req := &saschema.InterruptRequest{Message: "需要确认", AutoConfirmKey: "auto_key"}
	tie := &saschema.ToolInterruptException{Request: req}
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "tool1"}

	interruptedTools := make(map[string]*saschema.ToolInterruptEntry)
	var payloads []PayloadEntry

	handleToolInterruptException(tie, toolCall, interruptedTools, &payloads)

	assert.Len(t, interruptedTools, 1)
	entry, ok := interruptedTools["tc1"]
	require.True(t, ok)
	assert.Equal(t, "tc1", entry.ToolCall.ID)
	assert.False(t, entry.IsSubAgent)
	assert.Len(t, entry.InterruptRequests, 1)
	assert.Equal(t, req, entry.InterruptRequests["tc1"])
	assert.Len(t, payloads, 1)
}

func TestHandleToolInterruptException_使用异常中的ToolCall(t *testing.T) {
	req := &saschema.InterruptRequest{Message: "需要确认"}
	tcInException := &llmschema.ToolCall{ID: "tc_from_exception", Name: "tool_from_exception"}
	tie := &saschema.ToolInterruptException{Request: req, ToolCall: tcInException}
	toolCall := &llmschema.ToolCall{ID: "tc_original", Name: "tool_original"}

	interruptedTools := make(map[string]*saschema.ToolInterruptEntry)
	var payloads []PayloadEntry

	handleToolInterruptException(tie, toolCall, interruptedTools, &payloads)

	// 对齐 Python _handle_tool_interrupt_exception：表键与 entry.tool_call 都是
	// 外层模型可见调用；异常携带的（inner）调用存 paused_call；payload 按 inner 上下文。
	entry, ok := interruptedTools["tc_original"]
	require.True(t, ok)
	assert.Equal(t, "tc_original", entry.ToolCall.ID)
	assert.Equal(t, "tc_from_exception", entry.PausedCall.ID)
	assert.Len(t, payloads, 1)
	assert.Equal(t, "tc_from_exception", payloads[0].InnerID)
	payloadTCIR, isTCIR := payloads[0].Payload.(*saschema.ToolCallInterruptRequest)
	require.True(t, isTCIR)
	assert.Equal(t, "tc_from_exception", payloadTCIR.ToolCallID)
}

func TestHandleSubAgentInterrupt(t *testing.T) {
	tcir := &saschema.ToolCallInterruptRequest{
		Request:    &saschema.InterruptRequest{Message: "子Agent确认", AutoConfirmKey: "sub_auto"},
		ToolName:   "sub_tool",
		ToolCallID: "inner_1",
	}
	subResult := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"inner_1"},
		"state": []any{
			&stream.OutputSchema{
				Type:  interaction.InteractionType,
				Index: 0,
				Payload: &interaction.InteractionOutput{
					ID:    "inner_1",
					Value: tcir,
				},
			},
		},
	}

	toolCall := &llmschema.ToolCall{ID: "tc_outer", Name: "agent_tool"}
	interruptedTools := make(map[string]*saschema.ToolInterruptEntry)
	var payloads []PayloadEntry

	handleSubAgentInterrupt(subResult, toolCall, interruptedTools, &payloads)

	assert.Len(t, interruptedTools, 1)
	entry, ok := interruptedTools["tc_outer"]
	require.True(t, ok)
	assert.True(t, entry.IsSubAgent)
	assert.Len(t, entry.InterruptRequests, 1)
	// 验证存储的是完整 ToolCallInterruptRequest（子类），不是截断的 InterruptRequest
	storedTCIR, ok := entry.InterruptRequests["inner_1"].(*saschema.ToolCallInterruptRequest)
	require.True(t, ok)
	assert.Equal(t, "sub_tool", storedTCIR.ToolName)
	assert.Equal(t, "inner_1", storedTCIR.ToolCallID)
	assert.Len(t, payloads, 1)
}

func TestHandleSubAgentInterrupt_tuple包装(t *testing.T) {
	subDict := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"inner_1"},
		"state":         []any{},
	}
	toolCall := &llmschema.ToolCall{ID: "tc_outer", Name: "agent_tool"}

	interruptedTools := make(map[string]*saschema.ToolInterruptEntry)
	var payloads []PayloadEntry

	// handleSubAgentInterrupt 现在直接接收 map[string]any，不再支持 [2]any tuple
	handleSubAgentInterrupt(subDict, toolCall, interruptedTools, &payloads)

	assert.Len(t, interruptedTools, 1)
	entry, ok := interruptedTools["tc_outer"]
	require.True(t, ok)
	assert.True(t, entry.IsSubAgent)
}

func TestSaveAutoConfirm(t *testing.T) {
	sess := session.NewSession()
	answers := map[string]*saschema.InterruptAnswer{
		"intr_1": {
			Owner: "rail.A",
			Kind:  saschema.InterruptKindPermission,
			Value: map[string]any{"auto_confirm": true},
			Request: &saschema.InterruptRequest{
				AutoConfirmKey: "auto_key_1",
			},
		},
	}

	saveAutoConfirm(answers, sess)

	configVal, _ := sess.GetState(sessionstate.StringKey(InterruptAutoConfirmKey))
	config, ok := configVal.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, config["auto_key_1"])
}

func TestSaveAutoConfirm_nilSession(t *testing.T) {
	saveAutoConfirm(map[string]*saschema.InterruptAnswer{}, nil)
}

func TestSaveAutoConfirm_kin不在白名单(t *testing.T) {
	// 技能演化审批期望 {"action": ...}，不应被 approval 形状的答案误开自动确认
	sess := session.NewSession()
	answers := map[string]*saschema.InterruptAnswer{
		"intr_1": {
			Kind:  saschema.InterruptKindSkillEvolutionApproval,
			Value: map[string]any{"auto_confirm": true},
			Request: &saschema.InterruptRequest{
				AutoConfirmKey: "auto_key_1",
			},
		},
	}

	saveAutoConfirm(answers, sess)

	configVal, _ := sess.GetState(sessionstate.StringKey(InterruptAutoConfirmKey))
	config, _ := configVal.(map[string]any)
	assert.NotEqual(t, true, config["auto_key_1"])
}

func TestSaveAutoConfirm_非autoConfirm形状(t *testing.T) {
	sess := session.NewSession()
	answers := map[string]*saschema.InterruptAnswer{
		"intr_1": {
			Kind:    saschema.InterruptKindPermission,
			Value:   "plain-string",
			Request: &saschema.InterruptRequest{AutoConfirmKey: "k"},
		},
	}

	saveAutoConfirm(answers, sess)

	configVal, _ := sess.GetState(sessionstate.StringKey(InterruptAutoConfirmKey))
	config, _ := configVal.(map[string]any)
	assert.NotEqual(t, true, config["k"])
}

func TestBuildSubAgentResumeToolCall(t *testing.T) {
	tc := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "agent_tool",
		Arguments: `{"arg1":"val1"}`,
	}

	result := buildSubAgentResumeToolCall(tc, "user_response")
	assert.Equal(t, "tc1", result.ID)

	var args map[string]any
	err := json.Unmarshal([]byte(result.Arguments), &args)
	require.NoError(t, err)
	assert.Equal(t, "user_response", args["query"])
	assert.Equal(t, "val1", args["arg1"])
}

func TestBuildSubAgentResumeToolCall_无效JSON(t *testing.T) {
	tc := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "agent_tool",
		Arguments: "invalid json",
	}

	result := buildSubAgentResumeToolCall(tc, "user_input")
	assert.Equal(t, "tc1", result.ID)

	var args map[string]any
	err := json.Unmarshal([]byte(result.Arguments), &args)
	require.NoError(t, err)
	assert.Equal(t, "user_input", args["query"])
}

func TestDeepCopyToolCall(t *testing.T) {
	tc := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "tool1",
		Arguments: `{"arg1":"val1"}`,
	}

	cp := deepCopyToolCall(tc)
	assert.Equal(t, "tc1", cp.ID)
	assert.Equal(t, "tool1", cp.Name)
	assert.Equal(t, `{"arg1":"val1"}`, cp.Arguments)

	cp.Name = "tool2"
	assert.Equal(t, "tool1", tc.Name)
}

func TestDeepCopyToolCall_nil(t *testing.T) {
	assert.Nil(t, deepCopyToolCall(nil))
}

func TestCollectInterrupts_混合结果(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)

	req1 := &saschema.InterruptRequest{Message: "确认1", AutoConfirmKey: "key1"}
	tie := &saschema.ToolInterruptException{Request: req1}

	subResult := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"inner_2"},
		"state": []any{
			&stream.OutputSchema{
				Type:  interaction.InteractionType,
				Index: 0,
				Payload: &interaction.InteractionOutput{
					ID: "inner_2",
					Value: &saschema.ToolCallInterruptRequest{
						Request:    &saschema.InterruptRequest{Message: "子Agent确认"},
						ToolName:   "sub_tool",
						ToolCallID: "inner_2",
					},
				},
			},
		},
	}

	results := []saschema.ExecuteResult{
		{Result: tie, ToolMsg: nil},
		{Result: map[string]any{}, ToolMsg: nil},
		{Result: subResult, ToolMsg: nil},
	}
	toolCalls := []*llmschema.ToolCall{
		{ID: "tc1", Name: "tool1"},
		{ID: "tc2", Name: "tool2"},
		{ID: "tc3", Name: "agent_tool"},
	}

	interruptedTools, payloads := h.collectInterrupts(results, toolCalls, nil)
	assert.Len(t, interruptedTools, 2)
	assert.NotEmpty(t, payloads)
}

// ──────────────────────────── held / 定向恢复键 / deferred images ────────────────────────────

// TestIsWaitingForClient 验证 client tool 中断且无人回答时保持暂停。
// Python: _is_waiting_for_client（handler.py:395-401）
func TestIsWaitingForClient(t *testing.T) {
	// client tool 中断、未回答 → held
	clientEntry := &saschema.ToolInterruptEntry{
		ToolCall:   &llmschema.ToolCall{ID: "outer1", Name: "client_tool"},
		PausedCall: &llmschema.ToolCall{ID: "outer1", Name: "client_tool"},
		InterruptRequests: map[string]saschema.InterruptRequester{
			"outer1": saschema.NewClientToolRequest("client_tool", map[string]any{}, map[string]any{}),
		},
	}
	clientReq := clientEntry.InterruptRequests["outer1"]
	saschema.IssueInterruptRequesterID(clientReq, "outer1", nil)
	assert.True(t, isWaitingForClient(clientEntry, map[string]*saschema.InterruptAnswer{}))

	// 回答了 → 不再 held
	fresh := map[string]*saschema.InterruptAnswer{
		clientReq.GetInterruptID(): {Owner: saschema.ClientToolInterruptOwner},
	}
	assert.False(t, isWaitingForClient(clientEntry, fresh))

	// 子 Agent 条目 → 恒 false
	sub := &saschema.ToolInterruptEntry{IsSubAgent: true, InterruptRequests: map[string]saschema.InterruptRequester{
		"i": saschema.NewClientToolRequest("t", nil, nil),
	}}
	assert.False(t, isWaitingForClient(sub, map[string]*saschema.InterruptAnswer{}))

	// 非 client tool owner → false
	perm := &saschema.ToolInterruptEntry{
		InterruptRequests: map[string]saschema.InterruptRequester{
			"i": &saschema.InterruptRequest{Message: "确认", InterruptOwner: "perm.rail"},
		},
	}
	saschema.IssueInterruptRequesterID(perm.InterruptRequests["i"], "i", nil)
	assert.False(t, isWaitingForClient(perm, map[string]*saschema.InterruptAnswer{}))
}

// TestResumeUserInputKeysForState 验证定向恢复键收集与泛化回退。
// Python: _resume_user_input_keys_for_state（handler.py:430-447）
func TestResumeUserInputKeysForState(t *testing.T) {
	// 带 metadata 定向键 + 无定向键请求并存 → 两键都注入
	reqDirected := &saschema.InterruptRequest{Message: "a", Metadata: map[string]any{"resume_user_input_key": "_host_key"}}
	reqPlain := &saschema.InterruptRequest{Message: "b"}
	saschema.IssueInterruptRequesterID(reqDirected, "c1", nil)
	saschema.IssueInterruptRequesterID(reqPlain, "c2", nil)
	state := &saschema.ToolInterruptionState{InterruptedTools: map[string]*saschema.ToolInterruptEntry{
		"outer": {ToolCall: &llmschema.ToolCall{ID: "outer"},
			InterruptRequests: map[string]saschema.InterruptRequester{"c1": reqDirected, "c2": reqPlain}},
	}}
	keys := resumeUserInputKeysForState(state)
	assert.ElementsMatch(t, []string{"_host_key", ResumeUserInputKey}, keys)

	// 全部无定向键 → 仅泛化键
	plainState := &saschema.ToolInterruptionState{InterruptedTools: map[string]*saschema.ToolInterruptEntry{
		"outer": {ToolCall: &llmschema.ToolCall{ID: "outer"},
			InterruptRequests: map[string]saschema.InterruptRequester{"c2": reqPlain}},
	}}
	assert.Equal(t, []string{ResumeUserInputKey}, resumeUserInputKeysForState(plainState))
}

// TestMergeHeldCalls 验证 held 调用按原样合回、payload 用 paused_call 重建。
// Python: _merge_held_calls（handler.py:403-428）
func TestMergeHeldCalls(t *testing.T) {
	heldEntry := &saschema.ToolInterruptEntry{
		ToolCall:   &llmschema.ToolCall{ID: "outer1", Name: "client_tool"},
		PausedCall: &llmschema.ToolCall{ID: "outer1", Name: "client_tool", Arguments: `{"final":true}`},
		InterruptRequests: map[string]saschema.InterruptRequester{
			"outer1": saschema.NewClientToolRequest("client_tool", map[string]any{}, map[string]any{}),
		},
	}
	heldReq := heldEntry.InterruptRequests["outer1"]
	saschema.IssueInterruptRequesterID(heldReq, "outer1", nil)
	state := &saschema.ToolInterruptionState{InterruptedTools: map[string]*saschema.ToolInterruptEntry{
		"outer1": heldEntry,
		"outer2": {ToolCall: &llmschema.ToolCall{ID: "outer2"}, IsSubAgent: true,
			InterruptRequests: map[string]saschema.InterruptRequester{}},
	}}
	replayed := map[string]*saschema.ToolInterruptEntry{"outer2": state.InterruptedTools["outer2"]}
	replayPayloads := []PayloadEntry{{InnerID: "inner2", Payload: "sub-payload"}}

	merged, payloads := mergeHeldCalls(state, map[string]*saschema.ToolInterruptEntry{"outer1": heldEntry}, replayed, replayPayloads)
	assert.Len(t, merged, 2)
	// held payload 重建为 ToolCallInterruptRequest，上下文取 paused_call
	found := false
	for _, p := range payloads {
		if tcir, ok := p.Payload.(*saschema.ToolCallInterruptRequest); ok {
			found = true
			assert.Equal(t, "outer1", p.InnerID)
			assert.Equal(t, "client_tool", tcir.ToolName)
			assert.Equal(t, `{"final":true}`, tcir.ToolArgs)
		}
	}
	assert.True(t, found, "held payload 应重建为 ToolCallInterruptRequest")
}

// TestHandleResume_clientTool无人回答保持暂停 验证 held 语义整条链：
// 不重放 held 调用、TOOL_CALLS_STILL_PAUSED 标记注入与清理、payload 用
// paused_call 重建、中断 id 沿用。
// Python: handle_resume（handler.py:317-393）
func TestHandleResume_clientTool无人回答保持暂停(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	sess := session.NewSession()

	clientReq := saschema.NewClientToolRequest("client_tool", map[string]any{}, map[string]any{})
	entry := &saschema.ToolInterruptEntry{
		ToolCall:          &llmschema.ToolCall{ID: "outer1", Name: "client_tool"},
		PausedCall:        &llmschema.ToolCall{ID: "outer1", Name: "client_tool", Arguments: `{"q":1}`},
		InterruptRequests: map[string]saschema.InterruptRequester{"outer1": clientReq},
	}
	saschema.IssueInterruptRequesterID(clientReq, "outer1", nil)
	previousID := clientReq.GetInterruptID()

	// 第二个调用：permission 中断，本轮用 InteractiveInput.answer 回答 → 会重放
	permReq := &saschema.InterruptRequest{Message: "允许吗", InterruptOwner: "perm.rail"}
	saschema.IssueInterruptRequesterID(permReq, "tc2", nil)
	permEntry := &saschema.ToolInterruptEntry{
		ToolCall:          &llmschema.ToolCall{ID: "tc2", Name: "bash"},
		PausedCall:        &llmschema.ToolCall{ID: "tc2", Name: "bash"},
		InterruptRequests: map[string]saschema.InterruptRequester{"tc2": permReq},
	}
	intState := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 1},
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{
			"outer1": entry,
			"tc2":    permEntry,
		},
		DeferredToolImages: []map[string]any{{"image": "img1"}},
	}
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)

	var executedIDs []string
	var stillPausedDuringExecute bool
	executeFn := func(ctx context.Context, cbc *interfaces.AgentCallbackContext, toolCalls []*llmschema.ToolCall, sess sessioninterfaces.SessionFacade, modelCtx ceinterface.ModelContext) ([]saschema.ExecuteResult, error) {
		for _, tc := range toolCalls {
			executedIDs = append(executedIDs, tc.ID)
		}
		_, stillPausedDuringExecute = cbc.GetExtra(saschema.ToolCallsStillPausedKey)
		return []saschema.ExecuteResult{{Result: map[string]any{"result": "ok"}, ToolMsg: nil}}, nil
	}

	// InteractiveInput 只回答 permission 调用；client tool 无人回答 → held
	interactive, err0 := interaction.NewInteractiveInput()
	require.NoError(t, err0)
	require.NoError(t, interactive.Answer(permReq.GetInterruptID(), map[string]any{"approved": true}))

	resumeCtx := &ResumeContext{
		State:           intState,
		UserInput:       interactive,
		Ctx:             cbc,
		Session:         sess,
		InvokeInputs:    &interfaces.InvokeInputs{},
		ExecuteToolCall: executeFn,
	}

	result, err := h.HandleResume(context.Background(), resumeCtx)
	require.NoError(t, err)
	// held 调用不重放；permission 调用重放
	assert.Equal(t, []string{"tc2"}, executedIDs)
	// 恢复期间打了 still-paused 标记
	assert.True(t, stillPausedDuringExecute)
	// 仍有中断（held 的 client tool）→ commit，payload 为 held 重建结果
	require.NotNil(t, result)
	assert.Equal(t, "interrupt", result["result_type"])
	interruptIDs, _ := result["interrupt_ids"].([]any)
	require.Len(t, interruptIDs, 1)
	// interrupt_ids 装的是 inner call id（对齐 Python payloads 的 (inner_id, payload)）
	assert.Equal(t, "outer1", interruptIDs[0])
	// 重建 payload 的请求保留原 interrupt_id（宿主手里的 id 一直有效）
	stateOutputs, _ := result["state"].([]any)
	require.Len(t, stateOutputs, 1)
	outputSchema, _ := stateOutputs[0].(*stream.OutputSchema)
	require.NotNil(t, outputSchema)
	interactionOutput, _ := outputSchema.Payload.(*interaction.InteractionOutput)
	require.NotNil(t, interactionOutput)
	rebuiltTCIR, _ := interactionOutput.Value.(*saschema.ToolCallInterruptRequest)
	require.NotNil(t, rebuiltTCIR)
	assert.Equal(t, previousID, rebuiltTCIR.Request.GetInterruptID())
	assert.Equal(t, `{"q":1}`, rebuiltTCIR.ToolArgs)
	// deferred images 保留到 state
	assert.Len(t, intState.DeferredToolImages, 1)
	// 清理完成
	_, ok := cbc.GetExtra(saschema.ToolCallsStillPausedKey)
	assert.False(t, ok)
	_, ok = cbc.GetExtra(saschema.DeferredToolImagesKey)
	assert.False(t, ok)
	_, ok = cbc.GetExtra(ResumeUserInputKey)
	assert.False(t, ok)
}

// TestHandleResume_deferredImages无新中断清空 验证无新中断时 deferred images 清空。
func TestHandleResume_deferredImages无新中断清空(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	sess := session.NewSession()

	req := &saschema.InterruptRequest{Message: "确认"}
	saschema.IssueInterruptRequesterID(req, "tc1", nil)
	entry := &saschema.ToolInterruptEntry{
		ToolCall:          &llmschema.ToolCall{ID: "tc1", Name: "tool1"},
		InterruptRequests: map[string]saschema.InterruptRequester{"tc1": req},
	}
	intState := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 1},
		InterruptedTools:      map[string]*saschema.ToolInterruptEntry{"tc1": entry},
		DeferredToolImages:    []map[string]any{{"image": "img1"}},
	}
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)
	executeFn := func(ctx context.Context, cbc *interfaces.AgentCallbackContext, toolCalls []*llmschema.ToolCall, sess sessioninterfaces.SessionFacade, modelCtx ceinterface.ModelContext) ([]saschema.ExecuteResult, error) {
		return []saschema.ExecuteResult{{Result: map[string]any{"result": "ok"}, ToolMsg: nil}}, nil
	}
	interactive, err0 := interaction.NewInteractiveInput()
	require.NoError(t, err0)
	require.NoError(t, interactive.Answer(req.GetInterruptID(), map[string]any{"approved": true}))
	resumeCtx := &ResumeContext{
		State: intState, UserInput: interactive, Ctx: cbc, Session: sess,
		InvokeInputs: &interfaces.InvokeInputs{}, ExecuteToolCall: executeFn,
	}

	result, err := h.HandleResume(context.Background(), resumeCtx)
	require.NoError(t, err)
	assert.Nil(t, result)
	// 无新中断 → deferred images 清空（对齐 Python else 分支 []）
	assert.Empty(t, intState.DeferredToolImages)
}

// TestHandleResume_定向恢复键注入 验证 metadata 定向键随恢复注入并在 finally 清理。
func TestHandleResume_定向恢复键注入(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	sess := session.NewSession()

	req := &saschema.InterruptRequest{Message: "确认", Metadata: map[string]any{"resume_user_input_key": "_host_key"}}
	saschema.IssueInterruptRequesterID(req, "tc1", nil)
	entry := &saschema.ToolInterruptEntry{
		ToolCall:          &llmschema.ToolCall{ID: "tc1", Name: "tool1"},
		InterruptRequests: map[string]saschema.InterruptRequester{"tc1": req},
	}
	intState := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 1},
		InterruptedTools:      map[string]*saschema.ToolInterruptEntry{"tc1": entry},
	}
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)
	var directedDuringExecute any
	var genericDuringExecute any
	executeFn := func(ctx context.Context, cbc *interfaces.AgentCallbackContext, toolCalls []*llmschema.ToolCall, sess sessioninterfaces.SessionFacade, modelCtx ceinterface.ModelContext) ([]saschema.ExecuteResult, error) {
		directedDuringExecute, _ = cbc.GetExtra("_host_key")
		genericDuringExecute, _ = cbc.GetExtra(ResumeUserInputKey)
		return []saschema.ExecuteResult{{Result: map[string]any{"result": "ok"}, ToolMsg: nil}}, nil
	}
	resumeCtx := &ResumeContext{
		State: intState, UserInput: "输入值", Ctx: cbc, Session: sess,
		InvokeInputs: &interfaces.InvokeInputs{}, ExecuteToolCall: executeFn,
	}

	_, err := h.HandleResume(context.Background(), resumeCtx)
	require.NoError(t, err)
	// 仅定向键注入（所有请求都有定向键时不暴露泛化键，对齐 Python）
	assert.Equal(t, "输入值", directedDuringExecute)
	assert.Nil(t, genericDuringExecute)
	// finally 清理
	_, ok := cbc.GetExtra("_host_key")
	assert.False(t, ok)
}

// TestSaveAutoConfirm_子类请求 验证 AskUserRequest 子类答案也能写 auto_confirm
// （D5：InterruptAnswer.Request 保留子类，经 GetAutoConfirmKey 读取）。
func TestSaveAutoConfirm_子类请求(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	sess := session.NewSession()

	askReq := &testAskUserRequest{InterruptRequest: saschema.InterruptRequest{Message: "反问", AutoConfirmKey: "ask_confirm_key"}}
	saschema.IssueInterruptRequesterID(askReq, "tc1", nil)
	answers := map[string]*saschema.InterruptAnswer{
		askReq.GetInterruptID(): {
			Owner:   askReq.GetInterruptOwner(),
			Kind:    askReq.GetInterruptKind(), // UNDECLARED，在白名单内
			Value:   map[string]any{"approved": true, "auto_confirm": true},
			Request: askReq,
		},
	}

	saveAutoConfirm(answers, sess)
	val, _ := sess.GetState(sessionstate.StringKey(saschema.InterruptAutoConfirmKey))
	config, ok := val.(map[string]any)
	require.True(t, ok)
	require.Len(t, config, 1)
	for key := range config {
		assert.Equal(t, askReq.GetAutoConfirmKey(), key)
	}
	_ = h
}

// testAskUserRequest 测试用 InterruptRequest 子类（模拟 rail 声明的子类请求；
// Python 侧 AskUserRequest 形态经 model_dump 保留扩展字段，Go 经嵌入继承多态）。
type testAskUserRequest struct {
	saschema.InterruptRequest
}

// Questions 子类扩展字段（Python: AskUserRequest.questions）。

// TestLoad_旧数据派生interruptID 验证恢复入口对无 id 的旧请求按调用派生
// intr_{call_id}，与上游 unpickle 恢复路径一致。
// Python: ToolInterruptEntry.__setstate__（state.py:75-81）
func TestLoad_旧数据派生interruptID(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	sess := session.NewSession()

	legacy := &saschema.InterruptRequest{Message: "旧数据无 id"}
	modern := &saschema.InterruptRequest{Message: "新数据有 id", InterruptID: "intr_kept"}
	entry := &saschema.ToolInterruptEntry{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "tool1"},
		InterruptRequests: map[string]saschema.InterruptRequester{
			"tc1": legacy,
			"tc2": modern,
		},
	}
	sess.UpdateState(map[string]any{h.key: &saschema.ToolInterruptionState{
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{"tc1": entry},
	}})

	st := h.Load(sess)
	require.NotNil(t, st)
	loaded := st.InterruptedTools["tc1"]
	assert.Equal(t, "intr_tc1", loaded.InterruptRequests["tc1"].GetInterruptID())
	assert.Equal(t, "intr_kept", loaded.InterruptRequests["tc2"].GetInterruptID())
}
