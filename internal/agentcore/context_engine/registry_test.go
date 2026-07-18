package context_engine

import (
	"context"
	"fmt"
	"sync"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// mockProcessorConfig 测试用 ProcessorConfig 实现
type mockProcessorConfig struct{}

func (m *mockProcessorConfig) Validate() error { return nil }

func (m *mockProcessorConfig) SetModelDefaults(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) {
}

func (m *mockProcessorConfig) GetModel() *llm_schema.ModelRequestConfig { return nil }

// mockProcessor 测试用 ContextProcessor 实现
type mockProcessor struct{}

func (m *mockProcessor) OnAddMessages(_ context.Context, _ iface.ModelContext, messages []llm_schema.BaseMessage, _ ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	return nil, messages, nil
}
func (m *mockProcessor) OnGetContextWindow(_ context.Context, _ iface.ModelContext, cw iface.ContextWindow, _ ...iface.Option) (*iface.ContextEvent, iface.ContextWindow, error) {
	return nil, cw, nil
}
func (m *mockProcessor) TriggerAddMessages(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	return false, nil
}
func (m *mockProcessor) TriggerGetContextWindow(_ context.Context, _ iface.ModelContext, _ iface.ContextWindow, _ ...iface.Option) (bool, error) {
	return false, nil
}
func (m *mockProcessor) SaveState() map[string]any  { return nil }
func (m *mockProcessor) LoadState(_ map[string]any) {}
func (m *mockProcessor) ProcessorType() string      { return "MockProcessor" }

// TestRegisterProcessorFactory 测试注册工厂函数
func TestRegisterProcessorFactory(t *testing.T) {
	// 清理注册表
	processorFactoriesMu.Lock()
	processorFactories = make(map[string]iface.ProcessorFactory)
	processorFactoriesMu.Unlock()

	factory := iface.ProcessorFactory(func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
		return &mockProcessor{}, nil
	})

	RegisterProcessorFactory("MockProcessor", factory)

	got, ok := GetProcessorFactory("MockProcessor")
	if !ok {
		t.Fatal("期望找到已注册的工厂，但未找到")
	}
	if got == nil {
		t.Fatal("期望工厂非 nil")
	}

	// 验证工厂能创建实例
	result, err := got(&mockProcessorConfig{})
	if err != nil {
		t.Fatalf("工厂调用失败: %v", err)
	}
	if result == nil {
		t.Fatal("期望工厂返回非 nil 处理器")
	}
	if result.ProcessorType() != "MockProcessor" {
		t.Fatalf("期望 ProcessorType=MockProcessor，实际=%s", result.ProcessorType())
	}
}

// TestGetProcessorFactory_NotFound 测试获取未注册的工厂
func TestGetProcessorFactory_NotFound(t *testing.T) {
	processorFactoriesMu.Lock()
	processorFactories = make(map[string]iface.ProcessorFactory)
	processorFactoriesMu.Unlock()

	_, ok := GetProcessorFactory("NonExistent")
	if ok {
		t.Fatal("期望未注册的工厂返回 false")
	}
}

// TestListProcessorFactories 测试列出已注册的工厂
func TestListProcessorFactories(t *testing.T) {
	processorFactoriesMu.Lock()
	processorFactories = make(map[string]iface.ProcessorFactory)
	processorFactoriesMu.Unlock()

	factory := iface.ProcessorFactory(func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
		return nil, fmt.Errorf("not implemented")
	})

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
	processorFactories = make(map[string]iface.ProcessorFactory)
	processorFactoriesMu.Unlock()

	factory := iface.ProcessorFactory(func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
		return nil, fmt.Errorf("not implemented")
	})

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
