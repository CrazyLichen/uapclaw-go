package task_loop

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// newUserWork 构造 user work item（测试辅助）
func newUserWork(query string) *hschema.RoundWorkItem {
	return hschema.NewUserWorkItem("req", map[string]any{"query": query}, false, true)
}

// newGoalWork 构造 goal work item（测试辅助）
func newGoalWork(goalID string, revision int) *hschema.RoundWorkItem {
	return hschema.NewGoalWorkItem(
		map[string]any{"query": "goal"}, goalID, revision, "sess-1")
}

// TestEventManager_先进先出 验证 user 队列按入队顺序弹出
// Python: EventManager.next_work 的 user 分支 (L51)
func TestEventManager_先进先出(t *testing.T) {
	m := NewEventManager()
	require.NoError(t, m.PushUser(newUserWork("u1")))
	require.NoError(t, m.PushUser(newUserWork("u2")))

	assert.True(t, m.HasPendingWork())
	assert.True(t, m.HasPendingUserWork())

	first := m.NextWork()
	require.NotNil(t, first)
	assert.Equal(t, hschema.RoundWorkKindUser, first.Kind)
	assert.Equal(t, "u1", first.Query())

	second := m.NextWork()
	require.NotNil(t, second)
	assert.Equal(t, "u2", second.Query())

	assert.False(t, m.HasPendingUserWork())
	assert.False(t, m.HasPendingWork())
}

// TestEventManager_PushUser类型校验 验证 kind 不匹配时报错
// Python: push_user 的 ValueError (L29)
func TestEventManager_PushUser类型校验(t *testing.T) {
	m := NewEventManager()
	err := m.PushUser(newGoalWork("g1", 1))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWorkKindMismatch)
}

// TestEventManager_MarkStarted与MarkFinished 验证运行态迁移
// Python: mark_started (L59) / mark_finished (L64)
func TestEventManager_MarkStarted与MarkFinished(t *testing.T) {
	m := NewEventManager()
	work := newUserWork("u1")
	require.NoError(t, m.PushUser(work))

	got := m.NextWork()
	require.Same(t, work, got)
	m.MarkStarted(work)
	assert.Same(t, work, m.ActiveWork())

	m.MarkFinished(work)
	assert.Nil(t, m.ActiveWork())
}

// TestEventManager_NextWork空队列 验证无待办返回 nil
func TestEventManager_NextWork空队列(t *testing.T) {
	m := NewEventManager()
	assert.Nil(t, m.NextWork())
	assert.False(t, m.HasPendingWork())
}

// TestEventManager_User优先于Goal 验证 user 队列优先于 goal 队列
// Python: EventManager.next_work (L50-57)
func TestEventManager_User优先于Goal(t *testing.T) {
	m := NewEventManager()
	pushed, err := m.PushGoal(newGoalWork("g1", 1))
	require.NoError(t, err)
	require.True(t, pushed)
	require.NoError(t, m.PushUser(newUserWork("u1")))

	got := m.NextWork()
	require.NotNil(t, got)
	assert.Equal(t, hschema.RoundWorkKindUser, got.Kind, "user 必须先于 goal 被消费")

	got = m.NextWork()
	require.NotNil(t, got)
	assert.Equal(t, hschema.RoundWorkKindGoal, got.Kind)
}

// TestEventManager_PushGoal去重_三态 验证 pending/dequeued/active 三态都去重
// Python: EventManager.push_goal + has_goal_work (L33/L74)
func TestEventManager_PushGoal去重_三态(t *testing.T) {
	// ① pending
	m := NewEventManager()
	pushed, err := m.PushGoal(newGoalWork("g1", 1))
	require.NoError(t, err)
	assert.True(t, pushed)
	pushed, err = m.PushGoal(newGoalWork("g1", 1))
	require.NoError(t, err)
	assert.False(t, pushed, "pending 同代际应去重")

	// ② dequeued
	m2 := NewEventManager()
	_, err = m2.PushGoal(newGoalWork("g1", 1))
	require.NoError(t, err)
	require.NotNil(t, m2.NextWork())
	pushed, err = m2.PushGoal(newGoalWork("g1", 1))
	require.NoError(t, err)
	assert.False(t, pushed, "dequeued 同代际应去重")

	// ③ active
	m3 := NewEventManager()
	work := newGoalWork("g1", 1)
	_, err = m3.PushGoal(work)
	require.NoError(t, err)
	require.Same(t, work, m3.NextWork())
	m3.MarkStarted(work)
	pushed, err = m3.PushGoal(newGoalWork("g1", 1))
	require.NoError(t, err)
	assert.False(t, pushed, "active 同代际应去重")
}

// TestEventManager_PushGoal不同revision 验证新代际不去重
func TestEventManager_PushGoal不同revision(t *testing.T) {
	m := NewEventManager()
	_, err := m.PushGoal(newGoalWork("g1", 1))
	require.NoError(t, err)

	pushed, err := m.PushGoal(newGoalWork("g1", 2))
	require.NoError(t, err)
	assert.True(t, pushed, "revision 变化应视为新代际")
	assert.True(t, m.HasGoalWork("g1", 1), "旧代际仍在队列中")
	assert.True(t, m.HasGoalWork("g1", 2))
}

// TestEventManager_PushGoal类型校验 验证 kind 不匹配时报错
// Python: push_goal 的 ValueError (L41)
func TestEventManager_PushGoal类型校验(t *testing.T) {
	m := NewEventManager()
	_, err := m.PushGoal(newUserWork("u1"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWorkKindMismatch)
}

// TestEventManager_HasRunningGoal忽略revision与pending 验证 pause/resume 判定
// Python: EventManager.has_running_goal (L89)
func TestEventManager_HasRunningGoal忽略revision与pending(t *testing.T) {
	// pending 不算 running
	m := NewEventManager()
	_, err := m.PushGoal(newGoalWork("g1", 1))
	require.NoError(t, err)
	assert.False(t, m.HasRunningGoal("g1"), "pending 不计入 running")

	// active 算 running（且不看 revision）
	work := m.NextWork()
	require.NotNil(t, work)
	m.MarkStarted(work)
	assert.True(t, m.HasRunningGoal("g1"))
	assert.False(t, m.HasRunningGoal("g2"))
}

// TestEventManager_DiscardGoalWork按会话与目标 验证精确丢弃
// Python: EventManager.discard_goal_work (L106)
//
// 注：去重键是 (goal_id, revision)，不含 session（上游语义，EventManager 每会话一个）；
// 故跨会话用例必须用不同 goal_id 才不会被去重。
func TestEventManager_DiscardGoalWork按会话与目标(t *testing.T) {
	m := NewEventManager()
	other := hschema.NewGoalWorkItem(map[string]any{"query": "g"}, "g3", 1, "sess-2")
	_, err := m.PushGoal(newGoalWork("g1", 1))
	require.NoError(t, err)
	_, err = m.PushGoal(newGoalWork("g2", 1))
	require.NoError(t, err)
	pushed, err := m.PushGoal(other)
	require.NoError(t, err)
	require.True(t, pushed, "不同 goal_id 不应被去重")

	// 只丢 sess-1 且 goal=g1
	assert.Equal(t, 1, m.DiscardGoalWork("sess-1", "g1"))
	assert.True(t, m.HasGoalWork("g2", 1))
	assert.True(t, m.HasGoalWork("g3", 1), "sess-2 的 g3 必须保留")

	// goalID 为空表示该会话全部目标
	assert.Equal(t, 1, m.DiscardGoalWork("sess-1", ""))
	assert.False(t, m.HasGoalWork("g2", 1))
	assert.True(t, m.HasGoalWork("g3", 1), "其他会话不受影响")
}

// TestEventManager_DiscardAllWork保留active 验证断线清场语义
// Python: EventManager.discard_all_work (L132)
func TestEventManager_DiscardAllWork保留active(t *testing.T) {
	m := NewEventManager()
	require.NoError(t, m.PushUser(newUserWork("u1")))
	_, err := m.PushGoal(newGoalWork("g1", 1))
	require.NoError(t, err)

	work := m.NextWork()
	require.NotNil(t, work)
	m.MarkStarted(work)

	m.DiscardAllWork()
	assert.False(t, m.HasPendingWork())
	assert.Same(t, work, m.ActiveWork(), "active 不受 discard_all_work 影响")
}
