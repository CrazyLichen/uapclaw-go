package adapter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
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
