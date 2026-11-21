package flow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/condition"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeBranchState 测试用可提交状态（graph/atomic_node_test.go 同款桩形）
type fakeBranchState struct {
	state.SessionState
	state.WorkflowState
	globals map[string]any
}

func (s *fakeBranchState) CommitCmp() {}

func (s *fakeBranchState) GetGlobal(key state.StateKey) any { return s.globals[key.String()] }

// nonCommitBranchState 不可提交状态
type nonCommitBranchState struct {
	state.SessionState
}

// fakeBranchSession 测试用最小会话
type fakeBranchSession struct {
	session.BaseSession
	st state.SessionState
}

func (s *fakeBranchSession) State() state.SessionState { return s.st }

// mustBranchSession 构造带可提交状态的会话
func mustBranchSession(t *testing.T) session.BaseSession {
	t.Helper()
	return &fakeBranchSession{st: &fakeBranchState{globals: map[string]any{}}}
}

// mustBranchSessionWithGlobals 构造带全局状态的会话
func mustBranchSessionWithGlobals(t *testing.T, globals map[string]any) session.BaseSession {
	t.Helper()
	return &fakeBranchSession{st: &fakeBranchState{globals: globals}}
}

// alwaysTargetCond 测试桩：恒真条件（控制命中序用）
type alwaysTargetCond struct {
	condition.BaseCondition
}

func newAlwaysTargetCond() *alwaysTargetCond {
	c := &alwaysTargetCond{}
	c.BaseCondition = condition.NewBaseCondition(
		func(_ context.Context, _ map[string]any, _ session.BaseSession) (bool, error) {
			return true, nil
		}, nil)
	return c
}

func (c *alwaysTargetCond) Invoke(_ context.Context, _ map[string]any, _ session.BaseSession) (bool, error) {
	return true, nil
}

func (c *alwaysTargetCond) TraceInfo(_ session.BaseSession) any { return "always" }

// ──────────────────────────── Branch 三态归一 ────────────────────────────

// TestBranch_三态归一 对齐 branch_router.py:24-31 isinstance 链
func TestBranch_三态归一(t *testing.T) {
	b1, err := NewBranch(condition.NewAlwaysTrue(), []string{"x"}, "id1")
	require.NoError(t, err)
	assert.Equal(t, "id1", b1.BranchID)
	_, ok := b1.condition.(*condition.AlwaysTrue)
	assert.True(t, ok)

	b2, err := NewBranch("${a} > 1", []string{"x"}, "")
	require.NoError(t, err)
	_, ok = b2.condition.(*condition.ExpressionCondition)
	assert.True(t, ok)

	b3, err := NewBranch(myBranchFunc, []string{"x"}, "")
	require.NoError(t, err)
	_, ok = b3.condition.(*condition.FuncCondition)
	assert.True(t, ok)

	_, err = NewBranch(123, []string{"x"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "branch condition type does not meet the requirements")
}

func myBranchFunc() bool { return true }

// TestBranch_Evaluate_表达式求值 对齐 evaluate → condition(session)
func TestBranch_Evaluate_表达式求值(t *testing.T) {
	b, err := NewBranch("${a} > 100", []string{"x"}, "")
	require.NoError(t, err)
	hit, err := b.Evaluate(context.Background(), mustBranchSessionWithGlobals(t, map[string]any{"a": 150}))
	require.NoError(t, err)
	assert.True(t, hit)
}

// TestBranch_TraceInfo_透传 对齐 trace_info → self._condition.trace_info(session)
func TestBranch_TraceInfo_透传(t *testing.T) {
	b, err := NewBranch(condition.NewAlwaysTrue(), []string{"x"}, "")
	require.NoError(t, err)
	assert.Equal(t, "True", b.TraceInfo(nil))
}

// ──────────────────────────── BranchRouter ────────────────────────────

// TestBranchRouter_AddBranch_校验与归一 对齐 add_branch nil 校验 + str 归一
func TestBranchRouter_AddBranch_校验与归一(t *testing.T) {
	r := NewBranchRouter(false)
	require.Error(t, r.AddBranch(nil, []string{"x"}, ""))
	require.Error(t, r.AddBranch("a>1", nil, ""))
	require.NoError(t, r.AddBranch("a>1", []string{"x"}, ""))
	assert.Equal(t, map[string]struct{}{"x": {}}, r.AllTargets())
}

// TestBranchRouter_AllTargets_去重 对齐 all_targets set 语义
func TestBranchRouter_AllTargets_去重(t *testing.T) {
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("true", []string{"x", "y"}, "b1"))
	require.NoError(t, r.AddBranch("false", []string{"x"}, "b2"))
	assert.Equal(t, map[string]struct{}{"x": {}, "y": {}}, r.AllTargets())
}

// TestBranchRouter_Call_首命中返回 对齐 __call__ 顺序评估首个命中即返回
func TestBranchRouter_Call_首命中返回(t *testing.T) {
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("${a} > 100", []string{"end_a"}, "high"))
	require.NoError(t, r.AddBranch(newAlwaysTargetCond(), []string{"end_b"}, "default"))
	require.NoError(t, r.SetSession(mustBranchSessionWithGlobals(t, map[string]any{"a": 150})))
	targets, err := r.Call(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"end_a"}, targets)
}

// TestBranchRouter_Call_命中序_前者不命中走后者
func TestBranchRouter_Call_命中序_前者不命中走后者(t *testing.T) {
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("${a} > 100", []string{"end_a"}, "high"))
	require.NoError(t, r.AddBranch(newAlwaysTargetCond(), []string{"end_b"}, "default"))
	require.NoError(t, r.SetSession(mustBranchSessionWithGlobals(t, map[string]any{"a": 1})))
	targets, err := r.Call(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"end_b"}, targets)
}

// TestBranchRouter_Call_全不命中报错 对齐 101021
func TestBranchRouter_Call_全不命中报错(t *testing.T) {
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("${a} > 999", []string{"x"}, "high"))
	require.NoError(t, r.SetSession(mustBranchSessionWithGlobals(t, map[string]any{"a": 1})))
	_, err := r.Call(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "branch meeting the condition was not found")
}

// TestBranchRouter_SetSession_双类型 对齐 set_session 校验
func TestBranchRouter_SetSession_双类型(t *testing.T) {
	r := NewBranchRouter(false)
	require.Error(t, r.SetSession(nil))
	assert.Contains(t, r.SetSession(nil).Error(), "session type is wrong")
	require.NoError(t, r.SetSession(mustBranchSession(t)))
}

// TestBranchRouter_Call_条件求值错误上抛 对齐 evaluate 错误不吞
func TestBranchRouter_Call_条件求值错误上抛(t *testing.T) {
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("${a} +", []string{"x"}, "b1"))
	require.NoError(t, r.SetSession(mustBranchSession(t)))
	_, err := r.Call(context.Background())
	require.Error(t, err)
}

// TestBranchRouter_Select_命中返回目标 对齐 graph.Router（=pregel.Selector）契约
func TestBranchRouter_Select_命中返回目标(t *testing.T) {
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("true", []string{"end_a"}, "b1"))
	require.NoError(t, r.SetSession(mustBranchSession(t)))
	targets, err := r.Select(context.Background(), nil) // state 恒 nil（8.5 D2 惯例）
	require.NoError(t, err)
	assert.Equal(t, []string{"end_a"}, targets)
}

// TestBranchRouter_Select_错误上抛 对齐 error 不包装（8.3b errors.As 依赖）
func TestBranchRouter_Select_错误上抛(t *testing.T) {
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("nope == 1", []string{"x"}, "b1"))
	require.NoError(t, r.SetSession(mustBranchSession(t)))
	_, err := r.Select(context.Background(), nil)
	require.Error(t, err)
}

// TestBranchRouter_reportTrace_无NodeSession形态不panic 接缝守卫
func TestBranchRouter_reportTrace_无NodeSession形态不panic(t *testing.T) {
	r := NewBranchRouter(true)
	require.NoError(t, r.AddBranch("true", []string{"x"}, "b1"))
	require.NoError(t, r.SetSession(mustBranchSession(t)))
	targets, err := r.Call(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"x"}, targets)
}
