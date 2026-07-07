package interfaces

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewAgentCallbackManager 测试创建回调管理器。
func TestNewAgentCallbackManager(t *testing.T) {
	m := NewAgentCallbackManager("task4_test_agent_1")
	if m == nil {
		t.Fatal("NewAgentCallbackManager 返回 nil")
	}
}

// TestRegisterCallback_And_Execute 测试注册回调并触发执行。
func TestRegisterCallback_And_Execute(t *testing.T) {
	const agentID = "task4_test_agent_3"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	var called int32
	fn := func(_ context.Context, _ any) error {
		atomic.AddInt32(&called, 1)
		return nil
	}

	m.RegisterCallback(context.Background(), CallbackBeforeModelCall, fn)

	if !m.HasHooks(CallbackBeforeModelCall) {
		t.Fatal("注册后 HasHooks 应返回 true")
	}

	// 触发执行（railCtx 传 nil，回调内不使用）
	err := m.Execute(context.Background(), CallbackBeforeModelCall, nil)
	if err != nil {
		t.Fatalf("Execute 返回错误: %v", err)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Fatalf("called = %d, 期望 1", called)
	}
}

// TestHasHooks 测试检查回调是否存在。
func TestHasHooks(t *testing.T) {
	const agentID = "task4_test_agent_4"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	if m.HasHooks(CallbackAfterInvoke) {
		t.Fatal("未注册时 HasHooks 应返回 false")
	}

	fn := func(_ context.Context, _ any) error { return nil }
	m.RegisterCallback(context.Background(), CallbackAfterInvoke, fn)

	if !m.HasHooks(CallbackAfterInvoke) {
		t.Fatal("注册后 HasHooks 应返回 true")
	}
}

// TestUnregister 测试注销回调。
func TestUnregister(t *testing.T) {
	const agentID = "task4_test_agent_5"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	fn := func(_ context.Context, _ any) error { return nil }
	m.RegisterCallback(context.Background(), CallbackBeforeToolCall, fn)

	if !m.HasHooks(CallbackBeforeToolCall) {
		t.Fatal("注册后 HasHooks 应返回 true")
	}

	m.Unregister(CallbackBeforeToolCall, fn)

	if m.HasHooks(CallbackBeforeToolCall) {
		t.Fatal("注销后 HasHooks 应返回 false")
	}
}

// TestClear_指定事件 测试清除指定事件的回调。
func TestClear_指定事件(t *testing.T) {
	const agentID = "task4_test_agent_6"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	fn1 := func(_ context.Context, _ any) error { return nil }
	fn2 := func(_ context.Context, _ any) error { return nil }
	m.RegisterCallback(context.Background(), CallbackOnModelException, fn1)
	m.RegisterCallback(context.Background(), CallbackOnToolException, fn2)

	if !m.HasHooks(CallbackOnModelException) || !m.HasHooks(CallbackOnToolException) {
		t.Fatal("注册后两个事件都应有回调")
	}

	m.Clear(CallbackOnModelException)

	if m.HasHooks(CallbackOnModelException) {
		t.Fatal("Clear 后 OnModelException 事件不应有回调")
	}
	if !m.HasHooks(CallbackOnToolException) {
		t.Fatal("Clear 指定事件不应影响其他事件")
	}
}

// TestClear_全部事件 测试清除所有事件的回调。
func TestClear_全部事件(t *testing.T) {
	const agentID = "task4_test_agent_7"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	fn := func(_ context.Context, _ any) error { return nil }
	m.RegisterCallback(context.Background(), CallbackBeforeInvoke, fn)
	m.RegisterCallback(context.Background(), CallbackAfterInvoke, fn)

	m.Clear()

	if m.HasHooks(CallbackBeforeInvoke) || m.HasHooks(CallbackAfterInvoke) {
		t.Fatal("Clear() 后不应有任何回调")
	}
}

// TestRegisterCallback_优先级 测试优先级排序。
func TestRegisterCallback_优先级(t *testing.T) {
	const agentID = "task4_test_agent_8"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	var order []int
	fn1 := func(_ context.Context, _ any) error {
		order = append(order, 1)
		return nil
	}
	fn2 := func(_ context.Context, _ any) error {
		order = append(order, 2)
		return nil
	}
	fn3 := func(_ context.Context, _ any) error {
		order = append(order, 3)
		return nil
	}

	// 按优先级降序：2(高) → 3(中) → 1(低)
	m.RegisterCallback(context.Background(), CallbackBeforeModelCall, fn1, cb.WithPriority(1))
	m.RegisterCallback(context.Background(), CallbackBeforeModelCall, fn3, cb.WithPriority(5))
	m.RegisterCallback(context.Background(), CallbackBeforeModelCall, fn2, cb.WithPriority(10))

	err := m.Execute(context.Background(), CallbackBeforeModelCall, nil)
	if err != nil {
		t.Fatalf("Execute 返回错误: %v", err)
	}

	if len(order) != 3 || order[0] != 2 || order[1] != 3 || order[2] != 1 {
		t.Fatalf("执行顺序 = %v, 期望 [2 3 1]（优先级降序）", order)
	}
}

// TestExecute_错误中断 测试 error 中断后续回调。
func TestExecute_错误中断(t *testing.T) {
	const agentID = "task4_test_agent_9"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	var called int32
	fnErr := func(_ context.Context, _ any) error {
		return errors.New("测试错误")
	}
	fnAfter := func(_ context.Context, _ any) error {
		atomic.AddInt32(&called, 1)
		return nil
	}

	// 优先级高先执行，fnErr 优先级 10 先执行返回 error
	m.RegisterCallback(context.Background(), CallbackAfterModelCall, fnAfter, cb.WithPriority(1))
	m.RegisterCallback(context.Background(), CallbackAfterModelCall, fnErr, cb.WithPriority(10))

	err := m.Execute(context.Background(), CallbackAfterModelCall, nil)
	if err == nil {
		t.Fatal("期望返回错误，得到 nil")
	}
	if err.Error() != "测试错误" {
		t.Fatalf("错误信息 = %q, 期望 %q", err.Error(), "测试错误")
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Fatal("error 应中断后续回调，fnAfter 不应被执行")
	}
}

// TestRegisterRail_批量注册 测试注册含 2 个钩子的 Rail。
func TestRegisterRail_批量注册(t *testing.T) {
	const agentID = "task67_test_rail_register"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	// 构造一个含 2 个钩子的 Rail
	r := NewBaseRail()
	var beforeCalled, afterCalled int32
	beforeFn := func(_ context.Context, _ any) error {
		atomic.AddInt32(&beforeCalled, 1)
		return nil
	}
	afterFn := func(_ context.Context, _ any) error {
		atomic.AddInt32(&afterCalled, 1)
		return nil
	}

	// 用一个实现了 AgentRail 的测试 struct
	testRail := &testRailWithHooks{
		BaseRail: r,
		callbacks: r.BuildCallbacks(
			r.CallbackFrom(CallbackBeforeModelCall, beforeFn),
			r.CallbackFrom(CallbackAfterModelCall, afterFn),
		),
	}

	err := m.RegisterRail(context.Background(), testRail)
	if err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}

	if !m.HasHooks(CallbackBeforeModelCall) {
		t.Fatal("注册后 BeforeModelCall 应有钩子")
	}
	if !m.HasHooks(CallbackAfterModelCall) {
		t.Fatal("注册后 AfterModelCall 应有钩子")
	}
}

// TestRegisterRail_优先级传递 测试 RegisterRail 传入 rail.Priority() 到 CallbackOption。
func TestRegisterRail_优先级传递(t *testing.T) {
	const agentID = "task67_test_rail_priority"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	r := NewBaseRail().WithPriority(90)
	fn := func(_ context.Context, _ any) error { return nil }
	testRail := &testRailWithHooks{
		BaseRail:  r,
		callbacks: r.BuildCallbacks(r.CallbackFrom(CallbackBeforeInvoke, fn)),
	}

	err := m.RegisterRail(context.Background(), testRail)
	if err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}

	if !m.HasHooks(CallbackBeforeInvoke) {
		t.Fatal("注册后 BeforeInvoke 应有钩子")
	}
}

// TestUnregisterRail_批量注销 测试注销后事件无钩子。
func TestUnregisterRail_批量注销(t *testing.T) {
	const agentID = "task67_test_rail_unregister"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	r := NewBaseRail()
	fn1 := func(_ context.Context, _ any) error { return nil }
	fn2 := func(_ context.Context, _ any) error { return nil }
	testRail := &testRailWithHooks{
		BaseRail: r,
		callbacks: r.BuildCallbacks(
			r.CallbackFrom(CallbackBeforeToolCall, fn1),
			r.CallbackFrom(CallbackAfterToolCall, fn2),
		),
	}

	err := m.RegisterRail(context.Background(), testRail)
	if err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}

	if !m.HasHooks(CallbackBeforeToolCall) || !m.HasHooks(CallbackAfterToolCall) {
		t.Fatal("注册后两个事件都应有钩子")
	}

	err = m.UnregisterRail(context.Background(), testRail)
	if err != nil {
		t.Fatalf("UnregisterRail 返回错误: %v", err)
	}

	if m.HasHooks(CallbackBeforeToolCall) {
		t.Fatal("注销后 BeforeToolCall 不应有钩子")
	}
	if m.HasHooks(CallbackAfterToolCall) {
		t.Fatal("注销后 AfterToolCall 不应有钩子")
	}
}

// testRailWithHooks 用于测试的 AgentRail 实现，覆盖 GetCallbacks。
type testRailWithHooks struct {
	*BaseRail
	callbacks map[AgentCallbackEvent]cb.PerAgentCallbackFunc
}

func (r *testRailWithHooks) GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────
