package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakePlanAgent 记录调用顺序并持有 plan 状态。
type fakePlanAgent struct {
	// calls 调用记录（execute / stream）
	calls []string
	// state 当前 plan 状态
	state PlanState
}

// ProcessMessage 非流式执行
func (f *fakePlanAgent) ProcessMessage(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	f.calls = append(f.calls, "execute")
	return &schema.AgentResponse{OK: true, Payload: map[string]any{"event_type": "chat.final"}}, nil
}

// ProcessMessageStream 流式执行
func (f *fakePlanAgent) ProcessMessageStream(_ context.Context, _ *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	f.calls = append(f.calls, "stream")
	ch := make(chan *schema.AgentResponseChunk, 2)
	ch <- &schema.AgentResponseChunk{Payload: map[string]any{"event_type": "chat.delta", "content": "a"}}
	ch <- &schema.AgentResponseChunk{Payload: map[string]any{"event_type": "chat.final"}, IsComplete: true}
	close(ch)
	return ch, nil
}

// PlanStateAccessor 返回状态入口
func (f *fakePlanAgent) PlanStateAccessor() PlanStateAccessor {
	return &fakePlanAccessor{agent: f}
}

// fakePlanAccessor 记录 plan 状态读写顺序。
type fakePlanAccessor struct {
	// agent 持有状态的 agent
	agent *fakePlanAgent
}

// LoadPlanState 读取状态
func (a *fakePlanAccessor) LoadPlanState(_ context.Context, _ string) (PlanState, error) {
	a.agent.calls = append(a.agent.calls, "load")
	return a.agent.state, nil
}

// SavePlanState 保存状态
func (a *fakePlanAccessor) SavePlanState(_ context.Context, _ string, st PlanState) error {
	a.agent.calls = append(a.agent.calls, "save")
	a.agent.state = st
	return nil
}

// SwitchPlanMode 切换状态
func (a *fakePlanAccessor) SwitchPlanMode(_ context.Context, _ string, mode string) error {
	a.agent.calls = append(a.agent.calls, "switch")
	a.agent.state.Mode = mode
	return nil
}

// CommitSession 提交会话
func (a *fakePlanAccessor) CommitSession(_ context.Context, _ string) error {
	a.agent.calls = append(a.agent.calls, "commit")
	return nil
}

// fakeManager 只实现端口，测试中不直接使用。
type fakeManager struct{}

// GetChatAgent 返回错误（测试一律注入 Prepare）
func (fakeManager) GetChatAgent(context.Context, string, string, string, string) (ChatAgentPort, error) {
	return nil, context.Canceled
}

// GetChatAgentNoWait 返回 nil
func (fakeManager) GetChatAgentNoWait(string, string, string, string) ChatAgentPort { return nil }

// ──────────────────────────── 测试 ────────────────────────────

// TestAgentRuntime_Invoke_步骤顺序 验证 prepare → ensure → execute → check 顺序。
func TestAgentRuntime_Invoke_步骤顺序(t *testing.T) {
	agent := &fakePlanAgent{}
	var events []*RuntimeEvent
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string) (string, string, ChatAgentPort, error) {
			return "agent", "plan", agent, nil
		},
		OnControlEvent: func(ev *RuntimeEvent) { events = append(events, ev) },
	})

	req := reqWithParams(t, map[string]any{
		"mode":              "agent.work.plan",
		"query":             "请规划",
		"plan_entry_source": PlanEntrySourcePlanToggle,
		"work_mode":         "work",
	})
	out, err := rt.Invoke(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, []string{"load", "switch", "commit", "execute"}, agent.calls[:4])
	require.Equal(t, "plan", agent.state.Mode)
	require.Empty(t, events, "进入 plan 不产生控制事件")
}

// TestAgentRuntime_Invoke_进入plan注入提醒 验证 query 提醒回写与原文保存。
func TestAgentRuntime_Invoke_进入plan注入提醒(t *testing.T) {
	agent := &fakePlanAgent{}
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string) (string, string, ChatAgentPort, error) {
			return "agent", "plan", agent, nil
		},
	})
	req := reqWithParams(t, map[string]any{
		"mode":              "agent.work.plan",
		"query":             "请规划",
		"plan_entry_source": PlanEntrySourcePlanToggle,
		"work_mode":         "work",
	})
	_, err := rt.Invoke(context.Background(), req)
	require.NoError(t, err)

	params := parseRequestParams(req)
	query, _ := params["query"].(string)
	require.True(t, strings.HasPrefix(query, "\n\n<system-reminder>\n"))
	require.True(t, strings.HasSuffix(query, "请规划"))
	require.Equal(t, "请规划", params[PlanReminderOriginalQueryKey])
}

// TestAgentRuntime_Invoke_退出plan下发控制事件 验证 plan→normal 事件与 mode 回写。
func TestAgentRuntime_Invoke_退出plan下发控制事件(t *testing.T) {
	agent := &fakePlanAgent{}
	var events []*RuntimeEvent
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string) (string, string, ChatAgentPort, error) {
			return "agent", "", agent, nil
		},
		OnControlEvent: func(ev *RuntimeEvent) { events = append(events, ev) },
	})

	// 第一轮：显式进入 plan（写入 active 记忆）
	enter := reqWithParams(t, map[string]any{
		"mode": "agent.work.plan", "query": "请规划",
		"plan_entry_source": PlanEntrySourcePlanToggle, "work_mode": "work",
	})
	_, err := rt.Invoke(context.Background(), enter)
	require.NoError(t, err)
	require.Equal(t, "plan", agent.state.Mode)

	// 第二轮：回到 normal（may_hold_state 由 active 记忆命中）
	exit := reqWithParams(t, map[string]any{"mode": "agent.work.normal", "query": "退出", "work_mode": "work"})
	_, err = rt.Invoke(context.Background(), exit)
	require.NoError(t, err)

	require.Len(t, events, 1)
	require.Equal(t, PlanModeExitedEvent, events[0].EventType())
	require.Equal(t, "agent.work.normal", events[0].Payload["mode"])
	require.Equal(t, "normal", agent.state.Mode)
	// Python: check_post_process_exit 首步回写 canonical mode
	require.Equal(t, "agent.work.normal", parseRequestParams(exit)["mode"])
}

// TestAgentRuntime_Invoke_跳过矩阵 验证 team 与 resume 不参与 plan 同步。
func TestAgentRuntime_Invoke_跳过矩阵(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
	}{
		{"team 模式", map[string]any{"mode": "team.work.plan", "query": "x", "work_mode": "work"}},
		{"中断续答", map[string]any{"mode": "agent.work.plan", "query": "x", "resume": true, "work_mode": "work"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := &fakePlanAgent{}
			rt := NewAgentRuntime(AgentRuntimeDeps{
				Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string) (string, string, ChatAgentPort, error) {
					return "agent", "plan", agent, nil
				},
			})
			_, err := rt.Invoke(context.Background(), reqWithParams(t, tc.params))
			require.NoError(t, err)
			require.Equal(t, []string{"execute"}, agent.calls)
		})
	}
}

// TestAgentRuntime_Invoke_非chat方法跳过 plan 同步。
func TestAgentRuntime_Invoke_非chat方法跳过(t *testing.T) {
	agent := &fakePlanAgent{}
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string) (string, string, ChatAgentPort, error) {
			return "agent", "plan", agent, nil
		},
	})
	req := reqWithParams(t, map[string]any{"mode": "agent.work.plan", "query": "x", "work_mode": "work"})
	req.ReqMethod = schema.ReqMethodSessionList
	_, err := rt.Invoke(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, []string{"execute"}, agent.calls)
}

// TestAgentRuntime_Stream_逐块回传 验证流式 chunk 透传与控制事件。
func TestAgentRuntime_Stream_逐块回传(t *testing.T) {
	agent := &fakePlanAgent{}
	var seen []*RuntimeEvent
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Prepare: func(_ context.Context, _ *schema.AgentRequest, _ string) (string, string, ChatAgentPort, error) {
			return "agent", "", agent, nil
		},
		OnControlEvent: func(ev *RuntimeEvent) { seen = append(seen, ev) },
	})

	// 第一轮进入 plan
	enter := reqWithParams(t, map[string]any{
		"mode": "agent.work.plan", "query": "请规划",
		"plan_entry_source": PlanEntrySourcePlanToggle, "work_mode": "work",
	})
	require.NoError(t, rt.Stream(context.Background(), enter, func(*RuntimeEvent) bool { return true }))

	// 第二轮流式退出 plan
	exit := reqWithParams(t, map[string]any{"mode": "agent.work.normal", "query": "退出", "work_mode": "work"})
	var chunks int
	err := rt.Stream(context.Background(), exit, func(ev *RuntimeEvent) bool {
		if ev.EventType() != PlanModeExitedEvent {
			chunks++
		}
		return true
	})
	require.NoError(t, err)
	require.Equal(t, 2, chunks)
	require.Len(t, seen, 1)
	require.Equal(t, PlanModeExitedEvent, seen[0].EventType())
}

// TestUsesSessionRuntime 验证 team/stateless 不占用会话流槽。
func TestUsesSessionRuntime(t *testing.T) {
	require.True(t, UsesSessionRuntime(reqWithParams(t, map[string]any{"mode": "agent.work.normal"})))
	require.False(t, UsesSessionRuntime(reqWithParams(t, map[string]any{"mode": "team.work.normal"})))
	require.False(t, UsesSessionRuntime(reqWithParams(t, map[string]any{"mode": "agent.work.normal", "stateless": true})))
}

// TestApplyPlanResult 验证阻断分支回写 mode、进入分支回写 query。
func TestApplyPlanResult(t *testing.T) {
	req := reqWithParams(t, map[string]any{"mode": "agent.work.plan", "query": "原文"})
	ApplyPlanResult(req, PlanStateResult{ModeOverride: "agent.work.normal"})
	require.Equal(t, "agent.work.normal", parseRequestParams(req)["mode"])

	req2 := reqWithParams(t, map[string]any{"mode": "agent.work.plan", "query": "原文"})
	ApplyPlanResult(req2, PlanStateResult{Entered: true, Query: "提醒+原文"})
	params := parseRequestParams(req2)
	require.Equal(t, "提醒+原文", params["query"])
	require.Equal(t, "原文", params[PlanReminderOriginalQueryKey])
}

// reqWithParams 构造带 params 的请求。
func reqWithParams(t *testing.T, params map[string]any) *schema.AgentRequest {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	sid := "sess-1"
	return &schema.AgentRequest{RequestID: "req-1", ChannelID: "web", SessionID: &sid, Params: raw}
}
