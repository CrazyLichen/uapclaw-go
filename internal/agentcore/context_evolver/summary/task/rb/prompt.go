package rb

import "text/template"

// ──────────────────────────── 结构体 ────────────────────────────

// ReasoningBankSummaryPrompt ReasoningBank 摘要相关提示词配置。
type ReasoningBankSummaryPrompt struct {
	// ExtractSuccessTrajSystemPrompt 成功轨迹提取系统提示词
	ExtractSuccessTrajSystemPrompt *template.Template
	// ExtractFailTrajSystemPrompt 失败轨迹提取系统提示词
	ExtractFailTrajSystemPrompt *template.Template
	// ExtractTrajUserPrompt 轨迹提取用户提示词
	ExtractTrajUserPrompt *template.Template
	// LLMJudgeSystemPrompt LLM 判定系统提示词
	LLMJudgeSystemPrompt *template.Template
	// LLMJudgeUserPrompt LLM 判定用户提示词
	LLMJudgeUserPrompt *template.Template
	// ParallelScalingSystemPrompt 并行缩放系统提示词
	ParallelScalingSystemPrompt *template.Template
	// ParallelScalingUserPrompt 并行缩放用户提示词
	ParallelScalingUserPrompt *template.Template
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// extractSuccessTrajSystemPrompt 成功轨迹提取系统提示词
	extractSuccessTrajSystemPrompt = `You are an expert in generating reusable memories. You will be given a user query, the corresponding trajectory that represents how an agent successfully accomplished the task.

## Guidelines
You need to extract and summarize useful insights in the format of memory items based on the agent's successful trajectory.
The goal of summarized memory items is to be helpful and generalizable for future similar tasks.

## Important notes
- You must first think why the trajectory is successful, and then summarize the insights.
- You can extract at most 3 memory items from the trajectory.
- You must not repeat similar or overlapping items.
- Do not mention specific queries, or string contents, but rather focus on the generalizable insights.

## Output Format
Your output must strictly follow the Markdown format shown below:
` + "```" + `
# Memory Item i
## Title <the title of the memory item>
## Description <one sentence summary of the memory item>
## Content <1-3 sentences describing the insights learned to successfully accomplishing the task>
` + "```" + `
`

	// extractFailTrajSystemPrompt 失败轨迹提取系统提示词
	extractFailTrajSystemPrompt = `You are an expert in generating reusable memories. You will be given a user query, the corresponding trajectory that represents how an agent attempted to resolve the task but failed.

## Guidelines
You need to extract and summarize useful insights in the format of memory items based on the agent's failed trajectory.
The goal of summarized memory items is to be helpful and generalizable for future similar tasks.

## Important notes
- You must first reflect and think why the trajectory failed, and then summarize what lessons you have learned or strategies to prevent the failure in the future.
- You can extract at most 3 memory items from the trajectory.
- You must not repeat similar or overlapping items.
- Do not mention specific websites, queries, or string contents, but rather focus on the generalizable insights.

## Output Format
Your output must strictly follow the Markdown format shown below:
` + "```" + `
# Memory Item i
## Title <the title of the memory item>
## Description <one sentence summary of the memory item>
## Content <1-3 sentences describing the insights learned to successfully accomplishing the task>
` + "```" + `
`

	// extractTrajUserPrompt 轨迹提取用户提示词
	extractTrajUserPrompt = `Query: {{.Query}}
Trajectory: {{.Trajectory}}
`

	// llmJudgeSystemPrompt LLM 判定系统提示词
	llmJudgeSystemPrompt = `You are an expert in evaluating the performance of an agent. The agent is designed to help a human solve problems.
Given the user's query, the agent's action history, and the agent's response to the user, your goal is to decide whether the agent's execution is successful or not.

*IMPORTANT*
Format your response into two lines as shown below:
Thoughts: <your thoughts and reasoning process>
Status: "success" or "failure"
`

	// llmJudgeUserPrompt LLM 判定用户提示词
	llmJudgeUserPrompt = `Query: {{.Query}}
Trajectory: {{.Trajectory}}
`

	// parallelScalingSystemPrompt 并行缩放系统提示词
	parallelScalingSystemPrompt = `
You are an expert in generating reusable memories. You will be given a user query and multiple trajectories showing how an agent attempted the task. Some trajectories may be successful, and others may have failed.

## Guidelines
Your goal is to compare and contrast these trajectories to identify the most useful and generalizable strategies as memory items.
Use self-contrast reasoning:
- Identify patterns and strategies that consistently led to success.
- Identify mistakes or inefficiencies from failed trajectories and formulate preventative strategies.
- Prefer strategies that generalize beyond specific pages or exact wording.

## Important notes
- Think first: Why did some trajectories succeed while others failed?
- You can extract at most 5 memory items from all trajectories combined.
- Do not repeat similar or overlapping items.
- Do not mention specific websites, queries, or string contents — focus on generalizable behaviors and reasoning patterns.
- Make sure each memory item captures actionable and transferable insights.

## Output Format
Your output must strictly follow the Markdown format shown below:
` + "```" + ` # Memory Item i
## Title <the title of the memory item>
## Description <one sentence summary of the memory item>
## Content <1-5 sentences describing the insights learned to successfully accomplishing the task> ` + "```" + `
`

	// parallelScalingUserPrompt 并行缩放用户提示词
	parallelScalingUserPrompt = `Query: {{.Query}}

Trajectories:
{{.Trajectories}}`
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultReasoningBankSummaryPrompt 默认 ReasoningBank 摘要提示词实例
	defaultReasoningBankSummaryPrompt = NewReasoningBankSummaryPrompt()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReasoningBankSummaryPrompt 创建 ReasoningBank 摘要提示词实例。
func NewReasoningBankSummaryPrompt() *ReasoningBankSummaryPrompt {
	return &ReasoningBankSummaryPrompt{
		ExtractSuccessTrajSystemPrompt: template.Must(template.New("extract_success").Parse(extractSuccessTrajSystemPrompt)),
		ExtractFailTrajSystemPrompt:    template.Must(template.New("extract_fail").Parse(extractFailTrajSystemPrompt)),
		ExtractTrajUserPrompt:          template.Must(template.New("extract_traj_user").Parse(extractTrajUserPrompt)),
		LLMJudgeSystemPrompt:           template.Must(template.New("llm_judge_system").Parse(llmJudgeSystemPrompt)),
		LLMJudgeUserPrompt:             template.Must(template.New("llm_judge_user").Parse(llmJudgeUserPrompt)),
		ParallelScalingSystemPrompt:    template.Must(template.New("parallel_scaling_system").Parse(parallelScalingSystemPrompt)),
		ParallelScalingUserPrompt:      template.Must(template.New("parallel_scaling_user").Parse(parallelScalingUserPrompt)),
	}
}
