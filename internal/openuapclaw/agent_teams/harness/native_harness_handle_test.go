package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
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
