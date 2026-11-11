package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// TestResolveRequestRuntimeMode 覆盖三段/裸 subMode/旧串回落的归一。
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
		{"code.normal + work_mode=code 保持 code", "code.normal", "code", "code", "normal", "code.normal"},
		{"code.normal + work_mode=work 回升 agent", "code.normal", "work", "agent", "", "agent"},
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
		session.FlushMetadataQueue(5.0)
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
	session.FlushMetadataQueue(5.0)

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
	session.FlushMetadataQueue(5.0)
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
	session.FlushMetadataQueue(5.0)

	// 未显式携带 mode：传入推断值也不得写盘
	ro := chatSendRequest(t, sid, map[string]any{})
	ro.ReqMethod = schema.ReqMethodSessionList
	SyncChatRequestMetadata(ro, "", "agent.work.normal", false, "")
	session.FlushMetadataQueue(5.0)

	require.Equal(t, "team.work.plan", session.GetSessionMetadata(sid)["mode"])
}

// TestSyncChatRequestMetadata_attachOnly只读 验证被动订阅不写元数据。
func TestSyncChatRequestMetadata_attachOnly只读(t *testing.T) {
	setupRuntimeSessionDir(t)
	sid := "sess-sync-3"
	seed := chatSendRequest(t, sid, map[string]any{"project_dir": "/tmp/locked"})
	seed.ReqMethod = schema.ReqMethodChatSend
	SyncChatRequestMetadata(seed, "/tmp/locked", "agent.work.normal", true, "")
	session.FlushMetadataQueue(5.0)
	before := session.GetSessionMetadata(sid)["last_message_at"]

	attach := chatSendRequest(t, sid, map[string]any{TeamAttachOnlyParam: true, "project_dir": "/tmp/other"})
	attach.ReqMethod = schema.ReqMethodChatSend
	locked := SyncChatRequestMetadata(attach, "/tmp/other", "agent.work.normal", true, "")
	session.FlushMetadataQueue(5.0)

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
	session.FlushMetadataQueue(5.0)
	before := session.GetSessionMetadata(sid)["last_message_at"]

	ro := chatSendRequest(t, sid, map[string]any{})
	ro.ReqMethod = schema.ReqMethodSessionList
	SyncChatRequestMetadata(ro, "", "agent.work.normal", true, "")
	session.FlushMetadataQueue(5.0)

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
	// agentErr GetChatAgent 的错误
	agentErr error
	// created 记录 GetChatAgent 调用次数
	created int
}

// GetChatAgent 记录创建次数并返回 agent
func (m *stubManager) GetChatAgent(_ context.Context, _, _, _, _ string, _ map[string]any, _ string) (ChatAgentPort, error) {
	m.created++
	return m.agent, m.agentErr
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

// ──────────────────────────── prepare_chat_turn 组装对话轮次 ────────────────────────────

// TestPrepareChatTurn_默认work_mode与元数据回写 验证 tui 渠道默认 work_mode=code、
// 同步元数据后回写 project_dir，以及 persist_session 锁定告警分支。
// Python: jiuwenswarm/runtime/request.py prepare_chat_turn（L361-575）
func TestPrepareChatTurn_默认work_mode与元数据回写(t *testing.T) {
	setupRuntimeSessionDir(t)
	sid := "sess-prepare-1"
	agent := &fakePlanAgent{}
	mgr := &stubManager{agent: agent}
	req := chatSendRequest(t, sid, map[string]any{"mode": "agent.work.normal"})

	mode, subMode, got, err := PrepareChatTurn(context.Background(), mgr, req, "tui", true, nil, "")
	require.NoError(t, err)
	require.Equal(t, "agent", mode)
	require.Equal(t, "", subMode)
	require.Same(t, agent, got)
	require.Equal(t, 1, mgr.created)

	params := parseRequestParams(req)
	require.Equal(t, "code", params["work_mode"], "tui 渠道缺省 work_mode=code")
	require.Equal(t, "agent.work.normal", params["mode"])
	require.NotContains(t, params, "project_dir", "无工程目录时应清除 params 中的键")

	session.FlushMetadataQueue(5.0)
	meta := session.GetSessionMetadata(sid)
	require.Equal(t, "agent.work.normal", meta["mode"])
	require.Equal(t, "code", meta["work_mode"])
}

// TestPrepareChatTurn_persistSession锁定告警 会话创建期锁定的 persist_session 不被后续轮次改写。
func TestPrepareChatTurn_persistSession锁定告警(t *testing.T) {
	setupRuntimeSessionDir(t)
	sid := "sess-prepare-lock"

	// 首轮不带 persist 标记，锁定为 false
	first := chatSendRequest(t, sid, map[string]any{"mode": "agent.work.normal"})
	_, _, _, err := PrepareChatTurn(context.Background(), &stubManager{agent: &fakePlanAgent{}}, first, "web", true, nil, "")
	require.NoError(t, err)
	session.FlushMetadataQueue(5.0)

	// 第二轮请求要求开启：仅告警，params 以锁定的 false 为准
	second := chatSendRequest(t, sid, map[string]any{
		"mode":                         "agent.work.normal",
		"persist_session":              true,
		"eternal_conversation_enabled": true,
	})
	_, _, _, err = PrepareChatTurn(context.Background(), &stubManager{agent: &fakePlanAgent{}}, second, "web", true, nil, "")
	require.NoError(t, err)
	require.Equal(t, false, parseRequestParams(second)["eternal_conversation_enabled"],
		"persist_session 为会话创建期身份，本轮值仅告警")

	session.FlushMetadataQueue(5.0)
	require.NotEqual(t, true, session.GetSessionMetadata(sid)["persist_session"])
}

// TestPrepareChatTurn_继承会话锁定的mode 验证内部轮次（无显式 mode）沿用会话 mode。
func TestPrepareChatTurn_继承会话锁定的mode(t *testing.T) {
	setupRuntimeSessionDir(t)
	sid := "sess-prepare-2"
	seed := chatSendRequest(t, sid, map[string]any{"mode": "agent.work.plan", "work_mode": "work"})
	SyncChatRequestMetadata(seed, "", "agent.work.plan", true, "")
	session.FlushMetadataQueue(5.0)

	req := chatSendRequest(t, sid, map[string]any{})
	req.ReqMethod = schema.ReqMethodHeartbeatGetConf
	_, _, agent, err := PrepareChatTurn(context.Background(), &stubManager{agent: &fakePlanAgent{}}, req, "web", false, nil, "")
	require.NoError(t, err)
	require.NotNil(t, agent)

	params := parseRequestParams(req)
	require.Equal(t, "agent.work.plan", params["mode"], "无显式 mode 时继承会话锁定值")
	require.Equal(t, "agent.work.plan", params[PreviousSessionModeKey])
	require.Equal(t, "work", params["work_mode"])
}

// TestPrepareChatTurn_不同步元数据回落锁定工程 验证 sync_metadata=false 时只读回退。
func TestPrepareChatTurn_不同步元数据回落锁定工程(t *testing.T) {
	setupRuntimeSessionDir(t)
	sid := "sess-prepare-3"
	seed := chatSendRequest(t, sid, map[string]any{"mode": "agent.work.normal", "project_dir": "/tmp/locked"})
	SyncChatRequestMetadata(seed, "/tmp/locked", "agent.work.normal", true, "")
	session.FlushMetadataQueue(5.0)

	req := chatSendRequest(t, sid, map[string]any{})
	_, _, _, err := PrepareChatTurn(context.Background(), &stubManager{agent: &fakePlanAgent{}}, req, "web", false, nil, "")
	require.NoError(t, err)
	require.Equal(t, "/tmp/locked", parseRequestParams(req)["project_dir"])
}

// TestPrepareChatTurn_请求work_mode优先 验证请求自带 work_mode 不被渠道默认覆盖。
func TestPrepareChatTurn_请求work_mode优先(t *testing.T) {
	setupRuntimeSessionDir(t)
	req := chatSendRequest(t, "sess-prepare-4", map[string]any{"mode": "agent", "work_mode": "work"})
	mode, subMode, _, err := PrepareChatTurn(context.Background(), &stubManager{agent: &fakePlanAgent{}}, req, "tui", false, nil, "")
	require.NoError(t, err)
	require.Equal(t, "agent", mode)
	require.Equal(t, "", subMode)
	require.Equal(t, "work", parseRequestParams(req)["work_mode"])
}

// TestPrepareChatTurn_取agent失败 验证构造 agent 失败时错误上抛。
func TestPrepareChatTurn_取agent失败(t *testing.T) {
	setupRuntimeSessionDir(t)
	mgr := &stubManager{agentErr: errors.New("boom")}
	req := chatSendRequest(t, "sess-prepare-5", map[string]any{"mode": "agent"})

	_, _, _, err := PrepareChatTurn(context.Background(), mgr, req, "web", false, nil, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "boom")
}

// ──────────────────────────── 辅助函数边界 ────────────────────────────

// TestDefaultWorkModeForChannel 渠道默认 work_mode：tui → code，其余 work。
func TestDefaultWorkModeForChannel(t *testing.T) {
	require.Equal(t, "code", defaultWorkModeForChannel("tui"))
	require.Equal(t, "code", defaultWorkModeForChannel(" TUI "))
	require.Equal(t, "work", defaultWorkModeForChannel("web"))
	require.Equal(t, "work", defaultWorkModeForChannel(""))
}

// TestRequestUserID user_id 依次取 Metadata、params。
func TestRequestUserID(t *testing.T) {
	require.Equal(t, "", requestUserID(nil))
	require.Equal(t, "", requestUserID(&schema.AgentRequest{}))
	require.Equal(t, "u-meta", requestUserID(&schema.AgentRequest{
		Metadata: map[string]any{"user_id": "u-meta"},
		Params:   json.RawMessage(`{"user_id":"u-param"}`),
	}))
	require.Equal(t, "u-param", requestUserID(&schema.AgentRequest{
		Params: json.RawMessage(`{"user_id":"u-param"}`),
	}))
}

// TestLegacyPersistSession 旧式 eternal_conversation_enabled 取值归一。
func TestLegacyPersistSession(t *testing.T) {
	require.Nil(t, legacyPersistSession(nil))
	require.Nil(t, legacyPersistSession("maybe"))
	require.Nil(t, legacyPersistSession(1))

	yes := legacyPersistSession(" YES ")
	require.NotNil(t, yes)
	require.True(t, *yes)

	off := legacyPersistSession("off")
	require.NotNil(t, off)
	require.False(t, *off)

	truthy := legacyPersistSession(true)
	require.NotNil(t, truthy)
	require.True(t, *truthy)
}

// TestCancelRequest_autoHarness需新建 验证 auto_harness 归一为 agent 且 allowCreate 路径。
func TestCancelRequest_autoHarness需新建(t *testing.T) {
	agent := &fakePlanAgent{}
	mgr := &stubManager{agent: agent}
	req := chatSendRequest(t, "sess-cancel-3", map[string]any{"mode": "auto_harness"})

	resp, err := CancelRequest(context.Background(), mgr, req, true)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 1, mgr.created)
	require.Equal(t, []string{"execute"}, agent.calls)
}

// TestCancelRequest_新建失败上抛 验证 allowCreate 创建 agent 失败时返回错误。
func TestCancelRequest_新建失败上抛(t *testing.T) {
	mgr := &stubManager{agentErr: errors.New("create failed")}
	req := chatSendRequest(t, "sess-cancel-4", map[string]any{"mode": "agent"})

	_, err := CancelRequest(context.Background(), mgr, req, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "create failed")
}

// TestCancelRequest_新建返回空 验证创建成功但 agent 为空时的兜底错误。
func TestCancelRequest_新建返回空(t *testing.T) {
	mgr := &stubManager{}
	req := chatSendRequest(t, "sess-cancel-5", map[string]any{"mode": "agent"})

	_, err := CancelRequest(context.Background(), mgr, req, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Failed to get agent for cancel request")
}

// TestSyncChatRequestMetadata_attachOnly无锁定目录 被动订阅未锁定工程时返回入参目录。
func TestSyncChatRequestMetadata_attachOnly无锁定目录(t *testing.T) {
	setupRuntimeSessionDir(t)
	req := chatSendRequest(t, "sess-attach-new", map[string]any{TeamAttachOnlyParam: true})

	require.Equal(t, "/tmp/fallback", SyncChatRequestMetadata(req, "/tmp/fallback", "agent.work.normal", true, ""))
	session.FlushMetadataQueue(5.0)
	require.Empty(t, session.GetSessionMetadata("sess-attach-new"), "attach-only 不写任何元数据")
}
