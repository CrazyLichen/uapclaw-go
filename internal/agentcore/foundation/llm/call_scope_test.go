package llm

import (
	"context"
	"testing"
)

// TestGetCurrentLLMCallID_无作用域返回空串 无作用域时 ID 为空
func TestGetCurrentLLMCallID_无作用域返回空串(t *testing.T) {
	if got := GetCurrentLLMCallID(context.Background()); got != "" {
		t.Fatalf("期望空串，得到 %q", got)
	}
}

// TestEnterCallScope_生成随机ID 作用域内生成非空 call id
func TestEnterCallScope_生成随机ID(t *testing.T) {
	ctx, _ := EnterCallScope(context.Background(), false)
	id := GetCurrentLLMCallID(ctx)
	if id == "" {
		t.Fatal("作用域内应生成非空 call id")
	}
	if len(id) != 32 {
		t.Fatalf("uuid4 hex 应为 32 字符，得到 %d", len(id))
	}
}

// TestEnterCallScope_嵌套作用域 内层显式 ID 生效、内层退出后外层 ctx 保持自身值
func TestEnterCallScope_嵌套作用域(t *testing.T) {
	outer, _ := EnterCallScope(context.Background(), false)
	inner, done := EnterCallScopeWithID(outer, "explicit", true)
	if GetCurrentLLMCallID(inner) != "explicit" {
		t.Fatal("显式 call id 应生效")
	}
	if !ExpectsUnifiedLLMCompletion(inner) {
		t.Fatal("unified 标志应为 true")
	}
	done()
	// Python 语义：__exit__ 还原到外层值；Go ctx 不可变，还原表现为继续使用外层 ctx
	if GetCurrentLLMCallID(outer) == "explicit" {
		t.Fatal("外层 ctx 不应被内层污染")
	}
	if ExpectsUnifiedLLMCompletion(outer) {
		t.Fatal("外层 unified 应为 false")
	}
}

// TestExpectsUnifiedLLMCompletion_默认False 无作用域时返回 false
func TestExpectsUnifiedLLMCompletion_默认False(t *testing.T) {
	if ExpectsUnifiedLLMCompletion(context.Background()) {
		t.Fatal("默认应为 false")
	}
}

// TestLlmObservationSuppression_进入退出 抑制标志进入置位、还原闭包还原旧值
func TestLlmObservationSuppression_进入退出(t *testing.T) {
	ctx := context.Background()
	if IsLLMObservationSuppressed(ctx) {
		t.Fatal("默认不抑制")
	}
	// 外层已抑制 → 内层进入再退出应还原为 true（对齐 __exit__ 还原 _previous）
	sup1, restore1 := SuppressLLMObservation(ctx)
	sup2, restore2 := SuppressLLMObservation(sup1)
	if !IsLLMObservationSuppressed(sup2) {
		t.Fatal("抑制中应返回 true")
	}
	_ = restore2()
	_ = restore1()
	if IsLLMObservationSuppressed(ctx) {
		t.Fatal("还原后不应抑制")
	}
}
