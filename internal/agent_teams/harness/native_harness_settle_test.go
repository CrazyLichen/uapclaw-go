package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
)

// prepareSettleHarness 构造一个可驱动 settleRoundDone 的 NativeHarness（已 Start + 有 coordinator/controller）。
func prepareSettleHarness(t *testing.T) *NativeHarness {
	t.Helper()
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("settle", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))
	require.NoError(t, h.Start(context.Background(), sess))
	t.Cleanup(func() { _ = h.Stop(context.Background()) })
	return h
}

// TestSettleRoundDone_finished转idle 验证普通完成 → 发 finished 事件 + 转 idle。
func TestSettleRoundDone_finished转idle(t *testing.T) {
	h := prepareSettleHarness(t)
	events := make(chan string, 4)
	h.events.subscribe(context.Background(), nil, func(_ context.Context, ev HarnessRoundEvent) {
		events <- ev.Kind
	})

	// 模拟一个在飞的轮次
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "任务"})

	h.settleRoundDone(context.Background(), &cmdRoundFinished{
		roundID: 1,
		result:  map[string]any{"output": "完成"},
	})

	assert.Equal(t, harnessprotocol.HarnessStateIdle, h.State(), "普通完成应回到 idle")
	assert.Nil(t, h.st.Active())

	select {
	case k := <-events:
		assert.Equal(t, "finished", k)
	default:
		t.Error("应发出 finished 事件")
	}
}

// TestSettleRoundDone_崩溃重试一轮 验证 runtime crash → 重放查询开新轮。
func TestSettleRoundDone_崩溃重试一轮(t *testing.T) {
	h := prepareSettleHarness(t)
	events := make(chan string, 4)
	h.events.subscribe(context.Background(), nil, func(_ context.Context, ev HarnessRoundEvent) {
		events <- ev.Kind
	})

	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "会崩的任务"})

	h.settleRoundDone(context.Background(), &cmdRoundFinished{
		roundID: 1,
		err:     errors.New("崩溃"),
	})

	// 崩溃重试：开了新轮（roundSeq +1），状态仍 running
	assert.Equal(t, harnessprotocol.HarnessStateRunning, h.State(), "崩溃重试应保持 running（新轮在飞）")
	assert.NotNil(t, h.st.Active(), "崩溃重试应开新轮")

	// 事件：failed + started
	kinds := []string{}
	for i := 0; i < 2; i++ {
		select {
		case k := <-events:
			kinds = append(kinds, k)
		default:
		}
	}
	assert.Contains(t, kinds, "failed")
	assert.Contains(t, kinds, "started")
}

// TestSettleRoundDone_interrupt转idle 验证 interrupt 结果 → 转 idle 不续轮。
func TestSettleRoundDone_interrupt转idle(t *testing.T) {
	h := prepareSettleHarness(t)
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "打断"})

	h.settleRoundDone(context.Background(), &cmdRoundFinished{
		roundID: 1,
		result:  map[string]any{"result_type": "interrupt"},
	})

	assert.Equal(t, harnessprotocol.HarnessStateIdle, h.State())
	assert.Nil(t, h.st.Active())
}

// TestSettleRoundDone_followUp队列续轮 验证外部 follow-up 队列 → 续轮。
func TestSettleRoundDone_followUp队列续轮(t *testing.T) {
	h := prepareSettleHarness(t)
	events := make(chan string, 4)
	h.events.subscribe(context.Background(), nil, func(_ context.Context, ev HarnessRoundEvent) {
		events <- ev.Kind
	})

	// 预置一条 pending inbox（模拟 running 期间排队）
	h.enqueueInbox(InboxMessage{Content: "后续任务"})

	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "首轮"})

	h.settleRoundDone(context.Background(), &cmdRoundFinished{
		roundID: 1,
		result:  map[string]any{"output": "完成"},
	})

	assert.NotNil(t, h.st.Active(), "follow-up 队列应续轮")
	// 事件：finished + started
	kinds := []string{}
	for i := 0; i < 2; i++ {
		select {
		case k := <-events:
			kinds = append(kinds, k)
		default:
		}
	}
	assert.Contains(t, kinds, "finished")
	assert.Contains(t, kinds, "started")
}

// TestSettleRoundDone_taskFailed发failed事件 验证结构化 task_failed → 发 failed + idle。
func TestSettleRoundDone_taskFailed发failed事件(t *testing.T) {
	h := prepareSettleHarness(t)
	events := make(chan string, 4)
	h.events.subscribe(context.Background(), nil, func(_ context.Context, ev HarnessRoundEvent) {
		events <- ev.Kind
	})

	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "任务"})

	h.settleRoundDone(context.Background(), &cmdRoundFinished{
		roundID: 1,
		result:  map[string]any{"error": "some structured error"},
	})

	assert.Equal(t, harnessprotocol.HarnessStateIdle, h.State())
	select {
	case k := <-events:
		assert.Equal(t, "failed", k)
	default:
		t.Error("task_failed 应发 failed 事件")
	}
}

// TestSettleRoundDone_roundID不匹配跳过 验证已被取代的轮次结果被忽略。
func TestSettleRoundDone_roundID不匹配跳过(t *testing.T) {
	h := prepareSettleHarness(t)
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 2, TaskID: "t2", OriginalQuery: "新轮"})

	// 旧轮（roundID=1）的迟到结果 → 跳过
	h.settleRoundDone(context.Background(), &cmdRoundFinished{roundID: 1, result: map[string]any{}})

	assert.Equal(t, harnessprotocol.HarnessStateRunning, h.State(), "迟到结果不得改状态")
	assert.NotNil(t, h.st.Active(), "活跃轮不得被清除")
}

// TestNativeHarness_OutputsAbortSubscribe转发 覆盖简单转发方法（补齐覆盖率）。
func TestNativeHarness_OutputsAbortSubscribe转发(t *testing.T) {
	h := prepareSettleHarness(t)

	// Outputs 返回非 nil 迭代器
	out := h.Outputs()
	assert.NotNil(t, out)

	// Subscribe 注册回调（nil 回调不报错）
	assert.NoError(t, h.Subscribe(context.Background(), nil, nil))
	assert.NoError(t, h.Subscribe(context.Background(),
		func(_ context.Context, _ HarnessStateEvent) {}, nil))

	// Abort 无活跃轮时 no-op
	assert.NoError(t, h.Abort(context.Background(), true))
	assert.NoError(t, h.Abort(context.Background(), false))
}

// TestNativeHarness_SteerActive非字符串报错 覆盖 steerActive 的非法入参分支。
func TestNativeHarness_SteerActive非字符串报错(t *testing.T) {
	h := prepareSettleHarness(t)
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "首轮"})

	// 非字符串 content → steerActive 报错
	err := h.onSend(context.Background(), &InboxMessage{Content: 123, Immediate: true})
	assert.Error(t, err, "转向输入必须是字符串")
}

// TestNativeHarness_SteerActive无控制器报错 覆盖 steerActive 无 controller 分支。
func TestNativeHarness_SteerActive无控制器报错(t *testing.T) {
	h := newTestNativeHarness(t)
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "首轮"})
	// controller 为 nil（未 Start）
	err := h.onSend(context.Background(), &InboxMessage{Content: "转向", Immediate: true})
	assert.Error(t, err, "无 controller 时转向应报错")
}

// TestAck_nil通道不阻塞 覆盖 ack 的 nil channel 分支。
func TestAck_nil通道不阻塞(t *testing.T) {
	ack(nil, nil)
	ack(nil, errors.New("x"))
}

// TestHasRemainingTasks_无任务计划 覆盖 hasRemainingTasks 的 TaskPlan==nil 分支。
func TestHasRemainingTasks_无任务计划(t *testing.T) {
	h := prepareSettleHarness(t)
	// 无任务计划 → false
	assert.False(t, h.hasRemainingTasks())
}

// TestWaitAck_上下文取消 覆盖 waitAck 的 ctx.Done 分支。
func TestWaitAck_上下文取消(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消
	reply := make(chan error)
	err := waitAck(ctx, reply)
	assert.Error(t, err, "上下文取消应返回错误")
}

// TestSettleRoundDone_崩溃不可重放转idle 覆盖 InteractiveInput 不可重放分支。
// Python: _settle_round_done 的 replayable = not isinstance(original_query, InteractiveInput)
func TestSettleRoundDone_崩溃不可重放转idle(t *testing.T) {
	h := prepareSettleHarness(t)
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: &interaction.InteractiveInput{}})

	h.settleRoundDone(context.Background(), &cmdRoundFinished{roundID: 1, err: errors.New("崩溃")})

	assert.Equal(t, harnessprotocol.HarnessStateIdle, h.State(), "不可重放的崩溃应直接 idle")
	assert.Nil(t, h.st.Active())
}

// TestSettleRoundDone_aborted转idle 覆盖 coordinator.IsAborted 分支。
func TestSettleRoundDone_aborted转idle(t *testing.T) {
	h := prepareSettleHarness(t)
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "任务"})
	h.coordinator.RequestAbort() // 标记中止

	h.settleRoundDone(context.Background(), &cmdRoundFinished{roundID: 1, result: map[string]any{"output": "完成"}})

	assert.Equal(t, harnessprotocol.HarnessStateIdle, h.State(), "aborted 应转 idle 不续轮")
}

// TestSettleRoundDone_内部followUp续轮 覆盖 controller.HasFollowUp 分支。
func TestSettleRoundDone_内部followUp续轮(t *testing.T) {
	h := prepareSettleHarness(t)
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "任务"})
	h.controller.EnqueueFollowUp("内部跟进") // 预置内部 follow-up

	h.settleRoundDone(context.Background(), &cmdRoundFinished{roundID: 1, result: map[string]any{"output": "完成"}})

	assert.NotNil(t, h.st.Active(), "内部 follow-up 应续轮")
}

// TestSteerActive_字符串入队 覆盖 steerActive 的字符串成功分支。
func TestSteerActive_字符串入队(t *testing.T) {
	h := prepareSettleHarness(t)
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "首轮"})

	err := h.onSend(context.Background(), &InboxMessage{Content: "转向指令", Immediate: true})
	assert.NoError(t, err, "字符串转向应成功入队")
}

// TestSettle_协作暂停到PAUSED并解ack 验证协作暂停结算为 PAUSED 并兑现延迟应答。
func TestSettle_协作暂停到PAUSED并解ack(t *testing.T) {
	h := prepareSettleHarness(t)
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	ackCh := make(chan error, 1)
	active := &ActiveRound{
		RoundID: 1, TaskID: "t1", OriginalQuery: "任务",
		Done: make(chan struct{}), PauseAck: ackCh,
	}
	active.PauseRequested.Store(true)
	h.st.SetActive(active)

	h.settleRoundDone(context.Background(), &cmdRoundFinished{roundID: 1})

	assert.Equal(t, harnessprotocol.HarnessStatePaused, h.State())
	assert.NoError(t, <-ackCh)
}

// TestSettle_第二次崩溃放弃不重试 验证 failure_retry 的一次性护栏。
func TestSettle_第二次崩溃放弃不重试(t *testing.T) {
	h := prepareSettleHarness(t)
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	h.st.SetActive(&ActiveRound{
		RoundID: 5, TaskID: "t5", OriginalQuery: "任务",
		FailureRetry: true, Done: make(chan struct{}),
	})

	h.settleRoundDone(context.Background(), &cmdRoundFinished{roundID: 5, err: errors.New("boom")})

	assert.Equal(t, harnessprotocol.HarnessStateIdle, h.State())
	assert.Nil(t, h.st.Active())
}

// TestSettle_优雅中止丢弃待驱动输入 验证 graceful abort 丢弃排队输入后 IDLE。
func TestSettle_优雅中止丢弃待驱动输入(t *testing.T) {
	h := prepareSettleHarness(t)
	h.st.setPhase(harnessprotocol.HarnessStateRunning)
	active := &ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "任务", Done: make(chan struct{})}
	active.GracefulAbort.Store(true)
	h.st.SetActive(active)
	h.enqueueInbox(InboxMessage{Content: "不该跑"})

	h.settleRoundDone(context.Background(), &cmdRoundFinished{roundID: 1})

	assert.Equal(t, harnessprotocol.HarnessStateIdle, h.State())
	_, stillQueued := h.st.popInboxBatch()
	assert.False(t, stillQueued, "优雅中止应丢弃待驱动输入")
}
