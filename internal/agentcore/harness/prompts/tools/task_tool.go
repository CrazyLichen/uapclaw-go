package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// taskToolDescription task_tool 工具双语描述
var taskToolDescription = map[string]string{
	"cn": `启动新的子代理，自主处理复杂、多步骤任务。

task_tool 启动专门的子代理来自主处理复杂任务。每种子代理类型都有特定的能力和可用工具。

可用代理类型及其工具：

{available_agents}

使用 task_tool 时，请通过 subagent_type 参数选择要使用的子代理类型。如果不指定，将使用通用子代理。

何时不使用 task_tool：

- 如果你想读取某个具体文件路径，直接用 read_file 或 glob，比用 task_tool 更快
- 如果你在查找某个具体的类定义（如 "class Foo"），直接用 grep 或 glob，比用 task_tool 更快
- 如果你在 2-3 个特定文件内搜索代码，直接用 read_file，比用 task_tool 更快
- 其他与上述代理描述无关的任务

使用注意事项：

- task_description 应包含完整的上下文信息——子代理没有本次对话的任何记忆
- 子代理完成后会返回一条消息给你。该结果对用户不可见。如需向用户展示结果，你应发送一条文字消息，简明总结子代理的结果。
- 每次 task_tool 调用都是全新启动——请提供完整的任务描述。
- 子代理的输出通常应当被信任。
- 明确告知子代理你期望它写代码还是仅做调研（搜索、读文件、抓取网页等），因为它不知道用户的意图。
- 如果子代理的描述中提到应主动使用它，你应尽量在用户没有明确要求时就使用它。自行判断。
- 如果用户明确要求"并行"运行子代理，你必须在同一条消息中发出多个 task_tool 调用。`,
	"en": `Launch a new subagent to handle complex, multi-step tasks autonomously.

The task_tool launches specialized subagents that autonomously handle complex tasks. Each subagent type has specific capabilities and tools available to it.

Available subagent types and the tools they have access to:

{available_agents}

When using the task_tool, specify a subagent_type parameter to select which subagent type to use. If omitted, the general-purpose subagent is used.

When NOT to use the task_tool:

- If you want to read a specific file path, use read_file or glob instead of the task_tool, to find the match more quickly
- If you are searching for a specific class definition like "class Foo", use grep or glob instead, to find the match more quickly
- If you are searching for code within a specific file or set of 2-3 files, use read_file instead of the task_tool, to find the match more quickly
- Other tasks that are not related to the subagent descriptions above

Usage notes:

- Provide a thorough task_description with full context — the subagent starts with no memory of this conversation
- When the subagent is done, it will return a single message back to you. The result returned by the subagent is not visible to the user. To show the user the result, you should send a text message back to the user with a concise summary of the result.
- Each task_tool invocation starts fresh — provide a complete task description.
- The subagent's outputs should generally be trusted.
- Clearly tell the subagent whether you expect it to write code or just to do research (search, file reads, web fetches, etc.), since it is not aware of the user's intent.
- If the subagent description mentions that it should be used proactively, then you should try your best to use it without the user having to ask for it first. Use your judgement.
- If the user specifies that they want you to run subagents "in parallel", you MUST send a single message with multiple task_tool calls.`,
}

// ──────────────────────────── 结构体 ────────────────────────────

// TaskMetadataProvider task_tool 工具元数据提供者
type TaskMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTaskMetadataProviderInputParams 构建 task_tool 工具的参数 Schema
func GetTaskMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"subagent_type":    {"cn": "子代理类型", "en": "Type of subagent to use"},
		"task_description": {"cn": "任务描述", "en": "Task description"},
	}
	d := func(key string) string { if v, ok := p[key][lang]; ok { return v }; return p[key]["cn"] }
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
	if d, ok := taskToolDescription[language]; ok { return d }
	return taskToolDescription["cn"]
}
func (p *TaskMetadataProvider) GetInputParams(language string) map[string]any {
	return GetTaskMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&TaskMetadataProvider{}) }
