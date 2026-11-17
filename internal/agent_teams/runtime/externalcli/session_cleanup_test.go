package externalcli

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 会话清理测试 ────────────────────────────
// 对齐 Python session_cleanup 语义（上游单测无独立 session_cleanup 文件；
// 本处覆盖 _external_cli_configs_for_team / decodeTeamAgentSpec /
// cleanup_external_cli_backend_session 的全部可达分支）。

// fakeCleanupSession 复用 member_runtime_test 的 fakeMemberSession；经
// WriteTeamsBucket 形状写入团队命名空间。这里直接构造 metadata 层读取的
// 状态键值。

func TestDecodeTeamAgentSpec_四形状(t *testing.T) {
	spec := &schema.TeamAgentSpec{}
	if got, err := decodeTeamAgentSpec(spec); err != nil || got != spec {
		t.Fatalf("指针形状应原样返回: %v %v", got, err)
	}
	value := schema.TeamAgentSpec{}
	if got, err := decodeTeamAgentSpec(value); err != nil || got == nil {
		t.Fatalf("值形状应返回指针: %v %v", got, err)
	}
	got, err := decodeTeamAgentSpec(map[string]any{"bad": "payload"})
	if err == nil {
		// map 形状经 JSON 往返；未知字段默认忽略，能解出空 spec 也合法
		if got == nil {
			t.Fatal("map 形状必须返回 spec 或 error")
		}
	}
	if _, err := decodeTeamAgentSpec(42); err == nil {
		t.Fatal("不支持类型必须报错")
	}
}

func TestExternalCLIConfigsForTeam_无bucket返回空(t *testing.T) {
	sess := newFakeMemberSession()
	if got := externalCLIConfigsForTeam(sess, "pay"); got != nil {
		t.Fatalf("无 teams bucket 应返回 nil，got %v", got)
	}
}

func TestExternalCLIConfigsForTeam_无spec返回空(t *testing.T) {
	sess := newFakeMemberSession()
	writeTeamsBucketForTest(sess, "pay", map[string]any{})
	if got := externalCLIConfigsForTeam(sess, "pay"); got != nil {
		t.Fatalf("bucket 无 spec 应返回 nil，got %v", got)
	}
}

func TestExternalCLIConfigsForTeam_按cliAgent索引(t *testing.T) {
	sess := newFakeMemberSession()
	spec := map[string]any{
		"external_cli_agents": []any{
			map[string]any{"cli_agent": "openclaw", "command": []any{"fake-cli"}},
			map[string]any{"cli_agent": "hermes"},
		},
	}
	writeTeamsBucketForTest(sess, "pay", map[string]any{"spec": spec})
	got := externalCLIConfigsForTeam(sess, "pay")
	if len(got) != 2 {
		t.Fatalf("应索引两个条目，got %d", len(got))
	}
	if got["openclaw"] == nil || got["hermes"] == nil {
		t.Fatalf("键漂移: %v", got)
	}
	if len(got["openclaw"].Command) != 1 || got["openclaw"].Command[0] != "fake-cli" {
		t.Fatalf("command 未还原: %v", got["openclaw"].Command)
	}
}

func TestExternalCLIConfigsForTeam_坏spec返回空(t *testing.T) {
	sess := newFakeMemberSession()
	// skills 里放非可序列化形状不会触发；这里用非法 cli_agent 之外的路径：
	// spec 是非法 JSON 值（chan）使 map→JSON 往返失败
	writeTeamsBucketForTest(sess, "pay", map[string]any{"spec": map[string]any{
		"bad": make(chan int),
	}})
	got := externalCLIConfigsForTeam(sess, "pay")
	if got != nil {
		t.Fatalf("坏 spec 应返回 nil，got %v", got)
	}
}

func TestCleanupExternalCLIBackendSession_非claude立即返回(t *testing.T) {
	// 非 claude：Python `if cli_agent != "claude": return`
	cleanupExternalCLIBackendSession("openclaw", "s1", "beta", &schema.ExternalCliAgentSpec{})
	// claude：delete_claude_session ⤵️15.7，走 Debug 分支返回，不 panic
	cleanupExternalCLIBackendSession("claude", "s1", "beta", &schema.ExternalCliAgentSpec{})
}

// writeTeamsBucketForTest 按 metadata.ReadTeamsBucket 读取的状态形状写入。
func writeTeamsBucketForTest(sess *fakeMemberSession, teamName string, bucket map[string]any) {
	raw, ok := sess.states["teams"]
	var teams map[string]any
	if ok {
		if m, ok := raw.(map[string]any); ok {
			teams = m
		}
	}
	if teams == nil {
		teams = map[string]any{}
	}
	teams[teamName] = bucket
	sess.states[metadata.TeamsKey] = teams
}

// TestExternalCLIConfigsForTeam_无键过滤 对齐 session_cleanup.py:104 一行推导：
// Python dict 推导不过滤任何键——含 "/" 或空串的 cli_agent 照常收录
// （Go map 键无冲突问题，防御性跳过已摘除）。
func TestExternalCLIConfigsForTeam_无键过滤(t *testing.T) {
	sess := newFakeMemberSession()
	writeTeamsBucketForTest(sess, "team-x", map[string]any{"spec": map[string]any{
		"external_cli_agents": []any{
			map[string]any{"cli_agent": "codex"},
			map[string]any{"cli_agent": "some/provider"},
			map[string]any{"cli_agent": ""},
		},
	}})
	configs := externalCLIConfigsForTeam(sess, "team-x")
	if len(configs) != 3 {
		t.Fatalf("Python 推导不过滤键，应收录 3 条: %d", len(configs))
	}
	if configs["codex"] == nil || configs["some/provider"] == nil {
		t.Fatalf("键缺失: %v", configs)
	}
}
