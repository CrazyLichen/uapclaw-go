package context_engineer

import (
	"context"
	"testing"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── Mock ────────────────────────────

// mockSessionFacade RefreshTaskStateRuntime 测试用 mock
type mockSessionFacade struct {
	states  map[sessstate.StateKey]interface{}
	updated map[string]any
}

func newMockSessionFacade() *mockSessionFacade {
	return &mockSessionFacade{
		states:  make(map[sessstate.StateKey]interface{}),
		updated: make(map[string]any),
	}
}

func (m *mockSessionFacade) GetSessionID() string { return "test-session" }
func (m *mockSessionFacade) GetState(key sessstate.StateKey) (interface{}, error) {
	return m.states[key], nil
}
func (m *mockSessionFacade) UpdateState(data map[string]any) {
	for k, v := range data {
		m.updated[k] = v
	}
}
func (m *mockSessionFacade) DumpState() map[string]any                               { return m.updated }
func (m *mockSessionFacade) WriteStream(ctx context.Context, data interface{}) error { return nil }
func (m *mockSessionFacade) WriteCustomStream(ctx context.Context, data interface{}) error {
	return nil
}
func (m *mockSessionFacade) GetEnv(key string, defaultValue ...interface{}) interface{} { return nil }
func (m *mockSessionFacade) Interact(ctx context.Context, value interface{}) error      { return nil }

// 确保 mock 实现了 SessionFacade 接口
var _ sessioninterfaces.SessionFacade = (*mockSessionFacade)(nil)

// ──────────────────────────── 测试 ────────────────────────────

func TestRefreshTaskStateRuntime_nilSession(t *testing.T) {
	ctx := &sainterfaces.AgentCallbackContext{}
	// 不应 panic
	RefreshTaskStateRuntime(ctx)
}

func TestRefreshTaskStateRuntime_从运行时属性读取(t *testing.T) {
	sess := newMockSessionFacade()
	runtimeState := &hschema.DeepAgentState{
		Iteration:          5,
		StopConditionState: map[string]any{"iteration": 5},
		PendingFollowUps:   []string{"follow-up-1", "follow-up-2"},
		PlanMode:           hschema.PlanModeState{Mode: "normal"},
	}
	sess.states[sessstate.StringKey(sessionRuntimeAttr)] = runtimeState

	ctx := &sainterfaces.AgentCallbackContext{}
	// 通过反射设置 session 字段（私有字段）
	setCallbackSession(ctx, sess)

	RefreshTaskStateRuntime(ctx)

	// 验证 update_state 被调用
	if sess.updated["task_state"] == nil {
		t.Error("task_state 未设置")
	}
	if sess.updated["iteration"] != 5 {
		t.Errorf("iteration = %v, want 5", sess.updated["iteration"])
	}
	pfu, ok := sess.updated["pending_follow_ups"].([]string)
	if !ok || len(pfu) != 2 {
		t.Errorf("pending_follow_ups = %v, want 2 items", sess.updated["pending_follow_ups"])
	}
}

func TestRefreshTaskStateRuntime_从持久化状态读取(t *testing.T) {
	sess := newMockSessionFacade()
	// 不设置运行时属性，设置持久化状态
	persistedState := map[string]any{
		"iteration":          3,
		"pending_follow_ups": []string{"follow-up"},
		"plan_mode":          map[string]any{"mode": "plan"},
	}
	sess.states[sessstate.StringKey(sessionStateKey)] = persistedState

	ctx := &sainterfaces.AgentCallbackContext{}
	setCallbackSession(ctx, sess)

	RefreshTaskStateRuntime(ctx)

	if sess.updated["iteration"] != 3 {
		t.Errorf("iteration = %v, want 3", sess.updated["iteration"])
	}
}

func TestRefreshTaskStateRuntime_空状态不更新(t *testing.T) {
	sess := newMockSessionFacade()
	// 不设置任何状态

	ctx := &sainterfaces.AgentCallbackContext{}
	setCallbackSession(ctx, sess)

	RefreshTaskStateRuntime(ctx)

	if len(sess.updated) > 0 {
		t.Error("空状态不应触发 update_state")
	}
}

func TestRefreshTaskStateRuntime_stopConditionState优先(t *testing.T) {
	sess := newMockSessionFacade()
	runtimeState := &hschema.DeepAgentState{
		Iteration:          10,                             // 顶层 iteration
		StopConditionState: map[string]any{"iteration": 7}, // stop_condition_state 中的 iteration 优先
	}
	sess.states[sessstate.StringKey(sessionRuntimeAttr)] = runtimeState

	ctx := &sainterfaces.AgentCallbackContext{}
	setCallbackSession(ctx, sess)

	RefreshTaskStateRuntime(ctx)

	// stop_condition_state.iteration 应优先
	if sess.updated["iteration"] != 7 {
		t.Errorf("iteration = %v, want 7 (from stop_condition_state)", sess.updated["iteration"])
	}
}

// ──────────────────────────── 辅助 ────────────────────────────

// setCallbackSession 使用反射设置 AgentCallbackContext 的私有 session 字段
func setCallbackSession(ctx *sainterfaces.AgentCallbackContext, sess sessioninterfaces.SessionFacade) {
	// AgentCallbackContext 有 SetSession 吗？检查一下
	// 没有公开的 SetSession 方法，需要使用构造函数
	// 使用 NewAgentCallbackContext 重建
	*ctx = *sainterfaces.NewAgentCallbackContext(nil, nil, sess)
}
