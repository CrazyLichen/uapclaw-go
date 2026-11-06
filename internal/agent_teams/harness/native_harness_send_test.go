package harness

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// TestNativeHarness_Send在Idle时开轮 验证 idle 下 Send 立刻起一轮并进入 running（C1）。
func TestNativeHarness_Send在Idle时开轮(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, h.Start(context.Background(), sess))
	defer func() { _ = h.Stop(context.Background()) }()

	require.NoError(t, h.Send(context.Background(), "第一条", false))

	require.Eventually(t, func() bool {
		return h.State() == HarnessStateRunning
	}, 2*time.Second, 10*time.Millisecond)
}

// TestNativeHarness_Send在Running时排队 验证 running 下非立即输入进队列而非新开一轮（C13）。
//
// 直接驱动 onSend 分支而非走完整异步 supervisor：无模型环境下 SubmitRound 会
// 立刻结束轮次，无法稳定构造"running 中"的时序窗口。
func TestNativeHarness_Send在Running时排队(t *testing.T) {
	h := newTestNativeHarness(t)
	// 手动把相位拨到 running 并挂一个活动轮，模拟"有轮在飞"
	h.st.setPhase(HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", Query: "A"})

	require.NoError(t, h.onSend(context.Background(), &InboxMessage{Content: "B", Immediate: false}))

	// 非立即输入进入 pending 队列，而不是新开一轮（roundSeq 保持 0，未起新轮）
	assert.Equal(t, 0, h.st.roundSeqSnapshot())
	assert.True(t, h.st.dequeueInbox())
}

// TestNativeHarness_onSend在Running时立即输入转向 验证 immediate=true 走 steer（C1）。
func TestNativeHarness_onSend在Running时立即输入转向(t *testing.T) {
	h := newTestNativeHarness(t)
	h.st.setPhase(HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", Query: "A"})

	// 无 controller 时 steerActive 返回错误（验证走的是 steer 分支而非排队）
	err := h.onSend(context.Background(), &InboxMessage{Content: "steer", Immediate: true})
	assert.Error(t, err)
	assert.False(t, h.st.dequeueInbox(), "immediate 输入不得进 pending 队列")
}

// TestNativeHarness_Send在Terminated后报错 验证 C16 的入队守卫。
func TestNativeHarness_Send在Terminated后报错(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, h.Start(context.Background(), sess))
	require.NoError(t, h.Stop(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.Error(t, h.Send(ctx, "stop 之后", false))
}
