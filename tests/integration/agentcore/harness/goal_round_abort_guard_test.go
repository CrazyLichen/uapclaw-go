//go:build integration

package harness_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// GoalRoundAbortGuardSuite 覆盖「goal 轮在飞时不得全局 abort」的可观测前提。
//
// 背景：适配器的 abortSharedAgentIfSafe（Python: interface_deep.py:16277-16310）
// 读 _has_active_goal_round() 决定是否跳过 instance.abort。该判据落到 Go 侧就是
// ActiveRound().Work.Kind == RoundWorkKindGoal。这条用例把真实 goal 轮跑到工具
// 阻塞处，验证判据在真实链路上成立——单测无法构造在飞轮，只能在此验证。
type GoalRoundAbortGuardSuite struct {
	isuite.AgentSuite
}

// TestGoalRoundInFlight_ActiveRoundKindIsGoal goal 轮在飞时 ActiveRound.Work.Kind
// 必须是 goal。
// Python: interface_deep.py:15402-15425 _has_active_goal_round
func (s *GoalRoundAbortGuardSuite) TestGoalRoundInFlight_ActiveRoundKindIsGoal() {
	ctx := context.Background()

	blocking := testhelpers.NewBlockingTool("goal_blocking_tool")
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("goal_blocking_tool", `{}`),
		mockllm.CreateTextResponse("目标已完成"),
	)
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances:  []tool.Tool{blocking.Tool()},
		EnableTaskLoop: true,
		MaxIterations:  8,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("it-goal-round-guard")
	s.Require().NoError(sess.PreRun(ctx))
	s.Require().NoError(agent.Start(ctx, sess), "Start 失败")
	s.T().Cleanup(func() { _ = agent.Stop(context.Background()) })

	stream, attachErr := agent.AttachOutput(ctx)
	s.Require().NoError(attachErr)
	s.Require().NotNil(stream, "空闲会话必须能抢到租约")
	defer stream.Close(context.Background(), false)

	manager := agent.GoalManager()
	s.Require().NotNil(manager, "start 之后必须有 GoalManager")

	_, err = manager.Set(ctx, "修完测试", goal.SetOptions{})
	s.Require().NoError(err, "set 目标失败")

	// 幂等补排队：Set 内部可能已经排过，这里返回 false 表示"已有 work"而非失败。
	_, queueErr := manager.EnsureActiveGoalWorkLocked()
	s.Require().NoError(queueErr)

	s.Require().NoError(blocking.WaitEntered(ctx), "等待 goal 轮进入工具超时")

	active := agent.ActiveRound()
	s.Require().NotNil(active, "goal 轮应在飞")
	s.Require().NotNil(active.Work)
	s.Require().Equal(hschema.RoundWorkKindGoal, active.Work.Kind,
		"在飞轮必须是 goal —— 适配器据此跳过全局 abort")

	// goal 轮不得因为外部 abort 而消失：这里验证的是判据本身，
	// 真正的 abort 护栏在 adapter 单测 TestAbortSharedAgentIfSafe_* 覆盖。
	s.Require().True(agent.InteractionStarted(), "交互循环必须仍在运行")

	blocking.Release()
}

// TestGoalRoundAbortGuardSuite 注册套件。
func TestGoalRoundAbortGuardSuite(t *testing.T) {
	suite.Run(t, new(GoalRoundAbortGuardSuite))
}
