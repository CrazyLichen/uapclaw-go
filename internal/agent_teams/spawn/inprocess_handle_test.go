package spawn_test

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
)

// TestNewInProcessSpawnHandle_基本属性 测试句柄的基本属性。
func TestNewInProcessSpawnHandle_基本属性(t *testing.T) {
	done := make(chan struct{})
	cancelCtx := func() {}

	h := spawn.NewInProcessSpawnHandle("inproc-test", cancelCtx, done, nil)

	if h.ProcessID() != "inproc-test" {
		t.Errorf("ProcessID() = %q, want %q", h.ProcessID(), "inproc-test")
	}
	if !h.IsAlive() {
		t.Error("IsAlive() = false, want true（done chan 未关闭）")
	}
	if !h.IsHealthy() {
		t.Error("IsHealthy() = false, want true（alive 且未请求关闭）")
	}
}

// TestInProcessSpawnHandle_IsAlive_done关闭后返回false 测试 done 关闭后 IsAlive 返回 false。
func TestInProcessSpawnHandle_IsAlive_done关闭后返回false(t *testing.T) {
	done := make(chan struct{})
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, done, nil)

	close(done)

	if h.IsAlive() {
		t.Error("IsAlive() = true, want false（done 已关闭）")
	}
}

// TestInProcessSpawnHandle_IsHealthy_shutdownRequested后返回false 测试请求关闭后 IsHealthy 返回 false。
func TestInProcessSpawnHandle_IsHealthy_shutdownRequested后返回false(t *testing.T) {
	done := make(chan struct{})
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, done, nil)

	h.SetOnUnhealthy(func() {})
	// 模拟请求关闭
	_ = h.ForceKill()

	if h.IsHealthy() {
		t.Error("IsHealthy() = true, want false（已请求关闭）")
	}
}

// TestInProcessSpawnHandle_Shutdown_正常关闭 测试正常关闭流程。
func TestInProcessSpawnHandle_Shutdown_正常关闭(t *testing.T) {
	done := make(chan struct{})
	cancelCalled := false
	cancel := func() { cancelCalled = true }

	h := spawn.NewInProcessSpawnHandle("inproc-test", cancel, done, nil)

	// 模拟 goroutine 完成后关闭 done
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(done)
	}()

	graceful, err := h.Shutdown(context.Background(), 2*time.Second)
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
	if !graceful {
		t.Error("Shutdown() graceful = false, want true")
	}
	if !cancelCalled {
		t.Error("cancel 未被调用")
	}
}

// TestInProcessSpawnHandle_Shutdown_已关闭时返回true 测试已关闭时 Shutdown 返回 true。
func TestInProcessSpawnHandle_Shutdown_已关闭时返回true(t *testing.T) {
	done := make(chan struct{})
	close(done)
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, done, nil)

	graceful, err := h.Shutdown(context.Background(), 1*time.Second)
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
	if !graceful {
		t.Error("Shutdown() graceful = false, want true（已关闭）")
	}
}

// TestInProcessSpawnHandle_ForceKill 测试强制终止。
func TestInProcessSpawnHandle_ForceKill(t *testing.T) {
	done := make(chan struct{})
	cancelCalled := false
	cancel := func() { cancelCalled = true }

	h := spawn.NewInProcessSpawnHandle("inproc-test", cancel, done, nil)

	err := h.ForceKill()
	if err != nil {
		t.Errorf("ForceKill() error = %v", err)
	}
	if !cancelCalled {
		t.Error("cancel 未被调用")
	}
}

// TestInProcessSpawnHandle_WaitForCompletion_正常 测试正常等待完成。
func TestInProcessSpawnHandle_WaitForCompletion_正常(t *testing.T) {
	done := make(chan struct{})
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, done, nil)

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(done)
	}()

	code, err := h.WaitForCompletion()
	if err != nil {
		t.Errorf("WaitForCompletion() error = %v", err)
	}
	if code != 0 {
		t.Errorf("WaitForCompletion() code = %d, want 0", code)
	}
}

// TestInProcessSpawnHandle_StartHealthCheck_noop 测试健康检查为 no-op。
func TestInProcessSpawnHandle_StartHealthCheck_noop(t *testing.T) {
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, make(chan struct{}), nil)

	err := h.StartHealthCheck(context.Background())
	if err != nil {
		t.Errorf("StartHealthCheck() error = %v, want nil（no-op）", err)
	}

	err = h.StopHealthCheck()
	if err != nil {
		t.Errorf("StopHealthCheck() error = %v, want nil（no-op）", err)
	}
}

// TestInProcessSpawnHandle_SetOnUnhealthy 测试设置不健康回调。
func TestInProcessSpawnHandle_SetOnUnhealthy(t *testing.T) {
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, make(chan struct{}), nil)

	called := false
	h.SetOnUnhealthy(func() { called = true })

	// ForceKill 不应触发 onUnhealthy
	_ = h.ForceKill()
	if called {
		t.Error("onUnhealthy 不应在 ForceKill 时自动触发")
	}
}

// TestInProcessSpawnHandle_OnUnhealthy_触发回调 测试手动触发不健康回调。
func TestInProcessSpawnHandle_OnUnhealthy_触发回调(t *testing.T) {
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, make(chan struct{}), nil)

	called := false
	h.SetOnUnhealthy(func() { called = true })

	h.OnUnhealthy()
	if !called {
		t.Error("OnUnhealthy() 应触发回调")
	}
}

// TestInProcessSpawnHandle_满足SpawnHandle接口 编译期断言。
func TestInProcessSpawnHandle_满足SpawnHandle接口(t *testing.T) {
	var _ spawn.SpawnHandle = (*spawn.InProcessSpawnHandle)(nil)
}

// TestInProcessSpawnHandle_Shutdown_重复请求 测试重复 Shutdown 请求。
func TestInProcessSpawnHandle_Shutdown_重复请求(t *testing.T) {
	done := make(chan struct{})
	close(done)
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, done, nil)

	graceful, _ := h.Shutdown(context.Background(), 1*time.Second)
	if !graceful {
		t.Error("第一次 Shutdown() graceful = false, want true")
	}

	// 第二次应返回错误（shutdownRequested）
	_, err := h.Shutdown(context.Background(), 1*time.Second)
	if err == nil {
		t.Error("第二次 Shutdown() error = nil, want error（关闭已请求）")
	}
}
