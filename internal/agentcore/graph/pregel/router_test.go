package pregel

import (
	"context"
	"errors"
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

// TestBarrierRouter_产出汇聚消息 target 是 barrier 通道的合成路由键。
// Python: BarrierRouter([barrier.key])（builder.py:49），键格式见 channels.py:155-164
func TestBarrierRouter_产出汇聚消息(t *testing.T) {
	const key = "barrier:(a|b)&c->d"
	msgs, err := NewBarrierRouter(key).Dispatch(context.Background(), "a")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d，期望 1", len(msgs))
	}
	if msgs[0].Kind() != KindBarrier {
		t.Errorf("Kind() = %q，期望 %q", msgs[0].Kind(), KindBarrier)
	}
	if msgs[0].Sender != "a" {
		t.Errorf("Sender = %q，期望 \"a\"（BarrierChannel 用它判 OR 组）", msgs[0].Sender)
	}
	if msgs[0].Target != key {
		t.Errorf("Target = %q，期望 %q", msgs[0].Target, key)
	}
}

// TestBarrierRouter_零目标 空 targets 产出非 nil 空切片。
func TestBarrierRouter_零目标(t *testing.T) {
	msgs, err := NewBarrierRouter().Dispatch(context.Background(), "a")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if msgs == nil {
		t.Fatalf("msgs = nil，期望非 nil 空切片")
	}
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d，期望 0", len(msgs))
	}
}

// TestConditionalRouter_单目标 selector 返回单目标 → 1 条 Trigger 消息。
// Python: router.py:44-46（isinstance(targets, str) 的归一化在 Go 侧上移到调用方，偏差 D3）
func TestConditionalRouter_单目标(t *testing.T) {
	r := NewConditionalRouter(SelectorFunc(func(_ context.Context, _ any) ([]string, error) {
		return []string{"D"}, nil
	}))
	msgs, err := r.Dispatch(context.Background(), "A")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d，期望 1", len(msgs))
	}
	if msgs[0].Sender != "A" || msgs[0].Target != "D" {
		t.Errorf("msg = %+v，期望 Sender=\"A\" Target=\"D\"", msgs[0])
	}
	if msgs[0].Kind() != KindTrigger {
		t.Errorf("Kind() = %q，期望 %q", msgs[0].Kind(), KindTrigger)
	}
}

// TestConditionalRouter_多目标 多目标保序。
func TestConditionalRouter_多目标(t *testing.T) {
	targets := []string{"E", "F", "G"}
	r := NewConditionalRouter(SelectorFunc(func(_ context.Context, _ any) ([]string, error) {
		return targets, nil
	}))
	msgs, err := r.Dispatch(context.Background(), "A")
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
	}
}

// TestConditionalRouter_零目标 空目标产出非 nil 空切片。
func TestConditionalRouter_零目标(t *testing.T) {
	r := NewConditionalRouter(SelectorFunc(func(_ context.Context, _ any) ([]string, error) {
		return []string{}, nil
	}))
	msgs, err := r.Dispatch(context.Background(), "A")
	if err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if msgs == nil {
		t.Fatalf("msgs = nil，期望非 nil 空切片")
	}
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d，期望 0", len(msgs))
	}
}

// TestConditionalRouter_selector返回nil报错 nil 目标视为构图错误（fail-closed）。
//
// 对齐 Python：`for to in None` 抛 TypeError；Go 若把 nil 当作 END，
// selector 漏 return 会静默让图提前结束。
func TestConditionalRouter_selector返回nil报错(t *testing.T) {
	r := NewConditionalRouter(SelectorFunc(func(_ context.Context, _ any) ([]string, error) {
		return nil, nil
	}))
	msgs, err := r.Dispatch(context.Background(), "A")
	if err == nil {
		t.Fatalf("err = nil，期望非 nil（nil 目标必须判为构图错误）")
	}
	if msgs != nil {
		t.Errorf("msgs = %v，期望 nil", msgs)
	}
}

// TestConditionalRouter_typedNil报错 typed-nil（SelectorFunc(nil)）不得绕过 nil 守卫。
func TestConditionalRouter_typedNil报错(t *testing.T) {
	r := NewConditionalRouter(SelectorFunc(nil))
	if r == nil {
		t.Fatal("NewConditionalRouter 返回 nil")
	}
	msgs, err := r.Dispatch(context.Background(), "A")
	if err == nil {
		t.Fatalf("err = nil，期望非 nil（typed-nil selector 不得 panic 或静默）")
	}
	if msgs != nil {
		t.Errorf("msgs = %v，期望 nil", msgs)
	}
}

// TestConditionalRouter_selector收到nilState 记录 Python 事实基线：
// state 恒为 None（router.py:37），「注入/不注入」两分支行为等价（偏差 D2）。
func TestConditionalRouter_selector收到nilState(t *testing.T) {
	var gotState any = "sentinel"
	r := NewConditionalRouter(SelectorFunc(func(_ context.Context, state any) ([]string, error) {
		gotState = state
		return []string{"D"}, nil
	}))
	if _, err := r.Dispatch(context.Background(), "A"); err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if gotState != nil {
		t.Errorf("state = %v，期望 nil", gotState)
	}
}

// TestConditionalRouter_ctx透传 ctx 原样传给 selector。
func TestConditionalRouter_ctx透传(t *testing.T) {
	type ctxKey string
	key := ctxKey("trace")
	ctx := context.WithValue(context.Background(), key, "abc")

	var got any
	r := NewConditionalRouter(SelectorFunc(func(gotCtx context.Context, _ any) ([]string, error) {
		got = gotCtx.Value(key)
		return []string{}, nil
	}))
	if _, err := r.Dispatch(ctx, "A"); err != nil {
		t.Fatalf("Dispatch 返回错误：%v", err)
	}
	if got != "abc" {
		t.Errorf("ctx 值 = %v，期望 \"abc\"", got)
	}
}

// TestConditionalRouter_selector返错原样上抛 错误不得包装（偏差 D6）：
// 8.3b/8.3c 需要靠 errors.As 识别 *pregel.GraphInterrupt。
func TestConditionalRouter_selector返错原样上抛(t *testing.T) {
	errSentinel := errors.New("selector 失败")
	r := NewConditionalRouter(SelectorFunc(func(_ context.Context, _ any) ([]string, error) {
		return nil, errSentinel
	}))
	msgs, err := r.Dispatch(context.Background(), "A")
	if err != errSentinel {
		t.Errorf("err = %v，期望原样返回哨兵错误（不得包装）", err)
	}
	if msgs != nil {
		t.Errorf("msgs = %v，期望 nil", msgs)
	}
}

// TestConditionalRouter_selector为nil 在 Dispatch 阶段报错（对齐 Python
// 到 dispatch 才 inspect.signature(None) 抛 TypeError 的时机）。
func TestConditionalRouter_selector为nil(t *testing.T) {
	msgs, err := NewConditionalRouter(nil).Dispatch(context.Background(), "A")
	if err == nil {
		t.Fatalf("err = nil，期望非 nil")
	}
	if msgs != nil {
		t.Errorf("msgs = %v，期望 nil", msgs)
	}
}
