package agent_teams

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// ──────────────────────────── 导出函数（25.7 T1 新增段） ────────────────────────────

// SafePathSegment 返回不受信字符串的规范文件系统安全形式。
// Python: paths.py safe_path_segment(value, fallback="_") (:272-274)
func SafePathSegment(value string, fallback string) string {
	return safeSegment(value, fallback)
}

// TeamSessionsDir 返回保存团队全部会话状态目录的父目录。
// Python: paths.py team_sessions_dir(team_name) (:277-282)
//
// 布局：{team_home}/sessions/
func TeamSessionsDir(teamName string) string {
	return filepath.Join(TeamHome(teamName), "sessions")
}

// TeamSessionDir 返回团队下的单会话目录。
// Python: paths.py team_session_dir(team_name, session_id) (:285-296)
//
// 布局：{team_home}/sessions/{session_id}/（session_id 净化为单段路径）。
func TeamSessionDir(teamName string, sessionID string) string {
	return filepath.Join(TeamSessionsDir(teamName), SafePathSegment(sessionID, "_"))
}

// TeamSessionWorktreesDir 返回团队会话拥有的 worktree 根目录。
// Python: paths.py team_session_worktrees_dir(team_name, session_id) (:340-352)
//
// 布局：{team_home}/sessions/{session_id}/worktrees/
func TeamSessionWorktreesDir(teamName string, sessionID string) string {
	return filepath.Join(TeamSessionDir(teamName, sessionID), "worktrees")
}

// ProjectWorktreeHash 返回会话级 worktree 的稳定项目哈希段。
// Python: paths.py project_worktree_hash(project_dir) (:317-329)
//
// 路径必须已存在。团队托管的 worktree 隔离锚定到显式项目目录；
// 该值缺失时调用方不得静默用 cwd 或 workspace 替代。
// 偏差 D-25.7-h：Python realpath→Go Abs+EvalSymlinks 组合（D-15.12-a 同款）。
func ProjectWorktreeHash(projectDir string) (string, error) {
	resolved, err := filepath.Abs(projectDir)
	if err != nil {
		return "", err
	}
	if r, err2 := filepath.EvalSymlinks(resolved); err2 == nil {
		resolved = r
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("project_dir does not exist: %s", projectDir)
	}
	digest := sha256.Sum256([]byte(resolved))
	return hex.EncodeToString(digest[:])[:12], nil
}

// ──────────────────────────── 非导出函数（25.7 T1 新增段） ────────────────────────────

// safeSegment 把不受信字符串净化为单段安全路径。
// Python: paths.py _safe_segment(value, fallback="_") (:249-270)
//
// 把 [A-Za-z0-9_.-] 之外的字符替换为 _ 并剥掉首尾分隔符，结果永不逃逸
// 父目录（无 /、无 ..）。用于来自不受信输入的路径段（session id 等）。
func safeSegment(value string, fallback string) string {
	if fallback == "" {
		fallback = "_"
	}
	normalized := strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range normalized {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '.' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	normalized = strings.Trim(b.String(), "._-")
	runes := []rune(normalized)
	if len(runes) > 96 {
		normalized = string(runes[:96])
	}
	if normalized == "" {
		return fallback
	}
	return normalized
}
