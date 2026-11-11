package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// goalGuardRequest 构造带 params 的请求。
func goalGuardRequest(method schema.ReqMethod, params map[string]any) *schema.AgentRequest {
	req := &schema.AgentRequest{ReqMethod: method}
	if params != nil {
		raw, _ := json.Marshal(params)
		req.Params = raw
	}
	return req
}

// TestIsReadonlyGoalGetRequest 只有 command.goal 且 action=get（含缺省）为真。
// Python: service.py:4321 _is_readonly_goal_get_request
func TestIsReadonlyGoalGetRequest(t *testing.T) {
	cases := []struct {
		name   string
		req    *schema.AgentRequest
		expect bool
	}{
		{"nil请求", nil, false},
		{"非command.goal", goalGuardRequest(schema.ReqMethodChatSend, map[string]any{"action": "get"}), false},
		{"action缺省", goalGuardRequest(schema.ReqMethodCommandGoal, nil), true},
		{"action空串", goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": ""}), true},
		{"action等于get", goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": "get"}), true},
		{"action带空格且大写", goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": " GET "}), true},
		{"action等于set", goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": "set", "objective": "x"}), false},
		{"action等于resume", goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": "resume"}), false},
		{"action等于pause", goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": "pause"}), false},
		{"action等于clear", goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": "clear"}), false},
		// 非字符串 action 是协议违规（任何客户端都发不出），按包内既有
		// stringParam 约定归一为空串，等价于缺省 get。同 session_work_kind.go:69。
		{"action非字符串", goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": 123}), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.expect, isReadonlyGoalGetRequest(c.req))
		})
	}
}

// TestShouldSyncPlanState_只读goal查询为假 只读 get 不同步 plan 状态。
// Python: service.py:1860 `if not readonly_goal_get:`
func TestShouldSyncPlanState_只读goal查询为假(t *testing.T) {
	r := NewAgentRuntime(AgentRuntimeDeps{})
	require.False(t, r.shouldSyncPlanState(
		goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": "get"})))
	require.False(t, r.shouldSyncPlanState(goalGuardRequest(schema.ReqMethodCommandGoal, nil)))
	// set/resume/pause/clear 不属于只读 get，交给 EnsureState 内部白名单判定
	require.True(t, r.shouldSyncPlanState(
		goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": "set"})))
	require.True(t, r.shouldSyncPlanState(goalGuardRequest(schema.ReqMethodChatSend, nil)))
}

// TestShouldCheckPlanExit_只读goal查询为假 只读 get 不检查 plan 退出。
// Python: service.py:1908 `if agent is not None and not stateless and not readonly_goal_get:`
func TestShouldCheckPlanExit_只读goal查询为假(t *testing.T) {
	r := NewAgentRuntime(AgentRuntimeDeps{})
	require.False(t, r.shouldCheckPlanExit(goalGuardRequest(schema.ReqMethodCommandGoal, nil)))
	require.False(t, r.shouldCheckPlanExit(
		goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": "get"})))
	// 非 get 的 goal 动作仍要检查（上游只排除只读查询）
	require.True(t, r.shouldCheckPlanExit(
		goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{"action": "pause"})))
	require.True(t, r.shouldCheckPlanExit(goalGuardRequest(schema.ReqMethodChatSend, nil)))
}

// TestPlanGuards_控制器缺失或请求为空 两个守卫都不放行。
func TestPlanGuards_控制器缺失或请求为空(t *testing.T) {
	noController := &AgentRuntime{}
	require.False(t, noController.shouldSyncPlanState(goalGuardRequest(schema.ReqMethodChatSend, nil)))
	require.False(t, noController.shouldCheckPlanExit(goalGuardRequest(schema.ReqMethodChatSend, nil)))

	withController := NewAgentRuntime(AgentRuntimeDeps{})
	require.False(t, withController.shouldSyncPlanState(nil))
	require.False(t, withController.shouldCheckPlanExit(nil))
}

// fakeGuardAgentManager 只实现 AgentManagerPort 的两个方法。
type fakeGuardAgentManager struct {
	// agent 被返回的聊天 agent
	agent ChatAgentPort
}

// GetChatAgent 返回预置 agent。
func (m *fakeGuardAgentManager) GetChatAgent(
	_ context.Context, _, _, _, _ string,
	_ map[string]any, _ string) (ChatAgentPort, error) {
	return m.agent, nil
}

// GetChatAgentNoWait 返回预置 agent。
func (m *fakeGuardAgentManager) GetChatAgentNoWait(string, string, string, string) ChatAgentPort {
	return m.agent
}

// TestPrepareTurn_走回退路径时按只读goal查询决定metadata同步 覆盖未注入
// Prepare 的真实路径：只读 command.goal get 以 syncMetadata=false 进入
// PrepareChatTurn，不再兜底写 session metadata。
// Python: service.py:1848 `"sync_metadata": not readonly_goal_get`
func TestPrepareTurn_走回退路径时按只读goal查询决定metadata同步(t *testing.T) {
	cases := []struct {
		name   string
		action string
	}{
		{"只读get", "get"},
		{"非只读set", "set"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agent := &fakePlanAgent{}
			rt := NewAgentRuntime(AgentRuntimeDeps{Manager: &fakeGuardAgentManager{agent: agent}})
			sid := "sess-plan-guard"
			req := goalGuardRequest(schema.ReqMethodCommandGoal, map[string]any{
				"action": c.action, "mode": "agent", "work_mode": "work",
			})
			req.SessionID = &sid

			mode, _, got, err := rt.prepareTurn(context.Background(), req, nil, "")
			require.NoError(t, err)
			require.Equal(t, "agent", mode)
			require.Same(t, agent, got)
		})
	}
}
