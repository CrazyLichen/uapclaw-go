package buildctx

import (
	"testing"

	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/workspace"
)

// TestSwarmBuildContextToFromSeedRoundtrip 对齐 context.py:196-260，16 键往返；
// per-member 五字段与活句柄不入 seed。
func TestSwarmBuildContextToFromSeedRoundtrip(t *testing.T) {
	base := NewSwarmBuildContext(SwarmBuildContextParams{
		SessionID: "s1", RequestID: "r1", UserID: "u1",
		ChannelID: "web", Channel: "web",
		RequestMetadata: map[string]any{"mode": "team"},
		Mode:            "team", ProjectDir: "/proj",
		TrustedDirs:             []string{"/proj/a"},
		TeamID:                  "t1",
		TeamWsRoot:              "/ws",
		TeamSkillVisibilityPath: "/ws/skills-visibility.json",
		GlobalSkillsDir:         "/lib",
	})
	seed := base.ToSeed()
	for _, banned := range []string{"member_name", "role", "language", "workspace",
		"member_card_id", "config", "trajectory_span_processor"} {
		if _, ok := seed[banned]; ok {
			t.Fatalf("seed 不应含 %q", banned)
		}
	}
	rebuilt := SwarmBuildContextFromSeed(seed, map[string]any{"k": "v"}, nil)
	if rebuilt.SessionID != "s1" || rebuilt.ProjectDir != "/proj" || rebuilt.TeamID != "t1" {
		t.Fatalf("往返丢失: %+v", rebuilt)
	}
	if rebuilt.Channel != "web" {
		t.Fatal("channel 显式值保留")
	}
	if rebuilt.UserID != "u1" || rebuilt.RequestID != "r1" {
		t.Fatal("身份字段丢失")
	}
	if len(rebuilt.TrustedDirs) != 1 || rebuilt.TrustedDirs[0] != "/proj/a" {
		t.Fatalf("trusted_dirs 往返丢失: %v", rebuilt.TrustedDirs)
	}
	if rebuilt.Config["k"] != "v" {
		t.Fatal("config 应由接收方注入")
	}
}

// TestSwarmBuildContextFromSeedDefaults 对齐 context.py:245-260 缺省值链。
func TestSwarmBuildContextFromSeedDefaults(t *testing.T) {
	r := SwarmBuildContextFromSeed(map[string]any{"channel_id": ""}, nil, nil)
	if r.Channel != "default" {
		t.Fatalf("channel 缺省 default，got %q", r.Channel)
	}
	if r.Mode != "team" {
		t.Fatalf("mode 缺省 team，got %q", r.Mode)
	}
	if r.DisableTeammateWorktree {
		t.Fatal("disable_teammate_worktree 缺省 false")
	}
}

// TestSwarmBuildContextCronBridge 对齐 context.py:127-147 __post_init__。
func TestSwarmBuildContextCronBridge(t *testing.T) {
	with := NewSwarmBuildContext(SwarmBuildContextParams{RequestMetadata: map[string]any{"cron": "0 8 * * *"}})
	if with.GetExtras()["is_cron"] != true {
		t.Fatal("cron 信号应桥接 extras")
	}
	without := NewSwarmBuildContext(SwarmBuildContextParams{})
	if _, ok := without.GetExtras()["is_cron"]; ok {
		t.Fatal("无 cron 不应写 extras")
	}
}

// TestResolveMemberSkillVisibilityPath 三分支（context.py:150-181）。
func TestResolveMemberSkillVisibilityPath(t *testing.T) {
	// workspace.root_path 优先
	ws := NewSwarmBuildContext(SwarmBuildContextParams{})
	ws.Workspace = &hworkspace.Workspace{RootPath: "/member-ws"}
	if got := ws.ResolveMemberSkillVisibilityPath(); got != "/member-ws/skills-visibility.json" {
		t.Fatalf("workspace 优先: %q", got)
	}
	// (team_id, member_name) 稳定布局回退
	c := NewSwarmBuildContext(SwarmBuildContextParams{TeamID: "t1", MemberName: "m1"})
	if got := c.ResolveMemberSkillVisibilityPath(); got == "" {
		t.Fatal("稳定布局回退应非空")
	}
	// 双缺空串
	none := NewSwarmBuildContext(SwarmBuildContextParams{})
	if got := none.ResolveMemberSkillVisibilityPath(); got != "" {
		t.Fatalf("双缺应空: %q", got)
	}
}

// TestResolveMemberWorkDir 覆盖基类恒 nil（context.py:183-195）。
func TestResolveMemberWorkDir(t *testing.T) {
	c := NewSwarmBuildContext(SwarmBuildContextParams{TaskWorkspaceRoot: t.TempDir(), MemberName: "m1"})
	got, err := c.ResolveMemberWorkDir()
	if err != nil || got == "" {
		t.Fatalf("平台子类应返回 work 目录: %q %v", got, err)
	}
	empty := NewSwarmBuildContext(SwarmBuildContextParams{})
	got, err = empty.ResolveMemberWorkDir()
	if got != "" || err != nil {
		t.Fatalf("无 task_workspace_root 应空串: %q %v", got, err)
	}
}

// TestSwarmBuildContextDeriveKeepsPlatformFields 偏差 D-15.16-c4：Derive
// 返回自身具体类型并保留平台字段。
func TestSwarmBuildContextDeriveKeepsPlatformFields(t *testing.T) {
	c := NewSwarmBuildContext(SwarmBuildContextParams{TeamID: "t1", TeamWsRoot: "/ws", SessionID: "s1"})
	derived := c.Derive(hschemaOverridesMember("m2"))
	d, ok := derived.(*SwarmBuildContext)
	if !ok {
		t.Fatalf("Derive 应保留平台子类，got %T", derived)
	}
	if d.TeamID != "t1" || d.TeamWsRoot != "/ws" || d.SessionID != "s1" {
		t.Fatalf("平台字段丢失: %+v", d)
	}
	if d.MemberName != "m2" {
		t.Fatalf("override 未生效: %q", d.MemberName)
	}
}

// TestHeartbeatSlot 单槽 Set/Get（context.py:28-41）。
func TestHeartbeatSlot(t *testing.T) {
	defer SetHeartbeatJobService(nil)
	if GetHeartbeatJobService() != nil {
		t.Fatal("未安装应 nil")
	}
	SetHeartbeatJobService("fake-heartbeat")
	if GetHeartbeatJobService() != "fake-heartbeat" {
		t.Fatal("应返回同值")
	}
	// from_seed 从本进程单槽取（context.py:258）
	r := SwarmBuildContextFromSeed(map[string]any{}, nil, nil)
	if r.HeartbeatJobService != "fake-heartbeat" {
		t.Fatal("from_seed 应从单槽取 heartbeat")
	}
}

// hschemaOverridesMember 构造仅覆盖 MemberName 的 overrides。
func hschemaOverridesMember(name string) hschema.BuildContextOverrides {
	return hschema.BuildContextOverrides{MemberName: hschema.StringPtr(name)}
}
