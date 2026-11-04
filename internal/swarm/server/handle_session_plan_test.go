package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试 ────────────────────────────

// TestHandleSessionPlanStatus_in_plan契约 验证返回键为 in_plan（前端读 payload.in_plan）。
func TestHandleSessionPlanStatus_in_plan契约(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sid := "sess-plan-status"
	writeTestMetadata(t, sessionsDir, sid, map[string]any{
		"session_id": sid, "channel_id": "web", "mode": "agent.work.plan",
	})

	resp, err := s.handleSessionPlanStatus(context.Background(), planStatusRequest(t, map[string]any{"session_id": sid}))
	require.NoError(t, err)
	require.True(t, resp.OK)
	require.Equal(t, sid, resp.Payload["session_id"])
	require.Equal(t, true, resp.Payload["in_plan"])
	_, hasPlanMode := resp.Payload["plan_mode"]
	require.False(t, hasPlanMode, "契约键只有 session_id/in_plan")
}

// TestHandleSessionPlanStatus_缺session_id 验证 BAD_REQUEST。
func TestHandleSessionPlanStatus_缺session_id(t *testing.T) {
	_, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	resp, err := s.handleSessionPlanStatus(context.Background(), planStatusRequest(t, map[string]any{}))
	require.NoError(t, err)
	require.False(t, resp.OK)
	require.Equal(t, "BAD_REQUEST", resp.Payload["code"])
}

// TestHandleSessionPlanStatus_会话不存在 验证 NOT_FOUND。
func TestHandleSessionPlanStatus_会话不存在(t *testing.T) {
	_, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	resp, err := s.handleSessionPlanStatus(context.Background(), planStatusRequest(t, map[string]any{"session_id": "missing-session-x"}))
	require.NoError(t, err)
	require.False(t, resp.OK)
	require.Equal(t, "NOT_FOUND", resp.Payload["code"])
}

// TestHandleSessionPlanStatus_team以metadata为准 验证 team 会话不被 live plan_mode 覆盖。
func TestHandleSessionPlanStatus_team以metadata为准(t *testing.T) {
	sessionsDir, s, cleanup := setupTestSessionsDir(t)
	defer cleanup()

	sid := "sess-team-plan"
	writeTestMetadata(t, sessionsDir, sid, map[string]any{
		"session_id": sid, "channel_id": "web", "mode": "team.work.plan",
	})

	resp, err := s.handleSessionPlanStatus(context.Background(), planStatusRequest(t, map[string]any{"session_id": sid}))
	require.NoError(t, err)
	require.True(t, resp.OK)
	require.Equal(t, true, resp.Payload["in_plan"])
}

// TestCombineSessionInPlan 验证 team/live/active/metadata 的合并优先级。
func TestCombineSessionInPlan(t *testing.T) {
	// team 会话忽略 live，以 active 或 metadata 为准
	require.True(t, combineSessionInPlan("normal", false, "team.work.plan"))
	require.False(t, combineSessionInPlan("plan", false, "team.work.normal"))
	// 非 team：live 优先
	require.False(t, combineSessionInPlan("normal", true, "agent.work.plan"))
	require.True(t, combineSessionInPlan("plan", false, "agent.work.normal"))
	// 无 live 时 active 兜底
	require.True(t, combineSessionInPlan("", true, "agent.work.normal"))
	require.False(t, combineSessionInPlan("", false, "agent.work.normal"))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// planStatusRequest 构造 session.plan_status 请求。
func planStatusRequest(t *testing.T, params map[string]any) *schema.AgentRequest {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	return &schema.AgentRequest{
		RequestID: "req-plan-status",
		ChannelID: "web",
		ReqMethod: schema.ReqMethodSessionPlanStatus,
		Params:    raw,
	}
}
