package signal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
)

// review_feedback_test.go 归因器全分支测试。
// Python: review_feedback.py 各策略分支一比一。

// fakeReviewLLM 桩：按脚本返回 JSON 文本或报错。
type fakeReviewLLM struct {
	payload string
	err     error
}

func (f *fakeReviewLLM) Invoke(_ context.Context, _ string, _ []map[string]any, _ time.Duration) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.payload, nil
}

// newTestAttributor 构造测试归因器。
func newTestAttributor(t *testing.T, llm ReviewFeedbackLLM) *ReviewFeedbackAttributor {
	t.Helper()
	attributor, err := NewReviewFeedbackAttributor(llm, "test-model", "cn", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return attributor
}

// TestReviewFeedbackAttributor_枚举全值 枚举字符串对齐。
func TestReviewFeedbackAttributor_枚举全值(t *testing.T) {
	if ReviewFeedbackActionEvolveExistingSkill != "evolve_existing_skill" ||
		ReviewFeedbackActionSuggestNewSkill != "suggest_new_skill" ||
		ReviewFeedbackActionRecordTaskFailure != "record_task_failure" ||
		ReviewFeedbackActionSkipUnattributed != "skip_unattributed" {
		t.Fatal("action enum values drifted")
	}
	if ReviewFeedbackSignalType != "review_feedback" || ReviewFeedbackSource != "scheduler_review_feedback" {
		t.Fatal("signal constants drifted")
	}
}

// TestReviewFeedbackAttributor_空反馈封闭 空反馈封闭返回。
// Python: attribute L152-156。
func TestReviewFeedbackAttributor_空反馈封闭(t *testing.T) {
	a := newTestAttributor(t, &fakeReviewLLM{})
	out := a.Attribute(context.Background(), "  ", nil)
	if out.Action != ReviewFeedbackActionSkipUnattributed {
		t.Fatalf("action = %s", out.Action)
	}
	if out.Reason != "empty review feedback cannot be attributed" {
		t.Fatalf("reason = %s", out.Reason)
	}
}

// TestReviewFeedbackAttributor_LLM失败封闭 LLM 失败封闭——绝不允许 Skill 变更。
// Python: attribute L164-171。
func TestReviewFeedbackAttributor_LLM失败封闭(t *testing.T) {
	a := newTestAttributor(t, &fakeReviewLLM{err: errors.New("boom")})
	out := a.Attribute(context.Background(), "并发不安全", nil)
	if out.Action != ReviewFeedbackActionSkipUnattributed {
		t.Fatalf("action = %s", out.Action)
	}
	if out.Reason != "review feedback attribution failed; no Skill mutation is allowed" {
		t.Fatalf("reason = %s", out.Reason)
	}
}

// TestReviewFeedbackAttributor_无证据不判SkillIssue skill_issue 但无读取证据 → 封闭。
// Python: _enforce_policy L296-299。
func TestReviewFeedbackAttributor_无证据不判SkillIssue(t *testing.T) {
	a := newTestAttributor(t, &fakeReviewLLM{payload: `{"classification":"skill_issue","skill_name":"data-processor","target":"body","reusable_guidance":"补充并发说明","is_reusable":true,"confidence":0.9}`})
	out := a.Attribute(context.Background(), "并发不安全", &ReviewFeedbackContext{})
	if out.Action != ReviewFeedbackActionSkipUnattributed {
		t.Fatalf("action = %s", out.Action)
	}
	if out.Reason != "no SKILL.md read evidence exists; existing Skill evolution is forbidden" {
		t.Fatalf("reason = %s", out.Reason)
	}
}

// TestReviewFeedbackAttributor_四分类处置矩阵 executor_error / new_skill_pattern 两种形态 / skill_issue 完整路径。
// Python: _enforce_policy 全分支。
func TestReviewFeedbackAttributor_四分类处置矩阵(t *testing.T) {
	ctx := &ReviewFeedbackContext{
		SkillReads:      []string{"data-processor"},
		SkillContents:   map[string]string{"data-processor": "# data-processor"},
		RepetitionCount: 2,
	}

	// executor_error → RECORD_TASK_FAILURE。
	exec := newTestAttributor(t, &fakeReviewLLM{payload: `{"classification":"executor_error","reason":"执行者没遵循","is_reusable":true,"confidence":0.8}`})
	out := exec.Attribute(context.Background(), "忘了写文件", ctx)
	if out.Action != ReviewFeedbackActionRecordTaskFailure || out.IsSkillActionable {
		t.Fatalf("executor_error: %+v", out)
	}
	if !out.ShouldRecordTaskFailure() {
		t.Fatal("should_record_task_failure")
	}

	// new_skill_pattern + 重复证据 + 可复用 → SUGGEST_NEW_SKILL。
	newSkill := newTestAttributor(t, &fakeReviewLLM{payload: `{"classification":"new_skill_pattern","reusable_guidance":"先建索引再导出","is_reusable":true,"confidence":0.7}`})
	out = newSkill.Attribute(context.Background(), "缺导出流程", ctx)
	if out.Action != ReviewFeedbackActionSuggestNewSkill || out.ShouldCreateSkill() == false {
		t.Fatalf("new_skill_pattern: %+v", out)
	}

	// new_skill_pattern 无重复证据 → SKIP（保留安全候选）。
	sparseCtx := &ReviewFeedbackContext{SkillReads: []string{"data-processor"}}
	out = newSkill.Attribute(context.Background(), "缺导出流程", sparseCtx)
	if out.Action != ReviewFeedbackActionSkipUnattributed {
		t.Fatalf("sparse new_skill_pattern: %+v", out)
	}
	if out.ReusableGuidance != "先建索引再导出" {
		t.Fatalf("candidate guidance must be preserved: %q", out.ReusableGuidance)
	}

	// skill_issue 完整路径 → EVOLVE_EXISTING_SKILL。
	skillIssue := newTestAttributor(t, &fakeReviewLLM{payload: `{"classification":"skill_issue","skill_name":"data-processor","target":"body","reusable_guidance":"补充并发安全说明","is_reusable":true,"confidence":0.9,"reason":"Skill 缺并发指导"}`})
	out = skillIssue.Attribute(context.Background(), "并发不安全", ctx)
	if out.Action != ReviewFeedbackActionEvolveExistingSkill || !out.IsSkillActionable || out.SkillName != "data-processor" {
		t.Fatalf("skill_issue: %+v", out)
	}

	// unattributed → SKIP 封闭。
	unknown := newTestAttributor(t, &fakeReviewLLM{payload: `{"classification":"unattributed","reason":"无法归因"}`})
	out = unknown.Attribute(context.Background(), "随机失败", ctx)
	if out.Action != ReviewFeedbackActionSkipUnattributed || out.Classification != ReviewFeedbackClassificationUnattributed {
		t.Fatalf("unattributed: %+v", out)
	}
}

// TestReviewFeedbackAttributor_越权Skill名拒绝 skill_name 不在读取证据内 → 封闭。
// Python: _enforce_policy L305-308。
func TestReviewFeedbackAttributor_越权Skill名拒绝(t *testing.T) {
	a := newTestAttributor(t, &fakeReviewLLM{payload: `{"classification":"skill_issue","skill_name":"other-skill","target":"body","reusable_guidance":"x","is_reusable":true}`})
	out := a.Attribute(context.Background(), "问题", &ReviewFeedbackContext{SkillReads: []string{"data-processor"}})
	if out.Action != ReviewFeedbackActionSkipUnattributed {
		t.Fatalf("action = %s", out.Action)
	}
	if out.Reason != "the proposed Skill is not backed by SKILL.md read evidence" {
		t.Fatalf("reason = %s", out.Reason)
	}
}

// TestReviewFeedbackAttributor_单读取证据回填 skill_name 缺省时唯一读取证据回填。
// Python: _enforce_policy L301-303。
func TestReviewFeedbackAttributor_单读取证据回填(t *testing.T) {
	a := newTestAttributor(t, &fakeReviewLLM{payload: `{"classification":"skill_issue","target":"body","reusable_guidance":"补充说明","is_reusable":true}`})
	out := a.Attribute(context.Background(), "问题", &ReviewFeedbackContext{SkillReads: []string{"data-processor"}, SkillContents: map[string]string{"data-processor": "c"}})
	if out.Action != ReviewFeedbackActionEvolveExistingSkill || out.SkillName != "data-processor" {
		t.Fatalf("fallback: %+v", out)
	}
}

// TestReviewFeedbackAttributor_构造校验 model 空/timeout 非正拒绝。
// Python: __init__ L126-133。
func TestReviewFeedbackAttributor_构造校验(t *testing.T) {
	if _, err := NewReviewFeedbackAttributor(nil, "  ", "cn", time.Second); err == nil {
		t.Fatal("empty model must fail")
	}
	if _, err := NewReviewFeedbackAttributor(nil, "m", "cn", 0); err == nil {
		t.Fatal("zero timeout must fail")
	}
}

// TestAttributionToEvolutionSignal 可操作归因 → 标准演化信号。
// Python: attribution_to_evolution_signal。
func TestAttributionToEvolutionSignal(t *testing.T) {
	// 不可操作 → nil。
	if AttributionToEvolutionSignal(&ReviewFeedbackAttribution{Action: ReviewFeedbackActionSkipUnattributed}, "t1", 1) != nil {
		t.Fatal("non-actionable must yield nil")
	}
	sig := AttributionToEvolutionSignal(&ReviewFeedbackAttribution{
		Action:            ReviewFeedbackActionEvolveExistingSkill,
		IsSkillActionable: true,
		SkillName:         "data-processor",
		Target:            EvolutionTargetBody,
		HasTarget:         true,
		ReusableGuidance:  "补充并发说明",
		FeedbackExcerpt:   "并发不安全",
	}, "t1", 1)
	if sig == nil {
		t.Fatal("actionable must yield signal")
	}
	if sig.SignalType != ReviewFeedbackSignalType || sig.Section != "Reviewer Feedback" {
		t.Fatalf("signal = %+v", sig)
	}
	if sig.SkillName == nil || *sig.SkillName != "data-processor" {
		t.Fatalf("skill name = %v", sig.SkillName)
	}
	if !strings.Contains(sig.Excerpt, "Reusable guidance: 补充并发说明") {
		t.Fatalf("excerpt = %s", sig.Excerpt)
	}
}

// TestExtractSkillReadsFromTrajectory SKILL.md 读取证明与 skill_tool 证明。
// Python: _extract_skill_reads_from_trajectory + _tool_call_proves_skill_read。
func TestExtractSkillReadsFromTrajectory(t *testing.T) {
	traj, err := trajectory.NewTestTrajectory("sess", nil, []trajectory.TestStep{
		{Kind: "tool", ToolName: "read_file", CallArgs: map[string]any{"path": "/skills/data-processor/SKILL.md"}},
		{Kind: "tool", ToolName: "skill_tool", CallArgs: map[string]any{"skill_name": "other-skill"}},
		{Kind: "tool", ToolName: "bash", CallArgs: map[string]any{"command": "cat /tmp/unrelated.txt"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reads := ExtractSkillReadsFromTrajectory(traj, []string{"data-processor", "other-skill", "bash-skill"})
	if len(reads) != 2 || reads[0] != "data-processor" || reads[1] != "other-skill" {
		t.Fatalf("reads = %v", reads)
	}
}

// TestParseJSONObject_围栏与噪声 容忍 ``` 围栏与前后文本。
// Python: _parse_json_object。
func TestParseJSONObject_围栏与噪声(t *testing.T) {
	out, err := parseJSONObject("```json\n{\"classification\":\"skill_issue\"}\n```")
	if err != nil || out["classification"] != "skill_issue" {
		t.Fatalf("fenced parse = %v err=%v", out, err)
	}
	out, err = parseJSONObject("前置文本 {\"classification\":\"unattributed\"} 后置文本")
	if err != nil || out["classification"] != "unattributed" {
		t.Fatalf("noisy parse = %v err=%v", out, err)
	}
	if _, err = parseJSONObject("不是 JSON"); err == nil {
		t.Fatal("non-JSON must fail")
	}
}

// TestTrajectoryExcerpt 轨迹摘要行（tool/llm）。
// Python: _trajectory_excerpt。
func TestTrajectoryExcerpt(t *testing.T) {
	traj, err := trajectory.NewTestTrajectory("sess", nil, []trajectory.TestStep{
		{Kind: "tool", ToolName: "write_file", CallArgs: map[string]any{"path": "/x"}, CallResult: map[string]any{"ok": 1}},
		{Kind: "llm", Messages: []map[string]any{{"role": "user", "content": "q"}}, Response: map[string]any{"content": "分析完成"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	excerpt := TrajectoryExcerpt(traj)
	if !strings.Contains(excerpt, "[tool] write_file") || !strings.Contains(excerpt, "[assistant] 分析完成") {
		t.Fatalf("excerpt = %s", excerpt)
	}
}
