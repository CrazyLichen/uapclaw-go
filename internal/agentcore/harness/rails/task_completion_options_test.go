package rails

import (
	"context"
	"testing"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewTaskCompletionRail_默认值 验证构造后的默认字段。
func TestNewTaskCompletionRail_默认值(t *testing.T) {
	r := NewTaskCompletionRail()
	if r == nil {
		t.Fatal("不应返回 nil")
	}
	if r.requiredConfirmations != 1 {
		t.Fatalf("requiredConfirmations = %d, 期望 1", r.requiredConfirmations)
	}
	if r.goalLanguage != "cn" {
		t.Fatalf("默认 goalLanguage 应为 cn，实际 %q", r.goalLanguage)
	}
}

// TestTaskCompletionRail_GoalLanguage 验证选项与 setter 对 goal 语言的赋值。
func TestTaskCompletionRail_GoalLanguage(t *testing.T) {
	t.Run("构造期选项生效", func(t *testing.T) {
		r := NewTaskCompletionRail(WithGoalLanguage("en"))
		if r.goalLanguage != "en" {
			t.Fatalf("goalLanguage = %q, 期望 en", r.goalLanguage)
		}
	})

	t.Run("SetGoalLanguage 覆盖", func(t *testing.T) {
		r := NewTaskCompletionRail()
		r.SetGoalLanguage("en")
		if r.goalLanguage != "en" {
			t.Fatalf("goalLanguage = %q, 期望 en", r.goalLanguage)
		}
	})
}

// TestTaskCompletionRail_GoalReportSink 验证 sink 惰性创建且幂等。
func TestTaskCompletionRail_GoalReportSink(t *testing.T) {
	r := NewTaskCompletionRail()
	first := r.GoalReportSink()
	if first == nil {
		t.Fatal("应惰性创建 sink")
	}
	if second := r.GoalReportSink(); second != first {
		t.Fatal("重复调用应返回同一实例")
	}
}

// TestTaskCompletionRail_SetGoalManager 验证注入后惰性建 sink 与 evaluator。
func TestTaskCompletionRail_SetGoalManager(t *testing.T) {
	r := NewTaskCompletionRail()
	assertNotPanicsRail(t, func() { r.SetGoalManager(nil) })
	if r.goalReportSink == nil {
		t.Fatal("SetGoalManager 后应创建 sink")
	}
	if r.goalEvaluator == nil {
		t.Fatal("SetGoalManager 后应创建 evaluator")
	}
}

// TestTaskCompletionRail_BuildEvaluators 验证评估器按配置装配。
func TestTaskCompletionRail_BuildEvaluators(t *testing.T) {
	t.Run("默认无评估器", func(t *testing.T) {
		r := NewTaskCompletionRail()
		if got := r.BuildEvaluators(); len(got) != 0 {
			t.Fatalf("默认应无评估器，实际 %d", len(got))
		}
	})

	t.Run("配置三项后各生成一个", func(t *testing.T) {
		r := NewTaskCompletionRail()
		r.maxRounds = 3
		r.timeoutSeconds = 60
		r.completionPromise = "DONE"
		if got := r.BuildEvaluators(); len(got) != 3 {
			t.Fatalf("应生成 3 个评估器，实际 %d", len(got))
		}
	})

	t.Run("额外评估器追加在末尾", func(t *testing.T) {
		r := NewTaskCompletionRail()
		r.extraEvaluators = append(r.extraEvaluators, nil)
		if got := r.BuildEvaluators(); len(got) != 1 {
			t.Fatalf("应含 1 个额外评估器，实际 %d", len(got))
		}
	})
}

// TestTaskCompletionRail_Hooks 验证回调钩子在无 goal 流程时安全返回。
func TestTaskCompletionRail_Hooks(t *testing.T) {
	r := NewTaskCompletionRail()
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)

	t.Run("BeforeInvoke", func(t *testing.T) {
		assertNotPanicsRail(t, func() {
			_ = r.BeforeInvoke(context.Background(), cbc)
		})
	})

	t.Run("AfterModelCall", func(t *testing.T) {
		assertNotPanicsRail(t, func() {
			_ = r.AfterModelCall(context.Background(), cbc)
		})
	})

	t.Run("Uninit 清空 goal 工具列表", func(t *testing.T) {
		// Uninit 需要真实 BaseAgent（内部取 AbilityManager），用包内 fake
		if err := r.Uninit(newFakeBaseAgent()); err != nil {
			t.Fatalf("Uninit 不应报错: %v", err)
		}
		if r.goalTools != nil {
			t.Fatalf("Uninit 后 goalTools 应为 nil，实际 %v", r.goalTools)
		}
	})
}

// assertNotPanicsRail 断言 fn 不 panic。
func assertNotPanicsRail(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("不应 panic，实际 %v", rec)
		}
	}()
	fn()
}
