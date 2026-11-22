package condition

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/constants"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
)

// ──────────────────────────── 测试 ────────────────────────────

// TestArrayConditionInSession_遍历取元素写state 对齐 array.py:31-46
func TestArrayConditionInSession_遍历取元素写state(t *testing.T) {
	sess, st := mustWorkflowSessionWithState(t, map[state.StateKey]any{
		state.StringKey(constants.Index): 0,
	})

	c, err := NewArrayConditionInSession(map[string]any{"texts": []any{"你好", "世界", "再见"}})
	require.NoError(t, err)

	flag, err := c.Call(context.Background(), sess)
	require.NoError(t, err)
	assert.True(t, flag)
	// Python: session.state().update(updates) + 元组分支 set_outputs(io_updates)
	assert.Equal(t, "你好", st.values[state.StringKey("texts")])
	require.Len(t, st.setOutputs, 1)
	assert.Equal(t, map[string]any{"texts": "你好"}, st.setOutputs[0])
}

// TestArrayConditionInSession_耗尽返回False无updates 对齐 array.py:34-35
func TestArrayConditionInSession_耗尽返回False无updates(t *testing.T) {
	sess, st := mustWorkflowSessionWithState(t, map[state.StateKey]any{
		state.StringKey(constants.Index): 3,
	})

	c, err := NewArrayConditionInSession(map[string]any{"texts": []any{"a"}})
	require.NoError(t, err)

	flag, err := c.Call(context.Background(), sess)
	require.NoError(t, err)
	assert.False(t, flag)
	assert.Empty(t, st.setOutputs)
}

// TestArrayConditionInSession_多数组取最短 对齐 min_length 语义
func TestArrayConditionInSession_多数组取最短(t *testing.T) {
	sess, st := mustWorkflowSessionWithState(t, map[state.StateKey]any{
		state.StringKey(constants.Index): 2,
	})

	// texts 有 3 项、ids 只有 2 项 → idx=2 越过最短数组即停
	c, err := NewArrayConditionInSession(map[string]any{
		"texts": []any{"a", "b", "c"},
		"ids":   []any{1, 2},
	})
	require.NoError(t, err)

	flag, err := c.Call(context.Background(), sess)
	require.NoError(t, err)
	assert.False(t, flag)
	assert.Empty(t, st.setOutputs)
	_ = st
}

// TestArrayConditionInSession_构造期校验 对齐 _check_arrays（array.py:52-59）
func TestArrayConditionInSession_构造期校验(t *testing.T) {
	// None 拒绝
	_, err := NewArrayConditionInSession(map[string]any{"k": nil})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be None")

	// 非 list/tuple 拒绝
	_, err = NewArrayConditionInSession(map[string]any{"k": "str"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Expected list/tuple")
}

// TestArrayConditionInSession_Invoke与TraceInfo 非导出面/默认 trace_info
func TestArrayConditionInSession_Invoke与TraceInfo(t *testing.T) {
	sess, _ := mustWorkflowSessionWithState(t, map[state.StateKey]any{
		state.StringKey(constants.Index): 0,
	})
	c, err := NewArrayConditionInSession(map[string]any{"texts": []any{"x"}})
	require.NoError(t, err)

	flag, err := c.Invoke(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	assert.True(t, flag)
	assert.Equal(t, "", c.TraceInfo(nil)) // Python Condition.trace_info 默认 ""
}

// TestArrayCondition_死代码鸭子怪癖逐字保留 拍板 1：inputs 按 schema 取，
// 数组实际来自 inputs.get(key, [])——取不到 → min_length=0 → current_idx>=0 → False
func TestArrayCondition_死代码鸭子怪癖逐字保留(t *testing.T) {
	sess, _ := mustWorkflowSessionWithState(t, map[state.StateKey]any{
		state.StringKey(constants.Index): 0,
	})
	// input_schema 传 arrays schema；get_inputs 在 fake 上返回空 → 数组取不到
	arrays := map[string]any{"texts": []any{"a", "b"}}
	c := NewArrayCondition(arrays, state.SchemaKey(arrays))

	flag, err := c.Call(context.Background(), sess)
	require.NoError(t, err)
	assert.False(t, flag) // 怪癖：min_length 受 inputs 支配 → 0 → 停
}

// TestArrayCondition_Invoke_空schema直调 对齐 array.py:16-27 正常路径（schema 按键取到数组）
func TestArrayCondition_Invoke_空schema直调(t *testing.T) {
	sess, st := mustWorkflowSessionWithState(t, map[state.StateKey]any{
		state.StringKey(constants.Index): 1,
	})
	// inputSchema=nil → inputs={} → inputs.get("texts", []) → 空 → min_length=0 → False
	c := NewArrayCondition(map[string]any{"texts": []any{"a", "b"}}, nil)
	flag, err := c.Invoke(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	assert.False(t, flag)
	assert.Empty(t, st.setOutputs)
	_ = reflect.DeepEqual
}
