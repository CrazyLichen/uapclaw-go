package interrupt

import (
	"testing"

	"github.com/stretchr/testify/assert"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestInterruptKey常量(t *testing.T) {
	assert.Equal(t, "__react_agent_interruption__", InterruptionKey)
	assert.Equal(t, "_resume_user_input", saschema.ResumeUserInputKey)
	assert.Equal(t, "__interrupt_auto_confirm__", saschema.InterruptAutoConfirmKey)
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
	entry := &saschema.WorkflowInterruptEntry{
		ToolCall:               &llmschema.ToolCall{ID: "wf_1"},
		ComponentIDs:           []string{"comp1", "comp2"},
		WorkflowExecutionState: nil,
		CollectedInput:         nil,
	}
	assert.Equal(t, "wf_1", entry.ToolCall.ID)
	assert.Len(t, entry.ComponentIDs, 2)
}

func TestInterruptionState(t *testing.T) {
	s := &saschema.InterruptionState{
		BaseInterruptionState: BaseInterruptionState{Iteration: 2},
		InterruptedWorkflows: map[string]*saschema.WorkflowInterruptEntry{
			"wf1": {ToolCall: &llmschema.ToolCall{ID: "wf1"}},
		},
		PendingWorkflowID:  "wf1",
		PendingComponentID: "comp1",
	}
	assert.Equal(t, 2, s.Iteration)
	assert.Equal(t, "wf1", s.PendingWorkflowID)
	assert.Equal(t, "comp1", s.PendingComponentID)
}
