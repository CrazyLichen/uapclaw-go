package utils

import (
	"fmt"
	"sync"
	"testing"
)

// ──────────────────────────── 测试辅助类型 ────────────────────────────

// testCleanableResource 实现 resettable 接口的测试类型。
// 用于验证 Singleton.Reset 时自动调用 Cleanup。
type testCleanableResource struct {
	cleaned bool
}

// Cleanup 实现 resettable 接口，标记已清理。
func (c *testCleanableResource) Cleanup() error {
	c.cleaned = true
	return nil
}

// testFailingResource Cleanup 返回错误的测试类型。
// 用于验证 Reset 在 Cleanup 失败时仍不阻断流程。
type testFailingResource struct {
	cleanupErr error
}

// Cleanup 实现 resettable 接口，返回预设错误。
func (f *testFailingResource) Cleanup() error {
	return f.cleanupErr
}

// ──────────────────────────── 导出函数（测试） ────────────────────────────

func TestSingleton_Get(t *testing.T) {
	var s Singleton[string]
	callCount := 0

	factory := func() *string {
		callCount++
		v := "hello"
		return &v
	}

	// 首次 Get 应调用 factory
	got := s.Get(factory)
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if *got != "hello" {
		t.Fatalf("Get() = %q, want %q", *got, "hello")
	}
	if callCount != 1 {
		t.Fatalf("factory called %d times, want 1", callCount)
	}

	// 再次 Get 不应调用 factory
	got2 := s.Get(factory)
	if got2 != got {
		t.Fatal("Get() returned different instance on second call")
	}
	if callCount != 1 {
		t.Fatalf("factory called %d times, want 1", callCount)
	}
}

func TestSingleton_Concurrent(t *testing.T) {
	var s Singleton[int]
	callCount := 0
	var mu sync.Mutex

	factory := func() *int {
		mu.Lock()
		callCount++
		mu.Unlock()
		v := 42
		return &v
	}

	const goroutines = 100
	results := make([]*int, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = s.Get(factory)
		}(i)
	}
	wg.Wait()

	// factory 只应被调用一次
	mu.Lock()
	if callCount != 1 {
		t.Fatalf("factory called %d times, want 1", callCount)
	}
	mu.Unlock()

	// 所有 goroutine 应获得相同实例
	for i, r := range results {
		if r != results[0] {
			t.Fatalf("goroutine %d got different instance", i)
		}
		if *r != 42 {
			t.Fatalf("goroutine %d got %d, want 42", i, *r)
		}
	}
}

func TestSingleton_DifferentTypes(t *testing.T) {
	// 不同类型的 Singleton 互不影响
	var sInt Singleton[int]
	var sStr Singleton[string]

	v := sInt.Get(func() *int { p := 100; return &p })
	s := sStr.Get(func() *string { p := "world"; return &p })

	if *v != 100 {
		t.Fatalf("int singleton = %d, want 100", *v)
	}
	if *s != "world" {
		t.Fatalf("string singleton = %q, want %q", *s, "world")
	}
}

func TestSingleton_StructType(t *testing.T) {
	type config struct {
		Name string
		Port int
	}

	var s Singleton[config]

	cfg := s.Get(func() *config {
		return &config{Name: "test", Port: 8080}
	})

	if cfg.Name != "test" || cfg.Port != 8080 {
		t.Fatalf("Get() = %+v, want {Name:test Port:8080}", cfg)
	}

	// 再次获取应为同一实例
	cfg2 := s.Get(func() *config {
		return &config{Name: "other", Port: 9090}
	})
	if cfg2 != cfg {
		t.Fatal("Get() returned different instance")
	}
}

func TestSingleton_Reset(t *testing.T) {
	var s Singleton[string]
	callCount := 0

	factory := func() *string {
		callCount++
		v := "first"
		return &v
	}

	// 首次 Get
	got1 := s.Get(factory)
	if *got1 != "first" {
		t.Fatalf("Get() = %q, want %q", *got1, "first")
	}

	// Reset 后再 Get，factory 应再次被调用
	s.Reset()
	if callCount != 1 {
		t.Fatalf("factory called %d times before second Get, want 1", callCount)
	}

	factory2 := func() *string {
		callCount++
		v := "second"
		return &v
	}

	got2 := s.Get(factory2)
	if *got2 != "second" {
		t.Fatalf("Get() after Reset = %q, want %q", *got2, "second")
	}
	if callCount != 2 {
		t.Fatalf("factory called %d times total, want 2", callCount)
	}
}

func TestSingleton_ResetWithCleanup(t *testing.T) {
	// 使用实现了 resettable 接口的测试类型 testCleanableResource
	var s Singleton[testCleanableResource]

	// 创建实例
	inst := s.Get(func() *testCleanableResource {
		return &testCleanableResource{cleaned: false}
	})
	if inst == nil {
		t.Fatal("Get() returned nil")
	}
	if inst.cleaned {
		t.Fatal("实例不应已被清理")
	}

	// Reset 时应自动调用 Cleanup
	s.Reset()

	// 注意：Reset 后 inst 已被清理，验证 cleaned 标记
	if !inst.cleaned {
		t.Fatal("Reset 应通过 resettable 接口调用 Cleanup，cleaned 应为 true")
	}
}

func TestSingleton_ResetWithCleanupError(t *testing.T) {
	// 使用 Cleanup 返回错误的测试类型 testFailingResource
	// 验证 Reset 在 Cleanup 失败时仍不阻断流程
	var s Singleton[testFailingResource]

	s.Get(func() *testFailingResource {
		return &testFailingResource{cleanupErr: fmt.Errorf("cleanup failed")}
	})

	// Cleanup 返回错误，但 Reset 不应 panic，仅记日志
	s.Reset()
}

func TestSingleton_ResetWithNonResettable(t *testing.T) {
	// 未实现 resettable 接口的类型，Reset 不应 panic
	type simpleStruct struct {
		value int
	}

	var s Singleton[simpleStruct]

	s.Get(func() *simpleStruct {
		return &simpleStruct{value: 42}
	})

	// simpleStruct 未实现 resettable，Reset 不应 panic
	s.Reset()

	// Reset 后再 Get 应创建新实例
	newInst := s.Get(func() *simpleStruct {
		return &simpleStruct{value: 99}
	})
	if newInst.value != 99 {
		t.Fatalf("Get() after Reset = %d, want 99", newInst.value)
	}
}

func TestSingleton_ResetNil(t *testing.T) {
	// 对未初始化的 Singleton 调用 Reset 不应 panic
	var s Singleton[string]
	s.Reset()

	// Reset 后 Get 应正常工作
	got := s.Get(func() *string {
		v := "after-nil-reset"
		return &v
	})
	if *got != "after-nil-reset" {
		t.Fatalf("Get() = %q, want %q", *got, "after-nil-reset")
	}
}
