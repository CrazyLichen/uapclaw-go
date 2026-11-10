package runtime

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeGoalBusyManager 只实现 HasActiveGoal 的管理器替身。
// 内嵌 fakeGuardAgentManager 拿到其余方法的最小实现。
type fakeGoalBusyManager struct {
	fakeGuardAgentManager
	// active HasActiveGoal 的返回值
	active bool
}

// HasActiveGoal 返回预置的 goal 活跃状态。
func (m *fakeGoalBusyManager) HasActiveGoal(string, string) bool { return m.active }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSessionMessageRequiresQueue_goal活跃时排队 跨会话消息不得打断 goal。
// Python: service.py:513-518 session_message_requires_queue
func TestSessionMessageRequiresQueue_goal活跃时排队(t *testing.T) {
	busy := NewAgentRuntime(AgentRuntimeDeps{Manager: &fakeGoalBusyManager{active: true}})
	require.True(t, busy.sessionMessageRequiresQueue("sess-busy"))

	idle := NewAgentRuntime(AgentRuntimeDeps{Manager: &fakeGoalBusyManager{active: false}})
	require.False(t, idle.sessionMessageRequiresQueue("sess-idle"))
}

// TestSessionMessageRequiresQueue_plan活跃时排队 plan 会话同样要求排队。
// Python: service.py:515-517 `session_id in self._plan_controller.active_sessions`
func TestSessionMessageRequiresQueue_plan活跃时排队(t *testing.T) {
	plan := NewPlanModeController()
	plan.markActive("sess-plan")

	r := NewAgentRuntime(AgentRuntimeDeps{
		Manager:        &fakeGoalBusyManager{active: false},
		PlanController: plan,
	})
	require.True(t, r.sessionMessageRequiresQueue("sess-plan"), "plan 活跃时必须排队")
	require.False(t, r.sessionMessageRequiresQueue("sess-other"), "无关会话不排队")
}

// TestRequireCrossSessionInputAdmission_拒收 plan/goal 活跃时的跨会话消息。
// Python: service.py:520-530 _require_cross_session_input_admission
func TestRequireCrossSessionInputAdmission_拒收(t *testing.T) {
	r := NewAgentRuntime(AgentRuntimeDeps{Manager: &fakeGoalBusyManager{active: true}})
	req := crossSessionInputRequest("sess-busy", "agent")

	err := r.requireCrossSessionInputAdmission(req)
	require.Error(t, err)

	var queueErr *SessionInputQueueRequiredError
	require.True(t, errors.As(err, &queueErr), "必须可识别为 QueueRequired")
	require.Equal(t, sessionInputQueueRequiredCode, queueErr.Code())
}

// TestRequireCrossSessionInputAdmission_放行 非跨会话消息或 goal 空闲时放行。
func TestRequireCrossSessionInputAdmission_放行(t *testing.T) {
	idle := NewAgentRuntime(AgentRuntimeDeps{Manager: &fakeGoalBusyManager{active: false}})
	require.NoError(t, idle.requireCrossSessionInputAdmission(
		crossSessionInputRequest("sess-idle", "agent")),
		"goal 空闲时跨会话消息不排队")

	busy := NewAgentRuntime(AgentRuntimeDeps{Manager: &fakeGoalBusyManager{active: true}})
	require.NoError(t, busy.requireCrossSessionInputAdmission(
		plainInputRequest("sess-busy")),
		"非跨会话消息不受 goal 影响")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// crossSessionInputRequest 构造带跨会话载荷的补充输入请求。
func crossSessionInputRequest(sessionID, mode string) *schema.AgentRequest {
	sid := sessionID
	params := map[string]any{
		"query":      "插一句",
		"input_mode": "steer",
		"mode":       mode,
		sessionMessageInternalKey: map[string]any{
			"message_id": "msg-1",
			"content":    "插一句",
		},
	}
	req := &schema.AgentRequest{
		RequestID: "req-busy",
		ChannelID: "itest-channel",
		ReqMethod: schema.ReqMethodChatSend,
		SessionID: &sid,
	}
	writeRequestParams(req, params)
	return req
}

// plainInputRequest 构造不带跨会话载荷的补充输入请求。
func plainInputRequest(sessionID string) *schema.AgentRequest {
	sid := sessionID
	req := &schema.AgentRequest{
		RequestID: "req-plain",
		ChannelID: "itest-channel",
		ReqMethod: schema.ReqMethodChatSend,
		SessionID: &sid,
	}
	writeRequestParams(req, map[string]any{"query": "继续", "input_mode": "steer"})
	return req
}
