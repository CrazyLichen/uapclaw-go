package callback

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCallbackChain_顺序执行 测试三个回调按顺序执行
func TestCallbackChain_顺序执行(t *testing.T) {
	chain := NewCallbackChain("test-chain")

	var order []int
	var mu sync.Mutex

	recordOrder := func(idx int) ChainCallbackFunc {
		return func(_ context.Context, _ *ChainContext) (any, error) {
			mu.Lock()
			order = append(order, idx)
			mu.Unlock()
			return idx, nil
		}
	}

	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: recordOrder(1),
		Priority: 10,
		Enabled:  true,
	}, nil, nil)
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: recordOrder(2),
		Priority: 10,
		Enabled:  true,
	}, nil, nil)
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: recordOrder(3),
		Priority: 10,
		Enabled:  true,
	}, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionContinue {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionContinue, result.Action)
	}
	if result.Error != nil {
		t.Errorf("期望 Error=nil, 实际=%v", result.Error)
	}
	if !cctx.IsCompleted {
		t.Error("期望 IsCompleted=true")
	}

	mu.Lock()
	expected := []int{1, 2, 3}
	if len(order) != len(expected) {
		t.Errorf("期望执行顺序长度 %d, 实际 %d", len(expected), len(order))
	} else {
		for i, v := range expected {
			if order[i] != v {
				t.Errorf("位置 %d: 期望 %d, 实际 %d", i, v, order[i])
			}
		}
	}
	mu.Unlock()
}

// TestCallbackChain_优先级排序 测试高优先级先执行
func TestCallbackChain_优先级排序(t *testing.T) {
	chain := NewCallbackChain("priority-chain")

	var order []string
	var mu sync.Mutex

	makeCallback := func(name string) ChainCallbackFunc {
		return func(_ context.Context, _ *ChainContext) (any, error) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return name, nil
		}
	}

	// 低优先级先注册
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: makeCallback("low"),
		Priority: 1,
		Enabled:  true,
	}, nil, nil)
	// 高优先级后注册
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: makeCallback("high"),
		Priority: 100,
		Enabled:  true,
	}, nil, nil)
	// 中优先级
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: makeCallback("mid"),
		Priority: 50,
		Enabled:  true,
	}, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionContinue {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionContinue, result.Action)
	}

	mu.Lock()
	expected := []string{"high", "mid", "low"}
	if len(order) != len(expected) {
		t.Errorf("期望执行顺序长度 %d, 实际 %d", len(expected), len(order))
	} else {
		for i, v := range expected {
			if order[i] != v {
				t.Errorf("位置 %d: 期望 %s, 实际 %s", i, v, order[i])
			}
		}
	}
	mu.Unlock()
}

// TestCallbackChain_Break动作 测试中断后续回调
func TestCallbackChain_Break动作(t *testing.T) {
	chain := NewCallbackChain("break-chain")

	var executed int

	cb1 := func(_ context.Context, _ *ChainContext) (any, error) {
		return &ChainResult{
			Action: ChainActionBreak,
			Result: "break-value",
		}, nil
	}
	cb2 := func(_ context.Context, _ *ChainContext) (any, error) {
		executed++
		return "should-not-execute", nil
	}

	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb1,
		Priority: 10,
		Enabled:  true,
	}, nil, nil)
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb2,
		Priority: 5,
		Enabled:  true,
	}, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionBreak {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionBreak, result.Action)
	}
	if result.Result != "break-value" {
		t.Errorf("期望 Result=break-value, 实际=%v", result.Result)
	}
	if executed != 0 {
		t.Errorf("期望后续回调不执行, 实际执行了 %d 次", executed)
	}
}

// TestCallbackChain_Retry动作 测试重试当前回调
func TestCallbackChain_Retry动作(t *testing.T) {
	chain := NewCallbackChain("retry-chain")

	var callCount int32

	cb := func(_ context.Context, _ *ChainContext) (any, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			// 前两次返回 Retry 动作
			return &ChainResult{
				Action: ChainActionRetry,
				Result: count,
			}, nil
		}
		return "success-after-retry", nil
	}

	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback:   cb,
		Priority:   10,
		Enabled:    true,
		MaxRetries: 5,
	}, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionContinue {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionContinue, result.Action)
	}

	finalCount := atomic.LoadInt32(&callCount)
	if finalCount != 3 {
		t.Errorf("期望回调被调用 3 次, 实际 %d 次", finalCount)
	}
}

// TestCallbackChain_Rollback动作 测试出错时逆序调用回滚处理器
func TestCallbackChain_Rollback动作(t *testing.T) {
	chain := NewCallbackChain("rollback-chain")

	var rollbackOrder []string
	var mu sync.Mutex

	cb1 := func(_ context.Context, _ *ChainContext) (any, error) {
		return "result1", nil
	}
	cb2 := func(_ context.Context, _ *ChainContext) (any, error) {
		return nil, errors.New("cb2 failed")
	}

	rollback1 := func(_ context.Context, _ *ChainContext) error {
		mu.Lock()
		rollbackOrder = append(rollbackOrder, "rollback1")
		mu.Unlock()
		return nil
	}
	rollback2 := func(_ context.Context, _ *ChainContext) error {
		mu.Lock()
		rollbackOrder = append(rollbackOrder, "rollback2")
		mu.Unlock()
		return nil
	}

	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb1,
		Priority: 10,
		Enabled:  true,
	}, rollback1, nil)
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb2,
		Priority: 5,
		Enabled:  true,
	}, rollback2, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionRollback {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionRollback, result.Action)
	}
	if !cctx.IsRolledBack {
		t.Error("期望 IsRolledBack=true")
	}

	mu.Lock()
	// cb1 先执行成功，cb2 失败触发回滚，应逆序调用 rollback1（只有 cb1 成功执行了）
	expected := []string{"rollback1"}
	if len(rollbackOrder) != len(expected) {
		t.Errorf("期望回滚顺序长度 %d, 实际 %d", len(expected), len(rollbackOrder))
	} else {
		for i, v := range expected {
			if rollbackOrder[i] != v {
				t.Errorf("位置 %d: 期望 %s, 实际 %s", i, v, rollbackOrder[i])
			}
		}
	}
	mu.Unlock()
}

// TestCallbackChain_超时控制 测试回调超时返回错误
func TestCallbackChain_超时控制(t *testing.T) {
	chain := NewCallbackChain("timeout-chain")

	var rollbackCalled bool
	var mu sync.Mutex

	// 先执行一个成功的回调，注册回滚处理器
	cb1 := func(_ context.Context, _ *ChainContext) (any, error) {
		return "result1", nil
	}
	// 超时的回调
	cb2 := func(ctx context.Context, _ *ChainContext) (any, error) {
		select {
		case <-time.After(5 * time.Second):
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	rollback1 := func(_ context.Context, _ *ChainContext) error {
		mu.Lock()
		rollbackCalled = true
		mu.Unlock()
		return nil
	}

	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb1,
		Priority: 10,
		Enabled:  true,
	}, rollback1, nil)
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback:   cb2,
		Priority:   5,
		Enabled:    true,
		Timeout:    0.1, // 100ms 超时
		MaxRetries: 0,
	}, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionRollback {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionRollback, result.Action)
	}

	mu.Lock()
	if !rollbackCalled {
		t.Error("期望回滚处理器被调用")
	}
	mu.Unlock()
}

// TestCallbackChain_错误处理器 测试 errorHandler 决定后续动作
func TestCallbackChain_错误处理器(t *testing.T) {
	chain := NewCallbackChain("error-handler-chain")

	var nextExecuted bool
	var mu sync.Mutex

	cb1 := func(_ context.Context, _ *ChainContext) (any, error) {
		return nil, errors.New("cb1 error")
	}
	cb2 := func(_ context.Context, _ *ChainContext) (any, error) {
		mu.Lock()
		nextExecuted = true
		mu.Unlock()
		return "cb2-result", nil
	}

	// 错误处理器决定继续执行
	continueHandler := func(_ context.Context, _ *ChainContext, _ error) (ChainAction, error) {
		return ChainActionContinue, nil
	}

	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb1,
		Priority: 10,
		Enabled:  true,
	}, nil, continueHandler)
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb2,
		Priority: 5,
		Enabled:  true,
	}, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionContinue {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionContinue, result.Action)
	}

	mu.Lock()
	if !nextExecuted {
		t.Error("期望后续回调被执行（errorHandler 返回 Continue）")
	}
	mu.Unlock()
}

// TestCallbackChain_错误处理器Break 测试 errorHandler 返回 Break 动作
func TestCallbackChain_错误处理器Break(t *testing.T) {
	chain := NewCallbackChain("error-handler-break")

	var nextExecuted bool
	var mu sync.Mutex

	cb1 := func(_ context.Context, _ *ChainContext) (any, error) {
		return nil, errors.New("cb1 error")
	}
	cb2 := func(_ context.Context, _ *ChainContext) (any, error) {
		mu.Lock()
		nextExecuted = true
		mu.Unlock()
		return "cb2-result", nil
	}

	breakHandler := func(_ context.Context, _ *ChainContext, _ error) (ChainAction, error) {
		return ChainActionBreak, nil
	}

	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb1,
		Priority: 10,
		Enabled:  true,
	}, nil, breakHandler)
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb2,
		Priority: 5,
		Enabled:  true,
	}, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionBreak {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionBreak, result.Action)
	}

	mu.Lock()
	if nextExecuted {
		t.Error("期望后续回调不执行（errorHandler 返回 Break）")
	}
	mu.Unlock()
}

// TestCallbackChain_Rollback动作回调触发 测试回调返回 Rollback 动作触发回滚
func TestCallbackChain_Rollback动作回调触发(t *testing.T) {
	chain := NewCallbackChain("rollback-callback-chain")

	var rollbackCalled bool
	var mu sync.Mutex

	cb1 := func(_ context.Context, _ *ChainContext) (any, error) {
		return "result1", nil
	}
	cb2 := func(_ context.Context, _ *ChainContext) (any, error) {
		return &ChainResult{
			Action: ChainActionRollback,
			Error:  errors.New("rollback triggered"),
		}, nil
	}

	rollback1 := func(_ context.Context, _ *ChainContext) error {
		mu.Lock()
		rollbackCalled = true
		mu.Unlock()
		return nil
	}

	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb1,
		Priority: 10,
		Enabled:  true,
	}, rollback1, nil)
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb2,
		Priority: 5,
		Enabled:  true,
	}, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionRollback {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionRollback, result.Action)
	}
	if !cctx.IsRolledBack {
		t.Error("期望 IsRolledBack=true")
	}

	mu.Lock()
	if !rollbackCalled {
		t.Error("期望回滚处理器被调用")
	}
	mu.Unlock()
}

// TestCallbackChain_错误重试 测试回调失败后重试成功
func TestCallbackChain_错误重试(t *testing.T) {
	chain := NewCallbackChain("error-retry-chain")

	var callCount int32

	cb := func(_ context.Context, _ *ChainContext) (any, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			return nil, errors.New("temporary error")
		}
		return "success", nil
	}

	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback:   cb,
		Priority:   10,
		Enabled:    true,
		MaxRetries: 3,
		RetryDelay: 0.01, // 10ms
	}, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionContinue {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionContinue, result.Action)
	}
	if result.Error != nil {
		t.Errorf("期望 Error=nil, 实际=%v", result.Error)
	}

	finalCount := atomic.LoadInt32(&callCount)
	if finalCount != 3 {
		t.Errorf("期望回调被调用 3 次, 实际 %d 次", finalCount)
	}
}

// TestCallbackChain_禁用回调 测试禁用的回调不执行
func TestCallbackChain_禁用回调(t *testing.T) {
	chain := NewCallbackChain("disabled-chain")

	var executed bool
	var mu sync.Mutex

	cb := func(_ context.Context, _ *ChainContext) (any, error) {
		mu.Lock()
		executed = true
		mu.Unlock()
		return "result", nil
	}

	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb,
		Priority: 10,
		Enabled:  false, // 禁用
	}, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionContinue {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionContinue, result.Action)
	}

	mu.Lock()
	if executed {
		t.Error("期望禁用的回调不执行")
	}
	mu.Unlock()
}

// TestCallbackChain_Once回调 测试只执行一次的回调
func TestCallbackChain_Once回调(t *testing.T) {
	chain := NewCallbackChain("once-chain")

	info := &CallbackInfo[ChainCallbackFunc]{
		Callback: func(_ context.Context, _ *ChainContext) (any, error) {
			return "once-result", nil
		},
		Priority: 10,
		Enabled:  true,
		Once:     true,
	}

	chain.Add(info, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	chain.Execute(context.Background(), cctx)

	if info.Enabled {
		t.Error("期望 Once 回调执行后 Enabled=false")
	}
}

// TestCallbackChain_Remove 测试移除回调
func TestCallbackChain_Remove(t *testing.T) {
	chain := NewCallbackChain("remove-chain")

	var cb2Executed bool
	var mu sync.Mutex

	cb1 := func(_ context.Context, _ *ChainContext) (any, error) {
		return "result1", nil
	}
	cb2 := func(_ context.Context, _ *ChainContext) (any, error) {
		mu.Lock()
		cb2Executed = true
		mu.Unlock()
		return "result2", nil
	}

	info1 := &CallbackInfo[ChainCallbackFunc]{
		Callback: cb1,
		Priority: 10,
		Enabled:  true,
	}
	info2 := &CallbackInfo[ChainCallbackFunc]{
		Callback: cb2,
		Priority: 5,
		Enabled:  true,
	}

	chain.Add(info1, nil, nil)
	chain.Add(info2, nil, nil)

	chain.Remove(info1)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionContinue {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionContinue, result.Action)
	}

	mu.Lock()
	if !cb2Executed {
		t.Error("期望 cb2 被执行")
	}
	mu.Unlock()

	if len(chain.callbacks) != 1 {
		t.Errorf("期望剩余 1 个回调, 实际 %d 个", len(chain.callbacks))
	}
}

// TestCallbackChain_结果传递 测试上一个回调结果传递给下一个
func TestCallbackChain_结果传递(t *testing.T) {
	chain := NewCallbackChain("result-chain")

	cb1 := func(_ context.Context, _ *ChainContext) (any, error) {
		return 10, nil
	}
	cb2 := func(_ context.Context, cctx *ChainContext) (any, error) {
		lastResult := cctx.GetLastResult()
		if lastResult != 10 {
			t.Errorf("期望上一个结果 10, 实际 %v", lastResult)
		}
		return 20, nil
	}

	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb1,
		Priority: 10,
		Enabled:  true,
	}, nil, nil)
	chain.Add(&CallbackInfo[ChainCallbackFunc]{
		Callback: cb2,
		Priority: 5,
		Enabled:  true,
	}, nil, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}
	result := chain.Execute(context.Background(), cctx)

	if result.Action != ChainActionContinue {
		t.Errorf("期望 Action=%s, 实际=%s", ChainActionContinue, result.Action)
	}
	if result.Result != 20 {
		t.Errorf("期望最终结果 20, 实际 %v", result.Result)
	}
}

// TestCallbackChain_Rollback公开方法 测试 Rollback 公开方法
func TestCallbackChain_Rollback公开方法(t *testing.T) {
	chain := NewCallbackChain("rollback-public")

	var rollbackCalled bool
	var mu sync.Mutex

	cb1 := func(_ context.Context, _ *ChainContext) (any, error) {
		return "result1", nil
	}

	rollback1 := func(_ context.Context, _ *ChainContext) error {
		mu.Lock()
		rollbackCalled = true
		mu.Unlock()
		return nil
	}

	info1 := &CallbackInfo[ChainCallbackFunc]{
		Callback: cb1,
		Priority: 10,
		Enabled:  true,
	}
	chain.Add(info1, rollback1, nil)

	cctx := &ChainContext{
		Event:     "test",
		StartTime: time.Now(),
	}

	// 直接调用公开 Rollback 方法
	chain.Rollback(context.Background(), cctx, []*CallbackInfo[ChainCallbackFunc]{info1})

	mu.Lock()
	if !rollbackCalled {
		t.Error("期望回滚处理器被调用")
	}
	mu.Unlock()

	if !cctx.IsRolledBack {
		t.Error("期望 IsRolledBack=true")
	}
}
