package agent_teams

import (
	"path/filepath"
	"testing"
)

// TestConfigureHome_设置自定义路径 测试 ConfigureHome 覆盖 home 目录
func TestConfigureHome_设置自定义路径(t *testing.T) {
	// 保存原始值
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/tmp/custom_home")
	if configuredHome != "/tmp/custom_home" {
		t.Errorf("configuredHome = %q, want /tmp/custom_home", configuredHome)
	}
}

// TestResetHome_清除覆盖 测试 ResetHome 恢复默认
func TestResetHome_清除覆盖(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/tmp/some_path")
	ResetHome()
	if configuredHome != "" {
		t.Errorf("configuredHome = %q, want empty", configuredHome)
	}
}

// TestGetHome_自定义路径 测试 GetHome 返回覆盖路径
func TestGetHome_自定义路径(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/tmp/my_home")
	home := GetHome()
	if home != "/tmp/my_home" {
		t.Errorf("GetHome() = %q, want /tmp/my_home", home)
	}
}

// TestGetHome_默认路径 测试 GetHome 未覆盖时返回默认路径
func TestGetHome_默认路径(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ResetHome()
	home := GetHome()
	if home == "" {
		t.Error("GetHome() 返回空字符串")
	}
	// 默认路径应包含 .openjiuwen
	if filepath.Base(home) != ".openjiuwen" {
		t.Errorf("GetHome() base = %q, want .openjiuwen", filepath.Base(home))
	}
}

// TestGetAgentTeamsHome_路径 测试 GetAgentTeamsHome 返回 agent_teams 子目录
func TestGetAgentTeamsHome_路径(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/tmp/test_home")
	ath := GetAgentTeamsHome()
	want := filepath.Join("/tmp/test_home", agentTeamsSubDir)
	if ath != want {
		t.Errorf("GetAgentTeamsHome() = %q, want %q", ath, want)
	}
}

// TestTeamHome_路径 测试 TeamHome 返回团队目录
func TestTeamHome_路径(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/tmp/test_home")
	th := TeamHome("my_team")
	want := filepath.Join("/tmp/test_home", agentTeamsSubDir, "my_team")
	if th != want {
		t.Errorf("TeamHome(my_team) = %q, want %q", th, want)
	}
}

// TestTeamHome_不同团队名 测试不同团队名生成不同路径
func TestTeamHome_不同团队名(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/tmp/test_home")
	team1 := TeamHome("alpha")
	team2 := TeamHome("beta")
	if team1 == team2 {
		t.Errorf("不同团队应返回不同路径: %q == %q", team1, team2)
	}
	if filepath.Base(team1) != "alpha" {
		t.Errorf("TeamHome(alpha) base = %q, want alpha", filepath.Base(team1))
	}
}
