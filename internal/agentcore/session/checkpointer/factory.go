package checkpointer

import (
	"context"
	"fmt"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CheckpointerConfigStruct 检查点器配置结构体。
// 对应 Python: openjiuwen/core/session/checkpointer/checkpointer.py (CheckpointerConfig)
type CheckpointerConfigStruct struct {
	// Type 检查点器类型（如 "in_memory"、"redis"）
	Type string
	// Conf 检查点器额外配置
	Conf map[string]any
}

// CheckpointerFactory 检查点器工厂，管理 Provider 注册和实例创建。
// 对应 Python: openjiuwen/core/session/checkpointer/checkpointer.py (CheckpointerFactory)
type CheckpointerFactory struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// registry 已注册的 Provider，key=类型名
	registry map[string]CheckpointerProvider
	// defaultCheckpointer 默认检查点器实例
	defaultCheckpointer Checkpointer
	// typeCheckpointers 按类型缓存的检查点器实例
	typeCheckpointers map[string]Checkpointer
}

// ──────────────────────────── 接口 ────────────────────────────

// CheckpointerProvider 检查点器提供者接口。
// 对应 Python: openjiuwen/core/session/checkpointer/checkpointer.py (CheckpointerProvider)
type CheckpointerProvider interface {
	// Create 创建检查点器实例
	Create(ctx context.Context, conf map[string]any) (Checkpointer, error)
}

// inMemoryProvider InMemory 检查点器提供者。
// 对应 Python: InMemoryCheckpointerProvider
type inMemoryProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// defaultFactory 全局默认工厂单例
var defaultFactory *CheckpointerFactory

// defaultInMemoryCheckpointer 全局默认 InMemory 检查点器实例
// 对应 Python: default_inmemory_checkpointer
var defaultInMemoryCheckpointer Checkpointer

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCheckpointerFactory 创建检查点器工厂，自动注册 in_memory Provider。
func NewCheckpointerFactory() *CheckpointerFactory {
	f := &CheckpointerFactory{
		registry:           make(map[string]CheckpointerProvider),
		typeCheckpointers: make(map[string]Checkpointer),
	}
	f.Register("in_memory", &inMemoryProvider{})
	return f
}

// Register 注册检查点器 Provider。
// 对应 Python: CheckpointerFactory.register(name)
func (f *CheckpointerFactory) Register(name string, provider CheckpointerProvider) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registry[name] = provider
}

// Create 根据配置创建检查点器实例。
// 对应 Python: CheckpointerFactory.create(checkpointer_conf)
func (f *CheckpointerFactory) Create(ctx context.Context, conf CheckpointerConfigStruct) (Checkpointer, error) {
	f.mu.RLock()
	provider, exists := f.registry[conf.Type]
	f.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("未知的检查点器类型: %s", conf.Type)
	}
	return provider.Create(ctx, conf.Conf)
}

// SetDefaultCheckpointer 设置默认检查点器实例。
// 对应 Python: CheckpointerFactory.set_default_checkpointer(checkpointer)
func (f *CheckpointerFactory) SetDefaultCheckpointer(cp Checkpointer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaultCheckpointer = cp
}

// SetCheckpointer 设置指定类型的检查点器实例。
// 对应 Python: CheckpointerFactory.set_checkpointer(store_type, checkpointer)
func (f *CheckpointerFactory) SetCheckpointer(storeType string, cp Checkpointer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.typeCheckpointers[storeType] = cp
}

// GetCheckpointer 获取检查点器实例。
// 对应 Python: CheckpointerFactory.get_checkpointer(store_type)
//
// 优先级：
// 1. 指定 storeType 时，先查 typeCheckpointers 缓存
// 2. storeType 为 "in_memory" 时，返回 defaultInMemoryCheckpointer
// 3. 返回 defaultCheckpointer
// 4. defaultCheckpointer 未设置时，返回 defaultInMemoryCheckpointer
func (f *CheckpointerFactory) GetCheckpointer(storeType ...string) Checkpointer {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if len(storeType) > 0 && storeType[0] != "" {
		st := storeType[0]
		// 1. 检查类型缓存
		if cp, ok := f.typeCheckpointers[st]; ok {
			return cp
		}
		// 2. in_memory 类型返回默认实例
		if st == "in_memory" {
			return defaultInMemoryCheckpointer
		}
	}

	// 3. 返回默认实例
	if f.defaultCheckpointer != nil {
		return f.defaultCheckpointer
	}
	return defaultInMemoryCheckpointer
}

// ──────────────────────────── inMemoryProvider 方法 ────────────────────────────

// Create 创建 InMemory 检查点器。
func (p *inMemoryProvider) Create(ctx context.Context, conf map[string]any) (Checkpointer, error) {
	return defaultInMemoryCheckpointer, nil
}

// ──────────────────────────── 包级便捷函数 ────────────────────────────

func init() {
	defaultInMemoryCheckpointer = NewInMemoryCheckpointer()
	defaultFactory = NewCheckpointerFactory()
}

// GetCheckpointer 从全局工厂获取检查点器实例。
func GetCheckpointer(storeType ...string) Checkpointer {
	return defaultFactory.GetCheckpointer(storeType...)
}

// SetDefaultCheckpointer 设置全局默认检查点器。
func SetDefaultCheckpointer(cp Checkpointer) {
	defaultFactory.SetDefaultCheckpointer(cp)
}

// SetCheckpointer 设置全局指定类型的检查点器。
func SetCheckpointer(storeType string, cp Checkpointer) {
	defaultFactory.SetCheckpointer(storeType, cp)
}

// CreateCheckpointer 从全局工厂创建检查点器。
func CreateCheckpointer(ctx context.Context, conf CheckpointerConfigStruct) (Checkpointer, error) {
	return defaultFactory.Create(ctx, conf)
}

// RegisterCheckpointer 向全局工厂注册 Provider。
func RegisterCheckpointer(name string, provider CheckpointerProvider) {
	defaultFactory.Register(name, provider)
}
