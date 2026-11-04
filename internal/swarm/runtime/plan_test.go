package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeStore 内存实现 PlanStateStore。
type fakeStore struct {
	modes    map[string]string
	slugs    map[string]string
	switches []string
	commits  int
}

func newFakeStore() *fakeStore {
	return &fakeStore{modes: map[string]string{}, slugs: map[string]string{}}
}

// LoadPlanState 读取状态
func (f *fakeStore) LoadPlanState(_ context.Context, sid string) (PlanState, error) {
	return PlanState{Mode: f.modes[sid], PlanSlug: f.slugs[sid]}, nil
}

// SavePlanState 保存状态
func (f *fakeStore) SavePlanState(_ context.Context, sid string, st PlanState) error {
	f.modes[sid], f.slugs[sid] = st.Mode, st.PlanSlug
	return nil
}

// SwitchPlanMode 切模式
func (f *fakeStore) SwitchPlanMode(_ context.Context, sid, mode string) error {
	f.modes[sid] = mode
	f.switches = append(f.switches, mode)
	return nil
}

// CommitSession 提交
func (f *fakeStore) CommitSession(_ context.Context, _ string) error { f.commits++; return nil }

// TestEnsureState_显式进入并改写query 验证 plan_toggle 进入、清记忆、前置提醒。
//
// Python: ensure_state 的进入分支 + inject_activation_reminder
func TestEnsureState_显式进入并改写query(t *testing.T) {
	store := newFakeStore()
	c := NewPlanModeController(store)
	c.markExited("s1")

	res, err := c.EnsureState(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send", EntrySource: "plan_toggle",
		Query: "帮我重构",
	})
	require.NoError(t, err)
	require.True(t, res.Entered)
	require.Equal(t, "plan", store.modes["s1"])
	require.True(t, c.IsActive("s1"))
	require.False(t, c.isExited("s1"), "显式进入应清掉退出记忆")
	require.Contains(t, res.Query, "<system-reminder>")
	require.Contains(t, res.Query, "帮我重构")
}

// TestEnsureState_非显式进入被阻断 验证退出记忆阻断并强制 normal。
func TestEnsureState_非显式进入被阻断(t *testing.T) {
	store := newFakeStore()
	c := NewPlanModeController(store)
	c.markExited("s1")

	res, err := c.EnsureState(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send", Query: "继续",
	})
	require.NoError(t, err)
	require.False(t, res.Entered)
	require.Equal(t, "normal", store.modes["s1"])
	require.Len(t, res.Events, 1)
	require.Equal(t, PlanModeExitedEvent, res.Events[0].EventType)
	require.Equal(t, "继续", res.Query, "阻断不应改写 query")
}

// TestEnsureState_planSlug阻断 验证残留计划文件阻断并清 slug。
func TestEnsureState_planSlug阻断(t *testing.T) {
	store := newFakeStore()
	store.slugs["s1"] = "old-plan"
	c := NewPlanModeController(store)

	res, err := c.EnsureState(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send",
	})
	require.NoError(t, err)
	require.False(t, res.Entered)
	require.Equal(t, "normal", store.modes["s1"])
	require.Empty(t, store.slugs["s1"], "阻断后必须清空 slug")
	require.Len(t, res.Events, 1)
}

// TestEnsureState_退出计划并发事件 验证 plan→normal 的状态与事件。
func TestEnsureState_退出计划并发事件(t *testing.T) {
	store := newFakeStore()
	store.modes["s1"] = "plan"
	c := NewPlanModeController(store)
	c.markActive("s1")

	res, err := c.EnsureState(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.normal", ReqMethod: "chat.send",
	})
	require.NoError(t, err)
	require.True(t, res.Restored)
	require.Equal(t, "normal", store.modes["s1"])
	require.False(t, c.IsActive("s1"))
	require.Len(t, res.Events, 1)
}

// TestEnsureState_跳过矩阵 验证 team/非 chat 方法/中断续答/非单 agent 不写状态。
func TestEnsureState_跳过矩阵(t *testing.T) {
	requests := []PlanRequest{
		{SessionID: "s1", Mode: "code.team", ReqMethod: "chat.send"},
		{SessionID: "s1", Mode: "team", ReqMethod: "chat.send"},
		{SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "session.list"},
		{SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send", IsInterruptResume: true},
		{SessionID: "", Mode: "agent.work.plan", ReqMethod: "chat.send"},
	}
	for _, req := range requests {
		store := newFakeStore()
		c := NewPlanModeController(store)
		_, err := c.EnsureState(context.Background(), req)
		require.NoError(t, err)
		require.Empty(t, store.switches, "请求 %+v 不应写状态", req)
	}
}

// TestEnsureState_同模式幂等 验证重复请求不重复提交。
func TestEnsureState_同模式幂等(t *testing.T) {
	store := newFakeStore()
	store.modes["s1"] = "plan"
	c := NewPlanModeController(store)

	res, err := c.EnsureState(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send",
	})
	require.NoError(t, err)
	require.False(t, res.Entered)
	require.Empty(t, store.switches)
	require.True(t, c.IsActive("s1"), "已处于 plan 应重新记为 active")
}

// TestCheckPostProcessExit 验证工具退出后发事件并记记忆。
//
// Python: check_post_process_exit
func TestCheckPostProcessExit(t *testing.T) {
	store := newFakeStore()
	store.modes["s1"] = "normal"
	c := NewPlanModeController(store)
	c.markActive("s1")

	events, err := c.CheckPostProcessExit(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send",
	})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, PlanModeExitedEvent, events[0].EventType)
	require.False(t, c.IsActive("s1"))
	require.True(t, c.isExited("s1"))
}

// TestCheckPostProcessExit_仍在plan时不发事件 验证正常路径。
func TestCheckPostProcessExit_仍在plan时不发事件(t *testing.T) {
	store := newFakeStore()
	store.modes["s1"] = "plan"
	c := NewPlanModeController(store)

	events, err := c.CheckPostProcessExit(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send",
	})
	require.NoError(t, err)
	require.Empty(t, events)
}

// TestResetSession 验证删除会话清空两条记忆与锁。
func TestResetSession(t *testing.T) {
	c := NewPlanModeController(newFakeStore())
	c.markActive("s1")
	c.markExited("s1")
	_ = c.LockFor("s1")

	c.ResetSession("s1")
	require.False(t, c.IsActive("s1"))
	require.False(t, c.isExited("s1"))
	c.mu.Lock()
	_, hasLock := c.locks["s1"]
	c.mu.Unlock()
	require.False(t, hasLock)
}

// TestSessionMessageRequiresQueue 占位实现必须透传 false。
func TestSessionMessageRequiresQueue(t *testing.T) {
	c := NewPlanModeController(newFakeStore())
	c.markActive("s1")
	require.False(t, c.SessionMessageRequiresQueue("s1"))
}

// TestInjectActivationReminder 验证提醒文案与拼接顺序。
func TestInjectActivationReminder(t *testing.T) {
	got := InjectActivationReminder("原始query")
	require.Contains(t, got, "Plan mode is active")
	require.True(t, len(got) > len("原始query"))
	require.Equal(t, "原始query", got[len(got)-len("原始query"):], "原 query 必须原样在尾部")
}
