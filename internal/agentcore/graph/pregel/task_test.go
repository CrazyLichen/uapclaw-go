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

// ──────────────────────────── TaskExecutorPool ────────────────────────────

func TestPool_Submit与Clear(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{Ns: "top"})
	pool.Submit(context.Background(), NewPregelNode("a", NoopNodeFunc), 1)
	if len(pool.runningTasks) != 1 {
		t.Fatalf("runningTasks 应有 1 项")
	}
	// 清空前必须先收割结果，否则 goroutine 永久阻塞在 done 发送
	if err := pool.WaitAll(); err != nil {
		t.Fatalf("WaitAll 报错: %v", err)
	}
	if len(pool.succeedMessages) != 0 {
		t.Fatalf("无路由应零消息，得 %d", len(pool.succeedMessages))
	}
	pool.Clear()
	if len(pool.runningTasks) != 0 || len(pool.failed) != 0 || len(pool.succeedMessages) != 0 {
		t.Fatalf("Clear 后应全空")
	}
}

func TestPool_commitFailure首记不覆盖(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	node := NewPregelNode("a", NoopNodeFunc)
	pool.commitFailure(node, errors.New("first"))
	pool.commitFailure(node, errors.New("second"))
	pn := pool.failed["a"]
	if len(pn.Exception) != 1 || pn.Exception[0].Error() != "first" {
		t.Fatalf("应首记不覆盖: %+v", pn)
	}
	if pn.Status != TaskStatusError {
		t.Fatalf("状态应为 __error__: %q", pn.Status)
	}
}

func TestPool_commitFailure中断状态(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	node := NewPregelNode("a", NoopNodeFunc)
	pool.commitFailure(node, &GraphInterrupt{Interrupts: []Interrupt{{Value: "v"}}})
	if pool.failed["a"].Status != TaskStatusInterrupt {
		t.Fatalf("状态应为 __interrupt__: %q", pool.failed["a"].Status)
	}
}

func TestPool_空池WaitAll安全(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	if err := pool.WaitAll(); err != nil {
		t.Fatalf("空池应直接返回 nil: %v", err)
	}
}

// ──────────────────────────── WaitAll ────────────────────────────

func TestWaitAll_单节点异常上抛(t *testing.T) {
	want := errors.New("boom")
	pool := NewTaskExecutorPool(&PregelConfig{})
	pool.Submit(context.Background(), NewPregelNode("a",
		func(context.Context, NodeInput) error { return want }), 1)
	err := pool.WaitAll()
	if !errors.Is(err, want) {
		t.Fatalf("应上抛 boom，得 %v", err)
	}
	if pool.failed["a"].Status != TaskStatusError {
		t.Fatalf("失败记账缺失")
	}
}

func TestWaitAll_中断上抛(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	pool.Submit(context.Background(), NewPregelNode("a",
		func(context.Context, NodeInput) error { PanicGraphInterrupt(Interrupt{Value: "v"}); return nil }), 1)
	err := pool.WaitAll()
	var gi *GraphInterrupt
	if !errors.As(err, &gi) {
		t.Fatalf("应上抛中断，得 %v", err)
	}
	if pool.failed["a"].Status != TaskStatusInterrupt {
		t.Fatalf("中断记账缺失")
	}
}

func TestWaitAll_兄弟并发异常取消成功任务(t *testing.T) {
	// b 先异常 → a 被取消（fn 响应 ctx 取消）→ a 记 context.Canceled
	pool := NewTaskExecutorPool(&PregelConfig{})
	slowDone := make(chan struct{})
	pool.Submit(context.Background(), NewPregelNode("a",
		func(ctx context.Context, _ NodeInput) error {
			<-ctx.Done() // 协作取消：对齐 asyncio 在 await 点注入 CancelledError
			close(slowDone)
			return ctx.Err()
		}), 1)
	pool.Submit(context.Background(), NewPregelNode("b",
		func(context.Context, NodeInput) error { return errors.New("boom") }), 1)
	err := pool.WaitAll()
	if err == nil || err.Error() != "boom" {
		t.Fatalf("应上抛 boom，得 %v", err)
	}
	<-slowDone
	pn := pool.failed["a"]
	if pn.Status != TaskStatusError || pn.Exception[0] != context.Canceled {
		t.Fatalf("被取消任务应记 context.Canceled: %+v", pn)
	}
}

func TestWaitAll_同轮异常优先于中断(t *testing.T) {
	// 白盒确定性构造：两个任务先同步跑完、结果已入 done，再由 WaitAll 统一收割
	// ——复刻 Python asyncio.wait 唤醒时双任务均已 done 的场景（task.py:86-90）。
	// 若中断先醒、错误任务仍在途，Python 同样上抛中断并取消在途（见兄弟并发用例）。
	pool := NewTaskExecutorPool(&PregelConfig{})
	ta := newNodeTask(NewPregelNode("a", func(context.Context, NodeInput) error {
		PanicGraphInterrupt(Interrupt{Value: "i"})
		return nil
	}), &InnerPregelConfig{}, 1, context.Background())
	tb := newNodeTask(NewPregelNode("b", func(context.Context, NodeInput) error {
		return errors.New("boom")
	}), &InnerPregelConfig{}, 1, context.Background())
	ta.run()
	tb.run()
	pool.mu.Lock()
	pool.runningTasks[ta] = ta.node
	pool.runningTasks[tb] = tb.node
	pool.mu.Unlock()
	err := pool.WaitAll()
	if err == nil || err.Error() != "boom" {
		t.Fatalf("普通异常应优先于中断，得 %v", err)
	}
}

func TestWaitAll_成功消息收集(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	pool.Submit(context.Background(), NewPregelNode("a", NoopNodeFunc, NewStaticRouter("b", "c")), 1)
	if err := pool.WaitAll(); err != nil {
		t.Fatalf("WaitAll 报错: %v", err)
	}
	if len(pool.succeedMessages) != 2 {
		t.Fatalf("成功消息应 2 条，得 %d", len(pool.succeedMessages))
	}
}

func TestWaitAll_无异常收满全部才返回(t *testing.T) {
	// FIRST_EXCEPTION 的另一半语义：无异常时等价全部完成
	pool := NewTaskExecutorPool(&PregelConfig{})
	for _, n := range []string{"a", "b", "c"} {
		pool.Submit(context.Background(), NewPregelNode(n, NoopNodeFunc), 1)
	}
	if err := pool.WaitAll(); err != nil {
		t.Fatalf("全成功应 nil: %v", err)
	}
	if len(pool.failed) != 0 {
		t.Fatalf("不应有失败记账: %+v", pool.failed)
	}
}

// ──────────────────────────── CancelAll ────────────────────────────

func TestCancelAll_取消未完成并记账(t *testing.T) {
	fin := make(chan struct{})
	fastDone := make(chan struct{})
	pool := NewTaskExecutorPool(&PregelConfig{})
	pool.Submit(context.Background(), NewPregelNode("slow",
		func(ctx context.Context, _ NodeInput) error {
			<-ctx.Done()
			close(fin)
			return ctx.Err()
		}), 1)
	pool.Submit(context.Background(), NewPregelNode("fast",
		func(context.Context, NodeInput) error {
			close(fastDone)
			return nil
		}), 1)
	<-fastDone // 确定 fast 已完成（Python cancel_all 跳过 done 任务）
	pool.CancelAll()
	<-fin
	if len(pool.runningTasks) != 0 {
		t.Fatalf("runningTasks 应清空")
	}
	pn := pool.failed["slow"]
	if pn.Status != TaskStatusError || pn.Exception[0] != context.Canceled {
		t.Fatalf("被取消任务应记 context.Canceled: %+v", pn)
	}
	if _, ok := pool.failed["fast"]; ok {
		t.Fatalf("已完成的 fast 不应记账（Python cancel_all 只处理 to_cancel）")
	}
}

func TestCancelAll_空池安全(t *testing.T) {
	pool := NewTaskExecutorPool(&PregelConfig{})
	pool.CancelAll() // 不 panic 即通过
}
