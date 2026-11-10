package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TestCloneSnapshotMessages_独立副本 验证快照切片与源切片隔离。
func TestCloneSnapshotMessages_独立副本(t *testing.T) {
	src := []llmschema.BaseMessage{llmschema.NewUserMessage("本轮消息")}
	out := cloneSnapshotMessages(src)
	require.Len(t, out, 1)

	out[0] = llmschema.NewUserMessage("篡改")
	assert.Equal(t, "本轮消息", src[0].GetContent().String(), "快照切片必须与源切片隔离")
}

// TestNextIterationIndex 验证显式 index、链式 +1 与默认 1。
func TestNextIterationIndex(t *testing.T) {
	zero := 0
	assert.Equal(t, 0, nextIterationIndex(nil, &zero))
	assert.Equal(t, 1, nextIterationIndex(nil, nil))
	assert.Equal(t, 4, nextIterationIndex(&SafeStateSnapshot{IterationIndex: 3}, nil))
}

// TestPhaseSnapshotRail_模型相位武装后强制收尾 验证暂停在模型调用前强制收尾。
func TestPhaseSnapshotRail_模型相位武装后强制收尾(t *testing.T) {
	h := &NativeHarness{st: newInternalState()}
	active := &ActiveRound{RoundID: 1, Done: make(chan struct{})}
	h.st.SetActive(active)
	rail := newPhaseSnapshotRail(h)
	cbc := agentinterfaces.NewAgentCallbackContext(nil,
		&agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString("q")}, nil)

	active.PauseRequested.Store(true)
	require.NoError(t, rail.BeforeModelCall(context.Background(), cbc))
	assert.Equal(t, RoundPhaseModel, active.Phase())
	assert.True(t, active.ModelCallInFlight.Load())
	assert.True(t, cbc.HasForceFinishRequest())
	req := cbc.ConsumeForceFinish()
	assert.Equal(t, cooperativeStopType, req.Result["result_type"])
}

// TestPhaseSnapshotRail_工具相位不强制收尾 验证已开始的迭代必须跑完。
func TestPhaseSnapshotRail_工具相位不强制收尾(t *testing.T) {
	h := &NativeHarness{st: newInternalState()}
	active := &ActiveRound{RoundID: 1, Done: make(chan struct{})}
	h.st.SetActive(active)
	rail := newPhaseSnapshotRail(h)
	cbc := agentinterfaces.NewAgentCallbackContext(nil,
		&agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString("q")}, nil)

	active.PauseRequested.Store(true)
	require.NoError(t, rail.BeforeToolCall(context.Background(), cbc))
	assert.Equal(t, RoundPhaseTool, active.Phase())
	assert.True(t, active.ToolStarted.Load())
	assert.False(t, cbc.HasForceFinishRequest())
}

// TestPhaseSnapshotRail_无活动轮时全部noop 验证没有在飞轮时钩子全部空转。
func TestPhaseSnapshotRail_无活动轮时全部noop(t *testing.T) {
	h := &NativeHarness{st: newInternalState()}
	rail := newPhaseSnapshotRail(h)
	cbc := agentinterfaces.NewAgentCallbackContext(nil,
		&agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString("q")}, nil)

	assert.NoError(t, rail.BeforeModelCall(context.Background(), cbc))
	assert.NoError(t, rail.AfterModelCall(context.Background(), cbc))
	assert.NoError(t, rail.BeforeToolCall(context.Background(), cbc))
	assert.NoError(t, rail.AfterReactIteration(context.Background(), cbc))
	assert.NoError(t, rail.AfterTaskIteration(context.Background(), cbc))
	assert.False(t, cbc.HasForceFinishRequest())
}
