package team

import (
	"os"
	"path/filepath"
	"testing"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	upath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// setupSkillTestEnv 隔离 workspace 根与 agent_teams home（测试辅助）。
//
// UAPCLAW_DATA_DIR 控制 workspace.AgentSkillsDir()，
// agentteams.ConfigureHome 控制 TeamHome(teamName)。
// 返回 (workspaceDir, agentTeamsHome)，并注册 cleanup 恢复缓存。
func setupSkillTestEnv(t *testing.T) (string, string) {
	t.Helper()
	wsDir := t.TempDir()
	teamsHome := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", wsDir)
	upath.ResetCache()
	agentteams.ConfigureHome(teamsHome)
	t.Cleanup(func() {
		os.Unsetenv("UAPCLAW_DATA_DIR")
		upath.ResetCache()
		agentteams.ResetHome()
	})
	return wsDir, teamsHome
}

// TestSyncSkillsDir_源目录遍历 有效技能复制、目标已有先删再拷。
func TestSyncSkillsDir_源目录遍历(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()

	// 有效技能 skill-a（含 SKILL.md）；无效目录 no-skill（无 SKILL.md）；普通文件
	mkdir(t, filepath.Join(source, "skill-a"))
	writeFile(t, filepath.Join(source, "skill-a", "SKILL.md"), "a")
	mkdir(t, filepath.Join(source, "no-skill"))
	writeFile(t, filepath.Join(source, "plain.txt"), "x")

	// target 已有旧版 skill-a（将被覆盖）
	mkdir(t, filepath.Join(target, "skill-a"))
	writeFile(t, filepath.Join(target, "skill-a", "SKILL.md"), "old")

	syncSkillsDir(source, target)

	if _, err := os.Stat(filepath.Join(target, "skill-a", "SKILL.md")); err != nil {
		t.Fatalf("skill-a should be synced: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(target, "skill-a", "SKILL.md"))
	if string(data) != "a" {
		t.Fatalf("skill-a should be overwritten with new version, got %q", data)
	}
	if _, err := os.Stat(filepath.Join(target, "no-skill")); !os.IsNotExist(err) {
		t.Fatalf("no-skill (missing SKILL.md) should not be synced")
	}
	if _, err := os.Stat(filepath.Join(target, "plain.txt")); !os.IsNotExist(err) {
		t.Fatalf("plain file should not be synced")
	}
}

// TestSyncSkillsDir_源不存在直接返回。
func TestSyncSkillsDir_源不存在直接返回(t *testing.T) {
	target := t.TempDir()
	// 不 panic 即通过
	syncSkillsDir(filepath.Join(target, "nonexistent"), target)
}

// TestEnsureTeamSharedSkillsInitialized_marker幂等。
func TestEnsureTeamSharedSkillsInitialized_marker幂等(t *testing.T) {
	wsDir, teamsHome := setupSkillTestEnv(t)
	globalSkills := filepath.Join(wsDir, "agent", "workspace", "skills")
	mkdir(t, filepath.Join(globalSkills, "shared-skill"))
	writeFile(t, filepath.Join(globalSkills, "shared-skill", "SKILL.md"), "v1")
	writeFile(t, filepath.Join(globalSkills, "skills_state.json"), `{}`)

	spec := &atschema.TeamAgentSpec{TeamName: "marker-team"}

	mgr := NewTeamManager()
	mgr.EnsureTeamSharedSkillsInitialized(spec)

	// 解析 team shared dir（TeamHome(teamName)/team-workspace/skills）
	sharedDir := filepath.Join(agentteams.TeamHome("marker-team"), "team-workspace", "skills")
	if _, err := os.Stat(filepath.Join(sharedDir, "shared-skill", "SKILL.md")); err != nil {
		t.Fatalf("shared skill should be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sharedDir, ".team_skills_copied")); err != nil {
		t.Fatalf("marker file should exist: %v", err)
	}
	// 镜像回 global（syncSkillsDir 复制回 globalSkills——但 global 已有该技能，覆盖后仍为 v1）
	if _, err := os.Stat(filepath.Join(globalSkills, "shared-skill", "SKILL.md")); err != nil {
		t.Fatalf("mirror back to global should keep skill: %v", err)
	}
	_ = teamsHome

	// 二次调用：marker 存在 → 跳过复制（修改 global 后 shared 不变）
	writeFile(t, filepath.Join(globalSkills, "shared-skill", "SKILL.md"), "v2")
	mgr.EnsureTeamSharedSkillsInitialized(spec)
	data, _ := os.ReadFile(filepath.Join(sharedDir, "shared-skill", "SKILL.md"))
	if string(data) != "v1" {
		t.Fatalf("second call should skip copy (marker idempotent), got %q", data)
	}
}

// TestEnsureTeamSharedSkillsInitialized_specWorkspaceRoot优先。
func TestEnsureTeamSharedSkillsInitialized_specWorkspaceRoot优先(t *testing.T) {
	wsDir, _ := setupSkillTestEnv(t)
	globalSkills := filepath.Join(wsDir, "agent", "workspace", "skills")
	mkdir(t, filepath.Join(globalSkills, "s1"))
	writeFile(t, filepath.Join(globalSkills, "s1", "SKILL.md"), "x")

	wsRoot := t.TempDir()
	spec := &atschema.TeamAgentSpec{TeamName: "ws-team"}
	spec.Workspace = &team_workspace.TeamWorkspaceConfig{RootPath: wsRoot}

	mgr := NewTeamManager()
	mgr.EnsureTeamSharedSkillsInitialized(spec)

	if _, err := os.Stat(filepath.Join(wsRoot, "skills", "s1", "SKILL.md")); err != nil {
		t.Fatalf("spec.workspace.root_path should take precedence: %v", err)
	}
}

// mkdir 创建目录（测试辅助）。
func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// writeFile 写文件（测试辅助）。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
