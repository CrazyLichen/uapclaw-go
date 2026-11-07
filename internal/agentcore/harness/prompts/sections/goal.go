// 本文件对应 Python openjiuwen/harness/prompts/sections/goal.py。
// 本批（9.88）只落 <goal_task> 动态 query 的四个函数与三个双语常量；
// goal_protocol 附件、transcript assessor 提示词由 9.89 补在同一文件。
package sections

import (
	"fmt"
	"strings"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalTaskView <goal_task> 渲染所需的只读目标视图。
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

// ──────────────────────────── goal protocol 常量 ────────────────────────────

const (
	// goalProtocolPriority goal_protocol 附件优先级
	// Python: _GOAL_PROTOCOL_PRIORITY = 88 (goal.py L27)
	goalProtocolPriority = 88
)

var (
	// goalProtocolCN goal 协议中文逐字快照。
	// Python: _GOAL_PROTOCOL["cn"] (goal.py L30-51)
	goalProtocolCN = "\n\n# Goal 模式工作规则\n\n" +
		"当用户消息中包含 <goal_task> 标签时，本次调用处于 Goal 模式。" +
		"当前处理需要按照「尝试执行 -> 评估完成度」的节奏推进。" +
		"你不需要解释这个机制。 \n\n" +
		"Goal 上下文规则：\n" +
		"1. 用户消息中的 <goal_task> 是本次调用唯一可信的动态 goal 上下文，" +
		"包含 objective、previous_assessment、current_instruction 和 budget_notice；\n" +
		"2. 以 <goal_task> 中的目标和当前指令为准，不要依赖旧对话里的过期目标；\n" +
		"3. 用户如果在运行中补充约束，应该考虑将约束作为 goal 目标的完善；\n\n" +
		"工作原则：\n" +
		"1. 尽力完成整个目标，而不是刻意只完成一小步。" +
		"需要读代码、改文件、运行命令或验证时，正常使用可用工具。\n" +
		"2. 如果本次无法完成整个目标，完成最有价值、最可验证的推进，并留下清楚证据。\n" +
		"3. 不要为了推进 goal 询问用户「是否继续」。\n" +
		"4. 不要把「还有剩余工作」「任务较大」「还需要验证」「证据暂时不足」当成 blocked。" +
		"这些情况应提交 continue，并写清下一次应该补齐什么。\n" +
		"5. 只有缺少用户输入、权限、必要依赖、外部服务或环境状态，" +
		"导致无法继续任何有意义推进时，才提交 blocked。\n\n" +
		"6. 当完成尝试执行后，应调用工具 submit_goal_report 去评估完成度。" +
		"submit_goal_report 调用成功后**不允许再次调用其它工具**\n\n"

	// goalProtocolEN goal 协议英文逐字快照。
	// Python: _GOAL_PROTOCOL["en"] (goal.py L52-81)
	goalProtocolEN = "\n\n# Goal Mode Work Rules\n\n" +
		"When the user message contains a <goal_task> tag, this call is in Goal mode. " +
		"The current handling should progress in an \"attempt -> assess completion\" " +
		"cadence. Do not explain this mechanism.\n\n" +
		"Goal context rules:\n" +
		"1. The <goal_task> block in the user message is the sole authoritative dynamic goal " +
		"context for this call, containing objective, previous_assessment, current_instruction, " +
		"and budget_notice.\n" +
		"2. Use the objective and instructions from <goal_task>; do not rely on stale " +
		"goal descriptions from older conversation turns.\n" +
		"3. If the user adds constraints during execution, consider them refinements " +
		"to the goal objective.\n\n" +
		"Work principles:\n" +
		"1. Strive to complete the entire objective, not just one small step. " +
		"Use available tools normally when you need to read code, edit files, " +
		"run commands, or verify results.\n" +
		"2. If you cannot fully complete the objective this time, make the most " +
		"valuable, verifiable progress and leave clear evidence.\n" +
		"3. Do not ask the user \"should I continue?\" to advance the goal.\n" +
		"4. Do not treat \"remaining work\", \"large task\", \"needs verification\", " +
		"or \"insufficient evidence\" as blocked. These should be continue with a " +
		"clear description of what to address next.\n" +
		"5. Only submit blocked when lacking user input, permissions, required " +
		"dependencies, external services, or environment state that prevents any " +
		"meaningful progress.\n\n" +
		"6. After completing the attempt, call submit_goal_report to assess completion. " +
		"After submit_goal_report succeeds, **do not call any other tools**.\n\n"

	// transcriptAssessorSystemCN transcript assessor 系统提示词中文逐字快照。
	// Python: TRANSCRIPT_ASSESSOR_SYSTEM["cn"] (goal.py L217-243)
	transcriptAssessorSystemCN = "你是 Goal 完成度评估器。你只能根据输入中的目标、当前指令和本轮尝试的模型上下文判断目标状态。" +
		"不要执行工具，不要读取文件，不要发起后续工作。\n\n" +
		"你必须只输出一个 JSON 对象，不要输出 Markdown、解释文字或代码块。JSON 字段如下：\n" +
		"{\n" +
		"  \"status\": \"continue | complete | blocked\",\n" +
		"  \"evidence\": \"判断依据；complete/blocked 时须为面向用户的详细报告\",\n" +
		"  \"remaining_work\": \"status=continue 时填写剩余缺口，否则可为空字符串\",\n" +
		"  \"next_instruction\": \"status=continue 时填写下一次最具体、可执行的动作，否则可为空字符串\"\n" +
		"}\n\n" +
		"评估规则：\n" +
		"1. 先从目标和当前指令中提取必须满足的交付物、验收条件和可验证结果。\n" +
		"2. 判断依据必须来自本轮尝试上下文中的可验证证据，而不是主模型的语气或承诺。\n" +
		"3. 只有上下文证据表明验收条件已经满足，才能输出 status=\"complete\"。\n" +
		"4. 如果还缺少实现、测试、验证、交付物或证据，但仍然可以继续推进，输出 status=\"continue\"。\n" +
		"5. status=\"continue\" 不是失败；remaining_work 必须说明缺口，next_instruction 必须具体可执行；" +
		"此时 evidence 简述本轮已完成与可审计依据即可。\n" +
		"6. 只有缺少用户输入、权限、必要依赖、外部服务或环境状态，导致无法继续任何有意义推进时，才输出 status=\"blocked\"。\n" +
		"7. 任务复杂、尚未做完、测试没跑或证据不足都不是 blocked，通常应输出 continue。\n" +
		"8. 如果上下文显示目标已经完成且能找到相关依据，输出 complete。\n" +
		"9. 不输出 paused、cleared 或其它状态。\n" +
		"10. 当 status 为 complete 或 blocked 时，evidence 必须写成详细报告，供用户直接阅读，" +
		"不能只写一句结论。报告应覆盖：目标完成/阻塞结论、关键交付物或阻塞原因、" +
		"上下文中的关键事实与依据；complete 时应完整收录产物正文或等价可核对内容" +
		"（禁止仅用「见上文 / 如上 / 已给出」指代）；blocked 时应写清缺什么、为何无法继续、" +
		"用户或环境需提供什么才能解除阻塞。"

	// transcriptAssessorSystemEN transcript assessor 系统提示词英文逐字快照。
	// Python: TRANSCRIPT_ASSESSOR_SYSTEM["en"] (goal.py L244-285)
	transcriptAssessorSystemEN = "You are a Goal completion assessor. Judge goal status only from the objective, " +
		"the current instruction, and the model context from this attempt. Do not execute " +
		"tools, read files, or initiate follow-up work.\n\n" +
		"You must output exactly one JSON object. Do not output Markdown, explanation text, " +
		"or code blocks. JSON fields:\n" +
		"{\n" +
		"  \"status\": \"continue | complete | blocked\",\n" +
		"  \"evidence\": \"Basis for the judgment; for complete/blocked must be a detailed user-facing report\",\n" +
		"  \"remaining_work\": \"Gaps to fill when status=continue, else empty string\",\n" +
		"  \"next_instruction\": \"Most specific actionable step for status=continue, else empty string\"\n" +
		"}\n\n" +
		"Assessment rules:\n" +
		"1. Extract deliverables, acceptance criteria, and verifiable results " +
		"from the objective and current instruction.\n" +
		"2. Base judgment on verifiable evidence in the attempt context, " +
		"not the main model's tone or promises.\n" +
		"3. Output status=\"complete\" only when context evidence shows " +
		"acceptance criteria are met.\n" +
		"4. If implementation, tests, verification, deliverables, or evidence " +
		"are still missing but progress is possible, output status=\"continue\".\n" +
		"5. status=\"continue\" is not failure; remaining_work must describe " +
		"gaps, next_instruction must be specific and actionable; evidence may " +
		"briefly summarize progress and auditable basis for this attempt.\n" +
		"6. Output status=\"blocked\" only when user input, permissions, required " +
		"dependencies, external services, or environment state prevent any " +
		"meaningful progress.\n" +
		"7. Complex tasks, incomplete work, unrun tests, or insufficient " +
		"evidence are not blocked; usually output continue.\n" +
		"8. If the context shows the objective is complete and relevant " +
		"evidence can be found, output complete.\n" +
		"9. Do not output paused, cleared, or any other status.\n" +
		"10. When status is complete or blocked, evidence must be a detailed " +
		"report for the user to read directly—not a one-line conclusion. Cover: " +
		"the complete/blocked verdict, key deliverables or blockers, and the " +
		"main facts from context. For complete, include the deliverable body " +
		"or equivalent verifiable content in full (do not substitute with " +
		"\"see above / as shown earlier / already given\"). For blocked, state " +
		"what is missing, why progress cannot continue, and what the user or " +
		"environment must provide to unblock."
)

// ──────────────────────────── goal protocol / assessor 导出函数 ────────────────────────────

// BuildGoalProtocolSection 构造 goal 协议提示词节。
// Python: build_goal_protocol_section (goal.py L84)
func BuildGoalProtocolSection(language string) saprompt.PromptSection {
	content := goalProtocolCN
	if language == "en" {
		content = goalProtocolEN
	}
	return saprompt.PromptSection{
		Name:     SectionGoalProtocol,
		Content:  map[string]string{language: content},
		Priority: goalProtocolPriority,
	}
}

// TranscriptAssessorSystem 返回指定语言的 transcript assessor 系统提示词。
// Python: TRANSCRIPT_ASSESSOR_SYSTEM.get(language, cn) (task_completion_rail.py L678)
func TranscriptAssessorSystem(language string) string {
	if language == "en" {
		return transcriptAssessorSystemEN
	}
	return transcriptAssessorSystemCN
}

// BuildTranscriptAssessorPrompt 构造 transcript assessor 用户提示词。
// Python: build_transcript_assessor_prompt (goal.py L288)
func BuildTranscriptAssessorPrompt(objective, currentInstruction, attemptContext, language string) string {
	_ = language // 上游 build_transcript_assessor_prompt 忽略 language（goal.py L305 `_ = language`）
	return strings.Join([]string{
		fmt.Sprintf("<objective>\n%s\n</objective>", objective),
		fmt.Sprintf("<current_instruction>\n%s\n</current_instruction>", currentInstruction),
		fmt.Sprintf("<attempt_context>\n%s\n</attempt_context>", attemptContext),
	}, "\n\n")
}
