package task_loop

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestMaxRoundsEvaluator_基本功能(t *testing.T) {
	e := NewMaxRoundsEvaluator(3)

	if e.Name() != "max_rounds" {
		t.Errorf("Name() = %q, want %q", e.Name(), "max_rounds")
	}

	ctx := StopEvaluationContext{Iteration: 2}
	if e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=2 with maxRounds=3, want false")
	}

	ctx.Iteration = 3
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=3 with maxRounds=3, want true")
	}

	ctx.Iteration = 5
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=5 with maxRounds=3, want true")
	}
}

func TestMaxRoundsEvaluator_边界值(t *testing.T) {
	e := NewMaxRoundsEvaluator(0)
	ctx := StopEvaluationContext{Iteration: 0}
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=0 with maxRounds=0, want true")
	}

	e1 := NewMaxRoundsEvaluator(1)
	ctx1 := StopEvaluationContext{Iteration: 0}
	if e1.ShouldStop(ctx1) {
		t.Error("ShouldStop at iteration=0 with maxRounds=1, want false")
	}
	ctx1.Iteration = 1
	if !e1.ShouldStop(ctx1) {
		t.Error("ShouldStop at iteration=1 with maxRounds=1, want true")
	}
}

func TestMaxRoundsEvaluator_状态方法(t *testing.T) {
	e := NewMaxRoundsEvaluator(5)

	// 无状态评估器：ExportState 返回空 map
	state := e.ExportState()
	if len(state) != 0 {
		t.Errorf("ExportState() = %v, want empty map", state)
	}

	// ImportState 不 panic 即可
	e.ImportState(map[string]any{"foo": "bar"})

	// Reset 不 panic 即可
	e.Reset()
}

func TestTokenBudgetEvaluator_基本功能(t *testing.T) {
	e := NewTokenBudgetEvaluator(1000)

	if e.Name() != "token_budget" {
		t.Errorf("Name() = %q, want %q", e.Name(), "token_budget")
	}

	ctx := StopEvaluationContext{TokenUsage: 500}
	if e.ShouldStop(ctx) {
		t.Error("ShouldStop at tokenUsage=500 with maxTokens=1000, want false")
	}

	ctx.TokenUsage = 1000
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at tokenUsage=1000 with maxTokens=1000, want true")
	}
}

func TestTokenBudgetEvaluator_边界值(t *testing.T) {
	e := NewTokenBudgetEvaluator(0)
	ctx := StopEvaluationContext{TokenUsage: 0}
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at tokenUsage=0 with maxTokens=0, want true")
	}
}

func TestTokenBudgetEvaluator_状态方法(t *testing.T) {
	e := NewTokenBudgetEvaluator(100)
	state := e.ExportState()
	if len(state) != 0 {
		t.Errorf("ExportState() = %v, want empty map", state)
	}
	e.ImportState(nil)
	e.Reset()
}

func TestTimeoutEvaluator_基本功能(t *testing.T) {
	e := NewTimeoutEvaluator(60.0)

	if e.Name() != "timeout" {
		t.Errorf("Name() = %q, want %q", e.Name(), "timeout")
	}

	ctx := StopEvaluationContext{ElapsedSeconds: 30.0}
	if e.ShouldStop(ctx) {
		t.Error("ShouldStop at 30s with timeout=60s, want false")
	}

	ctx.ElapsedSeconds = 60.0
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at 60s with timeout=60s, want true")
	}

	ctx.ElapsedSeconds = 120.0
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at 120s with timeout=60s, want true")
	}
}

func TestTimeoutEvaluator_边界值(t *testing.T) {
	e := NewTimeoutEvaluator(0.0)
	ctx := StopEvaluationContext{ElapsedSeconds: 0.0}
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at 0s with timeout=0s, want true")
	}
}

func TestTimeoutEvaluator_状态方法(t *testing.T) {
	e := NewTimeoutEvaluator(60.0)
	state := e.ExportState()
	if len(state) != 0 {
		t.Errorf("ExportState() = %v, want empty map", state)
	}
	e.ImportState(nil)
	e.Reset()
}

func TestCompletionPromiseEvaluator_基本功能(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 2)

	if e.Name() != "completion_promise" {
		t.Errorf("Name() = %q, want %q", e.Name(), "completion_promise")
	}

	if e.Promise() != "<promise>" {
		t.Errorf("Promise() = %q, want %q", e.Promise(), "<promise>")
	}

	if e.RequiredConfirmations() != 2 {
		t.Errorf("RequiredConfirmations() = %d, want 2", e.RequiredConfirmations())
	}

	// 初始状态：不应停止
	ctx := StopEvaluationContext{}
	if e.ShouldStop(ctx) {
		t.Error("ShouldStop initially, want false")
	}
}

func TestCompletionPromiseEvaluator_连续确认(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 2)

	// 第 1 次满足：还差 1 次
	e.NotifyFulfilled("task done")
	if e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after 1 confirmation with required=2, want false")
	}
	if e.Confirmations() != 1 {
		t.Errorf("Confirmations() = %d, want 1", e.Confirmations())
	}

	// 第 2 次满足：达到所需次数
	e.NotifyFulfilled("task done again")
	if !e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after 2 confirmations with required=2, want true")
	}
	if e.Confirmations() != 2 {
		t.Errorf("Confirmations() = %d, want 2", e.Confirmations())
	}
}

func TestCompletionPromiseEvaluator_中断归零(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 2)

	// 第 1 次满足
	e.NotifyFulfilled("task done")
	if e.Confirmations() != 1 {
		t.Errorf("Confirmations() = %d, want 1", e.Confirmations())
	}

	// 中断：计数归零
	e.NotifyAbsent()
	if e.Confirmations() != 0 {
		t.Errorf("Confirmations() after absent = %d, want 0", e.Confirmations())
	}
	if e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after absent, want false")
	}

	// 重新开始计数
	e.NotifyFulfilled("task done")
	if e.Confirmations() != 1 {
		t.Errorf("Confirmations() = %d, want 1", e.Confirmations())
	}
}

func TestCompletionPromiseEvaluator_仅需1次确认(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 1)

	e.NotifyFulfilled("done")
	if !e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after 1 confirmation with required=1, want true")
	}
}

func TestCompletionPromiseEvaluator_所需次数最少为1(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 0)
	if e.RequiredConfirmations() != 1 {
		t.Errorf("RequiredConfirmations() = %d, want 1 (至少为1)", e.RequiredConfirmations())
	}

	e2 := NewCompletionPromiseEvaluator("<promise>", -5)
	if e2.RequiredConfirmations() != 1 {
		t.Errorf("RequiredConfirmations() = %d, want 1 (至少为1)", e2.RequiredConfirmations())
	}
}

func TestCompletionPromiseEvaluator_状态导出导入(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 3)

	// 模拟 2 次确认
	e.NotifyFulfilled("done")
	e.NotifyFulfilled("done again")

	state := e.ExportState()
	if state["confirmation_count"] != 2 {
		t.Errorf("ExportState confirmation_count = %v, want 2", state["confirmation_count"])
	}
	if state["fulfilled"] != false {
		t.Errorf("ExportState fulfilled = %v, want false", state["fulfilled"])
	}
	if state["required_confirmations"] != 3 {
		t.Errorf("ExportState required_confirmations = %v, want 3", state["required_confirmations"])
	}

	// 恢复到新评估器
	e2 := NewCompletionPromiseEvaluator("<promise>", 1)
	e2.ImportState(state)
	if e2.Confirmations() != 2 {
		t.Errorf("Confirmations after ImportState = %d, want 2", e2.Confirmations())
	}
	if e2.RequiredConfirmations() != 3 {
		t.Errorf("RequiredConfirmations after ImportState = %d, want 3", e2.RequiredConfirmations())
	}
	// confirmationCount(2) < requiredConfirmations(3)，所以 fulfilled 仍为 false
	if e2.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after ImportState with count=2 < required=3, want false")
	}

	// 再确认 1 次达到 3 次
	e2.NotifyFulfilled("final")
	if !e2.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after 3rd confirmation with required=3, want true")
	}
}

func TestCompletionPromiseEvaluator_状态导入覆盖fulfilled(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 2)

	// 直接导入一个 fulfilled=true 的状态
	e.ImportState(map[string]any{
		"fulfilled":              true,
		"matched_text":           "done",
		"required_confirmations": 2,
		"confirmation_count":     2,
	})
	if !e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after ImportState with fulfilled=true, want true")
	}
}

func TestCompletionPromiseEvaluator_状态导入空数据(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 2)
	e.NotifyFulfilled("done")

	// 导入 nil 不 panic
	e.ImportState(nil)

	// 导入空 map 不 panic
	e.ImportState(map[string]any{})
}

func TestCompletionPromiseEvaluator_Reset(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 1)
	e.NotifyFulfilled("done")

	if !e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop before Reset, want true")
	}

	e.Reset()

	if e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after Reset, want false")
	}
	if e.Confirmations() != 0 {
		t.Errorf("Confirmations after Reset = %d, want 0", e.Confirmations())
	}
}

func TestCustomPredicateEvaluator_基本功能(t *testing.T) {
	called := false
	e := NewCustomPredicateEvaluator("custom_test", func(ctx StopEvaluationContext) bool {
		called = true
		return ctx.Iteration >= 5
	})

	if e.Name() != "custom_test" {
		t.Errorf("Name() = %q, want %q", e.Name(), "custom_test")
	}

	ctx := StopEvaluationContext{Iteration: 3}
	if e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=3, want false")
	}
	if !called {
		t.Error("predicate was not called")
	}

	called = false
	ctx.Iteration = 5
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=5, want true")
	}
}

func TestCustomPredicateEvaluator_状态方法(t *testing.T) {
	e := NewCustomPredicateEvaluator("test", func(ctx StopEvaluationContext) bool {
		return false
	})
	state := e.ExportState()
	if len(state) != 0 {
		t.Errorf("ExportState() = %v, want empty map", state)
	}
	e.ImportState(nil)
	e.Reset()
}
