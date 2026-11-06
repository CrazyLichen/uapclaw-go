package goal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeGoalSession 内存会话：状态用真 state.InMemoryStateLike，
// 复刻生产语义（点号即嵌套路径 + 嵌套 nil 删键）。
type fakeGoalSession struct {
	// sessionID 会话标识
	sessionID string
	// state 内存状态
	state *state.InMemoryStateLike
	// commits 提交次数
	commits int
}

// newFakeGoalSession 创建内存会话
func newFakeGoalSession(sessionID string) *fakeGoalSession {
	return &fakeGoalSession{sessionID: sessionID, state: state.NewInMemoryStateLike()}
}

// GetSessionID 返回会话标识
func (s *fakeGoalSession) GetSessionID() string { return s.sessionID }

// GetState 读取状态
func (s *fakeGoalSession) GetState(key state.StateKey) (any, error) { return s.state.Get(key), nil }

// UpdateState 写入状态
func (s *fakeGoalSession) UpdateState(data map[string]any) { _ = s.state.Update(data) }

// Commit 记录提交
func (s *fakeGoalSession) Commit(context.Context) error { s.commits++; return nil }

// ──────────────────────────── 用例 ────────────────────────────

// TestSessionGoalStore_保存加载清空 校验基本读写
// Python: test_session_store_save_load_and_clear (test_goal_store.py L46)
func TestSessionGoalStore_保存加载清空(t *testing.T) {
	session := newFakeGoalSession("test-session")
	store := NewSessionGoalStore(session)
	record := CreateGoalRecord(session.GetSessionID(), "write a report", nil, nil)

	store.Save(record)
	loaded := store.Load()
	require.NotNil(t, loaded)
	assert.Equal(t, record.GoalID, loaded.GoalID)
	assert.Equal(t, "write a report", loaded.Objective)
	assert.Equal(t, "test-session", store.SessionID())

	store.Clear()
	assert.Nil(t, store.Load())
	raw, err := session.GetState(state.StringKey(SessionGoalRecordKey))
	require.NoError(t, err)
	assert.Nil(t, raw)
}

// TestSessionGoalStore_丢弃畸形数据 校验类型不符与还原失败都清空
// Python: test_session_store_drops_malformed_persistence_data (test_goal_store.py L63)
func TestSessionGoalStore_丢弃畸形数据(t *testing.T) {
	session := newFakeGoalSession("test-session")
	store := NewSessionGoalStore(session)

	session.UpdateState(map[string]any{SessionGoalRecordKey: "not-a-record"})
	assert.Nil(t, store.Load())
	raw, err := session.GetState(state.StringKey(SessionGoalRecordKey))
	require.NoError(t, err)
	assert.Nil(t, raw, "畸形类型必须被清空")

	session.UpdateState(map[string]any{SessionGoalRecordKey: map[string]any{"goal_id": "g"}})
	assert.Nil(t, store.Load())
	raw, err = session.GetState(state.StringKey(SessionGoalRecordKey))
	require.NoError(t, err)
	assert.Nil(t, raw, "还原失败必须被清空")
}

// TestSessionGoalStore_零值删键往返 校验 pause 后 active_started_at 键消失仍能读回
// Python: test_session_store_pause_round_trip_survives_none_as_delete (test_goal_store.py L76)
func TestSessionGoalStore_零值删键往返(t *testing.T) {
	session := newFakeGoalSession("test-session")
	store := NewSessionGoalStore(session)
	record := CreateGoalRecord(session.GetSessionID(), "write a report", nil, nil)
	store.Save(record)

	loaded := store.Load()
	require.NotNil(t, loaded)
	loaded.SettleActiveTime(false)
	loaded.Status = GoalStatusPaused
	store.Save(loaded)

	raw, err := session.GetState(state.StringKey(SessionGoalRecordKey))
	require.NoError(t, err)
	rawMap, ok := raw.(map[string]any)
	require.True(t, ok, "状态必须是 map 形态")
	assert.Nil(t, rawMap["active_started_at"], "nil 值必须删键而非留 null")

	paused := store.Load()
	require.NotNil(t, paused)
	assert.Equal(t, GoalStatusPaused, paused.Status)
	assert.Nil(t, paused.ActiveStartedAt)
	assert.GreaterOrEqual(t, paused.TimeUsedSeconds, 0)
}

// TestSessionGoalStore_提交委托会话 校验 commit 透传
func TestSessionGoalStore_提交委托会话(t *testing.T) {
	session := newFakeGoalSession("s1")
	store := NewSessionGoalStore(session)
	require.NoError(t, store.Commit(context.Background()))
	assert.Equal(t, 1, session.commits)
}
