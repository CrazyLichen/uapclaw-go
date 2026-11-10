package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestNewNativeHarness_注入AsyncToolHost句柄 验证 buildCtx extras 有宿主句柄。
func TestNewNativeHarness_注入AsyncToolHost句柄(t *testing.T) {
	spec := atschema.NewDeepAgentSpec()
	spec.Card = agentschema.NewAgentCard(agentschema.WithAgentName("handle-test"))
	ctx := &hschema.BuildContext{Extras: map[string]any{}}
	require.NotNil(t, ctx)

	h, err := NewNativeHarness(spec, ctx)
	require.NoError(t, err)
	got, _ := ctx.GetExtras()[tools.TeamHandleAsyncToolHost].(*NativeHarness)
	assert.Same(t, h, got)
}
