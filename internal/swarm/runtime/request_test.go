package runtime

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
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
func TestResolveAgentRequestMode(t *testing.T) {
	cases := []struct {
		name                       string
		raw                        any
		work                       string
		wantMode, wantSub, wantCan string
	}{
		{"空值默认 agent", "", "", "agent", "", "agent"},
		{"nil 默认 agent", nil, "", "agent", "", "agent"},
		{"裸 agent canonical 短路", "agent", "", "agent", "", "agent.work.normal"},
		{"裸 plan 不下沉 plan", "plan", "", "agent", "", "agent"},
		{"三段 plan", "agent.work.plan", "", "agent", "plan", "agent.work.plan"},
		{"三段 normal", "agent.work.normal", "", "agent", "", "agent.work.normal"},
		{"code.normal", "code.normal", "", "code", "normal", "agent.code.normal"},
		{"code 裸串补 normal", "code", "", "code", "normal", "agent.code.normal"},
		{"team.plan 归一", "team.plan", "", "team", "plan", "team.work.plan"},
		{"auto_harness", "auto_harness", "", "auto_harness", "auto_harness", "auto_harness"},
		{"agent.plan 保留 plan", "agent.plan", "", "agent", "plan", "agent.work.plan"},
		{"plan + work_mode=code", "plan", "code", "code", "normal", "code.normal"},
		{"agent + work_mode=code（canonical 短路优先）", "agent", "code", "agent", "", "agent.work.normal"},
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
		{"裸 code", json.RawMessage(`{"mode": "code"}`), "code", "normal", "agent.code.normal"},
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
