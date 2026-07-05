package interrupt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeInterruptAgent InterruptAgent 的测试桩
type fakeInterruptAgent struct {
	ce ceinterface.ContextEngine
}

func (f *fakeInterruptAgent) ContextEngine() ceinterface.ContextEngine { return f.ce }

// ──────────────────────────── 导出函数 ────────────────────────────

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
	results := []agentschema.ExecuteResult{
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

	req := &InterruptRequest{Message: "需要确认", AutoConfirmKey: "confirm_key"}
	tie := &ToolInterruptException{Request: req}
	aiMessage := &llmschema.AssistantMessage{}

	results := []agentschema.ExecuteResult{
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
					Value: &ToolCallInterruptRequest{
						InterruptRequest: InterruptRequest{Message: "子Agent确认"},
						ToolName:         "sub_tool",
						ToolCallID:       "inner_1",
					},
				},
			},
		},
	}
	aiMessage := &llmschema.AssistantMessage{}
	results := []agentschema.ExecuteResult{{Result: subResult, ToolMsg: nil}}
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

	intState := &ToolInterruptionState{
		BaseInterruptionState: BaseInterruptionState{Iteration: 5},
		InterruptedTools: map[string]*ToolInterruptEntry{
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

	intState := &ToolInterruptionState{
		BaseInterruptionState: BaseInterruptionState{Iteration: 1},
	}
	h.Save(intState, nil)
	assert.Nil(t, h.Load(nil))
	h.Clear(nil)
}

func TestCommitInterrupt(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	sess := session.NewSession()

	intState := &ToolInterruptionState{
		BaseInterruptionState: BaseInterruptionState{Iteration: 1},
		InterruptedTools: map[string]*ToolInterruptEntry{
			"tc1": {ToolCall: &llmschema.ToolCall{ID: "tc1"}},
		},
	}
	invokeInputs := &rail.InvokeInputs{}
	subAgentOutputs := []PayloadEntry{
		{InnerID: "inner_1", Payload: &ToolCallInterruptRequest{
			InterruptRequest: InterruptRequest{Message: "确认"},
			ToolName:         "tool1",
			ToolCallID:       "tc1",
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

	intState := &ToolInterruptionState{
		BaseInterruptionState: BaseInterruptionState{Iteration: 1},
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

	intState := &ToolInterruptionState{
		BaseInterruptionState: BaseInterruptionState{Iteration: 3},
		InterruptedTools: map[string]*ToolInterruptEntry{
			"tc1": {
				ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "tool1", Arguments: `{"arg1":"val1"}`},
				InterruptRequests: map[string]InterruptRequester{
					"tc1": &InterruptRequest{Message: "确认"},
				},
			},
		},
		AutoConfirmMapping: map[string]string{},
	}
	cbc := rail.NewAgentCallbackContext(nil, &rail.InvokeInputs{}, nil)

	executeFn := func(ctx context.Context, cbc *rail.AgentCallbackContext, toolCalls []*llmschema.ToolCall, sess sessioninterfaces.SessionFacade, modelCtx ceinterface.ModelContext) ([]agentschema.ExecuteResult, error) {
		return []agentschema.ExecuteResult{{Result: map[string]any{"result": "ok"}, ToolMsg: nil}}, nil
	}

	resumeCtx := &ResumeContext{
		State:           intState,
		UserInput:       "user_response",
		Ctx:             cbc,
		Session:         sess,
		InvokeInputs:    &rail.InvokeInputs{},
		ExecuteToolCall: executeFn,
	}

	result, err := h.HandleResume(context.Background(), resumeCtx)
	require.NoError(t, err)
	assert.Nil(t, result)
	assert.Equal(t, 4, cbc.Extra()[ResumeStartIterationKey])
}

func TestHandleResume_有新中断(t *testing.T) {
	agent := &fakeInterruptAgent{}
	h := NewToolInterruptHandler(agent)
	sess := session.NewSession()

	req := &InterruptRequest{Message: "再次确认"}
	intState := &ToolInterruptionState{
		BaseInterruptionState: BaseInterruptionState{Iteration: 2},
		InterruptedTools: map[string]*ToolInterruptEntry{
			"tc1": {
				ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "tool1", Arguments: `{}`},
				InterruptRequests: map[string]InterruptRequester{
					"tc1": &InterruptRequest{Message: "确认"},
				},
			},
		},
		AutoConfirmMapping: map[string]string{},
	}
	cbc := rail.NewAgentCallbackContext(nil, &rail.InvokeInputs{}, nil)

	executeFn := func(ctx context.Context, cbc *rail.AgentCallbackContext, toolCalls []*llmschema.ToolCall, sess sessioninterfaces.SessionFacade, modelCtx ceinterface.ModelContext) ([]agentschema.ExecuteResult, error) {
		return []agentschema.ExecuteResult{{Result: &ToolInterruptException{Request: req, ToolCall: &llmschema.ToolCall{ID: "tc1"}}, ToolMsg: nil}}, nil
	}

	resumeCtx := &ResumeContext{
		State:           intState,
		UserInput:       "user_response",
		Ctx:             cbc,
		Session:         sess,
		InvokeInputs:    &rail.InvokeInputs{},
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
		{InnerID: "inner_1", Payload: &ToolCallInterruptRequest{
			InterruptRequest: InterruptRequest{Message: "确认"},
			ToolName:         "tool1",
			ToolCallID:       "tc1",
		}},
	}
	result := BuildInterruptResult(payloads)
	require.NotNil(t, result)
	assert.Equal(t, "interrupt", result["result_type"])

	interruptIDs, _ := result["interrupt_ids"].([]string)
	assert.Equal(t, []string{"inner_1"}, interruptIDs)

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

// ──────────────────────────── 非导出函数 ────────────────────────────

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
	req := &InterruptRequest{Message: "需要确认", AutoConfirmKey: "auto_key"}
	tie := &ToolInterruptException{Request: req}
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "tool1"}

	interruptedTools := make(map[string]*ToolInterruptEntry)
	var payloads []PayloadEntry
	autoConfirmMapping := make(map[string]string)

	handleToolInterruptException(tie, toolCall, interruptedTools, &payloads, autoConfirmMapping)

	assert.Len(t, interruptedTools, 1)
	entry, ok := interruptedTools["tc1"]
	require.True(t, ok)
	assert.Equal(t, "tc1", entry.ToolCall.ID)
	assert.False(t, entry.IsSubAgent)
	assert.Len(t, entry.InterruptRequests, 1)
	assert.Equal(t, req, entry.InterruptRequests["tc1"])
	assert.Len(t, payloads, 1)
	assert.Equal(t, "auto_key", autoConfirmMapping["tc1"])
}

func TestHandleToolInterruptException_使用异常中的ToolCall(t *testing.T) {
	req := &InterruptRequest{Message: "需要确认"}
	tcInException := &llmschema.ToolCall{ID: "tc_from_exception", Name: "tool_from_exception"}
	tie := &ToolInterruptException{Request: req, ToolCall: tcInException}
	toolCall := &llmschema.ToolCall{ID: "tc_original", Name: "tool_original"}

	interruptedTools := make(map[string]*ToolInterruptEntry)
	var payloads []PayloadEntry
	autoConfirmMapping := make(map[string]string)

	handleToolInterruptException(tie, toolCall, interruptedTools, &payloads, autoConfirmMapping)

	entry, ok := interruptedTools["tc_from_exception"]
	require.True(t, ok)
	assert.Equal(t, "tc_from_exception", entry.ToolCall.ID)
}

func TestHandleSubAgentInterrupt(t *testing.T) {
	tcir := &ToolCallInterruptRequest{
		InterruptRequest: InterruptRequest{Message: "子Agent确认", AutoConfirmKey: "sub_auto"},
		ToolName:         "sub_tool",
		ToolCallID:       "inner_1",
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
	interruptedTools := make(map[string]*ToolInterruptEntry)
	var payloads []PayloadEntry
	autoConfirmMapping := make(map[string]string)

	handleSubAgentInterrupt(subResult, toolCall, interruptedTools, &payloads, autoConfirmMapping)

	assert.Len(t, interruptedTools, 1)
	entry, ok := interruptedTools["tc_outer"]
	require.True(t, ok)
	assert.True(t, entry.IsSubAgent)
	assert.Len(t, entry.InterruptRequests, 1)
	// 验证存储的是完整 ToolCallInterruptRequest（子类），不是截断的 InterruptRequest
	storedTCIR, ok := entry.InterruptRequests["inner_1"].(*ToolCallInterruptRequest)
	require.True(t, ok)
	assert.Equal(t, "sub_tool", storedTCIR.ToolName)
	assert.Equal(t, "inner_1", storedTCIR.ToolCallID)
	assert.Equal(t, "sub_auto", autoConfirmMapping["inner_1"])
	assert.Len(t, payloads, 1)
}

func TestHandleSubAgentInterrupt_tuple包装(t *testing.T) {
	subDict := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"inner_1"},
		"state":         []any{},
	}
	toolCall := &llmschema.ToolCall{ID: "tc_outer", Name: "agent_tool"}

	interruptedTools := make(map[string]*ToolInterruptEntry)
	var payloads []PayloadEntry
	autoConfirmMapping := make(map[string]string)

	// handleSubAgentInterrupt 现在直接接收 map[string]any，不再支持 [2]any tuple
	handleSubAgentInterrupt(subDict, toolCall, interruptedTools, &payloads, autoConfirmMapping)

	assert.Len(t, interruptedTools, 1)
	entry, ok := interruptedTools["tc_outer"]
	require.True(t, ok)
	assert.True(t, entry.IsSubAgent)
}

func TestSaveAutoConfirmFromState(t *testing.T) {
	sess := session.NewSession()
	intState := &ToolInterruptionState{
		AutoConfirmMapping: map[string]string{
			"inner_1": "auto_key_1",
		},
	}

	interactiveInput := &interaction.InteractiveInput{
		UserInputs: map[string]any{
			"inner_1": map[string]any{"auto_confirm": true},
		},
	}

	saveAutoConfirmFromState(intState, interactiveInput, sess)

	configVal, _ := sess.GetState(sessionstate.StringKey(InterruptAutoConfirmKey))
	config, ok := configVal.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, config["auto_key_1"])
}

func TestSaveAutoConfirmFromState_nilSession(t *testing.T) {
	intState := &ToolInterruptionState{}
	saveAutoConfirmFromState(intState, &interaction.InteractiveInput{}, nil)
}

func TestSaveAutoConfirmFromState_非InteractiveInput(t *testing.T) {
	sess := session.NewSession()
	intState := &ToolInterruptionState{}
	saveAutoConfirmFromState(intState, "string_input", sess)
	configVal, _ := sess.GetState(sessionstate.StringKey(InterruptAutoConfirmKey))
	assert.Nil(t, configVal)
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

	req1 := &InterruptRequest{Message: "确认1", AutoConfirmKey: "key1"}
	tie := &ToolInterruptException{Request: req1}

	subResult := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"inner_2"},
		"state": []any{
			&stream.OutputSchema{
				Type:  interaction.InteractionType,
				Index: 0,
				Payload: &interaction.InteractionOutput{
					ID: "inner_2",
					Value: &ToolCallInterruptRequest{
						InterruptRequest: InterruptRequest{Message: "子Agent确认"},
						ToolName:         "sub_tool",
						ToolCallID:       "inner_2",
					},
				},
			},
		},
	}

	results := []agentschema.ExecuteResult{
		{Result: tie, ToolMsg: nil},
		{Result: map[string]any{}, ToolMsg: nil},
		{Result: subResult, ToolMsg: nil},
	}
	toolCalls := []*llmschema.ToolCall{
		{ID: "tc1", Name: "tool1"},
		{ID: "tc2", Name: "tool2"},
		{ID: "tc3", Name: "agent_tool"},
	}

	interruptedTools, payloads, autoConfirmMapping := h.collectInterrupts(results, toolCalls)
	assert.Len(t, interruptedTools, 2)
	assert.NotEmpty(t, payloads)
	assert.Equal(t, "key1", autoConfirmMapping["tc1"])
}
