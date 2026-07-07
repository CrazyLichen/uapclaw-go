package interfaces

import (
	"context"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBaseRail_默认优先级 测试 NewBaseRail 默认优先级为 50。
func TestBaseRail_默认优先级(t *testing.T) {
	r := NewBaseRail()
	if r.Priority() != 50 {
		t.Fatalf("Priority() = %d, 期望 50", r.Priority())
	}
}

// TestBaseRail_WithPriority 测试 WithPriority 设置自定义优先级。
func TestBaseRail_WithPriority(t *testing.T) {
	r := NewBaseRail().WithPriority(80)
	if r.Priority() != 80 {
		t.Fatalf("Priority() = %d, 期望 80", r.Priority())
	}
}

// TestBaseRail_所有钩子为NoOp 测试 10 个钩子方法均返回 nil。
func TestBaseRail_所有钩子为NoOp(t *testing.T) {
	r := NewBaseRail()
	ctx := context.Background()

	if err := r.BeforeInvoke(ctx, nil); err != nil {
		t.Fatalf("BeforeInvoke 返回错误: %v", err)
	}
	if err := r.AfterInvoke(ctx, nil); err != nil {
		t.Fatalf("AfterInvoke 返回错误: %v", err)
	}
	if err := r.BeforeModelCall(ctx, nil); err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}
	if err := r.AfterModelCall(ctx, nil); err != nil {
		t.Fatalf("AfterModelCall 返回错误: %v", err)
	}
	if err := r.OnModelException(ctx, nil); err != nil {
		t.Fatalf("OnModelException 返回错误: %v", err)
	}
	if err := r.BeforeToolCall(ctx, nil); err != nil {
		t.Fatalf("BeforeToolCall 返回错误: %v", err)
	}
	if err := r.AfterToolCall(ctx, nil); err != nil {
		t.Fatalf("AfterToolCall 返回错误: %v", err)
	}
	if err := r.OnToolException(ctx, nil); err != nil {
		t.Fatalf("OnToolException 返回错误: %v", err)
	}
	if err := r.BeforeTaskIteration(ctx, nil); err != nil {
		t.Fatalf("BeforeTaskIteration 返回错误: %v", err)
	}
	if err := r.AfterTaskIteration(ctx, nil); err != nil {
		t.Fatalf("AfterTaskIteration 返回错误: %v", err)
	}
}

// TestBaseRail_InitUninit为NoOp 测试 Init 和 Uninit 均返回 nil。
func TestBaseRail_InitUninit为NoOp(t *testing.T) {
	r := NewBaseRail()
	if err := r.Init(nil); err != nil {
		t.Fatalf("Init 返回错误: %v", err)
	}
	if err := r.Uninit(nil); err != nil {
		t.Fatalf("Uninit 返回错误: %v", err)
	}
}

// TestBaseRail_GetCallbacks_返回空Map 测试默认 GetCallbacks 返回空 map。
func TestBaseRail_GetCallbacks_返回空Map(t *testing.T) {
	r := NewBaseRail()
	callbacks := r.GetCallbacks()
	if len(callbacks) != 0 {
		t.Fatalf("GetCallbacks 返回 %d 条映射，期望 0", len(callbacks))
	}
}

// TestCallbackFrom_单条映射 测试 CallbackFrom 构建单条映射并传给 BuildCallbacks。
func TestCallbackFrom_单条映射(t *testing.T) {
	r := NewBaseRail()
	fn := func(_ context.Context, _ any) error { return nil }
	entry := r.CallbackFrom(CallbackBeforeModelCall, fn)
	// 验证 BuildCallbacks 能正确使用 entry
	m := r.BuildCallbacks(entry)
	if len(m) != 1 {
		t.Fatalf("BuildCallbacks 返回 %d 条映射，期望 1", len(m))
	}
	if _, ok := m[CallbackBeforeModelCall]; !ok {
		t.Fatal("缺少 CallbackBeforeModelCall 映射")
	}
}

// TestBuildCallbacks_多条映射 测试 BuildCallbacks 合并多条映射。
func TestBuildCallbacks_多条映射(t *testing.T) {
	r := NewBaseRail()
	fn1 := func(_ context.Context, _ any) error { return nil }
	fn2 := func(_ context.Context, _ any) error { return nil }
	m := r.BuildCallbacks(
		r.CallbackFrom(CallbackBeforeModelCall, fn1),
		r.CallbackFrom(CallbackAfterModelCall, fn2),
	)
	if len(m) != 2 {
		t.Fatalf("BuildCallbacks 返回 %d 条映射，期望 2", len(m))
	}
	if _, ok := m[CallbackBeforeModelCall]; !ok {
		t.Fatal("缺少 CallbackBeforeModelCall 映射")
	}
	if _, ok := m[CallbackAfterModelCall]; !ok {
		t.Fatal("缺少 CallbackAfterModelCall 映射")
	}
}

// TestBuildCallbacks_空输入 测试无参数时返回空 map。
func TestBuildCallbacks_空输入(t *testing.T) {
	r := NewBaseRail()
	m := r.BuildCallbacks()
	if len(m) != 0 {
		t.Fatalf("BuildCallbacks() 返回 %d 条映射，期望 0", len(m))
	}
}

// TestAgentRail_接口满足 测试 BaseRail 满足 AgentRail 接口（编译期检查）。
func TestAgentRail_接口满足(t *testing.T) {
	// 编译期检查：BaseRail 实现了 AgentRail 接口
	var _ AgentRail = NewBaseRail()
}

// TestRailAgent_接口满足 测试 BaseAgent 隐式满足 RailAgent 接口。
func TestRailAgent_接口满足(t *testing.T) {
	// 通过 fakeBaseAgent 验证 BaseAgent 接口可满足
	_ = &fakeBaseAgent{agentID: "test", cbMgr: nil}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
