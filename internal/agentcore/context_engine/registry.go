package context_engine

import (
	"sort"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProcessorFactory 处理器工厂函数类型。
//
// 根据 ProcessorConfig 创建对应的 ContextProcessor 实例。
// 对应 Python: ContextEngine._PROCESSOR_MAP 中存储的 processor_class，
// 运行时通过 processor_class(config) 创建实例。
//
// 参数 config 必须实现 processor.ProcessorConfig 接口，
// 返回值必须实现 processor.ContextProcessor 接口。
// 使用 any 类型避免 context_engine → processor 循环依赖。
type ProcessorFactory func(config any) any

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// processorFactories 处理器工厂注册表
	processorFactories = make(map[string]ProcessorFactory)
	// processorFactoriesMu 注册表读写锁
	processorFactoriesMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterProcessorFactory 注册处理器工厂函数。
//
// 各处理器在 init() 函数中调用此函数将自己注册到全局注册表，
// 5.30 ContextEngine 门面实现时通过 GetProcessorFactory 获取工厂创建实例。
//
// 对应 Python: @ContextEngine.register_processor() 装饰器
func RegisterProcessorFactory(processorType string, factory ProcessorFactory) {
	processorFactoriesMu.Lock()
	defer processorFactoriesMu.Unlock()
	processorFactories[processorType] = factory
}

// GetProcessorFactory 获取处理器工厂函数。
//
// 返回工厂函数和是否找到的标志。5.30 ContextEngine._create_processor 对应使用。
//
// 对应 Python: ContextEngine._PROCESSOR_MAP.get(processor_type)
func GetProcessorFactory(processorType string) (ProcessorFactory, bool) {
	processorFactoriesMu.RLock()
	defer processorFactoriesMu.RUnlock()
	factory, ok := processorFactories[processorType]
	return factory, ok
}

// ListProcessorFactories 列出所有已注册的处理器类型名称。
//
// 返回排序后的类型名称列表，便于调试和诊断。
func ListProcessorFactories() []string {
	processorFactoriesMu.RLock()
	defer processorFactoriesMu.RUnlock()
	types := make([]string, 0, len(processorFactories))
	for k := range processorFactories {
		types = append(types, k)
	}
	sort.Strings(types)
	return types
}

// ──────────────────────────── 非导出函数 ────────────────────────────
