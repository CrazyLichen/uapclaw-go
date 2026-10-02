package prompts

import (
	"strings"

	harnesssections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// teamPlanModePromptCN 中文 team.plan 模板（包级初始化）。
// Python: TEAM_PLAN_MODE_PROMPT_CN (openjiuwen/agent_teams/prompts/team_plan_mode.py)
var teamPlanModePromptCN string

// teamPlanModePromptEN 英文 team.plan 模板（包级初始化）。
// Python: TEAM_PLAN_MODE_PROMPT_EN (openjiuwen/agent_teams/prompts/team_plan_mode.py)
var teamPlanModePromptEN string

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTeamPlanModePrompt 返回指定语言的 team.plan 提示词模板。
// Python: get_team_plan_mode_prompt(language) (openjiuwen/agent_teams/prompts/team_plan_mode.py)
func GetTeamPlanModePrompt(language string) string {
	if language == "en" {
		return teamPlanModePromptEN
	}
	return teamPlanModePromptCN
}

// BuildTeamPlanModePrompt 渲染 team.plan 提示词模板。
// Python: build_team_plan_mode_prompt(language, enter_plan_mode_status, plan_file_info)
//
// 使用单花括号 {var} 占位符渲染（team_plan_mode.md 格式）。
func BuildTeamPlanModePrompt(language string, enterPlanModeStatus string, planFileInfo string) string {
	tpl := GetTeamPlanModePrompt(language)
	return strings.ReplaceAll(strings.ReplaceAll(tpl,
		"{enter_plan_mode_status}", enterPlanModeStatus),
		"{plan_file_info}", planFileInfo)
}

// BuildTeamPlanModeSection 构建 team.plan MODE_INSTRUCTIONS PromptSection。
// Python: build_team_plan_mode_section(language, agent, session) (openjiuwen/agent_teams/prompts/team_plan_mode.py)
//
// 将 BuildTeamPlanModePrompt 的结果包装为 PromptSection，
// name=SectionModeInstructions, priority=85。
// enterPlanModeStatus 和 planFileInfo 由调用方提前计算后传入。
func BuildTeamPlanModeSection(language string, enterPlanModeStatus string, planFileInfo string) *saprompt.PromptSection {
	content := BuildTeamPlanModePrompt(language, enterPlanModeStatus, planFileInfo)
	section := saprompt.NewPromptSection(
		harnesssections.SectionModeInstructions,
		map[string]string{language: content},
		85,
	)
	return &section
}

// BuildEnterPlanModeStatusCN 构建 plan_mode 状态文本（中文）。
// Python: _build_enter_plan_mode_status(agent, session, language) 中文分支
//
// planFilePath 非空表示已调用 enter_plan_mode。
func BuildEnterPlanModeStatusCN(planFilePath string) string {
	if planFilePath != "" {
		return "enter_plan_mode 已调用完成。请继续工作流。"
	}
	return "你尚未调用 enter_plan_mode。请立即调用它作为你的第一个操作。"
}

// BuildEnterPlanModeStatusEN 构建 plan_mode 状态文本（英文）。
// Python: _build_enter_plan_mode_status(agent, session, language) 英文分支
//
// planFilePath 非空表示已调用 enter_plan_mode。
func BuildEnterPlanModeStatusEN(planFilePath string) string {
	if planFilePath != "" {
		return "enter_plan_mode has been called. Proceed with the workflow."
	}
	return "You have NOT called enter_plan_mode yet. Call it NOW as your first action."
}

// BuildPlanFileInfoCN 构建 plan 文件信息（中文）。
// Python: _build_plan_file_info(agent, session, language) 中文分支
//
// planFilePath 非空表示 plan 文件路径已确定，exists 表示文件是否已存在。
func BuildPlanFileInfoCN(planFilePath string, exists bool) string {
	if planFilePath == "" {
		return "暂无 plan 文件。请先调用 enter_plan_mode 创建。"
	}
	if exists {
		return "计划文件已存在于 " + planFilePath + "。你可以使用 edit_file 工具读取并增量编辑它。"
	}
	return "计划文件尚不存在。你应该使用 write_file 工具在 " + planFilePath + " 创建计划。"
}

// BuildPlanFileInfoEN 构建 plan 文件信息（英文）。
// Python: _build_plan_file_info(agent, session, language) 英文分支
func BuildPlanFileInfoEN(planFilePath string, exists bool) string {
	if planFilePath == "" {
		return "No plan file yet. Call enter_plan_mode first to create one."
	}
	if exists {
		return "A plan file already exists at " + planFilePath + ". You can read it and make incremental edits using the edit_file tool."
	}
	return "No plan file exists yet. You should create your plan at " + planFilePath + " using the write_file tool."
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	tplCN := LoadTemplate("team_plan_mode", "cn")
	if tplCN != nil {
		teamPlanModePromptCN = strings.TrimSpace(tplCN.Content())
	}
	tplEN := LoadTemplate("team_plan_mode", "en")
	if tplEN != nil {
		teamPlanModePromptEN = strings.TrimSpace(tplEN.Content())
	}
}
