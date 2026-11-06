package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// TestPrepareInteractionTaskLoop_导出可用且幂等 验证宿主可自建内核（C4）。
func TestPrepareInteractionTaskLoop_导出可用且幂等(t *testing.T) {
	d := newTestDeepAgent()
	sess := session.CreateAgentSession("default", d.Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))

	coord1, ctrl1, err := d.PrepareInteractionTaskLoop(context.Background(), sess)
	require.NoError(t, err)
	require.NotNil(t, coord1)
	require.NotNil(t, ctrl1)

	coord2, ctrl2, err := d.PrepareInteractionTaskLoop(context.Background(), sess)
	require.NoError(t, err)
	assert.Same(t, coord1, coord2)
	assert.Same(t, ctrl1, ctrl2)
}

// TestWriteRoundResultToStream_导出可用 验证跨包窄入口转发到内层 reactAgent。
//
// newTestDeepAgent 未 SetReactAgent（reactAgent==nil），writeRoundResultToStream
// 内部走 nil 守卫 no-op，此处只验证导出方法可安全调用、不 panic。
func TestWriteRoundResultToStream_导出可用(t *testing.T) {
	d := newTestDeepAgent()
	sess := session.CreateAgentSession("default", d.Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))

	assert.NotPanics(t, func() {
		d.WriteRoundResultToStream(context.Background(), map[string]any{
			"output":      "answer text",
			"result_type": "answer",
		}, sess)
	})
}
