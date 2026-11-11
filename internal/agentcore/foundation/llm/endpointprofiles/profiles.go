package endpointprofiles

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EndpointProfile 一个 OpenAI 兼容端点的结构化规则。
// Python: endpoint_profiles.py:18-28 EndpointProfile
type EndpointProfile struct {
	// Name 档案名
	Name string
	// DefaultAPIBase 缺省 api_base（仅 ollama/lmstudio 有）
	DefaultAPIBase string
}

// providerAlias 一个 provider 的归一规则。
// Python: endpoint_profiles.py:104-142 LEGACY_PROVIDER_ALIASES 的值形状
type providerAlias struct {
	// ClientProvider 归一后的客户端实现名
	ClientProvider string
	// EndpointProfile 注入的端点档案（空表示不注入）
	EndpointProfile string
	// APIMode 注入的协议模式（空表示不注入）
	APIMode llmschema.APIMode
	// AuthMode 注入的鉴权模式（空表示不注入）
	AuthMode llmschema.AuthMode
	// KVCacheMode 注入的 KV cache 模式（空表示不注入）
	KVCacheMode string
}

// ──────────────────────────── 常量 ────────────────────────────

// legacyProviderExtraKey 归一前 provider 原名的存放键。
// Python: endpoint_profiles.py:213 data["legacy_client_provider"] = provider
const legacyProviderExtraKey = "legacy_client_provider"

// ──────────────────────────── 全局变量 ────────────────────────────

// endpointProfiles 端点档案表（17 个）。
// Python: endpoint_profiles.py:52-99 ENDPOINT_PROFILES
var endpointProfiles = map[string]EndpointProfile{
	"openai":            {Name: "openai"},
	"openai_compatible": {Name: "openai_compatible"},
	"deepseek":          {Name: "deepseek"},
	"openrouter":        {Name: "openrouter"},
	"siliconflow":       {Name: "siliconflow"},
	"dashscope":         {Name: "dashscope"},
	"moonshot":          {Name: "moonshot"},
	"minimax":           {Name: "minimax"},
	"modelarts":         {Name: "modelarts"},
	"qianfan":           {Name: "qianfan"},
	"volcengine":        {Name: "volcengine"},
	"zhipu":             {Name: "zhipu"},
	"mimo":              {Name: "mimo"},
	"vllm":              {Name: "vllm"},
	"ollama":            {Name: "ollama", DefaultAPIBase: "http://localhost:11434/v1"},
	"lmstudio":          {Name: "lmstudio", DefaultAPIBase: "http://localhost:1234/v1"},
	"sglang":            {Name: "sglang"},
}

// legacyProviderAliases provider 别名归一表（14 条）。
// Python: endpoint_profiles.py:104-155 LEGACY_PROVIDER_ALIASES
var legacyProviderAliases = map[string]providerAlias{
	"DeepSeek":          {ClientProvider: "OpenAI", EndpointProfile: "deepseek"},
	"OpenRouter":        {ClientProvider: "OpenAI", EndpointProfile: "openrouter"},
	"SiliconFlow":       {ClientProvider: "OpenAI", EndpointProfile: "siliconflow"},
	"DashScope":         {ClientProvider: "OpenAI", EndpointProfile: "dashscope"},
	"Moonshot":          {ClientProvider: "OpenAI", EndpointProfile: "moonshot"},
	"MiniMax":           {ClientProvider: "OpenAI", EndpointProfile: "minimax"},
	"ModelArts":         {ClientProvider: "OpenAI", EndpointProfile: "modelarts"},
	"VolcEngine":        {ClientProvider: "OpenAI", EndpointProfile: "volcengine"},
	"Qianfan":           {ClientProvider: "OpenAI", EndpointProfile: "qianfan"},
	"Zhipu":             {ClientProvider: "OpenAI", EndpointProfile: "zhipu"},
	"MiMo":              {ClientProvider: "OpenAI", EndpointProfile: "mimo"},
	"InferenceAffinity": {ClientProvider: "OpenAI", EndpointProfile: "openai_compatible", KVCacheMode: "release"},
	"AscendAffinity": {
		ClientProvider:  "OpenAI",
		EndpointProfile: "openai_compatible",
		AuthMode:        llmschema.AuthModeCustomHeaders,
		KVCacheMode:     "affinity",
	},
	"OpenAIAccount": {
		ClientProvider: "OpenAI",
		APIMode:        llmschema.APIModeResponses,
		AuthMode:       llmschema.AuthModeOpenAIAccountOAuth,
	},
}

// anthropicCompatibleProviders 走 anthropic_messages 协议时改用 Anthropic 客户端的 provider 集合。
// Python: endpoint_profiles.py:143-155 _ANTHROPIC_COMPATIBLE_PROVIDER_ALIASES（10 个）
var anthropicCompatibleProviders = map[string]struct{}{
	"DeepSeek": {}, "OpenRouter": {}, "DashScope": {}, "Moonshot": {}, "MiniMax": {},
	"ModelArts": {}, "VolcEngine": {}, "Qianfan": {}, "Zhipu": {}, "MiMo": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeModelClientConfig 返回携带协议/档案/鉴权/协议模式元数据的归一化配置副本。
// 不修改入参；未知 provider 原样返回。
// Python: endpoint_profiles.py:192-232 normalize_model_client_config
func NormalizeModelClientConfig(config *llmschema.ModelClientConfig) *llmschema.ModelClientConfig {
	if config == nil {
		return nil
	}
	// 浅拷贝后对 Extensions/Extra 深拷贝，避免与入参共享可变状态
	out := *config
	out.Extra = cloneAnyMap(config.Extra)
	out.Extensions = cloneExtensions(config.Extensions)

	provider := config.ClientProvider
	alias, ok := legacyProviderAliases[provider]
	if !ok {
		return &out
	}

	// 协议为 anthropic_messages 且 provider 在 anthropic 兼容集合内 → 改用 Anthropic 客户端。
	// 这是 Anthropic 兼容网关的入口，无视原 provider 的别名档案。
	// Python: endpoint_profiles.py:203-205
	if out.APIMode == "anthropic_messages" {
		if _, compat := anthropicCompatibleProviders[provider]; compat {
			out.ClientProvider = "Anthropic"
			setLegacyProvider(&out, provider)
			return &out
		}
	}

	out.ClientProvider = alias.ClientProvider
	// 显式 endpoint_profile 优先，别名只补空缺
	if out.EndpointProfile == "" && alias.EndpointProfile != "" {
		out.EndpointProfile = alias.EndpointProfile
	}
	// OpenAIAccount 的 auth_mode=api_key / api_mode="" 是打印出来的默认值，不算用户意图
	// Python: endpoint_profiles.py:183-190 _is_implicit_openai_account_default
	if alias.APIMode != "" && (out.APIMode == "" || isImplicitOpenAIAccountDefault(provider, "api_mode", out.APIMode)) {
		out.APIMode = alias.APIMode
	}
	if alias.AuthMode != "" && (out.AuthMode == "" || out.AuthMode == llmschema.AuthModeAPIKey) {
		out.AuthMode = alias.AuthMode
	}
	// extensions 走 deep_merge_defaults 语义：别名值只作默认，用户显式值优先。
	// Python: endpoint_profiles.py:220-230（deep_merge_defaults(explicit, normalized)）
	//   merged = deepcopy(defaults); merged.update(target) —— target 即显式值，最终胜出
	//
	// Go 无 model_fields_set，用「零值即未设置」近似：KVCache.Mode 空串时填别名值，
	// 非空则保留用户显式设置。AffinityField 同理由结构体默认值承担。
	if alias.KVCacheMode != "" {
		if out.Extensions == nil {
			out.Extensions = &llmschema.LLMExtensionsConfig{}
		}
		if out.Extensions.KVCache.Mode == "" {
			out.Extensions.KVCache.Mode = alias.KVCacheMode
		}
	}
	setLegacyProvider(&out, provider)
	return &out
}

// LegacyProvider 返回归一前的 provider 原名（provenance）。
// 未归一（无别名）时返回当前 ClientProvider。
// Python: reasoning.py:100-103 provider_identity
func LegacyProvider(config *llmschema.ModelClientConfig) string {
	if config == nil {
		return ""
	}
	if config.Extra != nil {
		if v, ok := config.Extra[legacyProviderExtraKey].(string); ok && v != "" {
			return v
		}
	}
	return config.ClientProvider
}

// ResolveEndpointProfile 解析生效的端点档案；未配置时按 provider 推断缺省档案。
// Python: endpoint_profiles.py:235-247 resolve_endpoint_profile
func ResolveEndpointProfile(config *llmschema.ModelClientConfig) (EndpointProfile, bool) {
	name := ""
	if config != nil {
		name = config.EndpointProfile
	}
	if name == "" {
		name = "openai_compatible"
		if config != nil && config.ClientProvider == "OpenAI" {
			name = "openai"
		}
	}
	profile, ok := endpointProfiles[name]
	return profile, ok
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setLegacyProvider 记录归一前的 provider 原名。
func setLegacyProvider(config *llmschema.ModelClientConfig, provider string) {
	if config.Extra == nil {
		config.Extra = make(map[string]any, 1)
	}
	config.Extra[legacyProviderExtraKey] = provider
}

// isImplicitOpenAIAccountDefault 判断是否是 OpenAIAccount 隐式默认值。
// Python: endpoint_profiles.py:183-190 _is_implicit_openai_account_default
func isImplicitOpenAIAccountDefault(provider, key string, current any) bool {
	if provider != "OpenAIAccount" {
		return false
	}
	switch key {
	case "api_mode":
		return current == "" || current == nil
	case "auth_mode":
		return current == "api_key" || current == llmschema.AuthModeAPIKey
	}
	return false
}

// cloneAnyMap 浅拷贝 map。
func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// cloneExtensions 深拷贝扩展配置。
func cloneExtensions(in *llmschema.LLMExtensionsConfig) *llmschema.LLMExtensionsConfig {
	if in == nil {
		return nil
	}
	return &llmschema.LLMExtensionsConfig{
		PromptCache:      cloneAnyMap(in.PromptCache),
		KVCache:          in.KVCache,
		ResponseFields:   cloneAnyMap(in.ResponseFields),
		RequestExtraBody: cloneAnyMap(in.RequestExtraBody),
	}
}
