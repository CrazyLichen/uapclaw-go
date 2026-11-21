//go:build integration

// 25.2b 评审链 I-4 全链集成（spec §7 Task 13 Step 2）。
// 真实组件直连：DB + TeamTaskManager（评审面）+ TeamScheduler（reconcileReviews）
// + VerifyTask 工具路径（runner 以 reviewer-scoped TM 模拟 harness 调用）。
// 覆盖：评审门进 IN_REVIEW / pass 结算（作者不唤醒 + digest）/ fail 返工 /
// 轮上限升级 + rework_summary 一次 / 计票矩阵 / 幂等重放 / scheduled board
// 三事件映射。

package agentteams_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/handlers"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/scheduling"
	ateamprompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/prompts"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// TeamScheduledReviewSuite 评审链集成套件（独立 DB，避免与派发套件共享板面）。
type TeamScheduledReviewSuite struct {
	isuite.AgentSuite
	suite.Suite
	db      *database.InMemoryTeamDatabase
	tm      *tools.TeamTaskManager
	backend *schedFakeBackend
	host    *schedFakeHost
	sender  *schedFakeSender
	sched   *scheduling.TeamScheduler
}

// TestTeamScheduledReviewSuite 入口。
func TestTeamScheduledReviewSuite(t *testing.T) {
	suite.Run(t, new(TeamScheduledReviewSuite))
}

// strPtr 字符串指针助手（本套件私有，与派发套件的助手互不共享）。
func (s *TeamScheduledReviewSuite) strPtr(v string) *string { return &v }

// SetupSuite 建团队行 + 全套管理器（scheduled dispatch 团队）。
func (s *TeamScheduledReviewSuite) SetupSuite() {
	ctx := context.Background()
	db := database.NewInMemoryTeamDatabase()
	s.db = db
	s.Require().True(db.Team().CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false))
	for _, member := range []string{"leader1", "agent1", "reviewer1", "inspector1"} {
		role := "teammate"
		if member == "leader1" {
			role = "leader"
		}
		s.Require().True(db.Member().CreateMember(ctx, member, "alpha", member, "{}", "ready", role, "", "", "build_mode", "", ""))
	}
	s.Require().NoError(db.Initialize(ctx))
	s.tm = tools.NewTeamTaskManager(db, "alpha", "leader1", nil, nil, s.T().TempDir(), "", "leader1")
	s.host = &schedFakeHost{}
	s.backend = &schedFakeBackend{shutdown: map[string]bool{}, verificationEnabled: true}
	s.sender = &schedFakeSender{rows: map[string][]*database.TeamMessageBase{}}
	s.sched = scheduling.NewTeamScheduler(s.host, s.tm, s.backend, s.sender, 2, 3600, "alpha", "cn")
}

// beforeReviewTest 每用例复位板面（清非终态任务）。
func (s *TeamScheduledReviewSuite) beforeReviewTest() {
	s.backend.verificationEnabled = true
	ctxReset := context.Background()
	for _, status := range []string{"pending", "blocked", "planning", "in_progress", "in_review"} {
		rows, err := s.tm.ListTasks(ctxReset, status)
		if err != nil {
			continue
		}
		for _, row := range rows {
			_, _ = s.db.Task().UpdateTaskStatus(ctxReset, row.TaskID, "in_progress")
			_, _ = s.db.Task().UpdateTaskStatus(ctxReset, row.TaskID, "completed")
		}
	}
	s.sender = &schedFakeSender{rows: map[string][]*database.TeamMessageBase{}}
	s.host = &schedFakeHost{}
	s.sched = scheduling.NewTeamScheduler(s.host, s.tm, s.backend, s.sender, 2, 3600, "alpha", "cn")
}

// createReviewTask 建一条带验收人（verifier reviewer1）的任务并交给作者完成。
// 返回任务行（应为 IN_REVIEW）。
func (s *TeamScheduledReviewSuite) createReviewTask(taskID string) *database.TeamTaskBase {
	ctx := context.Background()
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: taskID, TeamName: "alpha", Title: "评审任务 " + taskID, Content: "内容",
		Status: "in_progress", Assignee: s.strPtr("agent1"),
		Reviewer: s.strPtr(`[{"type":"verifier","reviewer_id":"reviewer1"}]`),
	}))
	// 作者完成 → 评审门（F_62）：IN_REVIEW 而非 COMPLETED。
	tm := tools.NewTeamTaskManager(s.db, "alpha", "agent1", nil, nil, s.T().TempDir(), "", "leader1")
	result, err := tm.Complete(ctx, taskID)
	s.Require().NoError(err)
	s.Require().True(result.OK, "complete must pass the review gate")
	row, getErr := s.db.Task().GetTask(ctx, taskID)
	s.Require().NoError(getErr)
	s.Require().NotNil(row)
	s.Equal("in_review", row.Status, "带验收人的任务完成时进 IN_REVIEW（评审门）")
	s.Equal(int64(1), row.ReviewRound, "review_round 随提交自增")
	return row
}

// injectVerifierVote 注入一个以 VerifyTask 工具路径投票的 runner（模拟临时
// reviewer harness 的 verify_task 调用——MockLLM 编排在工具层等效）。
func (s *TeamScheduledReviewSuite) injectVerifierVote(decision, feedback string) {
	s.sched.SetReviewerRunner(func(_ context.Context, reviewer string, task *database.TeamTaskBase) bool {
		reviewerTM := tools.NewTeamTaskManager(s.db, "alpha", reviewer, nil, nil, s.T().TempDir(), "", "leader1")
		result, err := reviewerTM.VerifyTask(context.Background(), task.TaskID, decision, feedback)
		if err != nil || !result.OK {
			return false
		}
		return true
	})
}

// TestScheduledReview_I4_pass结算 pass 投票 → 任务 COMPLETED + leader digest +
// 作者不唤醒（无 rework mail，F_119 折叠由作者侧 Claim 承载）。
func (s *TeamScheduledReviewSuite) TestScheduledReview_I4_pass结算() {
	s.beforeReviewTest()
	s.createReviewTask("i4-pass")
	s.injectVerifierVote("pass", "")

	s.sched.Activate(context.Background())
	// vote 在派发 goroutine 中落账；二次扫描拾取票并结算（真实时序：下一唤醒）。
	time.Sleep(400 * time.Millisecond)
	s.sched.Activate(context.Background())

	row, _ := s.db.Task().GetTask(context.Background(), "i4-pass")
	s.Require().NotNil(row)
	s.Equal("completed", row.Status, "pass 结算完成任务")
	// 作者不唤醒：无任何发往 agent1 的 rework/start mail。
	s.Require().Empty(s.sender.rows["agent1"], "F_119: pass 不唤醒作者")
	// leader digest 注入。
	s.Require().NotEmpty(s.host.delivered, "leader must receive the terminal digest")
}

// TestScheduledReview_I4_fail返工 fail 投票 → meta_rework 返工邮件 + 任务回
// IN_PROGRESS。
func (s *TeamScheduledReviewSuite) TestScheduledReview_I4_fail返工() {
	s.beforeReviewTest()
	s.createReviewTask("i4-fail")
	s.injectVerifierVote("fail", "请补充并发测试")

	s.sched.Activate(context.Background())
	time.Sleep(400 * time.Millisecond)
	s.sched.Activate(context.Background())

	row, _ := s.db.Task().GetTask(context.Background(), "i4-fail")
	s.Require().NotNil(row)
	s.Equal("in_progress", row.Status, "fail 结算退回返工")
	// 作者收到 rework mail。
	mail := s.sender.rows["agent1"]
	s.Require().NotEmpty(mail, "author must receive rework mail")
	meta := ateamprompts.ParseMeta(mail[0].Meta)
	s.Require().Equal("scheduler_rework", meta["template"], "F_63 rework mail")
}

// TestScheduledReview_I4_超限升级 轮上限耗尽 → 升级 leader + rework_summary 一次。
func (s *TeamScheduledReviewSuite) TestScheduledReview_I4_超限升级() {
	s.beforeReviewTest()
	s.createReviewTask("i4-ceiling")
	// 真实链推到最后一轮（max_review_rounds=2）：round1 fail → 返工 →
	// 作者再次完成（评审门 round2）→ fail → 超限。
	authorTM := tools.NewTeamTaskManager(s.db, "alpha", "agent1", nil, nil, s.T().TempDir(), "", "leader1")
	s.injectVerifierVote("fail", "第一轮缺陷")
	s.sched.Activate(context.Background())
	time.Sleep(400 * time.Millisecond)
	s.sched.Activate(context.Background())
	result, err := authorTM.Complete(context.Background(), "i4-ceiling")
	s.Require().NoError(err)
	s.Require().True(result.OK, "rework completion re-enters review gate (round 2)")
	// 投 fail → 超限分支。
	s.injectVerifierVote("fail", "仍有缺陷")

	s.sched.Activate(context.Background())
	time.Sleep(400 * time.Millisecond)
	s.sched.Activate(context.Background())

	// 任务停在 in_review（不结算）。
	row, _ := s.db.Task().GetTask(context.Background(), "i4-ceiling")
	s.Require().NotNil(row)
	s.Equal("in_review", row.Status, "round ceiling must not settle")
	// 升级注入 leader。
	s.Require().NotEmpty(s.host.delivered, "leader must receive escalation")
	// rework_summary 一次发给作者。
	s.Require().NotEmpty(s.sender.rows["agent1"], "assignee must receive rework summary")
}

// TestScheduledReview_I4_计票矩阵 多类型票计（verifier + inspector）。
func (s *TeamScheduledReviewSuite) TestScheduledReview_I4_计票矩阵() {
	s.beforeReviewTest()
	ctx := context.Background()
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "i4-tally", TeamName: "alpha", Title: "计票任务", Content: "内容",
		Status: "in_progress", Assignee: s.strPtr("agent1"),
		Reviewer: s.strPtr(`[{"type":"verifier","reviewer_id":"reviewer1"},{"type":"inspector","reviewer_id":"inspector1"}]`),
	}))
	tm := tools.NewTeamTaskManager(s.db, "alpha", "agent1", nil, nil, s.T().TempDir(), "", "leader1")
	_, _ = tm.Complete(ctx, "i4-tally")
	row, _ := s.db.Task().GetTask(ctx, "i4-tally")
	s.Require().NotNil(row)

	// verifier1 pass + inspector1 分数票。
	reviewerTM := tools.NewTeamTaskManager(s.db, "alpha", "reviewer1", nil, nil, s.T().TempDir(), "", "leader1")
	result, _ := reviewerTM.VerifyTask(ctx, "i4-tally", "pass", "")
	s.Require().True(result.OK)
	inspectorTM := tools.NewTeamTaskManager(s.db, "alpha", "inspector1", nil, nil, s.T().TempDir(), "", "leader1")
	result, _ = inspectorTM.VerifyTask(ctx, "i4-tally", "0.9", "")
	s.Require().True(result.OK, "inspector score vote must be accepted")

	// 计票快照：pass 1 + inspector avg 0.9 → settle pass。
	tally := s.tm.GetReviewTally(row)
	s.Require().Equal(1, tally["pass_count"].(int), "verifier pass counted")
	s.Require().Equal(0.9, tally["inspector_avg"].(float64), "inspector average")

	s.sched.Activate(ctx)
	row, _ = s.db.Task().GetTask(ctx, "i4-tally")
	s.Equal("completed", row.Status, "two-pool verdict pass settles")
}

// TestScheduledReview_I4_幂等重放 重复 reconcile：已投票 reviewer 不重派、
// 已结算任务不重复动作。
func (s *TeamScheduledReviewSuite) TestScheduledReview_I4_幂等重放() {
	s.beforeReviewTest()
	s.createReviewTask("i4-idem")
	s.injectVerifierVote("pass", "")

	s.sched.Activate(context.Background())
	// vote 异步落账后二次扫描结算。
	time.Sleep(400 * time.Millisecond)
	s.sched.Activate(context.Background())
	// 第三次扫描：任务已 completed，不在 in_review 列表 → 无动作。
	s.sched.Activate(context.Background())
	s.Require().Equal("completed", func() string {
		row, _ := s.db.Task().GetTask(context.Background(), "i4-idem")
		return row.Status
	}())
}

// TestScheduledReview_I4_三事件映射 scheduled board 的三评审事件映射到
// OnTaskBoardEvent（F_62 评审投票/handoff 在该模式下是纯板面可观测性）。
func (s *TeamScheduledReviewSuite) TestScheduledReview_I4_三事件映射() {
	handler := handlers.NewScheduledTaskBoardHandler(nil, nil, nil, nil)
	callbacks := handler.GetCallbacks()
	for _, eventType := range []string{
		events.TeamEventTaskReviewVote,
		events.TeamEventTaskSubmittedForReview,
		events.TeamEventTaskVerified,
		events.TeamEventTaskRevisionRequested,
	} {
		callback, mapped := callbacks[eventType]
		s.Require().True(mapped, "event must be mapped: "+eventType)
		s.Require().NotNil(callback, "callback must be registered: "+eventType)
	}
	_ = atschema.TaskStatusInReview // 保持 schema 引用（状态面属评审半区）
}
