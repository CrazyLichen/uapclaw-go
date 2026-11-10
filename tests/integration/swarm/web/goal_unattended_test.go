//go:build integration

package web_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalUnattendedSuite G4：MockLLM 编排下目标自动续跑到终态的完整状态机。
// 被测子系统（goal 域 + rails 生命周期 + session 持久化）不 mock，
// 仅在模型边界用确定性报告代替真模型（对齐 goal_lifecycle_test.go 先例）。
type GoalUnattendedSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGoalUnattendedSuite 套件入口。
func TestGoalUnattendedSuite(t *testing.T) {
	suite.Run(t, new(GoalUnattendedSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newUnattendedManager 真 store / 真 EventManager / 真回调，不 mock 被测子系统。
// Python: GoalManager(store=SessionGoalStore(session), event_manager=EventManager, ...)
// Go 先例: tests/integration/agentcore/harness/goal_lifecycle_test.go:34-50
func (s *GoalUnattendedSuite) newUnattendedManager(
	sess *session.Session, em *task_loop.EventManager,
) *goal.GoalManager {
	return goal.NewGoalManager(goal.GoalManagerConfig{
		Store:             goal.NewSessionGoalStore(sess),
		EventManager:      em,
		ControlLock:       &sync.Mutex{},
		HasOutputStream:   func() bool { return true },
		CancelActiveRound: func(context.Context, hschema.RoundWorkKind, string, string) {},
		EmitEvent:         func(hschema.InteractionEvent) {},
		NotifyWork:        func() {},
		Language:          "cn",
	})
}

// goalRoundInputs 构造一轮 goal attempt 的迭代输入。
// Python: RoundWorkItem.goal(..., run_kind="goal", run_context{goal_id, revision})
func goalRoundInputs(rec *goal.GoalRecord) *agentinterfaces.TaskIterationInputs {
	return &agentinterfaces.TaskIterationInputs{
		Query:   "原始输入",
		RunKind: agentinterfaces.RunKindGoal,
		RunContext: &agentinterfaces.RunContext{
			GoalID: rec.GoalID, Revision: rec.Revision, SessionID: rec.SessionID,
		},
	}
}

// ──────────────────────────── 测试 ────────────────────────────

// TestContinue自动续跑且query重建 两轮 continue 后仍 ACTIVE，验证每轮
// query 重建（含 previous_assessment）、attempt_count 递增、revision 不变。
//
// 重要：默认 HYBRID 策略下 COMPLETE/BLOCKED 报告需要 transcript assessor
// 验证（evaluation.py:190-208 / task_completion.go:747-759）；本套件无模型，
// transcript 不可用 → evaluator 保守回落 continue（evaluation.py:228-237）。
// 因此这里不断言「跑到 COMPLETED」，终态交给真 LLM 冒烟以软断言覆盖。
// Python: GoalManager.apply_assessment (manager.py L303) → ACTIVE 时续排 (L339)
func (s *GoalUnattendedSuite) TestContinue自动续跑且query重建() {
	ctx := s.Ctx
	sess := s.NewTestSession("web-goal-unattended-continue")
	s.Require().NoError(sess.PreRun(ctx))
	em := task_loop.NewEventManager()
	mgr := s.newUnattendedManager(sess, em)
	rail := rails.NewTaskCompletionRail()
	rail.SetGoalManager(mgr)

	rec, err := mgr.Set(ctx, "补齐登录测试", goal.SetOptions{})
	s.Require().NoError(err)
	initialRevision := rec.Revision

	for attempt := 0; attempt < 3; attempt++ {
		started, err := mgr.BeginAttempt(ctx, rec.GoalID, rec.Revision)
		s.Require().NoError(err)
		s.Require().NotNil(started, "第 %d 轮应还能开始", attempt)

		inputs := goalRoundInputs(started)
		cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)
		s.Require().NoError(rail.BeforeTaskIteration(ctx, cbc))
		s.Contains(inputs.Query, "<goal_task>", "goal 轮 query 必须替换为 XML")
		s.Contains(inputs.Query, "补齐登录测试", "每轮都要带 objective")
		if attempt > 0 {
			s.Contains(inputs.Query, "<previous_assessment>",
				"第二轮 query 必须含上一轮评估，且是重建的")
		}

		rail.GoalReportSink().Submit(goal.GoalAssessment{
			Status: goal.GoalAssessmentStatusContinue, Evidence: "本轮已推进",
		})
		inputs.Result = map[string]any{"result_type": "normal"}
		cbc.SetInputs(inputs)
		s.Require().NoError(rail.AfterTaskIteration(ctx, cbc))

		rec = mgr.Get()
		s.Require().NotNil(rec)
		s.Equal(initialRevision, rec.Revision, "continue 不得提升 revision")
	}

	s.Equal(goal.GoalStatusActive, rec.Status,
		"HYBRID 下 continue 应续跑，无 transcript 不得误判终态")
	s.Equal(3, rec.AttemptCount, "attempt_count 应逐轮递增到 3")
	s.Require().NotNil(rec.LastAssessment)
	s.Equal(goal.GoalAssessmentStatusContinue, rec.LastAssessment.Status)
	s.Nil(rec.LastStopReason, "非终态不得有 stop reason")
}

// Test硬刹车MaxAttempts 单轮上限耗尽 → BLOCKED，continue 不得无限续跑。
// Python: GoalEvaluator._apply_hard_limits (evaluation.py L289-333)
func (s *GoalUnattendedSuite) Test硬刹车MaxAttempts() {
	ctx := s.Ctx
	sess := s.NewTestSession("web-goal-unattended-budget")
	s.Require().NoError(sess.PreRun(ctx))
	em := task_loop.NewEventManager()
	mgr := s.newUnattendedManager(sess, em)
	rail := rails.NewTaskCompletionRail()
	rail.SetGoalManager(mgr)

	one := 1
	rec, err := mgr.Set(ctx, "一次机会", goal.SetOptions{MaxAttempts: &one})
	s.Require().NoError(err)

	started, err := mgr.BeginAttempt(ctx, rec.GoalID, rec.Revision)
	s.Require().NoError(err)
	inputs := goalRoundInputs(started)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)
	s.Require().NoError(rail.BeforeTaskIteration(ctx, cbc))
	rail.GoalReportSink().Submit(goal.GoalAssessment{
		Status: goal.GoalAssessmentStatusContinue, Evidence: "还没做完",
	})
	inputs.Result = map[string]any{"result_type": "normal"}
	cbc.SetInputs(inputs)
	s.Require().NoError(rail.AfterTaskIteration(ctx, cbc))

	rec = mgr.Get()
	s.Equal(goal.GoalStatusBlocked, rec.Status,
		"continue 撞上 max_attempts 应判 BLOCKED（evaluation.py L301-312）")
	s.Require().NotNil(rec.LastAssessment)
	s.Contains(strings.ToLower(rec.LastAssessment.Evidence), "max_attempts")
}

// TestContinue轮间不重复排Work 同一 goal_id+revision 不得重复入队。
// Python: EventManager.push_goal (event_manager.py L33-47) 按 goal_id+revision 去重
func (s *GoalUnattendedSuite) TestContinue轮间不重复排Work() {
	ctx := s.Ctx
	sess := s.NewTestSession("web-goal-unattended-dedup")
	s.Require().NoError(sess.PreRun(ctx))
	em := task_loop.NewEventManager()
	mgr := s.newUnattendedManager(sess, em)
	rail := rails.NewTaskCompletionRail()
	rail.SetGoalManager(mgr)

	rec, err := mgr.Set(ctx, "一步到位", goal.SetOptions{})
	s.Require().NoError(err)
	started, err := mgr.BeginAttempt(ctx, rec.GoalID, rec.Revision)
	s.Require().NoError(err)

	inputs := goalRoundInputs(started)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)
	s.Require().NoError(rail.BeforeTaskIteration(ctx, cbc))
	rail.GoalReportSink().Submit(goal.GoalAssessment{
		Status: goal.GoalAssessmentStatusContinue, Evidence: "继续推进",
	})
	inputs.Result = map[string]any{"result_type": "normal"}
	cbc.SetInputs(inputs)
	s.Require().NoError(rail.AfterTaskIteration(ctx, cbc))

	// 同一代际重复补排必须被 EventManager 去重挡掉
	ok, err := mgr.EnsureActiveGoalWorkLocked()
	s.Require().NoError(err)
	s.False(ok, "同一 goal_id+revision 不得重复入队")
	ok2, err := mgr.EnsureActiveGoalWorkLocked()
	s.Require().NoError(err)
	s.False(ok2, "重复调用也不得入队")
}
