package sections

import (
	"fmt"
	"strings"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	skillRailListSkillSystemPromptCN = `
你是一个技能选择器。
你的任务是为给定的用户任务选择最相关的技能。
仅返回一个 JSON 对象。
输出格式：
{
  "skills": ["skill_name_1", "skill_name_2"]
}
`

	skillRailListSkillSystemPromptEN = `
You are a list_skill selector.
Your task is to select the most relevant skills for the given user task.
Return a JSON object only.
Output format:
{
  "skills": ["skill_name_1", "skill_name_2"]
}
`

	skillRailAllModeHeaderCN = "# 技能\n\n" +
		"执行前先用 read_file 阅读相关 SKILL.md。\n\n" +
		"可用技能：\n"

	skillRailAllModeHeaderEN = "# Skills\n\n" +
		"Read the relevant SKILL.md using read_file before execution.\n\n" +
		"Available skills:\n"

	skillRailAllModeInstructionCN = "\n选择最相关的技能，先阅读其 SKILL.md 再执行。"

	skillRailAllModeInstructionEN = "\nSelect the most relevant skill by reading its SKILL.md first."

	skillRailAutoListModePromptCN = `# 技能

需要时先调用 list_skill 查看可用技能，再用 read_file 读取相关 SKILL.md 后执行。
需要时使用 code 执行 Python 或 JavaScript，使用 bash 执行 shell 命令。
`

	skillRailAutoListModePromptEN = `# Skills

When needed, call list_skill first to see available skills, then read the relevant SKILL.md with read_file before execution.
Use code for Python or JavaScript snippets when needed, and use bash for shell commands.
`

	skillRailNoSkillPromptCN = `# 技能

当前任务没有选择任何技能。如有技能信息可用，请用 read_file 阅读相关 SKILL.md。
`

	skillRailNoSkillPromptEN = `# Skills

No skill was selected for this task. When skill information is available, read the relevant SKILL.md using read_file.
`
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildSkillLine 生成单行技能描述行。
// 对齐 Python: build_skill_line(index, skill_name, description)
func BuildSkillLine(index int, skillName, description string) string {
	return fmt.Sprintf("%d. %s: %s", index, skillName, description)
}

// BuildSkillLines 拼接多行技能描述行（用 "\n\n" 分隔）。
// 对齐 Python: build_skill_lines(lines)
func BuildSkillLines(lines []string) string {
	var filtered []string
	for _, line := range lines {
		if line != "" {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n\n")
}

// BuildAllModeSkillPrompt 构建 all 模式技能提示词。
// 对齐 Python: build_all_mode_skill_prompt(skill_lines, language)
func BuildAllModeSkillPrompt(skillLines, lang string) string {
	text := strings.TrimSpace(skillLines)
	if text == "" {
		if lang == "en" {
			return skillRailNoSkillPromptEN
		}
		return skillRailNoSkillPromptCN
	}
	if lang == "en" {
		return skillRailAllModeHeaderEN + text + skillRailAllModeInstructionEN
	}
	return skillRailAllModeHeaderCN + text + skillRailAllModeInstructionCN
}

// BuildAutoListModeSkillPrompt 构建 auto_list 模式技能提示词。
// 对齐 Python: build_auto_list_mode_skill_prompt(language)
func BuildAutoListModeSkillPrompt(lang string) string {
	if lang == "en" {
		return skillRailAutoListModePromptEN
	}
	return skillRailAutoListModePromptCN
}

// BuildSkillsSection 构建技能节。
// 对齐 Python: build_skills_section(skill_lines, language, mode)
func BuildSkillsSection(mode string, skillLines string, lang string) saprompt.PromptSection {
	var content string

	switch mode {
	case "all":
		content = BuildAllModeSkillPrompt(skillLines, lang)
	case "auto_list":
		content = BuildAutoListModeSkillPrompt(lang)
	default:
		if lang == "en" {
			content = skillRailNoSkillPromptEN
		} else {
			content = skillRailNoSkillPromptCN
		}
	}

	return saprompt.PromptSection{
		Name:     SectionSkills,
		Content:  map[string]string{lang: content},
		Priority: 40,
	}
}

// GetListSkillSystemPrompt 获取技能选择系统提示词
func GetListSkillSystemPrompt(lang string) string {
	if lang == "en" {
		return skillRailListSkillSystemPromptEN
	}
	return skillRailListSkillSystemPromptCN
}

// ──────────────────────────── 非导出函数 ────────────────────────────
