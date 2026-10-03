package dreaming

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ---------------------------------------------------------------------------
// 构造 & Health（对齐 Python: TestDreamingOrchestratorInit）
// ---------------------------------------------------------------------------

// TestNewDreamingOrchestrator_默认值 测试构造函数默认值。
// Python: test_init_defaults
func TestNewDreamingOrchestrator_默认值(t *testing.T) {
	sweepCalled := func(_ context.Context) error { return nil }
	orch := NewDreamingOrchestrator(sweepCalled, 3600*time.Second)

	assert.Equal(t, "dreaming", orch.name)
	assert.False(t, orch.running)
	assert.Nil(t, orch.cancelFunc)
	assert.Nil(t, orch.busyChecker)
}

// TestNewDreamingOrchestrator_间隔钳位 测试 interval 小于 60s 时钳位到 60s。
// Python: test_init_interval_clamped
func TestNewDreamingOrchestrator_间隔钳位(t *testing.T) {
	sweepCalled := func(_ context.Context) error { return nil }

	// 低于最小值 → 钳位到 60s
	orchLow := NewDreamingOrchestrator(sweepCalled, 10*time.Second)
	assert.Equal(t, minInterval, orchLow.interval)

	// 正常值 → 保持
	orchOk := NewDreamingOrchestrator(sweepCalled, 120*time.Second)
	assert.Equal(t, 120*time.Second, orchOk.interval)
}

// TestNewDreamingOrchestrator_Health属性 测试 health 属性。
// Python: test_health_property
func TestNewDreamingOrchestrator_Health属性(t *testing.T) {
	sweepCalled := func(_ context.Context) error { return nil }
	orch := NewDreamingOrchestrator(sweepCalled, 3600*time.Second)
	h := orch.Health()
	assert.False(t, h["running"].(bool))
	assert.Equal(t, 3600.0, h["interval_seconds"].(float64))
}

// ---------------------------------------------------------------------------
// 生命周期：Start / Stop（对齐 Python: TestDreamingOrchestratorLifecycle）
// ---------------------------------------------------------------------------

// TestDreamingOrchestrator_Start_创建goroutine 测试 start 创建 goroutine 并设置 running。
// Python: test_start_creates_task_and_sets_running
func TestDreamingOrchestrator_Start_创建goroutine(t *testing.T) {
	orch := NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)
	err := orch.Start(context.Background())
	require.NoError(t, err)
	defer orch.Stop(context.Background())

	assert.True(t, orch.IsRunning())
	assert.NotNil(t, orch.doneCh)
}

// TestDreamingOrchestrator_Start_幂等 测试重复 start 幂等。
// Python: test_start_idempotent
func TestDreamingOrchestrator_Start_幂等(t *testing.T) {
	orch := NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)
	err := orch.Start(context.Background())
	require.NoError(t, err)
	defer orch.Stop(context.Background())

	firstDoneCh := orch.doneCh
	err = orch.Start(context.Background())
	require.NoError(t, err)
	assert.Equal(t, firstDoneCh, orch.doneCh)
}

// TestDreamingOrchestrator_Stop_取消并清理 测试 stop 取消并清理。
// Python: test_stop_cancels_task_and_clears_running
func TestDreamingOrchestrator_Stop_取消并清理(t *testing.T) {
	orch := NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)
	err := orch.Start(context.Background())
	require.NoError(t, err)

	err = orch.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, orch.IsRunning())
	assert.Nil(t, orch.doneCh)
}

// TestDreamingOrchestrator_Stop_幂等 测试重复 stop 幂等。
// Python: test_stop_idempotent
func TestDreamingOrchestrator_Stop_幂等(t *testing.T) {
	orch := NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)
	err := orch.Start(context.Background())
	require.NoError(t, err)

	err = orch.Stop(context.Background())
	require.NoError(t, err)
	err = orch.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, orch.IsRunning())
}

// TestDreamingOrchestrator_Stop_从未启动 测试从未启动时 stop 安全。
// Python: test_stop_when_never_started
func TestDreamingOrchestrator_Stop_从未启动(t *testing.T) {
	orch := NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)
	err := orch.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, orch.IsRunning())
}

// ---------------------------------------------------------------------------
// tick — busy backoff & error handling（对齐 Python: TestDreamingOrchestratorTick）
// ---------------------------------------------------------------------------

// TestDreamingOrchestrator_Tick_无busyChecker时执行sweep 测试无 busyChecker 时执行 sweep。
// Python: test_tick_sweep_runs_when_no_busy_checker
func TestDreamingOrchestrator_Tick_无busyChecker时执行sweep(t *testing.T) {
	var sweepCount atomic.Int32
	orch := NewDreamingOrchestrator(func(_ context.Context) error {
		sweepCount.Add(1)
		return nil
	}, 60*time.Second)
	orch.running = true
	orch.tick(context.Background())
	assert.Equal(t, int32(1), sweepCount.Load())
}

// TestDreamingOrchestrator_Tick_busy时跳过sweep 测试 busy 时跳过 sweep。
// Python: test_tick_skips_sweep_when_busy
func TestDreamingOrchestrator_Tick_busy时跳过sweep(t *testing.T) {
	var sweepCount atomic.Int32
	orch := NewDreamingOrchestrator(
		func(_ context.Context) error {
			sweepCount.Add(1)
			return nil
		},
		60*time.Second,
		WithBusyChecker(func() bool { return true }),
	)
	orch.running = true
	orch.tick(context.Background())
	assert.Equal(t, int32(0), sweepCount.Load())
}

// TestDreamingOrchestrator_Tick_不busy时执行sweep 测试不 busy 时执行 sweep。
// Python: test_tick_runs_sweep_when_not_busy
func TestDreamingOrchestrator_Tick_不busy时执行sweep(t *testing.T) {
	var sweepCount atomic.Int32
	var checkerCalled atomic.Int32
	orch := NewDreamingOrchestrator(
		func(_ context.Context) error {
			sweepCount.Add(1)
			return nil
		},
		60*time.Second,
		WithBusyChecker(func() bool {
			checkerCalled.Add(1)
			return false
		}),
	)
	orch.running = true
	orch.tick(context.Background())
	assert.Equal(t, int32(1), checkerCalled.Load())
	assert.Equal(t, int32(1), sweepCount.Load())
}

// TestDreamingOrchestrator_Tick_busyChecker异常不阻塞sweep 测试 busyChecker 异常不阻塞 sweep。
// Python: test_tick_busy_checker_exception_does_not_block_sweep
func TestDreamingOrchestrator_Tick_busyChecker异常不阻塞sweep(t *testing.T) {
	var sweepCount atomic.Int32
	orch := NewDreamingOrchestrator(
		func(_ context.Context) error {
			sweepCount.Add(1)
			return nil
		},
		60*time.Second,
		WithBusyChecker(func() bool { panic("checker crash") }),
	)
	orch.running = true
	orch.tick(context.Background())
	// Python: busy_checker 异常后继续执行 sweep
	assert.Equal(t, int32(1), sweepCount.Load())
}

// TestDreamingOrchestrator_Tick_sweep异常记录日志 测试 sweep 异常不 panic。
// Python: test_tick_sweep_exception_logged
func TestDreamingOrchestrator_Tick_sweep异常记录日志(t *testing.T) {
	orch := NewDreamingOrchestrator(
		func(_ context.Context) error { return errors.New("sweep failure") },
		60*time.Second,
	)
	orch.running = true
	// 不 panic 即可
	orch.tick(context.Background())
}

// TestDreamingOrchestrator_Tick_CancelledError传播 测试 context.Canceled 传播。
// Python: test_tick_cancelled_error_propagates
func TestDreamingOrchestrator_Tick_CancelledError传播(t *testing.T) {
	orch := NewDreamingOrchestrator(
		func(_ context.Context) error { return context.Canceled },
		60*time.Second,
	)
	orch.running = true
	// 不 panic 即可，CancelledError 在 tick 中 return
	orch.tick(context.Background())
}

// ---------------------------------------------------------------------------
// loop 调度（对齐 Python: TestDreamingOrchestratorLoop）
// ---------------------------------------------------------------------------

// TestDreamingOrchestrator_Loop_尊重cancel 测试 loop 尊重 cancel。
// Python: test_loop_respects_cancelled_error
func TestDreamingOrchestrator_Loop_尊重cancel(t *testing.T) {
	orch := NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)
	err := orch.Start(context.Background())
	require.NoError(t, err)
	// Start 后 loop 在运行，cancel 后 Stop 应能正常退出
	err = orch.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, orch.IsRunning())
}

// TestDreamingOrchestrator_Loop_初始sleep期间stop 测试初始 sleep 期间 stop。
// Python: test_stop_during_initial_sleep_is_cancelled_cleanly
func TestDreamingOrchestrator_Loop_初始sleep期间stop(t *testing.T) {
	var sweepCount atomic.Int32
	orch := NewDreamingOrchestrator(func(_ context.Context) error {
		sweepCount.Add(1)
		return nil
	}, 60*time.Second)
	err := orch.Start(context.Background())
	require.NoError(t, err)
	// 在 120s 初始延迟内 stop
	time.Sleep(50 * time.Millisecond)
	err = orch.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, orch.IsRunning())
	assert.Equal(t, int32(0), sweepCount.Load())
}

// TestDreamingOrchestrator_Loop_自定义name传播 测试自定义 name 传播。
// Python: test_custom_name_propagates
func TestDreamingOrchestrator_Loop_自定义name传播(t *testing.T) {
	orch := NewDreamingOrchestrator(
		func(_ context.Context) error { return nil },
		60*time.Second,
		WithName("code-dreaming"),
	)
	assert.Equal(t, "code-dreaming", orch.name)
}
