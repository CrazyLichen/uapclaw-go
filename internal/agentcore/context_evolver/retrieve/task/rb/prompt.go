package rb

import "text/template"

// ──────────────────────────── 结构体 ────────────────────────────

// ReasoningBankPrompt ReasoningBank 检索和 MaTTS 相关提示词配置。
type ReasoningBankPrompt struct {
	// LLMJudgeSystemPrompt LLM 评判系统提示词
	LLMJudgeSystemPrompt *template.Template
	// LLMJudgeUserPrompt LLM 评判用户提示词
	LLMJudgeUserPrompt *template.Template
	// BestOfNEvalPrompt Best-of-N 评估提示词
	BestOfNEvalPrompt *template.Template
	// SelfContrastPrompt 自对比记忆提取提示词
	SelfContrastPrompt *template.Template
	// SequentialFirstRefinePrompt 串行缩放首次精炼提示词
	SequentialFirstRefinePrompt *template.Template
	// SequentialFollowUpRefinePrompt 串行缩放后续精炼提示词
	SequentialFollowUpRefinePrompt *template.Template
}

// ──────────────────────────── 常量 ────────────────────────────

// 提示词模板 — 一比一复刻 Python 原文，不做自行翻译
// 占位符从 Python 的 {xxx} 改为 Go text/template 的 {{.Xxx}} 格式

// llmJudgeSystemPrompt LLM 评判系统提示词
// 对齐 Python LLM_JUDGE_SYSTEM_PROMPT
const llmJudgeSystemPrompt = `You are an expert in evaluating the performance of an agent. \
The agent is designed to help a human solve problems.
Given the user's query, the agent's action history, and the agent's response to the user, \
your goal is to decide whether the agent's execution is successful or not.

*IMPORTANT*
Format your response into two lines as shown below:
Thoughts: <your thoughts and reasoning process>
Status: "success" or "failure"
`

// llmJudgeUserPrompt LLM 评判用户提示词
// 对齐 Python LLM_JUDGE_USER_PROMPT
const llmJudgeUserPrompt = `Query: {{.Query}}
Trajectory: {{.Trajectory}}
`

// bestOfNEvalPrompt Best-of-N 评估提示词
// 对齐 Python matts.py BestOfNOp.async_execute 中的 eval_prompt
const bestOfNEvalPrompt = `You are an expert in evaluating agent trajectories. \
You will be given the user query and {{.NumTrajectories}} candidate trajectories.
Your job is to select the single best trajectory that most effectively \
and efficiently solves the task.

Query: {{.Query}}

{{.TrajDescriptions}}

## Evaluation Criteria:
1. Progress Toward Goal: How well the trajectory advances toward completing the task
2. Trajectory Efficiency: How efficiently progress is achieved given number of steps
3. Error Severity: Assess fatal, significant, or minor errors
4. Overall Quality: Logical flow, coherence, and closeness to goal

Return ONLY the index (0-{{.MaxIndex}}) of the best trajectory.`

// selfContrastPrompt 自对比记忆提取提示词
// 对齐 Python matts.py SelfContrastMemoryOp.async_execute 中的 extraction_prompt
const selfContrastPrompt = `You are an expert in extracting reasoning strategies. \
You will be given a user query and multiple trajectories showing \
how an agent attempted the task. Some trajectories may be successful, \
and others may have failed.

## Guidelines
Your goal is to compare and contrast these trajectories to identify the most useful and generalizable strategies as memory items.

Use self-contrast reasoning:
- Identify patterns and strategies that consistently led to success
- Identify mistakes or inefficiencies from failed trajectories and formulate preventative strategies
- Prefer strategies that generalize beyond specific pages or exact wording

## Important notes
- Think first: Why did some trajectories succeed while others failed?
- You can extract at most 5 memory items from all trajectories combined
- Do not repeat similar or overlapping items
- Do not mention specific websites, queries, or string contents — focus on generalizable behaviors and reasoning patterns
- Make sure each memory item captures actionable and transferable insights

## Output Format
Your output must strictly follow the Markdown format shown below:
` + "```" + `
# Memory Item 1
## Title <the title of the memory item>
## Description <one sentence summary of the memory item>
## Content <1-5 sentences describing the insights learned to successfully accomplishing the task>

# Memory Item 2
...
` + "```" + `

Query: {{.Query}}

Successful Trajectories ({{.NumSuccessful}}):
{{.SuccessfulTrajectories}}

Failed Trajectories ({{.NumFailed}}):
{{.FailedTrajectories}}
`

// sequentialFirstRefinePrompt 串行缩放首次精炼提示词
// 对齐 Python matts.py SequentialScalingOp.async_execute 首轮 refinement_prompt
const sequentialFirstRefinePrompt = `Important: Let's carefully re-examine the previous trajectory,
including your reasoning steps and actions taken.

Pay special attention to whether you used the correct approach, and whether your response addresses
the user query. If you find inconsistencies, correct them. If everything seems correct, confirm your final answer.

Previous answer: {{.CurrentAnswer}}

Query: {{.Query}}`

// sequentialFollowUpRefinePrompt 串行缩放后续精炼提示词
// 对齐 Python matts.py SequentialScalingOp.async_execute 后续轮 refinement_prompt
const sequentialFollowUpRefinePrompt = `Let's check again.

Previous answer: {{.CurrentAnswer}}

Query: {{.Query}}`

// ──────────────────────────── 全局变量 ────────────────────────────

// defaultReasoningBankPrompt 默认提示词实例
var defaultReasoningBankPrompt = NewReasoningBankPrompt()

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReasoningBankPrompt 创建默认提示词实例并解析模板。
func NewReasoningBankPrompt() *ReasoningBankPrompt {
	return &ReasoningBankPrompt{
		LLMJudgeSystemPrompt:          template.Must(template.New("llm_judge_system").Parse(llmJudgeSystemPrompt)),
		LLMJudgeUserPrompt:            template.Must(template.New("llm_judge_user").Parse(llmJudgeUserPrompt)),
		BestOfNEvalPrompt:             template.Must(template.New("best_of_n_eval").Parse(bestOfNEvalPrompt)),
		SelfContrastPrompt:            template.Must(template.New("self_contrast").Parse(selfContrastPrompt)),
		SequentialFirstRefinePrompt:   template.Must(template.New("seq_first_refine").Parse(sequentialFirstRefinePrompt)),
		SequentialFollowUpRefinePrompt: template.Must(template.New("seq_follow_up_refine").Parse(sequentialFollowUpRefinePrompt)),
	}
}
