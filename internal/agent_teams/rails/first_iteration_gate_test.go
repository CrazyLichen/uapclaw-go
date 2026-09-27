package rails

import (
	"context"
	"testing"
	"time"
)

// TestFirstIterationGate_初始状态 测试门初始关闭
func TestFirstIterationGate_初始状态(t *testing.T) {
	gate := NewFirstIterationGate()
	if gate.IsReady() {
		t.Fatal("gate should not be ready initially")
	}
}

// TestFirstIterationGate_BeforeTaskIteration打开门 测试 BeforeTaskIteration 打开门
func TestFirstIterationGate_BeforeTaskIteration打开门(t *testing.T) {
	gate := NewFirstIterationGate()
	err := gate.BeforeTaskIteration(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeforeTaskIteration returned error: %v", err)
	}
	if !gate.IsReady() {
		t.Fatal("gate should be ready after BeforeTaskIteration")
	}
}

// TestFirstIterationGate_Wait阻塞直到打开 测试 Wait 阻塞然后打开
func TestFirstIterationGate_Wait阻塞直到打开(t *testing.T) {
	gate := NewFirstIterationGate()
	done := make(chan struct{})
	go func() {
		_ = gate.Wait(context.Background())
		close(done)
	}()
	// Wait 应阻塞
	select {
	case <-done:
		t.Fatal("Wait should block until gate is opened")
	case <-time.After(50 * time.Millisecond):
	}
	// 打开门
	_ = gate.BeforeTaskIteration(context.Background(), nil)
	select {
	case <-done:
		// 正常
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait should unblock after BeforeTaskIteration")
	}
}

// TestFirstIterationGate_Reset 测试重置门
func TestFirstIterationGate_Reset(t *testing.T) {
	gate := NewFirstIterationGate()
	_ = gate.BeforeTaskIteration(context.Background(), nil)
	if !gate.IsReady() {
		t.Fatal("gate should be ready")
	}
	gate.Reset()
	if gate.IsReady() {
		t.Fatal("gate should not be ready after Reset")
	}
}

// TestFirstIterationGate_WaitCtx取消 测试 Wait 支持 context 取消
func TestFirstIterationGate_WaitCtx取消(t *testing.T) {
	gate := NewFirstIterationGate()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = gate.Wait(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
		// 正常：context 取消后 Wait 应返回
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait should return when context is cancelled")
	}
}

// TestFirstIterationGate_多次打开幂等 测试多次 BeforeTaskIteration 幂等
func TestFirstIterationGate_多次打开幂等(t *testing.T) {
	gate := NewFirstIterationGate()
	_ = gate.BeforeTaskIteration(context.Background(), nil)
	_ = gate.BeforeTaskIteration(context.Background(), nil)
	if !gate.IsReady() {
		t.Fatal("gate should be ready after multiple BeforeTaskIteration calls")
	}
}

// TestFirstIterationGate_Reset后可再次打开 测试重置后可再次打开
func TestFirstIterationGate_Reset后可再次打开(t *testing.T) {
	gate := NewFirstIterationGate()
	_ = gate.BeforeTaskIteration(context.Background(), nil)
	gate.Reset()
	if gate.IsReady() {
		t.Fatal("gate should not be ready after Reset")
	}
	_ = gate.BeforeTaskIteration(context.Background(), nil)
	if !gate.IsReady() {
		t.Fatal("gate should be ready after BeforeTaskIteration after Reset")
	}
}
