package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// TestNativeHarness_PauseResume空闲时幂等 验证 IDLE 下 Pause/Resume 是 no-op。
// Python: _on_pause / _on_resume 在非 RUNNING / 非 PAUSED 时直接返回。
func TestNativeHarness_PauseResume空闲时幂等(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, h.Start(context.Background(), sess))
	defer func() { _ = h.Stop(context.Background()) }()

	assert.NoError(t, h.Pause(context.Background()))
	// 无 query 的 resume 在 IDLE 下是 no-op；带 query 属冷恢复，会起续跑轮。
	assert.NoError(t, h.Resume(context.Background(), nil))
	assert.Equal(t, HarnessStateIdle, h.State(), "空闲暂停/无 query 继续不得改相位")
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
		RoundID:       1,
		TaskID:        "t1",
		OriginalQuery: "长任务",
		Cancel:        func() { cancelled = true },
	})

	require.NoError(t, h.onAbort(&cmdAbort{immediate: true}))
	assert.True(t, cancelled, "立即中断必须取消活动轮的上下文")
}

// TestNativeHarness_onAbort优雅中断标记graceful 验证 graceful abort 只置标志（C11）。
// Python: _on_abort(immediate=False) 置 active.graceful_abort；legacy
// coordinator.request_abort 不再使用（内层循环不读 is_aborted）。
func TestNativeHarness_onAbort优雅中断标记graceful(t *testing.T) {
	h := newTestNativeHarness(t)

	cancelled := false
	h.st.setPhase(HarnessStateRunning)
	active := &ActiveRound{
		RoundID: 1, TaskID: "t1", OriginalQuery: "q",
		Cancel: func() { cancelled = true },
	}
	h.st.SetActive(active)

	require.NoError(t, h.onAbort(&cmdAbort{immediate: false}))
	assert.False(t, cancelled, "优雅中断不得取消活动轮")
	assert.True(t, active.GracefulAbort.Load(), "优雅中断必须置 graceful_abort")
}

// TestNativeHarness_onAbort无活动轮返回 验证无活动轮时 abort 是 no-op。
func TestNativeHarness_onAbort无活动轮返回(t *testing.T) {
	h := newTestNativeHarness(t)
	h.st.setPhase(HarnessStateIdle)
	require.NoError(t, h.onAbort(&cmdAbort{immediate: true}))
}
