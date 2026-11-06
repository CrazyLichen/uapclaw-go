// 本文件对应 Python openjiuwen/harness/prompts/sections/goal.py。
//
// 本批（9.88）只落 <goal_task> 动态 query 的四个函数与三个双语常量；
// goal_protocol 附件、transcript assessor 提示词由 9.89 补在同一文件。
package sections

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalTaskView <goal_task> 渲染所需的只读目标视图。
//
// Python 侧 build_goal_task_query 直接吃 GoalRecord（TYPE_CHECKING 前向引用）；
// Go 用视图类型打断 sections ↔ goal 的循环依赖（spec D4）。
type GoalTaskView struct {
	// Objective 目标原文
	Objective string
	// AttemptCount 已开始尝试次数
	AttemptCount int
	// MaxAttempts 尝试次数上限（可空）
	MaxAttempts *int
	// TokenBudget token 预算上限（可空）
	TokenBudget *int
	// TotalTokens 已用 token
	TotalTokens int
	// Previous 上一次评估视图（可空）
	Previous *GoalTaskAssessment
}

// GoalTaskAssessment previous_assessment 的只读视图。
type GoalTaskAssessment struct {
	// Status 评估状态字符串
	Status string
	// Evidence 判断依据
	Evidence string
	// RemainingWork 剩余缺口
	RemainingWork string
	// NextInstruction 下一次动作
	NextInstruction string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// goalTaskNoPreviousCN 首轮中文占位
	goalTaskNoPreviousCN = "无。这是第一次尝试。"
	// goalTaskNoPreviousEN 首轮英文占位
	goalTaskNoPreviousEN = "None. This is the first attempt."
	// goalTaskDefaultInstructionCN 默认中文指令
	goalTaskDefaultInstructionCN = "优先尝试完成整个目标；如果本次无法完成，就完成最有价值、可验证的推进。"
	// goalTaskDefaultInstructionEN 默认英文指令
	goalTaskDefaultInstructionEN = "Prioritize completing the entire objective; if not possible this time, " +
		"make the most valuable, verifiable progress."
	// goalTaskNoBudgetCN 无预算提示（中文）
	goalTaskNoBudgetCN = "无。"
	// goalTaskNoBudgetEN 无预算提示（英文）
	goalTaskNoBudgetEN = "None."
	// goalTaskTemplateCN 中文 XML 模板
	goalTaskTemplateCN = `<goal_task>
<objective>
{objective}
</objective>

<previous_assessment>
{previous_assessment}
</previous_assessment>

<current_instruction>
{current_instruction}
</current_instruction>

<budget_notice>
{budget_notice}
</budget_notice>
</goal_task>`
	// goalTaskTemplateEN 英文 XML 模板（上游与中文逐字相同，保留独立常量便于未来分化）
	goalTaskTemplateEN = `<goal_task>
<objective>
{objective}
</objective>

<previous_assessment>
{previous_assessment}
</previous_assessment>

<current_instruction>
{current_instruction}
</current_instruction>

<budget_notice>
{budget_notice}
</budget_notice>
</goal_task>`
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildGoalTaskQuery 构造一轮 goal 尝试的用户 query。
// Python: build_goal_task_query (goal.py L171)
func BuildGoalTaskQuery(view GoalTaskView, language string) string {
	previous := formatPreviousAssessment(view.Previous, language)
	instruction := BuildGoalCurrentInstruction(view, language)
	budget := formatBudgetNotice(view, language)

	replacer := strings.NewReplacer(
		"{objective}", view.Objective,
		"{previous_assessment}", previous,
		"{current_instruction}", instruction,
		"{budget_notice}", budget,
	)
	return replacer.Replace(goalTaskTemplate(language))
}

// BuildGoalCurrentInstruction 返回本轮权威指令。
// Python: build_goal_current_instruction (goal.py L199)
func BuildGoalCurrentInstruction(view GoalTaskView, language string) string {
	if view.Previous != nil && view.Previous.NextInstruction != "" {
		return view.Previous.NextInstruction
	}
	if language == "en" {
		return goalTaskDefaultInstructionEN
	}
	return goalTaskDefaultInstructionCN
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// goalTaskTemplate 按语言取模板，未知语言回退中文。
// Python: _GOAL_TASK_TEMPLATE.get(language, cn) (goal.py L190)
func goalTaskTemplate(language string) string {
	if language == "en" {
		return goalTaskTemplateEN
	}
	return goalTaskTemplateCN
}

// formatPreviousAssessment 渲染 previous_assessment 段落。
// Python: _format_previous_assessment (goal.py L138)
func formatPreviousAssessment(previous *GoalTaskAssessment, language string) string {
	if previous == nil {
		if language == "en" {
			return goalTaskNoPreviousEN
		}
		return goalTaskNoPreviousCN
	}
	parts := []string{fmt.Sprintf("status: %s", previous.Status)}
	if previous.Evidence != "" {
		parts = append(parts, fmt.Sprintf("evidence: %s", previous.Evidence))
	}
	if previous.RemainingWork != "" {
		parts = append(parts, fmt.Sprintf("remaining_work: %s", previous.RemainingWork))
	}
	return strings.Join(parts, "\n")
}

// formatBudgetNotice 渲染 budget_notice 段落。
// Python: _format_budget_notice (goal.py L152)
func formatBudgetNotice(view GoalTaskView, language string) string {
	notices := make([]string, 0, 2)
	if view.MaxAttempts != nil {
		remaining := *view.MaxAttempts - view.AttemptCount
		if remaining < 0 {
			remaining = 0
		}
		if language == "en" {
			notices = append(notices,
				fmt.Sprintf("Remaining attempts: %d/%d", remaining, *view.MaxAttempts))
		} else {
			notices = append(notices,
				fmt.Sprintf("剩余尝试次数：%d/%d", remaining, *view.MaxAttempts))
		}
	}
	if view.TokenBudget != nil {
		if language == "en" {
			notices = append(notices,
				fmt.Sprintf("Token budget: %d/%d", view.TotalTokens, *view.TokenBudget))
		} else {
			notices = append(notices,
				fmt.Sprintf("Token 预算：%d/%d", view.TotalTokens, *view.TokenBudget))
		}
	}
	if len(notices) == 0 {
		if language == "en" {
			return goalTaskNoBudgetEN
		}
		return goalTaskNoBudgetCN
	}
	return strings.Join(notices, "\n")
}
