package context_engine

import (
	"sync"
	"testing"
)

// TestRegisterProcessorFactory 测试注册工厂函数
func TestRegisterProcessorFactory(t *testing.T) {
	// 清理注册表
	processorFactoriesMu.Lock()
	processorFactories = make(map[string]ProcessorFactory)
	processorFactoriesMu.Unlock()

	factory := func(config any) any {
		return "mock-processor-instance"
	}

	RegisterProcessorFactory("MockProcessor", factory)

	got, ok := GetProcessorFactory("MockProcessor")
	if !ok {
		t.Fatal("期望找到已注册的工厂，但未找到")
	}
	if got == nil {
		t.Fatal("期望工厂非 nil")
	}

	// 验证工厂能创建实例
	result := got("mock-config")
	if result != "mock-processor-instance" {
		t.Fatalf("期望工厂返回 mock-processor-instance，实际=%v", result)
	}
}

// TestGetProcessorFactory_NotFound 测试获取未注册的工厂
func TestGetProcessorFactory_NotFound(t *testing.T) {
	processorFactoriesMu.Lock()
	processorFactories = make(map[string]ProcessorFactory)
	processorFactoriesMu.Unlock()

	_, ok := GetProcessorFactory("NonExistent")
	if ok {
		t.Fatal("期望未注册的工厂返回 false")
	}
}

// TestListProcessorFactories 测试列出已注册的工厂
func TestListProcessorFactories(t *testing.T) {
	processorFactoriesMu.Lock()
	processorFactories = make(map[string]ProcessorFactory)
	processorFactoriesMu.Unlock()

	factory := func(config any) any { return nil }

	RegisterProcessorFactory("ProcessorA", factory)
	RegisterProcessorFactory("ProcessorB", factory)

	types := ListProcessorFactories()
	if len(types) != 2 {
		t.Fatalf("期望 2 个已注册类型，实际 %d", len(types))
	}

	typeSet := make(map[string]bool)
	for _, tp := range types {
		typeSet[tp] = true
	}
	if !typeSet["ProcessorA"] || !typeSet["ProcessorB"] {
		t.Fatal("期望包含 ProcessorA 和 ProcessorB")
	}
}

// TestRegisterProcessorFactory_Concurrent 测试并发注册安全性
func TestRegisterProcessorFactory_Concurrent(t *testing.T) {
	processorFactoriesMu.Lock()
	processorFactories = make(map[string]ProcessorFactory)
	processorFactoriesMu.Unlock()

	factory := func(config any) any { return nil }

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			RegisterProcessorFactory("ConcurrentProcessor", factory)
		}(i)
	}
	wg.Wait()

	_, ok := GetProcessorFactory("ConcurrentProcessor")
	if !ok {
		t.Fatal("并发注册后期望能找到工厂")
	}
}
