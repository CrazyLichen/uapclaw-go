package team

import (
	"os"
	"path/filepath"
	"testing"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
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

// ──────────────────────────── 10.6.19 覆盖补齐：技能 rail 注册/同步/热更新 ────────────────────────────

// TestSyncTeamSkills_无目标与有目标 无 sync target 时 no-op，有 target 时执行同步。
func TestSyncTeamSkills_无目标与有目标(t *testing.T) {
	mgr := NewTeamManager()
	// 无 target：不 panic（debug 日志路径）
	mgr.SyncTeamSkills("sess-none")

	// 有 target：source → target 同步
	source, target := t.TempDir(), t.TempDir()
	mkdir(t, filepath.Join(source, "sk-1"))
	writeFile(t, filepath.Join(source, "sk-1", "SKILL.md"), "v1")
	mgr.RegisterTeamSkillSyncTarget("sess-s", source, target)
	if !mgr.HasTeamSkillSyncTarget("sess-s") {
		t.Fatalf("HasTeamSkillSyncTarget should be true")
	}
	mgr.SyncTeamSkills("sess-s")
	if _, err := os.Stat(filepath.Join(target, "sk-1", "SKILL.md")); err != nil {
		t.Fatalf("sync should copy skill to target: %v", err)
	}
}

// TestSyncTeamSkillsAcrossManagers_全局查找注册 target 的 manager 并同步。
func TestSyncTeamSkillsAcrossManagers_全局查找同步(t *testing.T) {
	_, _ = setupSkillTestEnv(t)
	source, target := t.TempDir(), t.TempDir()
	mkdir(t, filepath.Join(source, "sk-g"))
	writeFile(t, filepath.Join(source, "sk-g", "SKILL.md"), "g")

	// 注册到全局实例（GetTeamManager 内部 channel 注册）
	tm := GetTeamManager("sync-test-ch")
	tm.RegisterTeamSkillSyncTarget("sess-across", source, target)

	if !SyncTeamSkillsAcrossManagers("sess-across") {
		t.Fatalf("SyncTeamSkillsAcrossManagers should return true when target found")
	}
	if _, err := os.Stat(filepath.Join(target, "sk-g", "SKILL.md")); err != nil {
		t.Fatalf("across sync should copy: %v", err)
	}
	// 无 target 的 session → false
	if SyncTeamSkillsAcrossManagers("sess-no-target") {
		t.Fatalf("should return false when no target registered")
	}
}

// TestRegisterTeamLiveRail_幂等注册与反注册。
func TestRegisterTeamLiveRail_幂等注册与反注册(t *testing.T) {
	mgr := NewTeamManager()
	agent := newCustomizerFakeAgent("agent")
	rail := evolution.NewTeamSkillEvolutionRail([]string{t.TempDir()}, nil, "m", "zh")

	// 重复注册幂等
	mgr.RegisterTeamLiveRail("sess-lr", agent, rail)
	mgr.RegisterTeamLiveRail("sess-lr", agent, rail)
	if len(mgr.teamLiveRails["sess-lr"]) != 1 {
		t.Fatalf("duplicate registration should be idempotent, got %d", len(mgr.teamLiveRails["sess-lr"]))
	}

	// 反注册：清空后 map 键删除
	mgr.unregisterLiveRail("sess-lr", rail)
	if _, ok := mgr.teamLiveRails["sess-lr"]; ok {
		t.Fatalf("live rails entry should be removed when empty")
	}
	// 反注册不存在的 session：no-op
	mgr.unregisterLiveRail("sess-missing", rail)
}

// TestDrainTeamSkillEvents_无rail返回空 未注册 rail 时返回空切片。
func TestDrainTeamSkillEvents_无rail返回空(t *testing.T) {
	mgr := NewTeamManager()
	events := mgr.DrainTeamSkillEvents("sess-none")
	if len(events) != 0 {
		t.Fatalf("no rail registered should return empty, got %v", events)
	}
}

// TestUpdateEvolutionConfig_热更新开关 autoScan 传播与 create rail 反注册。
func TestUpdateEvolutionConfig_热更新开关(t *testing.T) {
	mgr := NewTeamManager()
	// 注册 member SkillEvolutionRail + team create rail
	memberRail := evolution.NewSkillEvolutionRail([]string{t.TempDir()}, nil, "m", "zh")
	mgr.RegisterTeamMemberSkillEvolutionRail("sess-ue", memberRail)
	mgr.RegisterTeamMemberSkillEvolutionRail("sess-ue", memberRail) // 幂等

	agent := newCustomizerFakeAgent("agent")
	createRail := evolution.NewTeamSkillCreateRail(t.TempDir())
	mgr.RegisterTeamSkillCreateRail("sess-ue", createRail)
	mgr.RegisterTeamLiveRail("sess-ue", agent, createRail)

	// skill_create 关闭（默认 False 时 evolution skill_create_enabled 未开启）→ create rail 反注册
	mgr.UpdateEvolutionConfig(map[string]any{})

	if _, ok := mgr.teamSkillCreateRails["sess-ue"]; ok {
		t.Fatalf("create rail should be unregistered when skill_create disabled")
	}
}

// TestDrainTeamSkillEvents_rail为nil返回空 已注册但 rail 为 nil 时返回空切片。
func TestDrainTeamSkillEvents_rail为nil返回空(t *testing.T) {
	mgr := NewTeamManager()
	mgr.teamSkillRails["sess-nil"] = nil // 直接注册 nil rail
	events := mgr.DrainTeamSkillEvents("sess-nil")
	if len(events) != 0 {
		t.Fatalf("nil rail should return empty, got %v", events)
	}
}

// TestFindTeamSkillRailForRequest_无匹配返回nil 遍历无 pending snapshot 的 rail 返回 nil。
func TestFindTeamSkillRailForRequest_无匹配返回nil(t *testing.T) {
	mgr := NewTeamManager()
	rail := evolution.NewTeamSkillEvolutionRail([]string{t.TempDir()}, nil, "m", "zh")
	mgr.RegisterTeamSkillRail("sess-fr", rail)
	// rail 无 pendingApprovalSnapshots 条目 → 不命中
	if got := mgr.FindTeamSkillRailForRequest("req-none"); got != nil {
		t.Fatalf("no pending snapshot should return nil, got %v", got)
	}
	// GetTeamSkillRail 取回同一实例
	if mgr.GetTeamSkillRail("sess-fr") != rail {
		t.Fatalf("GetTeamSkillRail should return registered rail")
	}
}

// TestRegisterTeamRailContext_非leader丢弃。
func TestRegisterTeamRailContext_非leader丢弃(t *testing.T) {
	mgr := NewTeamManager()
	// nil context / 非 leader role → 不注册
	mgr.RegisterTeamRailContext("sess-nc", nil)
	teammateRole := "teammate"
	mgr.RegisterTeamRailContext("sess-tl", &TeamRailMountContext{
		MemberInfo: &MemberInfo{Role: &teammateRole},
	})
	if mgr.GetTeamRailContext("sess-nc") != nil || mgr.GetTeamRailContext("sess-tl") != nil {
		t.Fatalf("nil/non-leader context should be discarded")
	}
}
