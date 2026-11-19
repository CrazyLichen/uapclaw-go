package scheduling

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 25.2b b 半区 ────────────────────────────

// reviewTaskFixture b 半区测试任务。
func reviewTaskFixture() *database.TeamTaskBase {
	maxRounds := int64(3)
	summary := "完成任务：实现导出功能"
	note := "关键决策：使用流式导出"
	return &database.TeamTaskBase{
		TaskID: "task-r1", TeamName: "alpha", Title: "实现导出功能", Content: "实现",
		Status: "in_review", Assignee: database.StringPtr("agent1"),
		Reviewer:        database.StringPtr(`[{"type":"verifier","reviewer_id":"reviewer1","instruction":"重点检查并发安全"}]`),
		ReviewRound:     2,
		MaxReviewRounds: &maxRounds,
		Summary:         &summary,
		Note:            &note,
	}
}

// TestRenderLeaderEscalationRounds 轮上限升级文本。
// Python: render_leader_escalation_rounds。
func TestRenderLeaderEscalationRounds(t *testing.T) {
	task := reviewTaskFixture()
	out := RenderLeaderEscalationRounds(task, "- reviewer1: 并发不安全")
	if !strings.Contains(out, "task-r1") || !strings.Contains(out, "2") {
		t.Fatalf("escalation rounds = %s", out)
	}
	if !strings.Contains(out, "reviewer1: 并发不安全") {
		t.Fatalf("feedback missing: %s", out)
	}
	// 空反馈 → scheduler.none 占位。
	empty := RenderLeaderEscalationRounds(task, "")
	if !strings.Contains(empty, "（无）") {
		t.Fatalf("empty feedback must render none placeholder: %s", empty)
	}
}

// TestRenderLeaderEscalationStall 停摆升级文本。
// Python: render_leader_escalation_stall。
func TestRenderLeaderEscalationStall(t *testing.T) {
	task := reviewTaskFixture()
	out := RenderLeaderEscalationStall(task, 30, []string{"reviewer1"}, []string{"inspector1"})
	if !strings.Contains(out, "30") || !strings.Contains(out, "reviewer1") || !strings.Contains(out, "inspector1") {
		t.Fatalf("stall = %s", out)
	}
	// 双空列表 → none 占位。
	empty := RenderLeaderEscalationStall(task, 10, nil, nil)
	if !strings.Contains(empty, "（无）") {
		t.Fatalf("empty voted/pending must render none: %s", empty)
	}
}

// TestRenderLeaderEscalationNoVote 无票 reviewer 升级文本。
// Python: render_leader_escalation_no_vote。
func TestRenderLeaderEscalationNoVote(t *testing.T) {
	task := reviewTaskFixture()
	out := RenderLeaderEscalationNoVote(task, "reviewer1", 2)
	if !strings.Contains(out, "reviewer1") || !strings.Contains(out, "2") {
		t.Fatalf("no_vote = %s", out)
	}
}

// TestMetaRework_MetaReworkSummary 返工投递载荷。
// Python: meta_rework / meta_rework_summary。
func TestMetaRework_MetaReworkSummary(t *testing.T) {
	task := reviewTaskFixture()
	meta := MetaRework(task, 3, "- reviewer1: 重试")
	params := meta["params"].(map[string]string)
	if params["max_rounds"] != "3" || params["feedback"] != "- reviewer1: 重试" {
		t.Fatalf("rework params = %v", params)
	}
	// 空反馈 → scheduler.none。
	empty := MetaRework(task, 3, "")
	if empty["params"].(map[string]string)["feedback"] != "（无）" {
		t.Fatalf("empty feedback = %v", empty["params"])
	}
	// rework_summary。
	summary := MetaReworkSummary(task, 3)
	if summary["params"].(map[string]string)["max_rounds"] != "3" {
		t.Fatalf("summary params = %v", summary["params"])
	}
}

// TestFormatFailFeedback 带署名的 fail 反馈块。
// Python: format_fail_feedback。
func TestFormatFailFeedback(t *testing.T) {
	out := FormatFailFeedback(map[string]string{"reviewer1": "并发不安全", "reviewer2": "缺少测试"})
	if !strings.Contains(out, "- reviewer1: 并发不安全") || !strings.Contains(out, "- reviewer2: 缺少测试") {
		t.Fatalf("fail feedback = %s", out)
	}
}

// TestSubmissionBody_全文不截断 提交正文逐字（F_119 review 半区）。
// Python: _submission_body。
func TestSubmissionBody_全文不截断(t *testing.T) {
	task := reviewTaskFixture()
	out := SubmissionBody(task, "cn")
	if !strings.Contains(out, "摘要: 完成任务：实现导出功能") {
		t.Fatalf("cn body = %s", out)
	}
	if !strings.Contains(out, "详情:\n关键决策：使用流式导出") {
		t.Fatalf("note = %s", out)
	}
	// 英文标签。
	en := SubmissionBody(task, "en")
	if !strings.Contains(en, "Summary: 完成任务：实现导出功能") {
		t.Fatalf("en body = %s", en)
	}
	// 双缺失 → 空。
	bare := &database.TeamTaskBase{}
	if SubmissionBody(bare, "cn") != "" {
		t.Fatal("empty submission must be empty")
	}
}

// TestRenderReviewRequestForHarness 完整评审请求（指令/上一票/提交正文/upstream）。
// Python: render_review_request_for_harness。
func TestRenderReviewRequestForHarness(t *testing.T) {
	task := reviewTaskFixture()
	previous := &database.TeamTaskReviewVoteBase{
		ReviewRound: 1, Reviewer: "reviewer1", Decision: "fail",
		Feedback: database.StringPtr("请补充并发测试"),
	}
	upstream := []*database.TeamTaskBase{
		{TaskID: "task-u1", Title: "上游任务", Summary: database.StringPtr("上游完成")},
	}
	out := renderReviewRequestForHarness(task, "cn", "reviewer1", previous, upstream)
	if out == "" {
		t.Fatal("review request must render")
	}
	// 任务占位符替换。
	if strings.Contains(out, "{{task.") {
		t.Fatalf("unresolved placeholders: %s", out)
	}
	// per-reviewer instruction 注入。
	if !strings.Contains(out, "重点检查并发安全") {
		t.Fatalf("instruction missing: %s", out)
	}
	// previous_vote 注入。
	if !strings.Contains(out, "请补充并发测试") {
		t.Fatalf("previous vote missing: %s", out)
	}
	// submission body 逐字。
	if !strings.Contains(out, "摘要: 完成任务：实现导出功能") {
		t.Fatalf("submission body missing: %s", out)
	}
	// upstream 简报。
	if !strings.Contains(out, "上游任务") {
		t.Fatalf("upstream missing: %s", out)
	}
	// 无 previous/upstream/summary 的裸任务：段落省略，无占位残留。
	bare := &database.TeamTaskBase{TaskID: "task-bare", Title: "裸任务", Status: "in_review", ReviewRound: 1}
	bareOut := renderReviewRequestForHarness(bare, "cn", "reviewer1", nil, nil)
	if strings.Contains(bareOut, "{{task.") {
		t.Fatalf("bare request has placeholders: %s", bareOut)
	}
	if strings.Contains(bareOut, "\n\n\n") {
		t.Fatal("blank separators must be collapsed")
	}
}
