package skills

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillUtil 技能高层门面类，组合 SkillManager + RemoteSkillUtil。
//
// 提供 Agent 级别的技能注册、提示词生成等高层接口。
//
// 对应 Python: SkillUtil
type SkillUtil struct {
	// skillManager 技能管理器
	skillManager *SkillManager
	// remoteSkillUtil 远程技能工具
	remoteSkillUtil *RemoteSkillUtil
}

// ──────────────────────────── 常量 ────────────────────────────

// skillPromptContent 技能提示词模板内容。
//
// 与 Python SKILL_PROMPT_CONTENT 逐字符对齐：
//
//	To help you better complete tasks, the following skill knowledge is equipped:
//	{{skills}}
//	You can use the read_file tool to read the corresponding SKILL.md file to obtain the relevant skill.
//
// 对应 Python: SKILL_PROMPT_CONTENT
const skillPromptContent = `
To help you better complete tasks, the following skill knowledge is equipped:
{{skills}}
You can use the read_file tool to read the corresponding SKILL.md file to obtain the relevant skill.
`

// skillSystemPrefix 技能提示词系统前缀。
//
// 对应 Python: SkillUtil.get_skill_prompt() 中的 system_prompt
const skillSystemPrefix = "You are an agent equipped with various skills to solve problems.\n" +
	"Before attempting any task, read the relevant skill document (SKILL.md) " +
	"using read_file and follow its workflow.\n"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillUtil 创建 SkillUtil 实例。
//
// 对应 Python: SkillUtil.__init__(sys_operation_id)
func NewSkillUtil(sysOperationID string) *SkillUtil {
	return &SkillUtil{
		skillManager:    NewSkillManager(sysOperationID),
		remoteSkillUtil: NewRemoteSkillUtil(sysOperationID),
	}
}

// NewSkillUtilWithProvider 创建使用自定义 FsProvider 的 SkillUtil 实例。
func NewSkillUtilWithProvider(sysOperationID string, provider FsProvider) *SkillUtil {
	return &SkillUtil{
		skillManager:    NewSkillManagerWithProvider(sysOperationID, provider),
		remoteSkillUtil: NewRemoteSkillUtilWithProvider(sysOperationID, provider),
	}
}

// SetSysOperationID 更新系统操作 ID。
//
// 对应 Python: SkillUtil.set_sys_operation_id(sys_operation_id)
func (su *SkillUtil) SetSysOperationID(sysOperationID string) {
	su.skillManager.SetSysOperationID(sysOperationID)
	su.remoteSkillUtil.SetSysOperationID(sysOperationID)
}

// SkillManager 返回内部 SkillManager 实例。
//
// 对应 Python: SkillUtil.skill_manager (property)
func (su *SkillUtil) SkillManager() *SkillManager {
	return su.skillManager
}

// RemoteSkillUtil 返回内部 RemoteSkillUtil 实例。
//
// 对应 Python: SkillUtil.remote_skill_util (property)
func (su *SkillUtil) RemoteSkillUtil() *RemoteSkillUtil {
	return su.remoteSkillUtil
}

// RegisterSkills 注册技能。
//
// 将路径列表委托给 SkillManager.Register()。
//
// 对应 Python: SkillUtil.register_skills(skill_path, agent, session_id)
func (su *SkillUtil) RegisterSkills(skillPaths []string, overwrite bool) error {
	return su.skillManager.Register(skillPaths, overwrite)
}

// RegisterRemoteSkills 从 GitHub 下载并注册远程技能。
//
// 对应 Python: SkillUtil.register_remote_skills(skills_dir, github_tree, token)
func (su *SkillUtil) RegisterRemoteSkills(skillsDir string, tree *GitHubTree, token string) ([]string, error) {
	return su.remoteSkillUtil.UploadSkillFromGitHub(tree, skillsDir, token)
}

// HasSkill 检查是否有已注册技能。
//
// 对应 Python: SkillUtil.has_skill()
func (su *SkillUtil) HasSkill() bool {
	return su.skillManager.Count() > 0
}

// GetSkillPrompt 生成包含所有已注册技能信息的提示词字符串。
//
// 格式：
//
//	system_prompt（固定前缀）
//	+ skill_prompt 模板渲染（包含技能列表）
//
// 对应 Python: SkillUtil.get_skill_prompt()
func (su *SkillUtil) GetSkillPrompt() string {
	skills := su.skillManager.GetAll()
	skillsInfo := make([]string, 0, len(skills))
	for index, skill := range skills {
		skillsInfo = append(skillsInfo, fmt.Sprintf(
			"%d.Skill name: %s; Skill description: %s; Skill directory: %s",
			index, skill.Name, skill.Description, skill.Directory,
		))
	}

	// 使用 PromptTemplate 渲染
	tmpl := prompt.NewPromptTemplate("skill_prompt", skillPromptContent)
	filled, err := tmpl.Format(map[string]any{
		"skills": strings.Join(skillsInfo, "\n"),
	})
	if err != nil {
		// 格式化失败时返回系统前缀 + 技能列表原文
		loggerWarnSkillPrompt(err)
		return skillSystemPrefix + "\n" + strings.Join(skillsInfo, "\n")
	}

	content, ok := filled.Content.(string)
	if !ok {
		return skillSystemPrefix + "\n" + strings.Join(skillsInfo, "\n")
	}

	return skillSystemPrefix + "\n" + content
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loggerWarnSkillPrompt 记录技能提示词格式化失败警告。
func loggerWarnSkillPrompt(err error) {
	logger.Warn(logger.ComponentAgentCore).
		Str("event_type", "skill_prompt_format_error").
		Err(err).
		Msg("技能提示词格式化失败")
}
