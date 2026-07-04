package tools

import "sync"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// registry 工具名称到提供者的映射
	registry sync.Map
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterToolProvider 注册工具元数据提供者
func RegisterToolProvider(provider ToolMetadataProvider) {
	registry.Store(provider.GetName(), provider)
}

// GetToolProvider 按名称查找工具元数据提供者
func GetToolProvider(name string) (ToolMetadataProvider, bool) {
	v, ok := registry.Load(name)
	if !ok {
		return nil, false
	}
	return v.(ToolMetadataProvider), true
}

// AllProviders 返回所有已注册的工具元数据提供者
func AllProviders() []ToolMetadataProvider {
	var result []ToolMetadataProvider
	registry.Range(func(_, v any) bool {
		result = append(result, v.(ToolMetadataProvider))
		return true
	})
	return result
}
