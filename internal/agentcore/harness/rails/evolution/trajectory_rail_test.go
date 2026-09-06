package evolution

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── TrajectoryRail 测试 ────────────────────────────

func TestNewTrajectoryRail_默认值(t *testing.T) {
	rail := NewTrajectoryRail()
	assert.NotNil(t, rail)
	assert.Equal(t, 10, rail.Priority())
	assert.NotNil(t, rail.TrajectoryStore())
}

func TestNewTrajectoryRail_带选项(t *testing.T) {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := NewTrajectoryRail(WithTrajectoryStore(store))
	assert.Equal(t, store, rail.TrajectoryStore())
}

func TestTrajectoryRail_收集轨迹(t *testing.T) {
	rail := NewTrajectoryRail()

	// before_invoke 初始化 builder
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-traj"})
	err := rail.BeforeInvoke(context.Background(), cbc)
	require.NoError(t, err)

	// 记录 LLM 步骤
	modelInputs := &agentinterfaces.ModelCallInputs{
		Messages: []llmschema.BaseMessage{llmschema.NewUserMessage("hi")},
		Response: llmschema.NewAssistantMessage("hello"),
	}
	cbcModel := &agentinterfaces.AgentCallbackContext{}
	cbcModel.SetInputs(modelInputs)
	err = rail.AfterModelCall(context.Background(), cbcModel)
	require.NoError(t, err)

	// 验证 builder 有步骤
	traj := rail.buildTrajectory()
	assert.NotNil(t, traj)
	assert.Len(t, traj.Steps, 1)
}

func TestTrajectoryRail_RunEvolution不执行任何操作(t *testing.T) {
	rail := NewTrajectoryRail()
	// TrajectoryRail 使用 noOpExtension，RunEvolution 不做任何事
	err := rail.ext.RunEvolution(context.Background(), nil, nil)
	assert.NoError(t, err)
}
