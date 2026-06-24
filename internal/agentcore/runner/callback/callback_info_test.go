package callback

import (
	"context"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSortCallbacks_优先级降序 测试按 Priority 降序排列。
func TestSortCallbacks_优先级降序(t *testing.T) {
	callbacks := []*CallbackInfo[LLMCallbackFunc]{
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 1 }, Priority: 10, CreatedAt: 1},
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 2 }, Priority: 30, CreatedAt: 2},
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 3 }, Priority: 20, CreatedAt: 3},
	}
	sortCallbacks(callbacks)
	if callbacks[0].Priority != 30 || callbacks[1].Priority != 20 || callbacks[2].Priority != 10 {
		t.Errorf("期望优先级降序 [30,20,10]，实际 [%d,%d,%d]",
			callbacks[0].Priority, callbacks[1].Priority, callbacks[2].Priority)
	}
}

// TestSortCallbacks_相同优先级按创建时间升序 测试相同 Priority 按 CreatedAt 升序。
func TestSortCallbacks_相同优先级按创建时间升序(t *testing.T) {
	callbacks := []*CallbackInfo[LLMCallbackFunc]{
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 1 }, Priority: 10, CreatedAt: 3.0},
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 2 }, Priority: 10, CreatedAt: 1.0},
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 3 }, Priority: 10, CreatedAt: 2.0},
	}
	sortCallbacks(callbacks)
	if callbacks[0].CreatedAt != 1.0 || callbacks[1].CreatedAt != 2.0 || callbacks[2].CreatedAt != 3.0 {
		t.Errorf("期望 CreatedAt 升序 [1,2,3]，实际 [%v,%v,%v]",
			callbacks[0].CreatedAt, callbacks[1].CreatedAt, callbacks[2].CreatedAt)
	}
}

// TestSortCallbacks_空列表 测试空列表排序不 panic。
func TestSortCallbacks_空列表(t *testing.T) {
	callbacks := []*CallbackInfo[LLMCallbackFunc]{}
	sortCallbacks(callbacks) // 不应 panic
}

// TestSortCallbacks_单元素 测试单元素排序。
func TestSortCallbacks_单元素(t *testing.T) {
	callbacks := []*CallbackInfo[LLMCallbackFunc]{
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 1 }, Priority: 10, CreatedAt: 1.0},
	}
	sortCallbacks(callbacks)
	if callbacks[0].Priority != 10 {
		t.Errorf("期望 Priority=10，实际 %d", callbacks[0].Priority)
	}
}

// TestCallbackInfo_字段默认值 测试 CallbackInfo 各字段。
func TestCallbackInfo_字段默认值(t *testing.T) {
	fn := func(_ context.Context, _ *LLMCallEventData) any { return nil }
	info := CallbackInfo[LLMCallbackFunc]{
		Callback: fn,
		Priority: 0,
		Enabled:  true,
	}
	if info.Callback == nil {
		t.Error("Callback 不应为 nil")
	}
	if !info.Enabled {
		t.Error("Enabled 应为 true")
	}
	if info.Once {
		t.Error("Once 默认应为 false")
	}
	if info.MaxRetries != 0 {
		t.Errorf("MaxRetries 默认应为 0，实际 %d", info.MaxRetries)
	}
	if info.Timeout != 0 {
		t.Errorf("Timeout 默认应为 0，实际 %f", info.Timeout)
	}
	if info.RetryDelay != 0 {
		t.Errorf("RetryDelay 默认应为 0，实际 %f", info.RetryDelay)
	}
}
