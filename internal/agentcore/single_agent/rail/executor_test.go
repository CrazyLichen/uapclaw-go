package rail

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewRailExecutor 验证构造函数字段初始化
func TestNewRailExecutor(t *testing.T) {
	re := NewRailExecutor(CallbackBeforeModelCall, CallbackAfterModelCall, CallbackOnModelException)
	assert.Equal(t, CallbackBeforeModelCall, re.Before)
	assert.Equal(t, CallbackAfterModelCall, re.After)
	assert.Equal(t, CallbackOnModelException, re.OnException)
}

// TestRailExecutor_Execute_正常路径 验证 fn 成功时：before → fn → after
func TestRailExecutor_Execute_正常路径(t *testing.T) {
	mgr := NewAgentCallbackManager("test_normal")
	defer mgr.Clear()

	var firedEvents []AgentCallbackEvent
	registerHook := func(event AgentCallbackEvent) {
		mgr.RegisterCallback(context.Background(), event, func(_ context.Context, _ any) error {
			firedEvents = append(firedEvents, event)
			return nil
		})
	}
	registerHook(CallbackBeforeModelCall)
	registerHook(CallbackAfterModelCall)

	agent := &fakeRailAgent{cbMgr: mgr}
	cbc := NewAgentCallbackContext(agent, &ModelCallInputs{}, nil)

	fnCalled := false
	re := NewRailExecutor(CallbackBeforeModelCall, CallbackAfterModelCall, "")
	err := re.Execute(context.Background(), cbc, func() error {
		fnCalled = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, fnCalled)
	assert.Equal(t, []AgentCallbackEvent{CallbackBeforeModelCall, CallbackAfterModelCall}, firedEvents)
}

// TestRailExecutor_Execute_fn出错时触发OnException 验证 fn 出错时：before → fn → on_exception → after
func TestRailExecutor_Execute_fn出错时触发OnException(t *testing.T) {
	mgr := NewAgentCallbackManager("test_exc")
	defer mgr.Clear()

	var firedEvents []AgentCallbackEvent
	registerHook := func(event AgentCallbackEvent) {
		mgr.RegisterCallback(context.Background(), event, func(_ context.Context, _ any) error {
			firedEvents = append(firedEvents, event)
			return nil
		})
	}
	registerHook(CallbackBeforeModelCall)
	registerHook(CallbackOnModelException)
	registerHook(CallbackAfterModelCall)

	agent := &fakeRailAgent{cbMgr: mgr}
	cbc := NewAgentCallbackContext(agent, &ModelCallInputs{}, nil)

	testErr := errors.New("model call failed")
	re := NewRailExecutor(CallbackBeforeModelCall, CallbackAfterModelCall, CallbackOnModelException)
	err := re.Execute(context.Background(), cbc, func() error {
		return testErr
	})

	assert.Equal(t, testErr, err)
	assert.Equal(t, []AgentCallbackEvent{
		CallbackBeforeModelCall,
		CallbackOnModelException,
		CallbackAfterModelCall,
	}, firedEvents)
	// exception 应被设置
	assert.Equal(t, testErr, cbc.Exception())
}

// TestRailExecutor_Execute_forceFinish门控 验证 before 钩子请求 force-finish 时跳过 fn
func TestRailExecutor_Execute_forceFinish门控(t *testing.T) {
	mgr := NewAgentCallbackManager("test_ff")
	defer mgr.Clear()

	var firedEvents []AgentCallbackEvent
	registerHook := func(event AgentCallbackEvent) {
		mgr.RegisterCallback(context.Background(), event, func(_ context.Context, _ any) error {
			firedEvents = append(firedEvents, event)
			return nil
		})
	}
	registerHook(CallbackBeforeModelCall)
	registerHook(CallbackAfterModelCall)

	// before 钩子中请求 force-finish
	mgr.RegisterCallback(context.Background(), CallbackBeforeModelCall, func(_ context.Context, railCtx any) error {
		cbc := railCtx.(*AgentCallbackContext)
		cbc.RequestForceFinish(map[string]any{"reason": "early_exit"})
		return nil
	})

	agent := &fakeRailAgent{cbMgr: mgr}
	cbc := NewAgentCallbackContext(agent, &ModelCallInputs{}, nil)

	fnCalled := false
	re := NewRailExecutor(CallbackBeforeModelCall, CallbackAfterModelCall, "")
	err := re.Execute(context.Background(), cbc, func() error {
		fnCalled = true
		return nil
	})

	// force-finish 门控：fn 不应被执行
	assert.NoError(t, err)
	assert.False(t, fnCalled)
	// before 和 after 都应触发
	assert.Contains(t, firedEvents, CallbackBeforeModelCall)
	assert.Contains(t, firedEvents, CallbackAfterModelCall)
}

// TestRailExecutor_Execute_无Before事件 验证 before 为空时不触发 before
func TestRailExecutor_Execute_无Before事件(t *testing.T) {
	mgr := NewAgentCallbackManager("test_no_before")
	defer mgr.Clear()

	var firedEvents []AgentCallbackEvent
	registerHook := func(event AgentCallbackEvent) {
		mgr.RegisterCallback(context.Background(), event, func(_ context.Context, _ any) error {
			firedEvents = append(firedEvents, event)
			return nil
		})
	}
	registerHook(CallbackAfterModelCall)

	agent := &fakeRailAgent{cbMgr: mgr}
	cbc := NewAgentCallbackContext(agent, &ModelCallInputs{}, nil)

	re := NewRailExecutor("", CallbackAfterModelCall, "")
	err := re.Execute(context.Background(), cbc, func() error { return nil })

	assert.NoError(t, err)
	assert.Equal(t, []AgentCallbackEvent{CallbackAfterModelCall}, firedEvents)
}

// TestRailExecutor_Execute_无After事件 验证 after 为空时不触发 after
func TestRailExecutor_Execute_无After事件(t *testing.T) {
	mgr := NewAgentCallbackManager("test_no_after")
	defer mgr.Clear()

	var firedEvents []AgentCallbackEvent
	registerHook := func(event AgentCallbackEvent) {
		mgr.RegisterCallback(context.Background(), event, func(_ context.Context, _ any) error {
			firedEvents = append(firedEvents, event)
			return nil
		})
	}
	registerHook(CallbackBeforeModelCall)

	agent := &fakeRailAgent{cbMgr: mgr}
	cbc := NewAgentCallbackContext(agent, &ModelCallInputs{}, nil)

	re := NewRailExecutor(CallbackBeforeModelCall, "", "")
	err := re.Execute(context.Background(), cbc, func() error { return nil })

	assert.NoError(t, err)
	assert.Equal(t, []AgentCallbackEvent{CallbackBeforeModelCall}, firedEvents)
}

// TestRailExecutor_Execute_全部事件为空 验证 before/after/on_exception 全为空时仅执行 fn
func TestRailExecutor_Execute_全部事件为空(t *testing.T) {
	cbc := NewAgentCallbackContext(nil, nil, nil)

	fnCalled := false
	re := NewRailExecutor("", "", "")
	err := re.Execute(context.Background(), cbc, func() error {
		fnCalled = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, fnCalled)
}

// TestRailExecutor_Execute_before出错时走OnException和After 验证 before 钩子出错时走 on_exception → after(finally)
// 对齐 Python: before 和 fn 在同一 try 块中，before 异常也走 except → finally
func TestRailExecutor_Execute_before出错时走OnException和After(t *testing.T) {
	mgr := NewAgentCallbackManager("test_before_err_exc")
	defer mgr.Clear()

	var firedEvents []AgentCallbackEvent
	registerHook := func(event AgentCallbackEvent) {
		mgr.RegisterCallback(context.Background(), event, func(_ context.Context, _ any) error {
			firedEvents = append(firedEvents, event)
			return nil
		})
	}
	registerHook(CallbackBeforeModelCall)
	registerHook(CallbackOnModelException)
	registerHook(CallbackAfterModelCall)

	beforeErr := errors.New("before hook failed")
	mgr.RegisterCallback(context.Background(), CallbackBeforeModelCall, func(_ context.Context, _ any) error {
		return beforeErr
	})

	agent := &fakeRailAgent{cbMgr: mgr}
	cbc := NewAgentCallbackContext(agent, &ModelCallInputs{}, nil)

	fnCalled := false
	re := NewRailExecutor(CallbackBeforeModelCall, CallbackAfterModelCall, CallbackOnModelException)
	err := re.Execute(context.Background(), cbc, func() error {
		fnCalled = true
		return nil
	})

	assert.Equal(t, beforeErr, err)
	assert.False(t, fnCalled) // fn 不应被执行
	// before 错误走 on_exception → after(finally)
	assert.Equal(t, []AgentCallbackEvent{
		CallbackBeforeModelCall,
		CallbackOnModelException,
		CallbackAfterModelCall,
	}, firedEvents)
	// exception 应被设置
	assert.Equal(t, beforeErr, cbc.Exception())
}

// TestRailExecutor_Execute_before出错无OnException 验证 before 出错但无 on_exception 时走 after(finally)
func TestRailExecutor_Execute_before出错无OnException(t *testing.T) {
	mgr := NewAgentCallbackManager("test_before_no_exc")
	defer mgr.Clear()

	var firedEvents []AgentCallbackEvent
	registerHook := func(event AgentCallbackEvent) {
		mgr.RegisterCallback(context.Background(), event, func(_ context.Context, _ any) error {
			firedEvents = append(firedEvents, event)
			return nil
		})
	}
	registerHook(CallbackBeforeModelCall)
	registerHook(CallbackAfterModelCall)

	beforeErr := errors.New("before hook failed")
	mgr.RegisterCallback(context.Background(), CallbackBeforeModelCall, func(_ context.Context, _ any) error {
		return beforeErr
	})

	agent := &fakeRailAgent{cbMgr: mgr}
	cbc := NewAgentCallbackContext(agent, &ModelCallInputs{}, nil)

	re := NewRailExecutor(CallbackBeforeModelCall, CallbackAfterModelCall, "") // 无 on_exception
	err := re.Execute(context.Background(), cbc, func() error { return nil })

	assert.Equal(t, beforeErr, err)
	// before 错误无 on_exception，但仍走 after(finally)
	assert.Equal(t, []AgentCallbackEvent{
		CallbackBeforeModelCall,
		CallbackAfterModelCall,
	}, firedEvents)
}

// TestRailExecutor_Execute_before异常请求重试 验证 before 异常也走重试逻辑
func TestRailExecutor_Execute_before异常请求重试(t *testing.T) {
	mgr := NewAgentCallbackManager("test_before_retry")
	defer mgr.Clear()

	beforeCallCount := 0
	mgr.RegisterCallback(context.Background(), CallbackBeforeModelCall, func(_ context.Context, _ any) error {
		beforeCallCount++
		if beforeCallCount == 1 {
			return errors.New("before first call failed")
		}
		return nil
	})

	// on_exception 钩子：第一次失败后请求重试
	mgr.RegisterCallback(context.Background(), CallbackOnModelException, func(_ context.Context, railCtx any) error {
		cbc := railCtx.(*AgentCallbackContext)
		if cbc.RetryAttempt() == 0 {
			cbc.RequestRetry(0)
		}
		return nil
	})

	var afterFired []AgentCallbackEvent
	mgr.RegisterCallback(context.Background(), CallbackAfterModelCall, func(_ context.Context, _ any) error {
		afterFired = append(afterFired, CallbackAfterModelCall)
		return nil
	})

	agent := &fakeRailAgent{cbMgr: mgr}
	cbc := NewAgentCallbackContext(agent, &ModelCallInputs{}, nil)

	fnCalled := false
	re := NewRailExecutor(CallbackBeforeModelCall, CallbackAfterModelCall, CallbackOnModelException)
	err := re.Execute(context.Background(), cbc, func() error {
		fnCalled = true
		return nil
	})

	// 重试后成功
	assert.NoError(t, err)
	assert.True(t, fnCalled)
	assert.Equal(t, 2, beforeCallCount) // before 被调用两次
	// after 应触发两次（每次迭代都触发，finally 语义）
	assert.Len(t, afterFired, 2)
}

// TestRailExecutor_Execute_context取消时跳过After 验证 context 取消后 after 事件被跳过
func TestRailExecutor_Execute_context取消时跳过After(t *testing.T) {
	mgr := NewAgentCallbackManager("test_cancel")
	defer mgr.Clear()

	var firedEvents []AgentCallbackEvent
	registerHook := func(event AgentCallbackEvent) {
		mgr.RegisterCallback(context.Background(), event, func(_ context.Context, _ any) error {
			firedEvents = append(firedEvents, event)
			return nil
		})
	}
	registerHook(CallbackBeforeModelCall)
	registerHook(CallbackAfterModelCall)

	agent := &fakeRailAgent{cbMgr: mgr}
	cbc := NewAgentCallbackContext(agent, &ModelCallInputs{}, nil)

	// 创建已取消的 context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	re := NewRailExecutor(CallbackBeforeModelCall, CallbackAfterModelCall, "")
	err := re.Execute(cancelledCtx, cbc, func() error { return nil })

	assert.NoError(t, err)
	// before 应触发，但 after 因 context 取消被跳过
	assert.Equal(t, []AgentCallbackEvent{CallbackBeforeModelCall}, firedEvents)
}

// TestRailExecutor_Execute_异常时Exception被设置 验证 fn 出错时 cbc.Exception() 被正确设置
func TestRailExecutor_Execute_异常时Exception被设置(t *testing.T) {
	cbc := NewAgentCallbackContext(nil, &ModelCallInputs{}, nil)

	testErr := errors.New("test error")
	re := NewRailExecutor("", "", "")
	_ = re.Execute(context.Background(), cbc, func() error {
		return testErr
	})

	assert.Equal(t, testErr, cbc.Exception())
}

// TestRailExecutor_Execute_正常时Exception被清空 验证 fn 成功时 cbc.Exception() 为 nil
func TestRailExecutor_Execute_正常时Exception被清空(t *testing.T) {
	cbc := NewAgentCallbackContext(nil, &ModelCallInputs{}, nil)

	// 先设置一个 exception
	cbc.SetException(errors.New("previous error"))

	re := NewRailExecutor("", "", "")
	err := re.Execute(context.Background(), cbc, func() error { return nil })

	assert.NoError(t, err)
	assert.Nil(t, cbc.Exception()) // 执行前 exception 被清空
}

// TestRailExecutor_Execute_RetryAttempt被设置 验证重试计数被正确设置
func TestRailExecutor_Execute_RetryAttempt被设置(t *testing.T) {
	cbc := NewAgentCallbackContext(nil, &ModelCallInputs{}, nil)

	re := NewRailExecutor("", "", "")
	err := re.Execute(context.Background(), cbc, func() error { return nil })

	assert.NoError(t, err)
	assert.Equal(t, 0, cbc.RetryAttempt()) // 首次执行 attempt = 0
}

// TestRailExecutor_Execute_异常无OnException 验证 fn 出错且无 on_exception 时直接返回错误
func TestRailExecutor_Execute_异常无OnException(t *testing.T) {
	cbc := NewAgentCallbackContext(nil, &ModelCallInputs{}, nil)

	testErr := errors.New("test error")
	re := NewRailExecutor(CallbackBeforeModelCall, CallbackAfterModelCall, "") // 无 on_exception
	err := re.Execute(context.Background(), cbc, func() error {
		return testErr
	})

	assert.Equal(t, testErr, err)
}

// ──────────────────────────── 预定义变量验证 ────────────────────────────

// TestModelCallRail 验证 ModelCallRail 预定义变量的三个事件
func TestModelCallRail(t *testing.T) {
	before, after, onException := ModelCallRail.RailEvents()
	assert.Equal(t, CallbackBeforeModelCall, before)
	assert.Equal(t, CallbackAfterModelCall, after)
	assert.Equal(t, CallbackOnModelException, onException)
}

// TestToolCallRail 验证 ToolCallRail 预定义变量的三个事件
func TestToolCallRail(t *testing.T) {
	before, after, onException := ToolCallRail.RailEvents()
	assert.Equal(t, CallbackBeforeToolCall, before)
	assert.Equal(t, CallbackAfterToolCall, after)
	assert.Equal(t, CallbackOnToolException, onException)
}

// ──────────────────────────── RailEvents 方法 ────────────────────────────

// TestRailExecutor_RailEvents 验证 RailEvents() 返回值
func TestRailExecutor_RailEvents(t *testing.T) {
	re := NewRailExecutor(CallbackBeforeToolCall, CallbackAfterToolCall, CallbackOnToolException)
	before, after, onException := re.RailEvents()
	assert.Equal(t, CallbackBeforeToolCall, before)
	assert.Equal(t, CallbackAfterToolCall, after)
	assert.Equal(t, CallbackOnToolException, onException)
}

// ──────────────────────────── isCancelled 辅助函数 ────────────────────────────

// TestIsCancelled 验证 isCancelled 辅助函数
func TestIsCancelled(t *testing.T) {
	// 正常 context
	assert.False(t, isCancelled(context.Background()))

	// 已取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.True(t, isCancelled(ctx))

	// 超时的 context
	timeoutCtx, cancel2 := context.WithTimeout(context.Background(), 0)
	defer cancel2()
	<-timeoutCtx.Done() // 等待超时
	assert.True(t, isCancelled(timeoutCtx))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestRailExecutor_Execute_重试循环 验证 on_exception 钩子请求重试时循环执行
func TestRailExecutor_Execute_重试循环(t *testing.T) {
	mgr := NewAgentCallbackManager("test_retry")
	defer mgr.Clear()

	var firedEvents []AgentCallbackEvent
	registerHook := func(event AgentCallbackEvent) {
		mgr.RegisterCallback(context.Background(), event, func(_ context.Context, _ any) error {
			firedEvents = append(firedEvents, event)
			return nil
		})
	}
	registerHook(CallbackBeforeModelCall)
	registerHook(CallbackOnModelException)
	registerHook(CallbackAfterModelCall)

	callCount := 0
	// on_exception 钩子：第一次失败后请求重试（无延迟），第二次成功
	mgr.RegisterCallback(context.Background(), CallbackOnModelException, func(_ context.Context, railCtx any) error {
		cbc := railCtx.(*AgentCallbackContext)
		if cbc.RetryAttempt() == 0 {
			cbc.RequestRetry(0) // 无延迟重试
		}
		return nil
	})

	agent := &fakeRailAgent{cbMgr: mgr}
	cbc := NewAgentCallbackContext(agent, &ModelCallInputs{}, nil)

	re := NewRailExecutor(CallbackBeforeModelCall, CallbackAfterModelCall, CallbackOnModelException)
	err := re.Execute(context.Background(), cbc, func() error {
		callCount++
		if callCount == 1 {
			return errors.New("first call failed")
		}
		return nil
	})

	// 重试后成功
	assert.NoError(t, err)
	assert.Equal(t, 2, callCount)
	// RetryAttempt 应为 1（第二次尝试）
	assert.Equal(t, 1, cbc.RetryAttempt())
}

// TestRailExecutor_Execute_重试仍失败 验证 on_exception 钩子不请求重试时直接返回错误
func TestRailExecutor_Execute_重试仍失败(t *testing.T) {
	mgr := NewAgentCallbackManager("test_retry_fail")
	defer mgr.Clear()

	// on_exception 钩子不请求重试
	mgr.RegisterCallback(context.Background(), CallbackOnModelException, func(_ context.Context, _ any) error {
		return nil
	})

	agent := &fakeRailAgent{cbMgr: mgr}
	cbc := NewAgentCallbackContext(agent, &ModelCallInputs{}, nil)

	callCount := 0
	re := NewRailExecutor(CallbackBeforeModelCall, CallbackAfterModelCall, CallbackOnModelException)
	err := re.Execute(context.Background(), cbc, func() error {
		callCount++
		return errors.New("always fails")
	})

	// 不重试，直接返回错误
	assert.Error(t, err)
	assert.Equal(t, 1, callCount)
}
