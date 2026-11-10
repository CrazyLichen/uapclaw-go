package agent

import (
	"context"
	"testing"
	"time"
)

// TestStreamControllerStop_未启动为空操作 验证未 Start 时 Stop 不 panic、不阻塞。
func TestStreamControllerStop_未启动为空操作(t *testing.T) {
	sc := newTestStreamController()
	done := make(chan struct{})
	go func() {
		sc.Stop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop 未启动时阻塞")
	}
}

// TestStreamControllerStop_重复调用幂等 验证 Stop 可重复调用。
func TestStreamControllerStop_重复调用幂等(t *testing.T) {
	sc := newTestStreamController()
	sc.Stop(context.Background())
	sc.Stop(context.Background())
}
