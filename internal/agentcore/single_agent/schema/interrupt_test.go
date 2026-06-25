//go:build test

package schema

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── exception.go ────────────────────────────

func TestToolInterruptException_Error_有Request(t *testing.T) {
	e := &ToolInterruptException{Request: &InterruptRequest{Message: "请确认"}}
	assert.Equal(t, "请确认", e.Error())
}

func TestToolInterruptException_Error_无Request(t *testing.T) {
	e := &ToolInterruptException{Request: nil}
	assert.Equal(t, "tool interrupt", e.Error())
}

func TestToolInterruptException_String_有ToolCall(t *testing.T) {
	e := &ToolInterruptException{
		Request:  &InterruptRequest{Message: "确认"},
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "tool1"},
	}
	assert.Contains(t, e.String(), "tool1")
	assert.Contains(t, e.String(), "tc1")
}

func TestToolInterruptException_String_无ToolCall(t *testing.T) {
	e := &ToolInterruptException{Request: &InterruptRequest{Message: "确认"}}
	assert.Equal(t, "tool interrupt: 确认", e.String())
}

func TestToolInterruptException_实现error接口(t *testing.T) {
	var err error = &ToolInterruptException{Request: &InterruptRequest{Message: "test"}}
	assert.Error(t, err)
}

func TestToolInterruptException_errors_As识别(t *testing.T) {
	err := &ToolInterruptException{Request: &InterruptRequest{Message: "test"}}
	var tie *ToolInterruptException
	require.True(t, errors.As(err, &tie))
	assert.Equal(t, "test", tie.Request.Message)
}

// ──────────────────────────── response.go ────────────────────────────

func TestInterruptRequester_接口满足(t *testing.T) {
	// *InterruptRequest 满足 InterruptRequester
	var req InterruptRequester = &InterruptRequest{Message: "确认", AutoConfirmKey: "key"}
	assert.Equal(t, "确认", req.GetMessage())
	assert.Equal(t, "key", req.GetAutoConfirmKey())

	// *ToolCallInterruptRequest 满足 InterruptRequester
	var tcir InterruptRequester = &ToolCallInterruptRequest{
		InterruptRequest: InterruptRequest{Message: "子Agent确认", AutoConfirmKey: "sub_key"},
		ToolName:         "sub_tool",
	}
	assert.Equal(t, "子Agent确认", tcir.GetMessage())
	assert.Equal(t, "sub_key", tcir.GetAutoConfirmKey())
}

func TestInterruptRequester_类型断言(t *testing.T) {
	var req InterruptRequester = &ToolCallInterruptRequest{
		InterruptRequest: InterruptRequest{Message: "确认"},
		ToolName:         "sub_tool",
		ToolCallID:       "inner_1",
	}
	// 可断言回 *ToolCallInterruptRequest
	tcir, ok := req.(*ToolCallInterruptRequest)
	require.True(t, ok)
	assert.Equal(t, "sub_tool", tcir.ToolName)

	// 断言为 *InterruptRequest 失败（实际类型是子类）
	_, ok = req.(*InterruptRequest)
	assert.False(t, ok)
}

func TestInterruptRequest_字段默认值(t *testing.T) {
	req := &InterruptRequest{}
	assert.Equal(t, "", req.Message)
	assert.Nil(t, req.PayloadSchema)
	assert.Equal(t, "", req.AutoConfirmKey)
	assert.Nil(t, req.UIOptions)
}

func TestInterruptRequest_Getter方法(t *testing.T) {
	req := &InterruptRequest{Message: "确认？", AutoConfirmKey: "auto_key"}
	assert.Equal(t, "确认？", req.GetMessage())
	assert.Equal(t, "auto_key", req.GetAutoConfirmKey())
}

func TestInterruptRequest_完整字段(t *testing.T) {
	req := &InterruptRequest{
		Message:        "确认执行？",
		PayloadSchema:  map[string]any{"type": "string"},
		AutoConfirmKey: "auto_key",
		UIOptions:      []map[string]any{{"label": "确认"}},
	}
	assert.Equal(t, "确认执行？", req.Message)
	assert.Equal(t, "auto_key", req.AutoConfirmKey)
}

func TestToolCallInterruptRequest_从ToolCall创建(t *testing.T) {
	req := &InterruptRequest{Message: "确认", AutoConfirmKey: "key"}
	tc := &llmschema.ToolCall{ID: "call_1", Name: "tool_a", Arguments: `{"x":1}`, Index: 2}
	tcir := NewToolCallInterruptRequest(req, tc)
	assert.Equal(t, "确认", tcir.Message)
	assert.Equal(t, "tool_a", tcir.ToolName)
	assert.Equal(t, "call_1", tcir.ToolCallID)
	assert.Equal(t, `{"x":1}`, tcir.ToolArgs)
	assert.Equal(t, 2, tcir.Index)
}

// ──────────────────────────── state.go ────────────────────────────

func TestInterruptKey常量(t *testing.T) {
	assert.Equal(t, "__react_agent_interruption__", InterruptionKey)
	assert.Equal(t, "_resume_user_input", ResumeUserInputKey)
	assert.Equal(t, "__interrupt_auto_confirm__", InterruptAutoConfirmKey)
	assert.Equal(t, "_resume_start_iteration", ResumeStartIterationKey)
}

func TestBaseInterruptionState(t *testing.T) {
	s := &BaseInterruptionState{
		AIMessage:     &llmschema.AssistantMessage{},
		Iteration:     3,
		OriginalQuery: "你好",
	}
	assert.Equal(t, 3, s.Iteration)
	assert.Equal(t, "你好", s.OriginalQuery)
}

func TestToolInterruptEntry(t *testing.T) {
	entry := &ToolInterruptEntry{
		ToolCall: &llmschema.ToolCall{ID: "c1", Name: "tool1"},
		InterruptRequests: map[string]InterruptRequester{
			"ir1": &InterruptRequest{Message: "确认1"},
		},
		IsSubAgent: false,
	}
	assert.Equal(t, "c1", entry.ToolCall.ID)
	assert.False(t, entry.IsSubAgent)
}

func TestToolInterruptEntry_子类存储(t *testing.T) {
	tcir := &ToolCallInterruptRequest{
		InterruptRequest: InterruptRequest{Message: "子Agent确认", AutoConfirmKey: "sub_key"},
		ToolName:         "sub_tool",
		ToolCallID:       "inner_1",
	}
	entry := &ToolInterruptEntry{
		ToolCall: &llmschema.ToolCall{ID: "c1", Name: "tool1"},
		InterruptRequests: map[string]InterruptRequester{
			"inner_1": tcir,
		},
		IsSubAgent: true,
	}
	assert.True(t, entry.IsSubAgent)
	// 验证存的是完整子类，可类型断言回 *ToolCallInterruptRequest
	stored, ok := entry.InterruptRequests["inner_1"].(*ToolCallInterruptRequest)
	require.True(t, ok)
	assert.Equal(t, "sub_tool", stored.ToolName)
	assert.Equal(t, "inner_1", stored.ToolCallID)
	assert.Equal(t, "sub_key", stored.GetAutoConfirmKey())
}

func TestToolInterruptionState(t *testing.T) {
	s := &ToolInterruptionState{
		BaseInterruptionState: BaseInterruptionState{Iteration: 5},
		InterruptedTools: map[string]*ToolInterruptEntry{
			"tc_1": {ToolCall: &llmschema.ToolCall{ID: "tc_1"}},
		},
		AutoConfirmMapping: map[string]string{"inner1": "auto_key"},
	}
	assert.Equal(t, 5, s.Iteration)
	assert.Len(t, s.InterruptedTools, 1)
}

func TestWorkflowInterruptEntry(t *testing.T) {
	entry := &WorkflowInterruptEntry{
		ToolCall:              &llmschema.ToolCall{ID: "wf_1"},
		ComponentIDs:          []string{"comp1", "comp2"},
		WorkflowExecutionState: nil,
		CollectedInput:        nil,
	}
	assert.Equal(t, "wf_1", entry.ToolCall.ID)
	assert.Len(t, entry.ComponentIDs, 2)
}

func TestInterruptionState(t *testing.T) {
	s := &InterruptionState{
		BaseInterruptionState: BaseInterruptionState{Iteration: 2},
		InterruptedWorkflows: map[string]*WorkflowInterruptEntry{
			"wf1": {ToolCall: &llmschema.ToolCall{ID: "wf1"}},
		},
		PendingWorkflowID:  "wf1",
		PendingComponentID: "comp1",
	}
	assert.Equal(t, 2, s.Iteration)
	assert.Equal(t, "wf1", s.PendingWorkflowID)
}
