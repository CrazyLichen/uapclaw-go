package sections

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 用例 ────────────────────────────

// TestBuildGoalTaskQuery_首次尝试 校验首轮 XML 结构
// Python: test_goal_task_query_first_attempt (test_goal_prompts.py L19)
func TestBuildGoalTaskQuery_首次尝试(t *testing.T) {
	query := BuildGoalTaskQuery(GoalTaskView{Objective: "Build a REST API"}, "cn")

	assert.Contains(t, query, "<goal_task>")
	assert.Contains(t, query, "<objective>")
	assert.Contains(t, query, "Build a REST API")
	assert.Contains(t, query, "</goal_task>")
	assert.NotContains(t, query, "submit_goal_report")
	assert.Contains(t, query, "无。这是第一次尝试。")
}

// TestBuildGoalTaskQuery_带上次评估 校验 previous_assessment 渲染
// Python: test_goal_task_query_with_assessment (test_goal_prompts.py L30)
func TestBuildGoalTaskQuery_带上次评估(t *testing.T) {
	view := GoalTaskView{
		Objective: "Build a REST API",
		Previous: &GoalTaskAssessment{
			Status:          "continue",
			Evidence:        "endpoints created",
			RemainingWork:   "add tests",
			NextInstruction: "add tests",
		},
	}
	query := BuildGoalTaskQuery(view, "cn")

	assert.Contains(t, query, "status: continue")
	assert.Contains(t, query, "evidence: endpoints created")
	assert.Contains(t, query, "remaining_work: add tests")
	assert.Contains(t, query, "<current_instruction>\nadd tests\n</current_instruction>")
}

// TestBuildGoalTaskQuery_英文 校验语言切换
// Python: test_goal_task_query_en (test_goal_prompts.py L43)
func TestBuildGoalTaskQuery_英文(t *testing.T) {
	query := BuildGoalTaskQuery(GoalTaskView{Objective: "Build feature"}, "en")

	assert.Contains(t, query, "<goal_task>")
	assert.NotContains(t, query, "submit_goal_report")
	assert.Contains(t, query, "None. This is the first attempt.")
	assert.Contains(t, query, "Prioritize completing the entire objective")
	assert.NotContains(t, query, "无。这是第一次尝试。")
}

// TestBuildGoalTaskQuery_预算提示 校验剩余次数渲染
// Python: test_goal_task_query_budget_notice (test_goal_prompts.py L54)
func TestBuildGoalTaskQuery_预算提示(t *testing.T) {
	maxAttempts := 10
	tokenBudget := 8000
	view := GoalTaskView{
		Objective:    "objective",
		MaxAttempts:  &maxAttempts,
		TokenBudget:  &tokenBudget,
		AttemptCount: 3,
		TotalTokens:  1200,
	}
	query := BuildGoalTaskQuery(view, "cn")

	assert.Contains(t, query, "剩余尝试次数：7/10")
	assert.Contains(t, query, "Token 预算：1200/8000")
}

// TestBuildGoalCurrentInstruction 校验指令优先级
// Python: test_goal_current_instruction_uses_next_instruction (test_goal_prompts.py L62)
func TestBuildGoalCurrentInstruction(t *testing.T) {
	view := GoalTaskView{
		Objective: "Build a REST API",
		Previous:  &GoalTaskAssessment{Status: "continue", NextInstruction: "add tests"},
	}
	assert.Equal(t, "add tests", BuildGoalCurrentInstruction(view, "en"))

	assert.Equal(t,
		goalTaskDefaultInstructionEN,
		BuildGoalCurrentInstruction(GoalTaskView{Objective: "Build a REST API"}, "en"),
	)
}

// TestBuildGoalTaskQuery_双语逐字快照 防翻译漂移
func TestBuildGoalTaskQuery_双语逐字快照(t *testing.T) {
	cn := BuildGoalTaskQuery(GoalTaskView{Objective: "OBJ"}, "cn")
	assert.Equal(t, "<goal_task>\n<objective>\nOBJ\n</objective>\n\n"+
		"<previous_assessment>\n无。这是第一次尝试。\n</previous_assessment>\n\n"+
		"<current_instruction>\n优先尝试完成整个目标；如果本次无法完成，就完成最有价值、可验证的推进。\n</current_instruction>\n\n"+
		"<budget_notice>\n无。\n</budget_notice>\n</goal_task>", cn)

	en := BuildGoalTaskQuery(GoalTaskView{Objective: "OBJ"}, "en")
	assert.Equal(t, "<goal_task>\n<objective>\nOBJ\n</objective>\n\n"+
		"<previous_assessment>\nNone. This is the first attempt.\n</previous_assessment>\n\n"+
		"<current_instruction>\nPrioritize completing the entire objective; if not possible this time, "+
		"make the most valuable, verifiable progress.\n</current_instruction>\n\n"+
		"<budget_notice>\nNone.\n</budget_notice>\n</goal_task>", en)
}
