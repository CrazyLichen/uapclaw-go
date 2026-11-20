//go:build integration

// 25.2b 收尾：任务通知三机制集成用例（对齐 team_context.py/scheduler.py）。
// 覆盖：①调度器修订推送 reconcileRevisions（F_129 scheduled 侧，执行中 →
// meta_task_revised / 阻塞挂恢复 → meta_subtask_review / 幂等）；
// ②TaskOutcomeTracker claim 折叠端到端（评审通过 → `<team-event>` 文本 →
// persist 去重 → WAKES=false 不推送）；③TaskRevisionTracker announce 正向
// 推送（编辑 → task-revised 文本 → WAKES=true 独立投递 → 二次 no-op）。
// 真实跨子系统：agent_teams/tracker + tools（TeamBackend/TeamTaskManager）+
// scheduling（TeamScheduler）+ database。
package agentteams_test

import (
	"context"
	"time"

	ateamprompts "github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/scheduling"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"

	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// TaskNoticesSuite 任务通知机制集成套件。
type TaskNoticesSuite struct {
	isuite.AgentSuite
	db *database.InMemoryTeamDatabase
}

// TestTaskNoticesSuite 入口。
func TestTaskNoticesSuite(t *testing.T) {
	suite.Run(t, new(TaskNoticesSuite))
}

// SetupSuite 建团队行 + 成员（bob 执行者 / reviewer1 验收人 / leader1）。
func (s *TaskNoticesSuite) SetupSuite() {
	ctx := context.Background()
	db := database.NewInMemoryTeamDatabase()
	s.db = db
	s.Require().True(db.Team().CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false))
	for _, member := range []string{"leader1", "bob", "reviewer1"} {
		role := "teammate"
		if member == "leader1" {
			role = "leader"
		}
		s.Require().True(db.Member().CreateMember(ctx, member, "alpha", member, "{}", "ready", role, "", "", "build_mode", "", ""))
	}
	s.Require().NoError(db.Initialize(ctx))
}

// strPtr 字符串指针助手（本套件私有）。
func (s *TaskNoticesSuite) strPtr(v string) *string { return &v }

// newBackend 按模式与成员建 TeamBackend。
func (s *TaskNoticesSuite) newBackend(member, dispatchMode string) *tools.TeamBackend {
	return tools.NewTeamBackend("alpha", member, member == "leader1", s.db, nil,
		tools.WithLeaderMemberName("leader1"),
		tools.WithDispatchMode(dispatchMode),
	)
}

// createTaskWithReviewer 建一条带验收人的执行中任务。
func (s *TaskNoticesSuite) createTaskWithReviewer(taskID string) {
	ctx := context.Background()
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: taskID, TeamName: "alpha", Title: "通知任务 " + taskID, Content: "原始内容",
		Status: "in_progress", Assignee: s.strPtr("bob"),
		Reviewer: s.strPtr(`[{"type":"verifier","reviewer_id":"reviewer1"}]`),
	}))
}

// TestTaskNotices_I8_调度器修订推送 执行中任务被编辑 → 下一轮扫描推
// meta_task_revised；二次扫描幂等。
func (s *TaskNoticesSuite) TestTaskNotices_I8_调度器修订推送() {
	ctx := context.Background()
	leaderTM := tools.NewTeamTaskManager(s.db, "alpha", "leader1", nil, nil, "", "", "leader1")
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "i8a", TeamName: "alpha", Title: "甲", Content: "v1",
		Status: "in_progress", Assignee: s.strPtr("bob"), Revision: 1,
	}))
	sender := &schedFakeSender{rows: map[string][]*database.TeamMessageBase{}}
	sched := scheduling.NewTeamScheduler(&schedFakeHost{}, tools.NewTeamTaskManager(s.db, "alpha", "leader1", nil, nil, "", "", "leader1"), &schedFakeBackend{shutdown: map[string]bool{}}, sender, 3, 3600, "alpha", "cn")
	// 第一轮扫描：B_06 兜底给 bob 补 start handoff（渲染当前 revision 并记录已读）。
	sched.Activate(context.Background())

	// 编辑：revision 推进 + change_notes（F_129 信号源）。
	s.Require().NoError(leaderTM.UpdateTask(ctx, "i8a", "甲", "v2"))
	row, _ := s.db.Task().GetTask(ctx, "i8a")
	s.Require().NotNil(row)
	s.Require().Equal(int64(2), row.Revision, "编辑必须推进 revision")

	// 第二轮扫描：已读基线落后于新 revision → 修订邮件。
	sched.Activate(context.Background())
	tpl := s.mailTemplateOf(sender.rows["bob"], "scheduler_task_revised")
	s.Require().Equal("scheduler_task_revised", tpl, "执行中任务编辑 → scheduler_task_revised")

	// 幂等：二次扫描不重发。
	before := len(sender.rows["bob"])
	sched.Activate(context.Background())
	s.Require().Len(sender.rows["bob"], before, "同一 revision 只推一次")
}

// TestTaskNotices_I8b_阻塞挂恢复评审 blocked 且 resume_pending →
// meta_subtask_review（父任务挂起等子任务，按新内容核对而非续跑）。
func (s *TaskNoticesSuite) TestTaskNotices_I8b_阻塞挂恢复评审() {
	ctx := context.Background()
	leaderTM := tools.NewTeamTaskManager(s.db, "alpha", "leader1", nil, nil, "", "", "leader1")
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "i8b", TeamName: "alpha", Title: "父", Content: "v1",
		Status: "blocked", Assignee: s.strPtr("bob"), Revision: 1, ResumePending: true,
	}))
	s.Require().NoError(leaderTM.UpdateTask(ctx, "i8b", "父", "v2"))

	sender := &schedFakeSender{rows: map[string][]*database.TeamMessageBase{}}
	sched := scheduling.NewTeamScheduler(&schedFakeHost{}, tools.NewTeamTaskManager(s.db, "alpha", "leader1", nil, nil, "", "", "leader1"), &schedFakeBackend{shutdown: map[string]bool{}}, sender, 3, 3600, "alpha", "cn")
	sched.Activate(context.Background())

	tpl := s.mailTemplateOf(sender.rows["bob"], "scheduler_subtask_review")
	s.Require().Equal("scheduler_subtask_review", tpl, "挂起父任务编辑 → scheduler_subtask_review")
}

// TestTaskNotices_I9_评审结果折叠 评审通过 → TaskOutcomeTracker claim 渲染
// `<team-event>` 文本 → persist 去重 → WAKES=false 不推送。
func (s *TaskNoticesSuite) TestTaskNotices_I9_评审结果折叠() {
	ctx := context.Background()
	s.createTaskWithReviewer("i9")
	// 作者完成 → 评审门；验收人 pass。
	authorTM := tools.NewTeamTaskManager(s.db, "alpha", "bob", nil, nil, "", "", "leader1")
	result, err := authorTM.Complete(ctx, "i9")
	s.Require().NoError(err)
	s.Require().True(result.OK)
	reviewerTM := tools.NewTeamTaskManager(s.db, "alpha", "reviewer1", nil, nil, "", "", "leader1")
	vote, voteErr := reviewerTM.VerifyTask(ctx, "i9", "pass", "")
	s.Require().NoError(voteErr)
	s.Require().True(vote.OK)

	// scheduled 模式：结算在调度器——扫票 → settle pass → 任务 completed。
	settleBackend := &schedFakeBackend{shutdown: map[string]bool{}, verificationEnabled: true}
	sender := &schedFakeSender{rows: map[string][]*database.TeamMessageBase{}}
	sched := scheduling.NewTeamScheduler(&schedFakeHost{}, tools.NewTeamTaskManager(s.db, "alpha", "leader1", nil, nil, "", "", "leader1"), settleBackend, sender, 3, 3600, "alpha", "cn")
	sched.Activate(context.Background())
	time.Sleep(400 * time.Millisecond)
	sched.Activate(context.Background())
	row, _ := s.db.Task().GetTask(ctx, "i9")
	s.Require().NotNil(row)
	s.Require().Equal("completed", row.Status, "pass 结算完成任务")

	// scheduled 模式构造评审结果通知器。
	tr := tracker.BuildTaskEventTracker("scheduled", s.newBackend("bob", "scheduled"), "bob")
	s.Require().NotNil(tr)
	s.Require().False(tr.Wakes(), "评审结果通知器不得唤醒作者（F_119）")

	bobSession := s.NewTestSession("sess-bob-i9")
	notice := tr.Claim(bobSession)
	s.Require().NotNil(notice, "评审通过后必须有待告知条目")
	s.Require().Contains(notice.Text, "i9", "通知文本须点名任务")

	// persist 后不再重复。
	tr.Persist(bobSession)
	s.Require().Nil(tr.Claim(bobSession), "已告知条目不得二次通知")

	// WAKES=false：announce 永不发送。
	sent := false
	delivered, announceErr := tr.Announce(bobSession, func(string) error { sent = true; return nil })
	s.Require().NoError(announceErr)
	s.Require().False(delivered)
	s.Require().False(sent, "F_119: 评审结果不得作为独立输入推送")
}

// TestTaskNotices_I10_编辑通知推送 autonomous 模式：编辑 → TaskRevisionTracker
// claim 渲染 → WAKES=true announce 独立投递 → 二次 no-op。
func (s *TaskNoticesSuite) TestTaskNotices_I10_编辑通知推送() {
	ctx := context.Background()
	leaderTM := tools.NewTeamTaskManager(s.db, "alpha", "leader1", nil, nil, "", "", "leader1")
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "i10", TeamName: "alpha", Title: "乙", Content: "v1",
		Status: "in_progress", Assignee: s.strPtr("bob"), Revision: 1,
	}))

	tr := tracker.BuildTaskEventTracker("autonomous", s.newBackend("bob", "autonomous"), "bob")
	s.Require().NotNil(tr)
	s.Require().True(tr.Wakes(), "编辑通知器必须可唤醒（F_129）")

	bobSession := s.NewTestSession("sess-bob-i10")
	// 首次 claim：建任务即 revision 1（bob 未看过）→ 已有通知；persist 清基线。
	first := tr.Claim(bobSession)
	s.Require().NotNil(first, "未看过的任务行即未读编辑信号")
	tr.Persist(bobSession)

	// 编辑 → announce（内部 claim+send+persist 一体）：独立投递（WAKES=true）。
	s.Require().NoError(leaderTM.UpdateTask(ctx, "i10", "乙", "v2"))
	deliveredText := ""
	sent, announceErr := tr.Announce(bobSession, func(text string) error { deliveredText = text; return nil })
	s.Require().NoError(announceErr)
	s.Require().True(sent, "WAKES=true 必须独立投递")
	s.Require().Contains(deliveredText, "i10", "投递文本须点名任务")
	s.Require().Contains(deliveredText, "task-revised", "投递文本为 task-revised 事件块")

	// 已投递即记账：二次 claim/announce 均 no-op。
	s.Require().Nil(tr.Claim(bobSession), "已通知的编辑不得重复")
	sent2, err2 := tr.Announce(bobSession, func(string) error { return nil })
	s.Require().NoError(err2)
	s.Require().False(sent2, "二次 announce 必须 no-op")
}

// ──────────────────────────── 助手 ────────────────────────────

// mailTemplateOf 返回信箱中首个匹配模板的行（缺失返回空串）。
func (s *TaskNoticesSuite) mailTemplateOf(rows []*database.TeamMessageBase, template string) string {
	for _, row := range rows {
		if row.Meta == nil {
			continue
		}
		meta := ateamprompts.ParseMeta(row.Meta)
		if tpl, _ := meta["template"].(string); tpl == template {
			return tpl
		}
	}
	return ""
}

// mailCountOf 统计信箱中匹配模板的行数。
func (s *TaskNoticesSuite) mailCountOf(rows []*database.TeamMessageBase, template string) int {
	count := 0
	for _, row := range rows {
		if row.Meta == nil {
			continue
		}
		meta := ateamprompts.ParseMeta(row.Meta)
		if tpl, _ := meta["template"].(string); tpl == template {
			count++
		}
	}
	return count
}
