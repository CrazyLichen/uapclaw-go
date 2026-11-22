package condition

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/constants"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
)

// ──────────────────────────── 测试 ────────────────────────────

// TestNumberConditionInSession_小于上限返回True裸bool 对齐 number.py:18-22（无 tuple）
func TestNumberConditionInSession_小于上限返回True裸bool(t *testing.T) {
	sess, st := mustWorkflowSessionWithState(t, map[state.StateKey]any{
		state.StringKey(constants.Index): 2,
	})

	c := NewNumberConditionInSession(3)
	flag, err := c.Call(context.Background(), sess)
	require.NoError(t, err)
	assert.True(t, flag)
	assert.Empty(t, st.setOutputs) // 裸 bool，不触发 set_outputs
}

// TestNumberConditionInSession_到达上限返回False
func TestNumberConditionInSession_到达上限返回False(t *testing.T) {
	sess, _ := mustWorkflowSessionWithState(t, map[state.StateKey]any{
		state.StringKey(constants.Index): 3,
	})

	c := NewNumberConditionInSession(3)
	flag, err := c.Call(context.Background(), sess)
	require.NoError(t, err)
	assert.False(t, flag)
}

// TestNumberConditionInSession_Invoke与TraceInfo 非导出面/默认 trace_info
func TestNumberConditionInSession_Invoke与TraceInfo(t *testing.T) {
	sess, _ := mustWorkflowSessionWithState(t, map[state.StateKey]any{
		state.StringKey(constants.Index): 1,
	})
	c := NewNumberConditionInSession(5)
	flag, err := c.Invoke(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	assert.True(t, flag)
	assert.Equal(t, "", c.TraceInfo(nil))
}

// TestNumberCondition_死代码逐字 拍板 1：invoke 比较对象是 inputs 而非 limit 的
// 鸭子怪癖（number.py:14 limit_num = inputs）；inputs 为 dict → Python dict<int
// 比较 raise TypeError → Go 等价上抛（偏差 D-20-4）
func TestNumberCondition_死代码逐字(t *testing.T) {
	sess, _ := mustWorkflowSessionWithState(t, map[state.StateKey]any{
		state.StringKey(constants.Index): 5,
	})

	c := NewNumberCondition(3) // _limit 存而不用（怪癖保留）
	// inputs={}（空 dict）→ Python {} < 5 → TypeError → Go 上抛
	_, err := c.Call(context.Background(), sess)
	require.Error(t, err, "dict 与 int 比较在 Python raise TypeError，Go 等价上抛（D-20-4）")
}

// TestNumberCondition_TraceInfo 默认空串
func TestNumberCondition_TraceInfo(t *testing.T) {
	c := NewNumberCondition(3)
	assert.Equal(t, "", c.TraceInfo(nil))
}
