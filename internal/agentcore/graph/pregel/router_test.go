package pregel

import (
	"context"
	"testing"
)

// TestSelectorFunc 函数适配器把 ctx 与 state 原样转发给被适配的函数。
// Python: 无对应物（Python 直接收 callable，由 router.py:39-43 判断是否协程）
func TestSelectorFunc(t *testing.T) {
	type ctxKey string
	key := ctxKey("trace")
	ctx := context.WithValue(context.Background(), key, "abc")

	var gotState any = "sentinel"
	sel := SelectorFunc(func(gotCtx context.Context, state any) ([]string, error) {
		if gotCtx.Value(key) != "abc" {
			t.Errorf("ctx 未透传：期望 \"abc\"")
		}
		gotState = state
		return []string{"D"}, nil
	})

	targets, err := sel.Select(ctx, nil)
	if err != nil {
		t.Fatalf("Select 返回错误：%v", err)
	}
	if len(targets) != 1 || targets[0] != "D" {
		t.Errorf("targets = %v，期望 [D]", targets)
	}
	if gotState != nil {
		t.Errorf("state = %v，期望 nil", gotState)
	}
}
