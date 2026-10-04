package prompts

import (
	"strings"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// policyLabels 双语标签字典，用于 BuildSystemPrompt 组装。
// Python: _I18N_LABELS (openjiuwen/agent_teams/prompts/policy.py)
var policyLabels = map[string]map[string]string{
	"cn": {
		"persona":               "当前人设",
		"member_name_label":     "你的成员名（member_name）",
		"team_info_heading":     "团队信息",
		"team_name_label":       "团队名（team_name）",
		"display_name_label":    "显示名（display_name）",
		"team_desc":             "团队目标与指令",
		"relationships_heading": "成员关系",
	},
	"en": {
		"persona":               "Current Persona",
		"member_name_label":     "Your member_name",
		"team_info_heading":     "Team Info",
		"team_name_label":       "team_name",
		"display_name_label":    "display_name",
		"team_desc":             "Team Goal & Directives",
		"relationships_heading": "Relationships",
	},
}

// workflowTemplates 工作流模板名映射。
// Python: _WORKFLOW_TEMPLATES (openjiuwen/agent_teams/prompts/policy.py)
var workflowTemplates = map[string]string{
	"default":    "leader_workflow",
	"predefined": "leader_workflow_predefined",
	"hybrid":     "leader_workflow_hybrid",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RolePolicy 返回指定角色的策略文本。
// Python: role_policy(role: TeamRole, language) (openjiuwen/agent_teams/prompts/policy.py)
func RolePolicy(role atschema.TeamRole, language string) string {
	policyName := "leader_policy"
	if role != atschema.TeamRoleLeader {
		policyName = "teammate_policy"
	}
	tpl := LoadTemplate(policyName, language)
	if tpl == nil {
		return ""
	}
	return tpl.Content()
}

// BuildSystemPrompt 组装团队角色的完整系统提示词。
// Python: build_system_prompt() (openjiuwen/agent_teams/prompts/policy.py)
//
// 使用 system_prompt.md 模板通过双花括号 {{var}} 渲染。
// 结果作为 DeepAgentSpec.build() 的 system_prompt 参数，
// 放入 SystemPromptBuilder 的 IDENTITY 节。
//
// 参数：
//   - memberName: 当前成员名，空时不显示
//   - role: 角色类型，"leader" / "teammate" / "human_agent"
//   - language: "cn" 或 "en"
//   - persona: 人设描述
//   - lifecycle: "temporary" 或 "persistent"
//   - teamMode: 团队模式，"default" / "predefined" / "hybrid"
//   - teamInfo: 团队元数据，nil 时省略
//   - teamMembers: 成员列表，nil 时省略
//   - basePrompt: 用户自定义额外指令，空时省略
func BuildSystemPrompt(
	memberName string,
	role string,
	language string,
	persona string,
	lifecycle string,
	teamMode string,
	teamInfo *TeamInfo,
	teamMembers []TeamMember,
	basePrompt string,
) string {
	lbl := policyLabelsFor(language)

	// 成员名段落
	memberNameSection := ""
	if memberName != "" {
		memberNameSection = lbl["member_name_label"] + ": " + memberName + "\n"
	}

	// 角色策略
	policyName := "leader_policy"
	if role != "leader" {
		policyName = "teammate_policy"
	}
	rolePolicyText := LoadTemplate(policyName, language).Content()

	// workflow_section（仅 LEADER）
	workflowSection := ""
	if role == "leader" {
		workflowName := "leader_workflow"
		if mapped, ok := workflowTemplates[teamMode]; ok {
			workflowName = mapped
		}
		workflowSection = LoadTemplate(workflowName, language).Content()
	}

	// lifecycle_section（仅 LEADER）
	lifecycleSection := ""
	if role == "leader" {
		lcName := "lifecycle_temporary"
		if lifecycle == "persistent" {
			lcName = "lifecycle_persistent"
		}
		lifecycleSection = LoadTemplate(lcName, language).Content()
	}

	// 人设标签 + 人设
	personaLabel := lbl["persona"]

	// 团队信息段落
	teamInfoSection := ""
	if teamInfo != nil {
		teamInfoSection = formatTeamInfo(teamInfo, lbl)
	}

	// 团队成员段落
	teamMembersSection := ""
	if len(teamMembers) > 0 {
		teamMembersSection = formatTeamMembers(teamMembers, lbl, memberName)
	}

	// 基础提示词段落
	basePromptSection := ""
	if basePrompt != "" {
		basePromptSection = "\n" + basePrompt
	}

	// 渲染 system_prompt.md 模板
	template := LoadSharedTemplate("system_prompt")
	if template == nil {
		return ""
	}
	return template.Render(map[string]string{
		"member_name_section":  memberNameSection,
		"role_policy":          rolePolicyText,
		"workflow_section":     workflowSection,
		"lifecycle_section":    lifecycleSection,
		"persona_label":        personaLabel,
		"persona":              persona,
		"team_info_section":    teamInfoSection,
		"team_members_section": teamMembersSection,
		"base_prompt_section":  basePromptSection,
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// policyLabelsFor 返回指定语言的 policy 标签字典，默认回退到中文。
// Python: _I18N_LABELS.get(language, _I18N_LABELS["cn"])
func policyLabelsFor(language string) map[string]string {
	if lbl, ok := policyLabels[language]; ok {
		return lbl
	}
	return policyLabels["cn"]
}

// formatTeamInfo 格式化团队信息为提示词段落。
// Python: _format_team_info(team_info, labels) (openjiuwen/agent_teams/prompts/policy.py)
func formatTeamInfo(teamInfo *TeamInfo, lbl map[string]string) string {
	lines := []string{"\n## " + lbl["team_info_heading"]}
	if teamInfo.TeamName != "" {
		lines = append(lines, "- "+lbl["team_name_label"]+": "+teamInfo.TeamName)
	}
	if teamInfo.DisplayName != "" {
		lines = append(lines, "- "+lbl["display_name_label"]+": "+teamInfo.DisplayName)
	}
	if teamInfo.Description != "" {
		lines = append(lines, "- "+lbl["team_desc"]+": "+teamInfo.Description)
	}
	return strings.Join(lines, "\n")
}

// formatTeamMembers 格式化成员关系为提示词段落。
// Python: _format_team_members(team_members, labels, self_member_name) (openjiuwen/agent_teams/prompts/policy.py)
func formatTeamMembers(teamMembers []TeamMember, lbl map[string]string, selfMemberName string) string {
	lines := []string{"\n## " + lbl["relationships_heading"]}
	for _, m := range teamMembers {
		if m.MemberName == selfMemberName {
			continue
		}
		displayName := m.DisplayName
		if displayName == "" {
			displayName = "unknown"
		}
		line := "- member_name=" + m.MemberName + " display_name=" + displayName
		if m.Description != "" {
			line += " :: " + m.Description
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
