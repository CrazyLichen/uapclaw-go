package agent_teams

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

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

// TestTeamWorkspaceDir_布局 验证共享工作区默认路径。
// Python: team_workspace_dir (paths.py:168-179)
func TestTeamWorkspaceDir_布局(t *testing.T) {
	origHome := configuredHome
	configuredHome = t.TempDir()
	defer func() { configuredHome = origHome }()
	got := TeamWorkspaceDir("alpha")
	want := filepath.Join(GetAgentTeamsHome(), "alpha", "team-workspace")
	if got != want {
		t.Fatalf("TeamWorkspaceDir 期望 %s，实际 %s", want, got)
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

// TestGetHome_默认数据根 未配置时 home 应为数据根
// 对齐 Python: bootstrap.py 启动时 configure_openjiuwen_home(get_user_workspace_dir())
func TestGetHome_默认数据根(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ResetHome()
	if got, want := GetHome(), workspace.WorkspaceDir(); got != want {
		t.Errorf("GetHome() = %q, want %q（数据根）", got, want)
	}
	if got, want := GetAgentTeamsHome(), workspace.WorkspaceDir()+"/.agent_teams"; got != want {
		t.Errorf("GetAgentTeamsHome() = %q, want %q", got, want)
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

// TestIndependentMemberWorkspace 测试成员独立工作空间路径
func TestIndependentMemberWorkspace(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/custom/home")
	result := IndependentMemberWorkspace("alice")
	expected := "/custom/home/alice_workspace"
	if result != expected {
		t.Errorf("IndependentMemberWorkspace(\"alice\") = %q, want %q", result, expected)
	}
}

// TestIndependentMemberWorkspace_空成员名 测试空成员名路径
func TestIndependentMemberWorkspace_空成员名(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()

	ConfigureHome("/custom/home")
	result := IndependentMemberWorkspace("")
	expected := "/custom/home/_workspace"
	if result != expected {
		t.Errorf("IndependentMemberWorkspace(\"\") = %q, want %q", result, expected)
	}
}

// TestConfigureGlobalSkillsDir_SetGetReset 对齐 paths.py:82-120 三函数。
func TestConfigureGlobalSkillsDir_SetGetReset(t *testing.T) {
	orig := configuredGlobalSkillsDir
	defer func() { configuredGlobalSkillsDir = orig }()

	ResetGlobalSkillsDir()
	ConfigureGlobalSkillsDir("/tmp/fake-lib")
	if got := GlobalSkillsDir(); got != "/tmp/fake-lib" {
		t.Fatalf("configured: got %q", got)
	}
	ResetGlobalSkillsDir()
	want := filepath.Join(GetHome(), "workspace", "skills")
	if got := GlobalSkillsDir(); got != want {
		t.Fatalf("fallback: got %q want %q", got, want)
	}
}

// TestTeamMemberWorkspaceDir 对齐 paths.py:182-194 布局。
func TestTeamMemberWorkspaceDir(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()
	ConfigureHome("/custom/home")
	got := TeamMemberWorkspaceDir("team-a", "m1")
	want := "/custom/home/.agent_teams/team-a/workspaces/m1_workspace"
	if got != want {
		t.Fatalf("TeamMemberWorkspaceDir = %q, want %q", got, want)
	}
}

// TestMemberSkillVisibilityPath 对齐 paths.py:205-224。
func TestMemberSkillVisibilityPath(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()
	ConfigureHome("/custom/home")
	got := MemberSkillVisibilityPath("team-a", "m1")
	want := "/custom/home/.agent_teams/team-a/workspaces/m1_workspace/skills-visibility.json"
	if got != want {
		t.Fatalf("MemberSkillVisibilityPath = %q, want %q", got, want)
	}
}

// TestTeamSkillVisibilityPath 对齐 paths.py:227-241。
func TestTeamSkillVisibilityPath(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()
	ConfigureHome("/custom/home")
	got := TeamSkillVisibilityPath("team-a")
	want := "/custom/home/.agent_teams/team-a/team-workspace/skills-visibility.json"
	if got != want {
		t.Fatalf("TeamSkillVisibilityPath = %q, want %q", got, want)
	}
}

// TestSetTaskOpenuapclawHome_任务级绑定优先 验证任务级 home 覆盖进程级配置。
// Python: set_task_openjiuwen_home/get_openjiuwen_home 解析序（paths.py:52-77）。
func TestSetTaskOpenuapclawHome_任务级绑定优先(t *testing.T) {
	origHome := configuredHome
	defer func() { configuredHome = origHome }()
	configuredHome = "/process/home"

	base := context.Background()
	if got := GetHomeWithContext(base); got != "/process/home" {
		t.Fatalf("无任务绑定时期望进程级 home，实际 %s", got)
	}

	taskCtx := SetTaskOpenuapclawHome(base, "/case/dir")
	if got := GetHomeWithContext(taskCtx); got != "/case/dir" {
		t.Fatalf("任务绑定后期望 case 级 home，实际 %s", got)
	}
	// 外层 ctx 不受影响（还原=改用外层 ctx，spec P1 口径）
	if got := GetHomeWithContext(base); got != "/process/home" {
		t.Fatalf("外层 ctx 被污染，实际 %s", got)
	}
	// GetHome 无 ctx 变体保持原语义
	if got := GetHome(); got != "/process/home" {
		t.Fatalf("GetHome 期望进程级 home，实际 %s", got)
	}
	// 空串绑定视为未绑定
	emptyCtx := SetTaskOpenuapclawHome(base, "")
	if got := GetHomeWithContext(emptyCtx); got != "/process/home" {
		t.Fatalf("空串绑定应回落进程级 home，实际 %s", got)
	}
}
