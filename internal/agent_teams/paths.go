package agent_teams

import (
	"os"
	"path/filepath"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

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
// 对齐 Python: configure_openjiuwen_home(path)
func ConfigureHome(path string) {
	configuredHome = path
}

// ResetHome 清除运行时 home 覆盖，恢复默认布局。
// 对齐 Python: reset_openjiuwen_home()
func ResetHome() {
	configuredHome = ""
}

// GetHome 返回 openjiuwen 本地状态的根目录。
// 对齐 Python: get_openjiuwen_home()
func GetHome() string {
	if configuredHome != "" {
		return configuredHome
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".openjiuwen")
}

// GetAgentTeamsHome 返回 agent_teams 状态的根目录。
// 对齐 Python: get_agent_teams_home()
func GetAgentTeamsHome() string {
	return filepath.Join(GetHome(), agentTeamsSubDir)
}

// TeamHome 返回每个团队的根目录。
// 对齐 Python: team_home(team_name)
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

// ──────────────────────────── 非导出函数 ────────────────────────────
