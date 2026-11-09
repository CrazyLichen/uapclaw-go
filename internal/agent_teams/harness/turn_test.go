package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// TestResolveMemberTurn_首次开新turn 验证首次解析开新 turn 并落会话状态。
func TestResolveMemberTurn_首次开新turn(t *testing.T) {
	sess := session.NewSession(session.WithSessionID("turn-sess-1"))

	turn, opened := ResolveMemberTurn(sess, false)

	require.True(t, opened)
	assert.Equal(t, 1, turn.TurnNumber)
	assert.NotEmpty(t, turn.TurnID)
	raw, err := sess.GetState(state.StringKey(memberTurnStateKey))
	require.NoError(t, err)
	assert.NotNil(t, raw)
}

// TestResolveMemberTurn_续用不递增 验证 continuesTurn 时复用当前 turn。
func TestResolveMemberTurn_续用不递增(t *testing.T) {
	sess := session.NewSession(session.WithSessionID("turn-sess-2"))
	first, _ := ResolveMemberTurn(sess, false)

	second, opened := ResolveMemberTurn(sess, true)

	require.False(t, opened)
	assert.Equal(t, first, second)
}

// TestResolveMemberTurn_续用但无记录则新开 验证无历史时仍开新 turn。
func TestResolveMemberTurn_续用但无记录则新开(t *testing.T) {
	sess := session.NewSession(session.WithSessionID("turn-sess-3"))

	turn, opened := ResolveMemberTurn(sess, true)

	require.True(t, opened)
	assert.Equal(t, 1, turn.TurnNumber)
}

// TestResolveMemberTurn_nil存储仍返回可用身份 验证无会话时只生成不持久化。
func TestResolveMemberTurn_nil存储仍返回可用身份(t *testing.T) {
	turn, opened := ResolveMemberTurn(nil, false)
	require.True(t, opened)
	assert.Equal(t, 1, turn.TurnNumber)
	assert.NotEmpty(t, turn.TurnID)
}

// TestMemberTurnFromDict_畸形数据视为不存在 验证损坏快照退化为“开新 turn”。
func TestMemberTurnFromDict_畸形数据视为不存在(t *testing.T) {
	assert.False(t, memberTurnFromDictValid(nil))
	assert.False(t, memberTurnFromDictValid(map[string]any{"turn_id": "", "turn_number": 1}))
	assert.False(t, memberTurnFromDictValid(map[string]any{"turn_id": "x", "turn_number": 0}))
	assert.False(t, memberTurnFromDictValid(map[string]any{"turn_id": "x", "turn_number": float64(1)}))
	assert.True(t, memberTurnFromDictValid(map[string]any{"turn_id": "x", "turn_number": 1}))
}

// memberTurnFromDictValid 测试辅助：判断字典能否恢复出有效 turn。
func memberTurnFromDictValid(data any) bool {
	_, ok := memberTurnFromDict(data)
	return ok
}
