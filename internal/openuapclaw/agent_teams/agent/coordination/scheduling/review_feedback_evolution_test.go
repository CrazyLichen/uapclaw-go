package scheduling

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	skillopt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/optimizer/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
)

// review_feedback_evolution_test.go 协调器聚合/路由测试（fake rail）。

// fakeCoordinatorRail TeamEvolutionRailLike 替身。
type fakeCoordinatorRail struct {
	existingSkills map[string]bool
	llmPayload     string
	evolveCalls    []string
	autoSave       bool
}

func (f *fakeCoordinatorRail) ReviewStore() *checkpointing.EvolutionStore              { return nil }
func (f *fakeCoordinatorRail) ReviewGenerator() *skillopt.TeamSkillExperienceOptimizer { return nil }
func (f *fakeCoordinatorRail) ReviewLanguage() string                                  { return "cn" }
func (f *fakeCoordinatorRail) AutoSave() bool                                          { return f.autoSave }
func (f *fakeCoordinatorRail) EvolveFromExternalSignals(_ context.Context, signals []*signal.EvolutionSignal, _ []map[string]any, _ *trajectory.Trajectory, userQuery string, _ *bool) evolution.ExternalEvolutionResult {
	name := ""
	if len(signals) > 0 && signals[0].SkillName != nil {
		name = *signals[0].SkillName
	}
	f.evolveCalls = append(f.evolveCalls, name+"|"+userQuery)
	return evolution.ExternalEvolutionResult{SkillName: name, Status: "attempted"}
}
func (f *fakeCoordinatorRail) RailGetTrajectory(_, _ string) (*trajectory.Trajectory, error) {
	return nil, nil
}
func (f *fakeCoordinatorRail) RailDrainApprovalEvents() []map[string]any { return nil }

// skillExistsFake 经 Observation 到 aggregation 的 SkillExists 校验在
// evolveTeamGroups 前用 teamRail.ReviewStore() —— nil store 会崩，所以
// 聚合测试用真 store。

// TestCoordinator_安全成员名过滤 负载守卫（缺 assignee/非法名跳过）。
// Python: __call__ L113-125。
func TestCoordinator_安全成员名过滤(t *testing.T) {
	rail := &fakeCoordinatorRail{}
	c := NewReviewFeedbackEvolutionCoordinator("sess", "team", func() TeamEvolutionRailLike { return rail }, true, 0.7)
	// 非法 assignee。
	_ = c.HandleReviewFeedback(context.Background(), map[string]any{
		"task_id": "t1", "review_round": int64(1), "feedback": "x", "assignee": "../evil",
	})
	// 缺 feedback。
	_ = c.HandleReviewFeedback(context.Background(), map[string]any{
		"task_id": "t1", "review_round": int64(1), "assignee": "agent1",
	})
	if len(c.observations) != 0 || len(c.newSkillPatterns) != 0 {
		t.Fatal("unsafe payloads must be skipped")
	}
}

// TestCoordinator_重复负载去重 processed 去重（task, round, feedback）。
// Python: __call__ L128-133。
func TestCoordinator_重复负载去重(t *testing.T) {
	rail := &fakeCoordinatorRail{}
	c := NewReviewFeedbackEvolutionCoordinator("sess", "team", func() TeamEvolutionRailLike { return rail }, true, 0.7)
	payload := map[string]any{
		"task_id": "t1", "review_round": int64(1), "feedback": "x", "assignee": "agent1",
	}
	_ = c.HandleReviewFeedback(context.Background(), payload)
	first := len(c.processed)
	_ = c.HandleReviewFeedback(context.Background(), payload)
	if len(c.processed) != first {
		t.Fatal("duplicate payload must not be re-processed")
	}
}

// TestCoordinator_提案键归一 newSkillProposalKey 归一。
// Python: _new_skill_proposal_key。
func TestCoordinator_提案键归一(t *testing.T) {
	key := newSkillProposalKey("先建索引 再导出!")
	if key != "先建索引-再导出" {
		t.Fatalf("proposal key = %q", key)
	}
	if len(newSkillProposalKey(strings.Repeat("a", 300))) > 160 {
		t.Fatal("proposal key must be capped at 160")
	}
}

// TestCoordinator_聚合反馈格式化 formatAggregatedFeedback 行格式。
// Python: _format_aggregated_feedback。
func TestCoordinator_聚合反馈格式化(t *testing.T) {
	out := formatAggregatedFeedback([]*TaskFeedbackObservation{
		{TaskID: "t1", ReviewRound: 1, Assignee: "agent1", Feedback: "并发不安全"},
	})
	if !strings.Contains(out, "- task=t1, round=1, assignee=agent1: 并发不安全") {
		t.Fatalf("aggregated = %s", out)
	}
	if !strings.HasPrefix(out, "团队全部任务完成") {
		t.Fatalf("header = %s", out)
	}
}

// TestCoordinator_匹配模式排除当前任务 matchingNewSkillPatterns 排除当前任务。
// Python: _matching_new_skill_patterns。
func TestCoordinator_匹配模式排除当前任务(t *testing.T) {
	c := &ReviewFeedbackEvolutionCoordinator{
		newSkillPatterns: []*NewSkillPatternObservation{
			{TaskID: "t1", ReusableGuidance: "先建索引再导出"},
			{TaskID: "t2", ReusableGuidance: "先建索引 再导出"},
			{TaskID: "t3", ReusableGuidance: "完全不同"},
		},
	}
	matches := c.matchingNewSkillPatterns("先建索引再导出", "t2")
	if len(matches) != 1 || matches[0].TaskID != "t1" {
		t.Fatalf("matches = %v", matches)
	}
}

// TestCoordinator_未启用短路 enabled=false 直接返回 false。
// Python: on_team_completed L243-245。
func TestCoordinator_未启用短路(t *testing.T) {
	rail := &fakeCoordinatorRail{}
	c := NewReviewFeedbackEvolutionCoordinator("sess", "team", func() TeamEvolutionRailLike { return rail }, false, 0.7)
	attempted, err := c.OnTeamCompleted(context.Background(), nil)
	if err != nil || attempted {
		t.Fatalf("disabled: attempted=%v err=%v", attempted, err)
	}
}
