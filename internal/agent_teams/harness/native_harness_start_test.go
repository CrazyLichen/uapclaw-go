package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentcore "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// newTestNativeHarness 构造一个不含模型的最小 NativeHarness。
func newTestNativeHarness(t *testing.T) *NativeHarness {
	t.Helper()
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-native"))
	agent := agentcore.NewDeepAgent(card)
	return NewNativeHarness(agent, atschema.DeepAgentSpec{})
}

// TestNativeHarness_Start幂等 验证重复 Start 不重建内核（C1）。
func TestNativeHarness_Start幂等(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))

	require.NoError(t, h.Start(context.Background(), sess))
	require.NoError(t, h.Start(context.Background(), sess))

	assert.Equal(t, HarnessStateRunning, h.State())
	assert.Equal(t, sess.GetSessionID(), h.SessionID())

	require.NoError(t, h.Stop(context.Background()))
	assert.Equal(t, HarnessStateTerminated, h.State())
}

// TestNativeHarness_Start绑定不同会话报错 对齐上游"一个实例一个会话"约束。
func TestNativeHarness_Start绑定不同会话报错(t *testing.T) {
	h := newTestNativeHarness(t)
	s1 := session.CreateAgentSession("s1", h.DeepAgent().Card(), nil)
	require.NoError(t, s1.PreRun(context.Background(), map[string]any{}))
	s2 := session.CreateAgentSession("s2", h.DeepAgent().Card(), nil)
	require.NoError(t, s2.PreRun(context.Background(), map[string]any{}))

	require.NoError(t, h.Start(context.Background(), s1))
	assert.Error(t, h.Start(context.Background(), s2))
	require.NoError(t, h.Stop(context.Background()))
}

// TestNativeHarness_InitialPhaseIsIdle 验证未启动即 idle。
func TestNativeHarness_InitialPhaseIsIdle(t *testing.T) {
	h := newTestNativeHarness(t)
	assert.Equal(t, HarnessStateIdle, h.State())
	assert.Empty(t, h.SessionID())
}
