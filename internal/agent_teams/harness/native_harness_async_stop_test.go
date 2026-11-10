package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// TestNativeHarness_Stop取消异步任务 验证停机先 CancelAll，不注入。
func TestNativeHarness_Stop取消异步任务(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, h.Start(context.Background(), sess))

	started := make(chan struct{})
	h.AsyncToolRuntime().Launch("t9", func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}, tools.AsyncToolLaunchOptions{ToolName: "demo"})
	<-started

	require.NoError(t, h.Stop(context.Background()))
	rt := h.AsyncToolRuntime()
	waitStatus(t, rt, "t9", AsyncToolStatusError)
	rec := rt.Get("t9")
	require.NotNil(t, rec)
	assert.Equal(t, "cancelled", rec.Error)
}
