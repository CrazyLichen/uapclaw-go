package agent_teams

import "testing"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestDefaultTeamMemoryDir(t *testing.T) {
	result := DefaultTeamMemoryDir("test-team")
	expected := TeamHome("test-team") + "/team-workspace/team-memory"
	if result != expected {
		t.Errorf("DefaultTeamMemoryDir = %q, expected %q", result, expected)
	}
}

func TestDefaultTeamMemoryDir_空团队名(t *testing.T) {
	result := DefaultTeamMemoryDir("")
	if result == "" {
		t.Error("期望非空路径")
	}
}

// TestConfigureHome 测试 ConfigureHome 和 ResetHome
func TestConfigureHome(t *testing.T) {
	// 保存原始值
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/custom/home")
	if configuredHome != "/custom/home" {
		t.Errorf("configuredHome = %q, want %q", configuredHome, "/custom/home")
	}

	ResetHome()
	if configuredHome != "" {
		t.Errorf("ResetHome 后 configuredHome = %q, want %q", configuredHome, "")
	}
}

// TestGetHome_自定义Home 测试自定义 Home 目录
func TestGetHome_自定义Home(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/custom/home")
	home := GetHome()
	if home != "/custom/home" {
		t.Errorf("GetHome() = %q, want %q", home, "/custom/home")
	}
}

// TestGetAgentTeamsHome 测试 agent_teams 根目录
func TestGetAgentTeamsHome(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/custom/home")
	ath := GetAgentTeamsHome()
	if ath != "/custom/home/.agent_teams" {
		t.Errorf("GetAgentTeamsHome() = %q, want %q", ath, "/custom/home/.agent_teams")
	}
}

// TestTeamHome 测试团队根目录
func TestTeamHome(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/custom/home")
	th := TeamHome("my-team")
	if th != "/custom/home/.agent_teams/my-team" {
		t.Errorf("TeamHome() = %q, want %q", th, "/custom/home/.agent_teams/my-team")
	}
}
