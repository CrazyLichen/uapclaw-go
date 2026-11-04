package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// TestResolveRequestRuntimeMode 覆盖三段/裸 subMode/旧串回落的归一。
//
// Python: jiuwenswarm/runtime/request.py resolve_request_runtime_mode
func TestResolveRequestRuntimeMode(t *testing.T) {
	cases := []struct {
		name, mode, subMode string
		wantCanonical       string
		wantPlan, wantTeam  bool
	}{
		{"三段 plan", "agent.work.plan", "", "agent.work.plan", true, false},
		{"三段 normal", "agent.work.normal", "", "agent.work.normal", false, false},
		{"code plan", "agent.code.plan", "", "agent.code.plan", true, false},
		{"code team", "team.code.normal", "", "team.code.normal", false, true},
		{"裸 subMode plan", "", "plan", "agent.work.plan", true, false},
		{"裸 subMode normal", "", "normal", "agent.work.normal", false, false},
		{"旧两段串 canonicalize", "agent." + "plan", "", "agent.work.plan", true, false},
		{"team 族", "team", "", "team.work.normal", false, true},
		{"team 带子模式", "team.work.plan", "", "team.work.plan", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveRequestRuntimeMode(c.mode, c.subMode)
			require.Equal(t, c.wantCanonical, got.CanonicalMode)
			require.Equal(t, c.wantPlan, got.IsPlan)
			require.Equal(t, c.wantTeam, got.IsTeam)
		})
	}
}

// TestShouldSync 验证只认 chat 类方法。
func TestShouldSync(t *testing.T) {
	require.True(t, ShouldSync(""))
	require.True(t, ShouldSync("chat.send"))
	require.True(t, ShouldSync("chat.resume"))
	require.True(t, ShouldSync("chat.answer"))
	require.False(t, ShouldSync("session.list"))
	require.False(t, ShouldSync("agent.status"))
}

// TestIsPlanEntrySource 验证显式进入来源集合。
func TestIsPlanEntrySource(t *testing.T) {
	require.True(t, IsPlanEntrySource(PlanEntrySourceSlashCommand))
	require.True(t, IsPlanEntrySource(PlanEntrySourcePlanToggle))
	require.False(t, IsPlanEntrySource(""))
	require.False(t, IsPlanEntrySource("auto"))
}

// TestResolveAgentRequestMode 验证 mode 解析与上游 resolve_agent_request_mode 一致。
//
// 期望值由上游 gateway/common/mode_matrix 的 canonicalize_mode_text +
// resolve_new_canonical_mode 实算得出（legacy canonical 原样返回，不升级三段）。
func TestResolveAgentRequestMode(t *testing.T) {
	cases := []struct {
		name                       string
		raw                        any
		work                       string
		wantMode, wantSub, wantCan string
	}{
		{"空值默认 agent", "", "", "agent", "", "agent"},
		{"nil 默认 agent", nil, "", "agent", "", "agent"},
		{"裸 agent", "agent", "", "agent", "", "agent"},
		{"裸 plan 不下沉 plan", "plan", "", "agent", "", "agent"},
		{"三段 plan", "agent.work.plan", "", "agent", "plan", "agent.work.plan"},
		{"三段 normal", "agent.work.normal", "", "agent", "", "agent.work.normal"},
		{"code.normal 保留 legacy canonical", "code.normal", "", "code", "normal", "code.normal"},
		{"code 裸串补 normal", "code", "", "code", "normal", "code.normal"},
		{"team.plan 归一 legacy canonical", "team.plan", "", "team", "plan", "team.plan.normal"},
		{"team.plan.code", "team.plan.code", "", "code", "team", "team.plan.code"},
		{"auto_harness", "auto_harness", "", "auto_harness", "auto_harness", "auto_harness"},
		{"agent.plan 保留 plan", "agent.plan", "", "agent", "plan", "agent.plan"},
		{"plan + work_mode=code", "plan", "code", "code", "normal", "code.normal"},
		{"agent + work_mode=code 下沉 code", "agent", "code", "code", "normal", "code.normal"},
		{"agent.plan + work_mode=code", "agent.plan", "code", "code", "plan", "code.plan"},
		{"code + work_mode=work 回升 agent", "code", "work", "agent", "", "agent"},
		{"team 族 work", "team", "work", "team", "", "team"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, sub, canonical := ResolveAgentRequestMode(tc.raw, tc.work)
			require.Equal(t, tc.wantMode, mode)
			require.Equal(t, tc.wantSub, sub)
			require.Equal(t, tc.wantCan, canonical)
		})
	}
}

// TestApplyResolvedModeToRequest 验证 canonical mode 回写到 params。
func TestApplyResolvedModeToRequest(t *testing.T) {
	req := &schema.AgentRequest{Params: json.RawMessage(`{"mode":" Agent.Work.Plan ","query":"hi"}`)}

	mode, subMode := ApplyResolvedModeToRequest(req, "")
	require.Equal(t, "agent", mode)
	require.Equal(t, "plan", subMode)

	params := parseRequestParams(req)
	require.Equal(t, "agent.work.plan", params["mode"])
	require.Equal(t, "hi", params["query"])
}

// TestResolveRequestRuntimeModeFromRequest 验证权威模式视图。
func TestResolveRequestRuntimeModeFromRequest(t *testing.T) {
	req := &schema.AgentRequest{Params: json.RawMessage(`{"mode":"agent.work.plan"}`)}
	resolved := ResolveRequestRuntimeModeFromRequest(req)
	require.Equal(t, "agent.work.plan", resolved.CanonicalMode)
	require.True(t, resolved.IsPlan)
	require.False(t, resolved.IsTeam)
	require.Equal(t, "agent.work.normal", resolved.NormalMode)

	team := &schema.AgentRequest{Params: json.RawMessage(`{"mode":"team.code.normal"}`)}
	require.True(t, ResolveRequestRuntimeModeFromRequest(team).IsTeam)
}

// TestResolveRequestRuntimeModeFromRequest_Web组合 验证 mode+work_mode 组合优先于 legacy 解析。
//
// Python: mode_matrix.resolve_request_mode 的 web-composition 分支（_WEB_MODE_TABLE）。
func TestResolveRequestRuntimeModeFromRequest_Web组合(t *testing.T) {
	cases := []struct {
		name, mode, work     string
		wantManager, wantSub string
		wantCanonical        string
		wantPlan, wantTeam   bool
	}{
		{"agent+code 走 code profile", "agent", "code", "code", "normal", "code.normal", false, false},
		{"agent+work 保持 agent", "agent", "work", "agent", "", "agent", false, false},
		{"agent.plan+code 保留 plan", "agent.plan", "code", "code", "plan", "code.plan", true, false},
		{"team+code 走 code team", "team", "code", "code", "team", "code.team", false, true},
		{"三段 plan+work 短路", "agent.work.plan", "work", "agent", "plan", "agent.work.plan", true, false},
		{"三段 plan+code 不进组合表", "agent.work.plan", "code", "agent", "plan", "agent.work.plan", true, false},
		{"非组合 mode 走 legacy", "code", "work", "agent", "", "agent", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params, err := json.Marshal(map[string]any{"mode": tc.mode, "work_mode": tc.work})
			require.NoError(t, err)
			resolved := ResolveRequestRuntimeModeFromRequest(&schema.AgentRequest{Params: params})
			require.Equal(t, tc.wantManager, resolved.ManagerMode)
			require.Equal(t, tc.wantSub, resolved.SubMode)
			require.Equal(t, tc.wantCanonical, resolved.CanonicalMode)
			require.Equal(t, tc.wantPlan, resolved.IsPlan)
			require.Equal(t, tc.wantTeam, resolved.IsTeam)
		})
	}
}

// TestApplyResolvedModeToRequest_默认值 验证空参数回落 agent（normal），不再默认 plan。
//
// Python: resolve_agent_request_mode 空值 → ("agent", None, "agent")
func TestApplyResolvedModeToRequest_默认值(t *testing.T) {
	cases := []struct {
		name          string
		params        json.RawMessage
		wantMode      string
		wantSub       string
		wantCanonical string
	}{
		{"空参数", json.RawMessage(`{}`), "agent", "", "agent"},
		{"nil 参数", nil, "agent", "", "agent"},
		{"空 mode", json.RawMessage(`{"mode": ""}`), "agent", "", "agent"},
		{"agent.code.normal", json.RawMessage(`{"mode": "agent.code.normal"}`), "code", "normal", "agent.code.normal"},
		{"agent.work.plan", json.RawMessage(`{"mode": "agent.work.plan"}`), "agent", "plan", "agent.work.plan"},
		{"裸 code", json.RawMessage(`{"mode": "code"}`), "code", "normal", "code.normal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &schema.AgentRequest{Params: tc.params}
			mode, subMode := ApplyResolvedModeToRequest(req, "")
			require.Equal(t, tc.wantMode, mode)
			require.Equal(t, tc.wantSub, subMode)
			if tc.params != nil {
				require.Equal(t, tc.wantCanonical, parseRequestParams(req)["mode"])
			}
		})
	}
}

// TestResolveRequestProjectDir 验证工程目录解析（上游 5 级 fallback + projectless 开关）。
func TestResolveRequestProjectDir(t *testing.T) {
	cases := []struct {
		name    string
		req     *schema.AgentRequest
		include bool
		want    string
	}{
		{
			name:    "params project_dir 优先",
			req:     &schema.AgentRequest{Params: json.RawMessage(`{"project_dir": "/tmp/p","cwd":"/tmp/c"}`)},
			include: false,
			want:    "/tmp/p",
		},
		{
			name:    "metadata project_dir",
			req:     &schema.AgentRequest{Metadata: map[string]any{"project_dir": "/tmp/m"}},
			include: false,
			want:    "/tmp/m",
		},
		{
			name:    "projectless 不回落 cwd",
			req:     &schema.AgentRequest{Params: json.RawMessage(`{"cwd": "/tmp/c"}`)},
			include: false,
			want:    "",
		},
		{
			name:    "legacy 回落 cwd",
			req:     &schema.AgentRequest{Params: json.RawMessage(`{"cwd": "/tmp/c"}`)},
			include: true,
			want:    "/tmp/c",
		},
		{
			name:    "legacy 回落 trusted_dirs 首项",
			req:     &schema.AgentRequest{Params: json.RawMessage(`{"trusted_dirs": ["/tmp/t1","/tmp/t2"]}`)},
			include: true,
			want:    "/tmp/t1",
		},
		{
			name:    "全空",
			req:     &schema.AgentRequest{Params: json.RawMessage(`{}`)},
			include: true,
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ResolveRequestProjectDir(tc.req, tc.include))
		})
	}
}

// TestResolveCancelProjectDir 验证取消请求的工程键解析（无注册表时回落会话 metadata）。
func TestResolveCancelProjectDir(t *testing.T) {
	req := &schema.AgentRequest{Params: json.RawMessage(`{"project_dir": "/tmp/p"}`)}
	require.Equal(t, "/tmp/p", ResolveCancelProjectDir(req))

	sid := "no-such-session"
	empty := &schema.AgentRequest{SessionID: &sid, Params: json.RawMessage(`{}`)}
	require.Equal(t, "", ResolveCancelProjectDir(empty))
	require.Equal(t, "", ResolveCancelProjectDir(nil))
}

// ──────────────────────────── metadata 同步 / 取消 ────────────────────────────

// setupRuntimeSessionDir 把全局 sessions 目录指向测试临时目录。
func setupRuntimeSessionDir(t *testing.T) {
	t.Helper()
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	pathutil.ResetCache()
	session.ClearAllSessionMetadataCache()
	t.Cleanup(func() {
		session.FlushMetadataQueue()
		session.ClearAllSessionMetadataCache()
		pathutil.ResetCache()
	})
}

// TestSyncChatRequestMetadata_首次锁定 验证 project_dir/mode/work_mode 首次写入与不可改。
func TestSyncChatRequestMetadata_首次锁定(t *testing.T) {
	setupRuntimeSessionDir(t)
	sid := "sess-sync-1"
	req := chatSendRequest(t, sid, map[string]any{
		"mode": "agent.work.plan", "model_name": "m1", "project_dir": "/tmp/p1",
	})
	req.ReqMethod = schema.ReqMethodChatSend

	locked := SyncChatRequestMetadata(req, "/tmp/p1", "agent.work.plan", true, "u1")
	require.Equal(t, "/tmp/p1", locked)
	session.FlushMetadataQueue()

	meta := session.GetSessionMetadata(sid)
	require.Equal(t, "agent.work.plan", meta["mode"])
	require.Equal(t, "/tmp/p1", meta["project_dir"])
	require.Equal(t, "m1", meta["model"])
	require.Equal(t, "u1", meta["user_id"])
	require.Contains(t, meta, "last_user_message_at")

	// 第二次带不同 project_dir / mode：锁定不可改，只告警
	req2 := chatSendRequest(t, sid, map[string]any{"mode": "agent.work.normal", "project_dir": "/tmp/p2"})
	req2.ReqMethod = schema.ReqMethodChatSend
	locked2 := SyncChatRequestMetadata(req2, "/tmp/p2", "agent.work.normal", true, "")
	session.FlushMetadataQueue()
	require.Equal(t, "/tmp/p1", locked2)

	meta2 := session.GetSessionMetadata(sid)
	require.Equal(t, "/tmp/p1", meta2["project_dir"])
	// mode 是显式覆盖式：显式携带时覆盖
	require.Equal(t, "agent.work.normal", meta2["mode"])
}

// TestSyncChatRequestMetadata_非显式mode不覆盖 验证只读 RPC 不腐蚀已锁定 mode。
func TestSyncChatRequestMetadata_非显式mode不覆盖(t *testing.T) {
	setupRuntimeSessionDir(t)
	sid := "sess-sync-2"
	seed := chatSendRequest(t, sid, map[string]any{"mode": "team.work.plan"})
	seed.ReqMethod = schema.ReqMethodChatSend
	SyncChatRequestMetadata(seed, "", "team.work.plan", true, "")
	session.FlushMetadataQueue()

	// 未显式携带 mode：传入推断值也不得写盘
	ro := chatSendRequest(t, sid, map[string]any{})
	ro.ReqMethod = schema.ReqMethodSessionList
	SyncChatRequestMetadata(ro, "", "agent.work.normal", false, "")
	session.FlushMetadataQueue()

	require.Equal(t, "team.work.plan", session.GetSessionMetadata(sid)["mode"])
}

// TestSyncChatRequestMetadata_attachOnly只读 验证被动订阅不写元数据。
func TestSyncChatRequestMetadata_attachOnly只读(t *testing.T) {
	setupRuntimeSessionDir(t)
	sid := "sess-sync-3"
	seed := chatSendRequest(t, sid, map[string]any{"project_dir": "/tmp/locked"})
	seed.ReqMethod = schema.ReqMethodChatSend
	SyncChatRequestMetadata(seed, "/tmp/locked", "agent.work.normal", true, "")
	session.FlushMetadataQueue()
	before := session.GetSessionMetadata(sid)["last_message_at"]

	attach := chatSendRequest(t, sid, map[string]any{TeamAttachOnlyParam: true, "project_dir": "/tmp/other"})
	attach.ReqMethod = schema.ReqMethodChatSend
	locked := SyncChatRequestMetadata(attach, "/tmp/other", "agent.work.normal", true, "")
	session.FlushMetadataQueue()

	require.Equal(t, "/tmp/locked", locked)
	require.Equal(t, before, session.GetSessionMetadata(sid)["last_message_at"])
}

// TestSyncChatRequestMetadata_只读方法不刷新时间戳 验证 last_message_at 仅 chat 轮次刷新。
func TestSyncChatRequestMetadata_只读方法不刷新时间戳(t *testing.T) {
	setupRuntimeSessionDir(t)
	sid := "sess-sync-4"
	seed := chatSendRequest(t, sid, map[string]any{})
	seed.ReqMethod = schema.ReqMethodChatSend
	SyncChatRequestMetadata(seed, "", "agent.work.normal", true, "")
	session.FlushMetadataQueue()
	before := session.GetSessionMetadata(sid)["last_message_at"]

	ro := chatSendRequest(t, sid, map[string]any{})
	ro.ReqMethod = schema.ReqMethodSessionList
	SyncChatRequestMetadata(ro, "", "agent.work.normal", true, "")
	session.FlushMetadataQueue()

	require.Equal(t, before, session.GetSessionMetadata(sid)["last_message_at"])
}

// TestCancelRequest_无agent不新建 验证 allowCreate=false 时返回 interrupt_result。
func TestCancelRequest_无agent不新建(t *testing.T) {
	mgr := &stubManager{}
	resp, err := CancelRequest(context.Background(), mgr, chatSendRequest(t, "sess-cancel", map[string]any{"mode": "agent"}), false)
	require.NoError(t, err)
	require.True(t, resp.OK)
	require.Equal(t, "chat.interrupt_result", resp.Payload["event_type"])
	require.Equal(t, true, resp.Payload["success"])
	require.Equal(t, "当前会话任务已终止", resp.Payload["message"])
	require.Equal(t, 0, mgr.created)
}

// TestCancelRequest_allowCreate兜底 验证 allowCreate=true 时创建并处理。
func TestCancelRequest_allowCreate兜底(t *testing.T) {
	agent := &fakePlanAgent{}
	mgr := &stubManager{agent: agent}
	req := chatSendRequest(t, "sess-cancel-2", map[string]any{"mode": "agent", "work_mode": "work"})
	resp, err := CancelRequest(context.Background(), mgr, req, true)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 1, mgr.created)
	require.Equal(t, []string{"execute"}, agent.calls)
}

// stubManager 可配置的 AgentManagerPort 桩。
type stubManager struct {
	// nowait GetChatAgentNoWait 的返回值（nil 表示无既有 agent）
	nowait ChatAgentPort
	// agent GetChatAgent 的返回值
	agent ChatAgentPort
	// created 记录 GetChatAgent 调用次数
	created int
}

// GetChatAgent 记录创建次数并返回 agent
func (m *stubManager) GetChatAgent(_ context.Context, _, _, _, _ string) (ChatAgentPort, error) {
	m.created++
	return m.agent, nil
}

// GetChatAgentNoWait 返回预置 agent（可为 nil）
func (m *stubManager) GetChatAgentNoWait(_, _, _, _ string) ChatAgentPort { return m.nowait }

// chatSendRequest 构造带 params 的 chat 请求。
func chatSendRequest(t *testing.T, sessionID string, params map[string]any) *schema.AgentRequest {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	sid := sessionID
	return &schema.AgentRequest{
		RequestID: "req-cancel", ChannelID: "web", SessionID: &sid,
		ReqMethod: schema.ReqMethodChatSend, Params: raw,
	}
}
