package provider

import (
	"sort"
	"sync"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// providerRegistry provider adapter 注册表。← provider/registry.py:9 _PROVIDER_REGISTRY
//
// Python 存 adapter 类并每次返回新实例（无状态）；Go adapter 均无状态结构体，
// 存单例实例，GetProviderAdapter 返回同一实例——语义等价（无状态可共享）。
var (
	providerRegistryMu sync.RWMutex
	providerRegistry   = map[string]BaseProviderAdapter{}
	registryOnce       sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterProvider 注册 provider adapter。← registry.py:13 register_provider
func RegisterProvider(name string, adapter BaseProviderAdapter) {
	providerRegistryMu.Lock()
	defer providerRegistryMu.Unlock()
	providerRegistry[name] = adapter
}

// GetProviderAdapter 获取 provider adapter（无状态）。← registry.py:16
// 未知 provider 报错，消息对齐 py:18-21 ValueError 文案。
func GetProviderAdapter(providerName string) (BaseProviderAdapter, error) {
	registerBuiltins()
	providerRegistryMu.RLock()
	defer providerRegistryMu.RUnlock()
	adapter, ok := providerRegistry[providerName]
	if !ok {
		names := make([]string, 0, len(providerRegistry))
		for name := range providerRegistry {
			names = append(names, name)
		}
		sort.Strings(names)
		return nil, UnknownProviderError(providerName, names)
	}
	return adapter, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerBuiltins 内置注册。← registry.py:28-44
//
// 偏差（D-2）：gemini/bedrock 独立 adapter 不移植（无 Go 客户端且不在
// 路由映射表）。dashscope/zhipu 在 Python 侧本就注册为 OpenAIProviderAdapter
// （py:43-44）；deepseek/siliconflow/inference-affinity 各自忠实移植。
func registerBuiltins() {
	registryOnce.Do(func() {
		RegisterProvider("openai", NewOpenAIAdapter())                        // py:36
		RegisterProvider("anthropic", NewAnthropicAdapter())                  // py:37
		RegisterProvider("dashscope", NewOpenAIAdapter())                     // py:43
		RegisterProvider("zhipu", NewOpenAIAdapter())                         // py:44
		RegisterProvider("deepseek", NewDeepSeekAdapter())                    // py:40
		RegisterProvider("siliconflow", NewSiliconFlowAdapter())              // py:41
		RegisterProvider("inference-affinity", NewInferenceAffinityAdapter()) // py:42
	})
}
