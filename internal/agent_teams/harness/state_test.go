package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHarnessState_零值为Idle 验证状态默认值是 idle（未启动即空闲）。
func TestHarnessState_零值为Idle(t *testing.T) {
	st := newInternalState()
	require.NotNil(t, st)
	assert.Equal(t, HarnessStateIdle, st.Phase())
}

// TestInternalState_ActiveRound可写入读出 验证唯一可变容器的最小契约。
func TestInternalState_ActiveRound可写入读出(t *testing.T) {
	st := newInternalState()
	assert.Nil(t, st.Active())

	r := &ActiveRound{RoundID: 1, TaskID: "t1", OriginalQuery: "hi"}
	st.SetActive(r)
	assert.Equal(t, r, st.Active())
	// NextRoundID 是独立自增源：首次调用返回 1
	assert.Equal(t, 1, st.NextRoundID())
	assert.Equal(t, 2, st.NextRoundID())

	st.SetActive(nil)
	assert.Nil(t, st.Active())
}
