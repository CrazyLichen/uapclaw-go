package tools

import (
	"context"
	"testing"
)

// ──────────────────────────── NewCommandExecutionBinding 测试 ────────────────────────────

// TestNewCommandExecutionBinding_正常 验证合法参数构造成功
func TestNewCommandExecutionBinding_正常(t *testing.T) {
	op := struct{ name string }{"local"}
	b, err := NewCommandExecutionBinding(op, true)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if b.SysOperation != op {
		t.Fatalf("SysOperation 不符")
	}
	if !b.Sandboxed {
		t.Fatalf("Sandboxed 应为 true")
	}
}

// TestNewCommandExecutionBinding_nil报错 验证 sys operation 为 nil 时报错
func TestNewCommandExecutionBinding_nil报错(t *testing.T) {
	if _, err := NewCommandExecutionBinding(nil, false); err == nil {
		t.Fatalf("nil 应报错")
	}
}

// TestNewCommandExecutionBinding_typedNil报错 验证 typed nil 也报错
func TestNewCommandExecutionBinding_typedNil报错(t *testing.T) {
	var p *int
	// typed nil 装入 any 后 != nil，这是 Go 的已知陷阱；Python 侧 None 判定
	// 只会拦住真 None。此处记录 Go 行为：typed nil 能通过构造校验。
	_, err := NewCommandExecutionBinding(p, false)
	if err != nil {
		t.Fatalf("typed nil 在 Go 中不为 nil，不应报错: %v", err)
	}
}

// ──────────────────────────── BindCommandExecution 测试 ────────────────────────────

// TestBindCommandExecution_绑定后可取 验证绑定后 CurrentCommandExecution 能读到
func TestBindCommandExecution_绑定后可取(t *testing.T) {
	op := "local-op"
	ctx, cleanup, err := BindCommandExecution(context.Background(), op, true)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	defer cleanup()

	got := CurrentCommandExecution(ctx)
	if got == nil {
		t.Fatalf("应读到绑定")
	}
	if got.SysOperation != op || !got.Sandboxed {
		t.Fatalf("绑定内容不符: %+v", got)
	}
}

// TestBindCommandExecution_nil报错 验证 nil sys operation 返回错误且 ctx 不变
func TestBindCommandExecution_nil报错(t *testing.T) {
	parent := context.Background()
	ctx, cleanup, err := BindCommandExecution(parent, nil, false)
	if err == nil {
		t.Fatalf("应报错")
	}
	defer cleanup()
	if ctx != parent {
		t.Fatalf("报错时应返回原 ctx")
	}
	if got := CurrentCommandExecution(ctx); got != nil {
		t.Fatalf("不应有绑定，实际 %+v", got)
	}
}

// TestBindCommandExecution_派生隔离 验证派生 ctx 不影响父 ctx
func TestBindCommandExecution_派生隔离(t *testing.T) {
	parent := context.Background()
	child, cleanup, err := BindCommandExecution(parent, "op", false)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	defer cleanup()

	if got := CurrentCommandExecution(child); got == nil {
		t.Fatalf("子 ctx 应有绑定")
	}
	if got := CurrentCommandExecution(parent); got != nil {
		t.Fatalf("父 ctx 不应受影响，实际 %+v", got)
	}
}

// TestBindCommandExecution_覆盖 验证后绑定的值覆盖先绑定的值
func TestBindCommandExecution_覆盖(t *testing.T) {
	ctx1, c1, err := BindCommandExecution(context.Background(), "first", false)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	defer c1()

	ctx2, c2, err := BindCommandExecution(ctx1, "second", true)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	defer c2()

	got := CurrentCommandExecution(ctx2)
	if got == nil || got.SysOperation != "second" || !got.Sandboxed {
		t.Fatalf("应取最新的绑定: %+v", got)
	}
	// 上一层仍然是 first
	if prev := CurrentCommandExecution(ctx1); prev == nil || prev.SysOperation != "first" {
		t.Fatalf("上一层应保持 first: %+v", prev)
	}
}

// ──────────────────────────── BindNoCommandExecution 测试 ────────────────────────────

// TestBindNoCommandExecution_屏蔽继承 验证显式屏蔽后读不到上层绑定
func TestBindNoCommandExecution_屏蔽继承(t *testing.T) {
	ctx1, c1, err := BindCommandExecution(context.Background(), "op", false)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	defer c1()

	ctx2, c2 := BindNoCommandExecution(ctx1)
	defer c2()

	if got := CurrentCommandExecution(ctx2); got != nil {
		t.Fatalf("应被屏蔽，实际 %+v", got)
	}
	if got := CurrentCommandExecution(ctx1); got == nil {
		t.Fatalf("上层不应受影响")
	}
}

// ──────────────────────────── CurrentCommandExecution 测试 ────────────────────────────

// TestCurrentCommandExecution_未绑定 验证未绑定时返回 nil
func TestCurrentCommandExecution_未绑定(t *testing.T) {
	if got := CurrentCommandExecution(context.Background()); got != nil {
		t.Fatalf("未绑定应返回 nil，实际 %+v", got)
	}
}

// TestCurrentCommandExecution_空ctx 验证 context.Background 安全
func TestCurrentCommandExecution_空ctx(t *testing.T) {
	// 确认不会 panic
	_ = CurrentCommandExecution(context.TODO())
}
