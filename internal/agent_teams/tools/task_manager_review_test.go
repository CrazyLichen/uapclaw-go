package tools

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/locales"
)

// task_manager_review_test.go 25.2b F_62 评审面测试。
// 对齐 Python: task_manager.py L769-1080（verify_task/get_review_tally/
// settle_review/get_previous_round_vote/get_review_tasks）。
// reviewReviewerJSON 构造结构化验收人 JSON（新对象格式）。
func reviewReviewerJSON(entries ...map[string]string) string {
	if len(entries) == 0 {
		return ""
	}
	result := "["
	for i, e := range entries {
		if i > 0 {
			result += ","
		}
		result += `{"type":"` + e["type"] + `","reviewer_id":"` + e["reviewer_id"] + `"}`
	}
	return result + "]"
}

// setupReviewTask 构造一条 IN_REVIEW 任务（作者 agent1，验收人 reviewer1/inspector1）。
func setupReviewTask(t *testing.T, db *database.InMemoryTeamDatabase) *database.TeamTaskBase {
	t.Helper()
	ctx := context.Background()
	summary := "done work"
	note := ""
	task := &database.TeamTaskBase{
		TaskID:   "task-review-1",
		TeamName: "alpha",
		Title:    "Reviewable task",
		Content:  "do work",
		Status:   fsm.TaskStatusInProgress,
		Assignee: database.StringPtr("agent1"),
		Reviewer: database.StringPtr(reviewReviewerJSON(
			map[string]string{"type": "verifier", "reviewer_id": "reviewer1"},
			map[string]string{"type": "inspector", "reviewer_id": "inspector1"},
		)),
		Summary: &summary,
		Note:    &note,
	}
	ok, err := db.CreateTask(ctx, task)
	if err != nil || !ok {
		t.Fatalf("create task: ok=%v err=%v", ok, err)
	}
	submitted, err := db.SubmitForReview(ctx, "task-review-1", "", "")
	if err != nil || !submitted {
		t.Fatalf("submit for review: submitted=%v err=%v", submitted, err)
	}
	return task
}

// setupReviewTaskManager 构造 review1 视角的 TaskManager（scheduled 模式团队）。
func setupReviewTaskManager(t *testing.T) (*TeamTaskManager, *database.InMemoryTeamDatabase, *database.TeamTaskBase) {
	t.Helper()
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false)
	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "reviewer1", "alpha", "Reviewer1", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "inspector1", "alpha", "Inspector1", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	task := setupReviewTask(t, db)
	tm := NewTeamTaskManager(db, "alpha", "reviewer1", nil, nil, "", "", "leader1")
	return tm, db, task
}

// TestReviewTally_列集逐列 验票快照列齐全（二池分组 + inspector 平均分）。
// Python: get_review_tally (task_manager.py L908-993)。
func TestReviewTally_列集逐列(t *testing.T) {
	tm, db, task := setupReviewTaskManager(t)
	ctx := context.Background()
	// verifier1: pass；inspector1: 0.8 分数票。
	_ = db.InsertReviewVote(ctx, "alpha", task.TaskID, task.ReviewRound, "reviewer1", "pass", nil)
	score := "0.8"
	_ = db.InsertReviewVote(ctx, "alpha", task.TaskID, task.ReviewRound, "inspector1", score, nil)
	// 刷新任务（round 已 +1）。
	refreshed, _ := db.Task().GetTask(ctx, task.TaskID)
	tally := tm.GetReviewTally(refreshed)
	if tally["pass_count"].(int) != 1 || tally["fail_count"].(int) != 0 {
		t.Fatalf("pass/fail = %v/%v, want 1/0", tally["pass_count"], tally["fail_count"])
	}
	if tally["reviewer_count"].(int) != 2 {
		t.Fatalf("reviewer_count = %v, want 2", tally["reviewer_count"])
	}
	if tally["verdict_total"].(int) != 1 || tally["verdict_voted"].(int) != 1 {
		t.Fatalf("verdict total/voted = %v/%v", tally["verdict_total"], tally["verdict_voted"])
	}
	if tally["inspector_count"].(int) != 1 || tally["inspector_voted"].(int) != 1 {
		t.Fatalf("inspector count/voted = %v/%v", tally["inspector_count"], tally["inspector_voted"])
	}
	avg := tally["inspector_avg"].(float64)
	if avg != 0.8 {
		t.Fatalf("inspector_avg = %v, want 0.8", avg)
	}
	voted := tally["voted"].([]string)
	if len(voted) != 2 || voted[0] != "inspector1" || voted[1] != "reviewer1" {
		t.Fatalf("voted = %v", voted)
	}
}

// TestReviewTally_最新票胜出 同 reviewer 改票取最新行。
// Python: get_review_tally 的 dict 覆盖语义。
func TestReviewTally_最新票胜出(t *testing.T) {
	tm, db, task := setupReviewTaskManager(t)
	ctx := context.Background()
	_ = db.InsertReviewVote(ctx, "alpha", task.TaskID, task.ReviewRound, "reviewer1", "fail", database.StringPtr("bad"))
	_ = db.InsertReviewVote(ctx, "alpha", task.TaskID, task.ReviewRound, "reviewer1", "pass", nil)
	refreshed, _ := db.Task().GetTask(ctx, task.TaskID)
	tally := tm.GetReviewTally(refreshed)
	if tally["pass_count"].(int) != 1 || tally["fail_count"].(int) != 0 {
		t.Fatalf("latest vote must win: pass=%v fail=%v", tally["pass_count"], tally["fail_count"])
	}
	if fb := tally["fail_feedback"].(map[string]string); len(fb) != 0 {
		t.Fatalf("superseded fail feedback must not count: %v", fb)
	}
}

// TestReviewTally_非验收人票忽略 不在 reviewer 列表内的票不计。
func TestReviewTally_非验收人票忽略(t *testing.T) {
	tm, db, task := setupReviewTaskManager(t)
	ctx := context.Background()
	_ = db.InsertReviewVote(ctx, "alpha", task.TaskID, task.ReviewRound, "intruder", "fail", nil)
	refreshed, _ := db.Task().GetTask(ctx, task.TaskID)
	tally := tm.GetReviewTally(refreshed)
	if tally["fail_count"].(int) != 0 || tally["verdict_voted"].(int) != 0 {
		t.Fatalf("non-reviewer vote must be ignored: %v", tally)
	}
}

// TestSettleReview_pass结算 pass：完成 + 解除依赖。
// Python: settle_review pass → _verify_pass。
func TestSettleReview_pass结算(t *testing.T) {
	tm, db, _ := setupReviewTaskManager(t)
	ctx := context.Background()
	result, err := tm.SettleReview(ctx, "task-review-1", "pass", "")
	if err != nil || !result.OK {
		t.Fatalf("settle pass: ok=%v err=%v reason=%s", result.OK, err, result.Reason)
	}
	after, _ := db.Task().GetTask(ctx, "task-review-1")
	if after.Status != fsm.TaskStatusCompleted {
		t.Fatalf("status after settle pass = %s, want completed", after.Status)
	}
}

// TestSettleReview_fail结算 fail：退回 IN_PROGRESS。
// Python: settle_review fail → _verify_fail。
func TestSettleReview_fail结算(t *testing.T) {
	tm, db, _ := setupReviewTaskManager(t)
	ctx := context.Background()
	result, err := tm.SettleReview(ctx, "task-review-1", "fail", "please redo step 2")
	if err != nil || !result.OK {
		t.Fatalf("settle fail: ok=%v err=%v", result.OK, err)
	}
	after, _ := db.Task().GetTask(ctx, "task-review-1")
	if after.Status != fsm.TaskStatusInProgress {
		t.Fatalf("status after settle fail = %s, want in_progress", after.Status)
	}
}

// TestSettleReview_非法决策决策拒绝 非 pass/fail 拒绝。
// Python: settle_review L1008-1010。
func TestSettleReview_非法决策决策拒绝(t *testing.T) {
	tm, _, _ := setupReviewTaskManager(t)
	result, err := tm.SettleReview(context.Background(), "task-review-1", "maybe", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Fatal("invalid decision must fail")
	}
	if result.Reason != "settle_review decision must be 'pass' or 'fail', got 'maybe'" {
		t.Fatalf("reason = %s", result.Reason)
	}
}

// TestSettleReview_非评审态拒绝 非 IN_REVIEW 状态拒绝结算（CAS 语义）。
func TestSettleReview_非评审态拒绝(t *testing.T) {
	tm, db, _ := setupReviewTaskManager(t)
	ctx := context.Background()
	summary := "done"
	note := ""
	_, _ = db.CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "task-not-review", TeamName: "alpha", Title: "x", Content: "x",
		Status: fsm.TaskStatusInProgress, Assignee: database.StringPtr("agent1"), Summary: &summary, Note: &note,
	})
	result, err := tm.SettleReview(ctx, "task-not-review", "pass", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Fatal("non-IN_REVIEW settle must fail")
	}
}

// TestVerifyTask_scheduled只记票 scheduled 模式：只记票，任务保持 IN_REVIEW。
// Python: verify_task dispatched=scheduled → _record_review_vote。
func TestVerifyTask_scheduled只记票(t *testing.T) {
	tm, db, _ := setupReviewTaskManager(t)
	ctx := context.Background()
	result, err := tm.VerifyTask(ctx, "task-review-1", "pass", "looks good")
	if err != nil || !result.OK {
		t.Fatalf("verify: ok=%v err=%v reason=%s", result.OK, err, result.Reason)
	}
	after, _ := db.Task().GetTask(ctx, "task-review-1")
	if after.Status != fsm.TaskStatusInReview {
		t.Fatalf("scheduled verify must not transition: %s", after.Status)
	}
	if result.Data == nil || result.Data["pass_count"].(int) != 1 {
		t.Fatalf("tally snapshot expected: %v", result.Data)
	}
}

// TestVerifyTask_身份守卫 非验收人/作者本人拒绝。
// Python: verify_task L805-811。
func TestVerifyTask_身份守卫(t *testing.T) {
	_, db, _ := setupReviewTaskManager(t)
	ctx := context.Background()
	// 非验收人。
	other := NewTeamTaskManager(db, "alpha", "agent1", nil, nil, "", "", "leader1")
	result, _ := other.VerifyTask(ctx, "task-review-1", "pass", "")
	if result.OK {
		t.Fatal("non-reviewer must be rejected")
	}
	if result.Reason != "agent1 is not a reviewer of task task-review-1; cannot verify it" {
		t.Fatalf("reason = %s", result.Reason)
	}
}

// TestVerifyTask_inspector带分数 inspector 票必须为 0~1 分数并格式化为两位小数。
// Python: verify_task L816-826。
func TestVerifyTask_inspector带分数(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false)
	db.CreateMember(ctx, "agent1", "alpha", "A", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "inspector1", "alpha", "I", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	reviewerJSON := `[{"type":"inspector","reviewer_id":"inspector1"}]`
	summary := "s"
	note := "n"
	_, _ = db.CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "task-insp", TeamName: "alpha", Title: "x", Content: "x",
		Status: fsm.TaskStatusInProgress, Assignee: database.StringPtr("agent1"),
		Reviewer: database.StringPtr(reviewerJSON), Summary: &summary, Note: &note,
	})
	_, _ = db.SubmitForReview(ctx, "task-insp", "", "")
	tm := NewTeamTaskManager(db, "alpha", "inspector1", nil, nil, "", "", "leader1")
	// 越界分数拒绝。
	result, _ := tm.VerifyTask(ctx, "task-insp", "1.5", "")
	if result.OK {
		t.Fatal("out-of-range inspector score must fail")
	}
	// 合法分数接受。
	result, _ = tm.VerifyTask(ctx, "task-insp", "0.75", "")
	if !result.OK {
		t.Fatalf("valid inspector score rejected: %s", result.Reason)
	}
	// 非数字拒绝。
	result, _ = tm.VerifyTask(ctx, "task-insp", "abc", "")
	if result.OK {
		t.Fatal("non-numeric inspector score must fail")
	}
}

// TestGetPreviousRoundVote_上一轮票 返回上一轮该 reviewer 的最新票。
// Python: get_previous_round_vote (task_manager.py L885-906)。
func TestGetPreviousRoundVote_上一轮票(t *testing.T) {
	tm, db, _ := setupReviewTaskManager(t)
	ctx := context.Background()
	// 轮 1：reviewer1 投 fail → 结算 fail（round 回 IN_PROGRESS）。
	_ = db.InsertReviewVote(ctx, "alpha", "task-review-1", 1, "reviewer1", "fail", database.StringPtr("fix it"))
	_, _ = tm.SettleReview(ctx, "task-review-1", "fail", "fix it")
	// 轮 2：重新提交评审（round → 2）。
	_, _ = db.SubmitForReview(ctx, "task-review-1", "", "")
	task, _ := db.Task().GetTask(ctx, "task-review-1")
	if task.ReviewRound != 2 {
		t.Fatalf("review_round = %d, want 2", task.ReviewRound)
	}
	vote, err := tm.GetPreviousRoundVote(ctx, task, "reviewer1")
	if err != nil {
		t.Fatal(err)
	}
	if vote == nil || vote.Decision != "fail" || vote.Feedback == nil || *vote.Feedback != "fix it" {
		t.Fatalf("previous vote = %+v", vote)
	}
}

// TestGetPreviousRoundVote_首轮nil 首轮返回 nil。
// Python: get_previous_round_vote L899-900。
func TestGetPreviousRoundVote_首轮nil(t *testing.T) {
	tm, _, task := setupReviewTaskManager(t)
	vote, err := tm.GetPreviousRoundVote(context.Background(), task, "reviewer1")
	if err != nil {
		t.Fatal(err)
	}
	if vote != nil {
		t.Fatalf("first round vote = %+v, want nil", vote)
	}
}

// TestGetReviewTasks_过滤作者 GetReviewTasks 过滤作者本人与状态。
// Python: get_review_tasks (task_manager.py L1073-1080)。
func TestGetReviewTasks_过滤作者(t *testing.T) {
	tm, db, _ := setupReviewTaskManager(t)
	ctx := context.Background()
	tasks, err := tm.GetReviewTasks(ctx, "reviewer1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].TaskID != "task-review-1" {
		t.Fatalf("review tasks = %v", tasks)
	}
	// 作者本人查询自己的任务 → 空（作者不审自己）。
	author := NewTeamTaskManager(db, "alpha", "agent1", nil, nil, "", "", "leader1")
	own, err := author.GetReviewTasks(ctx, "agent1")
	if err != nil {
		t.Fatal(err)
	}
	if len(own) != 0 {
		t.Fatalf("author must not review own task: %v", own)
	}
}

// ──────────────────────────── 工具面（25.2b） ────────────────────────────

// TestViewTaskTool_inReviewAction in_review 动作返回本成员待验收任务。
// Python: ViewTaskToolV2 in_review (tool_task.py L1437-1440)。
func TestViewTaskTool_inReviewAction(t *testing.T) {
	_, db, _ := setupReviewTaskManager(t)
	ctx := context.Background()
	tool := newViewTaskTool(NewTeamTaskManager(db, "alpha", "reviewer1", nil, nil, "", "", "leader1"), locales.MakeTranslator("cn"))

	out, err := tool.Invoke(ctx, map[string]any{"action": "in_review"})
	if err != nil {
		t.Fatal(err)
	}
	tasks := out["data"].(map[string]any)["tasks"].([]map[string]any)
	if len(tasks) != 1 || tasks[0]["task_id"] != "task-review-1" {
		t.Fatalf("in_review = %v", tasks)
	}
	// 作者视角 → 空（不审自己）。
	authorTool := newViewTaskTool(NewTeamTaskManager(db, "alpha", "agent1", nil, nil, "", "", "leader1"), locales.MakeTranslator("cn"))
	out, err = authorTool.Invoke(ctx, map[string]any{"action": "in_review"})
	if err != nil {
		t.Fatal(err)
	}
	tasks = out["data"].(map[string]any)["tasks"].([]map[string]any)
	if len(tasks) != 0 {
		t.Fatalf("author in_review = %v, want empty", tasks)
	}
}

// TestViewTaskTool_noteAction note 动作返回不截断的 summary/note 全文（F_119）。
// Python: ViewTaskToolV2 note (tool_task.py L1417-1435)。
func TestViewTaskTool_noteAction(t *testing.T) {
	_, db, _ := setupReviewTaskManager(t)
	ctx := context.Background()
	tool := newViewTaskTool(NewTeamTaskManager(db, "alpha", "reviewer1", nil, nil, "", "", "leader1"), locales.MakeTranslator("cn"))

	out, err := tool.Invoke(ctx, map[string]any{"action": "note", "task_id": "task-review-1"})
	if err != nil {
		t.Fatal(err)
	}
	data := out["data"].(map[string]any)
	if data["view"] != "note" || data["task_id"] != "task-review-1" {
		t.Fatalf("note = %v", out)
	}
	if data["summary"] != "done work" {
		t.Fatalf("summary = %v", data["summary"])
	}
	// 缺 task_id 报错（业务失败 map，非 Go error）。
	out, err = tool.Invoke(ctx, map[string]any{"action": "note"})
	if err != nil || out["success"] != false {
		t.Fatal("note without task_id must fail")
	}
}

// TestVerifyTaskTool_invoke scheduled 模式投票 → 保持 IN_REVIEW + tally 快照。
// Python: VerifyTaskTool.invoke (tool_task.py L2262-2287)。
func TestVerifyTaskTool_invoke(t *testing.T) {
	tm, db, _ := setupReviewTaskManager(t)
	ctx := context.Background()
	tool := newVerifyTaskTool(tm, locales.MakeTranslator("cn"), "verify_task_scheduled")

	out, err := tool.Invoke(ctx, map[string]any{"task_id": "task-review-1", "decision": "pass", "feedback": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	data := out["data"].(map[string]any)
	if data["task_id"] != "task-review-1" || data["decision"] != "pass" {
		t.Fatalf("out = %v", out)
	}
	tally, hasTally := data["tally"].(map[string]any)
	if !hasTally || tally["pass_count"].(int) != 1 {
		t.Fatalf("tally = %v", data["tally"])
	}
	after, _ := db.Task().GetTask(ctx, "task-review-1")
	if after.Status != fsm.TaskStatusInReview {
		t.Fatalf("status must stay IN_REVIEW: %s", after.Status)
	}
	// 缺 task_id 报错（业务失败 map，非 Go error）。
	out, err = tool.Invoke(ctx, map[string]any{"decision": "pass"})
	if err != nil || out["success"] != false {
		t.Fatal("missing task_id must fail")
	}
}

// TestVerifyTaskTool_autonomous直接结算 autonomous 模式：pass 直接完成。
// Python: verify_task autonomous 分支。
func TestVerifyTaskTool_autonomous直接结算(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "autonomous", false)
	db.CreateMember(ctx, "agent1", "alpha", "A", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "reviewer1", "alpha", "R", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	summary := "s"
	note := "n"
	_, _ = db.CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "task-auto", TeamName: "alpha", Title: "x", Content: "x",
		Status: fsm.TaskStatusInProgress, Assignee: database.StringPtr("agent1"),
		Reviewer: database.StringPtr(`[{"type":"verifier","reviewer_id":"reviewer1"}]`), Summary: &summary, Note: &note,
	})
	_, _ = db.SubmitForReview(ctx, "task-auto", "", "")
	tm := NewTeamTaskManager(db, "alpha", "reviewer1", nil, nil, "", "", "leader1")
	tool := newVerifyTaskTool(tm, locales.MakeTranslator("cn"), "verify_task")

	out, err := tool.Invoke(ctx, map[string]any{"task_id": "task-auto", "decision": "pass"})
	if err != nil {
		t.Fatal(err)
	}
	if _, hasTally := out["data"].(map[string]any)["tally"]; hasTally {
		t.Fatal("autonomous mode must not carry tally")
	}
	after, _ := db.Task().GetTask(ctx, "task-auto")
	if after.Status != fsm.TaskStatusCompleted {
		t.Fatalf("autonomous pass must complete: %s", after.Status)
	}
}

// TestComplete_带验收人进评审门 带验收人的任务完成时进 IN_REVIEW 而非 COMPLETED。
// Python: complete_task 带 reviewers 走 submit_for_review 边（events.py L403-411）。
func TestComplete_带验收人进评审门(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false)
	db.CreateMember(ctx, "agent1", "alpha", "A", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "reviewer1", "alpha", "R", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	summary := "s"
	note := "n"
	_, _ = db.CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "task-gate", TeamName: "alpha", Title: "x", Content: "x",
		Status: fsm.TaskStatusInProgress, Assignee: database.StringPtr("agent1"),
		Reviewer: database.StringPtr(`[{"type":"verifier","reviewer_id":"reviewer1"}]`), Summary: &summary, Note: &note,
	})
	tm := NewTeamTaskManager(db, "alpha", "agent1", nil, nil, "", "", "leader1")

	result, err := tm.Complete(ctx, "task-gate")
	if err != nil || !result.OK {
		t.Fatalf("complete: ok=%v err=%v reason=%s", result.OK, err, result.Reason)
	}
	after, _ := db.Task().GetTask(ctx, "task-gate")
	if after.Status != fsm.TaskStatusInReview {
		t.Fatalf("status = %s, want in_review", after.Status)
	}
	if after.ReviewRound != 1 {
		t.Fatalf("review_round = %d, want 1", after.ReviewRound)
	}
}

// TestComplete_无验收人直接完成 无验收人任务完成时直接 COMPLETED（原路径不变）。
func TestComplete_无验收人直接完成(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "autonomous", false)
	db.CreateMember(ctx, "agent1", "alpha", "A", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	summary := "s"
	note := "n"
	_, _ = db.CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "task-plain", TeamName: "alpha", Title: "x", Content: "x",
		Status: fsm.TaskStatusInProgress, Assignee: database.StringPtr("agent1"),
		Summary: &summary, Note: &note,
	})
	tm := NewTeamTaskManager(db, "alpha", "agent1", nil, nil, "", "", "leader1")

	result, err := tm.Complete(ctx, "task-plain")
	if err != nil || !result.OK {
		t.Fatalf("complete: ok=%v err=%v", result.OK, err)
	}
	after, _ := db.Task().GetTask(ctx, "task-plain")
	if after.Status != fsm.TaskStatusCompleted {
		t.Fatalf("status = %s, want completed", after.Status)
	}
}
