package graph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// fakeAtomicState 测试用可提交状态：嵌入 SessionState + WorkflowState 两个接口补齐
// 全接口面（接口嵌入为 nil，仅覆盖 CommitCmp；本测试不触达其余方法）。
type fakeAtomicState struct {
	state.SessionState
	state.WorkflowState
	commitCmpCalled bool
}

func (s *fakeAtomicState) CommitCmp() { s.commitCmpCalled = true }

// nonCommitState 不可提交状态：只嵌入 SessionState，不实现 WorkflowState（CommitCmp），
// 用于校验分支3。
type nonCommitState struct {
	state.SessionState
}

// fakeAtomicSession 测试用最小会话：嵌入 BaseSession（= interfaces.InnerSession）
// 补齐接口面，仅 State() 有实现。
type fakeAtomicSession struct {
	session.BaseSession
	st state.SessionState
}

func (s *fakeAtomicSession) State() state.SessionState { return s.st }

// TestValidateSessionAndState 校验三分支一比一复刻 atomic_node.py:13-22，
// 顺序不可调换：nil →（D2 注销）→ state 非 WorkflowState。
func TestValidateSessionAndState(t *testing.T) {
	// 分支1：session 为 nil → "session is None"
	_, err := validateSessionAndState(nil)
	require.Error(t, err)
	be, ok := err.(*exception.BaseError)
	require.True(t, ok)
	assert.Equal(t, exception.StatusGraphStateCommitError, be.Status())
	assert.Equal(t, "session is None", be.Params()["reason"])

	// 分支3：state 非 WorkflowState → "session is not support commit state"
	//（分支2 isinstance(BaseSession) 已在 Go 编译期由接口参数类型保证，偏差 D2 注销）
	sess := &fakeAtomicSession{st: &nonCommitState{}}
	_, err = validateSessionAndState(sess)
	require.Error(t, err)
	be = err.(*exception.BaseError)
	assert.Equal(t, exception.StatusGraphStateCommitError, be.Status())
	assert.Equal(t, "session is not support commit state", be.Params()["reason"])

	// 成功路径：返回断言后的 WorkflowState
	sessOK := &fakeAtomicSession{st: &fakeAtomicState{}}
	ws, err := validateSessionAndState(sessOK)
	require.NoError(t, err)
	require.NotNil(t, ws)
}

// TestNewAtomicNode_Nil panic 对齐 Python 抽象方法必须实现的约束（偏差 D3）
func TestNewAtomicNode_Nil(t *testing.T) {
	assert.Panics(t, func() { NewAtomicNode(nil) })
}

// TestAtomicInvoke_Success 成功路径：回调返回值透传 + CommitCmp 在回调之后被调用
func TestAtomicInvoke_Success(t *testing.T) {
	st := &fakeAtomicState{}
	sess := &fakeAtomicSession{st: st}
	called := false
	node := NewAtomicNode(func(ctx context.Context, kwargs map[string]any) (any, error) {
		called = true
		assert.False(t, st.commitCmpCalled, "commit 必须发生在回调之后")
		assert.Equal(t, sess, kwargs["session"])
		return "ok", nil
	})
	result, err := node.AtomicInvoke(context.Background(), map[string]any{"session": sess})
	require.NoError(t, err)
	assert.Equal(t, "ok", result)
	assert.True(t, called)
	assert.True(t, st.commitCmpCalled, "回调成功后必须提交")
}

// TestAtomicInvoke_回调错误 回调失败：错误原样上抛、不提交
func TestAtomicInvoke_回调错误(t *testing.T) {
	st := &fakeAtomicState{}
	sess := &fakeAtomicSession{st: st}
	sentinel := assert.AnError
	node := NewAtomicNode(func(ctx context.Context, kwargs map[string]any) (any, error) {
		return nil, sentinel
	})
	result, err := node.AtomicInvoke(context.Background(), map[string]any{"session": sess})
	require.ErrorIs(t, err, sentinel)
	assert.Nil(t, result)
	assert.False(t, st.commitCmpCalled, "失败不得提交")
}

// TestAtomicInvoke_校验失败 session 缺失：不触碰回调、不提交、错误码与 reason 锁定
func TestAtomicInvoke_校验失败(t *testing.T) {
	called := false
	node := NewAtomicNode(func(ctx context.Context, kwargs map[string]any) (any, error) {
		called = true
		return nil, nil
	})
	result, err := node.AtomicInvoke(context.Background(), map[string]any{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.False(t, called, "校验失败不得进入回调")
	be := err.(*exception.BaseError)
	assert.Equal(t, exception.StatusGraphStateCommitError, be.Status())
	assert.Equal(t, "session is None", be.Params()["reason"])
}
