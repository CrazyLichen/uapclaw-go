package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// newPauseTestHarness 构造一个已 Start 过的 harness 的最小替身：
// 有真实 DeepAgent 与事件总线，但没有 supervisor 消费 cmd（命令留在缓冲通道）。
func newPauseTestHarness(t *testing.T) (*NativeHarness, *ActiveRound) {
	t.Helper()
	h := newTestNativeHarness(t)
	h.sess = session.NewSession(session.WithSessionID("pause-sess"))

	h.st.setPhase(HarnessStateRunning)
	done := make(chan struct{})
	close(done)
	active := &ActiveRound{
		RoundID:       1,
		TaskID:        "t1",
		OriginalQuery: "任务",
		Cancel:        func() {},
		Done:          done,
	}
	active.SetPhase(RoundPhaseBoundary)
	h.st.SetActive(active)
	return h, active
}

// TestOnPause_模型相位硬取消并回滚 验证 LLM 相位暂停：回 IDLE 前先作废半轮。
func TestOnPause_模型相位硬取消并回滚(t *testing.T) {
	h, active := newPauseTestHarness(t)
	active.ModelCallInFlight.Store(true)
	active.LastIterSnapshot = &SafeStateSnapshot{IterationIndex: 1, DeepAgentState: map[string]any{}}
	reply := make(chan error, 1)

	require.NoError(t, h.onPause(&cmdPause{reply: reply}))

	assert.Equal(t, HarnessStatePaused, h.State())
	assert.Nil(t, h.st.Active())

	select {
	case chunk := <-h.queue:
		out, ok := chunk.(stream.OutputSchema)
		require.True(t, ok)
		assert.Equal(t, "round_aborted", out.Type)
		assert.Equal(t, "pause", out.Payload.(map[string]any)["kind"])
	default:
		t.Fatal("应投递 round_aborted 标记")
	}
}

// TestOnPause_工具相位延迟应答 验证工具相位不打断，ack 延迟到轮末。
func TestOnPause_工具相位延迟应答(t *testing.T) {
	h, active := newPauseTestHarness(t)
	active.ModelCallInFlight.Store(false)
	reply := make(chan error, 1)

	require.NoError(t, h.onPause(&cmdPause{reply: reply}))

	assert.Equal(t, HarnessStatePausing, h.State())
	assert.True(t, active.PauseRequested.Load())
	assert.Equal(t, reply, active.PauseAck)
}

// TestOnAbort_Immediate回滚并作废 验证立即中止回滚并投递 abort 标记。
func TestOnAbort_Immediate回滚并作废(t *testing.T) {
	h, _ := newPauseTestHarness(t)
	reply := make(chan error, 1)

	require.NoError(t, h.onAbort(&cmdAbort{immediate: true, reply: reply}))

	assert.Equal(t, HarnessStateIdle, h.State())
	select {
	case chunk := <-h.queue:
		out := chunk.(stream.OutputSchema)
		assert.Equal(t, "round_aborted", out.Type)
		assert.Equal(t, "abort", out.Payload.(map[string]any)["kind"])
	default:
		t.Fatal("应投递 round_aborted 标记")
	}
}

// TestOnResume_Warm续跑清空查询 验证 warm resume 起续跑轮并清空暂停查询。
func TestOnResume_Warm续跑清空查询(t *testing.T) {
	h, _ := newPauseTestHarness(t)
	h.st.SetActive(nil)
	h.st.setPhase(HarnessStatePaused)
	h.st.SetPausedQuery("任务")

	reply := make(chan error, 1)
	require.NoError(t, h.onResume(&cmdResume{reply: reply}))

	assert.Equal(t, HarnessStateRunning, h.State())
	q, has := h.st.PausedQuery()
	assert.False(t, has)
	assert.Empty(t, q)
	require.NotNil(t, h.st.Active())
}
