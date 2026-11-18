package agent_teams

import (
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// agentTeamsSubDir openjiuwen home 下的 agent_teams 子目录
	agentTeamsSubDir = ".agent_teams"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// configuredHome 覆盖的运行时 home 目录
	configuredHome string
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ConfigureHome 覆盖运行时 home 目录。
// Python: configure_openjiuwen_home(path)
func ConfigureHome(path string) {
	configuredHome = path
}

// ResetHome 清除运行时 home 覆盖，恢复默认布局。
// Python: reset_openjiuwen_home()
func ResetHome() {
	configuredHome = ""
}

// GetHome 返回 openjiuwen 本地状态的根目录。
// 默认值为数据根 WorkspaceDir()（约定 ~/.uapclaw）：对齐 Python 侧
// jiuwenswarm/agents/harness/team/bootstrap.py 在启动时调用
// configure_openjiuwen_home(get_user_workspace_dir()) 的效果。
// Python: get_openjiuwen_home()
func GetHome() string {
	if configuredHome != "" {
		return configuredHome
	}
	return workspace.WorkspaceDir()
}

// GetAgentTeamsHome 返回 agent_teams 状态的根目录。
// Python: get_agent_teams_home()
func GetAgentTeamsHome() string {
	return filepath.Join(GetHome(), agentTeamsSubDir)
}

// TeamHome 返回每个团队的根目录。
// Python: team_home(team_name)
//
// 布局：
//
//	{GetAgentTeamsHome()}/{team_name}/
//	  team-workspace/         # 默认团队共享工作空间
//	  workspaces/             # stable_base 成员工作空间
//	    {member}_workspace/
//	  team.db                 # 默认 sqlite 数据库
func TeamHome(teamName string) string {
	return filepath.Join(GetAgentTeamsHome(), teamName)
}

// TeamWorkspaceDir 返回团队共享工作空间根目录。
// Python: team_workspace_dir(team_name) (paths.py:168-179)
// 布局：{TeamHome(teamName)}/team-workspace/
func TeamWorkspaceDir(teamName string) string {
	return filepath.Join(TeamHome(teamName), "team-workspace")
}

// DefaultTeamMemoryDir 返回团队共享记忆目录默认路径。
// Python: team_memory_dir(team_name)
// 布局：{TeamHome(teamName)}/team-workspace/team-memory/
func DefaultTeamMemoryDir(teamName string) string {
	return filepath.Join(TeamHome(teamName), "team-workspace", "team-memory")
}

// IndependentMemberWorkspace 返回成员独立工作空间路径。
// Python: independent_member_workspace(member_name)
//
// 布局：{GetHome()}/{member_name}_workspace/
// 当 WorkspaceSpec.StableBase 为 true 时，symlink 从
// {TeamHome(teamName)}/workspaces/{member_name}_workspace/ 指向此处。
func IndependentMemberWorkspace(memberName string) string {
	return filepath.Join(GetHome(), memberName+"_workspace")
}

// ──────────────────────────── 全局变量（skills 目录覆盖） ────────────────────────────

var (
	// configuredGlobalSkillsDir 平台覆盖的全局 Skill 库目录
	// Python: _configured_global_skills_dir (paths.py:22)
	configuredGlobalSkillsDir string
)

// ──────────────────────────── 导出函数（skills 可见性） ────────────────────────────

// SKILL_VISIBILITY_FILENAME 团队/成员 Skill 可见性声明文件名。
// Python: SKILL_VISIBILITY_FILENAME (paths.py:40)
const SKILL_VISIBILITY_FILENAME = "skills-visibility.json"

// ConfigureGlobalSkillsDir 把全局唯一的 Skill 库指到宿主拥有的目录。
// Python: configure_global_skills_dir (paths.py:82-94)
func ConfigureGlobalSkillsDir(path string) {
	configuredGlobalSkillsDir = path
}

// ResetGlobalSkillsDir 清除 Skill 库覆盖，恢复默认布局。
// Python: reset_global_skills_dir (paths.py:97-100)
func ResetGlobalSkillsDir() {
	configuredGlobalSkillsDir = ""
}

// GlobalSkillsDir 返回全体团队成员共享的唯一天然 Skill 库目录。
// 没有按团队/成员的 Skill 目录：可见性是声明文件（见
// MemberSkillVisibilityPath），不是第二份磁盘拷贝。
// Python: global_skills_dir (paths.py:103-117)
func GlobalSkillsDir() string {
	if configuredGlobalSkillsDir != "" {
		return configuredGlobalSkillsDir
	}
	return filepath.Join(GetHome(), "workspace", "skills")
}

// TeamMemberWorkspaceDir 返回团队成员稳定工作空间根目录。
// Python: team_member_workspace_dir (paths.py:182-194)
// 布局：{TeamHome(teamName)}/workspaces/{memberName}_workspace/
func TeamMemberWorkspaceDir(teamName, memberName string) string {
	return filepath.Join(TeamHome(teamName), "workspaces", memberName+"_workspace")
}

// MemberSkillVisibilityPath 返回团队成员的 Skill 可见性声明路径。
// 文件位于工作空间根而非 skills/ 子目录：成员自己不拥有 Skill 目录，
// 只拥有指向 GlobalSkillsDir 的视图。
// Python: member_skill_visibility_path (paths.py:205-224)
func MemberSkillVisibilityPath(teamName, memberName string) string {
	return filepath.Join(TeamMemberWorkspaceDir(teamName, memberName), SKILL_VISIBILITY_FILENAME)
}

// TeamSkillVisibilityPath 返回团队级 Skill 可见性声明路径。
// Python: team_skill_visibility_path (paths.py:227-241)
// 布局：{TeamHome(teamName)}/team-workspace/skills-visibility.json
func TeamSkillVisibilityPath(teamName string) string {
	return filepath.Join(TeamWorkspaceDir(teamName), SKILL_VISIBILITY_FILENAME)
}
