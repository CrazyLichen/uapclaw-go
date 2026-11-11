//go:build integration

package adapter_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// GoalControlPlaneSuite goal 控制面端到端：真实 DeepAdapter + 真实 GoalManager。
//
// 覆盖 10.3.27 的主链路：start_interaction 构造 GoalManager → 五 action 改状态 →
// 流式 set 走 attach→control 两段映射并落 objective 历史。
// Python: interface_deep.py:15044 / :17744 / :19823
type GoalControlPlaneSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境
	env *testhelpers.AdapterTestEnv
}

// SetupTest 每个测试方法前构建隔离环境（数据目录 / MockLLM / env 备份）。
// 与 session_usage_isolation_test.go 的既有写法一致。
func (s *GoalControlPlaneSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "goal_control_plane")
}

// newGoalAdapter 建真实 DeepAdapter 并完成 start_interaction。
func (s *GoalControlPlaneSuite) newGoalAdapter(sessionID string) *adapter.DeepAdapter {
	// 10.3.28f 起根适配器是纯 router：不建自己的 DeepAgent，方法入口的分流头
	// 按 sessionID 造 child，child 的 CreateInstance 尾段自动 start_interaction。
	// 因此这里**不**标会话级，直接用根驱动（与 session_usage_isolation_test 一致）。
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", "", nil),
		"CreateInstance 失败")
	s.T().Cleanup(func() { _ = d.Cleanup() })
	return d
}

// TestFiveActions_状态往返 set→get→pause→resume→clear 全链路。
// Python: interface_deep.py:17764-17930
func (s *GoalControlPlaneSuite) TestFiveActions_状态往返() {
	d := s.newGoalAdapter("it-goal-five")
	ctx := context.Background()

	setOut, err := d.DispatchGoalControl(ctx, "it-goal-five", "set", "修完测试", false, 0, 0)
	s.Require().NoError(err)
	s.Require().Equal("goal_stream", setOut["result_type"])

	getOut, err := d.DispatchGoalControl(ctx, "it-goal-five", "get", "", false, 0, 0)
	s.Require().NoError(err)
	s.Require().Equal("Goal: 修完测试", getOut["output"])

	dupOut, err := d.DispatchGoalControl(ctx, "it-goal-five", "set", "新目标", false, 0, 0)
	s.Require().NoError(err)
	s.Require().Equal("goal_confirm_required", dupOut["result_type"])
	s.Require().Equal("already_exists", dupOut["error_code"])

	pauseOut, err := d.DispatchGoalControl(ctx, "it-goal-five", "pause", "", false, 0, 0)
	s.Require().NoError(err)
	s.Require().Equal("Goal paused.", pauseOut["output"])

	resumeOut, err := d.DispatchGoalControl(ctx, "it-goal-five", "resume", "", false, 0, 0)
	s.Require().NoError(err)
	s.Require().Equal("goal_stream", resumeOut["result_type"])

	clearOut, err := d.DispatchGoalControl(ctx, "it-goal-five", "clear", "", false, 0, 0)
	s.Require().NoError(err)
	s.Require().Equal("Goal cleared.", clearOut["output"])
	s.Require().NotNil(clearOut["cleared_goal"])
}

// TestUnknownAction_返回invalidAction 非法 action 不 panic。
// Python: interface_deep.py:17898-17902
func (s *GoalControlPlaneSuite) TestUnknownAction_返回invalidAction() {
	d := s.newGoalAdapter("it-goal-bad")
	out, err := d.DispatchGoalControl(context.Background(), "it-goal-bad", "explode", "", false, 0, 0)
	s.Require().NoError(err)
	s.Require().Equal("goal_error", out["result_type"])
	s.Require().Equal("invalid_action", out["error_code"])
}

// TestGoalControlPlaneSuite 注册套件。
func TestGoalControlPlaneSuite(t *testing.T) {
	suite.Run(t, new(GoalControlPlaneSuite))
}
