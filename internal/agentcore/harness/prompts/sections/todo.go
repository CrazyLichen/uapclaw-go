package sections

import (
	"strings"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	todoSystemPromptEN = `
Use the todo tools (todo_create, todo_modify, todo_list) to break down and manage your work. These tools help track progress, organize complex tasks, and ensure all requirements are completed.

**When to create a task list — call todo_create immediately when:**
- User explicitly requests a todo list or provides multiple items to complete
- Task requires 3 or more distinct steps
- Task has planning nature (multi-step implementation, feature development, etc.)

Identify the planning need and call todo_create BEFORE starting execution.

**Parallel awareness when breaking down tasks:**
- If a phase involves applying the same processing to multiple independent objects
  (multiple documents, sources, or files), design it as a **single "parallel batch" task**
  rather than creating one todo item per object.
- The task's description MUST explicitly list all parallel sub-items, for example:
  "Read and analyse the following 5 papers in parallel: [Paper A, Paper B, Paper C, Paper D, Paper E],
  each delegated to an independent subagent."
  This makes it unambiguous that one task_tool call should be dispatched per sub-item at execution time.

**Task management rules:**
- Update status in real-time: call todo_modify the moment a task status changes
- Only one task can be in_progress at a time; complete it before starting the next
  - Exception: if an in_progress task needs to process multiple independent sub-items in parallel
    (e.g., reading several documents simultaneously, concurrently searching multiple sources),
    you may issue multiple task_tool calls at once within that task and wait for all to return
    before marking the task completed. This is intra-task parallelism and does not violate the
    "one in_progress at a time" rule.
- Batch updates: consolidate multiple status changes into a single todo_modify call
- Cancel tasks that are no longer needed
- Can understand the current task planning progress by calling todo_list.

**Before marking a task completed:**
- Verify the work is fully done (e.g., run tests to confirm)
- Never mark completed if: partially implemented, tests failing, unresolved errors
- After completing, check if new follow-up tasks were discovered and append them via todo_modify
`

	todoSystemPromptCN = `
使用 todo 工具（todo_create、todo_modify、todo_list）拆解和管理工作。这些工具用于跟踪进度、组织复杂任务，确保所有需求都被完成。

**何时创建任务列表 — 以下情况立即调用 todo_create：**
- 用户明确要求使用待办清单，或提供了多个待完成事项
- 任务需要 3 个或更多步骤
- 任务具有规划性质（多步骤实现、功能开发等）

**识别到规划需求后，在开始执行前立即调用 todo_create。**

**拆解任务时的并行意识：**
- 若某个阶段涉及对多个独立对象（多篇文档、多个来源、多个文件）进行相同处理，
  应将其设计为**单个"并行批处理"任务**，而非为每个对象单独建立一条 todo。
- 该任务的 description 中必须明确列出所有并行子项，例如：
  "并行阅读并分析以下5篇论文：[论文A, 论文B, 论文C, 论文D, 论文E]，每篇委派独立子代理处理"
  这样执行时才能清楚地为每个子项发出一个 task_tool 调用。

**任务管理规则：**
- 实时更新状态：任务状态变化时立即调用 todo_modify
- 同一时间只能有一个任务处于 in_progress，完成后再开始下一个
  - 例外：若某个 in_progress 任务内部需要并行处理多个独立子项（如同时阅读多篇文档、并发搜索多个来源），
    可在该任务内一次性发出多个 task_tool 调用，等全部返回后再将任务标记为 completed。
    这属于任务内部的并行执行，不违反"同一时间只有一个 in_progress"原则。
- 批量更新：将多个状态变更合并为一次 todo_modify 调用
- 不再需要的任务用 todo_modify 标记为 cancelled
- 可通过调用 todo_list 了解当前任务规划进展

**将任务标记为已完成前：**
- 必须仔细验证工作已全部完成（如运行测试用例）
- 以下情况绝对不能标记为已完成：部分实现、测试失败、存在未解决的错误等
- 标记完成后，检查实现过程中是否发现新的后续任务，及时通过 todo_modify 追加
`

	progressReminderUserPromptEN = `
The following is the content and status of all tasks in the current task plan:

{tasks}

The task currently being executed is:

{in_progress_task}

Please review the above task progress to ensure the plan is being executed correctly.
If any tasks are stuck or need adjustment, please update them promptly
`

	progressReminderUserPromptCN = `
以下是当前任务规划中所有任务的内容和状态：

{tasks}

正在执行的任务为：

{in_progress_task}

请查看上述任务进度，确保计划正在正确执行。如果有任务卡住或需要调整，请及时更新
`

	modelSelectionPromptCN = `
## 模型选择策略

当前可用模型：
{model_list}

每个模型 ID 由用户配置，对应一个具体的模型实例及其描述。描述说明了该模型的能力特点和适用场景，
是你选择模型的主要依据。目标是：在保证任务质量的前提下，为每个子任务选择最合适的模型，控制整体 token 成本。

### 选择原则
创建子任务时，阅读每个模型的描述，根据任务复杂度为 selected_model_id 字段选择合适的模型 ID：
- 描述中标注"适合简单任务"、"成本低"、"速度快"等的模型 → 用于翻译、摘要、格式转换等无需深度推理的任务
- 描述中标注"适合复杂任务"、"推理能力强"、"效果好"等的模型 → 用于代码生成、逻辑分析、策略规划等任务
- 不填则使用 Agent 默认模型

### 执行质量保障
若某个子任务执行结果质量不佳（输出不准确、逻辑错误、未达到预期目标），
应通过 todo_modify 工具将该任务的 selected_model_id 修改为描述更强的模型 ID，然后重新执行该任务。
不要在低质量结果上继续推进后续依赖任务。
`

	modelSelectionPromptEN = `
## Model Selection Strategy

Available models:
{model_list}

Each model ID is configured by the user and maps to a specific model instance with a description.
The description explains the model's capability and best-fit scenarios — use it as the primary basis
for selection. The goal is to pick the most appropriate model for each subtask to maintain quality
while keeping overall token cost in check.

### Selection Principles
When creating subtasks, read each model's description and assign an appropriate model ID to selected_model_id:
- Models described as "suitable for simple tasks", "low cost", "fast" → use for translation, summarization,
  format conversion, and other tasks that don't require deep reasoning
- Models described as "suitable for complex tasks", "strong reasoning", "high quality" → use for code
  generation, logical analysis, strategic planning, etc.
- Omit to use the agent's default model

### Quality Assurance
If a subtask produces poor results (inaccurate output, logical errors, unmet objectives),
use todo_modify to update that task's selected_model_id to a model with a stronger description,
then re-execute the task. Do not proceed with downstream tasks that depend on low-quality results.
`

	noModelSelectionPromptCN = `
## 模型选择说明

当前未配置可选模型列表。创建和更新任务时，**不要使用 selected_model_id 字段**。
所有任务将使用 Agent 默认模型执行。
`

	noModelSelectionPromptEN = `
## Model Selection Note

No model selection list is configured. When creating or updating tasks, **do NOT use the selected_model_id field**.
All tasks will be executed using the Agent's default model.
`
)

// BuildTodoSection 构建待办节（Priority 90）。
//
// 返回 *PromptSection，对齐 Python build_todo_section 返回 Optional[PromptSection]。
// 当无内容可注入时返回 nil，调用方应判断 nil 并调用 RemoveSection。
// modelSelection 为预构建的模型列表字符串（可为空）。
// 若非空则追加模型选择提示词，否则追加"无模型选择"提示词。
// ──────────────────────────── 导出函数 ────────────────────────────

func BuildTodoSection(modelSelection string, lang string) *saprompt.PromptSection {
	var content string
	if lang == "en" {
		content = todoSystemPromptEN
	} else {
		content = todoSystemPromptCN
	}

	// 根据 modelSelection 是否存在动态选择提示词
	modelContent := BuildModelSelectionPrompt(modelSelection, lang)
	if modelContent != "" {
		content = content + modelContent
	} else {
		if lang == "en" {
			content = content + noModelSelectionPromptEN
		} else {
			content = content + noModelSelectionPromptCN
		}
	}

	section := saprompt.PromptSection{
		Name:     SectionTodo,
		Content:  map[string]string{lang: content},
		Priority: 90,
	}
	return &section
}

// BuildProgressReminderUserPrompt 构建进度提醒用户提示词
func BuildProgressReminderUserPrompt(tasks string, inProgressTask string, lang string) string {
	var template string
	if lang == "en" {
		template = progressReminderUserPromptEN
	} else {
		template = progressReminderUserPromptCN
	}
	content := strings.ReplaceAll(template, "{tasks}", tasks)
	content = strings.ReplaceAll(content, "{in_progress_task}", inProgressTask)
	return content
}

// BuildModelSelectionPrompt 构建模型选择提示词
func BuildModelSelectionPrompt(modelSelection string, lang string) string {
	if modelSelection == "" {
		return ""
	}
	var template string
	if lang == "en" {
		template = modelSelectionPromptEN
	} else {
		template = modelSelectionPromptCN
	}
	return strings.ReplaceAll(template, "{model_list}", modelSelection)
}
