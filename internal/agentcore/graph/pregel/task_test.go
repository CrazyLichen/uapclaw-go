package pregel

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── nodeTask.execute ────────────────────────────

func TestNodeTask_成功返回消息(t *testing.T) {
	node := NewPregelNode("a", NoopNodeFunc, NewStaticRouter("b", "c"))
	task := newNodeTask(node, &InnerPregelConfig{PregelConfig: PregelConfig{Ns: "top"}}, 1, context.Background())
	msgs, err := task.execute()
	if err != nil {
		t.Fatalf("execute 报错: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Sender != "a" || msgs[0].Target != "b" || msgs[1].Target != "c" {
		t.Fatalf("消息不符: %+v", msgs)
	}
	if msgs[0].Kind() != KindTrigger {
		t.Fatalf("消息种类不符: %v", msgs[0].Kind())
	}
}

func TestNodeTask_异常上抛(t *testing.T) {
	want := errors.New("boom")
	node := NewPregelNode("a", func(context.Context, NodeInput) error { return want })
	task := newNodeTask(node, &InnerPregelConfig{}, 1, context.Background())
	_, err := task.execute()
	if !errors.Is(err, want) {
		t.Fatalf("期望 boom，得 %v", err)
	}
}

func TestNodeTask_中断经recover降级为返回值(t *testing.T) {
	node := NewPregelNode("a", func(context.Context, NodeInput) error {
		PanicGraphInterrupt(Interrupt{Value: "v"})
		return nil
	})
	task := newNodeTask(node, &InnerPregelConfig{}, 1, context.Background())
	_, err := task.execute()
	var gi *GraphInterrupt
	if !errors.As(err, &gi) {
		t.Fatalf("期望 *GraphInterrupt，得 %v", err)
	}
	if len(gi.Interrupts) != 1 || gi.Interrupts[0].Value != "v" {
		t.Fatalf("中断值失真: %+v", gi.Interrupts)
	}
}

func TestNodeTask_ns拼接双写(t *testing.T) {
	var got NodeInput
	node := NewPregelNode("s1", func(_ context.Context, in NodeInput) error { got = in; return nil })
	cfg := &InnerPregelConfig{PregelConfig: PregelConfig{Ns: "main"}}
	cfg.ParentNs = "main:Sub:3"
	task := newNodeTask(node, cfg, 2, context.Background())
	if _, err := task.execute(); err != nil {
		t.Fatalf("execute 报错: %v", err)
	}
	want := "main:Sub:3:s1:2"
	if got.Config.Ns != want || got.Config.ParentNs != want {
		t.Fatalf("ns 双写不符: ns=%q parent=%q", got.Config.Ns, got.Config.ParentNs)
	}
	if got.Config.RecursionLimit != MaxRecursiveLimit {
		t.Fatalf("递归上限未补默认: %d", got.Config.RecursionLimit)
	}
	if got.State != nil {
		t.Fatalf("State 恒 nil，得 %v", got.State)
	}
}

func TestNodeTask_顶层无ParentNs不拼(t *testing.T) {
	var got NodeInput
	node := NewPregelNode("s1", func(_ context.Context, in NodeInput) error { got = in; return nil })
	task := newNodeTask(node, &InnerPregelConfig{PregelConfig: PregelConfig{Ns: "main"}}, 1, context.Background())
	if _, err := task.execute(); err != nil {
		t.Fatalf("execute 报错: %v", err)
	}
	if got.Config.Ns != "main" || got.Config.ParentNs != "" {
		t.Fatalf("顶层不应改写 ns: %+v", got.Config)
	}
}

func TestNodeTask_EdgeTraversed回调字段(t *testing.T) {
	calls := []map[string]any{}
	handler := func(ctx context.Context, data map[string]any) any {
		calls = append(calls, data)
		return nil
	}
	fw := callback.GetCallbackFramework()
	fw.OnCustom(string(callback.EdgeTraversed), handler)
	defer fw.OffCustom(string(callback.EdgeTraversed), handler)

	node := NewPregelNode("a", NoopNodeFunc, NewStaticRouter("b"))
	task := newNodeTask(node, &InnerPregelConfig{PregelConfig: PregelConfig{Ns: "top"}}, 1, context.Background())
	if _, err := task.execute(); err != nil {
		t.Fatalf("execute 报错: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("回调次数 %d != 1", len(calls))
	}
	if calls[0]["source_node"] != "a" || calls[0]["target_node"] != "b" || calls[0]["graph_id"] != "top" {
		t.Fatalf("回调字段不符: %+v", calls[0])
	}
}

func TestNodeTask_dispatch错误上抛(t *testing.T) {
	want := errors.New("dispatch fail")
	node := NewPregelNode("a", NoopNodeFunc, errRouter{want})
	task := newNodeTask(node, &InnerPregelConfig{}, 1, context.Background())
	_, err := task.execute()
	if !errors.Is(err, want) {
		t.Fatalf("dispatch 错误应上抛: %v", err)
	}
}

func TestNodeTask_非中断panic原样重抛(t *testing.T) {
	node := NewPregelNode("a", func(context.Context, NodeInput) error { panic("plain") })
	task := newNodeTask(node, &InnerPregelConfig{}, 1, context.Background())
	defer func() {
		if r := recover(); r != "plain" {
			t.Fatalf("非中断 panic 应原样重抛，得 %v", r)
		}
	}()
	_, _ = task.execute()
}

// errRouter 测试桩：Dispatch 恒返回错误
type errRouter struct{ err error }

func (r errRouter) Dispatch(context.Context, string) ([]Message, error) { return nil, r.err }
