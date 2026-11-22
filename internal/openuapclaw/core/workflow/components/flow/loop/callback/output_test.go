package callback

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
)

// ──────────────────────────── 测试 ────────────────────────────

// TestOutputCallback_轮次累积与收割 对齐 output.py 全链（first/end×N/out）
func TestOutputCallback_轮次累积与收割(t *testing.T) {
	sess, st := newCbSession(map[state.StateKey]any{}, map[string]any{"results": "Hello"})
	// outputs_format 引用路径首段==node_id（loop_1）→ end_round 取 get_inputs 累积
	c := NewOutputCallback(map[string]any{"results": "${loop_1.result}"}, "", "")

	lt := 1
	require.NoError(t, c.Call(context.Background(), StageFirstLoop, sess, nil))           // state["round"]=[]
	require.NoError(t, c.Call(context.Background(), StageEndRound, sess, &lt))            // append inputs
	require.NoError(t, c.Call(context.Background(), StageEndRound, sess, &lt))            // len>=loop_times 跳过
	require.NoError(t, c.Call(context.Background(), StageOutLoop, sess, nil))             // 收割(set_outputs)

	// round 数组有 1 条（end_round 经 state.update 累积，不走 set_outputs）
	assert.Len(t, st.values[state.StringKey("round")].([]any), 1)
	// 收割：首段==node_id → results[-1] 按 root(["results"]) 下钻 → 返回值经 _atomic_invoke set_outputs
	require.Len(t, st.setOutputs, 1)
	assert.Equal(t, map[string]any{"results": "Hello"}, st.setOutputs[0])
}

// TestOutputCallback_收割兜底遍历 对齐 output.py 兜底分支（引用路径首段 != node_id）
func TestOutputCallback_收割兜底遍历(t *testing.T) {
	sess, st := newCbSession(map[state.StateKey]any{
		state.StringKey("round"): []any{map[string]any{"all": "a"}, map[string]any{"all": "b"}},
	}, map[string]any{})
	// 引用路径首段为 other_node ≠ loop_1 → 走兜底遍历
	c := NewOutputCallback(map[string]any{"all": "${other_node.r}"}, "", "")
	require.NoError(t, c.Call(context.Background(), StageOutLoop, sess, nil))
	assert.Equal(t, []any{"a", "b"}, st.setOutputs[0].(map[string]any)["all"])
}

// TestOutputCallback_endRound_结果非list报错 对齐 output.py:48-49
func TestOutputCallback_endRound_结果非list报错(t *testing.T) {
	sess, _ := newCbSession(map[state.StateKey]any{}, map[string]any{}) // state 无 round 键
	c := NewOutputCallback(map[string]any{"k": "${loop_1.v}"}, "", "")
	lt := 1
	err := c.Call(context.Background(), StageEndRound, sess, &lt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error results in round process")
}

// TestIntermediateLoopVarCallback_首轮注入 对齐 intermediate_loop_var.py:13-23
func TestIntermediateLoopVarCallback_首轮注入(t *testing.T) {
	sess, st := newCbSession(map[state.StateKey]any{}, map[string]any{"user_num": 42})
	c := NewIntermediateLoopVarCallback(map[string]any{"user_num": "${start.user_num}"}, "")

	require.NoError(t, c.Call(context.Background(), StageFirstLoop, sess, nil))
	// update 进 state + commit
	assert.Equal(t, 42, st.values[state.StringKey("user_num")])
	// 返回 local_vars → set_outputs
	assert.Equal(t, map[string]any{"user_num": 42}, st.setOutputs[0])
}

// TestIntermediateLoopVarCallback_root包装 对齐 intermediate_loop_var.py:21-22
func TestIntermediateLoopVarCallback_root包装(t *testing.T) {
	sess, st := newCbSession(map[state.StateKey]any{}, map[string]any{"ss": "v"})
	c := NewIntermediateLoopVarCallback(map[string]any{"ss": "${start.ss}"}, "intermediateLoopVar")

	require.NoError(t, c.Call(context.Background(), StageFirstLoop, sess, nil))
	assert.Equal(t, map[string]any{"intermediateLoopVar": map[string]any{"ss": "v"}}, st.setOutputs[0])
}
