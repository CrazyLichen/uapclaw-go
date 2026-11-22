package callback

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeCbState 可提交状态（condition 包 fakeCondState 同款面）
type fakeCbState struct {
	state.SessionState
	state.WorkflowState
	values     map[state.StateKey]any
	inputs     map[string]any
	setOutputs []any
}

func (s *fakeCbState) CommitCmp() {}
func (s *fakeCbState) Commit()      {}
func (s *fakeCbState) GetInputs(_ state.StateKey) any { return s.inputs }
func (s *fakeCbState) SetOutputs(data map[string]any) { s.setOutputs = append(s.setOutputs, data) }
func (s *fakeCbState) Get(key state.StateKey) any     { return s.values[key] }
func (s *fakeCbState) Update(data map[string]any) error {
	for k, v := range data {
		s.values[state.StringKey(k)] = v
	}
	return nil
}

type fakeCbSession struct {
	session.BaseSession
	st state.SessionState
}

func (s *fakeCbSession) State() state.SessionState { return s.st }
func (s *fakeCbSession) ExecutableID() string      { return "loop_1" }

func newCbSession(values map[state.StateKey]any, inputs map[string]any) (session.BaseSession, *fakeCbState) {
	st := &fakeCbState{values: values, inputs: inputs}
	return &fakeCbSession{st: st}, st
}

// fakeLoopCallback 记录四阶段调用的回调
type fakeLoopCallback struct {
	LoopCallback
	calls []string
}

func newFakeLoopCallback() *fakeLoopCallback {
	f := &fakeLoopCallback{}
	f.LoopCallback = NewLoopCallback(
		func(_ session.BaseSession) (any, error) { f.calls = append(f.calls, StageFirstLoop); return nil, nil },
		func(_ session.BaseSession) (any, error) { f.calls = append(f.calls, StageStartRound); return nil, nil },
		func(_ session.BaseSession, _ int) (any, error) { f.calls = append(f.calls, StageEndRound); return nil, nil },
		func(_ session.BaseSession) (any, error) { f.calls = append(f.calls, StageOutLoop); return nil, nil },
	)
	return f
}

// ──────────────────────────── 测试 ────────────────────────────

// TestLoopCallback_四阶段分派 对齐 loop_callback.py:19-32（含未知阶段走 out_loop 的 else 分支）
func TestLoopCallback_四阶段分派(t *testing.T) {
	sess, st := newCbSession(map[state.StateKey]any{}, map[string]any{})
	cb := newFakeLoopCallback()

	lt := 1
	require.NoError(t, cb.Call(context.Background(), StageFirstLoop, sess, nil))
	require.NoError(t, cb.Call(context.Background(), StageStartRound, sess, nil))
	require.NoError(t, cb.Call(context.Background(), StageEndRound, sess, &lt))
	require.NoError(t, cb.Call(context.Background(), "unknown_stage", sess, nil)) // Python else → out_loop
	assert.Equal(t, []string{StageFirstLoop, StageStartRound, StageEndRound, StageOutLoop}, cb.calls)
	assert.Empty(t, st.setOutputs) // 四阶段均返回 nil → 不 set_outputs
}

// TestLoopCallback_非nil输出落set_outputs 对齐 loop_callback.py:31-32
func TestLoopCallback_非nil输出落set_outputs(t *testing.T) {
	sess, st := newCbSession(map[state.StateKey]any{}, map[string]any{})
	cb := NewLoopCallback(
		func(_ session.BaseSession) (any, error) { return map[string]any{"k": 1}, nil },
		func(_ session.BaseSession) (any, error) { return nil, nil },
		func(_ session.BaseSession, _ int) (any, error) { return nil, nil },
		func(_ session.BaseSession) (any, error) { return nil, nil },
	)
	require.NoError(t, cb.Call(context.Background(), StageFirstLoop, sess, nil))
	require.Len(t, st.setOutputs, 1)
	assert.Equal(t, map[string]any{"k": 1}, st.setOutputs[0])
}

// TestLoopCallback_阶段错误上抛（atomic 链任一步失败不提交）
func TestLoopCallback_阶段错误上抛(t *testing.T) {
	sess, st := newCbSession(map[state.StateKey]any{}, map[string]any{})
	cb := NewLoopCallback(
		func(_ session.BaseSession) (any, error) { return nil, assert.AnError },
		func(_ session.BaseSession) (any, error) { return nil, nil },
		func(_ session.BaseSession, _ int) (any, error) { return nil, nil },
		func(_ session.BaseSession) (any, error) { return nil, nil },
	)
	err := cb.Call(context.Background(), StageFirstLoop, sess, nil)
	require.ErrorIs(t, err, assert.AnError)
	assert.Empty(t, st.setOutputs)
}

// TestNewLoopCallback_槽nilpanic（Python 抽象方法必实现）
func TestNewLoopCallback_槽nilpanic(t *testing.T) {
	first := func(_ session.BaseSession) (any, error) { return nil, nil }
	start := func(_ session.BaseSession) (any, error) { return nil, nil }
	end := func(_ session.BaseSession, _ int) (any, error) { return nil, nil }
	out := func(_ session.BaseSession) (any, error) { return nil, nil }
	assert.Panics(t, func() { NewLoopCallback(nil, start, end, out) })
	assert.Panics(t, func() { NewLoopCallback(first, nil, end, out) })
	assert.Panics(t, func() { NewLoopCallback(first, start, nil, out) })
	assert.Panics(t, func() { NewLoopCallback(first, start, end, nil) })
}
