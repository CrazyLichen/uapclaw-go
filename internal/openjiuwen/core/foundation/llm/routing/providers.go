package routing

import (
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// routerProviderByCoreProvider core provider → intelli_router provider
// 归一映射（Python compiler.py:52-62 _ROUTER_PROVIDER_BY_CORE_PROVIDER）。
// 键为 ProviderType 枚举 value 的小写形式。
var routerProviderByCoreProvider = map[string]string{
	"openai":             "openai",
	"openai-account":     "openai",
	"openrouter":         "openai",
	"anthropic":          "anthropic",
	"siliconflow":        "siliconflow",
	"dashscope":          "dashscope",
	"deepseek":           "deepseek",
	"zhipu":              "zhipu",
	"inference-affinity": "inference-affinity",
}

// routerProviderByEndpointProfile endpoint_profile → intelli_router provider
// 归一映射（Python compiler.py:63-74 _ROUTER_PROVIDER_BY_ENDPOINT_PROFILE）。
// bedrock/gemini 键值全量保留（偏差 D-2 只限制 provider 适配器实现，
// 不裁剪映射表）。
var routerProviderByEndpointProfile = map[string]string{
	"anthropic":          "anthropic",
	"aws-bedrock":        "aws-bedrock",
	"bedrock":            "aws-bedrock",
	"dashscope":          "dashscope",
	"deepseek":           "deepseek",
	"google-gemini":      "google-gemini",
	"gemini":             "google-gemini",
	"inference-affinity": "inference-affinity",
	"siliconflow":        "siliconflow",
	"zhipu":              "zhipu",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RouterProvider 归一 deployment 的 provider 取值。
// Python: compiler.py:469-488 _router_provider（模块级函数，Go 导出供
// 客户端/测试复用；逻辑顺序逐行对齐）。
func RouterProvider(model *ResolvedModel) string {
	return routerProvider(model)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// routerProvider（compiler.py:469-488 _router_provider）。
func routerProvider(model *ResolvedModel) string {
	options := model.ClientOptions
	// Python: compiler.py:471-473 显式 intelli_router_provider 优先
	if explicit, exists := options["intelli_router_provider"]; exists {
		if s, ok := explicit.(string); ok && s != "" {
			return normalizeProvider(s)
		}
	}

	provider := normalizeProvider(model.Provider)
	apiMode := normalizeProvider(optionsValue(options, "api_mode"))
	// Python: compiler.py:477-481 api_mode anthropic-messages 或 core 映射 anthropic
	if apiMode == "anthropic-messages" || routerProviderByCoreProvider[provider] == "anthropic" {
		return "anthropic"
	}

	profile := normalizeProvider(pointerOr(optionsValue(options, "endpoint_profile"), model.EndpointProfile))
	if mapped, exists := routerProviderByEndpointProfile[profile]; exists {
		return mapped
	}

	if mapped, exists := routerProviderByCoreProvider[provider]; exists {
		return mapped
	}
	return provider
}

// normalizeProvider 小写 + 下划线转横线（compiler.py:490-491 _normalize_provider）。
func normalizeProvider(value any) string {
	s, _ := value.(string)
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "_", "-")
}

// optionsValue 读 client_options 键。
func optionsValue(options map[string]any, key string) any {
	if options == nil {
		return nil
	}
	return options[key]
}

// pointerOr options 值为空时回退到模型字段指针。
func pointerOr(v any, fallback *string) any {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	if fallback != nil {
		return *fallback
	}
	return v
}
