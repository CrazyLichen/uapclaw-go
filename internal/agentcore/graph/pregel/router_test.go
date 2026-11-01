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

// TestStaticRouter_零目标 空 targets 产出非 nil 空切片，等价「边终止」。
// Python: StaticRouter([]) 用于 END 节点（tests/unit_tests/core/graph/test_pregel.py:75）
func TestStaticRouter_零目标(t *testing.T) {
	msgs, err := NewStaticRouter().Dispatch(context.Background(), "A")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if msgs == nil {
		t.Fatalf("msgs = nil，期望非 nil 空切片（对齐 Python 的 []）")
	}
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d，期望 0", len(msgs))
	}
}

// TestStaticRouter_单目标 1→1 的字段填法。
// Python: router.py:17-18
func TestStaticRouter_单目标(t *testing.T) {
	msgs, err := NewStaticRouter("B").Dispatch(context.Background(), "A")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d，期望 1", len(msgs))
	}
	if msgs[0].Sender != "A" {
		t.Errorf("Sender = %q，期望 \"A\"", msgs[0].Sender)
	}
	if msgs[0].Target != "B" {
		t.Errorf("Target = %q，期望 \"B\"", msgs[0].Target)
	}
	if msgs[0].Kind() != KindTrigger {
		t.Errorf("Kind() = %q，期望 %q", msgs[0].Kind(), KindTrigger)
	}
}

// TestStaticRouter_多目标保序 1→N 的消息顺序与入参顺序逐位一致。
// Python: 列表推导保序（router.py:18）
func TestStaticRouter_多目标保序(t *testing.T) {
	targets := []string{"a", "b", "c", "d", "e"}
	msgs, err := NewStaticRouter(targets...).Dispatch(context.Background(), "src")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if len(msgs) != len(targets) {
		t.Fatalf("len(msgs) = %d，期望 %d", len(msgs), len(targets))
	}
	for i, want := range targets {
		if msgs[i].Target != want {
			t.Errorf("msgs[%d].Target = %q，期望 %q", i, msgs[i].Target, want)
		}
		if msgs[i].Sender != "src" {
			t.Errorf("msgs[%d].Sender = %q，期望 \"src\"", i, msgs[i].Sender)
		}
	}
}

// TestNewStaticRouter_复制入参 构造后调用方改原切片不影响实例（偏差 D5）。
// Python: self.targets = targets 存引用（router.py:15），Go 侧收紧
func TestNewStaticRouter_复制入参(t *testing.T) {
	targets := []string{"A", "B"}
	r := NewStaticRouter(targets...)
	targets[0] = "X"
	targets[1] = "Y"

	msgs, err := r.Dispatch(context.Background(), "src")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d，期望 2", len(msgs))
	}
	if msgs[0].Target != "A" || msgs[1].Target != "B" {
		t.Errorf("targets 被外部改写：%q, %q，期望 \"A\", \"B\"", msgs[0].Target, msgs[1].Target)
	}
}
