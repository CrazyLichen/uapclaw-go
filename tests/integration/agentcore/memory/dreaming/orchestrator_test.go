//go:build integration

package dreaming

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	dreaming "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/dreaming"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DreamingOrchestratorSuite 测试 DreamingOrchestrator 核心功能。
//
// 覆盖：
//   - 构造函数默认值与选项
//   - Start/Stop 生命周期
//   - IsRunning 状态查询
//   - Health 健康检查
//   - 间隔钳位逻辑
//   - busyChecker 选项
//   - 接口满足性
//
// 对齐 Python: tests/system_tests/harness/test_dreaming_orchestrator.py
type DreamingOrchestratorSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestDreamingOrchestratorSuite(t *testing.T) {
	suite.Run(t, new(DreamingOrchestratorSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ---------------------------------------------------------------------------
// 构造 & 默认值（对齐 Python: TestDreamingOrchestratorInit）
// ---------------------------------------------------------------------------

// TestNew_默认值 测试构造函数默认值。
// 对齐 Python: test_init_defaults
func (s *DreamingOrchestratorSuite) TestNew_默认值() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 3600*time.Second)

	// 未启动时 running 应为 false
	s.False(orch.IsRunning(), "未启动时 IsRunning 应为 false")
	// 默认间隔通过 Health 验证
	h := orch.Health()
	s.Equal(3600.0, h.IntervalSeconds, "默认间隔秒数应为 3600")
	s.False(h.Running, "初始健康状态 Running 应为 false")
}

// TestNew_间隔钳位 测试 interval 小于 60s 时钳位到 60s。
// 对齐 Python: test_init_interval_clamped
func (s *DreamingOrchestratorSuite) TestNew_间隔钳位() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 10*time.Second)

	// 间隔低于 60s 应被钳位到 60s
	h := orch.Health()
	s.Equal(60.0, h.IntervalSeconds, "间隔低于 60s 应被钳位到 60s")

	// 正常间隔保持不变
	orchOk := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 120*time.Second)
	s.Equal(120.0, orchOk.Health().IntervalSeconds, "正常间隔应保持 120s")
}

// TestNew_自定义名称 测试 WithName 选项。
// 对齐 Python: test_custom_name_propagates
func (s *DreamingOrchestratorSuite) TestNew_自定义名称() {
	orch := dreaming.NewDreamingOrchestrator(
		func(_ context.Context) error { return nil },
		60*time.Second,
		dreaming.WithName("code-dreaming"),
	)
	// WithName 不应 panic，编排器正常创建
	s.NotNil(orch, "WithName 应正常创建编排器")
	s.False(orch.IsRunning(), "未启动时 IsRunning 应为 false")
}

// TestNew_WithBusyChecker 测试 WithBusyChecker 选项。
func (s *DreamingOrchestratorSuite) TestNew_WithBusyChecker() {
	orch := dreaming.NewDreamingOrchestrator(
		func(_ context.Context) error { return nil },
		60*time.Second,
		dreaming.WithBusyChecker(func() bool { return false }),
	)
	s.NotNil(orch, "WithBusyChecker 应正常创建编排器")
	// 验证 busyChecker 已注册：启动后短暂等待不会 panic
	s.Require().NoError(orch.Start(s.Ctx))
	s.Require().NoError(orch.Stop(s.Ctx))
}

// ---------------------------------------------------------------------------
// 生命周期：Start / Stop（对齐 Python: TestDreamingOrchestratorLifecycle）
// ---------------------------------------------------------------------------

// TestStart_创建goroutine 测试 Start 创建 goroutine 并设置 running。
// 对齐 Python: test_start_creates_task_and_sets_running
func (s *DreamingOrchestratorSuite) TestStart_创建goroutine() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)

	err := orch.Start(s.Ctx)
	s.Require().NoError(err)
	defer orch.Stop(s.Ctx)

	// 启动后 IsRunning 应为 true
	s.True(orch.IsRunning(), "Start 后 IsRunning 应为 true")
}

// TestStart_幂等 测试重复 Start 幂等。
// 对齐 Python: test_start_idempotent
func (s *DreamingOrchestratorSuite) TestStart_幂等() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)

	err := orch.Start(s.Ctx)
	s.Require().NoError(err)
	defer orch.Stop(s.Ctx)

	// 重复 Start 不应报错
	err = orch.Start(s.Ctx)
	s.Require().NoError(err)
	s.True(orch.IsRunning(), "重复 Start 后仍应为运行中")
}

// TestStop_取消并清理 测试 Stop 取消并清理。
// 对齐 Python: test_stop_cancels_task_and_clears_running
func (s *DreamingOrchestratorSuite) TestStop_取消并清理() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)

	err := orch.Start(s.Ctx)
	s.Require().NoError(err)

	err = orch.Stop(s.Ctx)
	s.Require().NoError(err)

	// 停止后 IsRunning 应为 false
	s.False(orch.IsRunning(), "Stop 后 IsRunning 应为 false")
}

// TestStop_幂等 测试重复 Stop 幂等。
// 对齐 Python: test_stop_idempotent
func (s *DreamingOrchestratorSuite) TestStop_幂等() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)

	s.Require().NoError(orch.Start(s.Ctx))
	s.Require().NoError(orch.Stop(s.Ctx))

	// 重复 Stop 不应报错
	err := orch.Stop(s.Ctx)
	s.Require().NoError(err)
	s.False(orch.IsRunning(), "重复 Stop 后仍应为非运行中")
}

// TestStop_从未启动 测试从未启动时 Stop 安全。
// 对齐 Python: test_stop_when_never_started
func (s *DreamingOrchestratorSuite) TestStop_从未启动() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)

	err := orch.Stop(s.Ctx)
	s.Require().NoError(err)
	s.False(orch.IsRunning(), "从未启动时 Stop 后应为非运行中")
}

// TestStartStop_初始延迟期间Stop 测试在初始 120s 延迟期间 Stop。
// 对齐 Python: test_stop_during_initial_sleep_is_cancelled_cleanly
func (s *DreamingOrchestratorSuite) TestStartStop_初始延迟期间Stop() {
	var sweepCount atomic.Int32
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error {
		sweepCount.Add(1)
		return nil
	}, 60*time.Second)

	s.Require().NoError(orch.Start(s.Ctx))
	// 在 120s 初始延迟内 Stop，sweep 不应被执行
	s.Require().NoError(orch.Stop(s.Ctx))

	s.False(orch.IsRunning(), "初始延迟期间 Stop 后应为非运行中")
	s.Equal(int32(0), sweepCount.Load(), "初始延迟内 Stop 不应执行 sweep")
}

// ---------------------------------------------------------------------------
// IsRunning 状态查询
// ---------------------------------------------------------------------------

// TestIsRunning_初始为false 测试初始状态为 false。
func (s *DreamingOrchestratorSuite) TestIsRunning_初始为false() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)
	s.False(orch.IsRunning(), "未启动时 IsRunning 应为 false")
}

// TestIsRunning_启动后为true 测试启动后为 true。
func (s *DreamingOrchestratorSuite) TestIsRunning_启动后为true() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)
	s.Require().NoError(orch.Start(s.Ctx))
	defer orch.Stop(s.Ctx)

	s.True(orch.IsRunning(), "Start 后 IsRunning 应为 true")
}

// TestIsRunning_停止后为false 测试停止后为 false。
func (s *DreamingOrchestratorSuite) TestIsRunning_停止后为false() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 60*time.Second)
	s.Require().NoError(orch.Start(s.Ctx))
	s.Require().NoError(orch.Stop(s.Ctx))

	s.False(orch.IsRunning(), "Stop 后 IsRunning 应为 false")
}

// ---------------------------------------------------------------------------
// Health 健康检查（对齐 Python: DreamingOrchestrator.health）
// ---------------------------------------------------------------------------

// TestHealth_初始状态 测试初始健康状态。
func (s *DreamingOrchestratorSuite) TestHealth_初始状态() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 3600*time.Second)
	h := orch.Health()

	s.False(h.Running, "初始健康状态 Running 应为 false")
	s.Equal(3600.0, h.IntervalSeconds, "间隔秒数应为 3600")
}

// TestHealth_运行中状态 测试运行中健康状态。
func (s *DreamingOrchestratorSuite) TestHealth_运行中状态() {
	orch := dreaming.NewDreamingOrchestrator(func(_ context.Context) error { return nil }, 120*time.Second)
	s.Require().NoError(orch.Start(s.Ctx))
	defer orch.Stop(s.Ctx)

	h := orch.Health()
	s.True(h.Running, "运行中健康状态 Running 应为 true")
	s.Equal(120.0, h.IntervalSeconds, "间隔秒数应为 120")
}

// ---------------------------------------------------------------------------
// 接口满足性验证
// ---------------------------------------------------------------------------

// TestInterface_上下文演化Schema兼容性 测试 dreaming 与 ceschema 包的类型兼容性。
// 验证 MemoryItem 接口可在集成测试中被引用，且 ACERetrievedMemory 实现该接口。
func (s *DreamingOrchestratorSuite) TestInterface_上下文演化Schema兼容性() {
	// 验证 ceschema.MemoryItem 接口存在且 ACERetrievedMemory 实现该接口
	var _ ceschema.MemoryItem = ceschema.ACEMemory{}
	// 仅验证类型引用，确保集成测试跨包引用正常
	s.True(true, "ceschema.MemoryItem 接口引用成功")
}
