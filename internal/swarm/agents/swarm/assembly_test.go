package swarm

import (
	"path/filepath"
	"testing"

	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
	atschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	teamworkspace "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// TestEnrichTeamSpecForSwarmSeed_无项目分配产物目录 对齐 assembly.py:482-511。
func TestEnrichTeamSpecForSwarmSeed_无项目分配产物目录(t *testing.T) {
	spec := &atschema.TeamAgentSpec{}
	spec.TeamName = "t1"
	err := EnrichTeamSpecForSwarmSeed(spec, EnrichSeedParams{
		SessionID: "s1", Mode: "team", ChannelID: "web",
		RequestMetadata: map[string]any{"mode": "team"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.BuildContext == nil {
		t.Fatal("build_context 未挂")
	}
	bc := spec.BuildContext.(*buildctx.SwarmBuildContext)
	if bc.TaskWorkspaceRoot == "" || bc.TeamOutputsDir == "" {
		t.Fatal("无项目团队应分配产物目录")
	}
	if len(spec.BuildContextSeed) == 0 {
		t.Fatal("seed 未生成（assembly.py:540）")
	}
	if spec.BuildContextSeed["team_outputs_dir"] != bc.TeamOutputsDir {
		t.Fatal("seed 与活载体不一致")
	}
	// 产物目录应在团队工作空间 artifacts/ 下
	if !contains(bc.TeamOutputsDir, filepath.Join(workspace.WorkspaceDir(), ".agent_teams", "t1", "team-workspace", "artifacts")) &&
		filepath.Base(filepath.Dir(filepath.Dir(bc.TeamOutputsDir))) != "artifacts" {
		t.Fatalf("产物目录形状: %q", bc.TeamOutputsDir)
	}
}

// TestEnrichTeamSpecForSwarmSeed_有项目 对齐 :482 分支取反。
func TestEnrichTeamSpecForSwarmSeed_有项目(t *testing.T) {
	spec := &atschema.TeamAgentSpec{}
	spec.TeamName = "t1"
	err := EnrichTeamSpecForSwarmSeed(spec, EnrichSeedParams{
		SessionID: "s1", Mode: "team", ProjectDir: "/proj",
	})
	if err != nil {
		t.Fatal(err)
	}
	bc := spec.BuildContext.(*buildctx.SwarmBuildContext)
	if bc.TaskWorkspaceRoot != "" || bc.TeamOutputsDir != "" {
		t.Fatal("绑定项目的成员产物留在项目内，双 nil")
	}
	if bc.ProjectDir != "/proj" {
		t.Fatal("project_dir 应入 context")
	}
}

// TestEnrichTeamSpecForSwarmSeed_Workspace优先 对齐 :470-477。
func TestEnrichTeamSpecForSwarmSeed_Workspace优先(t *testing.T) {
	spec := &atschema.TeamAgentSpec{}
	spec.TeamName = "t1"
	spec.Workspace = teamWorkspaceConfigFixture("/custom/team-ws")
	err := EnrichTeamSpecForSwarmSeed(spec, EnrichSeedParams{SessionID: "s1", Mode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.BuildContext.(*buildctx.SwarmBuildContext).TeamWsRoot; got != "/custom/team-ws" {
		t.Fatalf("workspace.root_path 应优先: %q", got)
	}
	// 默认布局回退
	spec2 := &atschema.TeamAgentSpec{}
	spec2.TeamName = "t2"
	if err := EnrichTeamSpecForSwarmSeed(spec2, EnrichSeedParams{SessionID: "s1", Mode: "team"}); err != nil {
		t.Fatal(err)
	}
	if got := spec2.BuildContext.(*buildctx.SwarmBuildContext).TeamWsRoot; got == "" {
		t.Fatal("默认布局回退失败")
	}
}

// TestEnrichTeamSpecForSwarmSeed_DisableWorktree 对齐 :493-497 谓词。
func TestEnrichTeamSpecForSwarmSeed_DisableWorktree(t *testing.T) {
	spec := &atschema.TeamAgentSpec{}
	spec.TeamName = "t1"
	// web channel + team 模式（非 code profile）→ disable=true
	if err := EnrichTeamSpecForSwarmSeed(spec, EnrichSeedParams{
		SessionID: "s1", Mode: "team", ChannelID: "web",
	}); err != nil {
		t.Fatal(err)
	}
	if !spec.BuildContext.(*buildctx.SwarmBuildContext).DisableTeammateWorktree {
		t.Fatal("web+team 应禁用 teammate worktree")
	}
	// 非 web channel → false
	spec2 := &atschema.TeamAgentSpec{}
	spec2.TeamName = "t1"
	if err := EnrichTeamSpecForSwarmSeed(spec2, EnrichSeedParams{
		SessionID: "s1", Mode: "team", ChannelID: "cli",
	}); err != nil {
		t.Fatal(err)
	}
	if spec2.BuildContext.(*buildctx.SwarmBuildContext).DisableTeammateWorktree {
		t.Fatal("非 web channel 不禁用")
	}
}

// TestSeedParamsFromRequestMetadata 对齐 team_manager.py:2421-2426 提取语义。
func TestSeedParamsFromRequestMetadata(t *testing.T) {
	p := SeedParamsFromRequestMetadata("s1", map[string]any{
		"mode": "team", "project_dir": "/proj",
	})
	if p.Mode != "team" || p.ProjectDir != "/proj" || p.SessionID != "s1" {
		t.Fatalf("提取失败: %+v", p)
	}
	// mode 缺省空（由调用方/缺省链处理）
	p2 := SeedParamsFromRequestMetadata("s1", nil)
	if p2.Mode != "" || p2.ProjectDir != "" {
		t.Fatalf("空 metadata 应得空: %+v", p2)
	}
}

// contains 子串包含判定。
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// teamWorkspaceConfigFixture 构造指定根路径的工作空间配置。
func teamWorkspaceConfigFixture(root string) *teamworkspace.TeamWorkspaceConfig {
	return &teamworkspace.TeamWorkspaceConfig{RootPath: root}
}
