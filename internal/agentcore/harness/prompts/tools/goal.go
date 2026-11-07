package tools

// 本文件对应 Python openjiuwen/harness/prompts/tools/goal.py，
// 提供 submit_goal_report / get_current_goal 两个工具的双语描述与参数 schema。

// ──────────────────────────── 结构体 ────────────────────────────

// SubmitGoalReportMetadataProvider submit_goal_report 工具元数据提供者。
type SubmitGoalReportMetadataProvider struct{}

// GetCurrentGoalMetadataProvider get_current_goal 工具元数据提供者。
type GetCurrentGoalMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// submitGoalReportDescription submit_goal_report 双语描述。
// Python: SUBMIT_GOAL_REPORT_DESCRIPTION_CN/EN (prompts/tools/goal.py L13-35)
var submitGoalReportDescription = map[string]string{
	"cn": "仅当本次用户消息中包含 <goal_task> 标签（即处于 Goal 执行轮）时才调用本工具；" +
		"普通对话轮（无 <goal_task>）绝不要调用本工具。" +
		"本工具用于提交当前 goal 尝试的结构化结果。" +
		"这是本次 goal 尝试的最终工具动作；工具调用成功后不要再调用其他工具，" +
		"已有进展但目标仍可继续推进时使用 continue；" +
		"只有具备可审计完成证据时才使用 complete；" +
		"只有缺少用户输入、权限、依赖、外部服务或环境状态" +
		"且无法继续任何有意义推进时才使用 blocked。",
	"en": "Only call this tool when the current user message contains a <goal_task> tag " +
		"(i.e. this is a Goal execution round); never call it on a normal turn " +
		"without <goal_task>. " +
		"Submit a structured result for the current goal attempt. " +
		"This is the final tool action for the current goal attempt; after the tool " +
		"call succeeds, do not call any other tools. " +
		"Use continue when progress has been made but the objective can still be advanced; " +
		"use complete only with auditable completion evidence; " +
		"use blocked only when user input, permissions, dependencies, external services, " +
		"or environment state prevent any meaningful progress.",
}

// submitGoalReportParams submit_goal_report 参数双语描述。
// Python: _PARAMS (prompts/tools/goal.py L45-78)
var submitGoalReportParams = map[string]map[string]string{
	"status": {
		"cn": "当前 goal 尝试结果：continue、complete 或 blocked。" +
			"目标尚未完成但仍可推进时，不要报告 blocked。",
		"en": "Current goal attempt result: continue, complete, or blocked. " +
			"Do not report blocked when the objective is incomplete but can still be advanced.",
	},
	"evidence": {
		"cn": "支持该状态的可审计证据，例如测试输出、命令结果、文件路径、" +
			"工具结果、完成证明，或确切阻塞原因。",
		"en": "Auditable evidence supporting this status, e.g. test output, " +
			"command results, file paths, tool results, proof of completion, " +
			"or exact blocking reason.",
	},
	"remaining_work": {
		"cn": "目标未完成时说明剩余工作；完成时可以为空。",
		"en": "Remaining work when the objective is not yet met; may be empty when complete.",
	},
	"next_instruction": {
		"cn": "status=continue 时给出下一次尝试的具体、可执行指令；其他状态可以为空。",
		"en": "Specific, actionable instruction for the next attempt when status=continue; " +
			"may be empty for other statuses.",
	},
}

// getCurrentGoalDescription get_current_goal 双语描述。
// Python: GET_CURRENT_GOAL_DESCRIPTION (prompts/tools/goal.py L130-142)
var getCurrentGoalDescription = map[string]string{
	"cn": "查询当前会话的持续目标（goal）。" +
		"当你不确定当前 goal 任务是什么、或用户提到「目标 / 继续目标 / 那个任务」时调用。" +
		"返回 objective、status、attempt_count 与上一次评估（last_assessment）。",
	"en": "Query the current session's persistent goal. " +
		"Call this when you are unsure what the goal is, or when the user refers to " +
		"\"the goal / continue the goal / that task\". " +
		"Returns objective, status, attempt_count, and the last assessment.",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSubmitGoalReportInputParams 构建 submit_goal_report 工具的参数 Schema。
// Python: get_submit_goal_report_input_params (prompts/tools/goal.py L81)
func GetSubmitGoalReportInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	desc := func(key string) string {
		if d, ok := submitGoalReportParams[key][lang]; ok {
			return d
		}
		return submitGoalReportParams[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"type":        "string",
				"enum":        []any{"continue", "complete", "blocked"},
				"description": desc("status"),
			},
			"evidence": map[string]any{
				"type":        "string",
				"description": desc("evidence"),
			},
			"remaining_work": map[string]any{
				"type":        "string",
				"description": desc("remaining_work"),
			},
			"next_instruction": map[string]any{
				"type":        "string",
				"description": desc("next_instruction"),
			},
		},
		"required": []any{"status", "evidence"},
	}
}

// GetCurrentGoalInputParams 构建 get_current_goal 工具的参数 Schema（空对象）。
// Python: GetCurrentGoalMetadataProvider.get_input_params (prompts/tools/goal.py L156)
func GetCurrentGoalInputParams(language string) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
}

// GetName 返回工具注册名。
func (p *SubmitGoalReportMetadataProvider) GetName() string { return "submit_goal_report" }

// GetDescription 返回指定语言描述。
func (p *SubmitGoalReportMetadataProvider) GetDescription(language string) string {
	if d, ok := submitGoalReportDescription[language]; ok {
		return d
	}
	return submitGoalReportDescription["cn"]
}

// GetInputParams 返回参数 schema。
func (p *SubmitGoalReportMetadataProvider) GetInputParams(language string) map[string]any {
	return GetSubmitGoalReportInputParams(language)
}

// GetName 返回工具注册名。
func (p *GetCurrentGoalMetadataProvider) GetName() string { return "get_current_goal" }

// GetDescription 返回指定语言描述。
func (p *GetCurrentGoalMetadataProvider) GetDescription(language string) string {
	if d, ok := getCurrentGoalDescription[language]; ok {
		return d
	}
	return getCurrentGoalDescription["cn"]
}

// GetInputParams 返回参数 schema。
func (p *GetCurrentGoalMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCurrentGoalInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&SubmitGoalReportMetadataProvider{})
	RegisterToolProvider(&GetCurrentGoalMetadataProvider{})
}
