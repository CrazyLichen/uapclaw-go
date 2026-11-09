package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestStartInteraction_实例缺失返回错误 未 CreateInstance 时明确报错。
// Python: interface_deep.py:15052 `raise RuntimeError("DeepAgent instance is not initialized")`
func TestStartInteraction_实例缺失返回错误(t *testing.T) {
	d := &DeepAdapter{}
	err := d.StartInteraction(context.Background(), "sess-x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "实例未初始化")
}

// TestStartInteractionIfNeeded_实例缺失为noop 请求期惰建对未初始化实例不报错。
func TestStartInteractionIfNeeded_实例缺失为noop(t *testing.T) {
	d := &DeepAdapter{}
	require.NoError(t, d.startInteractionIfNeeded(context.Background(), "sess-x"))
	require.False(t, d.interactionStarted())
	require.Nil(t, d.interactionSession())
}

// TestStartInteraction_构造GoalManager start 是 GoalManager 的唯一构造点。
// Python: interface_deep.py:15044-15072 → deep_agent.py:3912-3996
func TestStartInteraction_构造GoalManager(t *testing.T) {
	ctx := context.Background()
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card: agentschema.NewAgentCard(agentschema.WithAgentName("goal-start-agent")),
	})
	require.NoError(t, err, "创建 DeepAgent 失败")
	t.Cleanup(func() { _ = agent.Stop(context.Background()) })

	d := &DeepAdapter{instance: agent}
	require.Nil(t, d.goalManager(), "启动前 GoalManager 必须为 nil")

	require.NoError(t, d.StartInteraction(ctx, "sess-start"))
	require.True(t, d.interactionStarted())
	require.NotNil(t, d.goalManager(), "start 之后 GoalManager 必须存在")
	require.Equal(t, "sess-start", d.interactionSession().GetSessionID())
}

// TestStartInteraction_二次绑定同会话幂等 重复调用不报错也不重建。
// Python: deep_agent.py:3927-3938（同会话重复 start 返回 None）
func TestStartInteraction_二次绑定同会话幂等(t *testing.T) {
	ctx := context.Background()
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card: agentschema.NewAgentCard(agentschema.WithAgentName("goal-dup-agent")),
	})
	require.NoError(t, err, "创建 DeepAgent 失败")
	t.Cleanup(func() { _ = agent.Stop(context.Background()) })

	d := &DeepAdapter{instance: agent}
	require.NoError(t, d.StartInteraction(ctx, "sess-dup"))
	first := d.goalManager()
	require.NotNil(t, first)

	require.NoError(t, d.startInteractionIfNeeded(ctx, "sess-dup"))
	require.Same(t, first, d.goalManager(), "重复启动不得重建 GoalManager")
}

// TestStartInteractionIfNeeded_改绑其它会话报错 一个交互循环只能绑一个会话。
// Python: deep_agent.py:3939-3944
func TestStartInteractionIfNeeded_改绑其它会话报错(t *testing.T) {
	ctx := context.Background()
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card: agentschema.NewAgentCard(agentschema.WithAgentName("goal-rebind-agent")),
	})
	require.NoError(t, err, "创建 DeepAgent 失败")
	t.Cleanup(func() { _ = agent.Stop(context.Background()) })

	d := &DeepAdapter{instance: agent}
	require.NoError(t, d.StartInteraction(ctx, "sess-a"))
	err = d.startInteractionIfNeeded(ctx, "sess-b")
	require.Error(t, err)
	require.Contains(t, err.Error(), "无法改绑")
}

// newStartedGoalAdapter 构造一个已 start_interaction 的适配器（GoalManager 就位）。
func newStartedGoalAdapter(t *testing.T, sessionID string) *DeepAdapter {
	t.Helper()
	ctx := context.Background()
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card: agentschema.NewAgentCard(agentschema.WithAgentName("goal-test-agent")),
	})
	require.NoError(t, err, "创建 DeepAgent 失败")
	t.Cleanup(func() { _ = agent.Stop(context.Background()) })

	d := &DeepAdapter{
		instance:         agent,
		isSessionScoped:  true,
		parentSessionID:  sessionID,
		activeSessionIDs: make(map[string]int),
	}
	require.NoError(t, d.StartInteraction(ctx, sessionID))
	return d
}

// TestOpenInteractionStream_拿到租约返回帧通道 空闲时成为唯一读者。
// Python: interface_deep.py:19996-20010
func TestOpenInteractionStream_拿到租约返回帧通道(t *testing.T) {
	d := newStartedGoalAdapter(t, "sess-open-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &schema.AgentRequest{RequestID: "r-open-1", ChannelID: "web",
		Params: mustJSON(t, map[string]any{"query": "hi"})}
	opened, err := d.openInteractionStream(ctx, req, map[string]any{"query": "hi"}, "sess-open-1")
	require.NoError(t, err)
	require.False(t, opened.accepted)
	require.Nil(t, opened.controlFrame)
	require.NotNil(t, opened.rawCh)
	require.NotNil(t, d.interactionOutputHandoff, "拿到租约后必须挂排空屏障")
}

// TestOpenInteractionStream_无租约非inject返回accepted 另一读者持有时放弃。
// Python: interface_deep.py:16035-16040
func TestOpenInteractionStream_无租约非inject返回accepted(t *testing.T) {
	d := newStartedGoalAdapter(t, "sess-open-2")
	holder, err := d.instance.AttachOutput(context.Background())
	require.NoError(t, err)
	require.NotNil(t, holder)
	defer holder.Close(context.Background(), false)

	req := &schema.AgentRequest{RequestID: "r-open-2", ChannelID: "web",
		Params: mustJSON(t, map[string]any{"query": "hi"})}
	opened, err := d.openInteractionStream(context.Background(), req,
		map[string]any{"query": "hi"}, "sess-open-2")
	require.NoError(t, err)
	require.True(t, opened.accepted, "非 inject 模式拿不到租约应回 accepted")
	require.Nil(t, opened.rawCh)
}

// TestOpenInteractionStream_inject模式无租约投递但不读输出 steer 的输入必须送进
// 在跑的轮，但调用方不读输出（与上游一致：interaction_stream 为 None 时只回 accepted）。
// Python: interface_deep.py:16037（send_without_output=True 不走放弃分支）+ :19996-20005
func TestOpenInteractionStream_inject模式无租约投递但不读输出(t *testing.T) {
	d := newStartedGoalAdapter(t, "sess-open-3")
	holder, err := d.instance.AttachOutput(context.Background())
	require.NoError(t, err)
	require.NotNil(t, holder)
	defer holder.Close(context.Background(), false)

	req := &schema.AgentRequest{RequestID: "r-open-3", ChannelID: "web",
		Params: mustJSON(t, map[string]any{"query": "转向", "input_mode": "steer"})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened, err := d.openInteractionStream(ctx, req,
		map[string]any{"query": "转向"}, "sess-open-3")
	require.NoError(t, err, "inject 模式必须投递且不得报错")
	require.True(t, opened.accepted, "没有可读流：调用方只回受理确认")
	require.Nil(t, opened.rawCh)

	// 直接验证 attachAndSendInputs：inject 模式在租约被占时仍不报错（SendInput 已执行），
	// 而非 inject 模式会静默放弃。
	_, _, injectErr := d.attachAndSendInputs(context.Background(), req,
		map[string]any{"query": "转向"}, true)
	require.NoError(t, injectErr)
}

// TestResolveInputDispatchMode 缺省 follow_up，steer 直通。
// Python: interface_deep.py:15940-15960
func TestResolveInputDispatchMode(t *testing.T) {
	require.Equal(t, hschema.InputDispatchModeFollowUp, resolveInputDispatchMode(nil))
	require.Equal(t, hschema.InputDispatchModeFollowUp,
		resolveInputDispatchMode(mustJSON(t, map[string]any{"query": "x"})))
	require.Equal(t, hschema.InputDispatchModeSteer,
		resolveInputDispatchMode(mustJSON(t, map[string]any{"input_mode": "steer"})))
}

// TestShouldInjectIntoExistingInteraction steer/follow_up/HITL 答案都算注入。
// Python: interface_deep.py:15802-15806
func TestShouldInjectIntoExistingInteraction(t *testing.T) {
	d := &DeepAdapter{}
	require.False(t, d.shouldInjectIntoExistingInteraction(nil))
	require.True(t, d.shouldInjectIntoExistingInteraction(
		mustJSON(t, map[string]any{"input_mode": "steer"})))
	require.True(t, d.shouldInjectIntoExistingInteraction(
		mustJSON(t, map[string]any{"input_mode": "follow_up"})))
	require.True(t, d.shouldInjectIntoExistingInteraction(
		mustJSON(t, map[string]any{"answers": []any{map[string]any{"a": 1}}})))
}

// TestAbortSharedAgentIfSafe_无实例直接放行 未初始化时无共享实例可中止。
// Python: interface_deep.py:16279 `if self._instance is None: return True`
func TestAbortSharedAgentIfSafe_无实例直接放行(t *testing.T) {
	d := &DeepAdapter{}
	require.True(t, d.abortSharedAgentIfSafe(context.Background(), "sess-a", "cancel"))
}

// TestAbortSharedAgentIfSafe_无冲突时中止 无 goal 轮、无其它会话 → 执行中止。
// Python: interface_deep.py:16303-16310
func TestAbortSharedAgentIfSafe_无冲突时中止(t *testing.T) {
	d := newStartedGoalAdapter(t, "sess-abort-ok")
	require.False(t, d.hasActiveGoalRound())
	require.True(t, d.abortSharedAgentIfSafe(context.Background(), "sess-abort-ok", "cancel"))
}

// TestAbortSharedAgentIfSafe_其它会话活跃时跳过 全局 abort 会误伤别的会话。
// Python: interface_deep.py:16291-16302
func TestAbortSharedAgentIfSafe_其它会话活跃时跳过(t *testing.T) {
	d := newStartedGoalAdapter(t, "sess-abort-other")
	d.markSessionActive("sess-other")
	t.Cleanup(func() { d.unmarkSessionActive("sess-other") })

	require.Equal(t, 1, d.otherActiveSessions("sess-abort-other"))
	require.False(t, d.abortSharedAgentIfSafe(context.Background(), "sess-abort-other", "cancel"))
}

// TestAbortSharedAgentIfSafe_goal记录活跃但无在飞轮仍中止 记录 ACTIVE 不等于轮在飞，
// 只有真正跑着 goal 轮才跳过中止（否则用户 ESC 会永远失效）。
// Python: interface_deep.py:16286（判的是 _has_active_goal_round，不是记录状态）
func TestAbortSharedAgentIfSafe_goal记录活跃但无在飞轮仍中止(t *testing.T) {
	ctx := context.Background()
	d := newStartedGoalAdapter(t, "sess-abort-record")
	_, err := d.dispatchGoalControl(ctx, "sess-abort-record", "set", "修完测试", false, 0, 0)
	require.NoError(t, err)
	require.True(t, d.goalRecordIsActive(), "记录应为 ACTIVE")
	require.False(t, d.hasActiveGoalRound(), "但没有在飞的 goal 轮")
	require.True(t, d.abortSharedAgentIfSafe(ctx, "sess-abort-record", "cancel"),
		"仅记录 ACTIVE 不得阻止中止")
}

// TestIsRoundTerminalChunkType 终块三类型。
// Python: interface_deep.py:197-203 _ROUND_TERMINAL_CHUNK_TYPES
func TestIsRoundTerminalChunkType(t *testing.T) {
	require.True(t, isRoundTerminalChunkType("answer"))
	require.True(t, isRoundTerminalChunkType("chat.ask_user_question"))
	require.True(t, isRoundTerminalChunkType("__interaction__"))
	require.False(t, isRoundTerminalChunkType("llm_output"))
	require.False(t, isRoundTerminalChunkType(""))
}

// TestTrackRoundOutputBoundary_每轮只采样一次 输出从队列读，active_round 可能已
// 指向下一轮；必须按轮闩住归属，否则 user 轮的尾巴会被误标成 goal。
// Python: interface_deep.py:15542-15573
func TestTrackRoundOutputBoundary_每轮只采样一次(t *testing.T) {
	d := &DeepAdapter{}
	// 第一轮归属 user（此处直接置闩，等价于首块采样结果）
	d.streamRoundKindLatch = "user"

	// 调度器切到 goal，但上一轮尾巴仍在队列里 —— 闩不能变
	d.trackRoundOutputBoundary(&stream.OutputSchema{Type: "llm_output"})
	require.Equal(t, "user", d.streamRoundKindLatch)

	// 上一轮终块到达 → 置位结束标志
	d.trackRoundOutputBoundary(&stream.OutputSchema{Type: "answer"})
	require.True(t, d.streamRoundOutputEnded)
	require.Equal(t, "user", d.streamRoundKindLatch, "终块本身仍属旧轮")

	// 下一块重新采样：无在飞轮 → 空串
	d.trackRoundOutputBoundary(&stream.OutputSchema{Type: "llm_output"})
	require.Equal(t, "", d.streamRoundKindLatch)
	require.False(t, d.streamRoundOutputEnded)
}

// TestNoteRoundVisibleText_尾部截断 上限 256KB，保留尾部。
// Python: interface_deep.py:15518-15525
func TestNoteRoundVisibleText_尾部截断(t *testing.T) {
	d := &DeepAdapter{}
	d.noteRoundVisibleText("")
	require.Equal(t, "", d.streamRoundVisibleText)

	d.noteRoundVisibleText("abc")
	d.noteRoundVisibleText("def")
	require.Equal(t, "abcdef", d.streamRoundVisibleText)

	big := strings.Repeat("x", roundVisibleTextMaxChars)
	d.noteRoundVisibleText(big)
	require.Len(t, d.streamRoundVisibleText, roundVisibleTextMaxChars)
}

// TestAdaptGoalIntermediateFinal_降级与丢弃 中间 final 不能当正式回答。
// Python: interface_deep.py:15619-15637
func TestAdaptGoalIntermediateFinal_降级与丢弃(t *testing.T) {
	ctx := context.Background()
	d := newStartedGoalAdapter(t, "sess-demote")
	_, err := d.dispatchGoalControl(ctx, "sess-demote", "set", "修完测试", false, 0, 0)
	require.NoError(t, err)
	d.streamRoundKindLatch = "goal"

	// 已流过的文本：直接丢弃（避免气泡重复）
	d.noteRoundVisibleText("完整答案")
	adapted, keep := d.adaptGoalIntermediateFinal(map[string]any{
		"event_type": "chat.final", "content": "完整答案",
	})
	require.False(t, keep)
	require.Nil(t, adapted)

	// 未流过的文本：降级为 chat.delta + goal_intermediate
	adapted, keep = d.adaptGoalIntermediateFinal(map[string]any{
		"event_type": "chat.final", "content": "新内容",
	})
	require.True(t, keep)
	require.Equal(t, "chat.delta", adapted["event_type"])
	require.Equal(t, true, adapted[goalIntermediateField])

	// 非 final 不降级
	adapted, keep = d.adaptGoalIntermediateFinal(map[string]any{
		"event_type": "chat.delta", "content": "增量",
	})
	require.True(t, keep)
	require.Equal(t, "chat.delta", adapted["event_type"])
	require.NotContains(t, adapted, goalIntermediateField)

	// user 轮的 final 不降级
	d.streamRoundKindLatch = "user"
	d.resetRoundKindLatch()
	d.streamRoundKindLatch = "user"
	adapted, keep = d.adaptGoalIntermediateFinal(map[string]any{
		"event_type": "chat.final", "content": "用户答案",
	})
	require.True(t, keep)
	require.Equal(t, "chat.final", adapted["event_type"])
	require.NotContains(t, adapted, goalIntermediateField)
}

// TestAdaptGoalIntermediateFinal_目标非ACTIVE不降级 记录已完成后 final 就是正式回答。
// Python: interface_deep.py:15470（先判 goal record ACTIVE）
func TestAdaptGoalIntermediateFinal_目标非ACTIVE不降级(t *testing.T) {
	ctx := context.Background()
	d := newStartedGoalAdapter(t, "sess-no-demote")
	_, err := d.dispatchGoalControl(ctx, "sess-no-demote", "set", "目标", false, 0, 0)
	require.NoError(t, err)
	_, err = d.dispatchGoalControl(ctx, "sess-no-demote", "pause", "", false, 0, 0)
	require.NoError(t, err)
	d.streamRoundKindLatch = "goal"

	adapted, keep := d.adaptGoalIntermediateFinal(map[string]any{
		"event_type": "chat.final", "content": "最终答案",
	})
	require.True(t, keep)
	require.Equal(t, "chat.final", adapted["event_type"])
}

// TestGoalIntermediateFinalRepeatsStreamedText 空答案视为重复，包含关系视为重复。
// Python: interface_deep.py:15527-15540
func TestGoalIntermediateFinalRepeatsStreamedText(t *testing.T) {
	d := &DeepAdapter{}
	require.True(t, d.goalIntermediateFinalRepeatsStreamedText(""), "空答案视为已重复")
	require.True(t, d.goalIntermediateFinalRepeatsStreamedText("   "))
	require.False(t, d.goalIntermediateFinalRepeatsStreamedText("答案"), "未流过任何文本时不重复")

	d.noteRoundVisibleText("前缀 答案 后缀")
	require.True(t, d.goalIntermediateFinalRepeatsStreamedText("答案"))
	require.False(t, d.goalIntermediateFinalRepeatsStreamedText("别的"))
}

// goalOpRequest 构造流式 command.goal 请求（set/resume 才进两段映射）。
func goalOpRequest(t *testing.T, sessionID string, params map[string]any) *schema.AgentRequest {
	t.Helper()
	sid := sessionID
	return &schema.AgentRequest{
		RequestID: "req-goal-op",
		ChannelID: "web",
		SessionID: &sid,
		ReqMethod: schema.ReqMethodCommandGoal,
		IsStream:  true,
		Params:    mustJSON(t, params),
	}
}

// TestOpenInteractionWithGoalOp_二次set未确认返回confirmRequired attach 后 dispatch，
// confirm_required 提前结束且不投递输入。
// Python: interface_deep.py:19823-19845
func TestOpenInteractionWithGoalOp_二次set未确认返回confirmRequired(t *testing.T) {
	isolateGoalHistoryDataDir(t)
	ctx := context.Background()
	d := newStartedGoalAdapter(t, "sess-goalop-confirm")
	_, err := d.dispatchGoalControl(ctx, "sess-goalop-confirm", "set", "第一个", false, 0, 0)
	require.NoError(t, err)

	req := goalOpRequest(t, "sess-goalop-confirm", map[string]any{
		"action": "set", "objective": "第二个", "mode": "agent",
	})
	opened, err := d.openInteractionStream(ctx, req, map[string]any{"query": "第二个"}, "sess-goalop-confirm")
	require.NoError(t, err)
	require.NotNil(t, opened.controlFrame, "已存在目标时必须以 confirm_required 提前结束")
	require.Equal(t, "goal.confirm_required", opened.controlFrame["event_type"])
	require.Equal(t, "第二个", opened.controlFrame["requested_objective"])
	require.Nil(t, opened.rawCh)
	// 未确认覆盖：不得写 objective 历史
	require.Empty(t, goalHistoryRecords(t, "sess-goalop-confirm"))
}

// TestOpenInteractionWithGoalOp_set成功写objective历史并继续 成功才落历史。
// Python: interface_deep.py:19850-19870
func TestOpenInteractionWithGoalOp_set成功写objective历史并继续(t *testing.T) {
	isolateGoalHistoryDataDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newStartedGoalAdapter(t, "sess-goalop-ok")

	req := goalOpRequest(t, "sess-goalop-ok", map[string]any{
		"action": "set", "objective": "修完测试", "mode": "agent",
	})
	opened, err := d.openInteractionStream(ctx, req, map[string]any{"query": "修完测试"}, "sess-goalop-ok")
	require.NoError(t, err)
	require.Nil(t, opened.controlFrame, "set 成功不得提前结束")
	require.False(t, opened.accepted)

	records := goalHistoryRecords(t, "sess-goalop-ok")
	require.Len(t, records, 1, "set 成功必须写 objective 历史")
	require.Equal(t, "user", records[0]["role"])
	require.Equal(t, "修完测试", records[0]["content"])
	require.Equal(t, true, records[0]["is_goal_objective_message"])
}

// TestOpenInteractionWithGoalOp_resume无目标返回error resume 无目标时报 no_goal。
// Python: interface_deep.py:19846-19875
func TestOpenInteractionWithGoalOp_resume无目标返回error(t *testing.T) {
	isolateGoalHistoryDataDir(t)
	ctx := context.Background()
	d := newStartedGoalAdapter(t, "sess-goalop-resume")

	req := goalOpRequest(t, "sess-goalop-resume", map[string]any{
		"action": "resume", "mode": "agent",
	})
	opened, err := d.openInteractionStream(ctx, req, map[string]any{"query": ""}, "sess-goalop-resume")
	require.NoError(t, err)
	require.NotNil(t, opened.controlFrame)
	require.Equal(t, "chat.error", opened.controlFrame["event_type"])
	require.Equal(t, "no_goal", opened.controlFrame["code"])
}
