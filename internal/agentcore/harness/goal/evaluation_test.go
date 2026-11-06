package goal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newEvaluationRecord 构造评估用记录
func newEvaluationRecord() *GoalRecord {
	return CreateGoalRecord("session-1", "Build a feature", nil, nil)
}

// newReport 构造主模型自报
func newReport(status GoalAssessmentStatus) *GoalAssessment {
	return &GoalAssessment{Status: status, Evidence: "agent evidence"}
}

// jsonText 序列化 JSON 文本
func jsonText(t *testing.T, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return string(raw)
}

// intPtr 取整型指针
func intPtr(value int) *int { return &value }

// ──────────────────────────── 用例 ────────────────────────────

// TestParseAssessmentJSON_裸与围栏 校验两种形态都能解析
// Python: test_parse_assessment_json_accepts_plain_and_fenced_json (test_goal_evaluation.py L28)
func TestParseAssessmentJSON_裸与围栏(t *testing.T) {
	plain := jsonText(t, map[string]any{"status": "complete", "evidence": "tests passed"})
	fenced := "```json\n" + plain + "\n```"

	parsed := parseAssessmentJSON(plain)
	require.NotNil(t, parsed)
	assert.Equal(t, GoalAssessmentStatusComplete, parsed.Status)
	assert.Equal(t, "tests passed", parsed.Evidence)

	parsed = parseAssessmentJSON(fenced)
	require.NotNil(t, parsed)
	assert.Equal(t, GoalAssessmentStatusComplete, parsed.Status)

	assert.Nil(t, parseAssessmentJSON(`{"status": "complete"}`), "缺 evidence 必须返回 nil")
	assert.Nil(t, parseAssessmentJSON("not json"))
	assert.Nil(t, parseAssessmentJSON(`{"status": "paused", "evidence": "x"}`), "非法状态必须返回 nil")
}

// TestParseAssessmentJSON_可选字段 校验 remaining_work/next_instruction 的空值语义
func TestParseAssessmentJSON_可选字段(t *testing.T) {
	parsed := parseAssessmentJSON(jsonText(t, map[string]any{
		"status": "continue", "evidence": "progress",
		"remaining_work": "add tests", "next_instruction": "",
	}))
	require.NotNil(t, parsed)
	require.NotNil(t, parsed.RemainingWork)
	assert.Equal(t, "add tests", *parsed.RemainingWork)
	assert.Nil(t, parsed.NextInstruction, "空串等价于 None")
}

// TestGoalEvaluator_AgentReport策略 校验自报缺失时回退 continue
// Python: test_agent_report_strategy_uses_report_and_falls_back_when_absent (L38)
func TestGoalEvaluator_AgentReport策略(t *testing.T) {
	assessor := NewGoalEvaluator(&GoalStopConfig{Strategy: GoalStopStrategyAgentReport})

	got := assessor.Assess(newEvaluationRecord(), newReport(GoalAssessmentStatusComplete), nil)
	assert.Equal(t, GoalAssessmentStatusComplete, got.Status)

	fallback := assessor.Assess(newEvaluationRecord(), nil, nil)
	assert.Equal(t, GoalAssessmentStatusContinue, fallback.Status)
	assert.Contains(t, fallback.Evidence, "no_report")
}

// TestGoalEvaluator_Transcript策略 校验只用监理输出
// Python: test_transcript_strategy_uses_verified_response (L50)
func TestGoalEvaluator_Transcript策略(t *testing.T) {
	assessor := NewGoalEvaluator(&GoalStopConfig{Strategy: GoalStopStrategyTranscript})
	transcript := jsonText(t, map[string]any{"status": "blocked", "evidence": "credential missing"})

	got := assessor.Assess(newEvaluationRecord(), nil, &transcript)
	assert.Equal(t, GoalAssessmentStatusBlocked, got.Status)
	assert.Equal(t, "credential missing", got.Evidence)

	noResponse := assessor.Assess(newEvaluationRecord(), nil, nil)
	assert.Equal(t, GoalAssessmentStatusContinue, noResponse.Status)
	assert.Contains(t, noResponse.Evidence, "assessment_error")
}

// TestGoalEvaluator_Hybrid_终态须复核 校验 complete 必须被复核
// Python: test_hybrid_requires_transcript_for_terminal_agent_report (L61)
func TestGoalEvaluator_Hybrid_终态须复核(t *testing.T) {
	assessor := NewGoalEvaluator(nil)

	unverified := assessor.Assess(newEvaluationRecord(), newReport(GoalAssessmentStatusComplete), nil)
	assert.Equal(t, GoalAssessmentStatusContinue, unverified.Status)
	assert.Contains(t, unverified.Evidence, "transcript_unavailable")

	transcript := jsonText(t, map[string]any{"status": "continue", "evidence": "verification pending"})
	verified := assessor.Assess(newEvaluationRecord(), newReport(GoalAssessmentStatusComplete), &transcript)
	assert.Equal(t, GoalAssessmentStatusContinue, verified.Status)
	assert.Equal(t, "verification pending", verified.Evidence, "冲突时监理赢")
}

// TestGoalEvaluator_Hybrid_continue直接信 校验 continue 不触发复核
func TestGoalEvaluator_Hybrid_continue直接信(t *testing.T) {
	assessor := NewGoalEvaluator(nil)
	record := newEvaluationRecord()
	record.MaxAttempts = intPtr(5)
	record.AttemptCount = 1

	got := assessor.Assess(record, newReport(GoalAssessmentStatusContinue), nil)
	assert.Equal(t, GoalAssessmentStatusContinue, got.Status)
	assert.Equal(t, "agent evidence", got.Evidence, "未抽检时原样返回自报")
}

// TestGoalEvaluator_Hybrid_无报告用监理 校验 report 缺失时走监理
func TestGoalEvaluator_Hybrid_无报告用监理(t *testing.T) {
	assessor := NewGoalEvaluator(nil)
	transcript := jsonText(t, map[string]any{"status": "complete", "evidence": "verified"})

	got := assessor.Assess(newEvaluationRecord(), nil, &transcript)
	assert.Equal(t, GoalAssessmentStatusComplete, got.Status)
	assert.Equal(t, "verified", got.Evidence)
}

// TestGoalEvaluator_抽检 校验间隔命中时复核
// Python: test_hybrid_spot_check_can_override_continue_report (L77)
func TestGoalEvaluator_抽检(t *testing.T) {
	interval := 2
	assessor := NewGoalEvaluator(&GoalStopConfig{
		Strategy:             GoalStopStrategyHybrid,
		VerificationInterval: &interval,
	})
	transcript := jsonText(t, map[string]any{"status": "complete", "evidence": "verified"})

	record := newEvaluationRecord()
	record.AttemptCount = 2
	got := assessor.Assess(record, newReport(GoalAssessmentStatusContinue), &transcript)
	assert.Equal(t, GoalAssessmentStatusComplete, got.Status)

	record.AttemptCount = 1
	plain := assessor.Assess(record, newReport(GoalAssessmentStatusContinue), &transcript)
	assert.Equal(t, GoalAssessmentStatusContinue, plain.Status, "未到抽检点则不复核")

	assert.False(t, assessor.ShouldSpotCheck(&GoalRecord{AttemptCount: 0}))
}

// TestGoalEvaluator_硬上限只拦CONTINUE 校验终态不被预算翻盘
// Python: test_hard_limits_block_only_continue (test_goal_evaluation.py L89)
func TestGoalEvaluator_硬上限只拦CONTINUE(t *testing.T) {
	maxed := newEvaluationRecord()
	maxed.AttemptCount = 3
	maxed.MaxAttempts = intPtr(3)

	budgeted := newEvaluationRecord()
	budgeted.TokenBudget = intPtr(10)
	budgeted.TokenUsage.TotalTokens = 10

	assessor := NewGoalEvaluator(nil)
	assert.Equal(t,
		GoalAssessmentStatusBlocked,
		assessor.Assess(maxed, newReport(GoalAssessmentStatusContinue), nil).Status,
	)
	assert.Equal(t,
		GoalAssessmentStatusBlocked,
		assessor.Assess(budgeted, newReport(GoalAssessmentStatusContinue), nil).Status,
	)

	reportAssessor := NewGoalEvaluator(&GoalStopConfig{Strategy: GoalStopStrategyAgentReport})
	assert.Equal(t,
		GoalAssessmentStatusComplete,
		reportAssessor.Assess(maxed, newReport(GoalAssessmentStatusComplete), nil).Status,
		"COMPLETE 不因超上限被改判",
	)

	blocked := assessor.Assess(maxed, newReport(GoalAssessmentStatusContinue), nil)
	assert.Contains(t, blocked.Evidence, "max_attempts_exhausted: 3/3")

	budgetBlocked := assessor.Assess(budgeted, newReport(GoalAssessmentStatusContinue), nil)
	assert.Contains(t, budgetBlocked.Evidence, "token_budget_exhausted: 10/10")
}

// TestGoalEvaluator_配置覆盖记录上限 校验 config 优先于 record
func TestGoalEvaluator_配置覆盖记录上限(t *testing.T) {
	record := newEvaluationRecord()
	record.AttemptCount = 3
	record.MaxAttempts = intPtr(10)

	assessor := NewGoalEvaluator(&GoalStopConfig{
		Strategy:    GoalStopStrategyAgentReport,
		MaxAttempts: intPtr(2),
	})
	got := assessor.Assess(record, newReport(GoalAssessmentStatusContinue), nil)
	assert.Equal(t, GoalAssessmentStatusBlocked, got.Status, "config 上限更严时以 config 为准")
	assert.Contains(t, got.Evidence, "/2")
}
