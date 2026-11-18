package spawn

import (
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
)

// ──────────────────────────── negotiate ────────────────────────────

// TestNegotiateProviderEndpoint 全分支对齐 Python _negotiate_provider_endpoint。
func TestNegotiateProviderEndpoint(t *testing.T) {
	rtc := &atschema.TeamRuntimeContext{CLIAgent: "dsh", MemberName: "dev"}
	// config nil → nil
	if got := negotiateProviderEndpoint(rtc, nil, func(string) bool { return true }); got != nil {
		t.Fatal("config nil 应返回 nil")
	}
	// api_base 与 provider 都空 → 原样返回（内置模型直通）
	cfg := &atschema.ExternalCliModelConfig{Model: "m"}
	if got := negotiateProviderEndpoint(rtc, cfg, func(string) bool { return false }); got != cfg {
		t.Fatal("无端点信息的配置应直通")
	}
	// provider 声明 ENDPOINT_MODELS → 直通
	cfg = &atschema.ExternalCliModelConfig{Provider: "anthropic", APIBase: "https://api"}
	if got := negotiateProviderEndpoint(rtc, cfg, func(string) bool { return true }); got != cfg {
		t.Fatal("provider 收端点模型应直通")
	}
	// provider 不收 → 丢弃（nil）
	if got := negotiateProviderEndpoint(rtc, cfg, func(string) bool { return false }); got != nil {
		t.Fatal("provider 不收端点模型应丢弃")
	}
}

// TestNegotiateProviderFallback 全分支对齐 Python _negotiate_provider_fallback。
func TestNegotiateProviderFallback(t *testing.T) {
	rtc := &atschema.TeamRuntimeContext{CLIAgent: "codex", MemberName: "dev"}
	if got := negotiateProviderFallback(rtc, nil, func(string) bool { return true }); got != nil {
		t.Fatal("config nil 应返回 nil")
	}
	cfg := &atschema.ExternalCliModelConfig{Provider: "openai"}
	if got := negotiateProviderFallback(rtc, cfg, func(string) bool { return true }); got != cfg {
		t.Fatal("takes_auth_fallback 成立应直通")
	}
	if got := negotiateProviderFallback(rtc, cfg, func(string) bool { return false }); got != nil {
		t.Fatal("无认证回退应丢弃")
	}
}

// ──────────────────────────── config 三级选择 ────────────────────────────

// TestExternalCliConfigForMember 三级选择顺序对齐 Python。
func TestExternalCliConfigForMember(t *testing.T) {
	predefinedCfg := &atschema.ExternalCliAgentSpec{CLIAgent: "dsh"}
	templateCfg := &atschema.ExternalCliAgentSpec{CLIAgent: "dsh"}
	teamCfg := &atschema.ExternalCliAgentSpec{CLIAgent: "dsh"}
	spec := atschema.TeamAgentSpec{
		PredefinedMembers: []atschema.TeamMemberSpec{
			{MemberName: "dev", ExternalCLI: predefinedCfg},
		},
		ExternalCliAgents: []atschema.ExternalCliAgentSpec{*teamCfg},
	}
	// 一级：预定义成员自带配置
	if got := externalCliConfigForMember(spec, "dev", "dsh", templateCfg); got != predefinedCfg {
		t.Fatal("预定义成员应返回自带配置")
	}
	// 二级：模板回读配置
	if got := externalCliConfigForMember(spec, "other", "dsh", templateCfg); got != templateCfg {
		t.Fatal("无预定义应返回模板配置")
	}
	// 三级：团队按 cli_agent 声明
	if got := externalCliConfigForMember(spec, "other", "dsh", nil); got == nil {
		t.Fatal("团队声明配置应命中")
	}
	// 全空 → nil
	if got := externalCliConfigForMember(spec, "other", "nope", nil); got != nil {
		t.Fatal("无任何配置应返回 nil")
	}
}

// ──────────────────────────── 路径助手 ────────────────────────────

// TestPathValue 空白剥离与空串归零。
func TestPathValue(t *testing.T) {
	if pathValue("  ") != "" {
		t.Fatal("全空白应归空串")
	}
	if pathValue(" /tmp/x ") != "/tmp/x" {
		t.Fatal("应剥离空白")
	}
}

// TestSamePath 规范化等价判定。
func TestSamePath(t *testing.T) {
	if !samePath("/tmp/x", "/tmp/x/") {
		t.Fatal("尾斜杠应等价")
	}
	if !samePath("/tmp/./x", "/tmp/x") {
		t.Fatal("点段应等价")
	}
	if samePath("/tmp/x", "/tmp/y") {
		t.Fatal("不同路径不等价")
	}
}

// TestAppendExtraDir nil/重复/cwd 相同三跳过。
func TestAppendExtraDir(t *testing.T) {
	var dirs []string
	appendExtraDir(&dirs, "", "/tmp") // nil 跳过
	if len(dirs) != 0 {
		t.Fatal("空路径不应追加")
	}
	appendExtraDir(&dirs, "/tmp", "/tmp") // cwd 相同跳过
	if len(dirs) != 0 {
		t.Fatal("与 cwd 相同不应追加")
	}
	appendExtraDir(&dirs, "/a", "/tmp")
	appendExtraDir(&dirs, "/a", "/tmp") // 重复跳过
	if len(dirs) != 1 || dirs[0] != "/a" {
		t.Fatalf("去重后应为 [/a]，实际 %v", dirs)
	}
}

// TestResolveExternalPaths 四级 cwd 选择 + extra_dirs 组装。
func TestResolveExternalPaths(t *testing.T) {
	spec := &atschema.TeamAgentSpec{
		TeamName: "team1",
		Workspace: &team_workspace.TeamWorkspaceConfig{
			Enabled:  true,
			RootPath: "/ws/root",
		},
	}
	rtc := &atschema.TeamRuntimeContext{WorktreePath: "/wt"}
	// 显式 cwd 优先
	cwd, dirs := resolveExternalPaths(spec, rtc, "/explicit", "team1")
	if cwd != "/explicit" {
		t.Fatalf("显式 cwd 应优先，实际 %s", cwd)
	}
	// 显式 cwd 不在 extra_dirs（与 cwd 相同）；project_dir 空被跳过，
	// 其余（worktree/workspace）去重进入
	if len(dirs) != 2 {
		t.Fatalf("extra_dirs 应含 worktree/workspace 两项，实际 %v", dirs)
	}
	// 依次回退：worktree
	cwd, _ = resolveExternalPaths(spec, rtc, "", "team1")
	if cwd != "/wt" {
		t.Fatalf("worktree 应次优先，实际 %s", cwd)
	}
	// project_dir 回退
	rtc2 := &atschema.TeamRuntimeContext{}
	cwd, _ = resolveExternalPaths(spec, rtc2, "", "team1")
	if cwd != "/ws/root" {
		t.Fatalf("workspace 应兜底，实际 %s", cwd)
	}
}
