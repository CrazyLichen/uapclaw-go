package runtime

import (
	"context"
	"testing"
	"time"
)

func TestNewInteractGate(t *testing.T) {
	g := NewInteractGate()
	if g.Closed() {
		t.Error("新门控不应已关闭")
	}
	if g.Inflight() != 0 {
		t.Errorf("Inflight = %d, want 0", g.Inflight())
	}
}

func TestInteractGate_Admit(t *testing.T) {
	g := NewInteractGate()
	ticket := g.Admit()
	if ticket == nil {
		t.Error("Admit 应返回非 nil 票据")
	}
	if g.Inflight() != 1 {
		t.Errorf("Inflight = %d, want 1", g.Inflight())
	}
}

func TestInteractGate_Admit_关闭后拒绝(t *testing.T) {
	g := NewInteractGate()
	ctx := context.Background()
	_ = g.CloseAndDrain(ctx)
	ticket := g.Admit()
	if ticket != nil {
		t.Error("关闭后 Admit 应返回 nil")
	}
}

func TestInteractGate_ConsumeDone(t *testing.T) {
	g := NewInteractGate()
	ticket := g.Admit()
	g.ConsumeDone(ticket)
	if g.Inflight() != 0 {
		t.Errorf("Inflight = %d, want 0", g.Inflight())
	}
}

func TestInteractGate_ConsumeDone_不同门控的票据(t *testing.T) {
	g1 := NewInteractGate()
	g2 := NewInteractGate()
	ticket := g1.Admit()
	g2.ConsumeDone(ticket) // 不应影响 g1
	if g1.Inflight() != 1 {
		t.Errorf("g1 Inflight = %d, want 1（不同门控票据应被忽略）", g1.Inflight())
	}
}

func TestInteractGate_ConsumeDone_nil票据(t *testing.T) {
	g := NewInteractGate()
	g.Admit()
	g.ConsumeDone(nil) // 不应 panic
	if g.Inflight() != 1 {
		t.Errorf("Inflight = %d, want 1", g.Inflight())
	}
}

func TestInteractGate_CloseAndDrain_无飞行中载荷(t *testing.T) {
	g := NewInteractGate()
	ctx := context.Background()
	err := g.CloseAndDrain(ctx)
	if err != nil {
		t.Errorf("CloseAndDrain error = %v", err)
	}
	if !g.Closed() {
		t.Error("CloseAndDrain 后应已关闭")
	}
}

func TestInteractGate_CloseAndDrain_等飞行中载荷(t *testing.T) {
	g := NewInteractGate()
	ticket := g.Admit()

	done := make(chan error, 1)
	go func() {
		ctx := context.Background()
		done <- g.CloseAndDrain(ctx)
	}()

	// 短暂等待确保 CloseAndDrain 已开始等待
	time.Sleep(50 * time.Millisecond)
	if !g.Closed() {
		t.Error("CloseAndDrain 应已设置 closed 标记")
	}

	// 消费完成 → 应解除 CloseAndDrain 等待
	g.ConsumeDone(ticket)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("CloseAndDrain error = %v", err)
		}
	case <-time.After(time.Second):
		t.Error("CloseAndDrain 超时")
	}
}

func TestInteractGate_CloseAndDrain_ctx取消(t *testing.T) {
	g := NewInteractGate()
	g.Admit()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := g.CloseAndDrain(ctx)
	if err == nil {
		t.Error("ctx 取消时应返回错误")
	}
}

func TestInteractGate_Reset(t *testing.T) {
	g := NewInteractGate()
	ctx := context.Background()
	_ = g.CloseAndDrain(ctx)

	g.Reset()
	if g.Closed() {
		t.Error("Reset 后不应已关闭")
	}
	if g.Inflight() != 0 {
		t.Errorf("Reset 后 Inflight = %d, want 0", g.Inflight())
	}
	// Reset 后应可再次 Admit
	ticket := g.Admit()
	if ticket == nil {
		t.Error("Reset 后 Admit 应返回非 nil 票据")
	}
}

func TestInteractGate_多载荷场景(t *testing.T) {
	g := NewInteractGate()
	t1 := g.Admit()
	t2 := g.Admit()
	t3 := g.Admit()

	if g.Inflight() != 3 {
		t.Errorf("Inflight = %d, want 3", g.Inflight())
	}

	g.ConsumeDone(t1)
	if g.Inflight() != 2 {
		t.Errorf("Inflight = %d, want 2", g.Inflight())
	}

	g.ConsumeDone(t3)
	g.ConsumeDone(t2)
	if g.Inflight() != 0 {
		t.Errorf("Inflight = %d, want 0", g.Inflight())
	}
}

func TestInteractGate_CloseAndDrain_多载荷逐个消费(t *testing.T) {
	g := NewInteractGate()
	t1 := g.Admit()
	t2 := g.Admit()

	done := make(chan error, 1)
	go func() {
		ctx := context.Background()
		done <- g.CloseAndDrain(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	g.ConsumeDone(t1)

	time.Sleep(50 * time.Millisecond)
	// 第一个消费完还不应结束
	select {
	case <-done:
		t.Error("还有 inflight 载荷，不应结束等待")
	default:
	}

	g.ConsumeDone(t2)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("CloseAndDrain error = %v", err)
		}
	case <-time.After(time.Second):
		t.Error("CloseAndDrain 超时")
	}
}
