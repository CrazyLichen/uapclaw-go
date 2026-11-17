package externalcli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
)

// newSessionCtx 构造绑定了 session id 的上下文（Go 侧 ctx 传播替代 Python contextvar）。
func newSessionCtx(t *testing.T, sessionID string) context.Context {
	t.Helper()
	state := &sessionctx.SessionState{}
	state.SetSessionID(sessionID)
	return sessionctx.WithSessionState(context.Background(), state)
}

// TestDescriptorToEnv_携带join键 对齐 Python descriptor.py to_env：
// 返回 {OPENJIUWEN_TEAM_JOIN: JSON}。
func TestDescriptorToEnv_携带join键(t *testing.T) {
	d := &TeamJoinDescriptor{
		SessionID:  "s1",
		TeamName:   "pay",
		MemberName: "beta",
		Role:       "teammate",
		Scope:      "member",
		Language:   "cn",
	}
	env := d.ToEnv()
	raw, ok := env[TeamJoinEnv]
	if !ok || raw == "" {
		t.Fatal("ToEnv 必须携带 OPENJIUWEN_TEAM_JOIN 键")
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(raw), &back); err != nil {
		t.Fatalf("键值必须是合法 JSON: %v", err)
	}
	if back["session_id"] != "s1" || back["team_name"] != "pay" || back["member_name"] != "beta" {
		t.Fatalf("JSON 字段缺失: %v", back)
	}
	if back["scope"] != "member" {
		t.Fatalf("scope 应为显式传递的 member: %v", back["scope"])
	}
}

// TestDescriptorFromJSON_非法报文报错 对齐 Python descriptor.py from_json：
// ValidationError → AGENT_TEAM_CONFIG_INVALID，报文带 "malformed team join descriptor"。
func TestDescriptorFromJSON_非法报文报错(t *testing.T) {
	_, err := TeamJoinDescriptorFromJSON("{not-json")
	if err == nil || !strings.Contains(err.Error(), "malformed team join descriptor") {
		t.Fatalf("got %v", err)
	}
}

// TestDescriptorFromEnv_未设置报错 对齐 Python descriptor.py from_env：
// 环境变量缺失 → AGENT_TEAM_CONFIG_INVALID。
func TestDescriptorFromEnv_未设置报错(t *testing.T) {
	_, err := TeamJoinDescriptorFromEnv(map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "environment variable "+TeamJoinEnv+" is not set") {
		t.Fatalf("got %v", err)
	}
}

// TestDescriptorFromEnv_往返 对齐 from_env → to_env 往返保真。
func TestDescriptorFromEnv_往返(t *testing.T) {
	d := &TeamJoinDescriptor{SessionID: "s1", TeamName: "pay", MemberName: "beta", MaxSubtaskDepth: 2, EnableMemberMessaging: true}
	env := d.ToEnv()
	back, err := TeamJoinDescriptorFromEnv(env)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if back.SessionID != "s1" || back.MaxSubtaskDepth != 2 || !back.EnableMemberMessaging {
		t.Fatalf("往返丢失字段: %+v", back)
	}
}

// TestWithHomeEnv_已配置不覆盖 对齐 Python spawn.py:63-76 的 setdefault 语义：
// 调用方已显式配置的 home 不被覆盖；未配置时回落当前进程解析的 home。
func TestWithHomeEnv_已配置不覆盖(t *testing.T) {
	env := WithHomeEnv(map[string]string{"OPENJIUWEN_HOME": "/custom"})
	if env["OPENJIUWEN_HOME"] != "/custom" {
		t.Fatalf("已显式配置的 home 不得被覆盖，got %q", env["OPENJIUWEN_HOME"])
	}
}

// TestWithHomeEnv_未配置回落进程home 对齐 setdefault 回落 get_openjiuwen_home()。
func TestWithHomeEnv_未配置回落进程home(t *testing.T) {
	env := WithHomeEnv(map[string]string{})
	got := env["OPENJIUWEN_HOME"]
	if got == "" {
		t.Fatal("未配置时必须回落进程 home（openjiuwen home 解析）")
	}
}

// TestDescriptorFromContext_缺memberName报错 对齐 spawn.py:81-86。
func TestDescriptorFromContext_缺memberName报错(t *testing.T) {
	ctx := &schema.TeamRuntimeContext{TeamSpec: &schema.TeamSpec{TeamName: "pay"}}
	_, err := DescriptorFromContext(newSessionCtx(t, "s1"), ctx)
	if err == nil || !strings.Contains(err.Error(), "requires a member_name") {
		t.Fatalf("got %v", err)
	}
}

// TestDescriptorFromContext_缺teamSpec报错 对齐 spawn.py:87-93。
func TestDescriptorFromContext_缺teamSpec报错(t *testing.T) {
	ctx := &schema.TeamRuntimeContext{MemberName: "beta"}
	_, err := DescriptorFromContext(newSessionCtx(t, "s1"), ctx)
	if err == nil || !strings.Contains(err.Error(), "requires a team spec with team_name") {
		t.Fatalf("got %v", err)
	}
}

// TestDescriptorFromContext_缺sessionID报错 对齐 spawn.py:97-102。
func TestDescriptorFromContext_缺sessionID报错(t *testing.T) {
	rctx := &schema.TeamRuntimeContext{MemberName: "beta", TeamSpec: &schema.TeamSpec{TeamName: "pay"}}
	_, err := DescriptorFromContext(context.Background(), rctx)
	if err == nil || !strings.Contains(err.Error(), "requires an active team session_id") {
		t.Fatalf("got %v", err)
	}
}

// TestDescriptorFromContext_transport与workspace 对齐 spawn.py:103-139：
// external_messager_config 优先、pyzmq direct_addr 改写、workspace enabled 门卫、
// root_path 回落 TeamWorkspaceDir。
func TestDescriptorFromContext_transport与workspace(t *testing.T) {
	ext := &schema.MessagerTransportConfig{Backend: "pyzmq", DirectAddr: "tcp://127.0.0.1:5555"}
	wsRoot := t.TempDir()
	rctx := &schema.TeamRuntimeContext{
		MemberName: "beta",
		Role:       schema.TeamRoleTeammate,
		TeamSpec: &schema.TeamSpec{
			TeamName:               "pay",
			Language:               "cn",
			DispatchMode:           "autonomous",
			TeammateMode:           schema.MemberModeBuildMode,
			EnableMemberMessaging:  true,
			MaxSubtaskDepth:        3,
			ExternalMessagerConfig: ext,
			Workspace: &team_workspace.TeamWorkspaceConfig{
				Enabled:  true,
				RootPath: wsRoot,
			},
		},
	}
	d, err := DescriptorFromContext(newSessionCtx(t, "s1"), rctx)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if d.SessionID != "s1" {
		t.Fatalf("session_id 必须来自团队会话上下文，got %q", d.SessionID)
	}
	if d.TransportConfig == nil || d.TransportConfig.Backend != "pyzmq" {
		t.Fatal("external_messager_config 必须优先")
	}
	if d.TransportConfig.DirectAddr != "tcp://127.0.0.1:*" {
		t.Fatalf("pyzmq direct_addr 必须改写为通配，got %q", d.TransportConfig.DirectAddr)
	}
	if d.TransportConfig.TeamName != "pay" || d.TransportConfig.NodeID != "beta" {
		t.Fatalf("transport 需带 team_name/node_id 更新: %+v", d.TransportConfig)
	}
	if d.WorkspacePath != wsRoot {
		t.Fatalf("workspace_path 应取 root_path，got %q", d.WorkspacePath)
	}
	if d.Scope != "member" {
		t.Fatal("spawn 路径 scope 固定 member（一等队员）")
	}
	if d.MaxSubtaskDepth != 3 || !d.EnableMemberMessaging {
		t.Fatal("max_subtask_depth / enable_member_messaging 需透传")
	}
}

// TestDescriptorFromContext_workspace未enabled时路径为空 对齐 spawn.py:109-117。
func TestDescriptorFromContext_workspace未enabled时路径为空(t *testing.T) {
	rctx := &schema.TeamRuntimeContext{
		MemberName: "beta",
		Role:       schema.TeamRoleTeammate,
		TeamSpec:   &schema.TeamSpec{TeamName: "pay", Workspace: &team_workspace.TeamWorkspaceConfig{Enabled: false}},
	}
	d, err := DescriptorFromContext(newSessionCtx(t, "s1"), rctx)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if d.WorkspaceConfig != nil || d.WorkspacePath != "" {
		t.Fatalf("未 enabled 的 workspace 不得写入描述符: %+v", d)
	}
}

// TestDescriptorFromContext_回落ctxMessagerConfig 对齐 spawn.py:103 的第二优先级
// ctx.messager_config，以及二者皆缺时默认 MessagerTransportConfig。
func TestDescriptorFromContext_回落ctxMessagerConfig(t *testing.T) {
	rctx := &schema.TeamRuntimeContext{
		MemberName: "beta",
		Role:       schema.TeamRoleTeammate,
		TeamSpec:   &schema.TeamSpec{TeamName: "pay"},
		MessagerConfig: &schema.MessagerTransportConfig{
			Backend: "inprocess",
		},
	}
	d, err := DescriptorFromContext(newSessionCtx(t, "s1"), rctx)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if d.TransportConfig == nil || d.TransportConfig.Backend != "inprocess" {
		t.Fatal("external 缺省时必须回落 ctx.messager_config")
	}
	if d.TransportConfig.TeamName != "pay" || d.TransportConfig.NodeID != "beta" {
		t.Fatal("回落路径同样需要 team_name/node_id 更新")
	}
}

// TestDescriptorFromJSON_required字段缺失报错 对齐 descriptor.py:116-118
// pydantic required：session_id/team_name/member_name 缺失报
// AGENT_TEAM_CONFIG_INVALID，文案对齐 pydantic "Field required"。
func TestDescriptorFromJSON_required字段缺失报错(t *testing.T) {
	for _, raw := range []string{
		`{"team_name":"t","member_name":"m"}`,
		`{"session_id":"s","member_name":"m"}`,
		`{"session_id":"s","team_name":"t"}`,
		`{}`,
	} {
		_, err := TeamJoinDescriptorFromJSON(raw)
		if err == nil || !strings.Contains(err.Error(), "Field required") {
			t.Fatalf("raw %s: got %v", raw, err)
		}
	}
	// 三字段齐全应通过
	d, err := TeamJoinDescriptorFromJSON(`{"session_id":"s","team_name":"t","member_name":"m"}`)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if d.SessionID != "s" || d.TeamName != "t" || d.MemberName != "m" {
		t.Fatalf("往返: %+v", d)
	}
}
