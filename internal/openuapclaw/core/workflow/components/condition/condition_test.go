package condition

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeCondState 测试用可提交状态：嵌入 SessionState + WorkflowState 两个接口补齐
// 全接口面（graph/atomic_node_test.go 同款桩），globals 承载 GetGlobal 取值。
type fakeCondState struct {
	state.SessionState
	state.WorkflowState
	globals map[string]any
}

func (s *fakeCondState) CommitCmp() {}

func (s *fakeCondState) GetGlobal(key state.StateKey) any { return s.globals[key.String()] }

// nonCommitState 不可提交状态：只嵌入 SessionState，不实现 WorkflowState。
type nonCommitState struct {
	state.SessionState
}

// fakeCondSession 测试用最小会话：嵌入 BaseSession 补齐接口面，仅 State() 有实现。
type fakeCondSession struct {
	session.BaseSession
	st state.SessionState
}

func (s *fakeCondSession) State() state.SessionState { return s.st }

// mustWorkflowSession 构造带可提交状态（空全局）的会话。
func mustWorkflowSession(t *testing.T) session.BaseSession {
	t.Helper()
	return &fakeCondSession{st: &fakeCondState{globals: map[string]any{}}}
}

// mustWorkflowSessionWithGlobals 构造带全局状态的会话。
func mustWorkflowSessionWithGlobals(t *testing.T, globals map[string]any) session.BaseSession {
	t.Helper()
	return &fakeCondSession{st: &fakeCondState{globals: globals}}
}

// newPlainSession 构造状态不可提交的会话（校验分支3 用）。
func newPlainSession(t *testing.T) session.BaseSession {
	t.Helper()
	return &fakeCondSession{st: &nonCommitState{}}
}

// ──────────────────────────── 测试 ────────────────────────────

// TestFuncCondition_Call_走原子链返回函数值 对齐 condition.py:35-44
func TestFuncCondition_Call_走原子链返回函数值(t *testing.T) {
	called := false
	c := NewFuncCondition(func() bool { called = true; return true })
	got, err := c.Call(context.Background(), mustWorkflowSession(t))
	require.NoError(t, err)
	assert.True(t, got)
	assert.True(t, called)
}

// TestFuncCondition_Invoke_直调不提交 非导出面
func TestFuncCondition_Invoke_直调不提交(t *testing.T) {
	c := NewFuncCondition(func() bool { return false })
	got, err := c.Invoke(context.Background(), map[string]any{}, mustWorkflowSession(t))
	require.NoError(t, err)
	assert.False(t, got)
}

// TestFuncCondition_TraceInfo_返回函数名 对齐 trace_info → self._func.__name__
func TestFuncCondition_TraceInfo_返回函数名(t *testing.T) {
	c := NewFuncCondition(myPredicate)
	assert.Equal(t, "myPredicate", c.TraceInfo(nil))
}

func myPredicate() bool { return true }

// TestAlwaysTrue_Call_恒真 对齐 condition.py:47-52
func TestAlwaysTrue_Call_恒真(t *testing.T) {
	c := NewAlwaysTrue()
	got, err := c.Call(context.Background(), mustWorkflowSession(t))
	require.NoError(t, err)
	assert.True(t, got)
	assert.Equal(t, "True", c.TraceInfo(nil))
}

// TestCondition_Call_session为nil_报sessionNone 对齐 atomic_node.py:13 校验分支1
func TestCondition_Call_session为nil_报sessionNone(t *testing.T) {
	c := NewAlwaysTrue()
	_, err := c.Call(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session is None")
}

// TestCondition_Call_状态非WorkflowCommitState_报错 对齐 atomic_node.py:13 校验分支3
func TestCondition_Call_状态非WorkflowCommitState_报错(t *testing.T) {
	c := NewAlwaysTrue()
	_, err := c.Call(context.Background(), newPlainSession(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session is not support commit state")
}

// TestNewBaseCondition_Nil实现panic 对齐 Python 抽象方法必须实现（D3 同款）
func TestNewBaseCondition_Nil实现panic(t *testing.T) {
	assert.Panics(t, func() { NewBaseCondition(nil, nil) })
}
