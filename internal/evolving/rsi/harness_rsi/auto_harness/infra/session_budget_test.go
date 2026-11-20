package infra

import (
	"context"
	"testing"
	"time"

	autoharness "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/auto_harness"
)

// TestSessionBudgetController_成本阈值预警
func TestSessionBudgetController_成本阈值预警(t *testing.T) {
	b := NewSessionBudgetController(3600, 10.0, 1200)
	b.AddCost(7.9)
	if b.ShouldStop() {
		t.Fatal("7.9/10 未耗尽不应停止")
	}
	b.AddCost(0.1) // 8.0 → 80% 预警
	if b.ShouldStop() {
		t.Fatal("80% 预警但仍未耗尽")
	}
	b.AddCost(2.0) // 10.0 → 耗尽
	if !b.ShouldStop() {
		t.Fatal("成本耗尽应停止")
	}
	if b.RemainingCostUSD() != 0 {
		t.Fatalf("剩余成本 = %v", b.RemainingCostUSD())
	}
}

// TestSessionBudgetController_时钟预算
func TestSessionBudgetController_时钟预算(t *testing.T) {
	base := time.Unix(0, 0)
	cur := base
	b := NewSessionBudgetController(100, 10, 30)
	b.SetNowFn(func() time.Time { return cur })
	b.Start()

	cur = base.Add(30 * time.Second)
	if b.ElapsedSecs() != 30 {
		t.Fatalf("elapsed = %v", b.ElapsedSecs())
	}
	if b.RemainingSecs() != 70 {
		t.Fatalf("remaining = %v", b.RemainingSecs())
	}
	if !b.CheckTaskBudget(0) { // 默认 30 ≤ 70
		t.Fatal("剩余充足应可启动")
	}
	if b.CheckTaskBudget(80) {
		t.Fatal("80 > 70 不应可启动")
	}

	// 90% 触发时间预警、100% 停止
	cur = base.Add(95 * time.Second)
	if b.ShouldStop() {
		t.Fatal("95s < 100s 不应停止")
	}
	cur = base.Add(100 * time.Second)
	if !b.ShouldStop() {
		t.Fatal("100s 应停止")
	}
}

// TestSessionBudgetController_未启动
func TestSessionBudgetController_未启动(t *testing.T) {
	b := NewSessionBudgetController(100, 10, 30)
	if b.ElapsedSecs() != 0 {
		t.Fatalf("未启动 elapsed = %v", b.ElapsedSecs())
	}
	if b.ShouldStop() {
		t.Fatal("未启动且无成本不应停止")
	}
}

// TestDetectPipelineSignal
func TestDetectPipelineSignal(t *testing.T) {
	cfg := autoharness.NewDefaultAutoHarnessConfig()
	cfg.OptimizationGoal = "把 cursor 的补全能力加入我们"
	if got := DetectPipelineSignal(nil, cfg); got != autoharness.ExtendedEvolvePipeline {
		t.Fatalf("goal 信号 = %q", got)
	}
	cfg2 := autoharness.NewDefaultAutoHarnessConfig()
	tasks := []autoharness.OptimizationTask{{Topic: "吸收 hermes 的做法"}}
	if got := DetectPipelineSignal(tasks, cfg2); got != autoharness.ExtendedEvolvePipeline {
		t.Fatalf("task 信号 = %q", got)
	}
	cfg3 := autoharness.NewDefaultAutoHarnessConfig()
	if got := DetectPipelineSignal([]autoharness.OptimizationTask{{Topic: "优化性能"}}, cfg3); got != "" {
		t.Fatalf("无信号 = %q", got)
	}
	if got := DetectPipelineSignal(nil, autoharness.NewDefaultAutoHarnessConfig()); got != "" {
		t.Fatalf("空输入 = %q", got)
	}
}

// TestChooseSessionPipeline
func TestChooseSessionPipeline(t *testing.T) {
	available := []string{autoharness.MetaEvolvePipeline, autoharness.ExtendedEvolvePipeline}

	// 偏好强制指定
	cfg := autoharness.NewDefaultAutoHarnessConfig()
	cfg.PipelinePreference = autoharness.MetaEvolvePipeline
	sel := ChooseSessionPipeline(nil, cfg, available)
	if sel.PipelineName != autoharness.MetaEvolvePipeline || sel.Confidence != 1.0 ||
		sel.Reason != "config pipeline preference" {
		t.Fatalf("偏好 = %+v", sel)
	}

	// auto + 信号命中
	cfg2 := autoharness.NewDefaultAutoHarnessConfig()
	cfg2.OptimizationGoal = "learn from competitor"
	sel2 := ChooseSessionPipeline(nil, cfg2, available)
	if sel2.PipelineName != autoharness.ExtendedEvolvePipeline || sel2.Confidence != 0.85 ||
		sel2.FallbackPipeline != autoharness.MetaEvolvePipeline {
		t.Fatalf("信号 = %+v", sel2)
	}

	// auto 无信号 → 默认 meta
	cfg3 := autoharness.NewDefaultAutoHarnessConfig()
	sel3 := ChooseSessionPipeline(nil, cfg3, available)
	if sel3.PipelineName != autoharness.MetaEvolvePipeline || sel3.Confidence != 0.7 {
		t.Fatalf("默认 = %+v", sel3)
	}

	// available 不含 meta → 取第一个
	cfg4 := autoharness.NewDefaultAutoHarnessConfig()
	sel4 := ChooseSessionPipeline(nil, cfg4, []string{autoharness.ExtendedEvolvePipeline})
	if sel4.PipelineName != autoharness.ExtendedEvolvePipeline {
		t.Fatalf("仅一条可选 = %+v", sel4)
	}

	// 偏好不在 available → 走信号/默认
	cfg5 := autoharness.NewDefaultAutoHarnessConfig()
	cfg5.PipelinePreference = autoharness.MetaEvolvePipeline
	sel5 := ChooseSessionPipeline(nil, cfg5, []string{autoharness.ExtendedEvolvePipeline})
	if sel5.PipelineName != autoharness.ExtendedEvolvePipeline || sel5.Confidence != 0.7 {
		t.Fatalf("偏好不可用回落 = %+v", sel5)
	}
}

// TestFixLoopController_Phase1通过
func TestFixLoopController_Phase1通过(t *testing.T) {
	f := NewFixLoopController(3, 2, 600*time.Second)
	attempts := 0
	res := f.Run(
		func(ctx context.Context) (bool, string, error) {
			attempts++
			if attempts >= 2 {
				return true, "", nil
			}
			return false, "lint error", nil
		},
		func(ctx context.Context, errs string) error { return nil },
		nil,
	)
	if !res.Success || res.Phase != 1 || res.Attempts != 2 {
		t.Fatalf("Phase1 = %+v", res)
	}
	if len(res.ErrorLog) != 1 || res.ErrorLog[0] != "Phase 1 attempt 1: lint error" {
		t.Fatalf("error_log = %v", res.ErrorLog)
	}
}

// TestFixLoopController_Phase1耗尽进Phase2
func TestFixLoopController_Phase1耗尽进Phase2(t *testing.T) {
	f := NewFixLoopController(2, 2, 600*time.Second)
	p1 := 0
	res := f.Run(
		func(ctx context.Context) (bool, string, error) {
			p1++
			return false, "err", nil
		},
		func(ctx context.Context, errs string) error { return nil },
		func(ctx context.Context) (bool, error) {
			return true, nil
		},
	)
	if !res.Success || res.Phase != 2 || res.Attempts != 3 { // p1 2 次 + p2 1 次
		t.Fatalf("Phase2 = %+v", res)
	}
	if p1 != 2 {
		t.Fatalf("p1 次数 = %d", p1)
	}
}

// TestFixLoopController_无evaluator与超时
func TestFixLoopController_无evaluator与超时(t *testing.T) {
	// evaluator nil → Phase1 耗尽后直接返回
	f := NewFixLoopController(1, 2, 600*time.Second)
	res := f.Run(
		func(ctx context.Context) (bool, string, error) { return false, "e", nil },
		func(ctx context.Context, errs string) error { return nil },
		nil,
	)
	if res.Success || res.Phase != 1 {
		t.Fatalf("无 evaluator = %+v", res)
	}

	// CI 超时 → 记录并 continue
	f2 := NewFixLoopController(1, 1, 20*time.Millisecond)
	res2 := f2.Run(
		func(ctx context.Context) (bool, string, error) {
			time.Sleep(50 * time.Millisecond)
			return true, "", nil
		},
		func(ctx context.Context, errs string) error { return nil },
		nil,
	)
	if res2.Success || len(res2.ErrorLog) != 1 ||
		res2.ErrorLog[0] != "Phase 1 attempt 1: CI timeout" {
		t.Fatalf("CI 超时 = %+v", res2)
	}

	// Phase2 evaluator 拒绝 → fixer 调用
	f3 := NewFixLoopController(1, 1, 600*time.Second)
	fixerCalled := 0
	res3 := f3.Run(
		func(ctx context.Context) (bool, string, error) { return false, "e", nil },
		func(ctx context.Context, errs string) error { fixerCalled++; return nil },
		func(ctx context.Context) (bool, error) { return false, nil },
	)
	if res3.Success || fixerCalled != 2 || // phase1 一次 + phase2 一次
		res3.ErrorLog[len(res3.ErrorLog)-1] != "Phase 2 attempt 1: evaluator rejected" {
		t.Fatalf("evaluator 拒绝 = %+v fixer=%d", res3, fixerCalled)
	}
}
