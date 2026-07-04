package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// sessionsListDescription sessions_list 工具双语描述
var sessionsListDescription = map[string]string{
	"cn": "查看当前所有后台异步子任务(包括运行中、已完成、失败、已取消)及其元数据",
	"en": "List all background async tasks (running, completed, failed, canceled) and its metadata",
}

// sessionsSpawnDescription sessions_spawn 工具双语描述
var sessionsSpawnDescription = map[string]string{
	"cn": "创建异步后台子代理任务，立即返回 pending 状态，任务在后台执行，不阻塞当前对话。\n\n" +
		"可用代理类型及对应工具：\n{available_agents}\n\n" +
		"重要：使用 sessions_spawn 时，必须指定 subagent_type, task_description 参数选择代理类型和描述任务。请勿指定你无权访问的其他代理！！！\n\n" +
		"## 使用场景:\n" +
		"- 任务复杂、多步骤、可独立执行\n" +
		"- 需要并行处理、专注推理、大量上下文 / Token\n" +
		"- 需要沙箱安全执行（代码、搜索、格式化）\n" +
		"- 只需最终输出，不关心中间过程\n" +
		"- 希望在长时间任务执行期间继续处理用户其他问题\n\n" +
		"## 不使用场景:\n" +
		"- 任务简单，可快速完成\n" +
		"- 需要查看中间步骤\n" +
		"- 拆分无收益、仅增加延迟\n" +
		"- 用户明确要求等待结果后再继续\n\n" +
		"## 使用原则:\n" +
		"1. 独立任务尽量并行执行以提升性能\n" +
		"2. 用子代理隔离复杂任务，避免主线程过载\n" +
		"3. 提交后立即告知用户任务已在后台执行\n" +
		"4. 不要因为状态是 pending 就重复调用 sessions_spawn 创建相同任务\n" +
		"5. 仅当用户明确要求重试或变更参数时，才发起新任务\n\n" +
		"## 重要说明:\n" +
		"- 实际任务在后台异步执行\n\n" +
		"## 示例:\n\n" +
		"示例 1：并行独立研究\n" +
		"    用户：研究詹姆斯和科比的成就并对比。\n" +
		"    助手：[并行启动 2 个 sessions_spawn 任务分别研究两位球员]\n" +
		"    助手：[汇总对比结果回复用户]\n" +
		"    说明：研究复杂且各球员相互独立，适合拆分并行执行。\n\n" +
		"示例 2：单任务高上下文隔离\n" +
		"    用户：分析大型代码库的安全漏洞并生成报告。\n" +
		"    助手：[启动单个 sessions_spawn 任务]\n" +
		"    助手：[继续处理用户其他问题]\n" +
		"    说明：用子代理隔离高消耗任务，避免主线程过载。\n\n" +
		"示例 3：简单任务 - 不要使用 sessions_spawn\n" +
		"    用户：读取 config.json 并告诉我版本号。\n" +
		"    助手：[直接调用 read_file 工具，不用 sessions_spawn]\n" +
		"    说明：简单任务，直接执行更快更清晰。",

	"en": "Create async background subagent task that returns pending status immediately \n" +
		"while the task executes in the background without blocking the current conversation.\n\n" +
		"Available agent types and the tools they have access to:\n{available_agents}\n\n" +
		"Important: When using sessions_spawn, \n" +
		"you must specify the subagent_type and task_description parameters to select the agent type and describe the task.\n" +
		"Do not specify agents you do not have access to!!!\n\n" +
		"## When to use:\n" +
		"- Tasks that are complex, multi-step, and can be executed independently\n" +
		"- Scenarios requiring parallel processing, focused reasoning, or large context/token usage\n" +
		"- Tasks that require sandboxed execution (e.g., code execution, search, formatting)\n" +
		"- When only the final output is needed and intermediate steps are not required\n" +
		"- When you want to continue handling user queries while a long-running task executes\n\n" +
		"## When NOT to use:\n" +
		"- Tasks are simple and can be completed quickly\n" +
		"- Intermediate steps need to be observed\n" +
		"- Task decomposition provides no benefit and only adds latency\n" +
		"- User explicitly wants to wait for results before continuing\n\n" +
		"## Usage Guidelines:\n" +
		"1. Execute independent tasks in parallel whenever possible to improve performance\n" +
		"2. Use sub-agents to isolate complex tasks and avoid main thread overload\n" +
		"3. After spawning, immediately inform user the task is running in background\n" +
		"4. Use sessions_list to check task status and retrieve results\n" +
		"5. Do NOT repeatedly call sessions_spawn for the same task just because status is pending\n" +
		"6. Only create new task when user explicitly requests retry or changes parameters\n\n" +
		"## Important Notes:\n" +
		"- Actual task execution happens asynchronously in background\n" +
		"- Use sessions_list with task_id to check completion status and get results\n\n" +
		"## Examples:\n\n" +
		"Example 1: Parallel independent research\n" +
		"    User: Research achievements of LeBron James and Kobe Bryant, then compare them.\n" +
		"    Assistant: [Spawns 2 parallel sessions_spawn tasks for each player]\n" +
		"    Assistant: [Waits and uses sessions_list to collect results]\n" +
		"    Assistant: [Summarizes comparison for user]\n" +
		"    Reasoning: Complex research tasks that are independent, suitable for parallel execution.\n\n" +
		"Example 2: High-context isolation\n" +
		"    User: Analyze security vulnerabilities in this large codebase and generate a report.\n" +
		"    Assistant: [Spawns single sessions_spawn task]\n" +
		"    Assistant: [Continues handling other user queries]\n" +
		"    Assistant: [Later retrieves report via sessions_list]\n" +
		"    Reasoning: Isolate high-consumption task to avoid main thread overload.\n\n" +
		"Example 3: Simple task - DO NOT use sessions_spawn\n" +
		"    User: Read file config.json and tell me the version.\n" +
		"    Assistant: [Directly calls read_file tool, no sessions_spawn]\n" +
		"    Reasoning: Simple task, direct execution is faster and clearer.",
}

// sessionsCancelDescription sessions_cancel 工具双语描述
var sessionsCancelDescription = map[string]string{
	"cn": "取消后台异步子任务。此操作会同步阻塞直到任务取消完成。",
	"en": "Cancel background async task. This operation blocks synchronously until cancellation completes.",
}

// ──────────────────────────── 结构体 ────────────────────────────

// SessionsListMetadataProvider sessions_list 工具元数据提供者
type SessionsListMetadataProvider struct{}

// SessionsSpawnMetadataProvider sessions_spawn 工具元数据提供者
type SessionsSpawnMetadataProvider struct{}

// SessionsCancelMetadataProvider sessions_cancel 工具元数据提供者
type SessionsCancelMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSessionsListMetadataProviderInputParams 构建 sessions_list 工具的参数 Schema
func GetSessionsListMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	_ = lang // 无参数工具
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
}

// GetSessionsSpawnMetadataProviderInputParams 构建 sessions_spawn 工具的参数 Schema
func GetSessionsSpawnMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"subagent_type":    {"cn": "子 agent 类型(如 'general-purpose')", "en": "Subagent type (e.g., 'general-purpose')"},
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

// GetSessionsCancelMetadataProviderInputParams 构建 sessions_cancel 工具的参数 Schema
func GetSessionsCancelMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"task_id": {"cn": "要取消的任务 ID（从 sessions_list 获取）", "en": "Task ID to cancel (obtained from sessions_list)"},
	}
	d := func(key string) string { if v, ok := p[key][lang]; ok { return v }; return p[key]["cn"] }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string", "description": d("task_id")},
		},
		"required": []any{"task_id"},
	}
}

func (p *SessionsListMetadataProvider) GetName() string { return "sessions_list" }
func (p *SessionsListMetadataProvider) GetDescription(language string) string {
	if d, ok := sessionsListDescription[language]; ok { return d }
	return sessionsListDescription["cn"]
}
func (p *SessionsListMetadataProvider) GetInputParams(language string) map[string]any {
	return GetSessionsListMetadataProviderInputParams(language)
}

func (p *SessionsSpawnMetadataProvider) GetName() string { return "sessions_spawn" }
func (p *SessionsSpawnMetadataProvider) GetDescription(language string) string {
	if d, ok := sessionsSpawnDescription[language]; ok { return d }
	return sessionsSpawnDescription["cn"]
}
func (p *SessionsSpawnMetadataProvider) GetInputParams(language string) map[string]any {
	return GetSessionsSpawnMetadataProviderInputParams(language)
}

func (p *SessionsCancelMetadataProvider) GetName() string { return "sessions_cancel" }
func (p *SessionsCancelMetadataProvider) GetDescription(language string) string {
	if d, ok := sessionsCancelDescription[language]; ok { return d }
	return sessionsCancelDescription["cn"]
}
func (p *SessionsCancelMetadataProvider) GetInputParams(language string) map[string]any {
	return GetSessionsCancelMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&SessionsListMetadataProvider{})
	RegisterToolProvider(&SessionsSpawnMetadataProvider{})
	RegisterToolProvider(&SessionsCancelMetadataProvider{})
}
