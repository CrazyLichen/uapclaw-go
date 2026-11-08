package prompts

import (
	"strings"

	harnesssections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTeamPlanModePrompt 返回指定语言的 team.plan 提示词模板。
// 模板缺失时返回 error（不在包级 init 中静默吞掉）。
// Python: get_team_plan_mode_prompt(language) (openjiuwen/agent_teams/prompts/team_plan_mode.py)
func GetTeamPlanModePrompt(language string) (string, error) {
	tpl, err := LoadTemplate("team_plan_mode", language)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(tpl.Content()), nil
}

// BuildTeamPlanModePrompt 渲染 team.plan 提示词模板。
//
// Go 侧与上游同样**不做占位符替换**：team_plan_mode.md 用的是单花括号
// {enter_plan_mode_status} / {plan_file_info}，而上游 PromptTemplate.format
// 走 PromptAssembler，其占位符正则是 `\{\{([^{}]*?)\}\}`（TextableVariable），
// 匹配不到单花括号——所以两个占位符在 Python 侧一直原样留在文本里。
// Go 侧保持这一事实，不"修正"成替换（规则 9：行为等价，不自行改良）。
//
// 两个入参保留：上游 build_team_plan_mode_prompt 同样收这两个参数并传给
// format，只是 format 匹配不到、不生效。签名保形是为了逐符号对照。
//
// Python: build_team_plan_mode_prompt (prompts/team_plan_mode.py L80-92)
func BuildTeamPlanModePrompt(language string, enterPlanModeStatus string, planFileInfo string) (string, error) {
	_, _ = enterPlanModeStatus, planFileInfo // 上游 format 匹配不到单花括号，等价于未使用
	return GetTeamPlanModePrompt(language)
}

// BuildTeamPlanModeSection 构建 team.plan MODE_INSTRUCTIONS PromptSection。
// Python: build_team_plan_mode_section(language, agent, session) (openjiuwen/agent_teams/prompts/team_plan_mode.py)
//
// 将 BuildTeamPlanModePrompt 的结果包装为 PromptSection，
// 其中 name 取 SectionModeInstructions、priority 取 85。
// enterPlanModeStatus 和 planFileInfo 由调用方提前计算后传入。
func BuildTeamPlanModeSection(language string, enterPlanModeStatus string, planFileInfo string) (*saprompt.PromptSection, error) {
	content, err := BuildTeamPlanModePrompt(language, enterPlanModeStatus, planFileInfo)
	if err != nil {
		return nil, err
	}
	section := saprompt.NewPromptSection(
		harnesssections.SectionModeInstructions,
		map[string]string{language: content},
		85,
	)
	return &section, nil
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
