package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// TestNativeHarness_PauseResume返回哨兵错误 验证 C2：状态不变、errors.Is 可判。
func TestNativeHarness_PauseResume返回哨兵错误(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, h.Start(context.Background(), sess))
	defer func() { _ = h.Stop(context.Background()) }()

	assert.True(t, errors.Is(h.Pause(context.Background()), ErrPauseUnsupported))
	assert.True(t, errors.Is(h.Resume(context.Background(), "x"), ErrPauseUnsupported))
	assert.Equal(t, HarnessStateIdle, h.State(), "哨兵错误不得改相位")
}

// TestNativeHarness_Pause未启动报错 验证未 Start 时入队被拒（不会空等 supervisor）。
func TestNativeHarness_Pause未启动报错(t *testing.T) {
	h := newTestNativeHarness(t)
	assert.Error(t, h.Pause(context.Background()))
}

// TestNativeHarness_onAbort立即中断取消活动轮 验证 immediate abort 取消活动轮（C11）。
func TestNativeHarness_onAbort立即中断取消活动轮(t *testing.T) {
	h := newTestNativeHarness(t)
	h.st.setPhase(HarnessStateRunning)
	cancelled := false
	h.st.SetActive(&ActiveRound{
		RoundID: 1,
		TaskID:  "t1",
		OriginalQuery:   "长任务",
		Cancel:  func() { cancelled = true },
	})

	require.NoError(t, h.onAbort(&cmdAbort{immediate: true}))
	assert.True(t, cancelled, "立即中断必须取消活动轮的上下文")
}

// TestNativeHarness_onAbort优雅中断标记coordinator 验证 graceful abort 走 RequestAbort（C11）。
func TestNativeHarness_onAbort优雅中断标记coordinator(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	coord, ctrl, err := h.DeepAgent().PrepareInteractionTaskLoop(context.Background(), sess)
	require.NoError(t, err)
	h.coordinator = coord
	h.controller = ctrl

	cancelled := false
	h.st.setPhase(HarnessStateRunning)
	h.st.SetActive(&ActiveRound{
		RoundID: 1, TaskID: "t1", OriginalQuery: "q",
		Cancel: func() { cancelled = true },
	})

	require.NoError(t, h.onAbort(&cmdAbort{immediate: false}))
	assert.False(t, cancelled, "优雅中断不得取消活动轮")
	assert.True(t, coord.IsAborted(), "优雅中断必须标记 coordinator 中止")
}

// TestNativeHarness_onAbort无活动轮返回 验证无活动轮时 abort 是 no-op。
func TestNativeHarness_onAbort无活动轮返回(t *testing.T) {
	h := newTestNativeHarness(t)
	h.st.setPhase(HarnessStateIdle)
	require.NoError(t, h.onAbort(&cmdAbort{immediate: true}))
}
