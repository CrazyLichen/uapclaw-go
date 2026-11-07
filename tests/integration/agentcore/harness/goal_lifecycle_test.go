//go:build integration

package harness_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// goalLifecycleSuite Goal 生命周期接线集成测试。
//
// 覆盖 TaskCompletionRail goal 分支的完整数据流：goal 轮 query 替换、
// token 累计、报告消费 → 评估 → apply_assessment → 状态转移。
// 被测子系统（goal 域模型 + rails 生命周期 + session 持久化）不 mock，
// 仅在模型边界用空模型（本套件不真实调 LLM，transcript 策略走 agent_report 短路）。
type goalLifecycleSuite struct {
	isuite.AgentSuite
}

// newGoalManager 构造绑定真实 session 的 GoalManager。
func (s *goalLifecycleSuite) newGoalManager(sess *session.Session) *goal.GoalManager {
	return goal.NewGoalManager(goal.GoalManagerConfig{
		Store:             goal.NewSessionGoalStore(sess),
		EventManager:      task_loop.NewEventManager(),
		ControlLock:       &sync.Mutex{},
		HasOutputStream:   func() bool { return false },
		CancelActiveRound: func(_ context.Context, _ hschema.RoundWorkKind, _, _ string) {},
		EmitEvent:         func(_ hschema.InteractionEvent) {},
		NotifyWork:        func() {},
		Language:          "cn",
	})
}

// TestGoalRailQuery替换与状态转移 验证 goal 轮 query 替换 + 报告消费 + apply 状态转移。
//
// 数据流：Set(goal) → BeforeTaskIteration(goal 轮) 替换 query 为 <goal_task> →
// AfterModelCall 累计 token → sink 提交 continue → AfterTaskIteration 评估
// → ApplyAssessment → 记录仍 ACTIVE 且 last_assessment 为 continue。
func (s *goalLifecycleSuite) TestGoalRailQuery替换与状态转移() {
	ctx := s.Ctx
	sess := s.NewTestSession("goal-lifecycle-1")
	s.Require().NoError(sess.PreRun(ctx))

	manager := s.newGoalManager(sess)
	record, err := manager.Set(ctx, "构建一个 REST API", goal.SetOptions{})
	s.Require().NoError(err)
	s.Require().Equal(goal.GoalStatusActive, record.Status)

	rail := rails.NewTaskCompletionRail()
	rail.SetGoalManager(manager)

	// 构造 goal 轮的迭代输入（RunKind=goal + RunContext{goal_id, revision}）
	inputs := &agentinterfaces.TaskIterationInputs{
		Query:   "原始用户输入",
		RunKind: agentinterfaces.RunKindGoal,
		RunContext: &agentinterfaces.RunContext{
			GoalID:    record.GoalID,
			Revision:  record.Revision,
			SessionID: sess.GetSessionID(),
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)

	// BeforeTaskIteration：goal 轮替换 query 为 <goal_task> XML
	s.Require().NoError(rail.BeforeTaskIteration(ctx, cbc))
	s.Require().NotEqual("原始用户输入", inputs.Query, "goal 轮应替换 query")
	s.Require().Contains(inputs.Query, "<goal_task>", "query 应为 <goal_task> XML")
	s.Require().Contains(inputs.Query, "构建一个 REST API", "query 应含 objective")

	// 模拟模型提交 continue 报告
	rail.GoalReportSink().Submit(goal.GoalAssessment{
		Status:          goal.GoalAssessmentStatusContinue,
		Evidence:        "已创建接口骨架",
		RemainingWork:   strPtr("补齐测试"),
		NextInstruction: strPtr("编写单元测试"),
	})

	// AfterTaskIteration：消费报告 → 评估 → apply
	inputs.Result = map[string]any{"result_type": "normal"}
	cbc.SetInputs(inputs)
	s.Require().NoError(rail.AfterTaskIteration(ctx, cbc))

	// 断言记录仍 ACTIVE 且 last_assessment 为 continue
	updated := manager.Get()
	s.Require().Equal(goal.GoalStatusActive, updated.Status)
	s.Require().NotNil(updated.LastAssessment)
	s.Require().Equal(goal.GoalAssessmentStatusContinue, updated.LastAssessment.Status)
}

// TestGoalRail_执行错误直接BLOCKED 验证 round error 直接判 BLOCKED（缺口 A 回归）。
func (s *goalLifecycleSuite) TestGoalRail_执行错误直接BLOCKED() {
	ctx := s.Ctx
	sess := s.NewTestSession("goal-lifecycle-2")
	s.Require().NoError(sess.PreRun(ctx))

	manager := s.newGoalManager(sess)
	record, err := manager.Set(ctx, "构建 API", goal.SetOptions{})
	s.Require().NoError(err)

	rail := rails.NewTaskCompletionRail()
	rail.SetGoalManager(manager)

	inputs := &agentinterfaces.TaskIterationInputs{
		RunKind: agentinterfaces.RunKindGoal,
		RunContext: &agentinterfaces.RunContext{
			GoalID:    record.GoalID,
			Revision:  record.Revision,
			SessionID: sess.GetSessionID(),
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)
	s.Require().NoError(rail.BeforeTaskIteration(ctx, cbc))

	// 模拟 executor 失败路径：exception 已设置（缺口 A 的 after_task_iteration 触发点）
	cbc.SetException(errors.New("round execution failed"))
	s.Require().NoError(rail.AfterTaskIteration(ctx, cbc))

	// 断言直接 BLOCKED
	updated := manager.Get()
	s.Require().Equal(goal.GoalStatusBlocked, updated.Status)
	s.Require().NotNil(updated.LastStopReason)
	s.Require().Equal("blocked", *updated.LastStopReason)
}

// TestGoalLifecycleSuite 套件入口
func TestGoalLifecycleSuite(t *testing.T) {
	suite.Run(t, new(goalLifecycleSuite))
}

// strPtr 返回字符串指针。
func strPtr(s string) *string { return &s }

// TestGoalTools注册_Start后可用 验证 Start 后 submit_goal_report / get_current_goal
// 已注册到 AbilityManager（守护「绑定后必须再 Init」的修复）。
func (s *goalLifecycleSuite) TestGoalTools注册_Start后可用() {
	ctx := s.Ctx
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{EnableTaskLoop: true})
	s.Require().NoError(err)

	sess := s.NewTestSession("goal-tools-1")
	s.Require().NoError(sess.PreRun(ctx))
	s.Require().NoError(agent.Start(ctx, sess))
	s.T().Cleanup(func() { _ = agent.Stop(context.Background()) })

	names := make(map[string]bool)
	for _, ability := range agent.AbilityManager().List() {
		names[ability.AbilityName()] = true
	}
	s.True(names["submit_goal_report"], "submit_goal_report 应已注册，实际=%v", names)
	s.True(names["get_current_goal"], "get_current_goal 应已注册，实际=%v", names)
}
