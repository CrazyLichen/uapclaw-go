package loop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/constants"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
	conditionpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/condition"
	callbackpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow/loop/callback"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeCond 可控条件（实现 condition.Condition）
type fakeCond struct {
	conditionpkg.BaseCondition
	flag bool
	err  error
}

func newFakeCond(flag bool, err error) *fakeCond {
	c := &fakeCond{flag: flag, err: err}
	c.BaseCondition = conditionpkg.NewBaseCondition(
		func(_ context.Context, _ map[string]any, _ session.BaseSession) (bool, map[string]any, error) {
			return flag, nil, err
		}, nil)
	return c
}

func (c *fakeCond) Invoke(_ context.Context, _ map[string]any, _ session.BaseSession) (bool, error) {
	return c.flag, c.err
}

func (c *fakeCond) TraceInfo(_ session.BaseSession) any { return "fake" }

// fakeBody 空循环体
type fakeBody struct {
	graphexeAlias
}

func (b *fakeBody) OnInvoke(_ context.Context, _ map[string]any, _ session.BaseSession, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

// fakeNodeSession 循环节点会话（nodeSession 视图；带 Parent/ParentID/Tracer 面）
type fakeNodeSession struct {
	session.BaseSession
	st state.SessionState
	id string
}

func (s *fakeNodeSession) State() state.SessionState { return s.st }
func (s *fakeNodeSession) ExecutableID() string      { return s.id }
func (s *fakeNodeSession) ParentID() string          { return "parent_node" }
func (s *fakeNodeSession) Parent() session.BaseSession {
	return nil
}
func (s *fakeNodeSession) Tracer() *tracer.Tracer { return nil }

func newFakeNodeSession(id string, values map[state.StateKey]any) (session.BaseSession, *fakeLoopState) {
	st := &fakeLoopState{values: values, globals: map[string]any{}}
	return &fakeNodeSession{st: st, id: id}, st
}

// recordingCb 记录阶段的回调
type recordingCb struct {
	callbackpkg.LoopCallback
	calls []string
}

func newRecordingCb() *recordingCb {
	r := &recordingCb{}
	r.LoopCallback = callbackpkg.NewLoopCallback(
		func(_ session.BaseSession) (any, error) { r.calls = append(r.calls, callbackpkg.StageFirstLoop); return nil, nil },
		func(_ session.BaseSession) (any, error) { r.calls = append(r.calls, callbackpkg.StageStartRound); return nil, nil },
		func(_ session.BaseSession, _ int) (any, error) { r.calls = append(r.calls, callbackpkg.StageEndRound); return nil, nil },
		func(_ session.BaseSession) (any, error) { r.calls = append(r.calls, callbackpkg.StageOutLoop); return nil, nil },
	)
	return r
}

// newTestALC 构造测试用 AdvancedLoopComponent（真小图，body 用 fake）
func newTestALC(t *testing.T, cond any, cbs []callbackpkg.LoopCallback) *AdvancedLoopComponent {
	t.Helper()
	a, err := NewAdvancedLoopComponent(&fakeBody{}, cond, nil, cbs)
	require.NoError(t, err)
	return a
}

// ──────────────────────────── 测试 ────────────────────────────

// TestNewAdvancedLoopComponent_条件四态归一 对齐 loop_comp.py:350-358
func TestNewAdvancedLoopComponent_条件四态归一(t *testing.T) {
	a := newTestALC(t, nil, nil) // nil → AlwaysTrue
	assert.Equal(t, "True", a.condition.TraceInfo(nil))

	fc := newFakeCond(true, nil)
	a = newTestALC(t, fc, nil)
	assert.Same(t, conditionpkg.Condition(fc), a.condition)

	a = newTestALC(t, func() bool { return true }, nil)
	assert.Equal(t, "func1", a.condition.TraceInfo(nil))

	a, err := NewAdvancedLoopComponent(&fakeBody{}, "1 == 1", nil, nil)
	require.NoError(t, err)
	assert.NotNil(t, a.condition)

	// 未知类型拒绝
	_, err = NewAdvancedLoopComponent(&fakeBody{}, 123, nil, nil)
	require.Error(t, err)
}

// TestAdvancedLoopComponent_首轮初始化与路由 对齐 _condition_invoke ①（INDEX 初始化）
func TestAdvancedLoopComponent_首轮初始化与路由(t *testing.T) {
	sess, _ := newFakeNodeSession("loop_1", map[state.StateKey]any{})
	a := newTestALC(t, newFakeCond(true, nil), nil)
	a.nodeSession = sess

	targets, err := a.Select(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{BodyNodeID}, targets) // in_loop
	// INDEX=0 已初始化
	st := sess.State().Get(state.StringKey(constants.Index))
	assert.Equal(t, 0, st)
}

// TestAdvancedLoopComponent_第二轮递增与END_ROUND 对齐 ④（finish_index==index → INDEX+1）
func TestAdvancedLoopComponent_第二轮递增与END_ROUND(t *testing.T) {
	sess, _ := newFakeNodeSession("loop_1", map[state.StateKey]any{
		state.StringKey(constants.Index): 0,
	})
	cb := newRecordingCb()
	a := newTestALC(t, newFakeCond(true, nil), []callbackpkg.LoopCallback{cb.LoopCallback})
	a.nodeSession = sess
	a.postBody.SetFinishIndex(0) // 第一轮 post_body 已跑（finish_index=0）

	targets, err := a.Select(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{BodyNodeID}, targets)
	// INDEX 递增 1
	assert.Equal(t, 1, sess.State().Get(state.StringKey(constants.Index)))
	// callbacks：END_ROUND(index+1=1) + START_ROUND
	assert.Equal(t, []string{callbackpkg.StageEndRound, callbackpkg.StageStartRound}, cb.calls)
}

// TestAdvancedLoopComponent_resume修正 对齐 ③（finish_index+1 < index → index-1）
func TestAdvancedLoopComponent_resume修正(t *testing.T) {
	sess, _ := newFakeNodeSession("loop_1", map[state.StateKey]any{
		state.StringKey(constants.Index): 2,
	})
	cb := newRecordingCb()
	a := newTestALC(t, newFakeCond(true, nil), []callbackpkg.LoopCallback{cb.LoopCallback})
	a.nodeSession = sess
	a.postBody.SetFinishIndex(0) // 中断重建后实例字段失配（真实值应为 1）

	targets, err := a.Select(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{BodyNodeID}, targets)
	// 修正：finish_index = index-1 = 1 → != index(2) → 不递增、无 END_ROUND
	assert.Equal(t, 2, sess.State().Get(state.StringKey(constants.Index)))
	assert.Equal(t, []string{callbackpkg.StageStartRound}, cb.calls)
}

// TestAdvancedLoopComponent_跳出复位 对齐 ⑤⑦（OUT_LOOP + INDEX/BROKEN/post_body 复位）
func TestAdvancedLoopComponent_跳出复位(t *testing.T) {
	sess, st := newFakeNodeSession("loop_1", map[state.StateKey]any{
		state.StringKey(constants.Index): 3,
	})
	cb := newRecordingCb()
	a := newTestALC(t, newFakeCond(false, nil), []callbackpkg.LoopCallback{cb.LoopCallback})
	a.nodeSession = sess
	a.postBody.SetFinishIndex(2)

	targets, err := a.Select(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{pregel.End}, targets) // out_loop
	// callbacks：仅 OUT_LOOP（INDEX=3 已在前轮递增过 → finish_index(2)!=index(3) 无 END_ROUND，
	// 对齐 Python elif finish_index == index 分支）
	assert.Equal(t, []string{callbackpkg.StageOutLoop}, cb.calls)
	// 复位：INDEX=nil、BROKEN=false、post_body=-1
	assert.Nil(t, st.values[state.StringKey(constants.Index)])
	assert.Equal(t, false, st.values[state.StringKey(Broken)])
	assert.Equal(t, -1, a.postBody.GetFinishIndex())
}

// TestAdvancedLoopComponent_break中断 对齐 is_broken/break_loop（loop_comp.py:439-446）
func TestAdvancedLoopComponent_break中断(t *testing.T) {
	// break_loop 发生在循环执行中（INDEX 已初始化）——预置 INDEX=0 对齐真实时序
	sess, _ := newFakeNodeSession("loop_1", map[state.StateKey]any{
		state.StringKey(constants.Index): 0,
	})
	a := newTestALC(t, newFakeCond(true, nil), nil)
	a.nodeSession = sess

	a.BreakLoop()
	assert.True(t, a.IsBroken())
	// break 后条件即使为 true 也跳出
	targets, err := a.Select(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{pregel.End}, targets)
}

// TestAdvancedLoopComponent_IsBroken_非bool回退False 对齐 loop_comp.py:441-443
func TestAdvancedLoopComponent_IsBroken_非bool回退False(t *testing.T) {
	sess, st := newFakeNodeSession("loop_1", map[state.StateKey]any{
		state.StringKey(Broken): "not-bool",
	})
	a := newTestALC(t, newFakeCond(true, nil), nil)
	a.nodeSession = sess
	_ = st
	assert.False(t, a.IsBroken())
}

// TestAdvancedLoopComponent_条件错误包装101041 对齐 _atomic_invoke（loop_comp.py:392-400）
func TestAdvancedLoopComponent_条件错误包装101041(t *testing.T) {
	sess, _ := newFakeNodeSession("loop_1", map[state.StateKey]any{})
	// 普通 error → 包装 101041
	a := newTestALC(t, newFakeCond(false, assert.AnError), nil)
	a.nodeSession = sess
	_, err := a.Select(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loop condition execution error")

	// BaseError → 原样透传（Python isinstance(e, BaseError): raise）
	baseErr := exception.BuildError(exception.StatusComponentLoopGroupParamInvalid,
		exception.WithParam("reason", "x"))
	a2 := newTestALC(t, newFakeCond(false, baseErr), nil)
	a2.nodeSession = sess
	_, err = a2.Select(context.Background(), nil)
	require.ErrorIs(t, err, baseErr)
}

// TestAdvancedLoopComponent_构图对位 对齐 loop_comp.py:369-376（三节点三边+条件边）
func TestAdvancedLoopComponent_构图对位(t *testing.T) {
	a := newTestALC(t, newFakeCond(true, nil), nil)
	nodes := a.g.GetNodes()
	assert.Contains(t, nodes, BodyNodeID)
	assert.Contains(t, nodes, ConditionNodeID)
	assert.Contains(t, nodes, PostBodyNodeID)
	// START/END 虚拟节点由 Compile 期注册（8.3a 构图器约定），路由语义由上属用例锁定
	_ = pregel.End
}
