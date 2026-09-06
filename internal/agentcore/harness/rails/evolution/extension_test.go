package evolution

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── noOpExtension 测试 ────────────────────────────

func TestNoOpExtension_AllMethods(t *testing.T) {
	ext := noOpExtension{}
	cbc := &agentinterfaces.AgentCallbackContext{}

	assert.NoError(t, ext.OnBeforeInvoke(context.Background(), cbc))
	assert.NoError(t, ext.OnAfterModelCall(context.Background(), cbc))
	assert.NoError(t, ext.OnAfterToolCall(context.Background(), cbc))
	assert.NoError(t, ext.OnAfterInvoke(context.Background(), cbc))
	assert.NoError(t, ext.OnAfterTaskIteration(context.Background(), cbc))
	assert.NoError(t, ext.OnAfterEvolutionTriggered(context.Background(), nil, cbc))
	assert.True(t, ext.AllowEvolutionTrigger(TriggerAfterInvoke, cbc))
	assert.NoError(t, ext.RunEvolution(context.Background(), nil, nil))
}

func TestNoOpExtension_SnapshotForEvolution(t *testing.T) {
	ext := noOpExtension{}
	traj := &trajectory.Trajectory{
		SessionID: "test",
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindLLM,
				Detail: &trajectory.LLMCallDetail{
					Messages: []map[string]any{{"role": "user", "content": "hi"}},
				},
			},
		},
	}
	snapshot := ext.SnapshotForEvolution(context.Background(), traj, nil)
	assert.NotNil(t, snapshot)
	assert.Equal(t, traj, snapshot.Trajectory)
	assert.NotEmpty(t, snapshot.Messages)
}

// ──────────────────────────── EvolutionTriggerPoint 测试 ────────────────────────────

func TestEvolutionTriggerPoint_值(t *testing.T) {
	assert.Equal(t, EvolutionTriggerPoint("after_invoke"), TriggerAfterInvoke)
	assert.Equal(t, EvolutionTriggerPoint("after_model_call"), TriggerAfterModelCall)
	assert.Equal(t, EvolutionTriggerPoint("after_tool_call"), TriggerAfterToolCall)
	assert.Equal(t, EvolutionTriggerPoint("after_task_iteration"), TriggerAfterTaskIteration)
	assert.Equal(t, EvolutionTriggerPoint("none"), TriggerNone)
}
