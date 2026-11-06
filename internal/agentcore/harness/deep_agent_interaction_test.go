package harness

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeGoalCoordinator 测试用 goal 协调器
type fakeGoalCoordinator struct {
	// active 是否有 ACTIVE 记录
	active bool
	// started BeginAttempt 返回值
	started bool
	// ensureCalls 补排次数（原子读，避免与 supervisor 协程竞争）
	ensureCalls atomic.Int64
}

// HasActiveRecord 返回预置标记
func (f *fakeGoalCoordinator) HasActiveRecord() bool { return f.active }

// EnsureActiveGoalWorkLocked 记录补排次数
func (f *fakeGoalCoordinator) EnsureActiveGoalWorkLocked() { f.ensureCalls.Add(1) }

// BeginAttempt 返回预置结果
func (f *fakeGoalCoordinator) BeginAttempt(context.Context, string, int) (bool, error) {
	return f.started, nil
}

// newInteractionWork 构造一个占位 work item
func newInteractionWork() *hschema.RoundWorkItem {
	return hschema.NewUserWorkItem("r0", map[string]any{"query": "q0"}, false, true)
}

// setActiveRoundForTest 设置/清除活跃轮（持 controlMu）
func (d *DeepAgent) setActiveRoundForTest(work *hschema.RoundWorkItem) {
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	if work == nil {
		d.activeInteractionRound = nil
		return
	}
	d.activeInteractionRound = &hschema.ActiveInteractionRound{Work: work}
}

// setRoundOverrideForTest 设置轮执行覆写（持 controlMu 避免竞态）
func (d *DeepAgent) setRoundOverrideForTest(
	fn func(ctx context.Context, work *hschema.RoundWorkItem) hschema.RoundOutcome,
) {
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	d.roundOverride = fn
}

// setGoalCoordinatorForTest 设置 goal 协调器（持 controlMu）
func (d *DeepAgent) setGoalCoordinatorForTest(coordinator hschema.GoalCoordinator) {
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	d.goalCoordinator = coordinator
}

// takeWork 在 controlMu 内取一个 work
func (d *DeepAgent) takeWork(t *testing.T) *hschema.RoundWorkItem {
	t.Helper()
	d.interactionControlMu.Lock()
	defer d.interactionControlMu.Unlock()
	work := d.interactionEvents.NextWork()
	require.NotNil(t, work)
	return work
}

// startTestInteraction 启动交互监督器并登记清理
func startTestInteraction(t *testing.T) *DeepAgent {
	t.Helper()
	d := newTestDeepAgent()
	sess := session.NewSession()
	if err := d.Start(context.Background(), sess); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background()) })
	return d
}

// startInteractionForInput 启动交互状态但不启动 supervisor，
// 便于观察 SendInput 的入队结果（Python 侧 SendInput 测试同样只断言队列）。
func startInteractionForInput(t *testing.T) *DeepAgent {
	t.Helper()
	d := newTestDeepAgent()
	d.ensureInteractionState()
	sess := session.NewSession()
	if _, _, err := d.prepareInteractionTaskLoop(context.Background(), sess); err != nil {
		t.Fatalf("prepareInteractionTaskLoop 失败: %v", err)
	}
	d.interactionSession = sess
	d.interactionSessionID = sess.GetSessionID()
	d.interactionSupervisorMu.Lock()
	d.interactionStarted = true
	d.interactionStopping = true // 阻止 supervisor 启动
	d.interactionSupervisorMu.Unlock()
	t.Cleanup(func() {
		d.interactionSupervisorMu.Lock()
		d.interactionStopping = false
		d.interactionStarted = false
		d.interactionSupervisorMu.Unlock()
	})
	return d
}

// 编译期断言：*DeepAgent 满足交互监督器接口
var _ hinterfaces.InteractionSupervisorInterface = (*DeepAgent)(nil)

// ──────────────────────────── 相位机与状态工厂 ────────────────────────────

// TestInteractionPhase机_终止不可逆 验证相位迁移规则
// Python: _try_transition_interaction_phase (L4093)
func TestInteractionPhase机_终止不可逆(t *testing.T) {
	d := newTestDeepAgent()
	d.ensureInteractionState()

	assert.Equal(t, hschema.InteractionPhaseIdle, d.Phase(), "初始相位必须是 idle")

	assert.True(t, d.tryTransitionInteractionPhase(hschema.InteractionPhaseRunning))
	assert.Equal(t, hschema.InteractionPhaseRunning, d.Phase())

	assert.True(t, d.tryTransitionInteractionPhase(hschema.InteractionPhaseTerminated))
	assert.False(t, d.tryTransitionInteractionPhase(hschema.InteractionPhaseIdle),
		"terminated 不可逆")
	assert.Equal(t, hschema.InteractionPhaseTerminated, d.Phase())
}

// TestSyncExpertRoleAttachment_空实现占位 验证 ⤵️ #9.3 未移植分支的两条路径
// Python: _sync_expert_role_attachment (L3267)
func TestSyncExpertRoleAttachment_空实现占位(t *testing.T) {
	d := newTestDeepAgent()
	d.ensureInteractionState()

	// activeAgentTemplate 为 nil：与上游"会话无 expert_role 节"的清理分支等价
	d.syncExpertRoleAttachment(nil, nil)

	// 非 nil：今天不可达，只告警不 panic（等待 9.3 回填）
	d.SetActiveAgentTemplate("load-1", "专家A")
	d.syncExpertRoleAttachment(nil, nil)
	require.NotNil(t, d.activeAgentTemplate)
	assert.Equal(t, "load-1", d.activeAgentTemplate.LoadID)
	assert.Equal(t, "专家A", d.activeAgentTemplate.RoleName)

	d.ClearActiveAgentTemplate()
	assert.Nil(t, d.activeAgentTemplate)
}

// TestRoundBoundaryFrame_流契约 验证边界帧实现 stream.Schema 的三个方法
// Python: _ROUND_BOUNDARY 只作哨兵，序列化方法不会被调用，但必须满足接口
func TestRoundBoundaryFrame_流契约(t *testing.T) {
	var f stream.Schema = roundBoundaryFrame{}
	assert.Equal(t, "round_boundary", f.SchemaType())
	assert.NoError(t, f.Validate())
	assert.Nil(t, f.ToMap())
}

// TestCloseChanSafely_幂等与nil 验证屏障通道关闭的幂等性
func TestCloseChanSafely_幂等与nil(t *testing.T) {
	closeChanSafely(nil)
	ch := make(chan struct{})
	closeChanSafely(ch)
	closeChanSafely(ch)
}

// TestWaitSupervisorReady_边界 验证 nil 通道与超时都不阻塞调用方
func TestWaitSupervisorReady_边界(t *testing.T) {
	d := newTestDeepAgent()

	// supervisor 已在运行 → ensureSupervisorRunning 返回 nil，Start 不得等待
	d.waitSupervisorReady(nil, time.Millisecond)

	// supervisor 迟迟未就绪 → 有界等待后返回，不卡住 Start
	start := time.Now()
	d.waitSupervisorReady(make(chan struct{}), 20*time.Millisecond)
	assert.Less(t, time.Since(start), time.Second)
}

// TestInteractionState_幂等工厂 验证零件只建一次
func TestInteractionState_幂等工厂(t *testing.T) {
	d := newTestDeepAgent()
	d.ensureInteractionState()
	events, output, wakeup := d.interactionEvents, d.interactionOutput, d.interactionWakeup

	d.ensureInteractionState()
	assert.Same(t, events, d.interactionEvents)
	assert.Same(t, output, d.interactionOutput)
	assert.Equal(t, wakeup, d.interactionWakeup)
	assert.False(t, d.HasOutputStream(), "无宿主 attach 时不应有输出消费者")
}

// ──────────────────────────── 输出路径 ────────────────────────────

// TestAttachOutput_未启动返回nil 验证未运行时不发租约
// Python: _ensure_interaction_running (L4086)
func TestAttachOutput_未启动返回nil(t *testing.T) {
	d := newTestDeepAgent()
	d.ensureInteractionState()

	assert.Nil(t, d.AttachOutput(context.Background()))
	assert.False(t, d.HasOutputStream())
}

// TestNextOutput_委托租约管理器 验证驱动实现
// Python: next_output (L4217)
func TestNextOutput_委托租约管理器(t *testing.T) {
	d := newTestDeepAgent()
	d.ensureInteractionState()

	lease := d.interactionOutput.Attach()
	require.NotNil(t, lease)
	d.interactionOutput.Emit(&stream.OutputSchema{Type: "chunk"}, "")

	item, ok := d.NextOutput(context.Background(), lease)
	require.True(t, ok)
	assert.Equal(t, "chunk", item.(*stream.OutputSchema).Type)
}

// TestEmitInteractionEvent_无票丢弃 验证事件不投给不存在的租约
// Python: _emit_interaction_event (L4254)
func TestEmitInteractionEvent_无票丢弃(t *testing.T) {
	d := newTestDeepAgent()
	d.ensureInteractionState()
	d.emitInteractionEvent(hschema.NewExecutionErrorEvent("c", "m", nil))
	d.waitInteractionEmits(time.Second)
}

// ──────────────────────────── 转发器与边界帧 ────────────────────────────

// TestForwardSessionStream_过滤边界帧 验证哨兵不落到宿主
// Python: _forward_session_stream (L4462)
func TestForwardSessionStream_过滤边界帧(t *testing.T) {
	d := newTestDeepAgent()
	d.ensureInteractionState()
	sess := session.NewSession()
	d.interactionSession = sess

	lease := d.interactionOutput.Attach()
	require.NotNil(t, lease)

	forwarded := make(chan struct{})
	d.interactionRoundForwarded = forwarded

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); d.forwardSessionStream(ctx) }()

	require.NotNil(t, sess.StreamEmitter())
	require.NoError(t, sess.StreamEmitter().Emit(context.Background(),
		&stream.OutputSchema{Type: "chunk"}))
	require.NoError(t, sess.StreamEmitter().Emit(context.Background(), roundBoundaryFrame{}))

	item, ok := d.NextOutput(context.Background(), lease)
	require.True(t, ok)
	assert.Equal(t, "chunk", item.(*stream.OutputSchema).Type)

	select {
	case <-forwarded:
	case <-time.After(time.Second):
		t.Fatal("边界帧未置位转发屏障")
	}

	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelTimeout()
	_, ok = d.NextOutput(ctxTimeout, lease)
	assert.False(t, ok, "边界帧不得转发给宿主")

	cancel()
	<-done
}

// ──────────────────────────── 生命周期 ────────────────────────────

// TestStartStop_会话绑定与终止 验证生命周期
// Python: start (L3912) / stop (L3998)
func TestStartStop_会话绑定与终止(t *testing.T) {
	d := newTestDeepAgent()
	sess := session.NewSession()

	require.NoError(t, d.Start(context.Background(), sess))
	assert.True(t, d.InteractionStarted())
	assert.Same(t, sess, d.interactionSession)
	assert.NoError(t, d.Start(context.Background(), sess), "同会话重复 Start 幂等")

	require.NoError(t, d.Stop(context.Background()))
	assert.False(t, d.InteractionStarted())
	assert.Equal(t, hschema.InteractionPhaseTerminated, d.Phase())
	assert.ErrorIs(t, d.Start(context.Background(), sess), ErrInteractionNotRunning)
}

// TestStart_换会话报错 验证不得静默切换
// Python: L3944-3947
func TestStart_换会话报错(t *testing.T) {
	d := newTestDeepAgent()
	first := session.NewSession()
	second := session.NewSession()

	require.NoError(t, d.Start(context.Background(), first))
	t.Cleanup(func() { _ = d.Stop(context.Background()) })

	err := d.Start(context.Background(), second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already bound")
}

// TestNotifyWork_启动supervisor 验证唤醒即启动
// Python: _notify_work / _ensure_supervisor_running (L4369/L4373)
func TestNotifyWork_启动supervisor(t *testing.T) {
	d := startTestInteraction(t)
	d.notifyWork()

	d.interactionSupervisorMu.Lock()
	running := d.interactionSupervisorRunning
	d.interactionSupervisorMu.Unlock()
	assert.True(t, running)
}

// TestStop_无goroutine泄漏 验证停机收尾
func TestStop_无goroutine泄漏(t *testing.T) {
	d := newTestDeepAgent()
	require.NoError(t, d.Start(context.Background(), session.NewSession()))
	d.notifyWork()
	require.NoError(t, d.Stop(context.Background()))

	d.interactionSupervisorMu.Lock()
	defer d.interactionSupervisorMu.Unlock()
	assert.False(t, d.interactionSupervisorRunning)
	assert.Nil(t, d.interactionSupervisorDone)
	assert.Nil(t, d.interactionForwarderDone)
}

// ──────────────────────────── 输入分发 ────────────────────────────

// TestSendInput_空query报错 验证参数校验
// Python: _send_user 的 ValueError (L4107-4119)
func TestSendInput_空query报错(t *testing.T) {
	d := startInteractionForInput(t)

	assert.Error(t, d.SendInput(context.Background(), hschema.SendInputRequest{
		RequestID: "r", Inputs: map[string]any{"query": "  "},
	}))
	assert.Error(t, d.SendInput(context.Background(), hschema.SendInputRequest{
		RequestID: "r", Inputs: nil,
	}))
	assert.Error(t, d.SendInput(context.Background(), hschema.SendInputRequest{
		RequestID: "r", Inputs: map[string]any{"query": "q"}, Mode: "bogus",
	}))
}

// TestSendInput_Fresh入队且重置循环 验证新轮
// Python: fresh 分支 (L4164-4177)
func TestSendInput_Fresh入队且重置循环(t *testing.T) {
	d := startInteractionForInput(t)

	require.NoError(t, d.SendInput(context.Background(), hschema.SendInputRequest{
		RequestID: "r1", Inputs: map[string]any{"query": "你好"},
	}))

	work := d.takeWork(t)
	assert.Equal(t, hschema.RoundWorkKindUser, work.Kind)
	assert.Equal(t, "你好", work.Query())
	assert.True(t, work.ResetLoop(), "fresh 输入必须重置循环")
	assert.False(t, work.IsFollowUp)
}

// TestSendInput_KeepOpen转FollowUp 验证活跃轮期间插话
// Python: keep_open 分支 (L4151-4162)
func TestSendInput_KeepOpen转FollowUp(t *testing.T) {
	d := startInteractionForInput(t)
	d.setActiveRoundForTest(newInteractionWork())

	require.NoError(t, d.SendInput(context.Background(), hschema.SendInputRequest{
		RequestID: "r1", Inputs: map[string]any{"query": "补充"},
	}))

	work := d.takeWork(t)
	assert.True(t, work.IsFollowUp, "活跃轮期间的新输入必须标记 follow-up")
	assert.False(t, work.ResetLoop())
	d.setActiveRoundForTest(nil)
}

// TestSendInput_Steer直塞 验证 STEER 分发模式
// Python: STEER 分支 (L4144-4149)
func TestSendInput_Steer直塞(t *testing.T) {
	d := startInteractionForInput(t)
	d.setActiveRoundForTest(newInteractionWork())

	require.NoError(t, d.SendInput(context.Background(), hschema.SendInputRequest{
		RequestID: "r1",
		Inputs:    map[string]any{"query": "转向"},
		Mode:      hschema.InputDispatchModeSteer,
	}))
	assert.Equal(t, 1, d.currentLoopController().SteerLen(), "STEER 必须直塞转向队列")
	d.setActiveRoundForTest(nil)
}

// TestSendInput_打断恢复入队 验证 InteractiveInput 分支
// Python: resume 分支 (L4123-4132)
func TestSendInput_打断恢复入队(t *testing.T) {
	d := startInteractionForInput(t)
	input, err := interaction.NewInteractiveInput("ok")
	require.NoError(t, err)

	require.NoError(t, d.SendInput(context.Background(), hschema.SendInputRequest{
		RequestID: "r1", Inputs: map[string]any{"query": input},
	}))

	work := d.takeWork(t)
	assert.False(t, work.ResetLoop(), "打断恢复不得重置循环")
}

// ──────────────────────────── 后续轮次判定 ────────────────────────────

// TestBuildInteractionNextWork_goal轮不续跑 验证 goal 轮恒返回 nil
// Python: L3863
func TestBuildInteractionNextWork_goal轮不续跑(t *testing.T) {
	d := startTestInteraction(t)
	coord, ctrl, err := d.setupTaskLoop(context.Background(), d.interactionSession)
	require.NoError(t, err)

	work := hschema.NewGoalWorkItem(map[string]any{"query": "g"}, "g1", 1, d.interactionSessionID)
	next := d.buildInteractionNextWork(context.Background(), work,
		map[string]any{"status": "completed"}, d.interactionSession, coord, ctrl)
	assert.Nil(t, next, "goal 轮不得续 user work")
}

// TestBuildInteractionNextWork_待办followUp续跑 验证 follow-up 提升
// Python: L3866-3880
func TestBuildInteractionNextWork_待办followUp续跑(t *testing.T) {
	d := startTestInteraction(t)
	coord, ctrl, err := d.setupTaskLoop(context.Background(), d.interactionSession)
	require.NoError(t, err)

	state := d.LoadState(d.interactionSession)
	state.PendingFollowUps = []string{"继续"}
	d.saveState(d.interactionSession, state)

	work := hschema.NewUserWorkItem("r1", map[string]any{"query": "第一轮"}, false, true)
	next := d.buildInteractionNextWork(context.Background(), work,
		map[string]any{"status": "completed"}, d.interactionSession, coord, ctrl)

	require.NotNil(t, next)
	assert.True(t, next.IsFollowUp)
	assert.False(t, next.ResetLoop())
	assert.Equal(t, "继续", next.Query())
}

// TestBuildInteractionNextWork_中断不续跑 验证 interrupt 语义
// Python: L3861
func TestBuildInteractionNextWork_中断不续跑(t *testing.T) {
	d := startTestInteraction(t)
	coord, ctrl, err := d.setupTaskLoop(context.Background(), d.interactionSession)
	require.NoError(t, err)

	work := hschema.NewUserWorkItem("r1", map[string]any{"query": "q"}, false, true)
	next := d.buildInteractionNextWork(context.Background(), work,
		map[string]any{"result_type": "interrupt"}, d.interactionSession, coord, ctrl)
	assert.Nil(t, next)
}

// TestIsResumeInput_InteractiveInput 验证恢复输入判定
// Python: _is_resume_input (L2011)
func TestIsResumeInput_InteractiveInput(t *testing.T) {
	interactive, err := interaction.NewInteractiveInput("ok")
	require.NoError(t, err)

	assert.True(t, isResumeInput(&agentinterfaces.InvokeInputs{Query: interactive}))
	assert.False(t, isResumeInput(&agentinterfaces.InvokeInputs{
		Query: agentinterfaces.InvokeQueryString("hi"),
	}))
}

// ──────────────────────────── 轮次执行与取消 ────────────────────────────

// TestExecuteRound_标记状态与收尾 验证一轮的完整记账
// Python: _execute_round (L4497)
func TestExecuteRound_标记状态与收尾(t *testing.T) {
	d := startTestInteraction(t)
	out := d.AttachOutput(context.Background())
	require.NotNil(t, out)

	called := make(chan struct{})
	d.setRoundOverrideForTest(func(context.Context, *hschema.RoundWorkItem) hschema.RoundOutcome {
		close(called)
		return hschema.RoundOutcome{}
	})

	work := hschema.NewUserWorkItem("r1", map[string]any{"query": "q"}, false, true)
	d.executeRoundAndWait(context.Background(), work)
	<-called

	assert.Nil(t, d.ActiveRound(), "轮结束后必须清空 activeRound")
	assert.Equal(t, hschema.InteractionPhaseIdle, d.Phase())
}

// TestExecuteRound_goal轮未开始则跳过 验证 begin_attempt 门卫
// Python: L4513-4521
func TestExecuteRound_goal轮未开始则跳过(t *testing.T) {
	d := startTestInteraction(t)
	_ = d.AttachOutput(context.Background())

	ran := false
	d.setRoundOverrideForTest(func(context.Context, *hschema.RoundWorkItem) hschema.RoundOutcome {
		ran = true
		return hschema.RoundOutcome{}
	})
	d.setGoalCoordinatorForTest(&fakeGoalCoordinator{started: false})

	work := hschema.NewGoalWorkItem(map[string]any{"query": "g"}, "g1", 1, d.interactionSessionID)
	d.executeRoundAndWait(context.Background(), work)
	assert.False(t, ran, "begin_attempt 未开始时必须跳过本轮")
}

// TestCancelRound_无活跃轮返回false 验证空轮保护
// Python: cancel_round (L4268)
func TestCancelRound_无活跃轮返回false(t *testing.T) {
	d := startTestInteraction(t)
	ok, err := d.CancelRound(context.Background(), "test")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestDetachOutput_清待办 验证断线清场
// Python: detach_output (L4220)
func TestDetachOutput_清待办(t *testing.T) {
	d := startInteractionForInput(t)
	out := d.AttachOutput(context.Background())
	require.NotNil(t, out)

	require.NoError(t, d.SendInput(context.Background(), hschema.SendInputRequest{
		RequestID: "r1", Inputs: map[string]any{"query": "待办"},
	}))
	require.True(t, d.EventManager().HasPendingWork())

	d.DetachOutput(context.Background(), out.Lease().Token, false)
	assert.False(t, d.EventManager().HasPendingWork(), "detach 必须丢弃待办 work")
	assert.False(t, d.HasOutputStream())
}

// TestCancelActiveRound_有界不阻塞 验证 1.5s 上限
// Python: _cancel_active_round (L4287)
func TestCancelActiveRound_有界不阻塞(t *testing.T) {
	d := startTestInteraction(t)
	_ = d.AttachOutput(context.Background())

	gate := make(chan struct{})
	d.setRoundOverrideForTest(func(ctx context.Context, _ *hschema.RoundWorkItem) hschema.RoundOutcome {
		select {
		case <-ctx.Done():
		case <-gate:
		}
		return hschema.RoundOutcome{}
	})
	t.Cleanup(func() { close(gate) })

	work := hschema.NewUserWorkItem("r1", map[string]any{"query": "q"}, false, true)
	go d.executeRoundAndWait(context.Background(), work)
	require.Eventually(t, func() bool { return d.ActiveRound() != nil }, time.Second, 10*time.Millisecond)

	start := time.Now()
	ok, err := d.CancelRound(context.Background(), "test")
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Less(t, elapsed, 3*time.Second, "取消必须有界，不得等待整轮结束")
}

// TestCancelActiveRound_代际不匹配不取消 验证 expected_* 校验
// Python: _cancel_active_round 的 expected_* 参数 (L4312-4317)
func TestCancelActiveRound_代际不匹配不取消(t *testing.T) {
	d := startTestInteraction(t)
	_ = d.AttachOutput(context.Background())

	aborted := make(chan struct{})
	gate := make(chan struct{})
	d.setRoundOverrideForTest(func(ctx context.Context, _ *hschema.RoundWorkItem) hschema.RoundOutcome {
		select {
		case <-ctx.Done():
			close(aborted)
		case <-gate:
		}
		return hschema.RoundOutcome{}
	})
	t.Cleanup(func() { close(gate) })

	// goal 轮必须先通过 begin_attempt 门卫，否则会立即返回、观察不到活跃轮
	d.setGoalCoordinatorForTest(&fakeGoalCoordinator{started: true})

	work := hschema.NewGoalWorkItem(map[string]any{"query": "g"}, "goal-1", 1, d.interactionSessionID)
	go d.executeRoundAndWait(context.Background(), work)
	require.Eventually(t, func() bool { return d.ActiveRound() != nil }, time.Second, 10*time.Millisecond)

	wrongRev := 2
	d.interactionControlMu.Lock()
	d.cancelActiveRoundLocked(context.Background(), "mismatch", &RoundExpectation{Revision: &wrongRev})
	d.interactionControlMu.Unlock()

	select {
	case <-aborted:
		t.Fatal("代际不匹配时不得取消在飞轮")
	case <-time.After(100 * time.Millisecond):
	}

	rightRev := 1
	d.interactionControlMu.Lock()
	d.cancelActiveRoundLocked(context.Background(), "match", &RoundExpectation{Revision: &rightRev})
	d.interactionControlMu.Unlock()

	select {
	case <-aborted:
	case <-time.After(time.Second):
		t.Fatal("代际匹配时必须取消在飞轮")
	}
}

// TestPromoteLoopFollowUps 验证 follow-up 提升为 user work
// Python: _promote_loop_follow_ups (L4418)
func TestPromoteLoopFollowUps(t *testing.T) {
	d := startInteractionForInput(t)
	ctrl := d.currentLoopController()
	require.NotNil(t, ctrl)
	ctrl.EnqueueFollowUp("追一句")

	d.promoteLoopFollowUps()

	work := d.takeWork(t)
	assert.True(t, work.IsFollowUp)
	assert.Equal(t, "追一句", work.Query())
}

// ──────────────────────────── 错误与防御分支 ────────────────────────────

// TestStart_会话为nil时自建会话 验证 nil session 分支
// Python: start() 的 create_agent_session(session_id="default")（L3941-3951）
func TestStart_会话为nil时自建会话(t *testing.T) {
	d := newTestDeepAgent()
	require.NoError(t, d.Start(context.Background(), nil))
	t.Cleanup(func() { _ = d.Stop(context.Background()) })
	require.NotNil(t, d.interactionSession)
	assert.Equal(t, "default", d.interactionSessionID)
}

// TestStop_未启动时幂等 验证未 Start 的 Stop 直接返回
func TestStop_未启动时幂等(t *testing.T) {
	d := newTestDeepAgent()
	d.ensureInteractionState()
	assert.NoError(t, d.Stop(context.Background()))
}

// TestDetachOutput_陈旧票noop 验证票号不匹配时不清场
func TestDetachOutput_陈旧票noop(t *testing.T) {
	d := startTestInteraction(t)
	out := d.AttachOutput(context.Background())
	require.NotNil(t, out)

	d.DetachOutput(context.Background(), "stale-token", true)
	assert.True(t, d.HasOutputStream(), "陈旧票不得撤销当前租约")
}

// TestExecuteRound_错误码转execution_error 验证失败轮的事件投递
// Python: _execute_round 的 error_code → execution.error（L4545-4550）
func TestExecuteRound_错误码转execution_error(t *testing.T) {
	d := startTestInteraction(t)
	out := d.AttachOutput(context.Background())
	require.NotNil(t, out)

	d.setRoundOverrideForTest(func(context.Context, *hschema.RoundWorkItem) hschema.RoundOutcome {
		return hschema.RoundOutcome{ErrorCode: "round_execution_error", ErrorMessage: "boom"}
	})

	work := hschema.NewUserWorkItem("r1", map[string]any{"query": "q"}, false, true)
	d.executeRoundAndWait(context.Background(), work)

	// 错误事件应进入租约队列（先到的是它，因为 override 不产帧）
	item, ok := d.NextOutput(context.Background(), out.Lease())
	require.True(t, ok)
	assert.Equal(t, "execution.error", item.(*stream.OutputSchema).Type)
}

// TestDetachOutput_带abort取消在飞轮 验证断线取消分支
// Python: detach_output(token, abort_active_round=True)（L4220）
func TestDetachOutput_带abort取消在飞轮(t *testing.T) {
	d := startTestInteraction(t)
	out := d.AttachOutput(context.Background())
	require.NotNil(t, out)

	gate := make(chan struct{})
	d.setRoundOverrideForTest(func(ctx context.Context, _ *hschema.RoundWorkItem) hschema.RoundOutcome {
		select {
		case <-ctx.Done():
		case <-gate:
		}
		return hschema.RoundOutcome{}
	})
	t.Cleanup(func() { close(gate) })

	work := hschema.NewUserWorkItem("r1", map[string]any{"query": "q"}, false, true)
	go d.executeRoundAndWait(context.Background(), work)
	require.Eventually(t, func() bool { return d.ActiveRound() != nil }, time.Second, 10*time.Millisecond)

	d.DetachOutput(context.Background(), out.Lease().Token, true)
	assert.False(t, d.HasOutputStream())
}

// TestEmitRoundBoundary_无发射器返回false 验证边界帧失败兜底
func TestEmitRoundBoundary_无发射器返回false(t *testing.T) {
	d := newTestDeepAgent()
	d.ensureInteractionState()
	assert.False(t, d.emitRoundBoundary(nil), "nil 会话应返回 false")
	assert.False(t, d.emitRoundBoundary(&session.Session{}), "无发射器应返回 false")
}

// ──────────────────────────── supervisor 调度 ────────────────────────────

// TestSupervisor_消费待办work 验证调度闭环
// Python: _supervisor_loop 的 work 分支 (L4387-4411)
func TestSupervisor_消费待办work(t *testing.T) {
	d := startTestInteraction(t)
	_ = d.AttachOutput(context.Background())

	executed := make(chan *hschema.RoundWorkItem, 1)
	d.setRoundOverrideForTest(func(_ context.Context, work *hschema.RoundWorkItem) hschema.RoundOutcome {
		executed <- work
		return hschema.RoundOutcome{}
	})

	require.NoError(t, d.SendInput(context.Background(), hschema.SendInputRequest{
		RequestID: "r1", Inputs: map[string]any{"query": "跑一轮"},
	}))

	select {
	case work := <-executed:
		assert.Equal(t, "跑一轮", work.Query())
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor 未消费待办 work")
	}
}

// TestCloseIdle_无待办时发哨兵 验证闲置收尾
// Python: _close_idle_output_if_finished (L4433)
func TestCloseIdle_无待办时发哨兵(t *testing.T) {
	d := startTestInteraction(t)
	out := d.AttachOutput(context.Background())
	require.NotNil(t, out)

	assert.True(t, d.closeIdleOutputIfFinished(context.Background()))
	_, ok := d.NextOutput(context.Background(), out.Lease())
	assert.False(t, ok, "哨兵应结束宿主读循环")
}

// TestShouldKeepInteractionOpen_ACTIVE目标保持流开 验证 goal 分支
// Python: _should_keep_interaction_open_locked (L4442)
func TestShouldKeepInteractionOpen_ACTIVE目标保持流开(t *testing.T) {
	d := startTestInteraction(t)
	fakeGoal := &fakeGoalCoordinator{active: true}

	d.interactionControlMu.Lock()
	d.goalCoordinator = fakeGoal
	keep := d.shouldKeepInteractionOpenLocked()
	d.interactionControlMu.Unlock()

	assert.True(t, keep, "ACTIVE goal 必须保持流开")
	assert.GreaterOrEqual(t, fakeGoal.ensureCalls.Load(), int64(1), "并顺带补排 goal work")
}

// TestSetGoalCoordinator_goal分支生效 验证 9.89 注入点
func TestSetGoalCoordinator_goal分支生效(t *testing.T) {
	d := startTestInteraction(t)
	fakeGoal := &fakeGoalCoordinator{active: true}
	d.SetGoalCoordinator(fakeGoal)

	_ = d.AttachOutput(context.Background())
	// supervisor 的 close_idle 判定同样会补排，故断言 >=1 而非精确相等
	assert.GreaterOrEqual(t, fakeGoal.ensureCalls.Load(), int64(1), "ACTIVE goal 时 attach 必须补排 goal work")
}
