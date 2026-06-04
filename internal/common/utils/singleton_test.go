package utils

import (
	"sync"
	"testing"
)

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
