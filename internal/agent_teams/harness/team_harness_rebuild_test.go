package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// TestTeamHarnessStart_第二轮重建native 验证 Stop 后 Start 会重建 native。
func TestTeamHarnessStart_第二轮重建native(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("rebuild-test"))
	spec := atschema.NewDeepAgentSpec()
	spec.Card = card

	first, err := NewNativeHarness(spec, nil)
	require.NoError(t, err)

	th := newTeamHarnessWithNative(first, first.DeepAgent(), "leader", "leader_1", false, nil)
	th.agentSpec = spec
	th.buildCtx = nil

	sess := session.CreateAgentSession("default", card, nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, th.Start(context.Background(), sess))
	require.NoError(t, th.Stop(context.Background()))

	second := session.CreateAgentSession("default", card, nil)
	require.NoError(t, second.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, th.Start(context.Background(), second))
	assert.NotSame(t, first, th.native, "第二轮必须重建 native")
	assert.Equal(t, HarnessStateIdle, th.native.State())
	require.NoError(t, th.Stop(context.Background()))
}
