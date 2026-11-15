package externalcli

import (
	"strings"
	"testing"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	teamworkspace "github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	harnessschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// TestPathValue 对齐 _path_value（external_cli_spawn.py:239-243）。
func TestPathValue(t *testing.T) {
	if pathValue("") != "" || pathValue("   ") != "" {
		t.Fatal("空/空白应返回空串")
	}
	if got := pathValue("/tmp/x"); got != "/tmp/x" {
		t.Fatalf("期望 /tmp/x，实际 %s", got)
	}
}

// TestSamePath 对齐 _same_path（246-247）：normcase+normpath。
func TestSamePath(t *testing.T) {
	if !samePath("/tmp/x/", "/tmp/x") || !samePath("/tmp/./x", "/tmp/x") {
		t.Fatal("normpath 等价应成立")
	}
	if samePath("/tmp/x", "/tmp/y") {
		t.Fatal("不同路径不应相等")
	}
}

// TestAppendExtraDir 对齐 _append_extra_dir（250-257）：空跳过、与 cwd 相同跳过、重复跳过。
func TestAppendExtraDir(t *testing.T) {
	var result []string
	appendExtraDir(&result, "", "/c")
	appendExtraDir(&result, "/c", "/c") // 与 cwd 相同
	appendExtraDir(&result, "/a", "/c")
	appendExtraDir(&result, "/a", "/c") // 重复
	appendExtraDir(&result, "/b", "/c")
	if strings.Join(result, ",") != "/a,/b" {
		t.Fatalf("期望 /a,/b，实际 %v", result)
	}
}

// TestTeamWorkspacePath 对齐 _team_workspace_path（260-264）。
func TestTeamWorkspacePath(t *testing.T) {
	// 配置了 root_path → 直接用
	spec := &schema.TeamAgentSpec{Workspace: &teamworkspace.TeamWorkspaceConfig{Enabled: true, RootPath: "/ws/root"}}
	if got := teamWorkspacePath(spec, "alpha"); got != "/ws/root" {
		t.Fatalf("期望 /ws/root，实际 %s", got)
	}
	// 未配置 → 默认拼出
	spec = &schema.TeamAgentSpec{}
	agentteams.ConfigureHome(t.TempDir())
	defer agentteams.ResetHome()
	if got := teamWorkspacePath(spec, "alpha"); got != agentteams.TeamWorkspaceDir("alpha") {
		t.Fatalf("默认应转调 TeamWorkspaceDir: %s", got)
	}
}

// TestBuildContextExtractors 对齐 _build_context_project_dir/_build_context_team_outputs_dir（267-278）。
// 物化实例走 GetProjectDir/GetTeamOutputsDir 接口断言（Python getattr 语义）。
func TestBuildContextExtractors(t *testing.T) {
	bc := &harnessschema.BuildContext{ProjectDir: "/p", TeamOutputsDir: "/o"}
	spec := &schema.TeamAgentSpec{BuildContext: bc}
	if got := buildContextProjectDir(spec); got != "/p" {
		t.Fatalf("ProjectDir 提取失败: %s", got)
	}
	if got := buildContextTeamOutputsDir(spec); got != "/o" {
		t.Fatalf("TeamOutputsDir 提取失败: %s", got)
	}
	spec.BuildContext = nil
	if buildContextProjectDir(spec) != "" || buildContextTeamOutputsDir(spec) != "" {
		t.Fatal("nil build_context 应返回空串")
	}
	// dict 形态（物化前）→ 空串（Python getattr(dict) 取不到属性）
	spec.BuildContext = map[string]any{"project_dir": "/p"}
	if buildContextProjectDir(spec) != "" {
		t.Fatal("dict 形态应返回空串")
	}
}

// TestResolveExternalPaths 对齐 _resolve_external_paths（281-298）：
// cwd 四级顺序 + extra_dirs 全量去重。
func TestResolveExternalPaths(t *testing.T) {
	agentteams.ConfigureHome(t.TempDir())
	defer agentteams.ResetHome()
	// 显式 cwd 优先；worktree/project/workspace 进 extra_dirs
	spec := &schema.TeamAgentSpec{
		Workspace:    &teamworkspace.TeamWorkspaceConfig{Enabled: true, RootPath: "/ws"},
		BuildContext: &harnessschema.BuildContext{ProjectDir: "/proj"},
	}
	ctx := &schema.TeamRuntimeContext{WorktreePath: "/wt"}
	cwd, extra := ResolveExternalPaths(spec, ctx, "/explicit", "alpha")
	if cwd != "/explicit" {
		t.Fatalf("cwd 应取显式值: %s", cwd)
	}
	if strings.Join(extra, ",") != "/wt,/proj,/ws" {
		t.Fatalf("extra_dirs 顺序错误: %v", extra)
	}
	// 无显式 cwd → worktree 顶上
	cwd, extra = ResolveExternalPaths(spec, ctx, "", "alpha")
	if cwd != "/wt" {
		t.Fatalf("cwd 应回落 worktree: %s", cwd)
	}
	if strings.Join(extra, ",") != "/proj,/ws" {
		t.Fatalf("extra_dirs 应含 project 与 workspace: %v", extra)
	}
	// 全空 → 落 team_workspace
	specEmpty := &schema.TeamAgentSpec{}
	ctxEmpty := &schema.TeamRuntimeContext{}
	cwd, extra = ResolveExternalPaths(specEmpty, ctxEmpty, "", "alpha")
	if cwd != agentteams.TeamWorkspaceDir("alpha") || len(extra) != 0 {
		t.Fatalf("全空应落 workspace 且 extra 为空: %s %v", cwd, extra)
	}
}
