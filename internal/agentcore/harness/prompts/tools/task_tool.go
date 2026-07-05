package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// GeneralPurposeAgentDesc 通用型子代理描述双语常量
var GeneralPurposeAgentDesc = map[string]string{
	"cn": "通用型子代理，继承主代理的工具与能力（文件读写、bash、MCP、skills 等），" +
		"适合执行独立的复杂子任务（调研、搜代码、多步实现等）。" +
		"子代理运行在独立上下文窗口中，中间工具调用结果不会污染主代理上下文。" +
		"当你在搜索关键词或文件时，如果不确定前几次尝试就能找到正确匹配，" +
		"就用这个子代理来帮你搜索。",
	"en": "General-purpose subagent that inherits the parent agent's tools and " +
		"capabilities (file I/O, bash, MCP, skills, etc.) for independent complex " +
		"subtasks such as research, code search, and multi-step implementation. " +
		"Runs in an isolated context window so intermediate tool results do not " +
		"pollute the parent agent's context. When you are searching for a keyword " +
		"or file and are not confident you will find the right match in the first " +
		"few tries, use this subagent to perform the search for you.",
}

// ──────────────────────────── 全局变量 ────────────────────────────

// taskToolDescription task_tool 工具双语描述
var taskToolDescription = map[string]string{
	"cn": "启动新的子代理，自主处理复杂、多步骤任务。\n\n" +
		"task_tool 启动专门的子代理来自主处理复杂任务。每种子代理类型都有特定的能力和可用工具。\n\n" +
		"可用代理类型及其工具：\n\n" +
		"{available_agents}\n\n" +
		"使用 task_tool 时，请通过 subagent_type 参数选择要使用的子代理类型。如果不指定，将使用通用子代理。\n\n" +
		"何时不使用 task_tool：\n\n" +
		"- 如果你想读取某个具体文件路径，直接用 read_file 或 glob，比用 task_tool 更快\n" +
		"- 如果你在查找某个具体的类定义（如 \"class Foo\"），直接用 grep 或 glob，比用 task_tool 更快\n" +
		"- 如果你在 2-3 个特定文件内搜索代码，直接用 read_file，比用 task_tool 更快\n" +
		"- 其他与上述代理描述无关的任务\n\n" +
		"使用注意事项：\n\n" +
		"- task_description 应包含完整的上下文信息——子代理没有本次对话的任何记忆\n" +
		"- 子代理完成后会返回一条消息给你。该结果对用户不可见。如需向用户展示结果，你应发送一条文字消息，简明总结子代理的结果。\n" +
		"- 每次 task_tool 调用都是全新启动——请提供完整的任务描述。\n" +
		"- 子代理的输出通常应当被信任。\n" +
		"- 明确告知子代理你期望它写代码还是仅做调研（搜索、读文件、抓取网页等），因为它不知道用户的意图。\n" +
		"- 如果子代理的描述中提到应主动使用它，你应尽量在用户没有明确要求时就使用它。自行判断。\n" +
		"- 如果用户明确要求\"并行\"运行子代理，你必须在同一条消息中发出多个 task_tool 调用。" +
		"例如，如果你需要同时启动 build-validator 子代理和 test-runner 子代理，请在同一条消息中发出两个 tool 调用。\n\n" +
		"## 如何写好任务描述\n\n" +
		"像给一位刚走进房间的聪明同事做简报一样描述任务——子代理没看过本次对话，不知道你尝试过什么，也不理解这个任务为什么重要。\n" +
		"- 说明你想达成什么目标以及为什么。\n" +
		"- 描述你已经了解到的信息或已排除的可能性。\n" +
		"- 提供足够的问题背景，让子代理能够自主判断，而不是只能机械执行狭窄的指令。\n" +
		"- 如果需要简短回复，请明确说明（\"在 200 字内汇报\"）。\n" +
		"- 查找类任务：直接给出精确命令。调研类任务：给出要回答的问题——规定好的步骤在前提不成立时只会成为累赘。\n\n" +
		"简短的命令式描述会产生浅层、泛泛的结果。\n\n" +
		"**永远不要委托理解。** 不要写\"根据你的发现修复 bug\"或\"根据调研结果实现它\"。这类说法把综合分析推给了子代理，而不是你自己完成。写出的描述应能证明你已经理解了问题：包含文件路径、行号、具体需要修改的内容。",

	"en": "Launch a new subagent to handle complex, multi-step tasks autonomously.\n\n" +
		"The task_tool launches specialized subagents that autonomously handle complex tasks. Each subagent type has specific capabilities and tools available to it.\n\n" +
		"Available subagent types and the tools they have access to:\n\n" +
		"{available_agents}\n\n" +
		"When using the task_tool, specify a subagent_type parameter to select which subagent type to use. If omitted, the general-purpose subagent is used.\n\n" +
		"When NOT to use the task_tool:\n\n" +
		"- If you want to read a specific file path, use read_file or glob instead of the task_tool, to find the match more quickly\n" +
		"- If you are searching for a specific class definition like \"class Foo\", use grep or glob instead, to find the match more quickly\n" +
		"- If you are searching for code within a specific file or set of 2-3 files, use read_file instead of the task_tool, to find the match more quickly\n" +
		"- Other tasks that are not related to the subagent descriptions above\n\n" +
		"Usage notes:\n\n" +
		"- Provide a thorough task_description with full context — the subagent starts with no memory of this conversation\n" +
		"- When the subagent is done, it will return a single message back to you. The result returned by the subagent is not visible to the user. To show the user the result, you should send a text message back to the user with a concise summary of the result.\n" +
		"- Each task_tool invocation starts fresh — provide a complete task description.\n" +
		"- The subagent's outputs should generally be trusted.\n" +
		"- Clearly tell the subagent whether you expect it to write code or just to do research (search, file reads, web fetches, etc.), since it is not aware of the user's intent.\n" +
		"- If the subagent description mentions that it should be used proactively, then you should try your best to use it without the user having to ask for it first. Use your judgement.\n" +
		"- If the user specifies that they want you to run subagents \"in parallel\", you MUST send a single message with multiple task_tool calls. " +
		"For example, if you need to launch both a build-validator subagent and a test-runner subagent in parallel, send a single message with both tool calls.\n\n" +
		"## Writing the prompt\n\n" +
		"Brief the subagent like a smart colleague who just walked into the room — it hasn't seen this conversation, doesn't know what you've tried, doesn't understand why this task matters.\n" +
		"- Explain what you're trying to accomplish and why.\n" +
		"- Describe what you've already learned or ruled out.\n" +
		"- Give enough context about the surrounding problem that the subagent can make judgment calls rather than just following a narrow instruction.\n" +
		"- If you need a short response, say so (\"report in under 200 words\").\n" +
		"- Lookups: hand over the exact command. Investigations: hand over the question — prescribed steps become dead weight when the premise is wrong.\n\n" +
		"Terse command-style prompts produce shallow, generic work.\n\n" +
		"**Never delegate understanding.** Don't write \"based on your findings, fix the bug\" or \"based on the research, implement it.\" Those phrases push synthesis onto the subagent instead of doing it yourself. Write prompts that prove you understood: include file paths, line numbers, what specifically to change.",
}

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

// TaskMetadataProvider task_tool 工具元数据提供者
type TaskMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTaskMetadataProviderInputParams 构建 task_tool 工具的参数 Schema
func GetTaskMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"subagent_type":    {"cn": "子代理类型", "en": "Type of subagent to use"},
		"task_description": {"cn": "任务描述", "en": "Task description"},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subagent_type":    map[string]any{"type": "string", "description": d("subagent_type")},
			"task_description": map[string]any{"type": "string", "description": d("task_description")},
		},
		"required": []any{"subagent_type", "task_description"},
	}
}

func (p *TaskMetadataProvider) GetName() string { return "task_tool" }
func (p *TaskMetadataProvider) GetDescription(language string) string {
	if d, ok := taskToolDescription[language]; ok {
		return d
	}
	return taskToolDescription["cn"]
}
func (p *TaskMetadataProvider) GetInputParams(language string) map[string]any {
	return GetTaskMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&TaskMetadataProvider{}) }
