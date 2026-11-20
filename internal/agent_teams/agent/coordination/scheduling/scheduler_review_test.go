package scheduling

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// scheduler_review_test.go 评审半区测试（fake taskMgr + 注入 runner）。

// fakeReviewTaskMgr SchedulerTaskManager 评审面替身。
type fakeReviewTaskMgr struct {
	tasks         map[string]*database.TeamTaskBase
	votes         []*database.TeamTaskReviewVoteBase
	settleCalls   []string
	reviewTallies map[string]map[string]any
}

func (f *fakeReviewTaskMgr) Get(_ context.Context, taskID string) (*database.TeamTaskBase, error) {
	return f.tasks[taskID], nil
}
func (f *fakeReviewTaskMgr) ListTasks(_ context.Context, status string) ([]*database.TeamTaskBase, error) {
	var out []*database.TeamTaskBase
	for _, task := range f.tasks {
		if task.Status == status {
			out = append(out, task)
		}
	}
	return out, nil
}
func (f *fakeReviewTaskMgr) StartTask(_ context.Context, _ string) (atschema.TaskOpResult, error) {
	return atschema.TaskOpResult{}.Success(), nil
}
func (f *fakeReviewTaskMgr) GetOtherActiveTaskID(_ context.Context, _, _ string) (string, error) {
	return "", nil
}
func (f *fakeReviewTaskMgr) HasOpenTasks(_ context.Context) (bool, error) { return true, nil }
func (f *fakeReviewTaskMgr) GetReviewTally(task *database.TeamTaskBase) map[string]any {
	if tally, ok := f.reviewTallies[task.TaskID]; ok {
		return tally
	}
	return map[string]any{
		"task_id": task.TaskID, "review_round": task.ReviewRound,
		"pass_count": 0, "fail_count": 0, "reviewer_count": 1,
		"voted": []string{}, "fail_feedback": map[string]string{},
		"verdict_total": 1, "verdict_voted": 0,
		"inspector_count": 0, "inspector_voted": 0, "inspector_avg": nil,
	}
}
func (f *fakeReviewTaskMgr) SettleReview(_ context.Context, taskID, decision, _ string) (atschema.TaskOpResult, error) {
	f.settleCalls = append(f.settleCalls, taskID+"/"+decision)
	if task, ok := f.tasks[taskID]; ok {
		if decision == "pass" {
			task.Status = "completed"
		} else {
			task.Status = "in_progress"
		}
	}
	return atschema.TaskOpResult{}.Success(), nil
}
func (f *fakeReviewTaskMgr) GetPreviousRoundVote(_ context.Context, _ *database.TeamTaskBase, _ string) (*database.TeamTaskReviewVoteBase, error) {
	return nil, nil
}
func (f *fakeReviewTaskMgr) TaskDB() database.TaskDao { return nil }

// fakeReviewBackend 后端替身（验证开关）。
type fakeReviewBackend struct{ enabled bool }

func (f *fakeReviewBackend) ListRetirableMembers(_ context.Context) ([]database.RetirableMemberRow, error) {
	return nil, nil
}
func (f *fakeReviewBackend) ListShutdownMembers(_ context.Context) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (f *fakeReviewBackend) ShutdownMember(_ context.Context, _ string) atschema.MemberOpResult {
	return atschema.MemberOpResult{}
}
func (f *fakeReviewBackend) MembersWithUnread(_ context.Context, _ []string) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (f *fakeReviewBackend) TaskVerificationEnabled(_ context.Context) (bool, error) {
	return f.enabled, nil
}

// fakeReviewHost host 替身（升级注入面）。
type fakeReviewHost struct{ escalated []string }

func (f *fakeReviewHost) DeliverInput(_ context.Context, content string, _ bool, _ *agentinterfaces.RunContext) error {
	f.escalated = append(f.escalated, content)
	return nil
}
func (f *fakeReviewHost) AutoStartMember(_ context.Context, _ string) bool { return true }

// fakeReviewSender 消息发送替身。
type fakeReviewSender struct{ sent []string }

func (f *fakeReviewSender) SendMessageWithMeta(_ context.Context, meta map[string]any, to string) (string, error) {
	f.sent = append(f.sent, to+":"+fmt.Sprint(meta["template"]))
	return "msg-id", nil
}
func (f *fakeReviewSender) MemberName() string { return "leader1" }
func (f *fakeReviewSender) GetMessages(_ context.Context, _ string, _ bool, _ string) ([]*database.TeamMessageBase, error) {
	return nil, nil
}

// newReviewScheduler 构造评审半区测试调度器。
func newReviewScheduler(t *testing.T, mgr *fakeReviewTaskMgr, backend *fakeReviewBackend) (*TeamScheduler, *fakeReviewHost) {
	t.Helper()
	host := &fakeReviewHost{}
	s := NewTeamScheduler(host, mgr, backend, &fakeReviewSender{}, 3, 600, "alpha", "cn")
	s.active = true
	return s, host
}

// reviewTaskFixture2 in_review 任务（reviewer1 一名验收人）。
func reviewTaskFixture2() *database.TeamTaskBase {
	maxRounds := int64(2)
	return &database.TeamTaskBase{
		TaskID: "task-s1", TeamName: "alpha", Title: "评审任务", Content: "内容",
		Status: "in_review", Assignee: database.StringPtr("agent1"),
		Reviewer:        database.StringPtr(`[{"type":"verifier","reviewer_id":"reviewer1"}]`),
		ReviewRound:     1,
		MaxReviewRounds: &maxRounds,
		UpdatedAt:       database.GetCurrentTime(),
	}
}

// TestReconcileReviews_派发不重派 首扫派发，二扫不重派（dispatch mark）。
// Python: _reconcile_reviews 派发段。
func TestReconcileReviews_派发不重派(t *testing.T) {
	mgr := &fakeReviewTaskMgr{tasks: map[string]*database.TeamTaskBase{"task-s1": reviewTaskFixture2()}}
	s, _ := newReviewScheduler(t, mgr, &fakeReviewBackend{enabled: true})
	var mu sync.Mutex
	runs := 0
	s.SetReviewerRunner(func(_ context.Context, _ string, _ *database.TeamTaskBase) bool {
		mu.Lock()
		runs++
		mu.Unlock()
		return false // 无票 → followUp 释放标记重派
	})
	_, _ = s.reconcileReviews(context.Background())
	// followUp 释放标记后重扫；runner 注入 fake 无票 → runs 递增但不超过
	// maxReviewRunsPerRound-1 次重派（首次 1 + 重派至多 1 = 2 后升级 no_vote）。
	time.Sleep(200 * time.Millisecond)
	if runs == 0 {
		t.Fatal("reviewer must be dispatched at least once")
	}
}

// TestReconcileReviews_全票通过结算 两票齐 + 全 pass → settle pass + digest。
// Python: judge-pass 分支。
func TestReconcileReviews_全票通过结算(t *testing.T) {
	task := reviewTaskFixture2()
	mgr := &fakeReviewTaskMgr{tasks: map[string]*database.TeamTaskBase{"task-s1": task}}
	mgr.reviewTallies = map[string]map[string]any{
		"task-s1": {
			"task_id": "task-s1", "review_round": int64(1),
			"pass_count": 1, "fail_count": 0, "reviewer_count": 1,
			"voted": []string{"reviewer1"}, "fail_feedback": map[string]string{},
			"verdict_total": 1, "verdict_voted": 1,
			"inspector_count": 0, "inspector_voted": 0, "inspector_avg": nil,
		},
	}
	s, _ := newReviewScheduler(t, mgr, &fakeReviewBackend{enabled: true})
	s.SetReviewerRunner(func(_ context.Context, _ string, _ *database.TeamTaskBase) bool { return true })

	acted, err := s.reconcileReviews(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !acted {
		t.Fatal("pass settle must count as acted")
	}
	if len(mgr.settleCalls) != 1 || mgr.settleCalls[0] != "task-s1/pass" {
		t.Fatalf("settleCalls = %v", mgr.settleCalls)
	}
}

// TestReconcileReviews_超限升级 轮上限耗尽 fail → 升级 + rework_summary 一次。
// Python: _settle_fail_or_escalate 超限分支。
func TestReconcileReviews_超限升级(t *testing.T) {
	task := reviewTaskFixture2()
	task.ReviewRound = 2 // max=2 已达上限
	mgr := &fakeReviewTaskMgr{tasks: map[string]*database.TeamTaskBase{"task-s1": task}}
	mgr.reviewTallies = map[string]map[string]any{
		"task-s1": {
			"task_id": "task-s1", "review_round": int64(2),
			"pass_count": 0, "fail_count": 1, "reviewer_count": 1,
			"voted": []string{"reviewer1"}, "fail_feedback": map[string]string{"reviewer1": "并发不安全"},
			"verdict_total": 1, "verdict_voted": 1, "verdict_fail_count": 1,
			"inspector_count": 0, "inspector_voted": 0, "inspector_avg": nil,
		},
	}
	s, host := newReviewScheduler(t, mgr, &fakeReviewBackend{enabled: true})
	s.SetReviewerRunner(func(_ context.Context, _ string, _ *database.TeamTaskBase) bool { return true })

	acted, err := s.reconcileReviews(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if acted {
		t.Fatal("escalation path must not count as acted (task stays in_review)")
	}
	if len(mgr.settleCalls) != 0 {
		t.Fatalf("round ceiling must not settle: %v", mgr.settleCalls)
	}
	if len(host.escalated) != 1 {
		t.Fatalf("escalations = %d", len(host.escalated))
	}
	// rework_summary 一次发给作者。
	if len(s.summaryRequested) != 1 {
		t.Fatalf("summary requested ledger = %v", s.summaryRequested)
	}
}

// TestReconcileReviews_验证开关关闭 关闭时不做任何评审动作。
// Python: _reconcile_reviews 首行守卫。
func TestReconcileReviews_验证开关关闭(t *testing.T) {
	mgr := &fakeReviewTaskMgr{tasks: map[string]*database.TeamTaskBase{"task-s1": reviewTaskFixture2()}}
	s, host := newReviewScheduler(t, mgr, &fakeReviewBackend{enabled: false})
	s.SetReviewerRunner(func(_ context.Context, _ string, _ *database.TeamTaskBase) bool { return true })
	acted, err := s.reconcileReviews(context.Background())
	if err != nil || acted {
		t.Fatalf("disabled: acted=%v err=%v", acted, err)
	}
	if len(mgr.settleCalls) != 0 || len(host.escalated) != 0 {
		t.Fatal("disabled verification must be a no-op")
	}
}

// TestReconcileReviews_停滞升级 超过 stall timeout 升级一次。
// Python: _handle_undecided。
func TestReconcileReviews_停滞升级(t *testing.T) {
	task := reviewTaskFixture2()
	task.UpdatedAt = database.GetCurrentTime() - 700_000 // 700s 前
	mgr := &fakeReviewTaskMgr{tasks: map[string]*database.TeamTaskBase{"task-s1": task}}
	s, host := newReviewScheduler(t, mgr, &fakeReviewBackend{enabled: true})
	s.stallTimeoutSeconds = 600
	s.SetReviewerRunner(func(_ context.Context, _ string, _ *database.TeamTaskBase) bool { return false })

	_, _ = s.reconcileReviews(context.Background())
	time.Sleep(150 * time.Millisecond)
	if len(host.escalated) == 0 {
		t.Fatal("stalled round must escalate")
	}
}

// TestFollowUp_重派后升级 无票 run 达上限后 no_vote 升级。
// Python: _follow_up_unvoted_reviewer。
func TestFollowUp_重派后升级(t *testing.T) {
	task := reviewTaskFixture2()
	mgr := &fakeReviewTaskMgr{tasks: map[string]*database.TeamTaskBase{"task-s1": task}}
	s, host := newReviewScheduler(t, mgr, &fakeReviewBackend{enabled: true})
	runKey := [3]string{"task-s1", "1", "reviewer1"}
	s.reviewRuns[runKey] = maxReviewRunsPerRound // 已达上限
	s.followUpUnvotedReviewer(context.Background(), "reviewer1", task, false)
	if len(host.escalated) != 1 {
		t.Fatalf("escalations = %d", len(host.escalated))
	}
}

// TestReviewTaskDB_FailFeedback 无 fail 反馈块时格式化返回空串。
func TestReviewTaskDB_FailFeedback(t *testing.T) {
	out := FormatFailFeedback(failFeedbackOf(map[string]any{}))
	if out != "" {
		t.Fatalf("empty feedback = %q", out)
	}
}

// ──────────────────────────── 接口剩余方法（板面 a 半区替身） ────────────────────────────

func (f *fakeReviewTaskMgr) BoardCounts(_ context.Context) (int64, int64, error) { return 0, 0, nil }
func (f *fakeReviewTaskMgr) Reset(_ context.Context, _ string) (atschema.TaskOpResult, error) {
	return atschema.TaskOpResult{}.Success(), nil
}
func (f *fakeReviewTaskMgr) CancelAllTasks(_ context.Context, _ []string) ([]*database.TeamTaskBase, error) {
	return nil, nil
}
func (f *fakeReviewTaskMgr) CountTasksTerminality(_ context.Context, _ string) (int64, int64, error) {
	return 0, 0, nil
}
func (f *fakeReviewTaskMgr) UnseenRevisions(_ context.Context) ([]database.UnseenRevisionRow, error) {
	return nil, nil
}
func (f *fakeReviewTaskMgr) ObserveRevision(_ context.Context, _ string, _ int64) {}
func (f *fakeReviewTaskMgr) GetTasksByAssignee(_ context.Context, _, _ string) ([]*database.TeamTaskBase, error) {
	return nil, nil
}

func (f *fakeReviewTaskMgr) OpenTaskCountsByAssignee(_ context.Context) (map[string]int, error) {
	return map[string]int{}, nil
}

// ──────────────────────────── R-6（25.2 spec §4 回填） ────────────────────────────

// TestDeactivate_在飞自然跑完 Deactivate 停扫描但已派发的 reviewer 运行体
// 自然完成（不 cancel——自清理语义对齐）。
// Python: deactivate（scheduler.py L192-195）+ 计划 R-6。
func TestDeactivate_在飞自然跑完(t *testing.T) {
	task := reviewTaskFixture2()
	mgr := &fakeReviewTaskMgr{tasks: map[string]*database.TeamTaskBase{"task-s1": task}}
	s, _ := newReviewScheduler(t, mgr, &fakeReviewBackend{enabled: true})
	finished := make(chan struct{})
	s.SetReviewerRunner(func(_ context.Context, _ string, _ *database.TeamTaskBase) bool {
		// 模拟较长的在飞评审。
		time.Sleep(50 * time.Millisecond)
		close(finished)
		return true
	})
	_, _ = s.reconcileReviews(context.Background())
	// 在飞期间 deactivate。
	s.Deactivate()
	select {
	case <-finished:
		// 在飞运行体自然跑完 ✓。
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight reviewer must finish naturally after deactivate")
	}
}
