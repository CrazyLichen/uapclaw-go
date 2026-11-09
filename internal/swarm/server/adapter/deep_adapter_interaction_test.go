package adapter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
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
	rawCh, accepted, err := d.openInteractionStream(ctx, req, map[string]any{"query": "hi"}, "sess-open-1")
	require.NoError(t, err)
	require.False(t, accepted)
	require.NotNil(t, rawCh)
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
	rawCh, accepted, err := d.openInteractionStream(context.Background(), req,
		map[string]any{"query": "hi"}, "sess-open-2")
	require.NoError(t, err)
	require.True(t, accepted, "非 inject 模式拿不到租约应回 accepted")
	require.Nil(t, rawCh)
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
	rawCh, accepted, err := d.openInteractionStream(ctx, req,
		map[string]any{"query": "转向"}, "sess-open-3")
	require.NoError(t, err, "inject 模式必须投递且不得报错")
	require.True(t, accepted, "没有可读流：调用方只回受理确认")
	require.Nil(t, rawCh)

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
