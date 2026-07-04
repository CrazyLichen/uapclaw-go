package task_loop

import (
	"sync"
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestNewLoopCoordinator_空评估器(t *testing.T) {
	lc := NewLoopCoordinator(nil)
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue with no evaluators, want true")
	}
}

func TestNewLoopCoordinator_构造(t *testing.T) {
	evaluators := []StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		NewTimeoutEvaluator(60.0),
	}
	lc := NewLoopCoordinator(evaluators)

	if lc.Iteration() != 0 {
		t.Errorf("Iteration() = %d, want 0", lc.Iteration())
	}
	if lc.TokenUsage() != 0 {
		t.Errorf("TokenUsage() = %d, want 0", lc.TokenUsage())
	}
	if lc.IsAborted() {
		t.Error("IsAborted(), want false")
	}
	if lc.StopReason() != "" {
		t.Errorf("StopReason() = %q, want empty", lc.StopReason())
	}
}

func TestLoopCoordinator_ShouldContinue_单个评估器(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(3),
	})

	// iteration=0, maxRounds=3 → 继续
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue at iteration=0, want true")
	}

	lc.IncrementIteration() // 1
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue at iteration=1, want true")
	}

	lc.IncrementIteration() // 2
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue at iteration=2, want true")
	}

	lc.IncrementIteration() // 3
	if lc.ShouldContinue() {
		t.Error("ShouldContinue at iteration=3 with maxRounds=3, want false")
	}

	if lc.StopReason() != "MaxRoundsEvaluator" {
		t.Errorf("StopReason() = %q, want %q", lc.StopReason(), "MaxRoundsEvaluator")
	}
}

func TestLoopCoordinator_ShouldContinue_多个评估器OR语义(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		NewTimeoutEvaluator(3600.0),
		NewTokenBudgetEvaluator(10000),
	})

	// 三个条件都不满足 → 继续
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue with no condition met, want true")
	}

	// 设置 token 用量超过预算
	lc.AddTokenUsage(10001)
	if lc.ShouldContinue() {
		t.Error("ShouldContinue after exceeding token budget, want false")
	}
	if lc.StopReason() != "TokenBudgetEvaluator" {
		t.Errorf("StopReason() = %q, want %q", lc.StopReason(), "TokenBudgetEvaluator")
	}
}

func TestLoopCoordinator_ShouldContinue_OR语义第一个命中(t *testing.T) {
	callCount := 0
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(1), // 第一个评估器
		NewCustomPredicateEvaluator("never", func(ctx StopEvaluationContext) bool {
			callCount++
			return false
		}),
	})

	lc.IncrementIteration() // iteration=1, maxRounds=1 → 第一个命中
	if lc.ShouldContinue() {
		t.Error("ShouldContinue after max rounds, want false")
	}
	// OR 语义：第一个评估器命中后不再评估后续评估器
	// callCount 仍为 0，说明第二个评估器未被调用
	if callCount != 0 {
		t.Errorf("second evaluator was called %d times, want 0 (OR short-circuit)", callCount)
	}
}

func TestLoopCoordinator_RequestAbort(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(100),
	})

	if !lc.ShouldContinue() {
		t.Error("ShouldContinue before abort, want true")
	}

	lc.RequestAbort()
	if lc.ShouldContinue() {
		t.Error("ShouldContinue after abort, want false")
	}
	if lc.StopReason() != "Aborted" {
		t.Errorf("StopReason() = %q, want %q", lc.StopReason(), "Aborted")
	}
}

func TestLoopCoordinator_IncrementIteration(t *testing.T) {
	lc := NewLoopCoordinator(nil)

	for i := 0; i < 5; i++ {
		if lc.Iteration() != i {
			t.Errorf("Iteration() before increment = %d, want %d", lc.Iteration(), i)
		}
		lc.IncrementIteration()
	}
	if lc.Iteration() != 5 {
		t.Errorf("Iteration() = %d, want 5", lc.Iteration())
	}
}

func TestLoopCoordinator_AddTokenUsage(t *testing.T) {
	lc := NewLoopCoordinator(nil)

	lc.AddTokenUsage(100)
	if lc.TokenUsage() != 100 {
		t.Errorf("TokenUsage() = %d, want 100", lc.TokenUsage())
	}

	lc.AddTokenUsage(50)
	if lc.TokenUsage() != 150 {
		t.Errorf("TokenUsage() = %d, want 150", lc.TokenUsage())
	}

	// 负数和零无效
	lc.AddTokenUsage(0)
	if lc.TokenUsage() != 150 {
		t.Errorf("TokenUsage() after AddTokenUsage(0) = %d, want 150", lc.TokenUsage())
	}

	lc.AddTokenUsage(-10)
	if lc.TokenUsage() != 150 {
		t.Errorf("TokenUsage() after AddTokenUsage(-10) = %d, want 150", lc.TokenUsage())
	}
}

func TestLoopCoordinator_SetLastResult(t *testing.T) {
	lc := NewLoopCoordinator(nil)

	result := map[string]any{"status": "ok", "data": 42}
	lc.SetLastResult(result)

	// 通过 ExportState 验证 lastResult 被设置
	_ = lc.ExportState()
	// lastResult 不直接导出到 LoopCoordinatorState（Python 也不导出），
	// 但我们可通过 CompletionPromiseEvaluator 间接验证
}

func TestLoopCoordinator_ElapsedSeconds(t *testing.T) {
	lc := NewLoopCoordinator(nil)

	// 未 Reset 时返回 0.0
	elapsed := lc.ElapsedSeconds()
	if elapsed != 0.0 {
		t.Errorf("ElapsedSeconds() before Reset = %f, want 0.0", elapsed)
	}

	// Reset 后返回 > 0
	lc.Reset()
	elapsed = lc.ElapsedSeconds()
	if elapsed < 0 {
		t.Errorf("ElapsedSeconds() after Reset = %f, want >= 0", elapsed)
	}

	// 等待一小段时间后验证时间增长
	time.Sleep(50 * time.Millisecond)
	elapsed2 := lc.ElapsedSeconds()
	if elapsed2 < elapsed {
		t.Errorf("ElapsedSeconds() decreased: %f -> %f", elapsed, elapsed2)
	}
}

func TestLoopCoordinator_零值StartTime不影响ShouldContinue(t *testing.T) {
	// 未 Reset 时，TimeoutEvaluator 不应因零值 ElapsedSeconds 误触发
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewTimeoutEvaluator(60.0),
		NewMaxRoundsEvaluator(10),
	})

	// ElapsedSeconds 为 0.0，远小于 60s，不应触发超时停止
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue should be true when startTime is zero and timeout is 60s")
	}
}

func TestLoopCoordinator_Reset后ElapsedSeconds大于零(t *testing.T) {
	lc := NewLoopCoordinator(nil)

	// Reset 前
	if lc.ElapsedSeconds() != 0.0 {
		t.Errorf("ElapsedSeconds() before Reset = %f, want 0.0", lc.ElapsedSeconds())
	}

	lc.Reset()

	// Reset 后立即应 > 0（虽然可能极小）
	time.Sleep(10 * time.Millisecond)
	if lc.ElapsedSeconds() <= 0 {
		t.Errorf("ElapsedSeconds() after Reset = %f, want > 0", lc.ElapsedSeconds())
	}
}

func TestLoopCoordinator_ExportState_基本(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		NewCompletionPromiseEvaluator("<promise>", 2),
	})

	lc.IncrementIteration()
	lc.IncrementIteration()
	lc.AddTokenUsage(500)

	state := lc.ExportState()
	if state.Iteration != 2 {
		t.Errorf("Iteration = %d, want 2", state.Iteration)
	}
	if state.TokenUsage != 500 {
		t.Errorf("TokenUsage = %d, want 500", state.TokenUsage)
	}
	if state.StopReason != "" {
		t.Errorf("StopReason = %q, want empty", state.StopReason)
	}

	// MaxRoundsEvaluator 无状态 → ExportState 返回 nil，不包含在 evaluator_states 中
	if _, ok := state.EvaluatorStates["MaxRoundsEvaluator"]; ok {
		t.Error("evaluator_states should not contain MaxRoundsEvaluator (stateless)")
	}

	// CompletionPromiseEvaluator 有状态
	if _, ok := state.EvaluatorStates["CompletionPromiseEvaluator"]; !ok {
		t.Error("evaluator_states missing CompletionPromiseEvaluator")
	}
}

func TestLoopCoordinator_ImportState_恢复(t *testing.T) {
	cpe := NewCompletionPromiseEvaluator("<promise>", 3)
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		cpe,
	})

	// 模拟运行后导出
	lc.IncrementIteration()
	lc.IncrementIteration()
	lc.AddTokenUsage(300)
	cpe.NotifyFulfilled("done")

	exported := lc.ExportState()

	// 创建新的 coordinator 并导入
	lc2 := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		NewCompletionPromiseEvaluator("<promise>", 1), // requiredConfirmations 不同
	})
	lc2.ImportState(exported)

	if lc2.Iteration() != 2 {
		t.Errorf("Iteration after ImportState = %d, want 2", lc2.Iteration())
	}
	if lc2.TokenUsage() != 300 {
		t.Errorf("TokenUsage after ImportState = %d, want 300", lc2.TokenUsage())
	}
	if lc2.StopReason() != "" {
		t.Errorf("StopReason after ImportState = %q, want empty", lc2.StopReason())
	}

	// CompletionPromiseEvaluator 应从导入状态恢复
	cpe2 := lc2.GetCompletionPromiseEvaluator()
	if cpe2 == nil {
		t.Fatal("GetCompletionPromiseEvaluator() returned nil")
	}
	if cpe2.Confirmations() != 1 {
		t.Errorf("Confirmations after ImportState = %d, want 1", cpe2.Confirmations())
	}
	if cpe2.RequiredConfirmations() != 3 {
		t.Errorf("RequiredConfirmations after ImportState = %d, want 3", cpe2.RequiredConfirmations())
	}
}

func TestLoopCoordinator_ExportImportState_往返(t *testing.T) {
	cpe := NewCompletionPromiseEvaluator("<promise>", 2)
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(5),
		cpe,
	})

	lc.IncrementIteration()
	lc.AddTokenUsage(100)
	cpe.NotifyFulfilled("done")
	cpe.NotifyFulfilled("done again") // 2 次 → fulfilled

	state1 := lc.ExportState()

	// 导入到新 coordinator
	lc2 := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(5),
		NewCompletionPromiseEvaluator("<promise>", 2),
	})
	lc2.ImportState(state1)

	state2 := lc2.ExportState()

	// 验证两次导出一致
	if state1.Iteration != state2.Iteration {
		t.Errorf("Iteration mismatch: %d vs %d", state1.Iteration, state2.Iteration)
	}
	if state1.TokenUsage != state2.TokenUsage {
		t.Errorf("TokenUsage mismatch: %d vs %d", state1.TokenUsage, state2.TokenUsage)
	}
	if state1.StopReason != state2.StopReason {
		t.Errorf("StopReason mismatch: %q vs %q", state1.StopReason, state2.StopReason)
	}
}

func TestLoopCoordinator_ImportState_空数据(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(5),
	})

	// 导入空状态不 panic
	lc.ImportState(LoopCoordinatorState{})
	lc.ImportState(LoopCoordinatorState{EvaluatorStates: nil})
}

func TestLoopCoordinator_GetCompletionPromiseEvaluator_存在(t *testing.T) {
	cpe := NewCompletionPromiseEvaluator("<promise>", 2)
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		cpe,
	})

	found := lc.GetCompletionPromiseEvaluator()
	if found == nil {
		t.Error("GetCompletionPromiseEvaluator() returned nil, want non-nil")
	}
	if found.Promise() != "<promise>" {
		t.Errorf("Promise() = %q, want %q", found.Promise(), "<promise>")
	}
}

func TestLoopCoordinator_GetCompletionPromiseEvaluator_不存在(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
	})

	found := lc.GetCompletionPromiseEvaluator()
	if found != nil {
		t.Error("GetCompletionPromiseEvaluator() returned non-nil, want nil")
	}
}

func TestLoopCoordinator_Reset(t *testing.T) {
	cpe := NewCompletionPromiseEvaluator("<promise>", 1)
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		cpe,
	})

	lc.IncrementIteration()
	lc.IncrementIteration()
	lc.AddTokenUsage(200)
	cpe.NotifyFulfilled("done")
	lc.RequestAbort()

	lc.Reset()

	if lc.Iteration() != 0 {
		t.Errorf("Iteration after Reset = %d, want 0", lc.Iteration())
	}
	if lc.TokenUsage() != 0 {
		t.Errorf("TokenUsage after Reset = %d, want 0", lc.TokenUsage())
	}
	if lc.IsAborted() {
		t.Error("IsAborted after Reset, want false")
	}
	if lc.StopReason() != "" {
		t.Errorf("StopReason after Reset = %q, want empty", lc.StopReason())
	}
	if cpe.Confirmations() != 0 {
		t.Errorf("CPE Confirmations after Reset = %d, want 0", cpe.Confirmations())
	}
}

func TestLoopCoordinator_并发安全(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(1000),
	})

	var wg sync.WaitGroup
	const goroutines = 10
	const opsPerGoroutine = 100

	// 并发 IncrementIteration
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				lc.IncrementIteration()
			}
		}()
	}

	// 并发 AddTokenUsage
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				lc.AddTokenUsage(1)
			}
		}()
	}

	// 并发 ShouldContinue
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				lc.ShouldContinue()
			}
		}()
	}

	wg.Wait()

	expectedOps := goroutines * opsPerGoroutine
	if lc.Iteration() != expectedOps {
		t.Errorf("Iteration() = %d, want %d", lc.Iteration(), expectedOps)
	}
	if lc.TokenUsage() != expectedOps {
		t.Errorf("TokenUsage() = %d, want %d", lc.TokenUsage(), expectedOps)
	}
}

func TestLoopCoordinator_ShouldContinue_评估器panic不崩溃(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewCustomPredicateEvaluator("panicking", func(ctx StopEvaluationContext) bool {
			panic("test panic")
		}),
		NewMaxRoundsEvaluator(100),
	})

	// panic 的评估器被 recover，循环继续评估后续评估器
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue should be true when panicking evaluator is recovered and no other evaluator stops")
	}
}

// TestLoopCoordinator_CustomPredicate_返回true停止循环 验证自定义谓词返回 true 时停止循环。
// 对齐 Python: test_custom_predicate_stop
func TestLoopCoordinator_CustomPredicate_返回true停止循环(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewCustomPredicateEvaluator("always_stop", func(_ StopEvaluationContext) bool {
			return true
		}),
	})
	if lc.ShouldContinue() {
		t.Error("ShouldContinue with always-true predicate, want false")
	}
	if lc.StopReason() != "always_stop" {
		t.Errorf("StopReason() = %q, want %q", lc.StopReason(), "always_stop")
	}
}

// TestLoopCoordinator_CustomPredicate_返回false继续循环 验证自定义谓词返回 false 时继续循环。
// 对齐 Python: test_custom_predicate_continue
func TestLoopCoordinator_CustomPredicate_返回false继续循环(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewCustomPredicateEvaluator("never_stop", func(_ StopEvaluationContext) bool {
			return false
		}),
	})
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue with always-false predicate, want true")
	}
}
