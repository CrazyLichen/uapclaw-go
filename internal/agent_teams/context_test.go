package agent_teams

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInitSessionState_基本初始化 测试创建 SessionState 实例
// 对齐 Python: _session_id_context = ContextVar("session_id", default=None)
func TestInitSessionState_基本初始化(t *testing.T) {
	state := InitSessionState()
	assert.NotNil(t, state)
	assert.Equal(t, "", state.GetSessionID())
}

// TestSessionState_SetSessionID 测试设置 sessionID 后立即可见
// 对齐 Python: set_session_id(session_id) → _session_id_context.set(session_id)
func TestSessionState_SetSessionID(t *testing.T) {
	state := InitSessionState()
	state.SetSessionID("sess-123")
	assert.Equal(t, "sess-123", state.GetSessionID())
}

// TestSessionState_并发安全 测试并发读写不 panic
// Python 不需要锁因为 asyncio 是单线程协程，Go 需要 sync.RWMutex
func TestSessionState_并发安全(t *testing.T) {
	state := InitSessionState()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = state.GetSessionID()
		}()
		go func() {
			defer wg.Done()
			state.SetSessionID("concurrent-id")
		}()
	}
	wg.Wait()
}

// TestWithSessionState_上下文传播 测试注入 ctx 后可取出
// 对齐 Python: _session_id_context.set(state) → _session_id_context.get() == state
func TestWithSessionState_上下文传播(t *testing.T) {
	state := InitSessionState()
	state.SetSessionID("sess-456")
	ctx := WithSessionState(context.Background(), state)
	got := SessionStateFromCtx(ctx)
	assert.Equal(t, state, got)
}

// TestWithSessionState_上下文无SessionState 测试未注入时返回 nil
func TestWithSessionState_上下文无SessionState(t *testing.T) {
	got := SessionStateFromCtx(context.Background())
	assert.Nil(t, got)
}

// TestGetSessionID_从上下文获取 测试全局函数读取
// 对齐 Python: get_session_id()
func TestGetSessionID_从上下文获取(t *testing.T) {
	state := InitSessionState()
	state.SetSessionID("sess-789")
	ctx := WithSessionState(context.Background(), state)
	assert.Equal(t, "sess-789", GetSessionID(ctx))
}

// TestGetSessionID_上下文无SessionState回退 测试 nil 时返回空串
// 对齐 Python: get_session_id() → None → ""
func TestGetSessionID_上下文无SessionState回退(t *testing.T) {
	assert.Equal(t, "", GetSessionID(context.Background()))
}

// Test子Agent隔离 测试子 Agent 创建新 SessionState 不影响父
// 对齐 Python: 子 Task 的 contextvars.copy_context() 隔离
func Test子Agent隔离(t *testing.T) {
	parentState := InitSessionState()
	parentState.SetSessionID("parent-sess")
	parentCtx := WithSessionState(context.Background(), parentState)

	// 子 Agent 创建独立 SessionState
	subState := InitSessionState()
	subState.SetSessionID("sub-sess")
	subCtx := WithSessionState(parentCtx, subState)

	// 子 Agent 修改不影响父
	assert.Equal(t, "parent-sess", GetSessionID(parentCtx))
	assert.Equal(t, "sub-sess", GetSessionID(subCtx))
}

// Test父改子可见 测试同一指针修改后同 ctx 可见
// 对齐 Python: 同 Task 内共享 contextvar，set_session_id 后立即可见
func Test父改子可见(t *testing.T) {
	state := InitSessionState()
	ctx := WithSessionState(context.Background(), state)

	state.SetSessionID("first")
	assert.Equal(t, "first", GetSessionID(ctx))

	state.SetSessionID("second")
	assert.Equal(t, "second", GetSessionID(ctx))
}

// TestSessionState_SetSessionID_清空 测试清空 sessionID
// 对齐 Python: reset_session_id(token) 恢复旧值
// Go 不需要 Token，直接 SetSessionID("") 清空
func TestSessionState_SetSessionID_清空(t *testing.T) {
	state := InitSessionState()
	state.SetSessionID("sess-123")
	state.SetSessionID("")
	assert.Equal(t, "", state.GetSessionID())
}
